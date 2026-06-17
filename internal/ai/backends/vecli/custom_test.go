package vecli

import (
	"testing"

	"clawbench/internal/ai"
)

func TestVeCLIPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("vecli")
	if entry == nil {
		t.Fatal("vecli backend factory not registered")
	}
}

func TestVeCLIPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("vecli")
	if entry.NeedsAutoResume {
		t.Error("vecli should have needsAutoResume=false (handles session lifecycle internally)")
	}
}

func TestVeCLIPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("vecli")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "vecli" {
		t.Errorf("expected backend name 'vecli', got %q", backend.Name())
	}
}

func TestVeCLIPlugin_NewBackendIsVeCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("vecli")
	backend := entry.NewBackend()
	// VeCLIBackend wraps CLIBackend — verify it's a *VeCLIBackend
	_, ok := backend.(*ai.VeCLIBackend)
	if !ok {
		t.Error("expected *VeCLIBackend type")
	}
}
