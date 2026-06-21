package deepseek

import (
	"testing"

	"clawbench/internal/ai"
)

func TestDeepSeekPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	if entry == nil {
		t.Fatal("deepseek backend factory not registered")
	}
}

func TestDeepSeekPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	if !entry.NeedsAutoResume {
		t.Error("deepseek should have needsAutoResume=true")
	}
}

func TestDeepSeekPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "deepseek" {
		t.Errorf("expected backend name 'deepseek', got %q", backend.Name())
	}
}

func TestDeepSeekPlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	backend := entry.NewBackend()
	clib, ok := backend.(*ai.CLIBackend)
	if !ok {
		t.Fatal("expected *CLIBackend")
	}

	// Verify parser is a DeepSeekStreamParser
	parser := clib.NewParserFn()
	if _, ok := parser.(*ai.DeepSeekStreamParser); !ok {
		t.Errorf("expected *DeepSeekStreamParser, got %T", parser)
	}

	// Verify Cmd — primary command is "codewhale", legacy "deepseek" handled via req.Command
	if clib.Cmd != "codewhale" {
		t.Errorf("expected Cmd 'codewhale', got %q", clib.Cmd)
	}

	// Verify PreStartFn is nil
	if clib.PreStartFn != nil {
		t.Error("deepseek PreStartFn should be nil")
	}

	// Verify FilterLineFn is nil (default: skip empty lines only)
	if clib.FilterLineFn != nil {
		t.Error("deepseek FilterLineFn should be nil")
	}
}

func TestDeepSeekPlugin_BuildArgs(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "deepseek-sess-1",
		Resume:    true,
	}
	args := clib.BuildArgsFn(req)

	// Should start with "exec --auto --output-format stream-json"
	if len(args) < 4 {
		t.Fatalf("expected at least 4 args, got %d", len(args))
	}
	if args[0] != "exec" {
		t.Errorf("expected first arg 'exec', got %q", args[0])
	}
	if args[1] != "--auto" {
		t.Errorf("expected second arg '--auto', got %q", args[1])
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

	// Should have --resume for resume
	hasResume := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "deepseek-sess-1" {
			hasResume = true
		}
	}
	if !hasResume {
		t.Error("expected --resume deepseek-sess-1 in args")
	}

	// Prompt should be last positional argument
	if args[len(args)-1] != "test prompt" {
		t.Errorf("expected last arg 'test prompt', got %q", args[len(args)-1])
	}
}

func TestDeepSeekPlugin_BuildArgs_Model(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	clib := entry.NewBackend().(*ai.CLIBackend)

	// Model with provider prefix should be stripped
	req := ai.ChatRequest{
		Prompt: "test",
		Model:  "deepseek/deepseek-v4-pro",
	}
	args := clib.BuildArgsFn(req)

	hasModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "deepseek-v4-pro" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("expected --model deepseek-v4-pro (provider prefix stripped) in args")
	}

	// Model without provider prefix should pass through
	req = ai.ChatRequest{
		Prompt: "test",
		Model:  "deepseek-v4-flash",
	}
	args = clib.BuildArgsFn(req)

	hasModel = false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "deepseek-v4-flash" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("expected --model deepseek-v4-flash in args")
	}
}

func TestDeepSeekPlugin_BuildArgs_SystemPrompt(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
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
		t.Error("expected --system-prompt 'you are a helper' in args")
	}
}

func TestDeepSeekPlugin_BuildArgs_ContinueFallback(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	clib := entry.NewBackend().(*ai.CLIBackend)

	// Resume=true but SessionID empty → should use --continue fallback
	req := ai.ChatRequest{
		Prompt: "test",
		Resume: true,
	}
	args := clib.BuildArgsFn(req)

	hasContinue := false
	for _, a := range args {
		if a == "--continue" {
			hasContinue = true
		}
	}
	if !hasContinue {
		t.Error("expected --continue fallback in args when Resume=true but SessionID is empty")
	}
}

func TestDeepSeekPlugin_CmdName(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("deepseek")
	clib := entry.NewBackend().(*ai.CLIBackend)
	if clib.Cmd != "codewhale" {
		t.Errorf("expected Cmd 'codewhale', got %q", clib.Cmd)
	}
}
