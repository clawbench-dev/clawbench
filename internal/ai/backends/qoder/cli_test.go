package qoder

import (
	"os/exec"
	"testing"

	"clawbench/internal/ai"
)

func TestQoderPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	if entry == nil {
		t.Fatal("qoder backend factory not registered")
	}
}

func TestQoderPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	if !entry.NeedsAutoResume {
		t.Error("qoder should have needsAutoResume=true")
	}
}

func TestQoderPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "qoder" {
		t.Errorf("expected backend name 'qoder', got %q", backend.Name())
	}
}

func TestQoderPlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	backend := entry.NewBackend()
	clib, ok := backend.(*ai.CLIBackend)
	if !ok {
		t.Fatal("expected *CLIBackend")
	}

	// Verify parser is a StreamParser
	parser := clib.NewParserFn()
	if _, ok := parser.(*ai.StreamParser); !ok {
		t.Errorf("expected *StreamParser, got %T", parser)
	}

	// Verify BuildArgsFn produces args
	req := ai.ChatRequest{Prompt: "test", SessionID: "test-session", WorkDir: "/tmp/test"}
	args := clib.BuildArgsFn(req)
	if len(args) == 0 {
		t.Error("expected non-empty args")
	}
}

func TestQoderPlugin_PreStart(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{Prompt: "hello qoder"}
	cmd := exec.Command("echo")
	clib.PreStartFn(cmd, req)

	if cmd.Stdin == nil {
		t.Error("expected stdin to be set for qoder preStart (stdin prompt injection)")
	}
}

func TestQoderPlugin_FilterLineNil(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	clib := entry.NewBackend().(*ai.CLIBackend)

	if clib.FilterLineFn != nil {
		t.Error("qoder FilterLineFn should be nil")
	}
}

func TestQoderPlugin_BuildArgs(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "qoder-sess-1",
		WorkDir:   "/tmp/project",
		Model:     "qoder-v1",
	}
	args := clib.BuildArgsFn(req)

	// Should have --print
	hasPrint := false
	for _, a := range args {
		if a == "--print" {
			hasPrint = true
		}
	}
	if !hasPrint {
		t.Error("expected --print in args")
	}

	// Should have --output-format stream-json
	hasStreamJSON := false
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "stream-json" {
			hasStreamJSON = true
		}
	}
	if !hasStreamJSON {
		t.Error("expected --output-format stream-json in args")
	}

	// Should use --cwd (not --add-dir) for WorkDir
	hasCwd := false
	for i, a := range args {
		if a == "--cwd" && i+1 < len(args) && args[i+1] == "/tmp/project" {
			hasCwd = true
		}
	}
	if !hasCwd {
		t.Error("expected --cwd /tmp/project in args (not --add-dir)")
	}

	// Should NOT have --add-dir
	for _, a := range args {
		if a == "--add-dir" {
			t.Error("qoder should NOT use --add-dir, should use --cwd")
		}
	}

	// Should have --dangerously-skip-permissions
	hasSkipPerms := false
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			hasSkipPerms = true
		}
	}
	if !hasSkipPerms {
		t.Error("expected --dangerously-skip-permissions in args")
	}

	// Should have --session-id for new session
	hasSessionID := false
	for i, a := range args {
		if a == "--session-id" && i+1 < len(args) && args[i+1] == "qoder-sess-1" {
			hasSessionID = true
		}
	}
	if !hasSessionID {
		t.Error("expected --session-id qoder-sess-1 in args")
	}

	// Should have --model
	hasModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "qoder-v1" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("expected --model qoder-v1 in args")
	}
}

func TestQoderPlugin_BuildArgs_Resume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "continue",
		SessionID: "qoder-sess-1",
		Resume:    true,
	}
	args := clib.BuildArgsFn(req)

	// Should have --resume for resumed session
	hasResume := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "qoder-sess-1" {
			hasResume = true
		}
	}
	if !hasResume {
		t.Error("expected --resume qoder-sess-1 in args")
	}

	// Should NOT have --session-id when resuming
	for _, a := range args {
		if a == "--session-id" {
			t.Error("should not have --session-id when --resume is used")
		}
	}
}

func TestQoderPlugin_BuildArgs_SystemPrompt(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:       "test",
		SystemPrompt: "you are a helper",
	}
	args := clib.BuildArgsFn(req)

	hasSystemPrompt := false
	for i, a := range args {
		if a == "--system-prompt" && i+1 < len(args) && args[i+1] == "you are a helper" {
			hasSystemPrompt = true
		}
	}
	if !hasSystemPrompt {
		t.Error("expected --system-prompt in args when SystemPrompt is set")
	}
}

func TestQoderPlugin_CmdName(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("qoder")
	clib := entry.NewBackend().(*ai.CLIBackend)
	if clib.Cmd != "qodercli" {
		t.Errorf("expected Cmd 'qodercli', got %q", clib.Cmd)
	}
}
