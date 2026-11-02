package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type server struct {
	store   *Store
	builder *Builder
	token   string
}

func main() {
	home, _ := os.UserHomeDir()
	addr := flag.String("addr", envOr("BUILD_SERVER_ADDR", "0.0.0.0:8085"), "listen address")
	dataDir := flag.String("data", envOr("BUILD_SERVER_DATA", filepath.Join(home, ".local", "share", "android-build-server")), "data directory")
	token := flag.String("token", os.Getenv("BUILD_SERVER_TOKEN"), "optional token required to create/delete builds")
	timeout := flag.Duration("timeout", 30*time.Minute, "max duration of a single build")
	flag.Parse()

	if os.Getenv("ANDROID_HOME") == "" {
		if sdk := androidSdkDir(); sdk != "" {
			os.Setenv("ANDROID_HOME", sdk)
		}
	}

	store, err := OpenStore(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	builder := NewBuilder(store, *timeout)
	srv := &server{store: store, builder: builder, token: *token}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go builder.Run(ctx)

	httpSrv := &http.Server{Addr: *addr, Handler: logRequests(srv.routes())}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("android build server listening on %s (data: %s)", *addr, *dataDir)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/builds", s.listBuilds)
	mux.HandleFunc("POST /api/builds", s.auth(s.createBuild))
	mux.HandleFunc("GET /api/builds/{id}", s.getBuild)
	mux.HandleFunc("DELETE /api/builds/{id}", s.auth(s.deleteBuild))
	mux.HandleFunc("GET /api/builds/{id}/log", s.getLog)
	mux.HandleFunc("GET /api/builds/{id}/apk", s.getApk)
	return mux
}

func (s *server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, http.StatusUnauthorized, "invalid or missing token")
			return
		}
		next(w, r)
	}
}

func (s *server) listBuilds(w http.ResponseWriter, r *http.Request) {
	builds := s.store.List()
	if status := r.URL.Query().Get("status"); status != "" {
		filtered := builds[:0]
		for _, b := range builds {
			if string(b.Status) == status {
				filtered = append(filtered, b)
			}
		}
		builds = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"builds": builds})
}

type createRequest struct {
	RepoPath string `json:"repo_path"`
	Task     string `json:"task"`
	Name     string `json:"name"`
}

func (s *server) createBuild(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RepoPath == "" || !filepath.IsAbs(req.RepoPath) {
		writeError(w, http.StatusBadRequest, "repo_path must be an absolute path")
		return
	}
	repo := filepath.Clean(req.RepoPath)
	if fi, err := os.Stat(repo); err != nil || !fi.IsDir() {
		writeError(w, http.StatusBadRequest, "repo_path does not exist or is not a directory")
		return
	}
	_, errGradlew := os.Stat(filepath.Join(repo, "gradlew"))
	_, errSettings := os.Stat(filepath.Join(repo, "settings.gradle"))
	_, errSettingsKts := os.Stat(filepath.Join(repo, "settings.gradle.kts"))
	if errGradlew != nil && errSettings != nil && errSettingsKts != nil {
		writeError(w, http.StatusBadRequest, "repo_path does not look like a Gradle project")
		return
	}
	task := req.Task
	if task == "" {
		task = "assembleDebug"
	}
	if strings.HasPrefix(task, "-") || strings.ContainsAny(task, " \t\n") {
		writeError(w, http.StatusBadRequest, "task must be a single Gradle task name")
		return
	}
	name := req.Name
	if name == "" {
		name = filepath.Base(repo)
	}
	branch, commit, msg := gitInfo(repo)
	build := &Build{
		ID:        newID(),
		Project:   name,
		RepoPath:  repo,
		Task:      task,
		Branch:    branch,
		Commit:    commit,
		CommitMsg: msg,
		Status:    StatusQueued,
		CreatedAt: time.Now(),
	}
	if err := s.store.Add(build); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.builder.Enqueue(build.ID); err != nil {
		s.store.Update(build.ID, func(b *Build) { b.Status = StatusFailed; b.Error = err.Error() })
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, build)
}

func (s *server) getBuild(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *server) deleteBuild(w http.ResponseWriter, r *http.Request) {
	err := s.store.Delete(r.PathValue("id"))
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusConflict, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) getLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.Get(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.ServeFile(w, r, s.store.LogPath(id))
}

func (s *server) getApk(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if b.Status != StatusSuccess {
		writeError(w, http.StatusNotFound, "build has no APK")
		return
	}
	f, err := os.Open(s.store.ApkPath(b.ID))
	if err != nil {
		writeError(w, http.StatusNotFound, "APK file missing")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filename := fmt.Sprintf("%s-%s.apk", b.Project, b.ID)
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	// ServeContent sets Content-Length and supports Range requests.
	http.ServeContent(w, r, filename, fi.ModTime(), f)
}

func newID() string {
	buf := make([]byte, 4)
	rand.Read(buf)
	return time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(buf)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s (%s)", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
