package ai

import (
	"testing"
)

func TestMimoBackend_Name(t *testing.T) {
	if mimoBackend.Name() != "mimo" {
		t.Errorf("expected backend name 'mimo', got %q", mimoBackend.Name())
	}
}

func TestBuildMimoStreamArgs_NewSession(t *testing.T) {
	req := ChatRequest{
		Prompt:  "say hello",
		WorkDir: "/home/user/project",
		Model:   "mimo/mimo-auto",
	}
	args := buildMimoStreamArgs(req)

	expected := []string{"run", "say hello", "--format", "json", "--dangerously-skip-permissions", "--dir", "/home/user/project", "--model", "mimo/mimo-auto"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
		return
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg %d: expected %q, got %q", i, v, args[i])
		}
	}

	// Should NOT contain --session (new session, Resume=false)
	for _, a := range args {
		if a == "--session" {
			t.Error("should not contain --session for new session")
		}
	}
}

func TestBuildMimoStreamArgs_ResumeSession(t *testing.T) {
	req := ChatRequest{
		Prompt:         "continue",
		SessionID:      "ses_abc123",
		Resume:         true,
		WorkDir:        "/home/user/project",
		ThinkingEffort: "high",
	}
	args := buildMimoStreamArgs(req)

	// Should contain --session ses_abc123 because Resume=true
	found := false
	for i, a := range args {
		if a == "--session" && i+1 < len(args) && args[i+1] == "ses_abc123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected --session ses_abc123 in args when Resume=true")
	}

	// Should contain --variant high
	foundVariant := false
	for i, a := range args {
		if a == "--variant" && i+1 < len(args) && args[i+1] == "high" {
			foundVariant = true
			break
		}
	}
	if !foundVariant {
		t.Error("expected --variant high in args when ThinkingEffort=high")
	}
}

func TestBuildMimoStreamArgs_Minimal(t *testing.T) {
	req := ChatRequest{
		Prompt: "hello",
	}
	args := buildMimoStreamArgs(req)

	expected := []string{"run", "hello", "--format", "json", "--dangerously-skip-permissions"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
		return
	}
	for i, v := range expected {
		if args[i] != v {
			t.Errorf("arg %d: expected %q, got %q", i, v, args[i])
		}
	}
}

func TestNewBackend_Mimo(t *testing.T) {
	backend, err := NewBackend("mimo")
	if err != nil {
		t.Fatalf("NewBackend(\"mimo\") returned error: %v", err)
	}
	if backend.Name() != "mimo" {
		t.Errorf("expected backend name 'mimo', got %q", backend.Name())
	}
}
