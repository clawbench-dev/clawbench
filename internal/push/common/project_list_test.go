package common

import (
	"strings"
	"testing"
)

func TestRenderProjectList_IndexesAndPaths(t *testing.T) {
	refs := []ProjectRef{
		{Index: 1, Path: "/a/one", Exists: true},
		{Index: 2, Path: "/b/two", Exists: false},
	}
	out := RenderProjectList(refs)

	if !strings.Contains(out, "1. `/a/one`") {
		t.Errorf("missing index 1 row:\n%s", out)
	}
	if !strings.Contains(out, "2. `/b/two`") {
		t.Errorf("missing index 2 row:\n%s", out)
	}
	// A missing directory is flagged on its own row only.
	if strings.Count(out, "⚠️") != 1 {
		t.Errorf("expected exactly one missing-dir marker:\n%s", out)
	}
	if !strings.Contains(out, "⚠️") || strings.Contains(out, "`/a/one` ⚠️") {
		t.Errorf("missing-dir marker on the wrong row:\n%s", out)
	}
}

func TestRenderProjectList_Empty(t *testing.T) {
	out := RenderProjectList(nil)
	if !strings.Contains(out, T("PushNewNoProjects")) {
		t.Errorf("expected empty-list hint:\n%s", out)
	}
}

func TestSelectProject_Index(t *testing.T) {
	refs := []ProjectRef{
		{Index: 1, Path: "/a/one", Exists: true},
		{Index: 2, Path: "/b/two", Exists: true},
	}
	path, ref, err := SelectProject(refs, "2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/b/two" || ref.Index != 2 {
		t.Errorf("got %q ref %+v", path, ref)
	}
}

func TestSelectProject_ExactPath(t *testing.T) {
	refs := []ProjectRef{{Index: 1, Path: "/a/my project", Exists: true}}
	path, _, err := SelectProject(refs, "/a/my project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/a/my project" {
		t.Errorf("got %q", path)
	}
}

func TestSelectProject_RejectsBasename(t *testing.T) {
	// "one" is a directory basename; basename matching is deliberately absent
	// so two projects sharing a name can never be confused.
	refs := []ProjectRef{{Index: 1, Path: "/a/one", Exists: true}}
	if _, _, err := SelectProject(refs, "one"); err == nil {
		t.Fatal("expected basename selector to be rejected")
	}
}

func TestSelectProject_OutOfRange(t *testing.T) {
	refs := []ProjectRef{{Index: 1, Path: "/a/one"}}
	_, _, err := SelectProject(refs, "9")
	if err == nil {
		t.Fatal("expected out-of-range error")
	}
	if !strings.Contains(err.Error(), "1") {
		t.Errorf("out-of-range error should name the total: %v", err)
	}
}

func TestSelectProject_Empty(t *testing.T) {
	if _, _, err := SelectProject(nil, "  "); err == nil {
		t.Fatal("expected error for empty selector")
	}
}
