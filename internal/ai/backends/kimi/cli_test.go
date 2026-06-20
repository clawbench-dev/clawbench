package kimi

import (
	"strings"
	"testing"

	"clawbench/internal/ai"
)

func TestKimiPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	if entry == nil {
		t.Fatal("kimi backend factory not registered")
	}
}

func TestKimiPlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	if !entry.NeedsAutoResume {
		t.Error("kimi should have needsAutoResume=true")
	}
}

func TestKimiPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "kimi" {
		t.Errorf("expected backend name 'kimi', got %q", backend.Name())
	}
}

func TestKimiPlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	clib, ok := entry.NewBackend().(*ai.CLIBackend)
	if !ok {
		t.Fatal("expected *CLIBackend")
	}

	// Verify parser is a StreamJSONParser
	parser := clib.NewParserFn()
	if _, ok := parser.(*ai.StreamJSONParser); !ok {
		t.Errorf("expected *StreamJSONParser, got %T", parser)
	}

	// Verify fields
	if clib.Cmd != "kimi" {
		t.Errorf("expected Cmd 'kimi', got %q", clib.Cmd)
	}
	if clib.PreStartFn != nil {
		t.Error("kimi does not use preStart")
	}
}

func TestKimiPlugin_FilterLine(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	clib := entry.NewBackend().(*ai.CLIBackend)

	// Empty line should be filtered
	line, ok := clib.FilterLineFn("")
	if ok {
		t.Error("empty line should be filtered")
	}
	if line != "" {
		t.Errorf("expected empty string, got %q", line)
	}

	// Non-JSON line should be filtered
	_, ok = clib.FilterLineFn("not json")
	if ok {
		t.Error("non-JSON line should be filtered")
	}

	// Non-JSON info line should be filtered
	_, ok = clib.FilterLineFn("info: processing request")
	if ok {
		t.Error("non-JSON info line should be filtered")
	}

	// JSON line should pass
	line, ok = clib.FilterLineFn(`{"type":"result"}`)
	if !ok {
		t.Error("JSON line should pass filter")
	}
	if line != `{"type":"result"}` {
		t.Errorf("expected line unchanged, got %q", line)
	}
}

func TestKimiPlugin_BuildArgs(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "test prompt",
		SessionID: "kimi-sess-1",
		Resume:    true,
		WorkDir:   "/tmp/project",
		Model:     "moonshot-v1",
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

	// Should have --yes
	hasYes := false
	for _, a := range args {
		if a == "--yes" {
			hasYes = true
		}
	}
	if !hasYes {
		t.Error("expected --yes in args")
	}

	// Should have --session for resume
	hasSession := false
	for i, a := range args {
		if a == "--session" && i+1 < len(args) && args[i+1] == "kimi-sess-1" {
			hasSession = true
		}
	}
	if !hasSession {
		t.Error("expected --session kimi-sess-1 in args")
	}

	// Should have --work-dir
	hasWorkDir := false
	for i, a := range args {
		if a == "--work-dir" && i+1 < len(args) && args[i+1] == "/tmp/project" {
			hasWorkDir = true
		}
	}
	if !hasWorkDir {
		t.Error("expected --work-dir /tmp/project in args")
	}

	// Should have --model
	hasModel := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "moonshot-v1" {
			hasModel = true
		}
	}
	if !hasModel {
		t.Error("expected --model moonshot-v1 in args")
	}
}

func TestKimiPlugin_BuildArgs_Thinking(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	clib := entry.NewBackend().(*ai.CLIBackend)

	// Thinking on
	req := ai.ChatRequest{Prompt: "test", ThinkingEffort: "high"}
	args := clib.BuildArgsFn(req)
	hasThinking := false
	for _, a := range args {
		if a == "--thinking" {
			hasThinking = true
		}
	}
	if !hasThinking {
		t.Error("expected --thinking in args when ThinkingEffort is set")
	}

	// Thinking off
	req = ai.ChatRequest{Prompt: "test", ThinkingEffort: "off"}
	args = clib.BuildArgsFn(req)
	hasNoThinking := false
	for _, a := range args {
		if a == "--no-thinking" {
			hasNoThinking = true
		}
	}
	if !hasNoThinking {
		t.Error("expected --no-thinking in args when ThinkingEffort is 'off'")
	}
}

func TestKimiPlugin_InjectSystemPrompt(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("kimi")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:       "user question",
		SystemPrompt: "you are a helper",
	}
	args := clib.BuildArgsFn(req)

	// Find the prompt argument (after --prompt)
	for i, a := range args {
		if a == "--prompt" && i+1 < len(args) {
			prompt := args[i+1]
			if !strings.Contains(prompt, "you are a helper") {
				t.Error("system prompt should be injected into the prompt")
			}
			if !strings.Contains(prompt, "user question") {
				t.Error("user prompt should be preserved")
			}
			return
		}
	}
	t.Error("--prompt flag not found in args")
}
