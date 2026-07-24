package grok

import (
	"testing"

	"clawbench/internal/ai"
)

func TestGrokPlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("grok")
	if entry == nil {
		t.Fatal("expected grok backend to be registered")
	}
	if entry.NewBackendFn == nil {
		t.Fatal("expected NewBackendFn to be non-nil")
	}
	if !entry.NeedsAutoResume {
		t.Error("expected NeedsAutoResume true for grok")
	}
}

func TestGrokPlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("grok")
	backend := entry.NewBackendFn()
	clib, ok := backend.(*ai.CLIBackend)
	if !ok {
		// May be wrapped later by factory; factory entry returns raw CLIBackend.
		t.Fatalf("expected *ai.CLIBackend, got %T", backend)
	}
	if clib.BackendName != "grok" {
		t.Errorf("BackendName = %q, want grok", clib.BackendName)
	}
	if clib.Cmd != "grok" {
		t.Errorf("Cmd = %q, want grok", clib.Cmd)
	}
	if clib.BuildArgsFn == nil || clib.NewParserFn == nil || clib.FilterLineFn == nil {
		t.Error("expected BuildArgsFn/NewParserFn/FilterLineFn to be set")
	}
	parser := clib.NewParserFn()
	if _, ok := parser.(*ai.GrokStreamParser); !ok {
		t.Errorf("expected *ai.GrokStreamParser, got %T", parser)
	}
}

func TestGrokPlugin_FilterLine(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("grok")
	clib := entry.NewBackendFn().(*ai.CLIBackend)

	line, ok := clib.FilterLineFn(`{"type":"text","data":"hi"}`)
	if !ok || line != `{"type":"text","data":"hi"}` {
		t.Errorf("JSON line should pass filter, got ok=%v line=%q", ok, line)
	}
	if _, ok := clib.FilterLineFn("plain text"); ok {
		t.Error("plain text should be filtered")
	}
	if _, ok := clib.FilterLineFn(""); ok {
		t.Error("empty line should be filtered")
	}
}

func TestGrokPlugin_BuildArgs_Basic(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("grok")
	clib := entry.NewBackendFn().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:         "hello grok",
		WorkDir:        "/tmp/project",
		Model:          "grok-4.5",
		ThinkingEffort: "high",
		SystemPrompt:   "be concise",
	}
	args := clib.BuildArgsFn(req)

	assertArgPair(t, args, "-p", "hello grok")
	assertArgPair(t, args, "--output-format", "streaming-json")
	assertArgPresent(t, args, "--yolo")
	assertArgPair(t, args, "--cwd", "/tmp/project")
	assertArgPair(t, args, "--model", "grok-4.5")
	assertArgPair(t, args, "--reasoning-effort", "high")
	assertArgPair(t, args, "--rules", "be concise")
	// No resume without Resume=true
	assertArgAbsent(t, args, "--resume")
}

func TestGrokPlugin_BuildArgs_Resume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("grok")
	clib := entry.NewBackendFn().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "continue",
		SessionID: "sess-xyz",
		Resume:    true,
	}
	args := clib.BuildArgsFn(req)
	assertArgPair(t, args, "--resume", "sess-xyz")
}

func TestGrokPlugin_BuildArgs_NoResumeWithoutFlag(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("grok")
	clib := entry.NewBackendFn().(*ai.CLIBackend)

	req := ai.ChatRequest{
		Prompt:    "new",
		SessionID: "sess-xyz",
		Resume:    false,
	}
	args := clib.BuildArgsFn(req)
	assertArgAbsent(t, args, "--resume")
}

func TestGrokPlugin_SpecViaFactory(t *testing.T) {
	// Smoke: buildArgs with empty optional fields should still work.
	args := buildGrokStreamArgs(ai.ChatRequest{Prompt: "x"})
	assertArgPair(t, args, "-p", "x")
	assertArgPair(t, args, "--output-format", "streaming-json")
	assertArgPresent(t, args, "--yolo")
}

func assertArgPair(t *testing.T, args []string, key, val string) {
	t.Helper()
	for i, a := range args {
		if a == key && i+1 < len(args) && args[i+1] == val {
			return
		}
	}
	t.Errorf("expected arg pair %s %s in %v", key, val, args)
}

func assertArgPresent(t *testing.T, args []string, key string) {
	t.Helper()
	for _, a := range args {
		if a == key {
			return
		}
	}
	t.Errorf("expected arg %s in %v", key, args)
}

func assertArgAbsent(t *testing.T, args []string, key string) {
	t.Helper()
	for _, a := range args {
		if a == key {
			t.Errorf("did not expect arg %s in %v", key, args)
			return
		}
	}
}
