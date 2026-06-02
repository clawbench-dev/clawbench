package handler

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// --- processAtCommand tests ---

func TestProcessAtCommand_ChatSearchInjects(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processAtCommand("@chatsearch fix login bug", "/project", "session-123")

	// Must contain injection template
	assert.Contains(t, result, "historical conversation search")
	assert.Contains(t, result, "/usr/local/bin/clawbench rag search")
	assert.Contains(t, result, "--project /project")
	assert.Contains(t, result, "--exclude-session-id session-123")
	// processAtCommand returns ONLY the template (no raw message duplication);
	// the caller prepends the template to the prompt which already contains
	// the user's original message.
	assert.NotContains(t, result, "@chatsearch fix login bug")
}

func TestProcessAtCommand_TaskInjects(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processAtCommand("@task daily build", "/project", "session-456")

	assert.Contains(t, result, "scheduled task management")
	assert.Contains(t, result, "/usr/local/bin/clawbench task")
	assert.Contains(t, result, "--project /project")
	// processAtCommand returns ONLY the template (no raw message duplication)
	assert.NotContains(t, result, "@task daily build")
}

func TestProcessAtCommand_NoPrefixPassesThrough(t *testing.T) {
	result := processAtCommand("hello world", "/project", "session-123")
	assert.Equal(t, "hello world", result)
}

func TestProcessAtCommand_EmptyQueryReturnsRaw(t *testing.T) {
	// @chatsearch with only whitespace after should return the raw message
	// (caller handles the error response)
	result := processAtCommand("@chatsearch  ", "/project", "session-123")
	assert.Equal(t, "@chatsearch  ", result)
}

func TestProcessAtCommand_TaskEmptyDescReturnsInjected(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	// @task with just a space still injects — task description can be short
	result := processAtCommand("@task ", "/project", "session-123")
	assert.Contains(t, result, "scheduled task management")
}

func TestProcessAtCommand_PartialPrefixNoMatch(t *testing.T) {
	// @chat without "search" should not match
	result := processAtCommand("@chat something", "/project", "session-123")
	assert.Equal(t, "@chat something", result)
}

func TestProcessAtCommand_ChatSearchPlaceholderReplacement(t *testing.T) {
	model.ClawbenchBin = "/opt/clawbench/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processAtCommand("@chatsearch auth bug", "/my/project", "sess-abc")

	assert.Contains(t, result, "/opt/clawbench/bin/clawbench rag search")
	assert.Contains(t, result, "--project /my/project")
	assert.Contains(t, result, "--exclude-session-id sess-abc")
	// No unreplaced placeholders
	assert.NotContains(t, result, "{{CLAWBENCH_BIN}}")
	assert.NotContains(t, result, "{{PROJECT_PATH}}")
	assert.NotContains(t, result, "{{SESSION_ID}}")
}

func TestProcessAtCommand_TaskPlaceholderReplacement(t *testing.T) {
	model.ClawbenchBin = "/opt/clawbench/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processAtCommand("@task daily report", "/my/project", "sess-abc")

	assert.Contains(t, result, "/opt/clawbench/bin/clawbench task")
	assert.Contains(t, result, "--project /my/project")
	assert.NotContains(t, result, "{{CLAWBENCH_BIN}}")
	assert.NotContains(t, result, "{{PROJECT_PATH}}")
}

func TestProcessAtCommand_ChatSearchContainsXMLFormat(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processAtCommand("@chatsearch test", "/project", "session-123")

	// Must instruct AI about XML output format
	assert.Contains(t, result, "<rag-results>")
	assert.Contains(t, result, "<rag-item>")
	assert.Contains(t, result, "<session-id>")
	assert.Contains(t, result, "<session-title>")
	assert.Contains(t, result, "<created-at>")
	assert.Contains(t, result, "<summary>")
}

func TestProcessAtCommand_TaskContainsScheduledTaskTag(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processAtCommand("@task test task", "/project", "session-123")

	assert.Contains(t, result, "<scheduled-task")
	assert.Contains(t, result, "--agent-id")
}

// TestProcessAtCommand_NoMessageDuplication verifies the fix for ISS-287:
// processAtCommand must return ONLY the template without appending the
// original message, because the caller already prepends the result to a
// prompt that contains the user's original message. This prevents the
// user message from appearing twice in the AI prompt.
func TestProcessAtCommand_NoMessageDuplication(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	// Simulate the full prompt construction flow:
	// 1. prompt starts as the user message
	// 2. processAtCommand returns the template only
	// 3. caller prepends: prompt = template + "\n\n" + prompt
	userMsg := "@chatsearch how to fix auth"
	projectPath := "/project"
	sessionID := "sess-1"

	prompt := userMsg
	atInjected := processAtCommand(userMsg, projectPath, sessionID)
	prompt = atInjected + "\n\n" + prompt

	// Count occurrences of the user message in the final prompt
	count := 0
	idx := 0
	for {
		pos := indexOf(prompt, "@chatsearch how to fix auth", idx)
		if pos < 0 {
			break
		}
		count++
		idx = pos + 1
	}
	assert.Equal(t, 1, count, "user message should appear exactly once in the final prompt (ISS-287)")

	// Same check for @task
	userMsgTask := "@task daily build"
	prompt = userMsgTask
	atInjected = processAtCommand(userMsgTask, projectPath, sessionID)
	prompt = atInjected + "\n\n" + prompt

	count = 0
	idx = 0
	for {
		pos := indexOf(prompt, "@task daily build", idx)
		if pos < 0 {
			break
		}
		count++
		idx = pos + 1
	}
	assert.Equal(t, 1, count, "user message should appear exactly once in the final prompt for @task (ISS-287)")
}

// indexOf returns the index of substr in s starting at offset, or -1.
func indexOf(s, substr string, offset int) int {
	if offset > len(s) {
		return -1
	}
	for i := offset; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
