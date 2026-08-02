package mimo

import (
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends/opencode"
)

func TestMimoPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	if entry == nil {
		t.Fatal("mimo backend factory not registered")
	}
}

func TestMimoPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "mimo" {
		t.Errorf("expected backend name 'mimo', got %q", backend.Name())
	}
}

func TestMimoPlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	clib, ok := entry.NewBackend().(*ai.CLIBackend)
	if !ok {
		t.Fatal("expected *CLIBackend")
	}

	// Verify MiMo-specific fields differ from OpenCode
	if clib.Cmd != "mimo" {
		t.Errorf("expected Cmd 'mimo', got %q", clib.Cmd)
	}
	if clib.BackendName != "mimo" {
		t.Errorf("expected BackendName 'mimo', got %q", clib.BackendName)
	}

	// Verify shared infrastructure is reused from OpenCode
	if clib.BuildArgsFn == nil {
		t.Error("BuildArgsFn should not be nil")
	}
	if clib.FilterLineFn == nil {
		t.Error("FilterLineFn should not be nil")
	}

	// Verify parser is an OpenCodeStreamParser with OpenCode mappings
	parser := clib.NewParserFn()
	ocsp, ok := parser.(*ai.OpenCodeStreamParser)
	if !ok {
		t.Fatalf("expected *OpenCodeStreamParser, got %T", parser)
	}
	// Verify parser reuses OpenCode's shared maps (same pointer)
	if &ocsp.ToolNameMap != &opencode.OpenCodeToolNameMap {
		// Maps can't be compared directly, but we can verify key overlap
		for k, v := range opencode.OpenCodeToolNameMap {
			if ocsp.ToolNameMap[k] != v {
				t.Errorf("ToolNameMap[%q] = %q, want %q", k, ocsp.ToolNameMap[k], v)
			}
		}
	}
	if &ocsp.InputRemaps != &opencode.OpenCodeInputRemaps {
		for k, v := range opencode.OpenCodeInputRemaps {
			if ocsp.InputRemaps[k] != v {
				t.Errorf("InputRemaps[%q] = %q, want %q", k, ocsp.InputRemaps[k], v)
			}
		}
	}

	// Verify PreStartFn is nil
	if clib.PreStartFn != nil {
		t.Error("mimo PreStartFn should be nil")
	}
}

func TestMimoPlugin_CmdName(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	clib := entry.NewBackend().(*ai.CLIBackend)
	if clib.Cmd != "mimo" {
		t.Errorf("expected Cmd 'mimo', got %q", clib.Cmd)
	}
}
