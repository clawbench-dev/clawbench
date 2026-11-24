package backends_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"clawbench/internal/ai/backends"

	// Register every backend so the coverage check sees the real set.
	_ "clawbench/internal/ai/backends/all"
)

// The frontend keeps its own backend id → display name and id → icon maps
// (web/src/utils/backendNames.ts, agentIcons.ts). Nothing forces a new backend
// to update them, and the vitest cases only check one direction ("every id in
// MY list has an icon"), so a forgotten entry is silent: the agent list shows
// the raw backend id and an initial letter instead of the brand.
//
// This is the reverse check. It reads the two source files as text on purpose —
// the ids are plain identifiers, and a Go test cannot import TypeScript.
func TestFrontendMapsCoverEveryBackend(t *testing.T) {
	names := readRepoFile(t, "web/src/utils/backendNames.ts")
	icons := readRepoFile(t, "web/src/utils/agentIcons.ts")

	checked := 0
	for _, p := range backends.All() {
		id := p.Spec.Backend
		if id == "" {
			// Test-only plugins register with an empty Backend stub.
			continue
		}
		checked++
		// A map entry is `<indent>id: value` with the id optionally quoted.
		entry := regexp.MustCompile(`(?m)^\s+'?` + regexp.QuoteMeta(id) + `'?:`)
		if !entry.MatchString(names) {
			t.Errorf("backend %q has no entry in web/src/utils/backendNames.ts — "+
				"the UI would show the raw id instead of a display name", id)
		}
		if !entry.MatchString(icons) {
			t.Errorf("backend %q has no entry in web/src/utils/agentIcons.ts — "+
				"the UI would fall back to an initial letter", id)
		}
	}
	if checked < 20 {
		t.Fatalf("only %d backends were checked; the registry looks incomplete", checked)
	}
}

// readRepoFile reads a file relative to the repository root. The test's working
// directory is this package (internal/ai/backends).
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
