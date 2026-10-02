//go:build integration

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// CodeBuddy ACP Agent Teams — 完整功能探针 + 集成分析
// ===========================================================================
//
// 目的：在决定为 ClawBench 接入 CodeBuddy 的 Agent Teams 之前，用一个**完整的
// 集成探针**把该功能彻底跑一遍：建队 → 成员干活 → 成员审批 → 成员间通信 →
// 关闭成员 → 删除团队 → loadSession 恢复。每一步都 dump 真实 wire 载荷，并对照
// ClawBench 当前的映射产出（StreamEvent），据此给出集成与展示方案。
//
// 与既有探针的分工：
//   - codebuddy_acp_ext_probe_integration_test.go —— steer / 裸 RPC 通道
//   - codebuddy_task_wire_probe_integration_test.go —— Task* 工具
//   - 本文件 —— Agent Teams 全生命周期
//
// 复用的 harness（均在同包 integration 构建标签下）：
//   - acpRawTraffic / recordingWriter / recordingReader / parseWireMessages
//     （codebuddy_acp_metadata_integration_test.go）
//   - contextWithTimeout / acpTestWorkDir / OrphanChildEnvVar
//   - drainProbeStream / mustJSON（codebuddy_acp_ext_probe_integration_test.go）
//
// 运行：
//
//	go test -v -run 'TestCodebuddyACP_TeamMode_FullProbe' -tags integration \
//	    -timeout 900s ./internal/ai/
//
// 需要本机安装 codebuddy CLI 且已登录。

// ---------------------------------------------------------------------------
// Probe client: captures notifications + permission requests, controls outcomes
// ---------------------------------------------------------------------------

// teamProbeClient wraps the real ClawBenchACPClient so the probe can observe
// the typed notifications and the *raw* requestPermission payloads (which the
// SessionNotification stream does not carry). Permission outcomes are
// controlled by the probe so a turn never stalls waiting for a human.
type teamProbeClient struct {
	*ClawBenchACPClient

	mu     sync.Mutex
	notifs []acp.SessionNotification
	perms  []acp.RequestPermissionRequest

	// permResponder returns the outcome for a permission request. When nil,
	// every request is allowed once (so the probe keeps moving).
	permResponder func(acp.RequestPermissionRequest) acp.RequestPermissionResponse
}

func (c *teamProbeClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	c.mu.Lock()
	c.notifs = append(c.notifs, n)
	c.mu.Unlock()
	return c.ClawBenchACPClient.SessionUpdate(ctx, n)
}

func (c *teamProbeClient) RequestPermission(ctx context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	c.mu.Lock()
	c.perms = append(c.perms, p)
	responder := c.permResponder
	c.mu.Unlock()

	if responder != nil {
		return responder(p), nil
	}
	// Default: allow once, so the probe never blocks on a human.
	for _, opt := range p.Options {
		if opt.Kind == acp.PermissionOptionKindAllowOnce || opt.Kind == acp.PermissionOptionKindAllowAlways {
			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeSelected(opt.OptionId),
			}, nil
		}
	}
	return acp.RequestPermissionResponse{
		Outcome: acp.NewRequestPermissionOutcomeCancelled(),
	}, nil
}

func (c *teamProbeClient) notifications() []acp.SessionNotification {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]acp.SessionNotification, len(c.notifs))
	copy(out, c.notifs)
	return out
}

func (c *teamProbeClient) permissionRequests() []acp.RequestPermissionRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]acp.RequestPermissionRequest, len(c.perms))
	copy(out, c.perms)
	return out
}

func (c *teamProbeClient) reset() {
	c.mu.Lock()
	c.notifs = nil
	c.perms = nil
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

type teamProbeSetup struct {
	conn    *acp.ClientSideConnection
	client  *teamProbeClient
	writer  *recordingWriter
	reader  *recordingReader
	session string

	streamCh chan StreamEvent
	events   []StreamEvent
	evMu     sync.Mutex
	evDone   chan struct{}

	configOptions []acp.SessionConfigOption
}

// modeOptions returns the available mode ids advertised by session/new.
func (s *teamProbeSetup) modeOptions() []string {
	var out []string
	for _, opt := range s.configOptions {
		if opt.Select == nil {
			continue
		}
		if opt.Select.Category == nil || *opt.Select.Category != acp.SessionConfigOptionCategoryMode {
			continue
		}
		if opt.Select.Options.Ungrouped != nil {
			for _, v := range *opt.Select.Options.Ungrouped {
				out = append(out, string(v.Value))
			}
		}
	}
	return out
}

// startTeamProbe spawns a real codebuddy --acp process and completes
// initialize + session/new. It registers a stream sink so ClawBench's own
// mapping output is observable alongside the raw wire.
func startTeamProbe(t *testing.T, ctx context.Context) *teamProbeSetup {
	t.Helper()

	cmd := exec.CommandContext(ctx, "codebuddy", "--acp")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	agentOut, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	agentIn, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	cmd.Stderr = nil // keep test output readable

	recOut := &recordingReader{src: agentOut, buf: &bytes.Buffer{}}
	recIn := &recordingWriter{dst: agentIn, buf: &bytes.Buffer{}}

	base := NewClawBenchACPClient()
	client := &teamProbeClient{ClawBenchACPClient: base}
	conn := acp.NewClientSideConnection(client, recIn, recOut)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, cmd.Start(), "spawn codebuddy --acp")
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	setup := &teamProbeSetup{
		conn:     conn,
		client:   client,
		writer:   recIn,
		reader:   recOut,
		streamCh: make(chan StreamEvent, 8192),
		evDone:   make(chan struct{}),
	}
	setup.startEventPump()

	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-team-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	workDir := acpTestWorkDir()
	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: workDir, McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")
	setup.session = string(newResp.SessionId)
	setup.configOptions = newResp.ConfigOptions

	setup.client.RegisterSession(setup.session, setup.streamCh)
	return setup
}

func (s *teamProbeSetup) startEventPump() {
	go func() {
		for {
			select {
			case <-s.evDone:
				return
			case ev := <-s.streamCh:
				s.evMu.Lock()
				s.events = append(s.events, ev)
				s.evMu.Unlock()
			}
		}
	}()
}

func (s *teamProbeSetup) stopEventPump() {
	select {
	case <-s.evDone:
	default:
		close(s.evDone)
	}
}

func (s *teamProbeSetup) streamEvents() []StreamEvent {
	s.evMu.Lock()
	defer s.evMu.Unlock()
	out := make([]StreamEvent, len(s.events))
	copy(out, s.events)
	return out
}

// prompt runs one turn to completion (or timeout) and returns the response.
func (s *teamProbeSetup) prompt(ctx context.Context, timeout time.Duration, text string) (acp.PromptResponse, error) {
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.conn.Prompt(turnCtx, acp.PromptRequest{
		SessionId: acp.SessionId(s.session),
		Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
	})
}

// loadSession replays the current session and returns the replayed
// notifications (captured via the client during the replay window).
func (s *teamProbeSetup) loadSession(ctx context.Context, timeout time.Duration) (acp.LoadSessionResponse, []acp.SessionNotification, error) {
	s.client.reset()
	loadCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := s.conn.LoadSession(loadCtx, acp.LoadSessionRequest{
		SessionId:  acp.SessionId(s.session),
		Cwd:        acpTestWorkDir(),
		McpServers: []acp.McpServer{},
	})
	// Give late replay notifications a moment to land.
	time.Sleep(2 * time.Second)
	return resp, s.client.notifications(), err
}

// ---------------------------------------------------------------------------
// Findings
// ---------------------------------------------------------------------------

type teamFindings struct {
	teamUpdateTypes  map[string]int
	teamUpdates      []map[string]any
	memberEvents     map[string]int
	memberEventKinds map[string]map[string]int
	unsolicited      []map[string]any
	teamKeys         map[string]int
	permFrames       []map[string]any
	permRaw          []acp.RequestPermissionRequest
	streamTypes      map[string]int
	memberNames      map[string]bool
	historyItemIDs   []string
	toolNames        map[string]int

	// spawnLinks records the Agent spawn tool calls that created team members:
	// (toolCallId, memberName, subagentType). This is the only place the
	// member name and a tool-call id co-occur on the wire.
	spawnLinks []spawnLink
	// memberFrameParent counts member-tagged frames that ALSO carry
	// parentToolCallId. If this is zero, ClawBench's existing parent-link
	// grouping cannot route member content.
	memberFrameParent int
	memberFrameTotal  int
}

type spawnLink struct {
	ToolCallID   string
	MemberName   string
	SubagentType string
}

func newTeamFindings() *teamFindings {
	return &teamFindings{
		teamUpdateTypes:  map[string]int{},
		memberEvents:     map[string]int{},
		memberEventKinds: map[string]map[string]int{},
		teamKeys:         map[string]int{},
		streamTypes:      map[string]int{},
		memberNames:      map[string]bool{},
		toolNames:        map[string]int{},
	}
}

func (f *teamFindings) scan(t *testing.T, notifications []acp.SessionNotification, perms []acp.RequestPermissionRequest, events []StreamEvent) {
	t.Helper()
	seenSnapshot := map[string]bool{}

	for _, n := range notifications {
		if meta := sessionUpdateMeta(n); meta != nil {
			for k := range meta {
				if strings.HasPrefix(k, "codebuddy.ai/") {
					f.teamKeys[k]++
				}
			}
			if ut, ok := meta["codebuddy.ai/unsolicitedTurn"].(map[string]any); ok {
				f.unsolicited = append(f.unsolicited, ut)
			}
			if name, ok := meta["codebuddy.ai/memberEvent"].(string); ok && name != "" {
				f.memberEvents[name]++
				f.memberNames[name] = true
				f.memberFrameTotal++
				if _, hasParent := meta["codebuddy.ai/parentToolCallId"]; hasParent {
					f.memberFrameParent++
				}
				kind := sessionUpdateKind(n)
				if f.memberEventKinds[name] == nil {
					f.memberEventKinds[name] = map[string]int{}
				}
				f.memberEventKinds[name][kind]++
			}
			if hid, ok := meta["codebuddy.ai/memberHistoryItemId"].(string); ok && hid != "" {
				f.historyItemIDs = append(f.historyItemIDs, hid)
			}
		}

		if tu := teamUpdatePayload(n); tu != nil {
			typ, _ := tu["type"].(string)
			f.teamUpdateTypes[typ]++
			snap, _ := json.Marshal(tu)
			if !seenSnapshot[string(snap)] {
				seenSnapshot[string(snap)] = true
				f.teamUpdates = append(f.teamUpdates, tu)
			}
		}
	}

	for _, p := range perms {
		f.permRaw = append(f.permRaw, p)
		if len(p.Meta) > 0 {
			f.permFrames = append(f.permFrames, p.Meta)
		}
		// The team attribution may ride on the toolCall update instead.
		if len(p.ToolCall.Meta) > 0 {
			f.permFrames = append(f.permFrames, p.ToolCall.Meta)
		}
	}

	for _, e := range events {
		f.streamTypes[e.Type]++
		if e.Tool != nil && e.Tool.Name != "" {
			f.toolNames[e.Tool.Name]++
		}
	}
	for _, n := range notifications {
		if n.Update.ToolCall != nil {
			if name, ok := n.Update.ToolCall.Meta["codebuddy.ai/toolName"].(string); ok && name != "" {
				f.toolNames["meta:"+name]++
			}
			f.noteSpawn(n.Update.ToolCall.Meta, string(n.Update.ToolCall.ToolCallId))
		}
		if n.Update.ToolCallUpdate != nil {
			if name, ok := n.Update.ToolCallUpdate.Meta["codebuddy.ai/toolName"].(string); ok && name != "" {
				f.toolNames["meta:"+name]++
			}
			f.noteSpawn(n.Update.ToolCallUpdate.Meta, string(n.Update.ToolCallUpdate.ToolCallId))
		}
	}
}

// noteSpawn records an Agent spawn tool call (the frame whose meta carries
// both memberName and subagentType).
func (f *teamFindings) noteSpawn(meta map[string]any, toolCallID string) {
	if meta == nil {
		return
	}
	name, _ := meta["codebuddy.ai/memberName"].(string)
	sub, _ := meta["codebuddy.ai/subagentType"].(string)
	if name == "" || sub == "" {
		return
	}
	f.spawnLinks = append(f.spawnLinks, spawnLink{ToolCallID: toolCallID, MemberName: name, SubagentType: sub})
}

func (f *teamFindings) dump(t *testing.T) {
	t.Helper()

	t.Log("=== R1: teamUpdate types ===")
	if len(f.teamUpdateTypes) == 0 {
		t.Log("  <none>")
	}
	for _, typ := range sortedIntKeys(f.teamUpdateTypes) {
		t.Logf("  %s x%d", typ, f.teamUpdateTypes[typ])
	}

	t.Log("=== R1: teamUpdate payloads (deduped) ===")
	for i, tu := range f.teamUpdates {
		t.Logf("  [%d] %s", i, compactJSON(mustMarshal(t, tu)))
	}

	t.Log("=== R2: memberEvent tags ===")
	if len(f.memberEvents) == 0 {
		t.Log("  <none>")
	}
	for _, name := range sortedIntKeys(f.memberEvents) {
		t.Logf("  member=%q frames=%d kinds=%v", name, f.memberEvents[name], f.memberEventKinds[name])
	}

	t.Log("=== R3: unsolicitedTurn payloads ===")
	if len(f.unsolicited) == 0 {
		t.Log("  <none>")
	}
	for i, ut := range f.unsolicited {
		t.Logf("  [%d] %s", i, compactJSON(mustMarshal(t, ut)))
	}

	t.Log("=== R4: codebuddy.ai/* keys on session/update _meta ===")
	for _, k := range sortedIntKeys(f.teamKeys) {
		t.Logf("  %s x%d", k, f.teamKeys[k])
	}

	t.Log("=== R5: requestPermission payloads ===")
	if len(f.permRaw) == 0 {
		t.Log("  <none observed>")
	}
	for i, p := range f.permRaw {
		t.Logf("  [%d] session=%s toolCall=%s title=%v", i, p.SessionId, p.ToolCall.ToolCallId, p.ToolCall.Title)
		t.Logf("      request._meta   = %s", compactJSON(mustMarshal(t, p.Meta)))
		t.Logf("      toolCall._meta  = %s", compactJSON(mustMarshal(t, p.ToolCall.Meta)))
	}

	t.Log("=== R6: ClawBench StreamEvent types produced by current mapping ===")
	for _, k := range sortedIntKeys(f.streamTypes) {
		t.Logf("  %s x%d", k, f.streamTypes[k])
	}

	t.Logf("=== R7: memberHistoryItemId samples (%d total) ===", len(f.historyItemIDs))
	for i, h := range f.historyItemIDs {
		if i >= 5 {
			t.Logf("  ... (%d more)", len(f.historyItemIDs)-i)
			break
		}
		t.Logf("  [%d] %s", i, h)
	}

	t.Log("=== R8: tool names (stream + meta) ===")
	if len(f.toolNames) == 0 {
		t.Log("  <none>")
	}
	for _, k := range sortedIntKeys(f.toolNames) {
		t.Logf("  %s x%d", k, f.toolNames[k])
	}

	t.Log("=== R9: member-name ↔ tool-call correlation ===")
	t.Logf("  Agent spawn frames (memberName+subagentType): %d", len(f.spawnLinks))
	for i, s := range f.spawnLinks {
		t.Logf("  [%d] toolCallId=%s member=%s subagentType=%s", i, s.ToolCallID, s.MemberName, s.SubagentType)
	}
	t.Logf("  member-tagged frames: %d, carrying parentToolCallId: %d",
		f.memberFrameTotal, f.memberFrameParent)
	if f.memberFrameTotal > 0 && f.memberFrameParent == 0 {
		t.Log("  >>> IMPLICATION: member content is NOT linked by tool-call id.")
		t.Log("  >>> ClawBench's existing parent_tool_call_id grouping (acp_parent_link.go)")
		t.Log("  >>> cannot route it; the only join key is the member NAME.")
	}
}

func sortedIntKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// teamUpdatePayload extracts the codebuddy.ai/teamUpdate object from a
// session_info_update notification, or nil when absent.
func teamUpdatePayload(n acp.SessionNotification) map[string]any {
	if n.Update.SessionInfoUpdate == nil {
		return nil
	}
	meta := n.Update.SessionInfoUpdate.Meta
	if meta == nil {
		return nil
	}
	tu, _ := meta["codebuddy.ai/teamUpdate"].(map[string]any)
	return tu
}

// sessionUpdateMeta returns the _meta of whichever variant this notification
// carries, or nil when the variant has none.
func sessionUpdateMeta(n acp.SessionNotification) map[string]any {
	u := n.Update
	switch {
	case u.SessionInfoUpdate != nil:
		return u.SessionInfoUpdate.Meta
	case u.AgentMessageChunk != nil:
		return u.AgentMessageChunk.Meta
	case u.AgentThoughtChunk != nil:
		return u.AgentThoughtChunk.Meta
	case u.ToolCall != nil:
		return u.ToolCall.Meta
	case u.ToolCallUpdate != nil:
		return u.ToolCallUpdate.Meta
	case u.UserMessageChunk != nil:
		return u.UserMessageChunk.Meta
	case u.UsageUpdate != nil:
		return u.UsageUpdate.Meta
	default:
		return nil
	}
}

// sessionUpdateKind returns the JSON sessionUpdate discriminator for logging.
func sessionUpdateKind(n acp.SessionNotification) string {
	u := n.Update
	switch {
	case u.SessionInfoUpdate != nil:
		return "session_info_update"
	case u.AgentMessageChunk != nil:
		return "agent_message_chunk"
	case u.AgentThoughtChunk != nil:
		return "agent_thought_chunk"
	case u.ToolCall != nil:
		return "tool_call"
	case u.ToolCallUpdate != nil:
		return "tool_call_update"
	case u.UserMessageChunk != nil:
		return "user_message_chunk"
	case u.UsageUpdate != nil:
		return "usage_update"
	case u.AvailableCommandsUpdate != nil:
		return "available_commands_update"
	case u.CurrentModeUpdate != nil:
		return "current_mode_update"
	case u.ConfigOptionUpdate != nil:
		return "config_option_update"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

// teamProbeCreatePrompt creates an explicit, persistent team. Auto-teams
// (formed implicitly by a named Agent() call) are torn down when the lead's
// turn ends, so every later phase needs an explicit TeamCreate first.
const teamProbeCreatePrompt = `请调用 TeamCreate 工具创建一个团队，team_name 参数用 "clawbench-probe"。
创建完成后只回复一句"团队已创建"。`

// teamProbeSpawnPrompt populates the existing team with two members.
const teamProbeSpawnPrompt = `当前团队是 "clawbench-probe"。请严格按顺序执行：
1. 用 Agent 工具生成两个团队成员，参数 name 分别为 "probe-alpha" 和 "probe-beta"。
   - probe-alpha 的 prompt：先用 Bash 工具运行 ` + "`echo TEAMPROBE-ALPHA`" + `，然后用一句话报告输出。
   - probe-beta 的 prompt：用一句话说明 2+2 等于几。
2. 等待两个成员都返回。
3. 用一句中文汇总两个成员的结论。`

// teamProbeMessagingPrompt exercises member-to-member messaging + shutdown.
const teamProbeMessagingPrompt = `当前团队是 "clawbench-probe"，成员有 probe-alpha 和 probe-beta。请严格按顺序执行：
1. 用 SendMessage 工具给 probe-alpha 发一条消息（to 参数用 "probe-alpha"），内容是："请用一句话复述你的结论"。
2. 等 probe-alpha 回复。
3. 用 SendMessage 向 probe-beta 发送 shutdown_request（type 参数用 "shutdown_request"），请求它关闭。
4. 用一句中文报告你对 probe-alpha 的请求结果和 probe-beta 的关闭响应。`

// teamProbeCleanupPrompt asks the lead to delete the team.
const teamProbeCleanupPrompt = `请调用 TeamDelete 工具删除当前团队，team_name 参数用 "clawbench-probe"。
完成后用一句中文确认。`

// TestCodebuddyACP_TeamMode_FullProbe drives the whole Agent Teams feature
// against a real codebuddy --acp process and dumps every observable signal.
func TestCodebuddyACP_TeamMode_FullProbe(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 900*time.Second)
	defer cancel()

	setup := startTeamProbe(t, ctx)
	defer setup.stopEventPump()

	// Run one phase: reset capture, run the prompt, snapshot findings.
	runPhase := func(name, prompt string, timeout time.Duration) *teamFindings {
		t.Logf("################ PHASE: %s ################", name)
		setup.client.reset()
		prevEvents := len(setup.streamEvents())
		resp, err := setup.prompt(ctx, timeout, prompt)
		if err != nil {
			t.Logf("NOTE: %s prompt error: %v", name, err)
		}
		t.Logf("%s stopReason=%s", name, resp.StopReason)
		time.Sleep(3 * time.Second)
		f := newTeamFindings()
		f.scan(t, setup.client.notifications(), setup.client.permissionRequests(), setup.streamEvents()[prevEvents:])
		f.dump(t)
		return f
	}

	phases := map[string]*teamFindings{}
	phases["create"] = runPhase("create", teamProbeCreatePrompt, 180*time.Second)
	phases["spawn"] = runPhase("spawn", teamProbeSpawnPrompt, 300*time.Second)
	phases["messaging"] = runPhase("messaging", teamProbeMessagingPrompt, 300*time.Second)
	phases["cleanup"] = runPhase("cleanup", teamProbeCleanupPrompt, 180*time.Second)

	// ---- loadSession replay ----
	t.Log("################ PHASE: loadSession replay ################")
	_, replayNotifs, err4 := setup.loadSession(ctx, 120*time.Second)
	if err4 != nil {
		t.Logf("NOTE: loadSession error: %v", err4)
	}
	f4 := newTeamFindings()
	f4.scan(t, replayNotifs, nil, nil)
	t.Logf("replay notifications: %d", len(replayNotifs))
	f4.dump(t)
	phases["replay"] = f4

	// ---- Aggregate conclusion ----
	t.Log("################ CONCLUSION ################")
	agg := newTeamFindings()
	for _, f := range phases {
		for k, v := range f.teamUpdateTypes {
			agg.teamUpdateTypes[k] += v
		}
		for k := range f.memberNames {
			agg.memberNames[k] = true
		}
		for k, v := range f.teamKeys {
			agg.teamKeys[k] += v
		}
	}
	t.Logf("teamUpdate types across all phases: %v", sortedIntKeys(agg.teamUpdateTypes))
	t.Logf("members seen: %v", sortedBoolKeys(agg.memberNames))
	t.Logf("team meta keys seen: %v", sortedIntKeys(agg.teamKeys))

	if agg.teamUpdateTypes["team_created"] == 0 && agg.teamUpdateTypes["member_status_change"] == 0 {
		t.Logf("NOTE: no team events observed at all — the model may not have formed a team; " +
			"see per-phase dumps before concluding the protocol is absent")
		return
	}
	t.Logf("CONFIRMED: Agent Teams protocol is live over stdio (types=%v, members=%v)",
		sortedIntKeys(agg.teamUpdateTypes), sortedBoolKeys(agg.memberNames))
}

func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// teamProbeBackgroundPrompt asks the lead to dispatch a detached worker and
// end the turn immediately, so the coordinator runs a background_drain turn.
const teamProbeBackgroundPrompt = `请调用 Agent 工具生成一个后台工作者：name 用 "probe-worker"，
run_in_background 设为 true，prompt 是："先执行 ` + "`sleep 8`" + `，然后写一句话说明你完成了"。
关键：生成后**立刻结束你的回合**，不要等待这个后台工作者，也不要再调用任何工具。
只回复一句"已派出后台工作者"。`

// TestCodebuddyACP_TeamMode_UnsolicitedDrain probes the non-user-initiated
// drain turn: a detached worker finishes while the parent session is idle, so
// the coordinator re-enters with a CLI-assigned requestId and no user bubble.
func TestCodebuddyACP_TeamMode_UnsolicitedDrain(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 420*time.Second)
	defer cancel()

	setup := startTeamProbe(t, ctx)
	defer setup.stopEventPump()

	setup.client.reset()
	promptResp, err := setup.prompt(ctx, 180*time.Second, teamProbeBackgroundPrompt)
	if err != nil {
		t.Logf("NOTE: background prompt error: %v", err)
	}
	t.Logf("dispatch stopReason=%s", promptResp.StopReason)

	// Wait for the detached worker to finish and the drain turn to run.
	t.Log("waiting up to 90s for background_drain turn...")
	deadline := time.After(90 * time.Second)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	var sawUnsolicited bool
waitLoop:
	for {
		select {
		case <-deadline:
			break waitLoop
		case <-tick.C:
			for _, n := range setup.client.notifications() {
				meta := sessionUpdateMeta(n)
				if meta == nil {
					continue
				}
				if _, ok := meta["codebuddy.ai/unsolicitedTurn"]; ok {
					sawUnsolicited = true
					break waitLoop
				}
			}
		}
	}

	f := newTeamFindings()
	f.scan(t, setup.client.notifications(), setup.client.permissionRequests(), setup.streamEvents())
	f.dump(t)

	if !sawUnsolicited {
		t.Logf("NOTE: no unsolicitedTurn observed — the worker may not have been detached, " +
			"or the drain turn did not run. See dumps above.")
		return
	}
	t.Logf("CONFIRMED: unsolicitedTurn (background_drain) observed over stdio")
}

// teamProbePermissionPrompt asks a team member to run a command that must be
// approved, so a session/request_permission carrying member attribution meta
// reaches the client. The probe does NOT auto-approve — it records the raw
// request (including _meta) and then rejects, so the turn still terminates.
const teamProbePermissionPrompt = `请严格按顺序执行：
1. 用 Agent 工具生成一个团队成员，name 用 "probe-perm"，prompt 是：
   "请用 Bash 工具运行命令 ` + "`rm -f /tmp/clawbench-team-probe-nonexistent`" + `，然后报告结果。"
2. 等待该成员返回（无论成功或失败）。
3. 用一句中文说明结果。`

// TestCodebuddyACP_TeamMode_MemberPermission probes the one remaining unknown:
// whether a member's session/request_permission carries team attribution in
// its _meta (isTeamMember / memberName / agentColor), which the approval card
// needs to show which member is asking.
func TestCodebuddyACP_TeamMode_MemberPermission(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 420*time.Second)
	defer cancel()

	setup := startTeamProbe(t, ctx)
	defer setup.stopEventPump()

	// Record every permission request, but do not blanket-approve: respond with
	// the FIRST option so the turn never stalls, while keeping the raw payload.
	setup.client.mu.Lock()
	setup.client.permResponder = func(p acp.RequestPermissionRequest) acp.RequestPermissionResponse {
		if len(p.Options) > 0 {
			return acp.RequestPermissionResponse{
				Outcome: acp.NewRequestPermissionOutcomeSelected(p.Options[0].OptionId),
			}
		}
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}
	}
	setup.client.mu.Unlock()

	// Force an asking mode so the member's Bash call requires approval. Team
	// members run with full tool access by default (observed: 0 permission
	// requests under the default mode), so this is required to exercise the
	// request_permission path at all.
	modes := setup.modeOptions()
	t.Logf("available modes: %v", modes)
	askingMode := ""
	for _, m := range modes {
		if m == "default" || m == "ask" || m == "plan" {
			askingMode = m
			break
		}
	}
	if askingMode != "" {
		modeCtx, modeCancel := context.WithTimeout(ctx, 30*time.Second)
		if _, err := setup.conn.SetSessionMode(modeCtx, acp.SetSessionModeRequest{
			SessionId: acp.SessionId(setup.session),
			ModeId:    acp.SessionModeId(askingMode),
		}); err != nil {
			t.Logf("NOTE: SetSessionMode(%q) failed: %v", askingMode, err)
		} else {
			t.Logf("set session mode to %q to force an approval ask", askingMode)
		}
		modeCancel()
	} else {
		t.Logf("NOTE: no asking mode among %v — member tools may run unapproved", modes)
	}

	setup.client.reset()
	promptResp, err := setup.prompt(ctx, 300*time.Second, teamProbePermissionPrompt)
	if err != nil {
		t.Logf("NOTE: permission prompt error: %v", err)
	}
	t.Logf("stopReason=%s", promptResp.StopReason)
	time.Sleep(3 * time.Second)

	f := newTeamFindings()
	f.scan(t, setup.client.notifications(), setup.client.permissionRequests(), setup.streamEvents())
	f.dump(t)

	perms := setup.client.permissionRequests()
	t.Logf("permission requests observed: %d", len(perms))
	sawTeamMeta := false
	for i, p := range perms {
		t.Logf("perm[%d] tool=%s title=%v", i, p.ToolCall.ToolCallId, p.ToolCall.Title)
		t.Logf("  request._meta  = %s", compactJSON(mustMarshal(t, p.Meta)))
		t.Logf("  toolCall._meta = %s", compactJSON(mustMarshal(t, p.ToolCall.Meta)))
		for _, m := range []map[string]any{p.Meta, p.ToolCall.Meta} {
			if m == nil {
				continue
			}
			if m["codebuddy.ai/isTeamMember"] == true || m["codebuddy.ai/memberName"] != nil {
				sawTeamMeta = true
			}
		}
	}
	if len(perms) == 0 {
		t.Logf("NOTE: no permission request observed — the member's command may not have " +
			"required approval under the current permission mode")
		return
	}
	if !sawTeamMeta {
		t.Logf("NEGATIVE: permission frames carried no team attribution meta " +
			"(isTeamMember/memberName absent) — approval cards cannot be attributed to a member")
		return
	}
	t.Logf("CONFIRMED: permission frames carry team attribution meta")
}
