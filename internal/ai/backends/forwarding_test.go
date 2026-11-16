package backends_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// pluginsAtInit snapshots the registry during package init, before any test can
// call ResetForTest. Go runs tests in one process in file order, so a test that
// reads backends.All() directly may legitimately see an empty registry.
var pluginsAtInit = func() []*backends.BackendPlugin {
	return backends.All()
}()

// TestRegisterForwardsCapabilities pins the contract that makes ONE init()
// enough for a backend: everything a plugin declares must reach the registry
// that consumes it, and nothing may be registered that the plugin did not
// declare.
//
// Both directions matter. Forwarding is what lets a new backend exist as a
// single directory; the reverse direction is what keeps a stale
// ai.RegisterBackend call from silently resurrecting a backend that no longer
// declares a CLI transport.
func TestRegisterForwardsCapabilities(t *testing.T) {
	if len(pluginsAtInit) == 0 {
		t.Fatal("no backends registered at init; the check would be vacuous")
	}

	for _, p := range pluginsAtInit {
		hasFactory := ai.LookupBackendFactoryForTest(p.ID) != nil
		wantFactory := p.CLI != nil && p.CLI.NewBackend != nil
		if hasFactory != wantFactory {
			t.Errorf("%s: CLI factory registered=%v, plugin declares CLI=%v", p.ID, hasFactory, wantFactory)
		}

		if p.ModelSource != nil && !model.HasModelSource(p.ID) {
			t.Errorf("%s: plugin declares a ModelSource but none was registered", p.ID)
		}
		if p.ModelSource == nil && model.HasModelSource(p.ID) {
			t.Errorf("%s: a ModelSource was registered but the plugin declares none", p.ID)
		}

		if p.ListSessions != nil && !ai.HasListSessionsFromDisk(p.ID) {
			t.Errorf("%s: plugin declares ListSessions but no scanner was registered", p.ID)
		}
		if p.ListSessions == nil && ai.HasListSessionsFromDisk(p.ID) {
			t.Errorf("%s: an on-disk scanner was registered but the plugin declares none", p.ID)
		}
	}
}

// TestSubPackagesDoNotRegisterDirectly is a source-level guard: the three
// legacy registration calls must appear only in backends/registry.go (the
// forwarding point). A sub-package calling them directly would reintroduce the
// drift this design removes — a backend that half-registers itself, or one
// whose CLI factory outlives the plugin that declared it.
//
// It cannot be expressed behaviourally: a direct call produces exactly the same
// registries, just from a second place.
func TestSubPackagesDoNotRegisterDirectly(t *testing.T) {
	forbidden := []string{
		"ai.RegisterBackend(",
		"model.RegisterModelSource(",
		"ai.ListSessionsFromDiskRegister(",
	}

	// cwd is this package's directory; backend sub-packages are direct children.
	files, err := filepath.Glob("./*/*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no sub-package sources found; the guard would be vacuous")
	}

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, needle := range forbidden {
			if strings.Contains(text, needle) {
				t.Errorf("%s calls %s directly; declare it on the BackendPlugin instead "+
					"(see internal/ai/backends/registry.go Register)", path, needle)
			}
		}
	}
}
