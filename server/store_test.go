package main

import (
	"os"
	"testing"
	"time"
)

func TestAddUnlessDuplicate(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	mk := func(id, hash string) *Build {
		return &Build{ID: id, RepoPath: "/repo", Task: "assembleDebug", SourceHash: hash, Status: StatusQueued, CreatedAt: time.Now()}
	}

	if _, dup, _ := s.AddUnlessDuplicate(mk("a", "h1")); dup {
		t.Fatal("first build reported as duplicate")
	}
	if got, dup, _ := s.AddUnlessDuplicate(mk("b", "h1")); !dup || got.ID != "a" {
		t.Fatalf("queued build not de-duplicated: dup=%v id=%s", dup, got.ID)
	}
	if _, dup, _ := s.AddUnlessDuplicate(mk("c", "h2")); dup {
		t.Fatal("different source reported as duplicate")
	}

	// A failed build doesn't block a retry of the same source.
	s.Update("c", func(b *Build) { b.Status = StatusFailed })
	if _, dup, _ := s.AddUnlessDuplicate(mk("d", "h2")); dup {
		t.Fatal("failed build blocked a retry")
	}

	// A successful build is only reused while its APK still exists.
	s.Update("a", func(b *Build) { b.Status = StatusSuccess })
	if _, dup, _ := s.AddUnlessDuplicate(mk("e", "h1")); dup {
		t.Fatal("success without APK reported as duplicate")
	}
	s.Update("e", func(b *Build) { b.Status = StatusSuccess })
	os.WriteFile(s.ApkPath("e"), []byte("apk"), 0o644)
	if got, dup, _ := s.AddUnlessDuplicate(mk("f", "h1")); !dup || got.ID != "e" {
		t.Fatalf("successful build not reused: dup=%v id=%s", dup, got.ID)
	}

	// An empty hash (forced, or not a git repo) never de-duplicates.
	if _, dup, _ := s.AddUnlessDuplicate(mk("g", "")); dup {
		t.Fatal("empty hash reported as duplicate")
	}
	if _, dup, _ := s.AddUnlessDuplicate(mk("h", "")); dup {
		t.Fatal("empty hash reported as duplicate")
	}
}
