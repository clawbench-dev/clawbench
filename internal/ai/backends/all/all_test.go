package all

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// TestAllImportsMatchDirectories is the guard that makes "add a directory" a
// safe instruction: if someone creates a backend package and forgets to
// regenerate, this fails with the exact command to run instead of the backend
// silently never registering.
func TestAllImportsMatchDirectories(t *testing.T) {
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("read backends dir: %v", err)
	}

	var want []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "all" {
			continue
		}
		if !dirHasGoFile(t, filepath.Join("..", e.Name())) {
			continue
		}
		want = append(want, e.Name())
	}
	sort.Strings(want)
	if len(want) == 0 {
		t.Fatal("no backend directories found; the check would be vacuous")
	}

	data, err := os.ReadFile("all.go")
	if err != nil {
		t.Fatalf("read all.go: %v", err)
	}
	got := parseImportedBackends(t, string(data))

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("all.go is out of date:\n  generated: %v\n  on disk:   %v\nrun: go generate ./internal/ai/backends/all",
			got, want)
	}
}

// TestAllPackageRegistersEveryBackend proves the generated file actually does
// its job: importing this package must leave every backend in the registry.
func TestAllPackageRegistersEveryBackend(t *testing.T) {
	entries, err := os.ReadDir("..")
	if err != nil {
		t.Fatalf("read backends dir: %v", err)
	}
	var want int
	for _, e := range entries {
		if e.IsDir() && e.Name() != "all" && dirHasGoFile(t, filepath.Join("..", e.Name())) {
			want++
		}
	}

	registered := len(backends.All())
	if registered != want {
		t.Fatalf("registry has %d backends after importing this package, want %d", registered, want)
	}
}

// TestAllPackageInstallsWiring proves the backends package's init() ran and
// connected internal/ai's lookup function variables.
//
// cmd/server/main.go imports ONLY this package (not the backends package
// directly): the wiring happens as a side effect of the sub-packages importing
// backends. If that ever stops holding, nothing fails to compile — tool names
// silently stop being normalized and backend specs go missing. This test is the
// only thing that would catch it.
func TestAllPackageInstallsWiring(t *testing.T) {
	checks := []struct {
		name string
		fn   any
	}{
		{"ai.LookupACPRemapsFn", ai.LookupACPRemapsFn},
		{"ai.LookupACPToolCallIDPrefixesFn", ai.LookupACPToolCallIDPrefixesFn},
		{"ai.LookupACPTitlePrefixesFn", ai.LookupACPTitlePrefixesFn},
		{"ai.LookupACPToolCallWrapperPrefixesFn", ai.LookupACPToolCallWrapperPrefixesFn},
		{"ai.LookupACPModesMirrorThoughtLevelFn", ai.LookupACPModesMirrorThoughtLevelFn},
		{"ai.LookupACPModelSwitchMethodFn", ai.LookupACPModelSwitchMethodFn},
		{"ai.LookupACPBehaviorFn", ai.LookupACPBehaviorFn},
		{"ai.LookupACPSpawnHooksFn", ai.LookupACPSpawnHooksFn},
		{"ai.LookupPluginCommandsFn", ai.LookupPluginCommandsFn},
		{"ai.LookupMidTurnInjectorFn", ai.LookupMidTurnInjectorFn},
		{"model.LoadBackendSpecs", model.LoadBackendSpecs},
		{"model.LookupBackendCapabilitiesFn", model.LookupBackendCapabilitiesFn},
	}
	for _, c := range checks {
		if c.fn == nil {
			t.Errorf("%s is nil: importing backends/all no longer installs the "+
				"function-variable wiring (see internal/ai/backends/acp_wire.go)", c.name)
		}
	}
}

// parseImportedBackends returns the backend names from the generated import
// block, in file order.
func parseImportedBackends(t *testing.T, src string) []string {
	t.Helper()
	const prefix = `_ "clawbench/internal/ai/backends/`
	var out []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(line, prefix), `"`)
		out = append(out, name)
	}
	if len(out) == 0 {
		t.Fatal("no backend imports found in all.go; the file looks hand-edited or empty")
	}
	return out
}

func dirHasGoFile(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}
