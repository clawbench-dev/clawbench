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
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// ACP `_session/steering` 预研探针（claude / codex）
// ===========================================================================
//
// 目的：在决定是否把"运行中插话（mid-turn injection）"从 CodeBuddy 扩展到
// claude / codex 之前，用真实适配器进程实测 `_session/steering` 的请求/响应/回执
// 形状。全部为**观测型**探针 —— 事实写入 t.Logf，硬断言只覆盖"必须成立才能接入"
// 的前提（响应可达、非 method-not-found）。
//
// 背景：issue #339 提出用 ACP steering 实现 `/ps`（把消息注入在途回合）。
// 与 CodeBuddy 的 `session/steer`（无 `_` 前缀，只能走裸通道，返回
// {steered, ownerRequestId}）不同，claude-agent-acp / codex-acp 走的是带 `_`
// 前缀的 `_session/steering`（返回 {outcome}）。本探针把差异钉死。
//
// 关键问题（Q1–Q5）：
//
//	Q1  轮中调用 `_session/steering` 是否返回？响应形状如何？
//	Q2  空闲时调用返回什么？
//	    - claude：传 _meta.steering.idleBehavior=promptRequired 是否返回
//	      promptRequired（而非静默起新轮）？不传呢？
//	    - codex：无该选项，空闲必起新轮？
//	Q3  适配器是否回显被注入的消息？回显帧（user_message_chunk）带什么 id？
//	Q4  回显 id 能否与宿主关联？（claude/codex 的请求体都不接受
//	    clientUserMessageId —— 这决定能否做"前后分段"）
//	Q5  回显帧在时间线上是否夹在前段/后段内容之间？
//
// 运行：
//
//	go test -tags integration -v -run 'TestACPSteeringProbe' \
//	    -timeout 900s ./internal/ai/
//
// 需要本机安装 claude / codex 且已登录。

// steeringProbeBackend 描述一个待探测的桥接适配器。
type steeringProbeBackend struct {
	ID   string
	Args []string // spawn 命令 + 参数
	CLI  string   // 底层 CLI（可用性检查）
	// SendIdleBehavior 决定是否在请求里附 _meta.steering.idleBehavior=promptRequired。
	// claude 支持；codex 的解析器无此字段（仅透传 sessionId/prompt）。
	SendIdleBehavior bool
}

var steeringProbeBackends = []steeringProbeBackend{
	{
		ID:               "claude",
		Args:             []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp@latest"},
		CLI:              "claude",
		SendIdleBehavior: true,
	},
	{
		ID:               "codex",
		Args:             []string{"npx", "-y", "@agentclientprotocol/codex-acp@latest"},
		CLI:              "codex",
		SendIdleBehavior: false,
	},
}

// steeringProbeParams 构造 `_session/steering` 请求体。
//
// 注意：claude/codex 的解析器只读 sessionId / prompt（claude 另读
// _meta.steering.idleBehavior），**不接受** clientUserMessageId。这里仍附上
// clientUserMessageId，用来实证"它是否被忽略"（Q4 的核心）。
func steeringProbeParams(sessionID, text, sentID string, idleBehavior bool) map[string]any {
	p := map[string]any{
		"sessionId": sessionID,
		"prompt":    []map[string]any{{"type": "text", "text": text}},
	}
	if sentID != "" {
		p["clientUserMessageId"] = sentID
	}
	if idleBehavior {
		p["_meta"] = map[string]any{
			"steering": map[string]any{"idleBehavior": "promptRequired"},
		}
	}
	return p
}

// steeringProbeOutcome 从响应里提取 outcome（claude/codex 的形状）。
func steeringProbeOutcome(resp map[string]any) (string, string) {
	res, ok := resp["result"].(map[string]any)
	if !ok {
		return "", ""
	}
	outcome, _ := res["outcome"].(string)
	reason, _ := res["reason"].(string)
	return outcome, reason
}

// startSteeringProbe 启动一个桥接适配器并完成 initialize + new_session。
// 复用 codebuddy 探针的裸 JSON-RPC 通道（syncWriter/demuxReader/extProbeRawConn）。
func startSteeringProbe(t *testing.T, ctx context.Context, args []string, clientName string) *extProbeSetup {
	t.Helper()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	cmd.Stderr = nil // keep test output readable

	writer := &syncWriter{dst: stdinPipe, buf: &bytes.Buffer{}}
	reader := &demuxReader{src: stdoutPipe}

	client := NewClawBenchACPClient()
	conn := acp.NewClientSideConnection(client, writer, reader)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, cmd.Start(), "spawn %s", strings.Join(args, " "))
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	raw := &extProbeRawConn{pending: map[string]chan map[string]any{}}
	setup := &extProbeSetup{conn: conn, client: client, raw: raw, writer: writer, reader: reader}
	setup.pumpProbeResponses(ctx)

	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{Name: clientName, Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	// 确认 agent 广告了 steering 能力（typed struct 会丢弃 _meta，故看原始 wire）。
	t.Logf("initialize wire lines mentioning steering:")
	logWireLinesContaining(t, reader.snapshot(), "steering")

	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: acpTestWorkDir(), McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")
	setup.session = string(newResp.SessionId)

	return setup
}

// logWireLinesContaining 打印原始 wire 里含指定子串的行（用于核对 _meta 等
// typed 结构看不到的字段）。
func logWireLinesContaining(t *testing.T, wire []byte, substr string) {
	t.Helper()
	found := false
	for _, line := range strings.Split(string(wire), "\n") {
		if strings.Contains(line, substr) {
			t.Logf("  wire: %s", truncate(line, 800))
			found = true
		}
	}
	if !found {
		t.Logf("  (no wire line contains %q)", substr)
	}
}

// TestACPSteeringProbe 是入口：逐后端跑"轮中注入 + 空闲行为"两个阶段。
func TestACPSteeringProbe(t *testing.T) {
	for _, b := range steeringProbeBackends {
		b := b
		t.Run(b.ID, func(t *testing.T) {
			if _, err := exec.LookPath("npx"); err != nil {
				t.Skip("npx not available")
			}
			if _, err := exec.LookPath(b.CLI); err != nil {
				t.Skipf("%s CLI not available", b.CLI)
			}

			ctx, cancel := contextWithTimeout(t, 480*time.Second)
			defer cancel()

			setup := startSteeringProbe(t, ctx, b.Args, "clawbench-steering-probe")

			SetAutoApproveGetter(func(_ string) bool { return true })
			t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

			streamCh := make(chan StreamEvent, 8192)
			registerProbeSession(t, setup, streamCh)

			// 独立采集器：记录每一帧及其相对 steer 时刻的偏移。
			rec := &steerBoundaryRecorder{}
			go rec.pump(ctx, setup.reader)

			runSteeringMidTurnPhase(t, ctx, setup, rec, streamCh, b)
			runSteeringIdlePhase(t, ctx, setup, b)
		})
	}
}

// runSteeringMidTurnPhase 发起一个长回合，在轮中发 `_session/steering`，
// 观测响应 / 回执 / 时间线。
func runSteeringMidTurnPhase(t *testing.T, ctx context.Context, setup *extProbeSetup, rec *steerBoundaryRecorder, streamCh chan StreamEvent, b steeringProbeBackend) {
	t.Helper()
	t.Log("──────── PHASE 1: mid-turn steering ────────")

	promptDone := make(chan struct{})
	var promptResp acp.PromptResponse
	var promptErr error
	go func() {
		promptResp, promptErr = setup.conn.Prompt(ctx, acp.PromptRequest{
			SessionId: acp.SessionId(setup.session),
			Prompt: []acp.ContentBlock{acp.TextBlock(
				"请分三步，每步之间必须真的调用 Bash 工具执行命令：" +
					"1) 运行 `sleep 3` 并说明这是第一步；" +
					"2) 运行 `sleep 3` 并说明这是第二步；" +
					"3) 运行 `sleep 3` 并说明这是第三步。")},
		})
		close(promptDone)
	}()

	// 等本轮真正开始（前段已产出内容）再 steer，确保落在轮中。
	select {
	case <-promptDone:
		t.Log("NOTE: prompt finished before we could steer — too fast; Q1–Q5 for the " +
			"mid-turn case cannot be observed this run")
		require.NoError(t, promptErr, "prompt error")
		return
	case <-time.After(5 * time.Second):
	}

	const steerMarker = "CLAWBENCH-STEERING-PROBE-MARKER"
	const sentID = "clawbench-steering-probe-1"

	rec.markSteer()
	steerStart := time.Now()
	steerCtx, steerCancel := context.WithTimeout(ctx, 30*time.Second)
	defer steerCancel()

	resp, err := setup.raw.callRaw(steerCtx, setup.writer, "_session/steering",
		steeringProbeParams(setup.session,
			"请立刻回复这句话，一个字都不要多："+steerMarker, sentID, b.SendIdleBehavior))
	steerElapsed := time.Since(steerStart)
	require.NoError(t, err, "Q1 FAILED: no response to _session/steering while a prompt was in flight — "+
		"the method is either blocked behind the prompt or not dispatched")

	body, _ := json.Marshal(resp)
	t.Logf("Q1 mid-turn response (%.2fs): %s", steerElapsed.Seconds(), body)

	if code, msg, isErr := extractJSONRPCError(resp); isErr {
		if code == -32601 {
			t.Fatalf("Q1 FAILED: _session/steering returned Method not found (-32601); response=%s", body)
		}
		t.Logf("Q1 NOTE: JSON-RPC error code=%v msg=%q (method exists but rejected this request shape)", code, msg)
	} else {
		outcome, reason := steeringProbeOutcome(resp)
		t.Logf("Q1 outcome=%q reason=%q (%.2fs — response arrived DURING the prompt, "+
			"not after it, iff elapsed << turn length)", outcome, reason, steerElapsed.Seconds())
	}

	// 等本轮结束。
	select {
	case <-promptDone:
		require.NoError(t, promptErr, "prompt must complete cleanly after steering")
		t.Logf("Q1 turn completed; stopReason=%q", promptResp.StopReason)
	case <-time.After(240 * time.Second):
		t.Fatal("prompt did not complete within 240s after steering")
	}
	time.Sleep(500 * time.Millisecond) // 给采集器最后一个 tick 收尾

	analyzeSteeringEcho(t, rec.framesSince(), sentID, steerMarker, streamCh)
}

// runSteeringIdlePhase 在回合结束后发 `_session/steering`，观测空闲行为（Q2）。
func runSteeringIdlePhase(t *testing.T, ctx context.Context, setup *extProbeSetup, b steeringProbeBackend) {
	t.Helper()
	t.Log("──────── PHASE 2: idle steering ────────")

	// 等结算窗口过去，确保会话真的空闲（cancel/结算竞态见记忆）。
	time.Sleep(3 * time.Second)

	// (a) 带 idleBehavior=promptRequired（claude 支持；codex 忽略该字段）。
	idleCtx, idleCancel := context.WithTimeout(ctx, 30*time.Second)
	defer idleCancel()
	resp, err := setup.raw.callRaw(idleCtx, setup.writer, "_session/steering",
		steeringProbeParams(setup.session,
			"Reply with exactly: CLAWBENCH-IDLE-A", "clawbench-steering-idle-a", true))
	require.NoError(t, err, "Q2a FAILED: no response to idle _session/steering")
	body, _ := json.Marshal(resp)
	if code, msg, isErr := extractJSONRPCError(resp); isErr {
		t.Logf("Q2a (idleBehavior=promptRequired) error code=%v msg=%q raw=%s", code, msg, body)
	} else {
		outcome, reason := steeringProbeOutcome(resp)
		t.Logf("Q2a (idleBehavior=promptRequired) outcome=%q reason=%q raw=%s", outcome, reason, body)
	}

	// (b) 不带 idleBehavior —— 记录"静默起新轮"的危险行为（claude/codex 都可能）。
	time.Sleep(2 * time.Second)
	idleCtx2, idleCancel2 := context.WithTimeout(ctx, 30*time.Second)
	defer idleCancel2()
	resp2, err := setup.raw.callRaw(idleCtx2, setup.writer, "_session/steering",
		steeringProbeParams(setup.session,
			"Reply with exactly: CLAWBENCH-IDLE-B", "clawbench-steering-idle-b", false))
	if err != nil {
		t.Logf("Q2b (no idleBehavior) no response: %v", err)
	} else {
		body2, _ := json.Marshal(resp2)
		if code, msg, isErr := extractJSONRPCError(resp2); isErr {
			t.Logf("Q2b (no idleBehavior) error code=%v msg=%q raw=%s", code, msg, body2)
		} else {
			outcome, reason := steeringProbeOutcome(resp2)
			t.Logf("Q2b (no idleBehavior) outcome=%q reason=%q raw=%s "+
				"(startedNewTurn = adapter detached a turn the host does not own)", outcome, reason, body2)
		}
	}
	_ = b
}

// analyzeSteeringEcho 从采集到的帧里回答 Q3/Q4/Q5。
func analyzeSteeringEcho(t *testing.T, frames []steerBoundaryFrame, sentID, steerMarker string, streamCh chan StreamEvent) {
	t.Helper()

	var updates []string
	var echoFrames []steerBoundaryFrame
	seenTypes := map[string]int{}
	for _, f := range frames {
		if f.Kind != "session_update" {
			continue
		}
		updates = append(updates, f.Update)
		seenTypes[f.Update]++
		if f.Update == "user_message_chunk" {
			echoFrames = append(echoFrames, f)
		}
	}
	t.Logf("Q3 sessionUpdate sequence (%d frames): %s", len(updates), strings.Join(updates, " → "))
	t.Logf("Q3 sessionUpdate type counts: %v", seenTypes)

	if len(echoFrames) == 0 {
		t.Logf("Q3/Q4 NO user_message_chunk echo observed — the injected text did not come back " +
			"as an echo frame, so a host CANNOT use it as a split boundary.")
	} else {
		t.Logf("Q3 OBSERVED %d user_message_chunk frame(s):", len(echoFrames))
		for i, f := range echoFrames {
			var probe struct {
				Params struct {
					Update struct {
						SessionUpdate string         `json:"sessionUpdate"`
						MessageID     string         `json:"messageId"`
						Meta          map[string]any `json:"_meta"`
						Content       map[string]any `json:"content"`
					} `json:"update"`
				} `json:"params"`
			}
			if err := json.Unmarshal([]byte(f.Detail), &probe); err != nil {
				t.Logf("    [%d] at steer%+.2fs parse failed: %v", i, f.At.Seconds(), err)
				continue
			}
			u := probe.Params.Update
			metaKeySet := map[string]bool{}
			collectKeys(u.Meta, metaKeySet, 0)
			t.Logf("    [%d] at steer%+.2fs messageId=%q content.type=%v _meta.keys=%v",
				i, f.At.Seconds(), u.MessageID, u.Content["type"], metaKeySet)

			// Q4：回显 id 能否与宿主发送的 id 关联？
			matchesSent := u.MessageID == sentID
			metaHasSent := false
			for k, v := range u.Meta {
				if s, ok := v.(string); ok && s == sentID {
					metaHasSent = true
					t.Logf("    [%d] _meta[%q] == sentID", i, k)
				}
			}
			t.Logf("    [%d] Q4 CORRELATION: messageId==sentID(%q)=%v meta-carries-sentID=%v",
				i, sentID, matchesSent, metaHasSent)
			if !matchesSent && !metaHasSent {
				t.Logf("    [%d] Q4 => echo id is ADAPTER-GENERATED and NOT correlated with our request; "+
					"host cannot map it back to the queued message by id", i)
			}
		}
	}

	// Q5：回显帧是否夹在前段/后段内容之间（分段可行的充分条件）。
	boundaryIdx := -1
	for i, f := range frames {
		if f.Kind == "session_update" && f.Update == "user_message_chunk" {
			boundaryIdx = i
			break
		}
	}
	if boundaryIdx < 0 {
		t.Logf("Q5 NO boundary frame — cannot assess interleaving")
	} else {
		beforeContent, afterContent := 0, 0
		for i := 0; i < boundaryIdx; i++ {
			if frames[i].Kind == "session_update" && frames[i].Update == "agent_message_chunk" {
				beforeContent++
			}
		}
		for i := boundaryIdx + 1; i < len(frames); i++ {
			if frames[i].Kind == "session_update" && frames[i].Update == "agent_message_chunk" {
				afterContent++
			}
		}
		t.Logf("Q5 DECISIVE: boundary at index %d/%d; agent_message_chunk before=%d after=%d",
			boundaryIdx, len(frames), beforeContent, afterContent)
		if beforeContent > 0 && afterContent > 0 {
			t.Logf("Q5 *** SPLIT IS FEASIBLE *** — the echo frame is interleaved between part-1 " +
				"and part-2 content chunks.")
		} else {
			t.Logf("Q5 SPLIT NOT SUPPORTED by this run (content on one side only: before=%d after=%d)",
				beforeContent, afterContent)
		}

		lo := boundaryIdx - 6
		if lo < 0 {
			lo = 0
		}
		hi := boundaryIdx + 7
		if hi > len(frames) {
			hi = len(frames)
		}
		t.Logf("Q5 boundary neighbourhood (index: kind/update @ steer offset):")
		for i := lo; i < hi; i++ {
			marker := "  "
			if i == boundaryIdx {
				marker = ">>"
			}
			t.Logf("    %s [%d] %s/%s @ steer%+.2fs", marker, i, frames[i].Kind, frames[i].Update, frames[i].At.Seconds())
		}
	}

	// marker 是否出现在输出里（证明注入真的影响了模型输出）。
	events := drainProbeStream(streamCh, 3*time.Second)
	content := concatACPContent(events)
	t.Logf("Q1 steer marker present in final output: %v", strings.Contains(content, steerMarker))
	if !strings.Contains(content, steerMarker) {
		t.Logf("NOTE: marker absent — the model may have ignored the injected instruction. " +
			"This does NOT disprove steering; outcome=injected is the authoritative signal.")
	}
}
