package claude

import (
	"os/exec"
	"testing"

	"clawbench/internal/ai"
)

func TestClaudePlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	if entry == nil {
		t.Fatal("claude backend factory not registered")
	}
}

func TestClaudePlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	if !entry.NeedsAutoResume {
		t.Error("claude should have needsAutoResume=true")
	}
}

func TestClaudePlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "claude" {
		t.Errorf("expected backend name 'claude', got %q", backend.Name())
	}
}

func TestClaudePlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
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

	// Verify BuildArgsFn produces --verbose (unique to claude)
	req := ai.ChatRequest{Prompt: "test", SessionID: "test-session", WorkDir: "/tmp/test"}
	args := clib.BuildArgsFn(req)

	hasVerbose := false
	for _, a := range args {
		if a == "--verbose" {
			hasVerbose = true
		}
	}
	if !hasVerbose {
		t.Error("expected --verbose in claude args (unlike codebuddy)")
	}
}

func TestClaudePlugin_PreStart(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{Prompt: "hello claude"}
	cmd := exec.Command("echo")
	clib.PreStartFn(cmd, req)

	if cmd.Stdin == nil {
		t.Error("expected stdin to be set for claude preStart (stdin prompt injection)")
	}
}

func TestClaudePlugin_FilterLineNil(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	clib := entry.NewBackend().(*ai.CLIBackend)

	if clib.FilterLineFn != nil {
		t.Error("claude FilterLineFn should be nil")
	}
}

func TestClaudePlugin_BuildArgs(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "claude-sess-1",
		WorkDir:   "/tmp/project",
	}
	args := clib.BuildArgsFn(req)

	// Should have --verbose
	hasVerbose := false
	for _, a := range args {
		if a == "--verbose" {
			hasVerbose = true
		}
	}
	if !hasVerbose {
		t.Error("expected --verbose in args")
	}

	// Should have --print (from BuildBaseStreamArgs)
	hasPrint := false
	for _, a := range args {
		if a == "--print" {
			hasPrint = true
		}
	}
	if !hasPrint {
		t.Error("expected --print in args")
	}

	// Should have --output-format stream-json (from BuildBaseStreamArgs)
	hasStreamJSON := false
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "stream-json" {
			hasStreamJSON = true
		}
	}
	if !hasStreamJSON {
		t.Error("expected --output-format stream-json in args")
	}

	// Should have --session-id for new session
	hasSessionID := false
	for i, a := range args {
		if a == "--session-id" && i+1 < len(args) && args[i+1] == "claude-sess-1" {
			hasSessionID = true
		}
	}
	if !hasSessionID {
		t.Error("expected --session-id claude-sess-1 in args")
	}
}

func TestClaudePlugin_BuildArgs_Resume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "continue",
		SessionID: "claude-sess-1",
		Resume:    true,
	}
	args := clib.BuildArgsFn(req)

	// Should have --resume for resumed session
	hasResume := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "claude-sess-1" {
			hasResume = true
		}
	}
	if !hasResume {
		t.Error("expected --resume claude-sess-1 in args")
	}
}

func TestClaudePlugin_CmdName(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("claude")
	clib := entry.NewBackend().(*ai.CLIBackend)
	if clib.Cmd != "claude" {
		t.Errorf("expected Cmd 'claude', got %q", clib.Cmd)
	}
}
