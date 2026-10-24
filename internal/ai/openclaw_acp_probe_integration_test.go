//go:build integration

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// OpenClaw ACP — 可行性探针
// ===========================================================================
//
// 目的：在把 OpenClaw 作为 ACP 后端接入 ClawBench 之前，用真实进程实测端到端。
//
// 目标：**官方 CLI 内置的 ACP 子命令** —— `openclaw acp`
//
//	$ openclaw acp --help
//	Usage: openclaw acp [options] [command]
//	Run an ACP bridge backed by the Gateway
//
// 安装：`npm install -g openclaw`（需要 Node >= 24.22.1 或 >= 26.12.1）
//
// **与其它后端最本质的差异：`openclaw acp` 只是 bridge，不是 agent 本体。**
// 它把 ACP 请求转发给 Gateway 的 WebSocket（默认 ws://127.0.0.1:18789）。
// Gateway 没起时 initialize 不会返回错误 —— 进程直接打印
// `ACP bridge failed: connect ECONNREFUSED ...` 到 stderr 并退出，客户端只看到
// 超时。因此本探针在跑之前先探测 Gateway 可达性，不可达则 skip。
//
// 运行：
//
//	openclaw gateway start          # 先起 Gateway
//	go test -tags integration -v -run 'TestOpenClawACPProbe' \
//	    -timeout 1800s ./internal/ai/
//
// 可用 OPENCLAW_PROBE_CMD 覆盖 spawn 命令（默认 "openclaw acp"）。
//
// 四层探测：
//
//	L1  原始 JSON-RPC 直连 `openclaw acp`
//	    initialize → session/new → session/prompt，观察 session/update 通知、
//	    stopReason、能力面（loadSession / sessionCapabilities / promptCapabilities）。
//	L2  经 ClawBench 生产链路 ACPBackend.ExecuteStream
//	    （ACPConnManager + ClawBenchACPClient）——ClawBench 真正会走的那条路。
//	L3  接入约束（这些决定"能不能接"，不只是"能不能跑"）：
//	    a) `session/new` 的 modes 与 configOptions[thought_level] 是**同一组值**
//	       的两种表达 —— 若都照收，UI 会出现两个重复的选择器，且写 mode
//	       会被 bridge 拒绝。
//	    b) per-session `mcpServers` 非空时 session/new 直接失败（bridge 明确
//	       拒绝，不是静默忽略）——必须把 openclaw 从 MCP 注入里摘掉。
//	    c) `session/resume` 只认**本进程内**见过的 id；跨进程必须用
//	       `session/load`。ClawBench 的崩溃恢复先试 resume，故需要能力位
//	       让它直接走 load，否则进程一重启会话就永久报错。
//	    d) 工具调用会先发一个 `tool_call: id: X, args: {...}` 的**包装帧**
//	       （kind=other），随后才是真正的 `web_fetch: url: ...`。照收会渲染
//	       两张卡片，其中一张是噪音。
//	L4  工具帧形态（title / kind / rawInput），用于确定 TitlePrefixes 与
//	    input remap 表。
//
// 事实记录到 t.Logf；硬断言只覆盖「必须成立才能接入」的前提。

const (
	// openClawACPPrompt 要求模型原样回一个标记，便于判定流式内容真的到了。
	openClawACPPrompt = "Reply with exactly: OPENCLAW-ACP-OK"
	openClawACPWant   = "OPENCLAW-ACP-OK"
	// openClawACPNotifyWait 是 prompt resolve 之后继续收集通知的窗口，兜住
	// 「迟到通知」的可能（bridge 的 usage_update / session_info_update 常晚到）。
	openClawACPNotifyWait = 5 * time.Second
	// openClawACPInitTimeout 是 initialize 的等待上限。Gateway 不可达时进程
	// 直接退出、客户端只能等到超时，故这个值同时充当「Gateway 不可用」的判据。
	openClawACPInitTimeout = 30 * time.Second
)

// ---------------------------------------------------------------------------
// 环境与前置
// ---------------------------------------------------------------------------

// openClawACPArgs 返回 spawn 命令（命令 + 参数）。
func openClawACPArgs() []string {
	if v := os.Getenv("OPENCLAW_PROBE_CMD"); v != "" {
		return strings.Fields(v)
	}
	return []string{"openclaw", "acp"}
}

// requireOpenClawACP 检查探针全部前置条件；缺失则 skip（不是 fail）。
//
// 除了二进制存在，还要求 Gateway 可达 —— 这是 openclaw 独有的前置条件：
// bridge 自己不做任何 agent 工作，Gateway 不在时它连 initialize 都答不上来。
func requireOpenClawACP(t *testing.T) []string {
	t.Helper()

	args := openClawACPArgs()
	if _, err := exec.LookPath(args[0]); err != nil {
		t.Skipf("%s not installed, skipping OpenClaw ACP probe "+
			"(npm install -g openclaw; requires Node >= 24.22.1)", args[0])
	}

	// Gateway 可达性：`openclaw health --json` 正常十几毫秒返回，比等 initialize
	// 超时快得多，且失败信息明确。超时给到 60s：Gateway 同时承载多个 ACP 会话时
	// 健康检查会排到后面（实测跑完几轮探针后 20s 都不够）。
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, args[0], "health", "--json").Output()
	if err != nil {
		t.Skipf("OpenClaw Gateway not reachable (`%s health --json` failed: %v). "+
			"`openclaw acp` is only a bridge — start the Gateway first: "+
			"openclaw gateway start", args[0], err)
	}
	var health struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(out, &health) != nil || !health.OK {
		t.Skipf("OpenClaw Gateway reported unhealthy: %.300s", out)
	}

	return args
}

// ---------------------------------------------------------------------------
// 原始 JSON-RPC 通道
// ---------------------------------------------------------------------------

// openClawRawProbe 在 acp-go-sdk 之外维护一条裸 JSON-RPC 通道，以便精确测量
// prompt 时延、收集全部通知，并自动应答 agent→client 请求以免探针卡死。
type openClawRawProbe struct {
	mu       sync.Mutex
	nextID   int
	pending  map[string]chan map[string]any
	writer   io.Writer        // agent stdin, for post-setup calls
	updates  []map[string]any // session/update params
	other    []string         // 其它 agent→client 通知 method
	requests []string         // agent→client 请求 method（已自动应答）
	buf      bytes.Buffer
}

func newOpenClawRawProbe() *openClawRawProbe {
	return &openClawRawProbe{pending: map[string]chan map[string]any{}}
}

func (p *openClawRawProbe) call(ctx context.Context, w io.Writer, method string, params any) (map[string]any, error) {
	p.mu.Lock()
	p.nextID++
	id := fmt.Sprintf("oc-%d", p.nextID)
	ch := make(chan map[string]any, 1)
	p.pending[id] = ch
	p.mu.Unlock()

	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for %s (id=%s)", method, id)
	}
}

// dispatch 消费一段 stdout：路由探针响应、记录通知、自动应答 agent→client 请求。
func (p *openClawRawProbe) dispatch(chunk []byte, w io.Writer) {
	p.mu.Lock()
	p.buf.Write(chunk)
	all := p.buf.String()
	lines := strings.Split(all, "\n")
	p.buf.Reset()
	p.buf.WriteString(lines[len(lines)-1])
	full := lines[:len(lines)-1]
	p.mu.Unlock()

	for _, line := range full {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		rawID, hasID := m["id"]
		method, hasMethod := m["method"].(string)

		// agent→client 请求（有 method 且有 id）：自动应答。
		if hasMethod && hasID {
			p.mu.Lock()
			p.requests = append(p.requests, method)
			p.mu.Unlock()
			var result any
			switch method {
			case "session/request_permission":
				opts, _ := m["params"].(map[string]any)
				optionID := "allow-once"
				if list, ok := opts["options"].([]any); ok && len(list) > 0 {
					if o, ok := list[0].(map[string]any); ok {
						if s, ok := o["optionId"].(string); ok {
							optionID = s
						}
					}
				}
				result = map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}}
			case "fs/read_text_file":
				result = map[string]any{"content": ""}
			case "fs/write_text_file":
				result = map[string]any{}
			default:
				reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": rawID,
					"error": map[string]any{"code": -32601, "message": "not supported by probe"}})
				_, _ = w.Write(append(reply, '\n'))
				continue
			}
			reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": rawID, "result": result})
			_, _ = w.Write(append(reply, '\n'))
			continue
		}

		// 探针自己的响应。
		if hasID && !hasMethod {
			if id, ok := rawID.(string); ok && strings.HasPrefix(id, "oc-") {
				p.mu.Lock()
				ch, found := p.pending[id]
				if found {
					delete(p.pending, id)
				}
				p.mu.Unlock()
				if found {
					ch <- m
				}
			}
			continue
		}

		// 通知。
		if method == "session/update" {
			params, _ := m["params"].(map[string]any)
			p.mu.Lock()
			p.updates = append(p.updates, params)
			p.mu.Unlock()
		} else if method != "" {
			p.mu.Lock()
			p.other = append(p.other, method)
			p.mu.Unlock()
		}
	}
}

func (p *openClawRawProbe) snapshot() (updates []map[string]any, other, requests []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.updates...),
		append([]string(nil), p.other...),
		append([]string(nil), p.requests...)
}

// openClawUpdateKind 提取 sessionUpdate 判别式与文本。
func openClawUpdateKind(params map[string]any) (kind, text string) {
	u, _ := params["update"].(map[string]any)
	if u == nil {
		return "<no update>", ""
	}
	kind, _ = u["sessionUpdate"].(string)
	if content, ok := u["content"].(map[string]any); ok {
		text, _ = content["text"].(string)
	}
	return kind, text
}

// startOpenClawRawProbe 启动 `openclaw acp` 并完成 initialize + session/new，
// 返回裸通道与 sessionId。
func startOpenClawRawProbe(t *testing.T, ctx context.Context, args []string, cwd string) (*openClawRawProbe, string, func() ([]map[string]any, []string, []string)) {
	t.Helper()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	stdin, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	require.NoError(t, cmd.Start(), "spawn %s", strings.Join(args, " "))
	t.Cleanup(func() {
		if cmd.Process != nil {
			// Close stdin first: the bridge treats EOF as "shut down" and exits on
			// its own, which is far more reliable than SIGKILL followed by a Wait
			// that can block on a half-drained stdout pipe.
			_ = stdin.Close()
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				_ = cmd.Process.Kill()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Logf("WARN: bridge process %d did not exit after kill", cmd.Process.Pid)
				}
			}
		}
		if stderrBuf.Len() > 0 {
			t.Logf("child stderr: %.800s", stderrBuf.String())
		}
	})

	probe := newOpenClawRawProbe()
	probe.writer = stdin
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				probe.dispatch(buf[:n], stdin)
			}
			if err != nil {
				return
			}
		}
	}()

	initCtx, initCancel := context.WithTimeout(ctx, openClawACPInitTimeout)
	defer initCancel()
	initResp, err := probe.call(initCtx, stdin, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
		"clientInfo": map[string]any{"name": "clawbench-openclaw-probe", "version": "1.0.0"},
	})
	require.NoError(t, err, "initialize (Gateway down? `openclaw gateway start`)")
	t.Logf("initialize response: %s", jsonStr(initResp))

	sessResp, err := probe.call(ctx, stdin, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	require.NoError(t, err, "session/new")
	result, _ := sessResp["result"].(map[string]any)
	sessionID, _ := result["sessionId"].(string)
	require.NotEmpty(t, sessionID, "session/new must return a sessionId")

	snap := func() ([]map[string]any, []string, []string) { return probe.snapshot() }
	return probe, sessionID, snap
}

// ---------------------------------------------------------------------------
// L1 — 原始 JSON-RPC 直连 `openclaw acp`
// ---------------------------------------------------------------------------

// TestOpenClawACPProbe_L1_RawOfficial drives `openclaw acp` over a raw JSON-RPC
// channel, bypassing acp-go-sdk, to measure exactly what the bridge does on the wire.
func TestOpenClawACPProbe_L1_RawOfficial(t *testing.T) {
	args := requireOpenClawACP(t)
	cwd := acpTestWorkDir()

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	probe, sessionID, snapshot := startOpenClawRawProbe(t, ctx, args, cwd)
	require.NotNil(t, probe.writer, "raw probe must retain its stdin writer")

	promptCtx, promptCancel := context.WithTimeout(ctx, 240*time.Second)
	defer promptCancel()
	start := time.Now()
	promptResp, err := probe.call(promptCtx, probe.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": openClawACPPrompt}},
	})
	require.NoError(t, err, "session/prompt")
	t.Logf("session/prompt resolved in %s: %s", time.Since(start), jsonStr(promptResp))

	// prompt resolve 后继续收集迟到通知。
	time.Sleep(openClawACPNotifyWait)
	updates, other, requests := snapshot()

	t.Logf("notifications: %d session/update, other=%v, agent→client requests=%v",
		len(updates), other, requests)
	var text strings.Builder
	kinds := map[string]int{}
	for i, u := range updates {
		kind, txt := openClawUpdateKind(u)
		kinds[kind]++
		if i < 12 {
			t.Logf("  update[%d] kind=%q text=%.120q", i, kind, txt)
		}
		text.WriteString(txt)
	}
	t.Logf("update kinds: %v", kinds)
	t.Logf("concatenated agent text: %.200q", text.String())

	result, _ := promptResp["result"].(map[string]any)
	require.Equal(t, "end_turn", fmt.Sprint(result["stopReason"]), "prompt must end with stopReason=end_turn")
	require.NotEmpty(t, updates,
		"L1 FAILED: `openclaw acp` emitted ZERO session/update notifications — no assistant output")
	require.NotEmpty(t, kinds["agent_message_chunk"],
		"L1 FAILED: no agent_message_chunk updates (kinds=%v)", kinds)
	require.Contains(t, text.String(), openClawACPWant,
		"L1 FAILED: agent text did not contain the expected token")
}

// ---------------------------------------------------------------------------
// L2 — 经 ClawBench 生产链路（ACPBackend.ExecuteStream）
// ---------------------------------------------------------------------------

// TestOpenClawACPProbe_L2_Backend drives `openclaw acp` through the exact path
// ClawBench uses in production: ACPConnManager → ACPBackend.ExecuteStream →
// ClawBenchACPClient → StreamEvent channel.
func TestOpenClawACPProbe_L2_Backend(t *testing.T) {
	args := requireOpenClawACP(t)

	agent := &model.Agent{
		ID:         "openclaw-probe",
		Name:       "OpenClaw Probe",
		Backend:    "openclaw",
		Transport:  "acp-stdio",
		AcpCommand: strings.Join(args, " "),
	}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err, "NewACPBackend")

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	events := sendACPPrompt(t, backend, sessionID, openClawACPPrompt, 240*time.Second)

	t.Logf("L2 stream event types (%d): %v", len(events), acpEventTypes(events))
	for _, e := range events {
		switch e.Type {
		case "content", "thinking":
			t.Logf("  L2 %s: %.120q", e.Type, e.Content)
		case "error":
			t.Logf("  L2 ERROR: %.300q", e.Content)
		case "session_capture", "mode_update", "model_list_update", "commands_update",
			"thinking_effort_update", "config_update":
			t.Logf("  L2 %s: %.200q", e.Type, e.Content)
		}
	}
	content := concatACPContent(events)
	t.Logf("L2 concatenated content: %.200q", content)

	requireDoneEvent(t, events)
	requireNoErrorEvents(t, events)
	require.NotEmpty(t, content, "L2 FAILED: no assistant content reached the StreamEvent channel")
	require.Contains(t, content, openClawACPWant,
		"L2 FAILED: streamed content did not contain the expected token")
}

// ---------------------------------------------------------------------------
// L3 — 接入约束
// ---------------------------------------------------------------------------

// TestOpenClawACPProbe_L3_ModesMirrorThoughtLevel pins constraint (a): the
// `modes` block in session/new carries exactly the same ids as the
// `thought_level` config option. Consuming both produces two identical pickers,
// and writing the mode is rejected outright.
func TestOpenClawACPProbe_L3_ModesMirrorThoughtLevel(t *testing.T) {
	args := requireOpenClawACP(t)

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	probe, sessionID, _ := startOpenClawRawProbe(t, ctx, args, acpTestWorkDir())
	require.NotNil(t, probe.writer)

	// Re-query session/new on the same connection to read the raw capability面.
	newCtx, newCancel := context.WithTimeout(ctx, 120*time.Second)
	defer newCancel()
	resp, err := probe.call(newCtx, probe.writer, "session/new", map[string]any{
		"cwd": acpTestWorkDir(), "mcpServers": []any{},
	})
	require.NoError(t, err, "session/new (capability query)")
	result, _ := resp["result"].(map[string]any)

	modeIDs := openClawModeIDs(result)
	thoughtIDs, thoughtConfigID := openClawThoughtLevelOption(result)
	t.Logf("modes.availableModes ids: %v (current=%v)", modeIDs, openClawCurrentModeID(result))
	t.Logf("configOptions[thought_level]: id=%q values=%v", thoughtConfigID, thoughtIDs)
	t.Logf("all config option ids: %v", openClawConfigOptionIDs(result))

	require.NotEmpty(t, modeIDs, "L3 FAILED: session/new must advertise modes (used by the probe's own assertions)")
	require.NotEmpty(t, thoughtIDs, "L3 FAILED: session/new must advertise a thought_level config option")
	require.ElementsMatch(t, modeIDs, thoughtIDs,
		"L3 CHANGED: modes and thought_level are no longer the same value set — "+
			"revisit whether the mode picker should still be suppressed")

	// Writing the mode must be rejected: the bridge only honors the config option.
	setModeCtx, setModeCancel := context.WithTimeout(ctx, 60*time.Second)
	defer setModeCancel()
	setModeResp, err := probe.call(setModeCtx, probe.writer, "session/set_config_option", map[string]any{
		"sessionId": sessionID,
		"configId":  "mode",
		"value":     modeIDs[0],
	})
	require.NoError(t, err, "session/set_config_option(mode) must get a response")
	code, msg, isErr := extractJSONRPCError(setModeResp)
	t.Logf("set_config_option(configId=mode) -> isError=%v code=%v msg=%q", isErr, code, msg)
	require.True(t, isErr,
		"L3 CHANGED: the bridge now accepts configId=mode; the mode picker no longer needs suppressing")

	// The thought_level option must be writable — but NOT necessarily for every
	// advertised value. The advertised list is model-agnostic (it is the bridge's
	// static enum), while what the Gateway model accepts is per-model: minimax
	// rejects "adaptive" and "ultra" with an explicit -32603 naming the supported
	// set. So the assertion is "at least one advertised value is writable", and
	// the per-value outcome is recorded (ClawBench must not surface an
	// unwritable value as selectable, and must not treat a refused value as an
	// error — see setConfigOptionWithCrashCheck's isInvalidConfigValue path).
	var writable, refused []string
	for _, v := range thoughtIDs {
		oneCtx, oneCancel := context.WithTimeout(ctx, 60*time.Second)
		resp, err := probe.call(oneCtx, probe.writer, "session/set_config_option", map[string]any{
			"sessionId": sessionID,
			"configId":  thoughtConfigID,
			"value":     v,
		})
		oneCancel()
		require.NoError(t, err, "session/set_config_option(%s=%s) must get a response", thoughtConfigID, v)
		if code, msg, isErr := extractJSONRPCError(resp); isErr {
			refused = append(refused, v)
			t.Logf("  %s=%s refused: code=%v msg=%.160q", thoughtConfigID, v, code, msg)
			continue
		}
		writable = append(writable, v)
	}
	t.Logf("thought_level writable=%v refused=%v", writable, refused)
	require.NotEmpty(t, writable,
		"L3 FAILED: the advertised thought_level option is not writable for ANY value — "+
			"the UI would show a dead selector")
}

// TestOpenClawACPProbe_L3_RejectsPerSessionMCP pins constraint (b): the bridge
// refuses per-session mcpServers instead of ignoring them, so session/new FAILS
// whenever ClawBench injects one. This is why openclaw must be excluded from MCP
// injection rather than relying on "it will just ignore it".
//
// IMPORTANT — the rejection is triggered by the env/args payload, not by the mere
// presence of the field. Measured: `{name, command}` and `{name, command, args}`
// are ACCEPTED (and silently ignored); `{name, command, args, env: []}` is
// REJECTED. ClawBench's `toACPMcpServers` always emits BOTH `args: []` and
// `env: []` (see nonNilStrings/toEnvVariables — both exist to stop strict
// consumers from seeing JSON null), so the payload it actually sends is the
// rejected shape. A probe that omits `env` measures a request ClawBench never
// makes and concludes the opposite.
func TestOpenClawACPProbe_L3_RejectsPerSessionMCP(t *testing.T) {
	args := requireOpenClawACP(t)

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	probe, _, _ := startOpenClawRawProbe(t, ctx, args, acpTestWorkDir())
	require.NotNil(t, probe.writer)

	// Exactly the shape toACPMcpServers produces for a stdio server with no
	// env: both slices present and empty.
	clawbenchPayload := map[string]any{
		"name":    "probe-mcp",
		"command": "/bin/echo",
		"args":    []string{},
		"env":     []any{},
	}

	newCtx, newCancel := context.WithTimeout(ctx, 120*time.Second)
	defer newCancel()
	resp, err := probe.call(newCtx, probe.writer, "session/new", map[string]any{
		"cwd":        acpTestWorkDir(),
		"mcpServers": []any{clawbenchPayload},
	})
	require.NoError(t, err, "session/new must get a response (not a transport timeout)")

	code, msg, isErr := extractJSONRPCError(resp)
	detail := openClawErrorText(resp)
	t.Logf("session/new with ClawBench-shaped mcpServers -> isError=%v code=%v msg=%q detail=%q", isErr, code, msg, detail)
	require.True(t, isErr,
		"L3 CHANGED: the bridge now accepts the mcpServers payload ClawBench sends — "+
			"openclaw could be removed from the MCP-injection exclusion list")
	// The human-readable reason lives in data.details, not in message (which is
	// just "Internal error" — see openClawErrorText).
	require.Contains(t, strings.ToLower(detail), "mcp",
		"L3 CHANGED: session/new failed for a reason other than MCP; investigate before "+
			"assuming the MCP exclusion is still needed")

	// Control: the SAME server without env/args is accepted (and the tool never
	// appears). This is what makes "openclaw ignores MCP" a half-truth — the
	// bridge is either silently ignoring it or hard-failing depending on the
	// payload, so neither "it works" nor "it's filtered" can be assumed.
	controlCtx, controlCancel := context.WithTimeout(ctx, 120*time.Second)
	defer controlCancel()
	control, err := probe.call(controlCtx, probe.writer, "session/new", map[string]any{
		"cwd": acpTestWorkDir(),
		"mcpServers": []any{map[string]any{
			"name": "probe-mcp-min", "command": "/bin/echo",
		}},
	})
	require.NoError(t, err, "control session/new")
	controlCode, controlMsg, controlIsErr := extractJSONRPCError(control)
	t.Logf("control (no args/env) -> isError=%v code=%v msg=%q", controlIsErr, controlCode, controlMsg)
	require.False(t, controlIsErr,
		"L3 CHANGED: a minimal mcpServers entry is now rejected too — the rejection is no "+
			"longer payload-shape dependent, so the note above is stale")
}

// TestOpenClawACPProbe_L3_ResumeIsProcessLocal pins constraint (c): session/resume
// only resolves ids this bridge process has seen; after the process dies the same
// id must be recovered with session/load. ClawBench's crash recovery tries resume
// first and only falls back to load on -32601, so openclaw needs an explicit
// "load-only recovery" capability bit or every restart strands the session.
func TestOpenClawACPProbe_L3_ResumeIsProcessLocal(t *testing.T) {
	args := requireOpenClawACP(t)
	cwd := acpTestWorkDir()

	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	// --- process A: create a session and put a secret in it.
	probeA, sessionID, _ := startOpenClawRawProbe(t, ctx, args, cwd)
	require.NotNil(t, probeA.writer)

	promptCtx, promptCancel := context.WithTimeout(ctx, 240*time.Second)
	defer promptCancel()
	_, err := probeA.call(promptCtx, probeA.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": "Remember the secret word: PROBE-SECRET-42. Reply OK."}},
	})
	require.NoError(t, err, "seed prompt")
	t.Logf("seeded session %s", sessionID)

	// Kill process A hard — no graceful close, exactly what a crash/OOM looks like.
	require.NoError(t, probeA.writer.(io.Closer).Close(), "close stdin")
	time.Sleep(3 * time.Second)

	// --- process B: resume the same id.
	probeB, _, _ := startOpenClawRawProbe(t, ctx, args, cwd)
	require.NotNil(t, probeB.writer)

	resumeCtx, resumeCancel := context.WithTimeout(ctx, 120*time.Second)
	defer resumeCancel()
	resumeResp, err := probeB.call(resumeCtx, probeB.writer, "session/resume", map[string]any{
		"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{},
	})
	require.NoError(t, err, "session/resume must get a response")
	resumeCode, resumeMsg, resumeIsErr := extractJSONRPCError(resumeResp)
	t.Logf("cross-process session/resume -> isError=%v code=%v msg=%q", resumeIsErr, resumeCode, resumeMsg)
	require.True(t, resumeIsErr,
		"L3 CHANGED: session/resume now works across processes — openclaw no longer needs "+
			"load-only recovery")

	// --- process B: load the same id instead. Must succeed AND restore history.
	loadCtx, loadCancel := context.WithTimeout(ctx, 240*time.Second)
	defer loadCancel()
	loadResp, err := probeB.call(loadCtx, probeB.writer, "session/load", map[string]any{
		"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{},
	})
	require.NoError(t, err, "session/load must get a response")
	if code, msg, isErr := extractJSONRPCError(loadResp); isErr {
		t.Fatalf("L3 FAILED: session/load cannot recover a session after the bridge process "+
			"restarted (code=%v msg=%q) — openclaw sessions would be unrecoverable", code, msg)
	}

	// History must be there: ask for the secret on the loaded session.
	time.Sleep(2 * time.Second)
	askCtx, askCancel := context.WithTimeout(ctx, 240*time.Second)
	defer askCancel()
	_, err = probeB.call(askCtx, probeB.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": "What was the secret word? Reply with just the word."}},
	})
	require.NoError(t, err, "post-load prompt")
	time.Sleep(openClawACPNotifyWait)

	updates, _, _ := probeB.snapshot()
	var text strings.Builder
	for _, u := range updates {
		kind, txt := openClawUpdateKind(u)
		if kind == "agent_message_chunk" {
			text.WriteString(txt)
		}
	}
	t.Logf("answer after cross-process load: %.300q", text.String())
	require.Contains(t, text.String(), "PROBE-SECRET-42",
		"L3 FAILED: session/load returned OK but the conversation history was lost — "+
			"recovery would silently give the user an amnesiac session")
}

// TestOpenClawACPProbe_L3_WrapperToolFrames pins constraint (d): plugin/MCP tools
// arrive as a wrapper frame (`tool_call: id: web_fetch, args: {...}`, kind=other)
// followed by the real frame. Forwarding both renders a duplicate card whose name
// is the literal string "tool_call".
func TestOpenClawACPProbe_L3_WrapperToolFrames(t *testing.T) {
	args := requireOpenClawACP(t)

	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	probe, sessionID, _ := startOpenClawRawProbe(t, ctx, args, acpTestWorkDir())
	require.NotNil(t, probe.writer)

	promptCtx, promptCancel := context.WithTimeout(ctx, 420*time.Second)
	defer promptCancel()
	_, err := probe.call(promptCtx, probe.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt": []any{map[string]any{"type": "text", "text": "Use your web_fetch tool to fetch https://example.com " +
			"and tell me the page title. Then read the file /etc/hostname."}},
	})
	require.NoError(t, err, "tool prompt")
	time.Sleep(openClawACPNotifyWait)

	updates, _, _ := probe.snapshot()
	type frame struct {
		title string
		kind  string
		id    string
	}
	var frames []frame
	for _, u := range updates {
		upd, _ := u["update"].(map[string]any)
		if upd == nil || upd["sessionUpdate"] != "tool_call" {
			continue
		}
		title, _ := upd["title"].(string)
		kind, _ := upd["kind"].(string)
		id, _ := upd["toolCallId"].(string)
		frames = append(frames, frame{title: title, kind: kind, id: id})
	}

	for _, f := range frames {
		t.Logf("tool_call frame: kind=%-8s id=%-40s title=%.100q", f.kind, f.id, f.title)
	}
	require.NotEmpty(t, frames, "L3 FAILED: no tool_call frames — the prompt never invoked a tool")

	wrappers := 0
	for _, f := range frames {
		if strings.HasPrefix(f.title, "tool_call: id: ") {
			wrappers++
		}
	}
	t.Logf("wrapper frames: %d / %d", wrappers, len(frames))
	require.NotZero(t, wrappers,
		"L3 CHANGED: no wrapper frames observed — the duplicate-card filter can be removed")

	// The wrapper's toolCallId differs from the real frame's, so a name-based
	// filter is required (an id-based one would not collapse them).
	for _, f := range frames {
		if strings.HasPrefix(f.title, "tool_call: id: ") {
			require.Equal(t, "other", f.kind,
				"L3 CHANGED: wrapper frames no longer use kind=other; the filter predicate must be revisited")
		}
	}
}

// ---------------------------------------------------------------------------
// L4 — 工具帧形态（用于确定 TitlePrefixes / remaps）
// ---------------------------------------------------------------------------

// TestOpenClawACPProbe_L4_ToolFrameShapes records the exact title/kind/rawInput
// shapes so the backend's TitlePrefixes and input remap tables can be written
// from evidence rather than guesses.
func TestOpenClawACPProbe_L4_ToolFrameShapes(t *testing.T) {
	args := requireOpenClawACP(t)
	cwd := acpTestWorkDir()

	ctx, cancel := contextWithTimeout(t, 900*time.Second)
	defer cancel()

	probe, sessionID, _ := startOpenClawRawProbe(t, ctx, args, cwd)
	require.NotNil(t, probe.writer)

	probes := []string{
		"Use the read tool to read " + cwd + "/go.mod. Use the tool directly.",
		"Use the exec tool to run: echo OPENCLAW-EXEC-OK",
		"Use the ls tool to list " + cwd,
		"Use the edit tool to change the string 'aaa' to 'bbb' in /tmp/openclaw-probe-nonexistent.txt.",
		"Use the write tool to write 'probe' into /tmp/openclaw-probe-out.txt.",
		"Use the web_fetch tool on https://example.com",
		"Use the web_search tool for 'OpenClaw ACP'",
	}
	for i, p := range probes {
		t.Logf("=== probe[%d]: %s", i, p)
		pCtx, pCancel := context.WithTimeout(ctx, 300*time.Second)
		_, err := probe.call(pCtx, probe.writer, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt":    []any{map[string]any{"type": "text", "text": p}},
		})
		pCancel()
		if err != nil {
			t.Logf("  prompt error (recorded, not fatal): %v", err)
		}
		time.Sleep(1500 * time.Millisecond)
	}

	updates, _, _ := probe.snapshot()
	seen := map[string]bool{}
	for _, u := range updates {
		upd, _ := u["update"].(map[string]any)
		if upd == nil || upd["sessionUpdate"] != "tool_call" {
			continue
		}
		title, _ := upd["title"].(string)
		kind, _ := upd["kind"].(string)
		rawInput, _ := json.Marshal(upd["rawInput"])
		key := title + "|" + kind
		if seen[key] {
			continue
		}
		seen[key] = true
		t.Logf("  title=%.120q kind=%s rawInput=%.200s", title, kind, rawInput)
	}
	require.NotEmpty(t, seen, "L4 FAILED: no tool_call frames captured")

	// The tool name is always the first colon-delimited token of the title (or
	// the whole title when there is no colon). This is what TitlePrefixes keys on.
	prefixes := map[string]bool{}
	for key := range seen {
		title := strings.SplitN(key, "|", 2)[0]
		name := title
		if idx := strings.Index(title, ":"); idx > 0 {
			name = title[:idx]
		}
		prefixes[name] = true
	}
	names := make([]string, 0, len(prefixes))
	for n := range prefixes {
		names = append(names, n)
	}
	t.Logf("observed title prefixes (%d): %v", len(names), names)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// openClawErrorText flattens a JSON-RPC error response into one searchable
// string. Needed because openclaw reports every business failure as
// `message: "Internal error"` and puts the actual reason in `data.details` —
// asserting on `message` alone sees only "internal error" and matches nothing.
func openClawErrorText(resp map[string]any) string {
	e, ok := resp["error"].(map[string]any)
	if !ok {
		return ""
	}
	var sb strings.Builder
	if m, ok := e["message"].(string); ok {
		sb.WriteString(m)
	}
	if data, ok := e["data"].(map[string]any); ok {
		if d, ok := data["details"].(string); ok {
			sb.WriteString(" ")
			sb.WriteString(d)
		}
	}
	return sb.String()
}

// openClawModeIDs extracts modes.availableModes[].id from a session/new result.
func openClawModeIDs(result map[string]any) []string {
	modes, _ := result["modes"].(map[string]any)
	list, _ := modes["availableModes"].([]any)
	var ids []string
	for _, raw := range list {
		if m, ok := raw.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func openClawCurrentModeID(result map[string]any) string {
	modes, _ := result["modes"].(map[string]any)
	id, _ := modes["currentModeId"].(string)
	return id
}

// openClawThoughtLevelOption returns the thought_level option's values and id.
func openClawThoughtLevelOption(result map[string]any) ([]string, string) {
	opts, _ := result["configOptions"].([]any)
	for _, raw := range opts {
		opt, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		category, _ := opt["category"].(string)
		if category != "thought_level" {
			continue
		}
		id, _ := opt["id"].(string)
		var values []string
		if list, ok := opt["options"].([]any); ok {
			for _, o := range list {
				if m, ok := o.(map[string]any); ok {
					if v, ok := m["value"].(string); ok {
						values = append(values, v)
					}
				}
			}
		}
		return values, id
	}
	return nil, ""
}

// openClawConfigOptionIDs lists every advertised config option id.
func openClawConfigOptionIDs(result map[string]any) []string {
	opts, _ := result["configOptions"].([]any)
	var ids []string
	for _, raw := range opts {
		if opt, ok := raw.(map[string]any); ok {
			if id, ok := opt["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	return ids
}
