package backends_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

// stubBackend is an inert AIBackend for the registration tests.
type stubBackend struct{}

func (stubBackend) Name() string { return "stub" }

func (stubBackend) ExecuteStream(context.Context, ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	return nil, nil
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

	p := &backends.BackendPlugin{
		ID: "test-backend",
		Spec: model.BackendSpec{
			ID:        "test-backend",
			Backend:   "test-backend",
			Name:      "Test",
			Specialty: "test backend",
		},
		Implementation: &backends.InProcessPlugin{
			NewBackend: func() ai.AIBackend {
				return &stubBackend{}
			},
		},
	}

	backends.Register(p)

	got := mustLookup(t, "test-backend")
	if got.ID != "test-backend" {
		t.Errorf("expected ID 'test-backend', got %q", got.ID)
	}
	if got.Implementation == nil {
		t.Error("expected an in-process plugin")
	}
	// Register() must forward the in-process factory to internal/ai — that is what
	// makes a single init() enough for a backend.
	if ai.LookupBackendFactoryForTest("test-backend") == nil {
		t.Error("expected Register to forward Implementation.NewBackend to the ai factory table")
	}
}

func TestRegistry_LookupNotFound(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

	got := backends.Lookup("nonexistent")
	if got != nil {
		t.Errorf("expected nil for unregistered backend, got %+v", got)
	}
}

func TestRegistry_RegisterDuplicatePanics(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

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
	t.Cleanup(backends.ResetForTest())

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
	t.Cleanup(backends.ResetForTest())

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
	t.Cleanup(backends.ResetForTest())

	backends.Register(&backends.BackendPlugin{ID: "temp"})
	if backends.Lookup("temp") == nil {
		t.Fatal("expected to find 'temp' after registration")
	}

	restore := backends.ResetForTest()

	if backends.Lookup("temp") != nil {
		t.Error("expected nil after ResetForTest, but found backend")
	}

	// The restore function must put back what was registered before the clear.
	// Tests used to clear without restoring, which left every later test in this
	// binary reading an empty registry when the file order changed.
	backends.Register(&backends.BackendPlugin{ID: "temp-2"})
	restore()
	if backends.Lookup("temp-2") != nil {
		t.Error("restore reinstated the registry from the wrong point: a plugin registered after the clear survived")
	}
	if backends.Lookup("temp") == nil {
		t.Error("restore lost the plugin that was registered before the clear")
	}
	if got := len(backends.All()); got != 1 {
		t.Errorf("restore left %d plugins, want the single pre-clear plugin", got)
	}
}

func TestRegistry_ResetForTestConcurrent(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

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
	t.Cleanup(backends.ResetForTest())

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
	t.Cleanup(backends.ResetForTest())

	// A backend that declares an empty table gets the generic one.
	backends.Register(&backends.BackendPlugin{
		ID: "empty_acp",
		ACP: &backends.ACPPlugin{
			InputRemaps: map[string]string{},
		},
	})

	remaps := backends.LookupACPRemaps("empty_acp")
	if len(remaps) == 0 {
		t.Error("expected generic fallback remaps for empty_acp, got empty map")
	}
	if remaps["oldString"] != "old_string" {
		t.Errorf("expected oldString->old_string from generic fallback, got %q", remaps["oldString"])
	}
}

func TestRegistry_LookupACPToolCallIDPrefixes(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

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

// TestLookupMidTurnInjector verifies the mid-turn policy lookup used by core
// flow: registered backends resolve, everything else is nil (queue as usual).
func TestLookupMidTurnInjector(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

	stub := &stubMidTurnInjector{}
	backends.Register(&backends.BackendPlugin{ID: "with-inject", MidTurn: stub})
	backends.Register(&backends.BackendPlugin{ID: "without-inject"})

	if got := backends.LookupMidTurnInjector("with-inject"); got != stub {
		t.Errorf("expected the registered injector, got %v", got)
	}
	if got := backends.LookupMidTurnInjector("without-inject"); got != nil {
		t.Errorf("expected nil for a backend without a policy, got %v", got)
	}
	if got := backends.LookupMidTurnInjector("nonexistent"); got != nil {
		t.Errorf("expected nil for an unknown backend, got %v", got)
	}
}

// stubMidTurnInjector is a no-op policy used to exercise the registry.
type stubMidTurnInjector struct{}

func (s *stubMidTurnInjector) Inject(context.Context, ai.RawRPCTransport, ai.MidTurnInjectRequest) (ai.MidTurnInjectResult, error) {
	return ai.MidTurnInjectResult{}, nil
}

// TestLookupACPToolCallWrapperPrefixes covers the wrapper-frame filter lookup.
// It is wired into ai.LookupACPToolCallWrapperPrefixesFn during init(), and a
// backend that emits no wrappers must resolve to nil so the filter stays off.
func TestLookupACPToolCallWrapperPrefixes(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

	backends.Register(&backends.BackendPlugin{
		ID: "with-wrappers",
		ACP: &backends.ACPPlugin{
			ToolCallWrapperTitlePrefixes: []string{"tool_call: id: "},
		},
	})
	backends.Register(&backends.BackendPlugin{ID: "no-acp"})
	backends.Register(&backends.BackendPlugin{
		ID:  "acp-no-wrappers",
		ACP: &backends.ACPPlugin{},
	})

	got := backends.LookupACPToolCallWrapperPrefixes("with-wrappers")
	if len(got) != 1 || got[0] != "tool_call: id: " {
		t.Errorf("expected the registered prefixes, got %v", got)
	}
	if got := backends.LookupACPToolCallWrapperPrefixes("acp-no-wrappers"); got != nil {
		t.Errorf("expected nil when no prefixes are registered, got %v", got)
	}
	if got := backends.LookupACPToolCallWrapperPrefixes("no-acp"); got != nil {
		t.Errorf("expected nil for a backend without an ACP plugin, got %v", got)
	}
	if got := backends.LookupACPToolCallWrapperPrefixes("nonexistent"); got != nil {
		t.Errorf("expected nil for an unknown backend, got %v", got)
	}
}

// TestLookupACPModesMirrorThoughtLevel covers the modes-suppression lookup.
// A false result is the default and must also be returned for unknown backends
// and for backends whose ACP plugin is nil.
func TestLookupACPModesMirrorThoughtLevel(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

	backends.Register(&backends.BackendPlugin{
		ID:  "mirrored",
		ACP: &backends.ACPPlugin{ModesMirrorThoughtLevel: true},
	})
	backends.Register(&backends.BackendPlugin{ID: "no-acp"})
	backends.Register(&backends.BackendPlugin{
		ID:  "acp-normal",
		ACP: &backends.ACPPlugin{},
	})

	if !backends.LookupACPModesMirrorThoughtLevel("mirrored") {
		t.Error("expected true for a backend that mirrors its modes")
	}
	if backends.LookupACPModesMirrorThoughtLevel("acp-normal") {
		t.Error("expected false when the flag is unset")
	}
	if backends.LookupACPModesMirrorThoughtLevel("no-acp") {
		t.Error("expected false for a backend without an ACP plugin")
	}
	if backends.LookupACPModesMirrorThoughtLevel("nonexistent") {
		t.Error("expected false for an unknown backend")
	}
}

func TestLookupACPSpawnHooks(t *testing.T) {
	t.Cleanup(backends.ResetForTest())

	hooks := &ai.ACPSpawnHooks{Args: func(ai.ACPSpawnContext) []string { return nil }}
	backends.Register(&backends.BackendPlugin{
		ID: "with_spawn_hooks",
		ACP: &backends.ACPPlugin{
			SpawnHooks: hooks,
		},
	})
	backends.Register(&backends.BackendPlugin{
		ID:   "no_acp_plugin",
		Spec: model.BackendSpec{ID: "no_acp_plugin", Backend: "no_acp_plugin"},
	})
	backends.Register(&backends.BackendPlugin{
		ID:  "acp_without_hooks",
		ACP: &backends.ACPPlugin{},
	})

	if got := backends.LookupACPSpawnHooks("with_spawn_hooks"); got != hooks {
		t.Errorf("LookupACPSpawnHooks returned %p, want the registered hooks %p", got, hooks)
	}
	for _, id := range []string{"no_acp_plugin", "acp_without_hooks", "unknown"} {
		if got := backends.LookupACPSpawnHooks(id); got != nil {
			t.Errorf("LookupACPSpawnHooks(%q) = %v, want nil", id, got)
		}
	}
}
