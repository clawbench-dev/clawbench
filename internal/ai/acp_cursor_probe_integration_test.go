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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Cursor ACP — 可行性探针
// ===========================================================================
//
// 目的：在把 Cursor 作为 ACP 后端接入 ClawBench 之前，用真实进程实测端到端。
//
// 目标：**Cursor 官方 CLI 内置的 ACP 子命令** —— `agent acp`
// （`cursor-agent` 是同一二进制的 legacy 别名）
//
//	$ agent acp --help
//	Usage: agent acp [options]
//	Start the Cursor Agent as an ACP (Agent Client Protocol) server
//
// 安装：`curl https://cursor.com/install | bash` → ~/.local/bin/{agent,cursor-agent}
//
// 鉴权：cursor-agent **不读** ANTHROPIC_BASE_URL / ANTHROPIC_API_KEY，只认
// `CURSOR_API_KEY`（Cursor Dashboard → API Keys）或 `agent login`。故本探针
// **不能**复用 claude 的 minimax 配置，必须由环境提供 CURSOR_API_KEY：
//
//	CURSOR_API_KEY=crsr_... go test -tags integration -v \
//	    -run 'TestCursorACPProbe' -timeout 900s ./internal/ai/
//
// 注意：**不要**用 npm 上的第三方适配器 `cursor-agent-acp` / `cursor-acp`。
// 已实测（见 git 历史 / 本文件早期版本）`cursor-agent-acp@0.1.1` 的
// `session/prompt` 会立即返回硬编码的假 `end_turn` 且零 `session/update`
// 通知（子进程 stdin 从不 close，而 cursor-agent 要读到 EOF 才发起请求）。
// 官方子命令没有这些问题。
//
// 三层探测：
//
//	L1  原始 JSON-RPC 直连 `agent acp`
//	    initialize → session/new → session/prompt，观察 session/update 通知、
//	    stopReason、以及 modes/models 能力面。
//	L2  经 ClawBench 生产链路 ACPBackend.ExecuteStream
//	    （ACPConnManager + ClawBenchACPClient）——ClawBench 真正会走的那条路。
//	L3  运行中切换 mode / model（官方 session/new 广告了 availableModes /
//	    availableModels，验证这两个 RPC 真的可用而非只是广告）。
//
// 事实记录到 t.Logf；硬断言只覆盖「必须成立才能接入」的前提。

const (
	// cursorACPAgentBin 是官方 Cursor CLI。`cursor-agent` 是同一二进制的 legacy
	// 别名（安装脚本同时建了两个 symlink）；用 `agent` 更贴合当前文档。
	cursorACPAgentBin = "agent"
	cursorACPModeBin  = "cursor-agent"
	cursorACPPrompt   = "Reply with exactly: CURSOR-ACP-OK"
	cursorACPWant     = "CURSOR-ACP-OK"
	// cursorACPNotifyWait 是 prompt resolve 之后仍继续收集通知的窗口，兜住
	// 「迟到通知」的可能。
	cursorACPNotifyWait = 5 * time.Second
)

// ---------------------------------------------------------------------------
// 环境与信任
// ---------------------------------------------------------------------------

// cursorACPAvailable 返回可用于 spawn 的 CLI 名；优先 `agent`，回退 `cursor-agent`。
func cursorACPAvailable(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath(cursorACPAgentBin); err == nil {
		return cursorACPAgentBin
	}
	if _, err := exec.LookPath(cursorACPModeBin); err == nil {
		return cursorACPModeBin
	}
	t.Skipf("Cursor CLI not installed (%s / %s); run: curl https://cursor.com/install | bash",
		cursorACPAgentBin, cursorACPModeBin)
	return ""
}

// requireCursorACPProbe 检查探针全部前置条件；缺失则 skip（不是 fail）。
func requireCursorACPProbe(t *testing.T) string {
	t.Helper()
	bin := cursorACPAvailable(t)
	if os.Getenv("CURSOR_API_KEY") == "" {
		t.Skip("CURSOR_API_KEY not set — cursor-agent does NOT read " +
			"ANTHROPIC_BASE_URL/ANTHROPIC_API_KEY; skipping")
	}
	return bin
}

// ensureCursorACPPath 确保 cursor-agent 所在目录在 PATH 上。官方安装脚本装到
// ~/.local/bin，该目录未必在非交互 shell 的 PATH 里，缺失时 spawn 会失败。
func ensureCursorACPPath(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	binDir := filepath.Join(home, ".local", "bin")
	if _, err := os.Stat(filepath.Join(binDir, cursorACPAgentBin)); err != nil {
		if _, err2 := os.Stat(filepath.Join(binDir, cursorACPModeBin)); err2 != nil {
			return
		}
	}
	path := os.Getenv("PATH")
	for _, p := range filepath.SplitList(path) {
		if p == binDir {
			return
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+path)
}

// cursorTrustDir 复刻 cursor-agent 对工作目录的编码：绝对路径去掉前导 `/` 后，
// 所有非字母数字字符替换成 '-'（实测 /tmp → "tmp"、
// /tmp/cursor-trust-probe/sub → "tmp-cursor-trust-probe-sub"），再拼到
// ~/.cursor/projects/ 下。
func cursorTrustDir(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range strings.TrimPrefix(cwd, string(os.PathSeparator)) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return filepath.Join(home, ".cursor", "projects", b.String()), nil
}

// ensureCursorTrusted 写 .workspace-trusted 标记，避免工作目录未信任时 CLI
// 弹 "Workspace Trust Required" 而阻塞。
func ensureCursorTrusted(t *testing.T, cwd string) {
	t.Helper()
	dir, err := cursorTrustDir(cwd)
	if err != nil {
		t.Logf("WARN: cannot resolve cursor trust dir for %s: %v", cwd, err)
		return
	}
	marker := filepath.Join(dir, ".workspace-trusted")
	if _, err := os.Stat(marker); err == nil {
		t.Logf("cursor workspace trust already present: %s", marker)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("WARN: cannot create cursor trust dir %s: %v", dir, err)
		return
	}
	if err := os.WriteFile(marker, []byte("{}"), 0o644); err != nil {
		t.Logf("WARN: cannot write cursor trust marker %s: %v", marker, err)
		return
	}
	t.Logf("wrote cursor workspace trust marker: %s", marker)
}

// ---------------------------------------------------------------------------
// 原始 JSON-RPC 通道
// ---------------------------------------------------------------------------

// cursorRawProbe 在 acp-go-sdk 之外维护一条裸 JSON-RPC 通道，以便精确测量
// prompt 时延、收集全部通知，并自动应答 agent→client 请求（权限 / 文件读写）
// 以免探针卡死。
type cursorRawProbe struct {
	mu       sync.Mutex
	nextID   int
	pending  map[string]chan map[string]any
	writer   io.Writer        // agent stdin, for post-setup calls
	updates  []map[string]any // session/update params
	other    []string         // 其它 agent→client 通知 method
	requests []string         // agent→client 请求 method（已自动应答）
	buf      bytes.Buffer
}

func newCursorRawProbe() *cursorRawProbe {
	return &cursorRawProbe{pending: map[string]chan map[string]any{}}
}

func (p *cursorRawProbe) call(ctx context.Context, w io.Writer, method string, params any) (map[string]any, error) {
	p.mu.Lock()
	p.nextID++
	id := fmt.Sprintf("cur-%d", p.nextID)
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
		return nil, fmt.Errorf("timeout waiting for %s (id=%s)", method, id)
	}
}

// dispatch 消费一段 stdout：路由探针响应、记录通知、自动应答 agent→client 请求。
func (p *cursorRawProbe) dispatch(chunk []byte, w io.Writer) {
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
			if id, ok := rawID.(string); ok && strings.HasPrefix(id, "cur-") {
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

func (p *cursorRawProbe) snapshot() (updates []map[string]any, other, requests []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.updates...),
		append([]string(nil), p.other...),
		append([]string(nil), p.requests...)
}

// updateKind 提取 sessionUpdate 判别式与文本。
func updateKind(params map[string]any) (kind, text string) {
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

// startCursorRawProbe 启动 `agent acp` 并完成 initialize + session/new，
// 返回裸通道与 sessionId。
func startCursorRawProbe(t *testing.T, ctx context.Context, bin, cwd string) (*cursorRawProbe, string, func() ([]map[string]any, []string, []string)) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, "acp")
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	stdin, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	require.NoError(t, cmd.Start(), "spawn %s acp", bin)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	probe := newCursorRawProbe()
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

	initResp, err := probe.call(ctx, stdin, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
		"clientInfo": map[string]any{"name": "clawbench-cursor-probe", "version": "1.0.0"},
	})
	require.NoError(t, err, "initialize")
	t.Logf("initialize response: %s", jsonStr(initResp))

	sessResp, err := probe.call(ctx, stdin, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	require.NoError(t, err, "session/new")
	t.Logf("session/new response: %s", jsonStr(sessResp))
	result, _ := sessResp["result"].(map[string]any)
	sessionID, _ := result["sessionId"].(string)
	require.NotEmpty(t, sessionID, "session/new must return a sessionId")

	// modes / models 能力面（官方广告，供 L3 使用）。
	if modes, ok := result["modes"].(map[string]any); ok {
		if list, ok := modes["availableModes"].([]any); ok {
			var ids []string
			for _, mo := range list {
				if mm, ok := mo.(map[string]any); ok {
					ids = append(ids, fmt.Sprint(mm["id"]))
				}
			}
			t.Logf("advertised modes: %v (current=%v)", ids, modes["currentModeId"])
		}
	}
	if models, ok := result["models"].(map[string]any); ok {
		if list, ok := models["availableModels"].([]any); ok {
			t.Logf("advertised models: %d (current=%v)", len(list), models["currentModelId"])
		}
	}

	snap := func() ([]map[string]any, []string, []string) { return probe.snapshot() }
	return probe, sessionID, snap
}

// ---------------------------------------------------------------------------
// L1 — 原始 JSON-RPC 直连 `agent acp`
// ---------------------------------------------------------------------------

// TestCursorACPProbe_L1_RawOfficial drives `agent acp` over a raw JSON-RPC
// channel, bypassing acp-go-sdk, to measure exactly what the official ACP
// server does on the wire.
func TestCursorACPProbe_L1_RawOfficial(t *testing.T) {
	bin := requireCursorACPProbe(t)
	ensureCursorACPPath(t)
	cwd := acpTestWorkDir()
	ensureCursorTrusted(t, cwd)

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	probe, sessionID, snapshot := startCursorRawProbe(t, ctx, bin, cwd)

	// prompt 需要 stdin 句柄，故这里复用 startCursorRawProbe 内部的 writer：
	// 通过闭包外的 probe.call 需要 writer；改为直接在 probe 上保存 writer。
	require.NotNil(t, probe.writer, "raw probe must retain its stdin writer")

	promptCtx, promptCancel := context.WithTimeout(ctx, 180*time.Second)
	defer promptCancel()
	start := time.Now()
	promptResp, err := probe.call(promptCtx, probe.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": cursorACPPrompt}},
	})
	require.NoError(t, err, "session/prompt")
	t.Logf("session/prompt resolved in %s: %s", time.Since(start), jsonStr(promptResp))

	// prompt resolve 后继续收集迟到通知。
	time.Sleep(cursorACPNotifyWait)
	updates, other, requests := snapshot()

	t.Logf("notifications: %d session/update, other=%v, agent→client requests=%v",
		len(updates), other, requests)
	var text strings.Builder
	kinds := map[string]int{}
	for i, u := range updates {
		kind, txt := updateKind(u)
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
		"L1 FAILED: `agent acp` emitted ZERO session/update notifications — no assistant output")
	require.NotEmpty(t, kinds["agent_message_chunk"],
		"L1 FAILED: no agent_message_chunk updates (kinds=%v)", kinds)
	require.Contains(t, text.String(), cursorACPWant,
		"L1 FAILED: agent text did not contain the expected token")
}

// ---------------------------------------------------------------------------
// L2 — 经 ClawBench 生产链路（ACPBackend.ExecuteStream）
// ---------------------------------------------------------------------------

// TestCursorACPProbe_L2_Backend drives `agent acp` through the exact path
// ClawBench uses in production: ACPConnManager → ACPBackend.ExecuteStream →
// ClawBenchACPClient → StreamEvent channel.
func TestCursorACPProbe_L2_Backend(t *testing.T) {
	bin := requireCursorACPProbe(t)
	ensureCursorACPPath(t)
	cwd := acpTestWorkDir()
	ensureCursorTrusted(t, cwd)

	agent := &model.Agent{
		ID:         "cursor-probe",
		Name:       "Cursor Probe",
		Backend:    "cursor",
		Transport:  "acp-stdio",
		AcpCommand: bin + " acp",
	}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err, "NewACPBackend")

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	events := sendACPPrompt(t, backend, sessionID, cursorACPPrompt, 180*time.Second)

	t.Logf("L2 stream event types (%d): %v", len(events), acpEventTypes(events))
	for _, e := range events {
		switch e.Type {
		case "content", "thinking":
			t.Logf("  L2 %s: %.120q", e.Type, e.Content)
		case "error":
			t.Logf("  L2 ERROR: %.300q", e.Content)
		case "session_capture", "mode_update", "model_list_update", "commands_update":
			t.Logf("  L2 %s: %.200q", e.Type, e.Content)
		}
	}
	content := concatACPContent(events)
	t.Logf("L2 concatenated content: %.200q", content)

	requireDoneEvent(t, events)
	requireNoErrorEvents(t, events)
	require.NotEmpty(t, content, "L2 FAILED: no assistant content reached the StreamEvent channel")
	require.Contains(t, content, cursorACPWant,
		"L2 FAILED: streamed content did not contain the expected token")
}

// ---------------------------------------------------------------------------
// L3 — 运行中切换 mode / model
// ---------------------------------------------------------------------------

// TestCursorACPProbe_L3_ModeModelSwitch verifies the mode/model RPCs the
// official server advertises in session/new are actually callable (a capability
// advertisement is not proof — see the CodeBuddy lesson: codebuddy implements
// session/resume but does not advertise it; the inverse can also happen).
func TestCursorACPProbe_L3_ModeModelSwitch(t *testing.T) {
	bin := requireCursorACPProbe(t)
	ensureCursorACPPath(t)
	cwd := acpTestWorkDir()
	ensureCursorTrusted(t, cwd)

	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	probe, sessionID, _ := startCursorRawProbe(t, ctx, bin, cwd)
	require.NotNil(t, probe.writer)

	// session/set_mode → plan（官方广告的 availableModes 之一）。
	modeCtx, modeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer modeCancel()
	modeResp, err := probe.call(modeCtx, probe.writer, "session/set_mode", map[string]any{
		"sessionId": sessionID,
		"modeId":    "plan",
	})
	require.NoError(t, err, "session/set_mode must get a response")
	if code, msg, isErr := extractJSONRPCError(modeResp); isErr {
		require.NotEqual(t, float64(-32601), code,
			"session/set_mode returned method-not-found — mode switching is advertised but not implemented: %s", msg)
		t.Logf("session/set_mode business error (acceptable): code=%v msg=%q", code, msg)
	} else {
		t.Logf("session/set_mode OK: %s", jsonStr(modeResp))
	}

	// session/set_model → 取广告里的第一个非 default 模型。
	modelID := cursorFirstNonDefaultModel(t, probe, ctx, sessionID)
	if modelID != "" {
		modelCtx, modelCancel := context.WithTimeout(ctx, 30*time.Second)
		defer modelCancel()
		modelResp, err := probe.call(modelCtx, probe.writer, "session/set_model", map[string]any{
			"sessionId": sessionID,
			"modelId":   modelID,
		})
		require.NoError(t, err, "session/set_model must get a response")
		if code, msg, isErr := extractJSONRPCError(modelResp); isErr {
			require.NotEqual(t, float64(-32601), code,
				"session/set_model returned method-not-found: %s", msg)
			t.Logf("session/set_model business error (acceptable): code=%v msg=%q", code, msg)
		} else {
			t.Logf("session/set_model(%s) OK: %s", modelID, jsonStr(modelResp))
		}
	}
}

// cursorFirstNonDefaultModel re-reads the session's advertised models via
// session/new on a throwaway connection and returns the first non-"default"
// model id, or "" if none.
func cursorFirstNonDefaultModel(t *testing.T, probe *cursorRawProbe, ctx context.Context, sessionID string) string {
	t.Helper()
	// session/new 已在上文调用过，但未缓存 models 列表；这里用一次轻量的
	// session/new 探测（同一连接可建多个会话）。
	newCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := probe.call(newCtx, probe.writer, "session/new", map[string]any{
		"cwd": acpTestWorkDir(), "mcpServers": []any{},
	})
	if err != nil {
		t.Logf("could not re-query models: %v", err)
		return ""
	}
	result, _ := resp["result"].(map[string]any)
	models, _ := result["models"].(map[string]any)
	list, _ := models["availableModels"].([]any)
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		id, _ := m["modelId"].(string)
		if id != "" && !strings.HasPrefix(id, "default") {
			return id
		}
	}
	return ""
}
