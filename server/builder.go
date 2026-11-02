package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Builder runs Gradle builds one at a time from a queue.
type Builder struct {
	store   *Store
	queue   chan string
	timeout time.Duration
	aapt2   string
}

func NewBuilder(store *Store, timeout time.Duration) *Builder {
	return &Builder{
		store:   store,
		queue:   make(chan string, 100),
		timeout: timeout,
		aapt2:   findAapt2(),
	}
}

func (b *Builder) Enqueue(id string) error {
	select {
	case b.queue <- id:
		return nil
	default:
		return errors.New("build queue is full")
	}
}

func (b *Builder) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-b.queue:
			b.build(ctx, id)
		}
	}
}

func (b *Builder) build(ctx context.Context, id string) {
	build, err := b.store.Get(id)
	if err != nil {
		return
	}
	now := time.Now()
	b.store.Update(id, func(bd *Build) {
		bd.Status = StatusBuilding
		bd.StartedAt = &now
	})
	log.Printf("build %s: starting %s in %s", id, build.Task, build.RepoPath)

	apk, buildErr := b.runGradle(ctx, build)
	finished := time.Now()
	if buildErr != nil {
		log.Printf("build %s: failed: %v", id, buildErr)
		b.store.Update(id, func(bd *Build) {
			bd.Status = StatusFailed
			bd.Error = buildErr.Error()
			bd.FinishedAt = &finished
		})
		return
	}

	meta := b.apkMetadata(b.store.ApkPath(id))
	log.Printf("build %s: success (%s)", id, apk)
	b.store.Update(id, func(bd *Build) {
		bd.Status = StatusSuccess
		bd.FinishedAt = &finished
		bd.ApkName = filepath.Base(apk)
		if fi, err := os.Stat(b.store.ApkPath(id)); err == nil {
			bd.ApkSize = fi.Size()
		}
		// Without an explicit name, the app's label beats the folder name
		// (which is often just "android" or "app").
		if bd.AutoName && meta["label"] != "" {
			bd.Project = meta["label"]
		}
		bd.PackageName = meta["package"]
		bd.VersionName = meta["versionName"]
		bd.VersionCode = meta["versionCode"]
	})
}

// runGradle executes the Gradle task and copies the resulting APK into the
// build directory. It returns the path of the source APK.
func (b *Builder) runGradle(ctx context.Context, build Build) (string, error) {
	logFile, err := os.Create(b.store.LogPath(build.ID))
	if err != nil {
		return "", err
	}
	defer logFile.Close()

	cmdName := "gradle"
	if _, err := os.Stat(filepath.Join(build.RepoPath, "gradlew")); err == nil {
		cmdName = "./gradlew"
	}
	fmt.Fprintf(logFile, "$ %s %s --console=plain\n\n", cmdName, build.Task)

	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdName, build.Task, "--console=plain")
	cmd.Dir = build.RepoPath
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("build timed out after %s", b.timeout)
		}
		return "", fmt.Errorf("gradle %s failed: %v (see log)", build.Task, err)
	}

	apk, err := findApk(build.RepoPath, build.Task)
	if err != nil {
		fmt.Fprintf(logFile, "\n%v\n", err)
		return "", err
	}
	fmt.Fprintf(logFile, "\nCollected APK: %s\n", apk)
	return apk, copyFile(apk, b.store.ApkPath(build.ID))
}

// findApk looks for APKs under build/outputs/apk, preferring ones whose file
// name matches the variant of the Gradle task, then the most recently modified.
func findApk(root, task string) (string, error) {
	type candidate struct {
		path string
		mod  time.Time
	}
	var found []candidate
	sep := string(filepath.Separator)
	marker := sep + "build" + sep + "outputs" + sep + "apk" + sep
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".gradle" || name == "node_modules" || name == "intermediates" || name == "tmp" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".apk") || !strings.Contains(path, marker) {
			return nil
		}
		if strings.Contains(d.Name(), "androidTest") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		found = append(found, candidate{path, info.ModTime()})
		return nil
	})
	if len(found) == 0 {
		return "", errors.New("no APK found under build/outputs/apk")
	}
	variant := taskVariant(task)
	matches := func(c candidate) bool {
		return variant != "" && strings.Contains(strings.ToLower(strings.ReplaceAll(filepath.Base(c.path), "-", "")), variant)
	}
	sort.Slice(found, func(i, j int) bool {
		if mi, mj := matches(found[i]), matches(found[j]); mi != mj {
			return mi
		}
		return found[i].mod.After(found[j].mod)
	})
	return found[0].path, nil
}

// taskVariant turns ":app:assembleFreeDebug" into "freedebug".
func taskVariant(task string) string {
	if i := strings.LastIndex(task, ":"); i >= 0 {
		task = task[i+1:]
	}
	return strings.ToLower(strings.TrimPrefix(task, "assemble"))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func androidSdkDir() string {
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Android", "Sdk")
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	return ""
}

func findAapt2() string {
	if p, err := exec.LookPath("aapt2"); err == nil {
		return p
	}
	sdk := androidSdkDir()
	if sdk == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(sdk, "build-tools", "*", "aapt2"))
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	return matches[len(matches)-1]
}

var badgingRe = regexp.MustCompile(`(?:^|\s)(name|versionCode|versionName)='([^']*)'`)

// apkMetadata extracts package name and version via aapt2, best effort.
func (b *Builder) apkMetadata(apk string) map[string]string {
	meta := map[string]string{}
	if b.aapt2 == "" {
		return meta
	}
	out, err := exec.Command(b.aapt2, "dump", "badging", apk).Output()
	if err != nil {
		return meta
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "package:"):
			for _, m := range badgingRe.FindAllStringSubmatch(line, -1) {
				key := m[1]
				if key == "name" {
					key = "package"
				}
				meta[key] = m[2]
			}
		case strings.HasPrefix(line, "application-label:"):
			meta["label"] = strings.Trim(strings.TrimPrefix(line, "application-label:"), "'")
		}
	}
	return meta
}

func gitInfo(repo string) (branch, commit, msg string) {
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	return run("rev-parse", "--abbrev-ref", "HEAD"), run("rev-parse", "--short", "HEAD"), run("log", "-1", "--pretty=%s")
}

// sourceFingerprint identifies the exact source being built: the HEAD commit
// plus any uncommitted changes and untracked files under repo. It returns ""
// when repo is not inside a git work tree, which disables de-duplication.
func sourceFingerprint(repo string) (hash string, dirty bool) {
	git := func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil {
		return "", false
	}
	h := sha256.New()
	h.Write(head)

	diff, err := git("diff", "HEAD", "--binary", "--", ".")
	if err != nil {
		return "", false
	}
	h.Write(diff)
	dirty = len(diff) > 0

	untracked, err := git("ls-files", "--others", "--exclude-standard", "-z", "--", ".")
	if err != nil {
		return "", false
	}
	for _, name := range bytes.Split(untracked, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		dirty = true
		h.Write(name)
		if data, err := os.ReadFile(filepath.Join(repo, string(name))); err == nil {
			sum := sha256.Sum256(data)
			h.Write(sum[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), dirty
}
