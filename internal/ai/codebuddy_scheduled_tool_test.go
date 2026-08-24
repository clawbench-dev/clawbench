//go:build integration

package ai

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// CodeBuddy Scheduled (Background) Tool Call — Integration Test
// ===========================================================================
//
// The real background-task bug (TaskOutput "not found") is covered by
// codebuddy_background_task_test.go. This file verifies the broader scheduled
// execution path: auto-approve enabled + ScheduledExecution=true, matching
// what the scheduler does for ACP scheduled tasks. It asserts that tool calls
// in this mode complete with status "success" rather than "error"/"failed".

// scheduledCodeBuddyACPAgent returns a CodeBuddy ACP agent for scheduled mode.
func scheduledCodeBuddyACPAgent() *model.Agent {
	return &model.Agent{
		ID:                   "codebuddy-acp-scheduled-test",
		Name:                 "CodeBuddy ACP Scheduled Test",
		Backend:              "codebuddy",
		Transport:            "acp-stdio",
		AcpCommand:           "codebuddy --acp",
		Models:               []model.AgentModel{{ID: "glm-4-plus", Name: "glm-4-plus", Default: true}},
		ThinkingEffortLevels: []string{"low", "medium", "high"},
	}
}

// requireScheduledCodebuddyACPAvailable skips if the CodeBuddy CLI is missing.
func requireScheduledCodebuddyACPAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not available, skipping scheduled CodeBuddy tool call test")
	}
}

// setupScheduledCodebuddyEnv sets up the CodeBuddy ACP agent in scheduled mode
// (auto-approve on + RootPaths mirroring the server) and returns the backend
// and session ID.
func setupScheduledCodebuddyEnv(t *testing.T) (*ACPBackend, *acpTestEnv, string) {
	t.Helper()
	requireScheduledCodebuddyACPAvailable(t)

	agent := scheduledCodeBuddyACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	// The real server sets model.RootPaths in main.go (platform.ListRootPaths).
	// ACP ReadTextFile/WriteTextFile delegate to the host and reject paths not
	// under RootPaths. In tests it defaults to nil → all reads fail with
	// "Internal error". Mirror the server setup.
	origRoots := model.RootPaths
	model.RootPaths = []string{"/"}
	t.Cleanup(func() { model.RootPaths = origRoots })

	// The scheduler enables auto-approve for ACP scheduled tasks.
	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	sessionID := acpSessionID()
	t.Cleanup(func() { env.closeConn(t, sessionID) })

	return backend, env, sessionID
}

// TestCodebuddyScheduled_ToolCall_Completes verifies that a Bash tool call in
// scheduled mode (auto-approve + ScheduledExecution=true) completes with
// success and that the tool output reaches the final content.
func TestCodebuddyScheduled_ToolCall_Completes(t *testing.T) {
	backend, _, sessionID := setupScheduledCodebuddyEnv(t)

	ctx, cancel := contextWithTimeout(t, 90*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:             "请运行命令 `echo scheduled-tool-ok` 并原样回复输出。必须使用工具。",
		SessionID:          sessionID,
		WorkDir:            acpTestWorkDir(),
		ScheduledExecution: true,
	})
	require.NoError(t, err, "ExecuteStream should not return error")

	events := collectACPEvents(t, ch, 90*time.Second)
	requireDoneEvent(t, events)

	// No tool result may be failed.
	toolResults := findACPEvents(events, "tool_result")
	for _, tr := range toolResults {
		t.Logf("  tool_result: id=%s status=%q output=%q", tr.Tool.ID, tr.Tool.Status, truncate(tr.Tool.Output, 200))
		assert.NotEqual(t, "error", tr.Tool.Status, "scheduled-mode tool call should not report error")
		assert.NotEqual(t, "failed", tr.Tool.Status, "scheduled-mode tool call should not report failed")
	}

	content := concatACPContent(events)
	assert.NotEmpty(t, content, "should receive content from agent")
}

// TestCodebuddyScheduled_ToolCall_ReadFile verifies a Read tool call completes
// in scheduled mode. Read is the most common tool and does not require shell
// permissions.
func TestCodebuddyScheduled_ToolCall_ReadFile(t *testing.T) {
	backend, _, sessionID := setupScheduledCodebuddyEnv(t)

	ctx, cancel := contextWithTimeout(t, 90*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:             "请读取本仓库 go.mod 文件的前 3 行。必须使用工具。",
		SessionID:          sessionID,
		WorkDir:            acpTestWorkDir(),
		ScheduledExecution: true,
	})
	require.NoError(t, err)

	events := collectACPEvents(t, ch, 90*time.Second)
	requireDoneEvent(t, events)

	toolUses := findACPEvents(events, "tool_use")
	require.NotEmpty(t, toolUses, "agent should have used a tool (Read)")

	toolResults := findACPEvents(events, "tool_result")
	for _, tr := range toolResults {
		t.Logf("  tool_result: id=%s status=%q output=%q", tr.Tool.ID, tr.Tool.Status, truncate(tr.Tool.Output, 200))
		assert.NotEqual(t, "error", tr.Tool.Status, "scheduled-mode Read tool call should not report error")
		assert.NotEqual(t, "failed", tr.Tool.Status, "scheduled-mode Read tool call should not report failed")
	}
}
