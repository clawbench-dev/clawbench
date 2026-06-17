package cline

import (
	"os/exec"
	"strings"
	"testing"

	"clawbench/internal/ai"
)

func TestClinePlugin_Registered(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("cline")
	if entry == nil {
		t.Fatal("cline backend factory not registered")
	}
}

func TestClinePlugin_NeedsAutoResume(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("cline")
	if !entry.NeedsAutoResume {
		t.Error("cline should have needsAutoResume=true")
	}
}

func TestClinePlugin_NewBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("cline")
	backend := entry.NewBackend()
	if backend == nil {
		t.Fatal("NewBackend returned nil")
	}
	if backend.Name() != "cline" {
		t.Errorf("expected backend name 'cline', got %q", backend.Name())
	}
}

func TestClinePlugin_NewBackendIsCLIBackend(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("cline")
	clib := entry.NewBackend().(*ai.CLIBackend)

	if clib.Cmd != "cline" {
		t.Errorf("expected Cmd 'cline', got %q", clib.Cmd)
	}
	if clib.FilterLineFn != nil {
		t.Error("FilterLineFn should be nil for cline")
	}
	if clib.NewParserFn == nil {
		t.Error("NewParserFn should not be nil")
	}
	parser := clib.NewParserFn()
	if _, ok := parser.(*ai.StreamParser); !ok {
		t.Error("expected NewParserFn to return *StreamParser")
	}
}

func TestClinePlugin_PreStart(t *testing.T) {
	entry := ai.LookupBackendFactoryForTest("cline")
	clib := entry.NewBackend().(*ai.CLIBackend)

	req := ai.ChatRequest{Prompt: "hello from cline"}
	cmd := exec.Command("echo")
	clib.PreStartFn(cmd, req)

	if cmd.Stdin == nil {
		t.Fatal("expected stdin to be set")
	}
	stdinReader, ok := cmd.Stdin.(*strings.Reader)
	if !ok {
		t.Fatal("stdin should be a strings.Reader")
	}
	buf := make([]byte, 100)
	n, _ := stdinReader.Read(buf)
	if string(buf[:n]) != "hello from cline" {
		t.Errorf("expected stdin 'hello from cline', got %q", string(buf[:n]))
	}
}

func TestBuildClineStreamArgs_BasicPrompt(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{Prompt: "hello world"})

	if !containsPair(args, "--json", "") {
		t.Error("expected --json in args")
	}
	if !containsPair(args, "--auto-approve", "true") {
		t.Error("expected --auto-approve true in args")
	}
}

func TestBuildClineStreamArgs_WithResume(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{
		Prompt:    "follow-up",
		SessionID: "sess-123",
		Resume:    true,
	})

	if !containsPair(args, "--id", "sess-123") {
		t.Error("expected --id sess-123 in args")
	}
}

func TestBuildClineStreamArgs_SessionIDWithoutResume(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{
		Prompt:    "hello",
		SessionID: "sess-123",
		Resume:    false,
	})

	for _, a := range args {
		if a == "--id" {
			t.Error("should not have --id when Resume=false")
		}
	}
}

func TestBuildClineStreamArgs_EmptySessionIDWithResume(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{
		Prompt:    "hello",
		SessionID: "",
		Resume:    true,
	})

	for _, a := range args {
		if a == "--id" {
			t.Error("should not have --id when SessionID is empty even if Resume=true")
		}
	}
}

func TestBuildClineStreamArgs_WithWorkDir(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{
		Prompt:  "hello",
		WorkDir: "/tmp/project",
	})

	if !containsPair(args, "--cwd", "/tmp/project") {
		t.Error("expected --cwd /tmp/project in args")
	}
}

func TestBuildClineStreamArgs_WithoutWorkDir(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--cwd" {
			t.Error("should not have --cwd when WorkDir is empty")
		}
	}
}

func TestBuildClineStreamArgs_WithModel(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{
		Prompt: "hello",
		Model:  "claude-sonnet-4-6",
	})

	if !containsPair(args, "--model", "claude-sonnet-4-6") {
		t.Error("expected --model claude-sonnet-4-6 in args")
	}
}

func TestBuildClineStreamArgs_WithoutModel(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--model" {
			t.Error("should not have --model when Model is empty")
		}
	}
}

func TestBuildClineStreamArgs_WithThinkingEffort(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{
		Prompt:         "hello",
		ThinkingEffort: "high",
	})

	if !containsPair(args, "--thinking", "high") {
		t.Error("expected --thinking high in args")
	}
}

func TestBuildClineStreamArgs_WithoutThinkingEffort(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--thinking" {
			t.Error("should not have --thinking when ThinkingEffort is empty")
		}
	}
}

func TestBuildClineStreamArgs_AllOptions(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{
		Prompt:         "fix the bug",
		SessionID:      "sess-456",
		Resume:         true,
		WorkDir:        "/home/user/project",
		Model:          "claude-sonnet-4-6",
		ThinkingEffort: "medium",
	})

	argsStr := strings.Join(args, " ")
	if !strings.Contains(argsStr, "--id sess-456") {
		t.Error("expected --id sess-456")
	}
	if !strings.Contains(argsStr, "--cwd /home/user/project") {
		t.Error("expected --cwd /home/user/project")
	}
	if !strings.Contains(argsStr, "--model claude-sonnet-4-6") {
		t.Error("expected --model claude-sonnet-4-6")
	}
	if !strings.Contains(argsStr, "--thinking medium") {
		t.Error("expected --thinking medium")
	}
}

func TestBuildClineStreamArgs_Minimal(t *testing.T) {
	args := buildClineStreamArgs(ai.ChatRequest{Prompt: "hello"})

	expected := []string{"--json", "--auto-approve", "true"}
	if len(args) != len(expected) {
		t.Fatalf("minimal args: expected %d, got %d (%v)", len(expected), len(args), args)
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg %d: expected %q, got %q", i, v, args[i])
		}
	}
}

// containsPair checks if args contains a --flag value pair (or just --flag if value is "")
func containsPair(args []string, flag, value string) bool {
	for i, a := range args {
		if a == flag {
			if value == "" {
				return true
			}
			if i+1 < len(args) && args[i+1] == value {
				return true
			}
		}
	}
	return false
}
