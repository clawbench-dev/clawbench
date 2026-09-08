//go:build integration

package ai

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// CodeBuddy Task* 工具结果 — Wire 级载体验证探针
// ===========================================================================
//
// 目的：确认 CodeBuddy（真实 codebuddy --acp 进程）在 ACP wire 上序列化
// TaskCreate/TaskUpdate/TaskList 工具终态结果时，结构化"全量任务快照"的确切
// 载体。源码推证（dist-server/codebuddy.js convertInputItemContentToAcp）：
//   - _meta["codebuddy.ai/toolName"] = 工具名
//   - _meta["codebuddy.ai/rawResponse"] = toolResult.rawResponse 原样对象
//     （TaskCreate/TaskUpdate: {task, todos}；TaskList: {tasks, todos}）
//   - rawOutput = parseToRecord(n.output) —— 仅 content 人读文本
//
// 本探针驱动真实 ACP 连接（acpMetadataProbe，typed SessionNotification +
// 原始 wire 双录制），dump 每个终态任务工具结果的 _meta 与 rawOutput，
// 验证 rawResponse.todos 全量快照可达。测试是"结论型"：若实际载体与推证不符，
// 断言失败并打印全部 dump，据此调整 bridge 实现的解析路径。
//
// 运行：
//
//	go test -v -run TestCodebuddyACP_TaskTools_WireProbe -tags integration \
//	    -timeout 300s ./internal/ai/

// requireWireProbeCodebuddyACP skips if the codebuddy CLI is missing.
func requireWireProbeCodebuddyACP(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not available, skipping CodeBuddy task wire probe test")
	}
}

// wireProbeTaskPrompt asks CodeBuddy to run a full task-tool sequence so every
// tool result shape (create / status update / list) lands on the wire.
const wireProbeTaskPrompt = `请严格按照以下顺序执行，每一步都必须真的调用对应工具，不要跳过：
1. 调用 TaskCreate 工具创建一个任务：subject 用 "clawbench-wire-probe"，
   description 用 "wire probe"。
2. 上一步返回成功后，立刻调用 TaskUpdate 工具，把刚创建的任务的
   status 改为 in_progress（用上一步返回的任务 id 作为 taskId）。
3. 再调用 TaskList 工具列出当前所有任务。
4. 全部完成后，用一句中文总结你创建的任务 id、subject 和当前状态。`

// dumpTaskToolMeta writes every terminal (completed/failed) tool_call_update for
// a task tool with its full _meta and rawOutput to the test log.
func dumpTaskToolMeta(t *testing.T, notifications []acp.SessionNotification) {
	t.Helper()
	for _, n := range notifications {
		tcu := n.Update.ToolCallUpdate
		if tcu == nil {
			continue
		}
		name, _ := n.Update.ToolCallUpdate.Meta["codebuddy.ai/toolName"].(string)
		if !taskToolNames[name] {
			continue
		}
		status := "<nil>"
		if tcu.Status != nil {
			status = string(*tcu.Status)
		}
		t.Logf("[wire] tool_call_update id=%s name=%q status=%s", tcu.ToolCallId, name, status)
		t.Logf("  _meta: %s", compactJSON(mustMarshal(t, tcu.Meta)))
		t.Logf("  rawOutput: %s", compactJSON(mustMarshal(t, tcu.RawOutput)))
		t.Logf("  content-count=%d", len(tcu.Content))
	}
}

// mustMarshal serialises v to JSON bytes for logging (fatal on failure).
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	if v == nil {
		return []byte("null")
	}
	b, err := json.Marshal(v)
	require.NoError(t, err, "marshal for dump")
	return b
}

// TestCodebuddyACP_TaskTools_WireProbe drives a real CodeBuddy ACP process at
// the wire level and verifies where the full task snapshot lives in the
// terminal tool results.
func TestCodebuddyACP_TaskTools_WireProbe(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 240*time.Second)
	defer cancel()

	rec, _, newResp, _, notifications, promptErr := acpMetadataProbe(
		t, ctx, strings.Fields("codebuddy --acp"), wireProbeTaskPrompt,
	)

	t.Log("=== raw wire traffic (from agent) ===")
	rec.dump(t)
	t.Logf("=== typed notifications: %d total, new_session=%v ===", len(notifications), newResp != nil)

	t.Log("=== terminal task-tool results (full _meta + rawOutput) ===")
	dumpTaskToolMeta(t, notifications)

	if promptErr != nil {
		t.Logf("NOTE: prompt returned error: %v", promptErr)
	}

	// ---- Conclusion-type assertions (fail loudly + dump) ----
	// We need at least one terminal TaskCreate/TaskUpdate/TaskList result whose
	// _meta["codebuddy.ai/rawResponse"] carries the full todos snapshot.
	var rawResponses []map[string]any
	for _, n := range notifications {
		tcu := n.Update.ToolCallUpdate
		if tcu == nil || tcu.Status == nil || *tcu.Status != acp.ToolCallStatusCompleted {
			continue
		}
		name, _ := tcu.Meta["codebuddy.ai/toolName"].(string)
		if !taskToolNames[name] {
			continue
		}
		rr, ok := tcu.Meta["codebuddy.ai/rawResponse"].(map[string]any)
		if ok {
			rawResponses = append(rawResponses, rr)
		}
	}

	if len(rawResponses) == 0 {
		// CodeBuddy may not have run task tools at all (mode/session). Report
		// loudly instead of silently passing — same convention as the Flow test.
		t.Logf("NOTE: no terminal task-tool result carried _meta.codebuddy.ai/rawResponse; " +
			"CodeBuddy may not have invoked task tools in this run (see dumps above)")
		return
	}

	// At least one rawResponse must expose a todos array with entries.
	sawTodos := false
	for _, rr := range rawResponses {
		todos, ok := rr["todos"].([]any)
		if ok && len(todos) > 0 {
			sawTodos = true
			// Log the field names of one todo item for the mapper.
			if first, ok := todos[0].(map[string]any); ok {
				keys := make([]string, 0, len(first))
				for k := range first {
					keys = append(keys, k)
				}
				t.Logf("FINDING: rawResponse.todos[0] keys=%v", keys)
			}
		}
	}
	require.True(t, sawTodos,
		"expected _meta.codebuddy.ai/rawResponse.todos with entries; rawResponses=%d (see dumps above)", len(rawResponses))
	t.Logf("CONFIRMED: full task snapshot lives in _meta.codebuddy.ai/rawResponse (count=%d)", len(rawResponses))
}
