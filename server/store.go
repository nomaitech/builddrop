package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	StatusQueued   Status = "queued"
	StatusBuilding Status = "building"
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
)

type Build struct {
	ID          string     `json:"id"`
	Project     string     `json:"project"`
	RepoPath    string     `json:"repo_path"`
	Task        string     `json:"task"`
	Branch      string     `json:"branch,omitempty"`
	Commit      string     `json:"commit,omitempty"`
	CommitMsg   string     `json:"commit_message,omitempty"`
	Dirty       bool       `json:"dirty,omitempty"`
	SourceHash  string     `json:"source_hash,omitempty"`
	Status      Status     `json:"status"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	ApkName     string     `json:"apk_name,omitempty"`
	ApkSize     int64      `json:"apk_size,omitempty"`
	PackageName string     `json:"package_name,omitempty"`
	VersionName string     `json:"version_name,omitempty"`
	VersionCode string     `json:"version_code,omitempty"`
}

// Store keeps build metadata in memory and persists it to a JSON file.
type Store struct {
	mu     sync.RWMutex
	dir    string
	builds map[string]*Build
}

var ErrNotFound = errors.New("build not found")

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "builds"), 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, builds: map[string]*Build{}}
	data, err := os.ReadFile(s.indexPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(data) > 0 {
		var list []*Build
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		for _, b := range list {
			// Builds interrupted by a server restart can't be resumed.
			if b.Status == StatusQueued || b.Status == StatusBuilding {
				b.Status = StatusFailed
				b.Error = "interrupted by server restart"
			}
			s.builds[b.ID] = b
		}
	}
	return s, s.save()
}

func (s *Store) indexPath() string { return filepath.Join(s.dir, "builds.json") }

func (s *Store) BuildDir(id string) string { return filepath.Join(s.dir, "builds", id) }
func (s *Store) LogPath(id string) string  { return filepath.Join(s.BuildDir(id), "build.log") }
func (s *Store) ApkPath(id string) string  { return filepath.Join(s.BuildDir(id), "app.apk") }

// save must be called with s.mu held (or before the store is shared).
func (s *Store) save() error {
	list := s.listLocked()
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath())
}

func (s *Store) listLocked() []Build {
	list := make([]Build, 0, len(s.builds))
	for _, b := range s.builds {
		list = append(list, *b)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return list
}

func (s *Store) List() []Build {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) Get(id string) (Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.builds[id]
	if !ok {
		return Build{}, ErrNotFound
	}
	return *b, nil
}

// AddUnlessDuplicate adds b unless a build of the same source (repo, task and
// source hash) is already queued, building or available. It returns the
// existing build and true in that case. The check and insert are atomic.
func (s *Store) AddUnlessDuplicate(b *Build) (Build, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.SourceHash != "" {
		for _, existing := range s.listLocked() {
			if existing.RepoPath != b.RepoPath || existing.Task != b.Task || existing.SourceHash != b.SourceHash {
				continue
			}
			switch existing.Status {
			case StatusQueued, StatusBuilding:
				return existing, true, nil
			case StatusSuccess:
				if _, err := os.Stat(s.ApkPath(existing.ID)); err == nil {
					return existing, true, nil
				}
			}
		}
	}
	if err := os.MkdirAll(s.BuildDir(b.ID), 0o755); err != nil {
		return Build{}, false, err
	}
	s.builds[b.ID] = b
	return *b, false, s.save()
}

func (s *Store) Update(id string, fn func(b *Build)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.builds[id]
	if !ok {
		return ErrNotFound
	}
	fn(b)
	return s.save()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.builds[id]
	if !ok {
		return ErrNotFound
	}
	if b.Status == StatusQueued || b.Status == StatusBuilding {
		return errors.New("cannot delete a build that is in progress")
	}
	delete(s.builds, id)
	if err := os.RemoveAll(s.BuildDir(id)); err != nil {
		return err
	}
	return s.save()
}
