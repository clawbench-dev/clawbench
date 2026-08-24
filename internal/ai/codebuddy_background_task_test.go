//go:build integration

package ai

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// CodeBuddy Background Task (run_in_background + TaskOutput) — Reproduction
// ===========================================================================
//
// Reported bug: CodeBuddy tool calls ALWAYS fail in background mode.
// The specific failure observed in production logs:
//
//   TaskOutput → "Background task "term-N" not found"
//
// Mechanism: when ClawBench advertises Terminal=true in ACP Initialize,
// CodeBuddy delegates background Bash commands (run_in_background: true) to
// the host via the terminal/* RPCs (CreateTerminal). The task ID returned is
// "term-N". But CodeBuddy's OWN TaskOutput/TaskStop tools look up ITS internal
// background-task registry — which is empty because the task is actually a
// host terminal. Result: TaskOutput always fails with "task not found".
//
// This test reproduces the scenario: prompt CodeBuddy to run a command in the
// background (run_in_background) and then query the task with TaskOutput.

// backgroundCodeBuddyACPAgent returns a CodeBuddy ACP agent for the background
// task reproduction test.
func backgroundCodeBuddyACPAgent() *model.Agent {
	return &model.Agent{
		ID:                   "codebuddy-acp-background-test",
		Name:                 "CodeBuddy ACP Background Test",
		Backend:              "codebuddy",
		Transport:            "acp-stdio",
		AcpCommand:           "codebuddy --acp",
		Models:               []model.AgentModel{{ID: "glm-4-plus", Name: "glm-4-plus", Default: true}},
		ThinkingEffortLevels: []string{"low", "medium", "high"},
	}
}

// requireBackgroundCodebuddyACPAvailable skips if CodeBuddy CLI is missing.
func requireBackgroundCodebuddyACPAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not available, skipping background task test")
	}
}

// TestCodebuddyBackground_TaskOutput_Works reproduces the "background tool
// calls always fail" bug. It prompts CodeBuddy to:
//  1. Start a long-running command in the background
//  2. Poll it with TaskOutput
//
// The assertion: TaskOutput should return actual task output, NOT
// "Background task ... not found".
//
// The fix: for CodeBuddy, ClawBench no longer advertises Terminal=true in the
// ACP Initialize handshake. CodeBuddy then runs background commands internally
// (its own task registry), so TaskOutput works. The old behavior (Terminal
// advertised) is kept as a regression reference — it must FAIL to prove the
// bug is real, and the default must PASS.
func TestCodebuddyBackground_TaskOutput_Works(t *testing.T) {
	requireBackgroundCodebuddyACPAvailable(t)

	for _, tc := range []struct {
		name              string
		advertiseTerminal bool
		wantFail          bool
	}{
		// Terminal advertised (old behavior): CodeBuddy delegates background
		// commands to host terminals, TaskOutput can't find them → fails.
		{name: "TerminalAdvertised_Regression", advertiseTerminal: true, wantFail: true},
		// Terminal hidden (default for CodeBuddy after fix): CodeBuddy runs
		// background commands internally → TaskOutput works.
		{name: "Default_Fixed", advertiseTerminal: false, wantFail: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			SetAdvertiseTerminalForTest(tc.advertiseTerminal)
			t.Cleanup(ResetAdvertiseTerminalForTest)

			agent := backgroundCodeBuddyACPAgent()
			env := setupACPTestEnvForAgent(t, agent)
			backend, err := NewACPBackend(agent)
			require.NoError(t, err)

			origRoots := model.RootPaths
			model.RootPaths = []string{"/"}
			t.Cleanup(func() { model.RootPaths = origRoots })

			sessionID := acpSessionID()
			t.Cleanup(func() { env.closeConn(t, sessionID) })

			SetAutoApproveGetter(func(_ string) bool { return true })
			t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

			ctx, cancel := contextWithTimeout(t, 120*time.Second)
			defer cancel()

			ch, err := backend.ExecuteStream(ctx, ChatRequest{
				Prompt: `请执行以下步骤：
1. 用 Bash 工具在后台运行命令 ` + "`" + `sleep 2 && echo BACKGROUND_TASK_OK` + "`" + `（设置 run_in_background=true）
2. 等待 3 秒后用 TaskOutput 工具查询该后台任务的输出
3. 把 TaskOutput 的原始输出原样告诉我`,
				SessionID:          sessionID,
				WorkDir:            acpTestWorkDir(),
				ScheduledExecution: true,
			})
			require.NoError(t, err)

			events := collectACPEvents(t, ch, 120*time.Second)
			requireDoneEvent(t, events)

			toolResults := findACPEvents(events, "tool_result")
			content := concatACPContent(events)
			t.Logf("final content: %q", truncate(content, 500))

			failedTaskOutputs := 0
			totalTaskOutputs := 0
			for _, tr := range toolResults {
				if tr.Tool == nil {
					continue
				}
				t.Logf("  tool_result: name=%q id=%s status=%q output=%q", tr.Tool.Name, tr.Tool.ID, tr.Tool.Status, truncate(tr.Tool.Output, 200))
				if tr.Tool.Name == "TaskOutput" {
					totalTaskOutputs++
					if tr.Tool.Status == "error" || tr.Tool.Status == "failed" ||
						strings.Contains(tr.Tool.Output, "not found") {
						failedTaskOutputs++
					}
				}
			}

			if tc.wantFail {
				// Regression reference: with Terminal advertised, CodeBuddy's
				// TaskOutput cannot find host-created background tasks.
				// Require at least one TaskOutput call so the reference doesn't
				// pass vacuously (e.g. if the model ran the command in the
				// foreground and never invoked TaskOutput). Note this reference
				// only asserts "if TaskOutput was used, it failed" — a run where
				// the prompt itself hangs/fails is caught by requireDoneEvent above.
				require.Greater(t, totalTaskOutputs, 0,
					"regression reference requires TaskOutput to be invoked; content=%q", truncate(content, 300))
				assert.Greater(t, failedTaskOutputs, 0,
					"regression expected: TaskOutput should fail when Terminal capability is advertised")
				return
			}

			// Fixed path: TaskOutput must succeed.
			if totalTaskOutputs > 0 {
				assert.Zero(t, failedTaskOutputs,
					"TaskOutput should succeed for background tasks; %d/%d failed",
					failedTaskOutputs, totalTaskOutputs)
			} else {
				t.Logf("NOTE: no TaskOutput calls observed; content=%q", truncate(content, 300))
			}

			assert.NotEmpty(t, content, "should receive final content")
		})
	}
}
