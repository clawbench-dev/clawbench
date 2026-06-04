package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildCopilotStreamArgs_BasicPrompt(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{Prompt: "hello world"})

	assert.Contains(t, args, "--output-format")
	assert.Contains(t, args, "json")
	assert.Contains(t, args, "--allow-all")
	assert.Contains(t, args, "-p")
	assert.Contains(t, args, "hello world")
}

func TestBuildCopilotStreamArgs_WithResume(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{
		Prompt:    "follow-up",
		SessionID: "sess-123",
		Resume:    true,
	})

	found := false
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == "sess-123" {
			found = true
		}
	}
	assert.True(t, found, "expected --resume sess-123 in args")
}

func TestBuildCopilotStreamArgs_SessionIDWithoutResume(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{
		Prompt:    "hello",
		SessionID: "sess-123",
		Resume:    false,
	})

	for _, a := range args {
		if a == "--resume" {
			t.Error("should not have --resume when Resume=false")
		}
	}
}

func TestBuildCopilotStreamArgs_WithWorkDir(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{
		Prompt:  "hello",
		WorkDir: "/tmp/project",
	})

	found := false
	for i, a := range args {
		if a == "-C" && i+1 < len(args) && args[i+1] == "/tmp/project" {
			found = true
		}
	}
	assert.True(t, found, "expected -C /tmp/project in args")
}

func TestBuildCopilotStreamArgs_WithoutWorkDir(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "-C" {
			t.Error("should not have -C when WorkDir is empty")
		}
	}
}

func TestBuildCopilotStreamArgs_WithModel(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{
		Prompt: "hello",
		Model:  "gpt-4o",
	})

	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "gpt-4o" {
			found = true
		}
	}
	assert.True(t, found, "expected --model gpt-4o in args")
}

func TestBuildCopilotStreamArgs_WithoutModel(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--model" {
			t.Error("should not have --model when Model is empty")
		}
	}
}

func TestBuildCopilotStreamArgs_WithThinkingEffort(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{
		Prompt:         "hello",
		ThinkingEffort: "high",
	})

	found := false
	for i, a := range args {
		if a == "--effort" && i+1 < len(args) && args[i+1] == "high" {
			found = true
		}
	}
	assert.True(t, found, "expected --effort high in args")
}

func TestBuildCopilotStreamArgs_WithoutThinkingEffort(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--effort" {
			t.Error("should not have --effort when ThinkingEffort is empty")
		}
	}
}

func TestBuildCopilotStreamArgs_AllOptions(t *testing.T) {
	args := buildCopilotStreamArgs(ChatRequest{
		Prompt:         "fix the bug",
		SessionID:      "sess-456",
		Resume:         true,
		WorkDir:        "/home/user/project",
		Model:          "claude-sonnet-4-6",
		ThinkingEffort: "medium",
	})

	argsStr := strings.Join(args, " ")
	assert.Contains(t, argsStr, "--resume sess-456")
	assert.Contains(t, argsStr, "-C /home/user/project")
	assert.Contains(t, argsStr, "--model claude-sonnet-4-6")
	assert.Contains(t, argsStr, "--effort medium")
}
