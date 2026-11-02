package main

import "testing"

func TestGroupBuilds(t *testing.T) {
	// Newest first, as returned by Store.List.
	builds := []Build{
		{ID: "a3", RepoPath: "/a", Status: StatusBuilding},
		{ID: "b2", RepoPath: "/b", Status: StatusSuccess},
		{ID: "a2", RepoPath: "/a", Status: StatusSuccess},
		{ID: "c1", RepoPath: "/c", Status: StatusFailed},
		{ID: "a1", RepoPath: "/a", Status: StatusSuccess},
		{ID: "b1", RepoPath: "/b", Status: StatusFailed},
	}
	groups := groupBuilds(builds)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	a, b, c := groups[0], groups[1], groups[2]
	if a.Primary.ID != "a2" || a.Pending == nil || a.Pending.ID != "a3" || len(a.Older) != 1 || a.Older[0].ID != "a1" {
		t.Errorf("group a: %+v", a)
	}
	if b.Primary.ID != "b2" || b.Pending != nil || len(b.Older) != 1 || b.Older[0].ID != "b1" {
		t.Errorf("group b: %+v", b)
	}
	if c.Primary.ID != "c1" || c.Pending != nil || len(c.Older) != 0 {
		t.Errorf("group c: %+v", c)
	}
}
