package copilot

import (
	"os/exec"
	"testing"

	"clawbench/internal/ai"
)

func TestCopilotPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	if entry == nil {
		t.Fatal("copilot backend factory not registered")
	}
}

func TestCopilotPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	if !entry.NeedsAutoResume {
		t.Error("copilot should have needsAutoResume=true")
	}
}

func TestCopilotPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "copilot" {
		t.Errorf("expected backend name 'copilot', got %q", backend.Name())
	}
}

func TestCopilotPlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
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

	// Verify BuildArgsFn produces --output-format json (not stream-json)
	req := ai.ChatRequest{Prompt: "test", SessionID: "test-session", WorkDir: "/tmp/test"}
	args := clib.BuildArgsFn(req)

	// Should have --output-format json
	hasOutputFormatJSON := false
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "json" {
			hasOutputFormatJSON = true
		}
	}
	if !hasOutputFormatJSON {
		t.Error("expected --output-format json in copilot args (not stream-json)")
	}

	// Should have --allow-all
	hasAllowAll := false
	for _, a := range args {
		if a == "--allow-all" {
			hasAllowAll = true
		}
	}
	if !hasAllowAll {
		t.Error("expected --allow-all in copilot args")
	}

	// Should NOT have stream-json
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "stream-json" {
			t.Error("copilot should NOT use --output-format stream-json")
		}
	}
}

func TestCopilotPlugin_PreStart(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{Prompt: "hello copilot"}
	cmd := exec.Command("echo")
	clib.PreStartFn(cmd, req)

	if cmd.Stdin == nil {
		t.Error("expected stdin to be set for copilot preStart (stdin prompt injection)")
	}
}

func TestCopilotPlugin_FilterLine(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	clib := entry.NewBackend().(*ai.CLIBackend)

	// JSON line should pass
	line, ok := clib.FilterLineFn(`{"type":"assistant"}`)
	if !ok {
		t.Error("JSON line should pass filter")
	}
	if line != `{"type":"assistant"}` {
		t.Errorf("expected line unchanged, got %q", line)
	}

	// Plain text should be filtered
	_, ok = clib.FilterLineFn("plain text")
	if ok {
		t.Error("plain text should be filtered")
	}

	// Empty line should be filtered
	_, ok = clib.FilterLineFn("")
	if ok {
		t.Error("empty line should be filtered")
	}

	// Non-JSON prefix should be filtered
	_, ok = clib.FilterLineFn("info: processing request")
	if ok {
		t.Error("non-JSON line should be filtered")
	}
}

func TestCopilotPlugin_BuildArgs(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "copilot-sess-1",
		WorkDir:   "/tmp/project",
		Model:     "gpt-4",
	}
	args := clib.BuildArgsFn(req)

	// Should have --output-format json
	hasOutputFormatJSON := false
	for i, a := range args {
		if a == "--output-format" && i+1 < len(args) && args[i+1] == "json" {
			hasOutputFormatJSON = true
		}
	}
	if !hasOutputFormatJSON {
		t.Error("expected --output-format json in args")
	}

	// Should have --allow-all
	hasAllowAll := false
	for _, a := range args {
		if a == "--allow-all" {
			hasAllowAll = true
		}
	}
	if !hasAllowAll {
		t.Error("expected --allow-all in args")
	}

	// Should have -p for prompt
	hasP := false
	for i, a := range args {
		if a == "-p" && i+1 < len(args) && args[i+1] == "test prompt" {
			hasP = true
		}
	}
	if !hasP {
		t.Error("expected -p 'test prompt' in args")
	}

	// Should have -C for workdir
	hasC := false
	for i, a := range args {
		if a == "-C" && i+1 < len(args) && args[i+1] == "/tmp/project" {
			hasC = true
		}
	}
	if !hasC {
		t.Error("expected -C /tmp/project in args")
	}

	// Should have --model
	hasModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "gpt-4" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("expected --model gpt-4 in args")
	}
}

func TestCopilotPlugin_BuildArgs_Resume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "continue",
		SessionID: "copilot-sess-1",
		Resume:    true,
	}
	args := clib.BuildArgsFn(req)

	// Should have --resume for resumed session
	hasResume := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "copilot-sess-1" {
			hasResume = true
		}
	}
	if !hasResume {
		t.Error("expected --resume copilot-sess-1 in args")
	}
}

func TestCopilotPlugin_BuildArgs_ThinkingEffort(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:         "test",
		ThinkingEffort: "high",
	}
	args := clib.BuildArgsFn(req)

	hasEffort := false
	for i, a := range args {
		if a == "--effort" && i+1 < len(args) && args[i+1] == "high" {
			hasEffort = true
		}
	}
	if !hasEffort {
		t.Error("expected --effort high in args when ThinkingEffort is set")
	}
}

func TestCopilotPlugin_CmdName(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("copilot")
	clib := entry.NewBackend().(*ai.CLIBackend)
	if clib.Cmd != "copilot" {
		t.Errorf("expected Cmd 'copilot', got %q", clib.Cmd)
	}
}
