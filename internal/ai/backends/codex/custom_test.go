package codex

import (
	"testing"

	"clawbench/internal/ai"
)

func TestCodexPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codex")
	if entry == nil {
		t.Fatal("codex backend factory not registered")
	}
}

func TestCodexPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codex")
	if entry.NeedsAutoResume {
		t.Error("codex should have needsAutoResume=false")
	}
}

func TestCodexPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codex")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
}

func TestCodexPlugin_NewBackendIsCodexBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codex")
	backend := entry.NewBackend()
	codexB, ok := backend.(*ai.CodexBackend)
	if !ok {
		t.Fatalf("expected *ai.CodexBackend, got %T", backend)
	}
	_ = codexB
}

func TestCodexPlugin_Name(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("codex")
	backend := entry.NewBackend()
	if backend.Name() != "codex" {
		t.Errorf("expected backend name 'codex', got %q", backend.Name())
	}
}
