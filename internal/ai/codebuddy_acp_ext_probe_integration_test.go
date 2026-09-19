//go:build integration

package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
// CodeBuddy ACP 私有扩展 — 可行性验证探针
// ===========================================================================
//
// 目的：在决定是否接入 CodeBuddy 的 ACP 非标准扩展之前，先用真实
// `codebuddy --acp` 进程实测三件事是否成立。全部为"结论型"探针 —— 观测到的
// 事实写入 t.Logf，断言只覆盖"必须成立才能接入"的硬前提。
//
// 见 docs/dev/codebuddy_acp_extensions.md（接口清单）。
//
// 验证项：
//
//	P1 非 `_` 前缀方法可经裸 JSON-RPC 发送
//	    acp-go-sdk 的 CallExtension 会拒绝 session/steer（无 `_` 前缀），且
//	    ClientSideConnection.conn 非导出无 accessor。探针自行往 stdin 写一行
//	    JSON-RPC，验证 agent 是否正常应答（而不是报 Method not found）。
//	    → 这是接入 steer / 队列族 / inject_history 的前置条件。
//
//	P2 自建请求 ID 与 SDK 的响应路由互不干扰
//	    SDK 用 atomic uint64（1,2,3…）作 ID，探针用带前缀的字符串 ID。
//	    验证 agent 回给探针的响应不会被 SDK 误吞（SDK 对未知 ID 是静默 no-op），
//	    且探针能收到自己的响应。
//
//	P3 运行中并发发送 steer 不阻塞 / 不干扰在途 prompt
//	    在 session/prompt 尚未返回时，并发发 session/steer，验证：
//	      - 请求在合理时间内得到响应（不排在被 prompt 后面）
//	      - 最终 prompt 正常以 stopReason 收尾（不被 steer 破坏）
//
// 运行：
//
//	go test -v -run 'TestCodebuddyACP_ExtProbe' -tags integration \
//	    -timeout 300s ./internal/ai/
//
// 需要本机安装 codebuddy CLI 且已登录。

// extProbeRawConn 在 acp-go-sdk 之外自建一条"裸 JSON-RPC"通道：
//
//	写：所有出站字节（SDK 的 + 探针的）都经 syncWriter 串行化，避免交错；
//	读：tee 一份原始字节，探针按 id 前缀拣出自己的响应（模拟未来的 demux 层）。
//
// 这正是文档 §8.1 提出的改造方案的等价原型：stdin 包一层写锁、stdout 包一层
// 按 id 前缀分流。
type extProbeRawConn struct {
	mu      sync.Mutex
	nextID  int
	pending map[string]chan map[string]any // id -> response
}

// syncWriter serializes every outbound write through one mutex so the SDK's
// own writes and the probe's raw writes never interleave mid-line.
type syncWriter struct {
	mu  sync.Mutex
	dst io.Writer
	buf *bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	return w.dst.Write(p)
}

// demuxReader tees the agent's stdout: it records raw bytes AND forwards them
// to the SDK. The probe separately scans the recorded bytes for its own
// responses (identified by the "cb-" id prefix).
type demuxReader struct {
	src io.Reader
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *demuxReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.buf.Write(p[:n])
		r.mu.Unlock()
	}
	return n, err
}

func (r *demuxReader) snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, r.buf.Len())
	copy(out, r.buf.Bytes())
	return out
}

// callRaw sends one JSON-RPC request with a "cb-<n>" string id and waits for
// the matching response. This bypasses acp-go-sdk's CallExtension validation
// (which requires a "_" prefix) — the whole point of P1.
func (c *extProbeRawConn) callRaw(ctx context.Context, w io.Writer, method string, params any) (map[string]any, error) {
	c.mu.Lock()
	c.nextID++
	id := fmt.Sprintf("cb-%d", c.nextID)
	ch := make(chan map[string]any, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if _, err := w.Write(b); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for response to %s (id=%s)", method, id)
	}
}

// dispatch scans a chunk of agent output and routes any response whose id
// matches a probe-issued "cb-" id to the waiting caller. Returns the number of
// probe responses consumed.
func (c *extProbeRawConn) dispatch(chunk []byte) int {
	consumed := 0
	sc := bufio.NewScanner(bytes.NewReader(chunk))
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		rawID, ok := m["id"]
		if !ok {
			continue // notification
		}
		id, ok := rawID.(string)
		if !ok || !strings.HasPrefix(id, "cb-") {
			continue // SDK's numeric id — not ours
		}
		c.mu.Lock()
		ch, found := c.pending[id]
		if found {
			delete(c.pending, id)
		}
		c.mu.Unlock()
		if found {
			ch <- m
			consumed++
		}
	}
	return consumed
}

// extProbeSetup spawns a real codebuddy --acp process and wires the raw
// JSON-RPC channel alongside the SDK connection.
type extProbeSetup struct {
	conn    *acp.ClientSideConnection
	client  *ClawBenchACPClient
	raw     *extProbeRawConn
	writer  *syncWriter
	reader  *demuxReader
	session string
}

// pumpProbeResponses runs a goroutine that keeps feeding the probe's dispatcher
// with newly recorded agent output. This simulates the demux layer that a real
// implementation would live inside acpStdoutFilter.
func (s *extProbeSetup) pumpProbeResponses(ctx context.Context) {
	go func() {
		var seen int
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				snap := s.reader.snapshot()
				if len(snap) <= seen {
					continue
				}
				// Scan only the new suffix, but start from the last newline
				// boundary so a partially-written line is not mis-parsed.
				tail := snap[seen:]
				if idx := bytes.LastIndexByte(tail, '\n'); idx >= 0 {
					s.raw.dispatch(tail[:idx+1])
					seen += idx + 1
				}
			}
		}
	}()
}

// startExtProbe launches the agent and performs initialize + new_session.
func startExtProbe(t *testing.T, ctx context.Context, caps acp.ClientCapabilities, clientInfoName string) *extProbeSetup {
	t.Helper()

	cmd := exec.CommandContext(ctx, "codebuddy", "--acp")
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

	require.NoError(t, cmd.Start(), "spawn codebuddy --acp")
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
	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
		ClientInfo:         &acp.Implementation{Name: clientInfoName, Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	// Dump the agent's advertised capabilities. IMPORTANT: dump the RAW wire
	// response, not initResp.AgentCapabilities — acp-go-sdk's typed struct has no
	// fields for CodeBuddy's non-standard capabilities (delegateToolsSupport /
	// mainAgentSupport / multitaskSupport), so json.Unmarshal silently drops them.
	// CRITICAL: verify our capability advertisement actually reached the agent.
	// If acp-go-sdk dropped ClientCapabilities._meta on the way out, every
	// capability-gated extension would silently stay disabled.
	t.Logf("initialize REQUEST (what we advertised): %s", findRawRequest(t, writer.buf.Bytes(), "initialize"))

	capRaw := findRawResponse(t, reader.snapshot(), "agentCapabilities")
	t.Logf("initialize response (RAW WIRE): %s", capRaw)
	typedJSON, _ := json.Marshal(initResp.AgentCapabilities)
	t.Logf("initialize response (SDK typed struct — lossy): %s", typedJSON)
	t.Logf("authMethods: %d", len(initResp.AuthMethods))

	workDir := acpTestWorkDir()
	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: workDir, McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")
	setup.session = string(newResp.SessionId)

	// Dump config options — this is where CodeBuddy advertises the "multitask"
	// config id (the standard-method path to Multitask).
	cfgJSON, _ := json.Marshal(newResp.ConfigOptions)
	t.Logf("session/new configOptions (SDK typed): %s", cfgJSON)
	t.Logf("session/new response (RAW WIRE): %s", findRawResponse(t, reader.snapshot(), "configOptions"))

	return setup
}

// extractJSONRPCError pulls the error object out of a JSON-RPC response.
func extractJSONRPCError(resp map[string]any) (code float64, msg string, has bool) {
	e, ok := resp["error"].(map[string]any)
	if !ok {
		return 0, "", false
	}
	if c, ok := e["code"].(float64); ok {
		code = c
	}
	if m, ok := e["message"].(string); ok {
		msg = m
	}
	return code, msg, true
}

// TestCodebuddyACP_ExtProbe_RawMethodTransport 验证 P1 + P2：
// 非 `_` 前缀方法能否经裸 JSON-RPC 发出并被 agent 正确处理，且自建字符串
// ID 不会与 SDK 的数值 ID 冲突。
//
// 断言策略（结论型）：
//   - 必须能收到 agent 的响应（否则通道不成立 → 接入方案不可行，直接失败）；
//   - 响应**允许**是业务错误（如 session 状态不对），但**不允许**是
//     -32601 Method not found —— 后者说明方法名没被识别，方案要重新评估。
func TestCodebuddyACP_ExtProbe_RawMethodTransport(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	setup := startExtProbe(t, ctx, acp.ClientCapabilities{
		Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		Terminal: true,
	}, "clawbench-ext-probe")

	// P1: send session/set_model via raw JSON-RPC. This method takes a simple
	// {sessionId, modelId} shape and is NOT gated by product features, so it is
	// the cleanest probe of "is a non-underscore custom method reachable?".
	// We intentionally pass a bogus modelId: a business error is fine, a
	// method-not-found is not.
	callCtx, callCancel := context.WithTimeout(ctx, 30*time.Second)
	defer callCancel()

	resp, err := setup.raw.callRaw(callCtx, setup.writer, "session/set_model", map[string]any{
		"sessionId": setup.session,
		"modelId":   "clawbench-ext-probe-nonexistent-model",
	})
	require.NoError(t, err, "P1 FAILED: no response to session/set_model over raw JSON-RPC — "+
		"the raw-transport approach does not work with this agent version")

	code, msg, isErr := extractJSONRPCError(resp)
	body, _ := json.Marshal(resp)
	t.Logf("P1 session/set_model raw response: %s", body)

	if isErr && code == -32601 {
		t.Fatalf("P1 FAILED: agent returned Method not found (-32601) for session/set_model; "+
			"the method name is not dispatched via extMethod. Response: %s", body)
	}
	if isErr {
		t.Logf("P1 CONFIRMED: non-underscore method reached the agent's dispatcher; "+
			"got a business-level JSON-RPC error (code=%v msg=%q) rather than method-not-found. "+
			"This is the expected shape — the transport works.", code, msg)
	} else {
		t.Logf("P1 CONFIRMED: non-underscore method accepted; result=%s", body)
	}

	// P2: verify the SDK did not consume our response. If the SDK had routed it
	// to its own pending map, our channel would never have fired and callRaw
	// would have timed out. Reaching here already proves non-interference; log
	// the id-shape evidence for the record.
	setup.raw.mu.Lock()
	leftover := len(setup.raw.pending)
	setup.raw.mu.Unlock()
	require.Zero(t, leftover, "P2 FAILED: %d probe response(s) never routed back — id collision suspected", leftover)
	t.Logf("P2 CONFIRMED: string ids (\"cb-N\") routed to the probe while SDK uses numeric ids; " +
		"no leftover pending requests")
}

// TestCodebuddyACP_ExtProbe_SteerDuringPrompt 验证 P3：
// 在 session/prompt 在途时并发发送 session/steer，验证它不被 prompt 阻塞、
// 且不破坏 prompt 的正常收尾。
//
// 这是 steer 能否用作"运行中插话"的核心前提。断言策略（结论型）：
//   - prompt 必须在超时内正常结束（不能因 steer 而挂起）；
//   - steer 必须在 prompt 结束前拿到响应（否则它就等价于排队，失去价值）；
//   - steer 响应允许 {steered:false}（业务状态不满足），但不允许 -32601。
func TestCodebuddyACP_ExtProbe_SteerDuringPrompt(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 240*time.Second)
	defer cancel()

	setup := startExtProbe(t, ctx, acp.ClientCapabilities{
		Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		Terminal: true,
	}, "clawbench-ext-probe")

	// Auto-approve so ordinary tool permissions never stall the turn.
	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	streamCh := make(chan StreamEvent, 1024)
	registerProbeSession(t, setup, streamCh)

	// Start a prompt that takes a while (asks the model to do multi-step work)
	// so we have a window to inject a steer while it is in flight.
	promptDone := make(chan struct{})
	var promptResp acp.PromptResponse
	var promptErr error
	go func() {
		promptResp, promptErr = setup.conn.Prompt(ctx, acp.PromptRequest{
			SessionId: acp.SessionId(setup.session),
			Prompt: []acp.ContentBlock{acp.TextBlock(
				"请依次执行：1) 用 Bash 运行 `sleep 3`；2) 用 Bash 运行 `sleep 3`；" +
					"3) 用一句中文总结你执行了几条命令。必须真的调用 Bash 工具。")},
		})
		close(promptDone)
	}()

	// Give the turn a moment to actually start (so steer lands mid-turn rather
	// than before the agent is running).
	select {
	case <-promptDone:
		t.Log("NOTE: prompt finished before we could steer — too fast for this probe")
		require.NoError(t, promptErr, "prompt error")
		return
	case <-time.After(4 * time.Second):
	}

	steerCtx, steerCancel := context.WithTimeout(ctx, 30*time.Second)
	defer steerCancel()

	steerStart := time.Now()
	steerResp, err := setup.raw.callRaw(steerCtx, setup.writer, "session/steer", map[string]any{
		"sessionId": setup.session,
		"contentBlocks": []map[string]any{
			{"type": "text", "text": "补充一句：请在最开始的回复里加上 CLAWBENCH-STEER-MARKER。"},
		},
		"clientUserMessageId": "clawbench-steer-probe-1",
	})
	steerElapsed := time.Since(steerStart)

	require.NoError(t, err, "P3 FAILED: session/steer got no response while a prompt was in flight — "+
		"it is either blocked behind the prompt or the raw transport is broken")

	body, _ := json.Marshal(steerResp)
	t.Logf("P3 session/steer raw response (%.2fs): %s", steerElapsed.Seconds(), body)

	code, msg, isErr := extractJSONRPCError(steerResp)
	if isErr && code == -32601 {
		t.Fatalf("P3 FAILED: session/steer returned Method not found (-32601); response=%s", body)
	}
	if isErr {
		t.Logf("NOTE: session/steer returned JSON-RPC error code=%v msg=%q — "+
			"method exists but rejected this request shape", code, msg)
	} else {
		if steered, ok := steerResp["result"].(map[string]any); ok {
			t.Logf("P3 steer result: %s", mustJSON(t, steered))
			if v, ok := steered["steered"].(bool); ok {
				if v {
					t.Logf("P3 CONFIRMED: steer accepted mid-turn (steered=true, ownerRequestId=%v); "+
						"elapsed=%.2fs — NOT blocked behind the prompt",
						steered["ownerRequestId"], steerElapsed.Seconds())
				} else {
					t.Logf("P3 PARTIAL: steer responded {steered:false, reason=%v} in %.2fs — "+
						"transport + dispatch confirmed, but the session was not in an injectable state "+
						"(check that the prompt was actually running)",
						steered["reason"], steerElapsed.Seconds())
				}
			}
		}
	}

	// The prompt must still finish cleanly — a steer must not wedge the turn.
	select {
	case <-promptDone:
		require.NoError(t, promptErr, "P3 FAILED: prompt errored after steer")
		t.Logf("P3 CONFIRMED: prompt completed cleanly after steer; stopReason=%q", promptResp.StopReason)
	case <-time.After(120 * time.Second):
		t.Fatal("P3 FAILED: prompt did not complete within 120s after steer — steer wedged the turn")
	}

	// Report whether the steered content actually reached the model (the
	// marker may legitimately be absent if the model ignored it).
	events := drainProbeStream(streamCh, 3*time.Second)
	content := concatACPContent(events)
	hasMarker := strings.Contains(content, "CLAWBENCH-STEER-MARKER")
	t.Logf("P3 steer marker present in final output: %v", hasMarker)
	if !hasMarker {
		t.Logf("NOTE: marker not observed. This does NOT disprove steer — the model may have " +
			"ignored the injected instruction, or the marker was emitted before injection. " +
			"Check the raw response above for steered=true + ownerRequestId as the authoritative signal.")
	}
}

// TestCodebuddyACP_ExtProbe_CapabilityNegotiation 验证能力协商面：
// 用不同 clientInfo.name / clientCapabilities._meta 各起一次连接，对比
// CodeBuddy 广告的 agentCapabilities 与暴露的 configOptions，实证门控是否生效。
//
// 这决定了"接入前需要广告哪些能力位"。纯观测型：断言仅要求连接建立成功。
func TestCodebuddyACP_ExtProbe_CapabilityNegotiation(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	cases := []struct {
		name       string
		clientName string
		meta       map[string]any
	}{
		{
			name:       "baseline (clawbench)",
			clientName: "clawbench",
			meta:       nil,
		},
		{
			name:       "codebuddy-web-ui identity",
			clientName: "codebuddy-web-ui",
			meta:       nil,
		},
		{
			name:       "codebuddy.ai caps advertised",
			clientName: "clawbench",
			meta: map[string]any{
				"codebuddy.ai": map[string]any{
					"question":               true,
					"terminalOutput":         true,
					"terminalOutputChunk":    true,
					"mainAgentSupport":       true,
					"fileReferencesPathOnly": true,
					"promptSuggestion":       true,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := contextWithTimeout(t, 120*time.Second)
			defer cancel()

			setup := startExtProbe(t, ctx, acp.ClientCapabilities{
				Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
				Terminal: true,
				Meta:     tc.meta,
			}, tc.clientName)

			// Observation only: the dumps inside startExtProbe carry the payload.
			t.Logf("case %q connected OK (session=%s)", tc.name, setup.session)
		})
	}
}

// TestCodebuddyACP_ExtProbe_ExtensionMethodRouting 验证档 1 的前提：
// agent 发来的 `_` 前缀请求会被 SDK 转给 ExtensionMethodHandler（而非静默丢弃），
// 并且未实现该接口时回 method-not-found。
//
// 本探针用两种客户端各跑一次同一 prompt：
//   - 不实现 ExtensionMethodHandler（当前 ClawBench 状态）→ 记录 SDK 是否回错误；
//   - 实现 ExtensionMethodHandler（未来状态）→ 记录是否真的收到扩展请求。
//
// 观测项：实际收到了哪些 `_codebuddy.ai/*` 方法（若有）。这直接回答
// "接入档 1 能拿到什么"。
func TestCodebuddyACP_ExtProbe_ExtensionMethodRouting(t *testing.T) {
	requireWireProbeCodebuddyACP(t)

	ctx, cancel := contextWithTimeout(t, 240*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "codebuddy", "--acp")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)
	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err)

	writer := &syncWriter{dst: stdinPipe, buf: &bytes.Buffer{}}
	reader := &demuxReader{src: stdoutPipe}

	base := NewClawBenchACPClient()
	recording := &extRecordingClient{ClawBenchACPClient: base}

	conn := acp.NewClientSideConnection(recording, writer, reader)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, cmd.Start(), "spawn codebuddy --acp")
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
			// Advertise the question capability so CodeBuddy is willing to route
			// AskUserQuestion through the extension channel if it chooses to.
			Meta: map[string]any{"codebuddy.ai": map[string]any{"question": true}},
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-ext-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: acpTestWorkDir(), McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")

	streamCh := make(chan StreamEvent, 1024)
	base.RegisterSession(string(newResp.SessionId), streamCh)

	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	// Drain the stream concurrently so the prompt is not blocked on a full
	// channel, and so we can see which tools the model actually invoked.
	var streamEvents []StreamEvent
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		for e := range streamCh {
			streamEvents = append(streamEvents, e)
		}
	}()

	promptCtx, promptCancel := context.WithTimeout(ctx, 90*time.Second)
	defer promptCancel()
	_, promptErr := conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt: []acp.ContentBlock{acp.TextBlock(
			"请调用 AskUserQuestion 工具问我一个问题：'请选择 A 还是 B？'，给出 A 和 B 两个选项。" +
				"必须真的调用该工具，不要只用文字提问。")},
	})
	if promptErr != nil {
		t.Logf("NOTE: prompt returned error: %v", promptErr)
	}

	// Report which tools the model actually used — this disambiguates
	// "CodeBuddy doesn't route question via extension" from
	// "the model simply never called AskUserQuestion".
	var toolNames []string
	for _, e := range streamEvents {
		if e.Type == "tool_use" && e.Tool != nil {
			toolNames = append(toolNames, e.Tool.Name)
		}
	}
	sort.Strings(toolNames)
	t.Logf("tools the model invoked this turn: %v", toolNames)

	// Dump every ask-related wire message so the verdict is evidence-based:
	// a `_codebuddy.ai/question` REQUEST means the extension path; a
	// tool_call/tool_call_update with toolName=AskUserQuestion means the normal
	// tool path (with a separate permission/approval flow).
	t.Log("=== raw wire lines mentioning ask/question ===")
	for _, line := range strings.Split(string(reader.snapshot()), "\n") {
		low := strings.ToLower(line)
		if strings.Contains(low, "askuserquestion") || strings.Contains(low, "_codebuddy.ai/question") ||
			strings.Contains(low, "interruptionrequest") {
			t.Logf("  %s", truncate(line, 700))
		}
	}

	seen := recording.seenMethods()
	if len(seen) == 0 {
		t.Logf("NOTE: no `_codebuddy.ai/*` extension request reached the handler during this turn. " +
			"Either CodeBuddy did not expose AskUserQuestion for this client profile, or it " +
			"did not choose to call it. Compare with the raw prompt output above.")
	} else {
		sort.Strings(seen)
		t.Logf("CONFIRMED: extension requests received: %v", seen)
		for _, raw := range recording.rawCalls() {
			t.Logf("  raw: %s", truncate(raw, 400))
		}
		t.Logf("=> implementing ExtensionMethodHandler on ClawBenchACPClient is sufficient " +
			"to receive these; no infrastructure change needed (docs §8.1 档 1).")
	}

	// ---- Verdict (evidence-based) ----
	//
	// Observed in this run (see the wire dump above):
	//   * the model DID invoke AskUserQuestion (twice), and
	//   * it arrived as a normal tool call — sessionUpdate:"tool_call" with
	//     _meta["codebuddy.ai/toolName"]="AskUserQuestion" — followed by a
	//     PermissionApproval, NOT as a `_codebuddy.ai/question` request.
	//
	// So advertising question=true is NOT by itself sufficient to switch
	// CodeBuddy onto the extension channel in a raw-stdio ACP session.
	// Source-consistent explanation: CodeBuddy's AskUserQuestion tool
	// isEnabled gate requires runContext.context.meta.acpConnectionId
	// (set only by its HTTP ACP gateway, not by raw stdio), so the
	// "ask via extMethod" branch is not the one taken here.
	askedViaTool := false
	for _, n := range toolNames {
		if n == "AskUserQuestion" {
			askedViaTool = true
		}
	}
	switch {
	case recording.sawMethod("_codebuddy.ai/question"):
		t.Logf("VERDICT: CodeBuddy routed AskUserQuestion through `_codebuddy.ai/question` " +
			"(extension request). This is the authoritative path for replacing the " +
			"<clawbench-ask-question> convention.")
	case askedViaTool:
		t.Logf("VERDICT (negative): advertising question=true did NOT route AskUserQuestion " +
			"through the extension channel. The tool arrived as a NORMAL tool call " +
			"(sessionUpdate=tool_call, toolName=AskUserQuestion) plus a PermissionApproval. " +
			"Likely cause: the extension branch is gated on meta.acpConnectionId, which raw " +
			"stdio ACP sessions do not set. => 档 1 的 question 通道在 stdio 下不可用；" +
			"ClawBench 应继续使用 <clawbench-ask-question> 约定。")
	default:
		t.Logf("VERDICT (inconclusive): the model did not call AskUserQuestion at all this run, " +
			"so neither path was exercised. Re-run or rephrase the prompt.")
	}
}

// extRecordingClient embeds the real client and implements
// acp.ExtensionMethodHandler so the SDK routes `_`-prefixed requests here.
type extRecordingClient struct {
	*ClawBenchACPClient

	mu    sync.Mutex
	calls []string
	raw   []string
}

func (c *extRecordingClient) HandleExtensionMethod(_ context.Context, method string, params json.RawMessage) (any, error) {
	c.mu.Lock()
	c.calls = append(c.calls, method)
	c.raw = append(c.raw, fmt.Sprintf("%s %s", method, string(params)))
	c.mu.Unlock()
	// Return a well-formed response for the known request types so CodeBuddy
	// does not treat the extension as failed. Notifications need no response.
	switch method {
	case "_codebuddy.ai/question":
		return map[string]any{"outcome": map[string]any{
			"outcome": "cancelled", "reason": "probe does not answer questions",
		}}, nil
	case "_codebuddy.ai/delegateTool":
		return map[string]any{"status": "error", "error": map[string]any{"message": "probe"}}, nil
	}
	return map[string]any{}, nil
}

func (c *extRecordingClient) seenMethods() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.calls))
	copy(out, c.calls)
	return out
}

func (c *extRecordingClient) rawCalls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.raw))
	copy(out, c.raw)
	return out
}

func (c *extRecordingClient) sawMethod(method string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.calls {
		if m == method {
			return true
		}
	}
	return false
}

// --- small helpers -------------------------------------------------------

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// drainProbeStream reads whatever events are buffered without blocking long.
func drainProbeStream(ch <-chan StreamEvent, wait time.Duration) []StreamEvent {
	var out []StreamEvent
	deadline := time.After(wait)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-deadline:
			return out
		}
	}
}

// registerProbeSession wires a stream sink for the probe session so the client
// does not auto-cancel permission requests.
func registerProbeSession(t *testing.T, setup *extProbeSetup, ch chan StreamEvent) {
	t.Helper()
	// The client is owned by the SDK connection; reach it through the setup's
	// connection by re-registering on the embedded client. We keep a handle in
	// startExtProbe via the setup struct.
	if setup.client != nil {
		setup.client.RegisterSession(setup.session, ch)
	}
}

// findRawResponse scans recorded agent output for the JSON-RPC response whose
// result carries the named key, and returns its raw JSON. Used to inspect the
// wire payload for fields that acp-go-sdk's typed structs silently drop.
func findRawResponse(t *testing.T, wire []byte, key string) string {
	t.Helper()
	var found string
	sc := bufio.NewScanner(bytes.NewReader(wire))
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		res, ok := m["result"].(map[string]any)
		if !ok {
			continue
		}
		if _, hit := res[key]; hit {
			found = string(line)
		}
	}
	if found == "" {
		return "<not found on wire>"
	}
	return found
}

// findRawRequest scans recorded outbound traffic for the request with the given
// JSON-RPC method and returns its raw JSON. Used to verify that what ClawBench
// advertised (clientCapabilities._meta) actually went out on the wire.
func findRawRequest(t *testing.T, wire []byte, method string) string {
	t.Helper()
	var found string
	sc := bufio.NewScanner(bytes.NewReader(wire))
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m["method"] == method {
			found = string(line)
		}
	}
	if found == "" {
		return "<not found on wire>"
	}
	return found
}
