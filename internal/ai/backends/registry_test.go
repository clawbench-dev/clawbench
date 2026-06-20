package backends_test

import (
	"fmt"
	"sync"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	backends.ResetForTest()

	p := &backends.BackendPlugin{
		ID: "test-backend",
		Spec: model.BackendSpec{
			ID:        "test-backend",
			Backend:   "test-backend",
			Name:      "Test",
			Icon:      "T",
			Specialty: "test backend",
		},
		CLI: &backends.CLIPlugin{
			NewBackend: func() *ai.CLIBackend {
				return &ai.CLIBackend{}
			},
			ToolNameMap: map[string]string{"read_file": "Read"},
			InputRemaps: map[string]string{"filePath": "file_path"},
		},
		NeedsAutoResume: true,
	}

	backends.Register(p)

	got := backends.Lookup("test-backend")
	if got == nil {
		t.Fatal("expected to find registered backend, got nil")
	}
	if got.ID != "test-backend" {
		t.Errorf("expected ID 'test-backend', got %q", got.ID)
	}
	if got.CLI == nil {
		t.Error("expected CLI plugin to be non-nil")
	}
	if got.NeedsAutoResume != true {
		t.Error("expected NeedsAutoResume to be true")
	}
}

func TestRegistry_LookupNotFound(t *testing.T) {
	backends.ResetForTest()

	got := backends.Lookup("nonexistent")
	if got != nil {
		t.Errorf("expected nil for unregistered backend, got %+v", got)
	}
}

func TestRegistry_RegisterDuplicatePanics(t *testing.T) {
	backends.ResetForTest()

	p := &backends.BackendPlugin{ID: "dup"}
	backends.Register(p)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration, but no panic occurred")
		}
	}()
	backends.Register(&backends.BackendPlugin{ID: "dup"})
}

func TestRegistry_All(t *testing.T) {
	backends.ResetForTest()

	backends.Register(&backends.BackendPlugin{ID: "a"})
	backends.Register(&backends.BackendPlugin{ID: "b"})

	all := backends.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 backends, got %d", len(all))
	}

	ids := map[string]bool{}
	for _, p := range all {
		ids[p.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Errorf("expected backends 'a' and 'b', got %v", ids)
	}
}

func TestRegistry_AllSpecs(t *testing.T) {
	backends.ResetForTest()

	backends.Register(&backends.BackendPlugin{
		ID: "spec-test",
		Spec: model.BackendSpec{
			ID:      "spec-test",
			Backend: "spec-test",
			Name:    "SpecTest",
		},
	})

	specs := backends.AllSpecs()
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].ID != "spec-test" {
		t.Errorf("expected spec ID 'spec-test', got %q", specs[0].ID)
	}
}

func TestRegistry_ResetForTest(t *testing.T) {
	backends.ResetForTest()

	backends.Register(&backends.BackendPlugin{ID: "temp"})
	if backends.Lookup("temp") == nil {
		t.Fatal("expected to find 'temp' after registration")
	}

	backends.ResetForTest()

	if backends.Lookup("temp") != nil {
		t.Error("expected nil after ResetForTest, but found backend")
	}
}

func TestRegistry_ResetForTestConcurrent(t *testing.T) {
	backends.ResetForTest()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			backends.Register(&backends.BackendPlugin{ID: fmt.Sprintf("concurrent-%d", i)})
		}(i)
	}
	wg.Wait()

	all := backends.All()
	if len(all) != 10 {
		t.Errorf("expected 10 backends after concurrent registration, got %d", len(all))
	}
}

func TestRegistry_LookupACPRemaps(t *testing.T) {
	backends.ResetForTest()

	// Backend with ACP remaps
	backends.Register(&backends.BackendPlugin{
		ID: "kimi",
		ACP: &backends.ACPPlugin{
			InputRemaps: map[string]string{"filePath": "file_path"},
		},
	})

	// Backend without ACP
	backends.Register(&backends.BackendPlugin{ID: "claude"})

	// Lookup existing ACP remaps
	remaps := backends.LookupACPRemaps("kimi")
	if remaps == nil {
		t.Fatal("expected non-nil remaps for kimi")
	}
	if remaps["filePath"] != "file_path" {
		t.Errorf("expected filePath->file_path, got %q", remaps["filePath"])
	}

	// Lookup backend without ACP -> fallback to generic
	remaps = backends.LookupACPRemaps("claude")
	if len(remaps) == 0 {
		t.Error("expected generic_acp fallback remaps for claude, got empty map")
	}
	if remaps["oldString"] != "old_string" {
		t.Errorf("expected oldString->old_string from generic fallback, got %q", remaps["oldString"])
	}

	// Lookup nonexistent -> fallback to generic
	remaps = backends.LookupACPRemaps("nonexistent")
	if len(remaps) == 0 {
		t.Error("expected generic_acp fallback remaps for nonexistent, got empty map")
	}
	if remaps["oldString"] != "old_string" {
		t.Errorf("expected oldString->old_string from generic fallback, got %q", remaps["oldString"])
	}
}

func TestRegistry_LookupACPRemapsEmptyMapFallback(t *testing.T) {
	backends.ResetForTest()

	// Backend with empty ACP remaps should fall back to generic
	backends.Register(&backends.BackendPlugin{
		ID: "empty_acp",
		ACP: &backends.ACPPlugin{
			InputRemaps: map[string]string{},
		},
	})

	remaps := backends.LookupACPRemaps("empty_acp")
	if len(remaps) == 0 {
		t.Error("expected generic_acp fallback remaps for empty_acp, got empty map")
	}
	if remaps["oldString"] != "old_string" {
		t.Errorf("expected oldString->old_string from generic fallback, got %q", remaps["oldString"])
	}
}

func TestRegistry_LookupACPToolCallIDPrefixes(t *testing.T) {
	backends.ResetForTest()

	backends.Register(&backends.BackendPlugin{
		ID: "kimi",
		ACP: &backends.ACPPlugin{
			ToolCallIDPrefixes: map[string]string{"read_file": "Read"},
		},
	})

	// Lookup existing
	prefixes := backends.LookupACPToolCallIDPrefixes("kimi")
	if prefixes == nil {
		t.Fatal("expected non-nil prefixes for kimi")
	}
	if prefixes["read_file"] != "Read" {
		t.Errorf("expected read_file->Read, got %q", prefixes["read_file"])
	}

	// Lookup backend without ACP -> nil
	prefixes = backends.LookupACPToolCallIDPrefixes("nonexistent")
	if prefixes != nil {
		t.Errorf("expected nil for nonexistent backend, got %v", prefixes)
	}
}
