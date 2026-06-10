package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildKimiStreamArgs_BasicPrompt(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{Prompt: "hello world"})

	assert.Contains(t, args, "--print")
	assert.Contains(t, args, "--prompt")
	// The prompt may be modified by injectSystemPrompt
	promptIdx := -1
	for i, a := range args {
		if a == "--prompt" && i+1 < len(args) {
			promptIdx = i + 1
		}
	}
	assert.GreaterOrEqual(t, promptIdx, 0, "expected --prompt flag with value")
	assert.Contains(t, args[promptIdx], "hello world")
	assert.Contains(t, args, "--output-format")
	assert.Contains(t, args, "stream-json")
	assert.Contains(t, args, "--yes")
}

func TestBuildKimiStreamArgs_WithSystemPrompt(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt:       "hello",
		SystemPrompt: "be helpful",
	})

	// Kimi injects system prompt into the user prompt
	promptIdx := -1
	for i, a := range args {
		if a == "--prompt" && i+1 < len(args) {
			promptIdx = i + 1
		}
	}
	assert.GreaterOrEqual(t, promptIdx, 0)
	assert.Contains(t, args[promptIdx], "[System Instructions: be helpful]")
	assert.Contains(t, args[promptIdx], "hello")
}

func TestBuildKimiStreamArgs_WithResume(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt:    "follow-up",
		SessionID: "sess-123",
		Resume:    true,
	})

	found := false
	for i, a := range args {
		if a == "--session" && i+1 < len(args) && args[i+1] == "sess-123" {
			found = true
		}
	}
	assert.True(t, found, "expected --session sess-123 in args")
}

func TestBuildKimiStreamArgs_SessionIDWithoutResume(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt:    "hello",
		SessionID: "sess-123",
		Resume:    false,
	})

	for _, a := range args {
		if a == "--session" {
			t.Error("should not have --session when Resume=false")
		}
	}
}

func TestBuildKimiStreamArgs_WithWorkDir(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt:  "hello",
		WorkDir: "/tmp/project",
	})

	found := false
	for i, a := range args {
		if a == "--work-dir" && i+1 < len(args) && args[i+1] == "/tmp/project" {
			found = true
		}
	}
	assert.True(t, found, "expected --work-dir /tmp/project in args")
}

func TestBuildKimiStreamArgs_WithoutWorkDir(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--work-dir" {
			t.Error("should not have --work-dir when WorkDir is empty")
		}
	}
}

func TestBuildKimiStreamArgs_WithModel(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt: "hello",
		Model:  "kimi-latest",
	})

	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "kimi-latest" {
			found = true
		}
	}
	assert.True(t, found, "expected --model kimi-latest in args")
}

func TestBuildKimiStreamArgs_WithoutModel(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{Prompt: "hello"})

	for _, a := range args {
		if a == "--model" {
			t.Error("should not have --model when Model is empty")
		}
	}
}

func TestBuildKimiStreamArgs_ThinkingEffortOff(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt:         "hello",
		ThinkingEffort: "off",
	})

	assert.Contains(t, args, "--no-thinking")
	assert.NotContains(t, args, "--thinking")
}

func TestBuildKimiStreamArgs_ThinkingEffortOther(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt:         "hello",
		ThinkingEffort: "high",
	})

	assert.Contains(t, args, "--thinking")
	assert.NotContains(t, args, "--no-thinking")
}

func TestBuildKimiStreamArgs_WithoutThinkingEffort(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{Prompt: "hello"})

	assert.NotContains(t, args, "--thinking")
	assert.NotContains(t, args, "--no-thinking")
}

func TestBuildKimiStreamArgs_AllOptions(t *testing.T) {
	args := buildKimiStreamArgs(ChatRequest{
		Prompt:         "fix the bug",
		SessionID:      "sess-789",
		Resume:         true,
		WorkDir:        "/home/user/project",
		Model:          "moonshot-v1",
		ThinkingEffort: "high",
	})

	argsStr := strings.Join(args, " ")
	assert.Contains(t, argsStr, "--session sess-789")
	assert.Contains(t, argsStr, "--work-dir /home/user/project")
	assert.Contains(t, argsStr, "--model moonshot-v1")
	assert.Contains(t, argsStr, "--thinking")
}
