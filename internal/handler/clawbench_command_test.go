package handler

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// --- processClawbenchCommand tests ---

func TestProcessClawbenchCommand_ChatSearchInjects(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processClawbenchCommand("/cb-chatsearch fix login bug", "/project", "session-123")

	// Must contain injection template
	assert.Contains(t, result, "historical conversation search")
	assert.Contains(t, result, "/usr/local/bin/clawbench rag search")
	assert.Contains(t, result, "--project /project")
	assert.Contains(t, result, "--exclude-session-id session-123")
	// processClawbenchCommand returns ONLY the template (no raw message duplication);
	// the caller prepends the template to the prompt which already contains
	// the user's original message.
	assert.NotContains(t, result, "/cb-chatsearch fix login bug")
}

func TestProcessClawbenchCommand_TaskInjects(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processClawbenchCommand("/cb-task daily build", "/project", "session-456")

	assert.Contains(t, result, "scheduled task management")
	assert.Contains(t, result, "/usr/local/bin/clawbench task")
	assert.Contains(t, result, "--project /project")
	// processClawbenchCommand returns ONLY the template (no raw message duplication)
	assert.NotContains(t, result, "/cb-task daily build")
}

func TestProcessClawbenchCommand_NoPrefixPassesThrough(t *testing.T) {
	result := processClawbenchCommand("hello world", "/project", "session-123")
	assert.Equal(t, "hello world", result)
}

func TestProcessClawbenchCommand_EmptyQueryReturnsRaw(t *testing.T) {
	// /cb-chatsearch with only whitespace after should return the raw message
	// (caller handles the error response)
	result := processClawbenchCommand("/cb-chatsearch  ", "/project", "session-123")
	assert.Equal(t, "/cb-chatsearch  ", result)
}

func TestProcessClawbenchCommand_TaskEmptyDescReturnsInjected(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	// /cb-task with just a space still injects — task description can be short
	result := processClawbenchCommand("/cb-task ", "/project", "session-123")
	assert.Contains(t, result, "scheduled task management")
}

func TestProcessClawbenchCommand_PartialPrefixNoMatch(t *testing.T) {
	// /cb-chat without "search" should not match
	result := processClawbenchCommand("/cb-chat something", "/project", "session-123")
	assert.Equal(t, "/cb-chat something", result)
}

func TestProcessClawbenchCommand_AgentSlashCommandPassesThrough(t *testing.T) {
	// A regular agent slash command (no cb- namespace) must pass through
	// untouched — it is forwarded to the agent, not injected locally.
	result := processClawbenchCommand("/compact", "/project", "session-123")
	assert.Equal(t, "/compact", result)
}

// --- IsClawbenchCommand tests ---

func TestIsClawbenchCommand(t *testing.T) {
	assert.True(t, IsClawbenchCommand("/cb-chatsearch query"))
	assert.True(t, IsClawbenchCommand("/cb-task do thing"))
	assert.False(t, IsClawbenchCommand("/compact"))
	assert.False(t, IsClawbenchCommand("/cb-chatsearchx query")) // no space after command
	assert.False(t, IsClawbenchCommand("hello"))
	assert.False(t, IsClawbenchCommand(""))
}

func TestProcessClawbenchCommand_ChatSearchPlaceholderReplacement(t *testing.T) {
	model.ClawbenchBin = "/opt/clawbench/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processClawbenchCommand("/cb-chatsearch auth bug", "/my/project", "sess-abc")

	assert.Contains(t, result, "/opt/clawbench/bin/clawbench rag search")
	assert.Contains(t, result, "--project /my/project")
	assert.Contains(t, result, "--exclude-session-id sess-abc")
	// No unreplaced placeholders
	assert.NotContains(t, result, "{{CLAWBENCH_BIN}}")
	assert.NotContains(t, result, "{{PROJECT_PATH}}")
	assert.NotContains(t, result, "{{SESSION_ID}}")
}

func TestProcessClawbenchCommand_TaskPlaceholderReplacement(t *testing.T) {
	model.ClawbenchBin = "/opt/clawbench/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processClawbenchCommand("/cb-task daily report", "/my/project", "sess-abc")

	assert.Contains(t, result, "/opt/clawbench/bin/clawbench task")
	assert.Contains(t, result, "--project /my/project")
	assert.NotContains(t, result, "{{CLAWBENCH_BIN}}")
	assert.NotContains(t, result, "{{PROJECT_PATH}}")
}

func TestProcessClawbenchCommand_ChatSearchContainsNaturalFormat(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processClawbenchCommand("/cb-chatsearch test", "/project", "session-123")

	// Must instruct AI to present results naturally (no structured XML card format)
	assert.Contains(t, result, "natural, readable format")
	assert.NotContains(t, result, "<rag-results>")
	assert.NotContains(t, result, "<rag-item>")
}

func TestProcessClawbenchCommand_TaskContainsScheduledTaskTag(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	result := processClawbenchCommand("/cb-task test task", "/project", "session-123")

	assert.Contains(t, result, "<scheduled-task")
	assert.Contains(t, result, "--agent-id")
}

// TestProcessClawbenchCommand_PortAndDataDirInjected verifies that --port and --data-dir
// are injected into /cb-chatsearch and /cb-task templates so spawned CLI subprocesses
// can connect to the server even with non-default configuration.
func TestProcessClawbenchCommand_PortAndDataDirInjected(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	model.ServerPort = 8080
	model.DataDir = "/custom/data"
	defer func() {
		model.ClawbenchBin = ""
		model.ServerPort = 0
		model.DataDir = ""
	}()

	t.Run("chatsearch", func(t *testing.T) {
		result := processClawbenchCommand("/cb-chatsearch auth bug", "/project", "sess-1")
		assert.Contains(t, result, "--port 8080")
		assert.Contains(t, result, "--data-dir /custom/data")
		assert.NotContains(t, result, "{{PORT}}")
		assert.NotContains(t, result, "{{DATA_DIR}}")
	})

	t.Run("task", func(t *testing.T) {
		result := processClawbenchCommand("/cb-task daily build", "/project", "sess-1")
		assert.Contains(t, result, "--port 8080")
		assert.Contains(t, result, "--data-dir /custom/data")
		assert.NotContains(t, result, "{{PORT}}")
		assert.NotContains(t, result, "{{DATA_DIR}}")
	})
}

// TestProcessClawbenchCommand_PortAndDataDirDefault verifies that default port (20000)
// and default DataDir are correctly injected.
func TestProcessClawbenchCommand_PortAndDataDirDefault(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	model.ServerPort = 20000
	model.DataDir = "/home/user/.clawbench"
	defer func() {
		model.ClawbenchBin = ""
		model.ServerPort = 0
		model.DataDir = ""
	}()

	result := processClawbenchCommand("/cb-task test", "/project", "sess-1")
	assert.Contains(t, result, "--port 20000")
	assert.Contains(t, result, "--data-dir /home/user/.clawbench")
}

// TestProcessClawbenchCommand_TaskListAgentsIncludesPortAndDataDir verifies that the
// list-agents discovery command in the /cb-task template also includes --port and --data-dir.
func TestProcessClawbenchCommand_TaskListAgentsIncludesPortAndDataDir(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	model.ServerPort = 3000
	model.DataDir = "/tmp/cb"
	defer func() {
		model.ClawbenchBin = ""
		model.ServerPort = 0
		model.DataDir = ""
	}()

	result := processClawbenchCommand("/cb-task something", "/project", "sess-1")
	// The template contains a list-agents command that must also have port/data-dir
	assert.Contains(t, result, "list-agents --project /project --port 3000 --data-dir /tmp/cb")
}

// TestProcessClawbenchCommand_NoMessageDuplication verifies the fix for ISS-287:
// processClawbenchCommand must return ONLY the template without appending the
// original message, because the caller already prepends the result to a
// prompt that contains the user's original message. This prevents the
// user message from appearing twice in the AI prompt.
func TestProcessClawbenchCommand_NoMessageDuplication(t *testing.T) {
	model.ClawbenchBin = "/usr/local/bin/clawbench"
	defer func() { model.ClawbenchBin = "" }()

	// Simulate the full prompt construction flow:
	// 1. prompt starts as the user message
	// 2. processClawbenchCommand returns the template only
	// 3. caller prepends: prompt = template + "\n\n" + prompt
	userMsg := "/cb-chatsearch how to fix auth"
	projectPath := "/project"
	sessionID := "sess-1"

	prompt := userMsg
	atInjected := processClawbenchCommand(userMsg, projectPath, sessionID)
	prompt = atInjected + "\n\n" + prompt

	// Count occurrences of the user message in the final prompt
	count := 0
	idx := 0
	for {
		pos := indexOf(prompt, "/cb-chatsearch how to fix auth", idx)
		if pos < 0 {
			break
		}
		count++
		idx = pos + 1
	}
	assert.Equal(t, 1, count, "user message should appear exactly once in the final prompt (ISS-287)")

	// Same check for /cb-task
	userMsgTask := "/cb-task daily build"
	prompt = userMsgTask
	atInjected = processClawbenchCommand(userMsgTask, projectPath, sessionID)
	prompt = atInjected + "\n\n" + prompt

	count = 0
	idx = 0
	for {
		pos := indexOf(prompt, "/cb-task daily build", idx)
		if pos < 0 {
			break
		}
		count++
		idx = pos + 1
	}
	assert.Equal(t, 1, count, "user message should appear exactly once in the final prompt for /cb-task (ISS-287)")
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
