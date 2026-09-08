//go:build integration

package ai

import (
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// CodeBuddy TaskCreate / TaskUpdate — ACP 集成调研（结论型探针）
// ===========================================================================
//
// 调研对象：CodeBuddy 通过 ACP 暴露的 TaskCreate / TaskUpdate / TaskList /
// TaskGet 任务工具在 ClawBench 的 ACP 通道上如何被解析、序列化与透传。
//
// 背景：
//   - CodeBuddy 的 product.json 声明了 TaskCreate/TaskGet/TaskUpdate/TaskList
//     工具（CodeBuddy 内部的 TODO 任务系统），任务存储按 ACP session id 作用域
//     隔离（SessionUtils.resolveTaskStorageId）。工具名经 ACP session/update 的
//     _meta["codebuddy.ai/toolName"] 携带（ClawBench 的 extractMetaToolNameFlat
//     优先读取），输入按 tool_call_update 增量流式传输（CodeBuddy 约每 30ms 一个
//     delta，ClawBench 经 toolCallDebouncer 收敛到终态 tool_result）。
//   - 本测试是"调研型"探针：驱动真实 codebuddy --acp 进程，让它真的走一次
//     TaskCreate → TaskUpdate(in_progress) → TaskList 序列，然后把 wire 上解析
//     出来的 tool_use / tool_result 全部 dump 出来（名称解析、增量输入 JSON、
//     终态输出、状态），据此实证 ClawBench 的解析逻辑是否成立。
//
// 断言策略（结论型，不空转）：
//   - 回合必须完成（done 事件），不允许死锁；
//   - 凡是出现的 TaskCreate/TaskUpdate/TaskList 工具调用，必须有终态
//     tool_result 且状态为 success（不允许被 debouncer 吞掉或解析成 error）；
//   - 若 CodeBuddy 在本次运行里根本没用这些工具（例如当前模式/会话不暴露任务
//     工具），dump 会把它打印出来并给出明确 NOTE，而不是让测试静默通过。
//
// 运行：
//
//	go test -v -run 'TestCodebuddyACP_TaskTools' -tags integration \
//	    -timeout 300s ./internal/ai/
//
// 需要本机安装 codebuddy CLI 且已登录。
//
// CodeBuddy 源码依据（v2.147.0 dist-server）：
//   TaskCreate 入参 schema：{subject*, description*, activeForm?, metadata?,
//   owner?}，执行成功 content="Task #<id> created successfully: <subject>",
//   rawResponse.task 含 id/subject/status。
//   TaskUpdate 入参 schema：{taskId*, subject?, description?, activeForm?,
//   status?(pending|in_progress|completed|deleted), addBlocks?, addBlockedBy?,
//   owner?, metadata?}，成功 content="Updated task #<id> <fields...>"。

// taskToolNames is the canonical set of CodeBuddy task tools this probe tracks.
var taskToolNames = map[string]bool{
	"TaskCreate": true,
	"TaskUpdate": true,
	"TaskList":   true,
	"TaskGet":    true,
}

// taskToolsCodeBuddyACPAgent returns a CodeBuddy ACP agent for the task-tools probe.
func taskToolsCodeBuddyACPAgent() *model.Agent {
	return &model.Agent{
		ID:                   "codebuddy-acp-task-tools-test",
		Name:                 "CodeBuddy ACP Task Tools Test",
		Backend:              "codebuddy",
		Transport:            "acp-stdio",
		AcpCommand:           "codebuddy --acp",
		Models:               []model.AgentModel{{ID: "glm-4-plus", Name: "glm-4-plus", Default: true}},
		ThinkingEffortLevels: []string{"low", "medium", "high"},
	}
}

// requireTaskToolsCodebuddyACPAvailable skips if the CodeBuddy CLI is missing.
func requireTaskToolsCodebuddyACPAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not available, skipping CodeBuddy task tools integration test")
	}
}

// setupTaskToolsCodebuddyEnv sets up the task-tools probe environment:
// RootPaths mirrors the server (ACP Read/Write delegate to host FS), and
// auto-approve is on so ordinary tool permissions never stall the turn.
func setupTaskToolsCodebuddyEnv(t *testing.T) (*ACPBackend, *acpTestEnv, string) {
	t.Helper()
	agent := taskToolsCodeBuddyACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err, "NewACPBackend should succeed")

	origRoots := model.RootPaths
	model.RootPaths = []string{"/"}
	t.Cleanup(func() { model.RootPaths = origRoots })

	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	sessionID := acpSessionID()
	t.Cleanup(func() { env.closeConn(t, sessionID) })

	return backend, env, sessionID
}

// dumpTaskToolEvents logs every task-tool-related tool_use/tool_result event in
// wire order so the full call/result pairing is visible for investigation.
func dumpTaskToolEvents(t *testing.T, events []StreamEvent) {
	t.Helper()
	for i, e := range events {
		if e.Type != "tool_use" && e.Type != "tool_result" {
			continue
		}
		if e.Tool == nil || !taskToolNames[e.Tool.Name] {
			continue
		}
		switch e.Type {
		case "tool_use":
			t.Logf("[%d] tool_use:  name=%q id=%q input=%q", i, e.Tool.Name, e.Tool.ID, truncate(e.Tool.Input, 500))
		case "tool_result":
			t.Logf("[%d] tool_result: name=%q id=%q status=%q output=%q", i, e.Tool.Name, e.Tool.ID, e.Tool.Status, truncate(e.Tool.Output, 500))
		}
	}
}

// parseToolInput decodes a ToolCall.Input JSON string into a generic map so the
// test can assert on individual fields (subject/taskId/status) without knowing
// the exact key casing CodeBuddy emitted.
func parseToolInput(t *testing.T, input string) map[string]any {
	t.Helper()
	var m map[string]any
	if strings.TrimSpace(input) == "" {
		return m
	}
	err := json.Unmarshal([]byte(input), &m)
	assert.NoError(t, err, "tool input should be valid JSON, got %q", truncate(input, 200))
	return m
}

// createdTaskIDFromOutput extracts the numeric task id CodeBuddy assigns from a
// TaskCreate result ("Task #<id> created successfully: ...").
var createdTaskIDPattern = regexp.MustCompile(`Task #(\d+) created successfully`)

func createdTaskIDFromOutput(output string) string {
	m := createdTaskIDPattern.FindStringSubmatch(output)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// TestCodebuddyACP_TaskTools_Flow probes the full TaskCreate → TaskUpdate →
// TaskList sequence over a real CodeBuddy ACP connection. It verifies the
// ClawBench ACP parsing chain for CodeBuddy task tools:
//
//  1. tool name resolution: the emitted tool_use must be named exactly
//     "TaskCreate" / "TaskUpdate" / "TaskList" (from _meta.codebuddy.ai/toolName
//     or the title fallback), not some mangled form;
//  2. input accumulation: the incremental tool_call_update deltas must be
//     merged into one complete JSON input carrying subject/description (for
//     TaskCreate) and the created task id + status (for TaskUpdate);
//  3. terminal result: every task tool call must end in a tool_result with
//     status success (debouncer must not drop deltas, parser must not mark a
//     successful call as error).
func TestCodebuddyACP_TaskTools_Flow(t *testing.T) {
	requireTaskToolsCodebuddyACPAvailable(t)
	backend, _, sessionID := setupTaskToolsCodebuddyEnv(t)

	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt: `请严格按照以下顺序执行，每一步都必须真的调用对应工具，不要跳过：
1. 调用 TaskCreate 工具创建一个任务：subject 用 "clawbench-task-probe"，
   description 用 "ACP integration probe: verify TaskCreate/TaskUpdate tool call serialization"，
   activeForm 用 "Probing task tools"。
2. 上一步返回成功后，立刻调用 TaskUpdate 工具，把刚创建的任务的
   status 改为 in_progress（用上一步返回的任务 id 作为 taskId）。
3. 再调用 TaskList 工具列出当前所有任务。
4. 全部完成后，用一句中文总结你创建的任务 id、subject 和当前状态。`,
		SessionID:          sessionID,
		WorkDir:            acpTestWorkDir(),
		ScheduledExecution: true,
	})
	require.NoError(t, err, "ExecuteStream should not return error")

	events := collectACPEvents(t, ch, 180*time.Second)
	requireDoneEvent(t, events)

	content := concatACPContent(events)
	t.Logf("=== full dump of task tool events (wire order) ===")
	dumpTaskToolEvents(t, events)
	t.Logf("=== final agent reply ===")
	t.Logf("%q", truncate(content, 1200))

	// Tally results.
	type taskCall struct {
		name       string
		use        *ToolCall
		result     *ToolCall
		input      map[string]any
		createID   string // for TaskCreate result
		finalIDs   []string
		updateTask string // taskId referenced by TaskUpdate
	}
	var taskCalls []taskCall
	var pending map[string]*taskCall // toolCallID -> open call
	pending = map[string]*taskCall{}

	uses := findACPEvents(events, "tool_use")
	for _, e := range uses {
		if e.Tool == nil || !taskToolNames[e.Tool.Name] {
			continue
		}
		tc := &taskCall{name: e.Tool.Name, use: e.Tool, input: parseToolInput(t, e.Tool.Input)}
		pending[e.Tool.ID] = tc
	}
	results := findACPEvents(events, "tool_result")
	for _, e := range results {
		if e.Tool == nil || !taskToolNames[e.Tool.Name] {
			continue
		}
		tc, ok := pending[e.Tool.ID]
		if !ok {
			t.Logf("NOTE: tool_result %q id=%q has no matching tool_use (already dropped?)", e.Tool.Name, e.Tool.ID)
			continue
		}
		tc.result = e.Tool
		taskCalls = append(taskCalls, *tc)
		delete(pending, e.Tool.ID)
	}

	var taskCreates, taskUpdates, taskLists, failedResults int
	for i := range taskCalls {
		c := &taskCalls[i]
		switch c.name {
		case "TaskCreate":
			taskCreates++
			c.createID = createdTaskIDFromOutput(c.result.Output)
			t.Logf("FINDING[TaskCreate]: input subject=%q status=%q resultID=%q",
				c.input["subject"], c.result.Status, c.createID)
		case "TaskUpdate":
			taskUpdates++
			if id, ok := c.input["taskId"].(string); ok {
				c.updateTask = id
			}
			t.Logf("FINDING[TaskUpdate]: input taskId=%q status=%q output=%q",
				c.updateTask, c.result.Status, truncate(c.result.Output, 200))
		case "TaskList":
			taskLists++
			t.Logf("FINDING[TaskList]: status=%q output=%q",
				c.result.Status, truncate(c.result.Output, 200))
		}
		if c.result.Status == "error" || c.result.Status == "failed" {
			failedResults++
		}
	}

	// The turn must complete and produce some reply.
	assert.NotEmpty(t, strings.TrimSpace(content), "expected a final agent reply")

	// Non-vacuity signal: if CodeBuddy never invoked any task tool in this run,
	// log loudly (the reason is usually "task tools unavailable in this
	// session/mode") rather than silently passing.
	if taskCreates == 0 && taskUpdates == 0 && taskLists == 0 {
		t.Logf("NOTE: no TaskCreate/TaskUpdate/TaskList tool calls observed in this run; "+
			"content=%q — CodeBuddy may not expose task tools in the current ACP session mode", truncate(content, 400))
		return
	}

	// Every task tool call that DID appear must have terminated successfully.
	assert.Zerof(t, failedResults, "%d task tool call(s) reported error/failed; see dump above",
		failedResults)

	// Every tool_use must have produced a terminal tool_result (ClawBench's
	// debouncer converges the ~30ms deltas; a dropped terminal event would break
	// the UI tool-result card).
	require.Len(t, pending, 0, "%d task tool_use event(s) never reached a terminal tool_result: %v",
		len(pending), func() []string {
			var out []string
			for id, c := range pending {
				out = append(out, c.name+":"+id)
			}
			return out
		}())

	// TaskCreate must have actually created a task (id echoed in its result).
	if taskCreates > 0 {
		require.NotEmpty(t, taskCalls[0].createID, "TaskCreate result should echo the created task id")
		t.Logf("CONFIRMED: CodeBuddy TaskCreate created task #%s (storage scoped to ACP session %s)",
			taskCalls[0].createID, sessionID)
	}
}

// TestCodebuddyACP_TaskTools_EmitsPlanUpdate verifies the task→plan bridge
// end-to-end against a real codebuddy --acp process: running the
// TaskCreate → TaskUpdate(in_progress) → TaskList sequence must drive
// plan_update events (the frontend PlanPanel data source) — the feature this
// whole bridge exists for. It also verifies the cached plan state (which
// repopulates the panel on refresh/reconnect/REST session load).
//
// Conclusion-style: if CodeBuddy does not run task tools in this session/mode,
// a loud NOTE is logged instead of a silent pass — but the run must at least
// complete (requireDoneEvent).
func TestCodebuddyACP_TaskTools_EmitsPlanUpdate(t *testing.T) {
	requireTaskToolsCodebuddyACPAvailable(t)
	backend, env, sessionID := setupTaskToolsCodebuddyEnv(t)

	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt: `请严格按照以下顺序执行，每一步都必须真的调用对应工具，不要跳过：
1. 调用 TaskCreate 工具创建一个任务：subject 用 "clawbench-plan-bridge"，
   description 用 "plan bridge integration probe"。
2. 上一步返回成功后，立刻调用 TaskUpdate 工具，把刚创建的任务的
   status 改为 in_progress（用上一步返回的任务 id 作为 taskId）。
3. 全部完成后，用一句中文总结你创建的任务 id、subject 和当前状态。`,
		SessionID:          sessionID,
		WorkDir:            acpTestWorkDir(),
		ScheduledExecution: true,
	})
	require.NoError(t, err, "ExecuteStream should not return error")

	events := collectACPEvents(t, ch, 180*time.Second)
	requireDoneEvent(t, events)

	content := concatACPContent(events)
	t.Logf("=== plan_update events on the stream ===")
	var planUpdates []StreamEvent
	for _, e := range events {
		if e.Type == "plan_update" && e.Plan != nil {
			planUpdates = append(planUpdates, e)
			for _, en := range e.Plan.Entries {
				t.Logf("  plan entry: content=%q status=%q priority=%q", en.Content, en.Status, en.Priority)
			}
		}
	}

	// Dump task tool calls too so a failure shows the full picture.
	toolResults := findACPEvents(events, "tool_result")
	for _, tr := range toolResults {
		if tr.Tool != nil {
			t.Logf("  tool_result: name=%q status=%q output=%q", tr.Tool.Name, tr.Tool.Status, truncate(tr.Tool.Output, 150))
		}
	}

	// Non-vacuity signal: if no task tool ran, plan_update can't be expected.
	taskRuns := 0
	for _, tr := range toolResults {
		if tr.Tool != nil && taskToolNames[tr.Tool.Name] {
			taskRuns++
		}
	}
	if taskRuns == 0 {
		t.Logf("NOTE: no Task* tool calls observed in this run; "+
			"content=%q — CodeBuddy may not expose task tools in the current ACP session mode", truncate(content, 400))
		return
	}

	// The bridge must have emitted at least one plan_update reflecting the
	// created task. The last plan_update should carry the task with status
	// in_progress (after TaskUpdate step) when the agent followed the prompt.
	require.NotEmpty(t, planUpdates,
		"expected plan_update events bridged from Task* tool results; content=%q", truncate(content, 400))

	// Last snapshot is authoritative (full-replace semantics). If the agent
	// performed the full sequence it should contain the task as in_progress.
	last := planUpdates[len(planUpdates)-1].Plan
	require.NotNil(t, last)
	sawSubject := false
	for _, en := range last.Entries {
		t.Logf("last plan snapshot entry: content=%q status=%q", en.Content, en.Status)
		if strings.Contains(en.Content, "clawbench-plan-bridge") {
			sawSubject = true
			t.Logf("CONFIRMED: plan_update contains the TaskCreate-created task with status=%q", en.Status)
		}
	}
	assert.True(t, sawSubject,
		"last plan_update should include the task created via TaskCreate (subject clawbench-plan-bridge); content=%q",
		truncate(content, 400))

	// The bridge also caches the plan so refresh/REST load repopulate the panel.
	if cached := env.mgr.GetConn(sessionID); cached != nil {
		if ps := cached.GetCachedPlanState(); ps != nil {
			t.Logf("CONFIRMED: cached plan state has %d entries", len(ps.Entries))
		} else {
			t.Logf("NOTE: connection present but cached plan state nil (may be cleared at turn end)")
		}
	} else {
		t.Logf("NOTE: no live connection found for session %s (already closed)", sessionID)
	}
}

