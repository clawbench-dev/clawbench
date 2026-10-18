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
// Command Code ACP — 可行性探针
// ===========================================================================
//
// 目的：在把 Command Code（Langbase 的 coding agent，npm 包 `command-code`，
// 二进制名 `cmd`）作为 ACP 后端接入 ClawBench 之前，用真实进程实测端到端。
//
// 目标：**官方 CLI 内置的 ACP 子命令** —— `cmd acp`
//
//	$ cmd acp --help
//	Usage: cmd acp [options]
//	Run as an Agent Client Protocol (ACP) agent over stdio (for Zed and other
//	ACP clients)
//
// 安装：`npm i -g command-code`（bin: cmd / cmdc / commandcode / command-code）
// 官方文档：https://commandcode.ai/docs/acp
//
// 鉴权：**必须先 `cmd login`**（或在环境里放 `COMMAND_CODE_API_KEY`）。与
// cursor 不同，Command Code 会把「未登录」当成**协议级错误**：`session/new`
// 直接回 JSON-RPC error `-32000 AUTH_REQUIRED`（"Not authenticated. Run
// `cmd login` in a terminal first."）。initialize 本身不需要登录，因此能力面
// 探针（L1-Init）在未登录时也能跑。
//
// **免额度路径（BYOK，见 L0）**：鉴权门只检查 key **非空**，不校验有效性，所以
// 可以用占位 key 过闸 + 自备 provider（~/.commandcode/providers.json）跑真实
// 推理，不消耗 Command Code 额度。需要同时写 config.json 的 model/modelProvider。
//
// 运行：
//
//	# 已登录（推荐）：走 ~/.commandcode/auth.json
//	go test -tags integration -v -run 'TestCommandCodeACPProbe' \
//	    -timeout 900s ./internal/ai/
//
//	# 用 API key 而非登录态：
//	COMMAND_CODE_API_KEY=... go test -tags integration -v \
//	    -run 'TestCommandCodeACPProbe' -timeout 900s ./internal/ai/
//
//	# BYOK（需 MINIMAX_API_KEY；探针会临时改写并恢复 ~/.commandcode/*.json）：
//	MINIMAX_API_KEY=... go test -tags integration -v \
//	    -run 'TestCommandCodeACPProbe_L0' -timeout 600s ./internal/ai/
//
//	# 未安装 cmd 时探针自动回退 `npx -y command-code@latest acp`；
//	# 可用 COMMAND_CODE_PROBE_CMD 显式覆盖 spawn 命令。
//
// 四层探测：
//
//	L0  BYOK：占位 key 过闸 + 自备 provider 跑真实推理（免额度）
//	L1  原始 JSON-RPC 直连 `cmd acp`
//	    initialize → session/new → session/prompt，观察 session/update 通知、
//	    stopReason、以及 modes/models/configOptions 能力面。
//	L2  经 ClawBench 生产链路 ACPBackend.ExecuteStream
//	    （ACPConnManager + ClawBenchACPClient）——ClawBench 真正会走的那条路。
//	L3  Command Code 独有约束（这些决定能否接入，不只是"能不能跑"）：
//	    a) 单进程单项目：cwd lease —— 一个 `cmd acp` 进程只服务一个项目目录，
//	       第二个不同 cwd 的 session/new 被拒（INVALID_PARAMS）。
//	    b) 同会话并发 prompt 被拒（INVALID_REQUEST "A prompt is already
//	       running for this session"）—— 意味着没有 steer/queue 语义。
//	    c) **无轮中注入**：session/steer 与 _session/steering 都不在方法表里。
//	    d) set_mode / set_model 会校验取值；运行中切模型被拒。
//
// 事实记录到 t.Logf；硬断言只覆盖「必须成立才能接入」的前提。

const (
	// commandCodeACPProbePrompt 是一个最小的工具调用回合，用于验证 tool_call
	// 事件与文本流式两条链路。
	commandCodeACPProbePrompt = "Run the shell command `echo clawbench-cc-probe` with your terminal tool, " +
		"then reply with exactly this token and nothing else: COMMANDCODE-ACP-OK"
	commandCodeACPProbeWant = "COMMANDCODE-ACP-OK"

	// commandCodeACPNotifyWait 是 prompt resolve 之后继续收集通知的窗口。
	commandCodeACPNotifyWait = 5 * time.Second

	// commandCodeACPAuthRequired 是 ACP 的 AuthenticationRequired 错误码
	// （-32000..-32099 是 ACP 保留给协议级错误的区间；SDK 里对应
	// acp.NewAuthRequired）。Command Code 未登录时从 session/new 返回它
	// （cli.mjs: hn.AUTH_REQUIRED = -32000）。
	commandCodeACPAuthRequired = float64(-32000)

	// commandCodeACPNpxSpec 是未安装 cmd 时的回退 npm 包（spawn 为
	// `npx -y <spec> acp`）。
	commandCodeACPNpxSpec = "command-code@latest"
)

// ---------------------------------------------------------------------------
// 环境与信任
// ---------------------------------------------------------------------------

// commandCodeACPCommand 返回用于 spawn 的 (bin, args)。优先本机 `cmd`
// （Windows 上官方用的是 `cmdc`），缺失时回退 npx。COMMAND_CODE_PROBE_CMD
// 可显式覆盖（按空格切分）。
func commandCodeACPCommand(t *testing.T) (string, []string) {
	t.Helper()
	if override := os.Getenv("COMMAND_CODE_PROBE_CMD"); override != "" {
		parts := strings.Fields(override)
		require.NotEmpty(t, parts, "COMMAND_CODE_PROBE_CMD must not be empty")
		return parts[0], parts[1:]
	}
	// cmdc first: on Windows the bare `cmd` always resolves to the system shell,
	// so preferring it would make the probe spawn `cmd.exe acp` and hang. This
	// mirrors the spec's DefaultCmd choice for exactly the same reason.
	for _, bin := range []string{"cmdc", "commandcode", "cmd"} {
		if _, err := exec.LookPath(bin); err == nil {
			return bin, []string{"acp"}
		}
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skipf("Command Code CLI not installed (`cmd`) and npx unavailable; " +
			"run: npm i -g command-code")
	}
	return "npx", []string{"-y", commandCodeACPNpxSpec, "acp"}
}

// commandCodeAuthenticated 判断本机是否已具备 Command Code 凭据。凭据来源与
// CLI 自身一致（cli.mjs: getCommandAuthKey = env 优先，其次 ~/.commandcode/
// auth.json 的 apiKey）。
func commandCodeAuthenticated() bool {
	if strings.TrimSpace(os.Getenv("COMMAND_CODE_API_KEY")) != "" {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(home, ".commandcode", "auth.json"))
	if err != nil {
		return false
	}
	var parsed struct {
		APIKey string `json:"apiKey"`
	}
	if json.Unmarshal(b, &parsed) != nil {
		return false
	}
	return strings.TrimSpace(parsed.APIKey) != ""
}

// commandCodeRunEnv prepares an environment in which `cmd acp` will accept
// session/new, in priority order:
//
//  1. a real Command Code login / API key already present → use it as-is;
//  2. otherwise, if a BYOK upstream key is available, write the BYOK config and
//     install a placeholder gate key (no Command Code credits consumed);
//  3. otherwise skip.
//
// Returns the BYOK provider that was used (zero value when the real login was
// used). Auth-gated probes should call this so they run on a machine that only
// has a BYOK provider.
func commandCodeRunEnv(t *testing.T) (byok commandCodeBYOKProvider) {
	t.Helper()
	if commandCodeAuthenticated() {
		t.Log("using the existing Command Code login/API key")
		return commandCodeBYOKProvider{}
	}
	p := commandCodeBYOKMinimax
	if strings.TrimSpace(os.Getenv(p.APIKeyEnv)) == "" {
		t.Skipf("Command Code not authenticated and %s not set — cannot run auth-gated "+
			"probe; run `cmd login` or provide a BYOK upstream key", p.APIKeyEnv)
	}
	withCommandCodeBYOKConfig(t, p)
	t.Setenv("COMMAND_CODE_API_KEY", "clawbench-byok-gate-placeholder")
	t.Logf("using BYOK fallback: provider=%s model=%s/%s",
		p.ID, p.ID, strings.ToLower(p.Model))
	return p
}

// ---------------------------------------------------------------------------
// 原始 JSON-RPC 通道
// ---------------------------------------------------------------------------

// ccRawProbe 在 acp-go-sdk 之外维护一条裸 JSON-RPC 通道，以便精确测量 prompt
// 时延、收集全部通知，并自动应答 agent→client 请求（权限 / 文件读写）以免探针
// 卡死。
type ccRawProbe struct {
	mu       sync.Mutex
	nextID   int
	pending  map[string]chan map[string]any
	writer   io.Writer        // agent stdin, for post-setup calls
	updates  []map[string]any // session/update params
	other    []string         // 其它 agent→client 通知 method
	requests []string         // agent→client 请求 method（已自动应答）
	buf      bytes.Buffer
}

func newCCRawProbe() *ccRawProbe {
	return &ccRawProbe{pending: map[string]chan map[string]any{}}
}

func (p *ccRawProbe) call(ctx context.Context, w io.Writer, method string, params any) (map[string]any, error) {
	p.mu.Lock()
	p.nextID++
	id := fmt.Sprintf("cc-%d", p.nextID)
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
func (p *ccRawProbe) dispatch(chunk []byte, w io.Writer) {
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
				params, _ := m["params"].(map[string]any)
				optionID := "allow-once"
				if list, ok := params["options"].([]any); ok && len(list) > 0 {
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
			if id, ok := rawID.(string); ok && strings.HasPrefix(id, "cc-") {
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

func (p *ccRawProbe) snapshot() (updates []map[string]any, other, requests []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.updates...),
		append([]string(nil), p.other...),
		append([]string(nil), p.requests...)
}

// updateKind 提取 sessionUpdate 判别式与文本。
func ccUpdateKind(params map[string]any) (kind, text string) {
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

// startCCRawProbe 启动 `cmd acp` 并完成 initialize（+ 可选 session/new）。
// 返回裸通道与 initialize 的原始响应；session/new 失败时 sessionID 为空
// （未登录场景），由调用方决定。
func startCCRawProbe(t *testing.T, ctx context.Context, bin string, args []string, cwd string) (*ccRawProbe, string, map[string]any) {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)
	// Command Code is commonly launched through `npx`, which spawns the real CLI
	// as a child. Killing only npx leaves that child alive holding the stdout
	// pipe open, so cmd.Wait() hangs forever (observed: probe passed every
	// assertion, then the test timed out in cleanup). setProcessGroup puts the
	// whole tree in its own session/process group so killProcessGroup reaps it.
	setProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	require.NoError(t, cmd.Start(), "spawn %s %s", bin, strings.Join(args, " "))
	t.Cleanup(func() {
		if cmd.Process != nil {
			killProcessGroup(cmd.Process)
			_ = cmd.Wait()
		}
		if t.Failed() && stderrBuf.Len() > 0 {
			t.Logf("agent stderr: %s", truncate(stderrBuf.String(), 2000))
		}
	})

	probe := newCCRawProbe()
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
		"clientInfo": map[string]any{"name": "clawbench-commandcode-probe", "version": "1.0.0"},
	})
	require.NoError(t, err, "initialize")
	t.Logf("initialize response: %s", jsonStr(initResp))
	initResult, _ := initResp["result"].(map[string]any)

	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	sessResp, err := probe.call(newCtx, stdin, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		t.Logf("session/new: no response (%v)", err)
		return probe, "", initResult
	}
	if code, msg, isErr := extractJSONRPCError(sessResp); isErr {
		t.Logf("session/new error: code=%v msg=%q (raw=%s)", code, msg, jsonStr(sessResp))
		return probe, "", initResult
	}
	t.Logf("session/new response: %s", jsonStr(sessResp))
	result, _ := sessResp["result"].(map[string]any)
	sessionID, _ := result["sessionId"].(string)
	require.NotEmpty(t, sessionID, "session/new must return a sessionId")

	// modes / models / configOptions 能力面（供 L3 使用）。
	logCCSessionState(t, result)
	return probe, sessionID, initResult
}

// logCCSessionState 打印 session/new 里的 modes / models / configOptions。
// 注意：SDK v0.13.5 的 NewSessionResponse **没有 Models 字段**，所以这些必须
// 走裸通道读（这也是本探针自建通道的原因之一）。
func logCCSessionState(t *testing.T, result map[string]any) {
	t.Helper()
	if modes, ok := result["modes"].(map[string]any); ok {
		var ids []string
		if list, ok := modes["availableModes"].([]any); ok {
			for _, mo := range list {
				if mm, ok := mo.(map[string]any); ok {
					ids = append(ids, fmt.Sprint(mm["id"]))
				}
			}
		}
		t.Logf("advertised modes: %v (current=%v)", ids, modes["currentModeId"])
	}
	if models, ok := result["models"].(map[string]any); ok {
		list, _ := models["availableModels"].([]any)
		t.Logf("advertised models: %d (current=%v)", len(list), models["currentModelId"])
		for i, raw := range list {
			if i >= 5 {
				t.Logf("  ... (%d more)", len(list)-5)
				break
			}
			t.Logf("  model[%d]: %s", i, jsonStr(raw))
		}
	}
	if cfg, ok := result["configOptions"].([]any); ok {
		for _, raw := range cfg {
			if c, ok := raw.(map[string]any); ok {
				t.Logf("configOption id=%v category=%v current=%v options=%v",
					c["id"], c["category"], c["currentValue"], len(jsonList(c["options"])))
			}
		}
	}
}

func jsonList(v any) []any {
	l, _ := v.([]any)
	return l
}

// ---------------------------------------------------------------------------
// L0 — BYOK：用自备 provider（如 minimax）驱动 ACP，不花 Command Code 额度
// ---------------------------------------------------------------------------
//
// 背景：Command Code 的 ACP 鉴权门只检查 `getCommandAuthKey`（`COMMAND_CODE_API_KEY`
// 或 ~/.commandcode/auth.json 的 apiKey）**非空**，不校验有效性。因此可以放一个
// 占位 key 过闸，而真实推理走自备 provider（BYOK）。
//
// BYOK 需要两份文件（**实测确认，缺一不可**）：
//
//	~/.commandcode/providers.json   ← 声明 provider（注意顶层键是 "provider"）
//	~/.commandcode/config.json      ← 指定默认 model + modelProvider
//
// providers.json 形状（实测有效）：
//
//	{ "provider": { "minimax": {
//	    "api": "anthropic-messages",              // 或 openai-completions / openai-responses
//	    "baseURL": "https://api.minimaxi.com/anthropic",
//	    "apiKey": "$MINIMAX_API_KEY",             // 支持 $ENV / {env:VAR} / !command 引用
//	    "models": { "MiniMax-M2.7": { "name": "MiniMax M2.7" } } } } }
//
// config.json 形状：
//
//	{ "installed": true, "model": "minimax/minimax-m2.7", "modelProvider": "minimax" }
//
// 关键坑（这是整条路径最容易踩的地方）：
//
//  1. **ACP 的模型列表锁死在 Command Code 自带 catalog**。session/new 的
//     `configOptions[model].options` 恒为 CC 的 92 个网关模型，BYOK 模型**不会**
//     出现（cli.mjs: availableModels(){return catalogModels()}）。所以
//     `session/set_model` 传 BYOK 模型 id 会回 `-32602 Unknown model`。
//  2. **provider 绑定发生在会话创建时，且只认 config.json**：
//     `startingModel()` → `getConfiguredModel()`（非 resume 分支），
//     `providerForModel(modelId)` 要求模型在 CC catalog 里、且 lane 支持其 provider
//     —— BYOK 模型不在 catalog 中，故 providerForModel 返回 undefined → 回落到
//     Command Code 网关（实测：`POST /alpha/generate → 401`）。
//  3. 因此 **BYOK 在 ACP 下只能靠 config.json 的 `model` 预置**，无法在会话内切换；
//     切模型（`/model` 选择器）会把会话拉回 CC 网关模型。
//  4. `CMD_LOCAL_ONLY=1` **不能**与 ACP 同用：它会拦截一切 CC API 调用，连
//     taste-learning 都会失败并把错误文本当成 agent_message_chunk 流出来。
//
// 结论：ClawBench 若要接 BYOK，正确姿势是 **每个 BYOK provider 各起一个 cmd 进程
// 并预写 config.json**（或按会话切换 config），而不是靠 ACP 的模型选择器。

// commandCodeBYOKProvider 描述一个 BYOK provider 的测试配置。
type commandCodeBYOKProvider struct {
	// ID 是 providers.json / config.json 里的 provider 键名。
	ID string
	// Model 是 models 里声明的模型 id（不含 provider 前缀）。
	Model string
	// APIFlavor 是 api 字段（anthropic-messages / openai-completions / openai-responses）。
	APIFlavor string
	// BaseURL 是上游端点。
	BaseURL string
	// APIKeyEnv 是持有真实密钥的环境变量名（providers.json 用 "$NAME" 引用）。
	APIKeyEnv string
}

// commandCodeBYOKMinimax 是默认 BYOK 目标：MiniMax 的 Anthropic 兼容端点。
// 与 ClawBench 的 provider_registry.go 中 "minimax-cn" 指向同一上游。
var commandCodeBYOKMinimax = commandCodeBYOKProvider{
	ID:        "minimax",
	Model:     "MiniMax-M2.7",
	APIFlavor: "anthropic-messages",
	BaseURL:   "https://api.minimaxi.com/anthropic",
	APIKeyEnv: "MINIMAX_API_KEY",
}

// requireCommandCodeBYOK skips unless the BYOK provider's key is present.
func requireCommandCodeBYOK(t *testing.T, p commandCodeBYOKProvider) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(p.APIKeyEnv)) == "" {
		t.Skipf("%s not set — BYOK probe needs a real upstream key; skipping", p.APIKeyEnv)
	}
}

// withCommandCodeBYOKConfig points the agent at a throwaway HOME, writes
// providers.json + config.json there, and copies the existing auth.json across
// so a real login still works.
//
// Why a temp HOME instead of writing the real ~/.commandcode and restoring it:
// the restore runs in t.Cleanup, which does NOT run on SIGKILL or a hard panic
// (the probe spawns agents and has timeouts). A crashed run would then leave the
// developer's real Command Code config pointing at a test provider — silently
// changing which upstream their `cmd` uses. A private HOME makes the blast
// radius the test process itself.
//
// COMMAND_CODE_API_KEY is set separately by the caller, so auth.json is only
// needed when the probe runs against a real login.
func withCommandCodeBYOKConfig(t *testing.T, p commandCodeBYOKProvider) {
	t.Helper()
	realHome, err := os.UserHomeDir()
	require.NoError(t, err, "resolve real home dir")

	home := t.TempDir()
	t.Setenv("HOME", home)        // unix
	t.Setenv("USERPROFILE", home) // windows
	dir := filepath.Join(home, ".commandcode")
	require.NoError(t, os.MkdirAll(dir, 0o755), "create %s", dir)

	// Carry over the real login so a `cmd login` on this machine still applies.
	if b, err := os.ReadFile(filepath.Join(realHome, ".commandcode", "auth.json")); err == nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), b, 0o600),
			"copy auth.json into the isolated HOME")
	}

	providers := map[string]any{
		"provider": map[string]any{
			p.ID: map[string]any{
				"name":    p.ID + " (clawbench probe)",
				"api":     p.APIFlavor,
				"baseURL": p.BaseURL,
				"apiKey":  "$" + p.APIKeyEnv,
				"models": map[string]any{
					p.Model: map[string]any{"name": p.Model},
				},
			},
		},
	}
	providersJSON, err := json.MarshalIndent(providers, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "providers.json"), providersJSON, 0o600),
		"write providers.json")

	// config.json 的 model 用 "<provider>/<model>" 形式（--list-models 的展示形式）。
	cfg := map[string]any{
		"installed":     true,
		"model":         p.ID + "/" + strings.ToLower(p.Model),
		"modelProvider": p.ID,
	}
	cfgJSON, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), cfgJSON, 0o600),
		"write config.json")

	t.Logf("BYOK config written to isolated HOME %s: provider=%s model=%s/%s baseURL=%s",
		home, p.ID, p.ID, strings.ToLower(p.Model), p.BaseURL)
}

// TestCommandCodeACPProbe_L0_BYOK 验证「占位 key 过闸 + BYOK provider 真实推理」这条
// 路径在 ACP 下成立。这是把 Command Code 接进 ClawBench 时唯一可行的免额度用法。
//
// 注意：这里刻意**不设** CMD_LOCAL_ONLY（它会拦掉一切 CC API 调用，见文件头注释）。
func TestCommandCodeACPProbe_L0_BYOK(t *testing.T) {
	p := commandCodeBYOKMinimax
	requireCommandCodeBYOK(t, p)
	bin, args := commandCodeACPCommand(t)

	withCommandCodeBYOKConfig(t, p)

	// 占位 key 只为过 ACP 鉴权闸（判据只是"非空"）；真实密钥由 providers.json
	// 的 "$MINIMAX_API_KEY" 从环境解析。
	t.Setenv("COMMAND_CODE_API_KEY", "clawbench-byok-gate-placeholder")

	ctx, cancel := contextWithTimeout(t, 480*time.Second)
	defer cancel()

	probe, sessionID := startCCBYOKProbe(t, ctx, bin, args)
	require.NotEmpty(t, sessionID, "L0-BYOK FAILED: session/new did not return a sessionId")

	// 硬前提：会话的当前模型必须是 BYOK 模型（证明 config.json 生效）。
	cur := ccSessionCurrentModel(t, probe, ctx)
	require.Equal(t, p.ID+"/"+strings.ToLower(p.Model), cur,
		"L0-BYOK FAILED: session started on %q, not the BYOK model — provider binding "+
			"did not take effect (config.json model/modelProvider)", cur)

	promptCtx, promptCancel := context.WithTimeout(ctx, 300*time.Second)
	defer promptCancel()
	start := time.Now()
	promptResp, err := probe.call(promptCtx, probe.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": commandCodeBYOKPrompt}},
	})
	require.NoError(t, err, "L0-BYOK FAILED: session/prompt produced no response")
	t.Logf("BYOK prompt resolved in %s: %s", time.Since(start), jsonStr(promptResp))

	time.Sleep(commandCodeACPNotifyWait)
	updates, _, _ := probe.snapshot()
	var text strings.Builder
	for _, u := range updates {
		_, txt := ccUpdateKind(u)
		text.WriteString(txt)
	}
	require.Contains(t, text.String(), commandCodeBYOKWant,
		"L0-BYOK FAILED: BYOK model did not return the expected token (got %.300q)", text.String())

	result, _ := promptResp["result"].(map[string]any)
	require.Equal(t, "end_turn", fmt.Sprint(result["stopReason"]),
		"L0-BYOK FAILED: prompt must end with stopReason=end_turn")
	t.Logf("L0-BYOK CONFIRMED: ACP ran on BYOK provider %s/%s with a placeholder gate key "+
		"(no Command Code credits consumed)", p.ID, strings.ToLower(p.Model))
}

// commandCodeBYOKPrompt 是 BYOK 回合的最小提示词（不要求工具调用，避免上游差异）。
const (
	commandCodeBYOKPrompt = "Reply with exactly this token and nothing else: COMMANDCODE-BYOK-OK"
	commandCodeBYOKWant   = "COMMANDCODE-BYOK-OK"
)

// startCCBYOKProbe 启动 ACP 并完成 initialize + session/new，返回通道与会话 id。
func startCCBYOKProbe(t *testing.T, ctx context.Context, bin string, args []string) (*ccRawProbe, string) {
	t.Helper()
	probe, sessionID, _ := startCCRawProbe(t, ctx, bin, args, acpTestWorkDir())
	require.NotEmpty(t, sessionID,
		"session/new failed — check the BYOK config and that the gate key is set")
	return probe, sessionID
}

// ccSessionCurrentModel 通过一次 session/new 读取当前模型（configOptions[model]
// 的 currentValue）。ACP 的模型**列表**恒为 CC catalog，但 currentValue 会反映
// config.json 的预置模型。
func ccSessionCurrentModel(t *testing.T, probe *ccRawProbe, ctx context.Context) string {
	t.Helper()
	newCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := probe.call(newCtx, probe.writer, "session/new", map[string]any{
		"cwd": acpTestWorkDir(), "mcpServers": []any{},
	})
	require.NoError(t, err, "session/new (model read)")
	res, _ := resp["result"].(map[string]any)
	for _, raw := range jsonList(res["configOptions"]) {
		c, _ := raw.(map[string]any)
		if c["id"] == "model" {
			cur, _ := c["currentValue"].(string)
			return cur
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// L1 — 原始 JSON-RPC 直连 `cmd acp`
// ---------------------------------------------------------------------------

// TestCommandCodeACPProbe_L1_Initialize 验证 initialize 的能力面与鉴权方法。
// 这一层**不需要登录**（cli.mjs: handleInitialize 不查 isAuthenticated），因此
// 它是唯一在未登录机器上也会真正跑完的探针点。
func TestCommandCodeACPProbe_L1_Initialize(t *testing.T) {
	bin, args := commandCodeACPCommand(t)
	cwd := acpTestWorkDir()

	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	probe, _, initRes := startCCRawProbe(t, ctx, bin, args, cwd)
	require.NotNil(t, probe)
	require.NotNil(t, initRes, "L1-Init FAILED: initialize returned no result object")

	// 硬前提 1：loadSession —— ClawBench 的 acp-load / acp-sync 端点依赖它。
	// 已实测 v1.84.0 广告 loadSession:true。
	caps, _ := initRes["agentCapabilities"].(map[string]any)
	require.NotNil(t, caps, "L1-Init FAILED: no agentCapabilities in initialize result")
	require.Equal(t, true, caps["loadSession"],
		"L1-Init FAILED: loadSession not advertised — acp-load/acp-sync cannot work")

	// 硬前提 2：authMethods 非空（否则 ClawBench 无法提示用户如何登录）。
	auths, _ := initRes["authMethods"].([]any)
	require.NotEmpty(t, auths, "L1-Init FAILED: no authMethods advertised")
	for _, raw := range auths {
		a, _ := raw.(map[string]any)
		t.Logf("authMethod id=%v name=%v", a["id"], a["name"])
	}

	// 观测项：promptCapabilities / mcpCapabilities / sessionCapabilities。
	t.Logf("agentCapabilities: %s", jsonStr(caps))
	if sc, ok := caps["sessionCapabilities"].(map[string]any); ok {
		var keys []string
		for k := range sc {
			keys = append(keys, k)
		}
		t.Logf("sessionCapabilities keys: %v", keys)
	}
	if pc, ok := caps["promptCapabilities"].(map[string]any); ok {
		t.Logf("promptCapabilities: %s", jsonStr(pc))
		// 观测项（非硬断言）：audio=false 是官方文档明示的限制。
		if audio, ok := pc["audio"].(bool); ok {
			t.Logf("audio support: %v (Command Code 官方文档明示不支持音频)", audio)
		}
	}
	t.Log("L1-Init CONFIRMED: initialize advertises loadSession + authMethods")
}

// TestCommandCodeACPProbe_L1_AuthGate 记录**未登录**时 session/new 的行为。
// 有凭据时跳过（该分支不可达），无凭据时把 -32000 AUTH_REQUIRED 钉成事实。
func TestCommandCodeACPProbe_L1_AuthGate(t *testing.T) {
	if commandCodeAuthenticated() {
		t.Skip("Command Code is authenticated on this machine — the unauthenticated " +
			"branch (AUTH_REQUIRED) is unreachable; skipping")
	}
	bin, args := commandCodeACPCommand(t)
	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	probe, _, _ := startCCRawProbe(t, ctx, bin, args, acpTestWorkDir())
	resp, err := probe.call(ctx, probe.writer, "session/new", map[string]any{
		"cwd": acpTestWorkDir(), "mcpServers": []any{},
	})
	require.NoError(t, err, "L1-AuthGate FAILED: session/new produced no response at all")
	code, msg, isErr := extractJSONRPCError(resp)
	require.True(t, isErr, "L1-AuthGate FAILED: unauthenticated session/new should be a JSON-RPC error, got %s",
		jsonStr(resp))
	require.Equal(t, commandCodeACPAuthRequired, code,
		"L1-AuthGate: expected AUTH_REQUIRED (-32000), got code=%v msg=%q", code, msg)
	t.Logf("L1-AuthGate CONFIRMED: unauthenticated session/new → -32000 %q", msg)
	t.Log("L1-AuthGate NOTE: ClawBench 必须把 -32000 映射成「请先 cmd login」的用户提示，" +
		"而不是通用的后端错误——它与 -32601/-32602 语义完全不同")
}

// TestCommandCodeACPProbe_L1_RawTurn 直连 `cmd acp` 跑一个带工具调用的回合。
func TestCommandCodeACPProbe_L1_RawTurn(t *testing.T) {
	commandCodeRunEnv(t)
	bin, args := commandCodeACPCommand(t)
	cwd := acpTestWorkDir()

	ctx, cancel := contextWithTimeout(t, 480*time.Second)
	defer cancel()

	probe, sessionID, _ := startCCRawProbe(t, ctx, bin, args, cwd)
	require.NotEmpty(t, sessionID, "L1-Turn FAILED: session/new did not return a sessionId")

	promptCtx, promptCancel := context.WithTimeout(ctx, 300*time.Second)
	defer promptCancel()
	start := time.Now()
	promptResp, err := probe.call(promptCtx, probe.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": commandCodeACPProbePrompt}},
	})
	require.NoError(t, err, "L1-Turn FAILED: session/prompt produced no response")
	t.Logf("session/prompt resolved in %s: %s", time.Since(start), jsonStr(promptResp))

	time.Sleep(commandCodeACPNotifyWait)
	updates, other, requests := probe.snapshot()

	t.Logf("notifications: %d session/update, other=%v, agent→client requests=%v",
		len(updates), other, requests)
	var text strings.Builder
	kinds := map[string]int{}
	for i, u := range updates {
		kind, txt := ccUpdateKind(u)
		kinds[kind]++
		if i < 15 {
			t.Logf("  update[%d] kind=%q text=%.120q", i, kind, txt)
		}
		text.WriteString(txt)
	}
	t.Logf("update kinds: %v", kinds)
	t.Logf("concatenated agent text: %.200q", text.String())

	// 观测项：tool_call 的 title 形状。Command Code 的 toolTitleFor 生成
	// "<label>: <value>"（如 "Read: /path/to/file"），与 Hermes 的
	// "<native>: <preview>" 同型 —— 意味着 ClawBench 可能需要 ToolTitlePrefixes
	// 而不是 ToolCallIDPrefixes 来还原工具名。
	for _, u := range updates {
		up, _ := u["update"].(map[string]any)
		if up == nil {
			continue
		}
		if up["sessionUpdate"] == "tool_call" || up["sessionUpdate"] == "tool_call_update" {
			t.Logf("  tool update: toolCallId=%v title=%q kind=%v status=%v",
				up["toolCallId"], up["title"], up["kind"], up["status"])
		}
	}

	result, _ := promptResp["result"].(map[string]any)
	require.Equal(t, "end_turn", fmt.Sprint(result["stopReason"]),
		"L1-Turn FAILED: prompt must end with stopReason=end_turn")
	require.NotEmpty(t, updates,
		"L1-Turn FAILED: `cmd acp` emitted ZERO session/update notifications — no assistant output")
	require.NotEmpty(t, kinds["agent_message_chunk"],
		"L1-Turn FAILED: no agent_message_chunk updates (kinds=%v)", kinds)
	require.Contains(t, text.String(), commandCodeACPProbeWant,
		"L1-Turn FAILED: agent text did not contain the expected token")
}

// ---------------------------------------------------------------------------
// L2 — 经 ClawBench 生产链路（ACPBackend.ExecuteStream）
// ---------------------------------------------------------------------------

// TestCommandCodeACPProbe_L2_Backend drives `cmd acp` through the exact path
// ClawBench uses in production: ACPConnManager → ACPBackend.ExecuteStream →
// ClawBenchACPClient → StreamEvent channel.
func TestCommandCodeACPProbe_L2_Backend(t *testing.T) {
	commandCodeRunEnv(t)
	bin, args := commandCodeACPCommand(t)

	agent := &model.Agent{
		ID:         "commandcode-probe",
		Name:       "Command Code Probe",
		Backend:    "commandcode",
		Transport:  "acp-stdio",
		AcpCommand: strings.Join(append([]string{bin}, args...), " "),
	}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err, "NewACPBackend")

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	// 工具调用回合需要放行权限；否则回合会停在 request_permission 上。
	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	events := sendACPPrompt(t, backend, sessionID, commandCodeACPProbePrompt, 300*time.Second)

	t.Logf("L2 stream event types (%d): %v", len(events), acpEventTypes(events))
	for _, e := range events {
		switch e.Type {
		case "content", "thinking":
			t.Logf("  L2 %s: %.120q", e.Type, e.Content)
		case "error":
			t.Logf("  L2 ERROR: %.300q", e.Content)
		case "session_capture", "mode_update", "model_list_update", "commands_update", "config_update":
			t.Logf("  L2 %s: %.200q", e.Type, e.Content)
		}
	}
	content := concatACPContent(events)
	t.Logf("L2 concatenated content: %.200q", content)

	requireDoneEvent(t, events)
	requireNoErrorEvents(t, events)
	require.NotEmpty(t, content, "L2 FAILED: no assistant content reached the StreamEvent channel")
	require.Contains(t, content, commandCodeACPProbeWant,
		"L2 FAILED: streamed content did not contain the expected token")
}

// ---------------------------------------------------------------------------
// L3 — Command Code 独有约束
// ---------------------------------------------------------------------------

// TestCommandCodeACPProbe_L3_CwdLease 验证「一个 cmd acp 进程只服务一个项目
// 目录」这条硬约束（cli.mjs: createCwdLease + createAgentSession 的
// `This cmd acp process serves <held>; start another for <cwd>`）。
//
// 为什么关键：ClawBench 的连接池是**按 ClawBench 会话 id** 建连接的
// （ACPConnManager.conns[clawbenchSID]），一个 agent 的多个会话会各自 spawn
// 一个 cmd 进程。同 cwd 没问题；但只要两个会话的 cwd 不同（多项目工作台是
// 常态），每个进程仍各自服务自己的目录，故本约束**不会**被触发 —— 需要实测
// 确认「不同进程互不影响」以及「同一进程内不同 cwd 被拒」。
func TestCommandCodeACPProbe_L3_CwdLease(t *testing.T) {
	commandCodeRunEnv(t)
	bin, args := commandCodeACPCommand(t)

	base := acpTestWorkDir()
	other := filepath.Join(t.TempDir(), "commandcode-lease-other")
	require.NoError(t, os.MkdirAll(other, 0o755), "create second cwd")

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	probe, sessionID, _ := startCCRawProbe(t, ctx, bin, args, base)
	require.NotEmpty(t, sessionID, "L3-Lease FAILED: initial session/new did not return a sessionId")

	// (a) 同一进程、**相同** cwd 的第二个会话应被接受（lease 是计数式的，
	// cli.mjs: acquire 命中同一 canonicalCwd 时 n+=1 并复用）。
	sameCtx, sameCancel := context.WithTimeout(ctx, 60*time.Second)
	defer sameCancel()
	sameResp, err := probe.call(sameCtx, probe.writer, "session/new", map[string]any{
		"cwd": base, "mcpServers": []any{},
	})
	require.NoError(t, err, "L3-Lease FAILED: no response to same-cwd session/new")
	if code, msg, isErr := extractJSONRPCError(sameResp); isErr {
		t.Errorf("L3-Lease FAILED: same-cwd session/new was rejected (code=%v msg=%q) — "+
			"Command Code 的 cwd lease 是计数式，同目录必须可复用", code, msg)
	} else {
		res, _ := sameResp["result"].(map[string]any)
		t.Logf("L3-Lease CONFIRMED: same-cwd second session accepted (id=%v)", res["sessionId"])
	}

	// (b) 同一进程、**不同** cwd 的会话应被拒（INVALID_PARAMS，-32602）。
	diffCtx, diffCancel := context.WithTimeout(ctx, 60*time.Second)
	defer diffCancel()
	diffResp, err := probe.call(diffCtx, probe.writer, "session/new", map[string]any{
		"cwd": other, "mcpServers": []any{},
	})
	require.NoError(t, err, "L3-Lease FAILED: no response to different-cwd session/new")
	code, msg, isErr := extractJSONRPCError(diffResp)
	require.True(t, isErr,
		"L3-Lease FAILED: different-cwd session/new should be rejected by the cwd lease, got %s",
		jsonStr(diffResp))
	require.Equal(t, float64(-32602), code,
		"L3-Lease: expected INVALID_PARAMS (-32602) for cwd conflict, got code=%v msg=%q", code, msg)
	require.Contains(t, msg, "start another for",
		"L3-Lease: cwd-lease rejection message changed shape: %q", msg)
	t.Logf("L3-Lease CONFIRMED: different-cwd session/new rejected → -32602 %q", msg)

	// (c) 相对 cwd 应被拒（openSession 要求绝对路径）。
	relCtx, relCancel := context.WithTimeout(ctx, 60*time.Second)
	defer relCancel()
	relResp, err := probe.call(relCtx, probe.writer, "session/new", map[string]any{
		"cwd": "relative/dir", "mcpServers": []any{},
	})
	if err == nil {
		if rcode, rmsg, rIsErr := extractJSONRPCError(relResp); rIsErr {
			t.Logf("L3-Lease CONFIRMED: relative cwd rejected → code=%v msg=%q", rcode, rmsg)
		} else {
			t.Errorf("L3-Lease FAILED: relative cwd was accepted — ClawBench 必须始终传绝对路径")
		}
	}
}

// TestCommandCodeACPProbe_L3_NoMidTurnInjection 验证 Command Code **没有**轮中
// 注入能力。这直接决定 ClawBench 的 `/ps`（运行中插话）在 Command Code 上不可用。
//
// 对照：CodeBuddy 用 session/steer（无 `_` 前缀）；claude/codex 用
// `_session/steering`。两者都不在 Command Code 的方法表里
// （cli.mjs: vn 常量 + dispatchRequest 的 m[method] 查表 → -32601）。
func TestCommandCodeACPProbe_L3_NoMidTurnInjection(t *testing.T) {
	commandCodeRunEnv(t)
	bin, args := commandCodeACPCommand(t)

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	probe, sessionID, _ := startCCRawProbe(t, ctx, bin, args, acpTestWorkDir())
	require.NotEmpty(t, sessionID, "L3-NoSteer FAILED: session/new did not return a sessionId")

	// 基线：确认裸通道真的到达了 dispatcher（未知方法回 -32601）。
	baseCtx, baseCancel := context.WithTimeout(ctx, 30*time.Second)
	defer baseCancel()
	baseResp, err := probe.call(baseCtx, probe.writer, "session/__no_such_method__", map[string]any{
		"sessionId": sessionID,
	})
	require.NoError(t, err, "L3-NoSteer FAILED: baseline unknown-method produced no response")
	if code, _, isErr := extractJSONRPCError(baseResp); !isErr || code != -32601 {
		t.Fatalf("L3-NoSteer FAILED: baseline unknown method should be -32601, got %s", jsonStr(baseResp))
	}
	t.Log("L3-NoSteer baseline CONFIRMED: unknown methods return -32601")

	for _, method := range []string{"session/steer", "_session/steering"} {
		callCtx, callCancel := context.WithTimeout(ctx, 30*time.Second)
		resp, err := probe.call(callCtx, probe.writer, method, map[string]any{
			"sessionId": sessionID,
			"prompt":    []any{map[string]any{"type": "text", "text": "CLAWBENCH-CC-STEER-PROBE"}},
		})
		callCancel()
		require.NoError(t, err, "L3-NoSteer FAILED: no response to %s", method)
		code, msg, isErr := extractJSONRPCError(resp)
		require.True(t, isErr && code == -32601,
			"L3-NoSteer FAILED: %s must be METHOD_NOT_FOUND (-32601), got %s", method, jsonStr(resp))
		t.Logf("L3-NoSteer CONFIRMED: %s → -32601 %q (Command Code 无轮中注入)", method, msg)
	}
	t.Log("L3-NoSteer CONCLUSION: Command Code 上 /ps 不可用；" +
		"ClawBench 的 MidTurnSplitsReply / insert 能力位对后端应判定为 false")
}

// TestCommandCodeACPProbe_L3_ConcurrentPromptAndSwitches 验证：
//   - 同会话并发 prompt 被拒（无 queue 语义）；
//   - set_mode / set_model 校验取值，且运行中切模型被拒。
func TestCommandCodeACPProbe_L3_ConcurrentPromptAndSwitches(t *testing.T) {
	commandCodeRunEnv(t)
	bin, args := commandCodeACPCommand(t)

	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	probe, sessionID, _ := startCCRawProbe(t, ctx, bin, args, acpTestWorkDir())
	require.NotEmpty(t, sessionID, "L3-Concurrent FAILED: session/new did not return a sessionId")

	// 长回合（三个真实 sleep），保证注入落在轮中。
	promptDone := make(chan struct{})
	var firstResp map[string]any
	var firstErr error
	go func() {
		firstResp, firstErr = probe.call(ctx, probe.writer, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []any{map[string]any{"type": "text", "text": "请分三步，每步之间必须真的调用终端工具执行命令：" +
				"1) 运行 `sleep 3` 并说明这是第一步；2) 运行 `sleep 3` 并说明这是第二步；3) 运行 `sleep 3` 并说明这是第三步。"}},
		})
		close(promptDone)
	}()

	select {
	case <-promptDone:
		t.Log("L3-Concurrent NOTE: first prompt finished before the second could be sent — " +
			"turn too fast; the concurrent-prompt branch could not be observed")
		require.NoError(t, firstErr, "first prompt error")
		return
	case <-time.After(6 * time.Second):
	}

	// (a) 并发 prompt → INVALID_REQUEST "A prompt is already running for this session"。
	ccCtx, ccCancel := context.WithTimeout(ctx, 30*time.Second)
	ccResp, err := probe.call(ccCtx, probe.writer, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]any{"type": "text", "text": "CLAWBENCH-CC-CONCURRENT"}},
	})
	ccCancel()
	require.NoError(t, err, "L3-Concurrent FAILED: no response to concurrent prompt")
	code, msg, isErr := extractJSONRPCError(ccResp)
	require.True(t, isErr, "L3-Concurrent FAILED: concurrent prompt should be rejected, got %s", jsonStr(ccResp))
	require.Equal(t, float64(-32600), code,
		"L3-Concurrent: expected INVALID_REQUEST (-32600), got code=%v msg=%q", code, msg)
	t.Logf("L3-Concurrent CONFIRMED: concurrent prompt rejected → -32600 %q", msg)
	t.Log("L3-Concurrent CONCLUSION: Command Code 无 queue/redirect 语义，" +
		"ClawBench 必须自己串行化同一会话的发送")

	// (b) 运行中切模型 → INVALID_REQUEST（switchModel 有 prompting 守卫）。
	if modelID := ccFirstNonDefaultModel(t, probe, ctx); modelID != "" {
		mCtx, mCancel := context.WithTimeout(ctx, 30*time.Second)
		mResp, mErr := probe.call(mCtx, probe.writer, "session/set_model", map[string]any{
			"sessionId": sessionID, "modelId": modelID,
		})
		mCancel()
		if mErr == nil {
			if mCode, mMsg, mIsErr := extractJSONRPCError(mResp); mIsErr {
				t.Logf("L3-Concurrent: set_model(%s) while prompting → code=%v msg=%q", modelID, mCode, mMsg)
			} else {
				t.Logf("L3-Concurrent NOTE: set_model accepted while prompting (%s) — "+
					"the prompting guard did not trigger this run", jsonStr(mResp))
			}
		}
	}

	// 等第一回合结束。
	select {
	case <-promptDone:
		require.NoError(t, firstErr, "the running turn must complete cleanly")
		res, _ := firstResp["result"].(map[string]any)
		t.Logf("L3-Concurrent: first turn stopReason=%v", res["stopReason"])
	case <-time.After(300 * time.Second):
		t.Fatal("L3-Concurrent FAILED: running turn did not complete within 300s")
	}

	// (c) 空闲后：未知 mode / 未知 model 都应被校验拒绝（-32602）。
	badModeCtx, badModeCancel := context.WithTimeout(ctx, 30*time.Second)
	badModeResp, _ := probe.call(badModeCtx, probe.writer, "session/set_mode", map[string]any{
		"sessionId": sessionID, "modeId": "clawbench-nonexistent-mode",
	})
	badModeCancel()
	if bCode, bMsg, bIsErr := extractJSONRPCError(badModeResp); bIsErr {
		t.Logf("L3-Concurrent CONFIRMED: unknown mode rejected → code=%v msg=%q", bCode, bMsg)
	} else {
		t.Logf("L3-Concurrent NOTE: unknown mode accepted (%s) — Command Code 未校验 mode", jsonStr(badModeResp))
	}

	badModelCtx, badModelCancel := context.WithTimeout(ctx, 30*time.Second)
	badModelResp, _ := probe.call(badModelCtx, probe.writer, "session/set_model", map[string]any{
		"sessionId": sessionID, "modelId": "clawbench-nonexistent-model",
	})
	badModelCancel()
	if bCode, bMsg, bIsErr := extractJSONRPCError(badModelResp); bIsErr {
		t.Logf("L3-Concurrent CONFIRMED: unknown model rejected → code=%v msg=%q", bCode, bMsg)
	} else {
		t.Errorf("L3-Concurrent FAILED: unknown model accepted — Command Code 会校验 model id "+
			"（switchModel: availableModels.some(...)），未拒绝说明行为变了: %s", jsonStr(badModelResp))
	}
}

// ccFirstNonDefaultModel 通过一次轻量的 session/new 读取广告模型列表，返回第一个
// 非 default 的 modelId（无则空串）。
func ccFirstNonDefaultModel(t *testing.T, probe *ccRawProbe, ctx context.Context) string {
	t.Helper()
	newCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := probe.call(newCtx, probe.writer, "session/new", map[string]any{
		"cwd": acpTestWorkDir(), "mcpServers": []any{},
	})
	if err != nil {
		return ""
	}
	res, _ := resp["result"].(map[string]any)
	models, _ := res["models"].(map[string]any)
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
