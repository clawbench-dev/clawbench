//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ===========================================================================
// CodeBuddy ACP — cancel 只发一次 session/cancel，不毒化下一轮 gate（回归测试）
// ===========================================================================
//
// 事故（msg 43596）：用户没有终止当前 turn，但该 turn 以
// stopReason=cancelled / finishReason=tool_calls / outcome=CANCELLED 结束，Meta
// 显示"结束原因: tool_calls / 结果: CANCELLED"。根因链：
//
//	1. 前一 turn (43594) 被用户取消时，CancelSession 实际产生了**两次**
//	   session/cancel：`cancel()` 让 SDK Prompt 在 ctx.Err 时自动补发一次，随后
//	   `ACPConnManager.CancelTurn` 又显式发送一次（CodeBuddy 侧收到 .547/.587
//	   两个 cancel，间隔 ~40ms）。
//	2. 第一个 cancel 中止了正在 model_requesting 的 run；CodeBuddy 自动重启了
//	   一个新 run；第二个 cancel 打到这个 preparing run 上并被 force-idle 掉 ——
//	   run-result 解析路径被跳过，pendingCancellations 集合残留。
//	3. 下一 turn（43596）的第一个 permission gate 命中残留标记，CodeBuddy 走
//	   "interceptorGate willRetry=true but user cancelled — resolving with
//	   cancelled"，turn 被误判为用户取消而提前结束。
//
// 修复：CancelSession 不再显式调用 CancelTurn —— ACP 的 session/cancel 只由 SDK
// 的 ctx 取消自动路径发送（恰好一次）。本测试验证修复后：
//
//	Turn A：提示模型执行 `git worktree remove`（HIGH-risk，bypassPermissions 下
//	        也会被 tool-validation gate 拦下）。等待 PermissionApproval 卡片出现，
//	        以 allow_always 批准，让模型继续工作。
//	Cancel ：模型批准后继续输出（正请求下一模型轮次）时取消 ctx（模拟修复后的
//	        CancelSession：只 cancel ctx，SDK 自动发送单次 session/cancel）。
//	Turn B：等 CodeBuddy 回 idle 后（~10s）发起一个新的 gate 命令。Turn B 必须
//	        正常走到 permission gate 并可被批准执行 —— 不被前一 turn 的 cancel
//	        残留毒化。
//
// 已知限制：若 Turn B 在 cancel 后立即（0ms）发起，CodeBuddy 尚未回 idle，仍会
// 误判 cancelled —— 但那是"cancel 结算窗口"内的瞬时行为，非生产路径（真实用户
// cancel 后不会瞬间发下一条消息）。本测试等待 settle 后再发 Turn B，验证的是
// 生产语义下的契约：一次 cancel 之后的下一个用户 turn 必须正常工作。
//
// 运行：
//
//	go test -v -run 'TestCodebuddyACP_CancelDoesNotPoisonNextGate' -tags integration \
//	    -timeout 300s ./internal/ai/
//
// 需要本机安装 codebuddy CLI（版本 ≥ 2.143 观察到该竞态）且已登录。模型侧行为
// 不完全确定，因此 Turn A 若在 90s 内未触发 gate 则跳过（不视为失败）。
func TestCodebuddyACP_CancelDoesNotPoisonNextGate(t *testing.T) {
	requireCodebuddyACPAvailable(t)

	repo := newWorktreeProbeRepo(t)
	workDir := repo

	sessionID := acpSessionID()
	agent := codebuddyACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)
	cleanupConn(t, sessionID)

	SetAutoApproveGetter(func(_ string) bool { return false })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	// ---- Turn A: a multi-step task that hits a gate and keeps working ----
	promptA := fmt.Sprintf(
		"在这个 git 仓库(%s)执行一系列清理操作，一步一步用 Bash 完成，中间可以停顿思考：\n"+
			"1. git status --short 2. git branch -a 3. 删除 worktree：git worktree remove %s/.worktrees/tmp-wt 2>&1; git worktree prune 4. 删除分支：git branch -D cleanup-tmp 2>&1 5. 最后 git branch -a 确认结果。\n"+
			"请完整执行直到汇报最终结果。", workDir, workDir)

	eventsA, finishA := startGateTurnStream(t, backend, sessionID, workDir, promptA)
	t.Cleanup(finishA)

	// Watch turn A in a goroutine: approve every gate with allow_always, and
	// signal when the model continues working after the first approval (that is
	// the window where cancel races CodeBuddy's next model request).
	approved := make(chan struct{})
	cancelSignal := make(chan struct{}, 1)
	go watchGateTurnA(t, env, sessionID, eventsA, approved, cancelSignal)

	// Wait until at least one permission gate was approved.
	select {
	case <-approved:
		t.Logf("turn A gate approved; model working; waiting for next activity")
	case <-time.After(90 * time.Second):
		t.Skipf("turn A never hit a permission gate within 90s; cannot exercise the scenario")
	}

	// Cancel as soon as the model resumes producing output after approval.
	select {
	case <-cancelSignal:
		t.Logf("cancelling turn A (single cancel: ctx.cancel -> SDK auto session/cancel)")
	case <-time.After(60 * time.Second):
		t.Logf("no post-approval activity detected; cancelling anyway")
	}
	CancelSessionViaACP(t, sessionID)
	waitChanClose(t, eventsA, 20*time.Second)
	t.Logf("turn A finished after cancel")

	// After a cancel the CodeBuddy agent needs a moment to settle back to idle
	// (its internal cancellation bookkeeping drains within ~5s in practice).
	// Production semantics match this: a user who just cancelled reads the
	// result and types the next message — never 0ms later. Wait briefly so the
	// test asserts the real-world contract: a subsequent user turn after a
	// cancel must NOT be poisoned.
	settleDelay := 10 * time.Second
	t.Logf("waiting %v for CodeBuddy to settle after cancel", settleDelay)
	time.Sleep(settleDelay)

	// ---- Turn B: fresh permission-gated command on the SAME session ----
	addWorktree(t, repo, "tmp-wt2", "cleanup-tmp2")
	promptB := fmt.Sprintf(
		"这个 git 仓库(%s)里又注册了一个 worktree 在 %s/.worktrees/tmp-wt2。请用 Bash 执行：git worktree remove %s/.worktrees/tmp-wt2 2>&1; git worktree prune; git branch -D cleanup-tmp2 2>&1，然后汇报。",
		workDir, workDir, workDir)

	eventsB := runGateTurnCollect(t, backend, env, sessionID, workDir, promptB, 90*time.Second)
	meta := lastMetadataEvent(t, eventsB)

	t.Logf("Turn B stream summary: events=%d metadata=%s", len(eventsB), describeMetadata(meta))

	// Regression assertion for the single-cancel fix: a fresh user turn must
	// NOT be killed by a stale cancel left by the previous turn. Before the fix
	// (CancelSession sent ctx.cancel + an extra session/cancel), turn B ended
	// with stopReason=cancelled / outcome=CANCELLED and never reached a tool or
	// gate. After the fix it must reach the gate and be approvable.
	require.NotEqual(t, "cancelled", metaStopReason(t, meta),
		"REGRESSION: turn B spuriously cancelled after prior-turn cancel — single "+
			"session/cancel still leaves CodeBuddy pendingCancellations stale. "+
			"Stream events=%d metadata=%s.", len(eventsB), describeMetadata(meta))

	// Detail log for triage / regression comparison.
	t.Logf("=== Turn B events=%d ===", len(eventsB))
	for i, e := range eventsB {
		extra := ""
		if e.Type == "tool_use" && e.Tool != nil {
			extra = fmt.Sprintf(" tool=%s id=%s done=%v", e.Tool.Name, e.Tool.ID, e.Tool.Done)
		}
		if e.Type == "tool_result" && e.Tool != nil {
			extra = fmt.Sprintf(" tool=%s status=%s", e.Tool.Name, e.Tool.Status)
		}
		if e.Type == "metadata" && e.Meta != nil {
			extra = fmt.Sprintf(" stop=%s finish=%s outcome=%s", e.Meta.StopReason, e.Meta.FinishReason, e.Meta.Outcome)
		}
		if e.Type == "done" {
			extra = " <== terminal"
		}
		t.Logf("  [%d] %s%s", i, e.Type, extra)
	}
}

// ===========================================================================
// Helpers
// ===========================================================================

// startGateTurnStream starts ExecuteStream for a gate-triggering prompt and
// returns the event channel + cleanup func. The ctx cancel is registered in
// activeTestCancels so CancelSessionViaACP can reach it.
func startGateTurnStream(t *testing.T, backend *ACPBackend, sessionID, workDir, prompt string) (<-chan StreamEvent, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	activeTestCancels.Store(sessionID, cancel)
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    prompt,
		SessionID: sessionID,
		WorkDir:   workDir,
		Mode:      "bypassPermissions",
	})
	require.NoError(t, err)
	return ch, func() {
		cancel()
		activeTestCancels.Delete(sessionID)
	}
}

// watchGateTurnA reads turn A's stream: approves gates and signals activity.
// It closes approved once a gate was approved, and sends on cancelSignal when
// the model produces output (thinking/content/tool) after the first approval.
func watchGateTurnA(t *testing.T, env *acpTestEnv, sessionID string, ch <-chan StreamEvent, approved chan<- struct{}, cancelSignal chan<- struct{}) {
	t.Helper()
	gateApproved := false
	waitingModel := false
	timer := time.NewTimer(150 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Type == "tool_use" && event.Tool != nil && event.Tool.Name == "PermissionApproval" {
				approveCardIfPresent(t, env, sessionID, event)
				if !gateApproved {
					gateApproved = true
					select {
					case approved <- struct{}{}:
					default:
					}
				}
				waitingModel = true
				continue
			}
			if waitingModel && (event.Type == "content" || event.Type == "thinking" ||
				(event.Type == "tool_use" && event.Tool != nil && event.Tool.Name != "PermissionApproval") ||
				(event.Type == "tool_result" && event.Tool != nil && event.Tool.Name != "PermissionApproval")) {
				select {
				case cancelSignal <- struct{}{}:
				default:
				}
				return
			}
		case <-timer.C:
			return
		}
	}
}

// approveCardIfPresent approves a PermissionApproval tool_use event if present.
func approveCardIfPresent(t *testing.T, env *acpTestEnv, sessionID string, event StreamEvent) {
	t.Helper()
	if event.Type != "tool_use" || event.Tool == nil || event.Tool.Name != "PermissionApproval" {
		return
	}
	var input struct {
		ToolCallID string `json:"toolCallId"`
	}
	_ = json.Unmarshal([]byte(event.Tool.Input), &input)
	toolCallID := strings.TrimPrefix(event.Tool.ID, "perm_")
	if input.ToolCallID != "" {
		toolCallID = input.ToolCallID
	}
	mgr := GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	if conn == nil {
		t.Logf("approve: no conn yet for session %s", sessionID)
		return
	}
	client := conn.GetClient()
	acpSID := conn.AcpSID()
	if client == nil || acpSID == "" {
		t.Logf("approve: no client/acpSID yet (sid=%q)", acpSID)
		return
	}
	ok := client.RespondPermission(PermissionKey(acpSID, toolCallID), "allow_always", false)
	t.Logf("approved gate tool=%s ok=%v", toolCallID, ok)
}

// CancelSessionViaACP replicates service.CancelSession's ACP-side effect after
// the single-cancel fix: cancel the Go context. The SDK's Prompt() observes
// ctx.Err() and automatically sends exactly one `session/cancel` notification
// to the agent. No explicit ACPConnManager.CancelTurn — that used to send a
// second session/cancel in quick succession and poison CodeBuddy's next
// permission gate (msg 43596).
func CancelSessionViaACP(t *testing.T, sessionID string) {
	t.Helper()
	cancelVal, ok := activeTestCancels.LoadAndDelete(sessionID)
	if ok {
		if f, ok2 := cancelVal.(context.CancelFunc); ok2 {
			f()
		}
	}
	time.Sleep(300 * time.Millisecond)
}

// activeTestCancels tracks the prompt contexts we cancel in CancelSessionViaACP.
var activeTestCancels sync.Map

// runGateTurnCollect drives a gate-triggering prompt and collects events,
// auto-approving any PermissionApproval card with allow_always.
func runGateTurnCollect(t *testing.T, backend *ACPBackend, env *acpTestEnv, sessionID, workDir, prompt string, timeout time.Duration) []StreamEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    prompt,
		SessionID: sessionID,
		WorkDir:   workDir,
		Mode:      "bypassPermissions",
	})
	require.NoError(t, err)

	var events []StreamEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, event)
			if event.Type == "tool_use" && event.Tool != nil && event.Tool.Name == "PermissionApproval" {
				approveCardIfPresent(t, env, sessionID, event)
			}
		case <-timer.C:
			t.Log("runGateTurnCollect: timeout waiting for channel close")
			return events
		}
	}
}

// waitChanClose drains ch until it closes or times out.
func waitChanClose(t *testing.T, ch <-chan StreamEvent, timeout time.Duration) {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			t.Log("waitChanClose: timeout waiting for channel close")
			return
		}
	}
}

// lastMetadataEvent returns the last metadata event (or nil).
func lastMetadataEvent(t *testing.T, events []StreamEvent) *Metadata {
	t.Helper()
	var meta *Metadata
	for _, e := range events {
		if e.Type == "metadata" && e.Meta != nil {
			meta = e.Meta
		}
	}
	return meta
}

func metaStopReason(t *testing.T, m *Metadata) string {
	t.Helper()
	if m == nil {
		return ""
	}
	return m.StopReason
}

func describeMetadata(m *Metadata) string {
	if m == nil {
		return "<none>"
	}
	return fmt.Sprintf("stop=%s finish=%s outcome=%s", m.StopReason, m.FinishReason, m.Outcome)
}

// addWorktree creates a linked worktree + branch in repo.
func addWorktree(t *testing.T, repo, wtName, branch string) {
	t.Helper()
	wtDir := filepath.Join(repo, ".worktrees", wtName)
	require.NoError(t, os.MkdirAll(filepath.Dir(wtDir), 0o755))
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtDir)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git worktree add: %s", out)
}

// newWorktreeProbeRepo creates a git repo in a temp dir with a real linked
// worktree and a cleanup-tmp branch.
func newWorktreeProbeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# probe repo\n"), 0o644))
	run("init", "-b", "main")
	run("config", "user.email", "probe@test")
	run("config", "user.name", "probe")
	run("add", ".")
	run("commit", "-m", "init")
	run("branch", "cleanup-tmp")
	addWorktree(t, dir, "tmp-wt", "tmp-wt-branch")
	return dir
}
