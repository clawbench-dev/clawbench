package common

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderProjectList_IndexesAndPaths(t *testing.T) {
	refs := []ProjectRef{
		{Index: 1, Path: "/a/one", Exists: true},
		{Index: 2, Path: "/b/two", Exists: false},
	}
	out := RenderProjectList(refs, 0)

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
	out := RenderProjectList(nil, 0)
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

func TestSelectProject_IgnoresTextAfterIndex(t *testing.T) {
	// The agreed contract is that text after the selector is ignored, so
	// "/new 1 please" still means project 1 rather than failing to resolve.
	refs := []ProjectRef{
		{Index: 1, Path: "/a/one", Exists: true},
		{Index: 2, Path: "/b/two", Exists: true},
	}
	path, ref, err := SelectProject(refs, "1 please")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/a/one" || ref.Index != 1 {
		t.Errorf("got %q ref %+v, want project 1", path, ref)
	}
}

func TestSelectProject_PathWithSpacesKeepsWholeArg(t *testing.T) {
	// The path form must NOT be truncated at the first space, or projects with
	// spaces in their names become unreachable.
	refs := []ProjectRef{{Index: 1, Path: "/a/my project", Exists: true}}
	path, _, err := SelectProject(refs, "/a/my project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/a/my project" {
		t.Errorf("got %q", path)
	}
}

// The list is rendered into a single platform message, so a project count that
// exceeds the channel's ceiling must be cut rather than rejected by the
// platform. Cutting mid-entry would leave a dangling backtick and half a path,
// so entries are dropped whole and the note says how many were shown.
func TestRenderProjectList_TruncatesAtEntryBoundary(t *testing.T) {
	var refs []ProjectRef
	for i := 1; i <= 40; i++ {
		refs = append(refs, ProjectRef{
			Index:  i,
			Path:   fmt.Sprintf("/srv/projects/project-%02d-with-a-longish-name", i),
			Exists: true,
		})
	}

	const limit = 600
	out := RenderProjectList(refs, limit)

	if n := len([]rune(out)); n > limit {
		t.Fatalf("rendered %d runes, over the %d limit", n, limit)
	}

	// Every surviving row must be a complete entry: `<n>. `<path>``. A cut inside
	// a row would leave an odd number of backticks on that line.
	rows := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, ". `") {
			continue
		}
		rows++
		if strings.Count(line, "`") != 2 {
			t.Errorf("row is not a complete entry: %q", line)
		}
	}
	if rows == 0 || rows >= len(refs) {
		t.Fatalf("rows = %d, want a partial list of the %d projects", rows, len(refs))
	}

	// Indices stay contiguous from 1, so "the index you read is the index you
	// type" still holds for every row that survived.
	for i := 1; i <= rows; i++ {
		if !strings.Contains(out, fmt.Sprintf("%d. `", i)) {
			t.Errorf("index %d missing from the truncated list:\n%s", i, out)
		}
	}

	// The note must name the real counts — a wrong pair would tell the user to
	// look for projects that are not there.
	wantNote := T("PushNewListTruncated", map[string]any{
		TemplateCount: rows,
		TemplateTotal: len(refs),
	})
	if !strings.Contains(out, wantNote) {
		t.Fatalf("missing truncation note %q:\n%s", wantNote, out)
	}
}

func TestRenderProjectList_ShortListIsUntouched(t *testing.T) {
	refs := []ProjectRef{{Index: 1, Path: "/a", Exists: true}}
	full := RenderProjectList(refs, 0)
	if got := RenderProjectList(refs, 4000); got != full {
		t.Errorf("a list that fits must not be altered:\n%s", got)
	}
}

// A budget smaller than the header cannot be satisfied by dropping entries; the
// fallback must still produce something sendable rather than a panic.
func TestRenderProjectList_ImpossibleBudgetStillFits(t *testing.T) {
	refs := []ProjectRef{{Index: 1, Path: "/a", Exists: true}}
	const limit = 20
	out := RenderProjectList(refs, limit)
	if n := len([]rune(out)); n > limit+1 { // +1 for the ellipsis
		t.Fatalf("rendered %d runes for a %d budget: %q", n, limit, out)
	}
}
