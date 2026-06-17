package mimo

import (
	"testing"

	"clawbench/internal/ai"
)

func TestMimoPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	if entry == nil {
		t.Fatal("mimo backend factory not registered")
	}
}

func TestMimoPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	if !entry.NeedsAutoResume {
		t.Error("mimo should have needsAutoResume=true")
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
	backend := entry.NewBackend()
	clib, ok := backend.(*ai.CLIBackend)
	if !ok {
		t.Fatal("expected *CLIBackend")
	}

	// Verify parser is an OpenCodeStreamParser
	parser := clib.NewParserFn()
	if _, ok := parser.(*ai.OpenCodeStreamParser); !ok {
		t.Errorf("expected *OpenCodeStreamParser, got %T", parser)
	}

	// Verify Cmd
	if clib.Cmd != "mimo" {
		t.Errorf("expected Cmd 'mimo', got %q", clib.Cmd)
	}

	// Verify PreStartFn is nil
	if clib.PreStartFn != nil {
		t.Error("mimo PreStartFn should be nil")
	}
}

func TestMimoPlugin_FilterLine(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	clib := entry.NewBackend().(*ai.CLIBackend)

	// JSON line should pass
	line, ok := clib.FilterLineFn(`{"type":"text"}`)
	if !ok {
		t.Error(`{"type":"text"} should pass filter`)
	}
	if line != `{"type":"text"}` {
		t.Errorf("expected line unchanged, got %q", line)
	}

	// Plain text should be rejected
	_, ok = clib.FilterLineFn("plain text")
	if ok {
		t.Error("plain text should be rejected")
	}

	// [opencode-mobile] prefix should be rejected
	_, ok = clib.FilterLineFn("[opencode-mobile] stuff")
	if ok {
		t.Error("[opencode-mobile] prefix should be rejected")
	}

	// Empty line should be rejected
	_, ok = clib.FilterLineFn("")
	if ok {
		t.Error("empty line should be rejected")
	}
}

func TestMimoPlugin_BuildArgs(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "mimo-sess-1",
		Resume:    true,
		WorkDir:   "/tmp/project",
		Model:     "mimo-model",
	}
	args := clib.BuildArgsFn(req)

	// Should start with "run <prompt> --format json --dangerously-skip-permissions"
	if len(args) < 5 {
		t.Fatalf("expected at least 5 args, got %d", len(args))
	}
	if args[0] != "run" {
		t.Errorf("expected first arg 'run', got %q", args[0])
	}
	if args[1] != "test prompt" {
		t.Errorf("expected second arg 'test prompt', got %q", args[1])
	}

	// Should have --format json
	hasFormatJSON := false
	for i, a := range args {
		if a == "--format" && i+1 < len(args) && args[i+1] == "json" {
			hasFormatJSON = true
		}
	}
	if !hasFormatJSON {
		t.Error("expected --format json in args")
	}

	// Should have --dangerously-skip-permissions
	hasDangerous := false
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			hasDangerous = true
		}
	}
	if !hasDangerous {
		t.Error("expected --dangerously-skip-permissions in args")
	}

	// Should have --session for resume
	hasSession := false
	for i, a := range args {
		if a == "--session" && i+1 < len(args) && args[i+1] == "mimo-sess-1" {
			hasSession = true
		}
	}
	if !hasSession {
		t.Error("expected --session mimo-sess-1 in args")
	}

	// Should have --dir
	hasDir := false
	for i, a := range args {
		if a == "--dir" && i+1 < len(args) && args[i+1] == "/tmp/project" {
			hasDir = true
		}
	}
	if !hasDir {
		t.Error("expected --dir /tmp/project in args")
	}

	// Should have --model
	hasModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "mimo-model" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("expected --model mimo-model in args")
	}
}

func TestMimoPlugin_BuildArgs_Variant(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:         "test",
		ThinkingEffort: "high",
	}
	args := clib.BuildArgsFn(req)

	hasVariant := false
	for i, a := range args {
		if a == "--variant" && i+1 < len(args) && args[i+1] == "high" {
			hasVariant = true
		}
	}
	if !hasVariant {
		t.Error("expected --variant high in args when ThinkingEffort is set")
	}
}

func TestMimoPlugin_BuildArgs_NoSessionWithoutResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test",
		SessionID: "mimo-sess-1",
		Resume:    false,
	}
	args := clib.BuildArgsFn(req)

	for _, a := range args {
		if a == "--session" {
			t.Error("--session should NOT be in args when Resume=false")
		}
	}
}

func TestMimoPlugin_CmdName(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("mimo")
	clib := entry.NewBackend().(*ai.CLIBackend)
	if clib.Cmd != "mimo" {
		t.Errorf("expected Cmd 'mimo', got %q", clib.Cmd)
	}
}
