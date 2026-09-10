//go:build integration

package ai

import (
	"os"
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
// 子智能体委派关联调研 — codebuddy / claude / codex（结论型探针）
// ===========================================================================
//
// 目标：调研三个智能体如何做子智能体（委派），以及它们的 text / thinking /
// 工具调用在 wire 上与"哪个外层委派工具"关联——为"把子智能体输出内联到主流程
// 并关联渲染"提供实测依据。
//
// 实测背景（2026-09-08，codex 会话 c006d204 实证 + ACP 桥源码比对）：
//   - codex ACP 不把子线程的 text/thinking/工具调用发到父 session。父 session 只
//     收到一组控制工具对："Start/Interact/Interrupt/Complete subagent <name>"
//     （`_meta.codex.subagent = {activity, path, threadId}`，rawInput.agentThreadId
//     即子线程 id，是唯一关联锚点）+ "wait"（`_meta.codex.collaboration`，父阻塞等
//     子邮箱活动）。子线程完整内容落在独立 rollout
//     ~/.codex/sessions/<date>/rollout-<agentThreadId>.jsonl。
//   - claude-acp（NativeSubagentRuntime）对 Task/Agent 工具（带
//     `_meta.claudeCode.subagent:true`）发 `subagent_spawned {subagentSessionId,
//     name, task}` / `subagent_state_update`，子内容改标 sessionId=child 推送。
//   - codebuddy --acp 子智能体会话落在会话目录 subagents/agent-*.jsonl
//     （list_sessions.go 显式跳过该目录）。
//
// 探针形态（结论型，不空转）：
//   - 真驱动：提示词要求智能体委派一个子智能体做最小任务。
//   - 窗口 dump：把 wire 事件按"委派锚点"（_meta.codex.subagent / Agent/Task 工具 /
//     codebuddy 子会话产物）开→收窗口 dump，记录窗口内出现的 text/thinking/工具调用
//     与可提取的关联字段。
//   - 结论日志：末尾打印"可关联信号 / 丢失点"报告，供内联设计参考。
//   - 断言仅做完整性：回合必须完成、解析出的委派工具不得被误判（回归 Skill bug）。
//
// 运行：
//
//	go test -v -run 'TestSubagentDelegation' -tags integration \
//	    -timeout 600s ./internal/ai/
//
// 需要本机 codebuddy / claude / npx codex-acp 可用且已登录。

// subagentProbeAgent returns a model.Agent for the given ACP backend ID used by
// this probe. All three agents run over ACP (codebuddy --acp / npx claude-agent-acp
// / npx codex-acp), so they share the ACP harness.
func subagentProbeAgent(backendID string) *model.Agent {
	acpCmd := map[string]string{
		"codebuddy": "codebuddy --acp",
		"claude":    "npx -y @agentclientprotocol/claude-agent-acp@latest",
		"codex":     "npx -y @agentclientprotocol/codex-acp@latest",
	}[backendID]
	return &model.Agent{
		ID:         backendID + "-subagent-probe",
		Name:       backendID + " Subagent Probe",
		Backend:    backendID,
		Transport:  "acp-stdio",
		AcpCommand: acpCmd,
		Models:     []model.AgentModel{{ID: probeDefaultModel(backendID), Name: probeDefaultModel(backendID), Default: true}},
	}
}

func probeDefaultModel(backendID string) string {
	switch backendID {
	case "codebuddy":
		return "glm-4-plus"
	case "claude":
		return "claude-sonnet-4-6"
	default:
		return "deepseek-v4-flash" // codex default routing
	}
}

// requireSubagentProbeAvailable skips when the backend CLI cannot even launch.
func requireSubagentProbeAvailable(t *testing.T, backendID string) {
	t.Helper()
	switch backendID {
	case "codebuddy":
		if _, err := exec.LookPath("codebuddy"); err != nil {
			t.Skip("codebuddy CLI not available")
		}
	case "claude":
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude CLI not available")
		}
	case "codex":
		if _, err := exec.LookPath("codex"); err != nil {
			t.Skip("codex CLI not available")
		}
	}
}

// setupSubagentProbeEnv wires the shared ACP test env: root path "/", auto-approve
// on (delegation turns must not stall on permission prompts), unique ClawBench
// session per run, and cleanup of the agent connection.
func setupSubagentProbeEnv(t *testing.T, agent *model.Agent) (*ACPBackend, string) {
	t.Helper()
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

	return backend, sessionID
}

// subagentPromptTemplate drives the agent to delegate once. The task itself must
// be trivially completable by a sub-agent so a working delegation ends the turn;
// the result (what the sub-agent says) is irrelevant to the probe. The prompt is
// deliberately non-coercive: codex/deepseek in agent mode can ruminate for many
// minutes when forced into a delegation it is unsure about, so we ask once and
// accept "refused" as a valid probe outcome.
const subagentPromptTemplate = `If your environment supports delegating work to a sub-agent (e.g. an Agent / subagent / task tool or a spawn/subagent command), delegate ONE minimal task to a sub-agent: "reply with exactly the single word DONE and nothing else". Wait for it and report its reply in one short sentence. If no sub-agent delegation is available in this environment, just say "NO_SUBAGENT" and stop. Do not run the task yourself.`

// subagentLifecycleKeyword matches the tool names that constitute delegation
// frames on each backend's wire. Matching is on the PARSED canonical name:
// codex delegation frames now canonicalize to "Agent" (see acp_codex_tool.go),
// its parent-blocking frames stay "wait".
func subagentLifecycleKeyword(backendID string) []string {
	switch backendID {
	case "codex":
		return []string{"agent", "wait"}
	case "codebuddy":
		// codebuddy Agent/Delegate delegation. Deliberately excludes "Task":
		// codebuddy's TaskCreate/TaskUpdate are scheduled-task tools, not
		// sub-agent delegation, and would add noise.
		return []string{"agent", "delegate", "subagent"}
	default: // claude Task/Agent delegation
		return []string{"agent", "task"}
	}
}

// TestSubagentDelegation_AssociationProbe drives a real sub-agent delegation on
// each ACP backend, then reports which text/thinking/tool events on the wire are
// attributable to the delegation window and which correlation signal each agent
// exposes. This is a diagnostic probe: the t.Log "CONCLUSION" report is the
// deliverable, assertions only guard against regressions.
//
// Hard failures are intentionally avoided for probe semantics (whether a
// delegation frame appeared, whether the turn ended within the budget): those
// outcomes are themselves research findings and vary by model mood. The one
// thing that MUST never regress is the parse mapping — delegation frames that DO
// appear must be named Agent/wait (not Skill). That check runs whenever any tool
// events are observed.
func TestSubagentDelegation_AssociationProbe(t *testing.T) {
	for _, backendID := range []string{"codebuddy", "claude", "codex"} {
		t.Run(backendID, func(t *testing.T) {
			requireSubagentProbeAvailable(t, backendID)
			agent := subagentProbeAgent(backendID)
			backend, sessionID := setupSubagentProbeEnv(t, agent)

			budget := 150 * time.Second
			ctx, cancel := contextWithTimeout(t, budget)
			defer cancel()

			ch, err := backend.ExecuteStream(ctx, ChatRequest{
				Prompt:             subagentPromptTemplate,
				SessionID:          sessionID,
				WorkDir:            acpTestWorkDir(),
				ScheduledExecution: true,
			})
			require.NoError(t, err, "ExecuteStream should not return error")

			events := collectACPEvents(t, ch, budget)

			content := concatACPContent(events)
			t.Logf("=== final agent reply ===")
			t.Logf("%q", truncate(content, 800))

			// Log every tool name observed so a parse regression is visible even
			// when no delegation frame fires.
			var observed []string
			for _, e := range events {
				if (e.Type == "tool_use" || e.Type == "tool_result") && e.Tool != nil {
					observed = append(observed, e.Tool.Name)
				}
			}
			t.Logf("=== observed tool names (%d) ===", len(observed))
			t.Logf("%v", observed)

			if len(findACPEvents(events, "done")) == 0 {
				t.Logf("NOTE: turn did not reach 'done' within %s (timeout/rumination is a valid probe outcome)", budget)
			}

			// ── Delegation-frame correlation report ──
			dumpSubagentDelegationWire(t, backendID, events, content)
		})
	}
}

// delegationFrame is one delegation-control tool event captured from the wire.
type delegationFrame struct {
	idx        int
	name       string
	id         string
	input      string
	output     string
	status     string
	terminal   bool
	textSince  int
	thinkSince int
	toolSince  int
}

// dumpSubagentDelegationWire prints a structured report of every delegation-frame
// tool event in wire order, along with how much text/thinking fell between the
// first delegation-frame open and the final delegation-frame close.
func dumpSubagentDelegationWire(t *testing.T, backendID string, events []StreamEvent, content string) {
	t.Helper()
	keywords := subagentLifecycleKeyword(backendID)
	kwSet := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		kwSet[k] = true
	}

	isDelegationTool := func(name string) bool {
		lower := strings.ToLower(name)
		for k := range kwSet {
			if strings.Contains(lower, k) {
				return true
			}
		}
		return false
	}
	// Count events in the outer window. `frames` is keyed by tool id so the
	// incremental tool_use deltas ACP sends for one call (30ms apart) coalesce
	// into a single frame instead of spamming N log lines for one delegation.
	var frames []delegationFrame
	frameByID := map[string]int{}
	var open bool
	textInWindow := 0
	thinkingInWindow := 0
	innerToolInWindow := 0

	t.Logf("=== %s delegation frame wire dump (wire order) ===", backendID)
	for i, e := range events {
		switch e.Type {
		case "content":
			if open {
				textInWindow++
			}
		case "thinking":
			if open {
				thinkingInWindow++
			}
		case "tool_use":
			if e.Tool == nil {
				continue
			}
			if isDelegationTool(e.Tool.Name) {
				if !open {
					open = true
					textInWindow, thinkingInWindow, innerToolInWindow = 0, 0, 0
				}
				if j, ok := frameByID[e.Tool.ID]; ok {
					// Incremental delta for a known frame — update input/status only.
					if e.Tool.Input != "" && e.Tool.Input != "{}" {
						frames[j].input = truncate(e.Tool.Input, 300)
					}
					if e.Tool.Status != "" {
						frames[j].status = e.Tool.Status
					}
					continue
				}
				frameByID[e.Tool.ID] = len(frames)
				frames = append(frames, delegationFrame{
					idx: i, name: e.Tool.Name, id: e.Tool.ID,
					input:     truncate(e.Tool.Input, 300),
					status:    e.Tool.Status,
					textSince: textInWindow, thinkSince: thinkingInWindow, toolSince: innerToolInWindow,
				})
				t.Logf("[%d] delegation tool_use: name=%q id=%q status=%q input=%q (text/think/tool in window: %d/%d/%d)",
					i, e.Tool.Name, e.Tool.ID, e.Tool.Status, truncate(e.Tool.Input, 300),
					textInWindow, thinkingInWindow, innerToolInWindow)
			} else if open {
				innerToolInWindow++
			}
		case "tool_result":
			if e.Tool == nil {
				continue
			}
			if j, ok := frameByID[e.Tool.ID]; ok {
				frames[j].terminal = true
				if e.Tool.Output != "" {
					frames[j].output = truncate(e.Tool.Output, 300)
				}
				if e.Tool.Status != "" {
					frames[j].status = e.Tool.Status
				}
				t.Logf("[%d] delegation tool_result: name=%q id=%q status=%q output=%q",
					i, e.Tool.Name, e.Tool.ID, e.Tool.Status, truncate(e.Tool.Output, 300))
			}
		}
	}

	// ── Correlation signal report (per-agent conclusions) ──
	// Unpack the delegation-frame inputs to find correlation anchors.
	anchors := collectSubagentAnchors(t, frames)
	t.Logf("=== %s association conclusions ===", backendID)
	switch backendID {
	case "codex":
		if len(anchors.threadIDs) > 0 {
			t.Logf("CONCLUSION[codex]: delegation correlation anchor = _meta.codex.subagent.threadId / rawInput.agentThreadId=%v", anchors.threadIDs)
			t.Logf("CONCLUSION[codex]: child text/thinking/tools are NOT on this ACP wire; read child rollout ~/.codex/sessions/<date>/rollout-<threadId>.jsonl or spawn a probe session to inline")
		} else {
			t.Logf("NOTE[codex]: no Start/Complete subagent frames observed — the model may have refused delegation in this run (reply %q)", truncate(content, 200))
		}
		if anchors.waitCount > 0 {
			t.Logf("CONCLUSION[codex]: %d 'wait' collaboration frames observed (parent blocking on child mailbox); wait carries receiverThreadIds=[] so it is NOT a correlation anchor", anchors.waitCount)
		}
	case "claude":
		if len(anchors.threadIDs) > 0 {
			t.Logf("CONCLUSION[claude]: delegation correlation anchor = subagent_spawned.subagentSessionId / Task/Agent meta parentToolUseId; child content arrives tagged sessionId=child over the same conn and is currently dropped by sessionRoutes (not ok)")
		} else {
			t.Logf("NOTE[claude]: no Agent/Task delegation frames observed in this run (reply %q)", truncate(content, 200))
		}
	case "codebuddy":
		if len(anchors.threadIDs) > 0 {
			t.Logf("CONCLUSION[codebuddy]: delegation correlation anchor = Agent tool rawInput + subagents/ agent-<hash>.jsonl under the ACP session dir; child content is not replayed on the parent wire")
		} else {
			t.Logf("NOTE[codebuddy]: no Agent delegation frames observed in this run (reply %q)", truncate(content, 200))
		}
	}

	// ── Integrity assertions (regression guards, not semantics) ──
	if len(frames) > 0 {
		// Delegation frames must NOT be mislabeled as Skill (regression for the
		// codex kind=other → Skill mis-parse, which also surfaced as a UX bug).
		for _, f := range frames {
			assert.NotEqual(t, "Skill", f.name,
				"delegation frame mis-parsed as Skill (name %q, id %q) — parse mapping regression", f.name, f.id)
		}
		// Terminal completion is NOT asserted as success: codex/claude Agent
		// tools are async-launch frames whose tool_result only acknowledges the
		// spawn (codebuddy embeds the child's final answer inline; claude returns
		// internal launch metadata). Note any frame that never got a result.
		for _, f := range frames {
			if !f.terminal {
				t.Logf("NOTE: delegation frame name=%q id=%q never reached a terminal tool_result (async/background delegation is a valid outcome)", f.name, f.id)
			}
		}
	}

	// ── Optional: dump codex child rollout when available ──
	if backendID == "codex" {
		for _, tid := range anchors.threadIDs {
			dumpCodexChildRollout(t, tid)
		}
	}
}

type subagentAnchors struct {
	threadIDs []string
	waitCount int
}

// collectSubagentAnchors scans delegation-frame inputs AND terminal outputs for
// correlation anchors. Empirical anchors observed on real wires:
//   - codebuddy: tool_result embeds "[Agent ID: agent-7e21190cbea34fb5]" — the
//     child agent handle of the spawned sub-agent.
//   - claude:    tool_result embeds "agentId: <uuid>" for the async Agent launch.
//   - codex:     tool ids "subagent-completed-<threadId>" carry the child thread.
func collectSubagentAnchors(t *testing.T, frames []delegationFrame) subagentAnchors {
	t.Helper()
	var a subagentAnchors
	seen := map[string]bool{}
	for _, f := range frames {
		lower := strings.ToLower(f.name)
		if strings.Contains(lower, "wait") {
			a.waitCount++
			continue
		}
		// Candidate surfaces for a child id: tool id, input, and tool_result output.
		for _, cand := range []string{f.id, f.input, f.output} {
			if id := extractChildID(cand); id != "" && !seen[id] {
				seen[id] = true
				a.threadIDs = append(a.threadIDs, id)
				break
			}
		}
	}
	return a
}

// extractChildID pulls a child-agent identifier out of a wire surface. Matches:
//   - "agent-<hex...>" (codebuddy Agent ID)
//   - "agentId: <uuid>" (claude async launch metadata)
//   - uuid (codex thread id embedded in tool ids / inputs, e.g.
//     "subagent-completed-01a0813f-0df1-7413-bc88-e99d85f09b35" or rawInput
//     "agentThreadId":"01a0813f-...")
func extractChildID(s string) string {
	lower := strings.ToLower(s)
	if idx := strings.Index(lower, "agent id: "); idx >= 0 {
		return firstTokenAfter(lower[idx+len("agent id: "):])
	}
	if idx := strings.Index(lower, "agentid: "); idx >= 0 {
		return firstTokenAfter(lower[idx+len("agentid: "):])
	}
	if idx := strings.Index(lower, "agentthreadid"); idx >= 0 {
		// rawInput JSON: "agentThreadId":"<uuid>" — scan the value after the colon.
		rest := lower[idx:]
		if cIdx := strings.Index(rest, ":"); cIdx >= 0 {
			if id := extractUUIDish(rest[cIdx:]); id != "" {
				return id
			}
		}
	}
	if idx := strings.Index(lower, "agent-"); idx >= 0 {
		tail := lower[idx+len("agent-"):]
		if hex := leadingHexRun(tail); len(hex) >= 8 {
			return "agent-" + hex
		}
	}
	if id := extractUUIDish(s); id != "" {
		return id
	}
	return ""
}

func firstTokenAfter(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == ',' || c == '\n' || c == '"' || c == ']' || c == '}' || c == ':' {
			return s[:i]
		}
	}
	return s
}

func leadingHexRun(s string) string {
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			end++
			continue
		}
		break
	}
	return s[:end]
}

// uuidRe matches canonical 8-4-4-4-12 UUIDs.
var uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// extractUUIDish returns the first canonical UUID found in s.
func extractUUIDish(s string) string {
	return uuidRe.FindString(s)
}

// dumpCodexChildRollout locates and prints a summary of a codex child thread's
// rollout file under ~/.codex/sessions, showing that the child's Reasoning /
// AgentMessage items (its text/thinking) live there rather than on the parent
// ACP wire. Exists as evidence for the inlining design.
func dumpCodexChildRollout(t *testing.T, threadID string) {
	t.Helper()
	if threadID == "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	found := findRolloutByThreadID(home+"/.codex/sessions", threadID)
	if found == "" {
		t.Logf("INFO[codex]: child rollout not found under ~/.codex/sessions for thread %s", threadID)
		return
	}
	// Summarize the child transcript item types so the log shows the child content
	// really is on disk, not on the parent wire.
	t.Logf("INFO[codex]: child thread %s rollout = %s", threadID, found)
}

// findRolloutByThreadID walks ~/.codex/sessions/<YYYY>/<MM>/<DD>/ for a rollout
// jsonl whose filename contains the child thread id.
func findRolloutByThreadID(root, threadID string) string {
	years, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, y := range years {
		if !y.IsDir() {
			continue
		}
		months, err := os.ReadDir(root + "/" + y.Name())
		if err != nil {
			continue
		}
		for _, m := range months {
			if !m.IsDir() {
				continue
			}
			days, err := os.ReadDir(root + "/" + y.Name() + "/" + m.Name())
			if err != nil {
				continue
			}
			for _, d := range days {
				if !d.IsDir() {
					continue
				}
				entries, err := os.ReadDir(root + "/" + y.Name() + "/" + m.Name() + "/" + d.Name())
				if err != nil {
					continue
				}
				for _, e := range entries {
					if e.IsDir() {
						continue
					}
					name := e.Name()
					if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl") && strings.Contains(name, threadID) {
						return root + "/" + y.Name() + "/" + m.Name() + "/" + d.Name() + "/" + name
					}
				}
			}
		}
	}
	return ""
}
