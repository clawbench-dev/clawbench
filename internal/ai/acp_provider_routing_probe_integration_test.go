//go:build integration

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// ACP 运行时 provider 路由 — 能力探测（claude / codex）
// ===========================================================================
//
// 目的：issue #340 提出「让跨厂商切模型变成真切换」。老方案要 per-service
// 配置目录 + 改写全局 config.toml / settings.json；但两个桥接适配器现在都已
// 内置**运行时路由**扩展，ClawBench 一个都没接。本探针在动任何生产代码之前，
// 用真实适配器进程实测这些扩展是否可用、wire 形状如何、能否真的把流量导到
// 一个自建端点。全部为**观测型**探针：事实写 t.Logf，硬断言只覆盖「必须成立
// 才能接入」的前提。
//
// 两条路径（两个适配器机制不同）：
//
//	claude —— `providers/set`（ACP unstable 方法）
//	    {providerId:"main", apiType:"anthropic", baseUrl, headers}
//	    内部 createEnvForProvider() → 子进程 env ANTHROPIC_BASE_URL +
//	    ANTHROPIC_CUSTOM_HEADERS + ANTHROPIC_AUTH_TOKEN="acp-proxy"。
//
//	codex  —— `authenticate` + methodId:"gateway"
//	    前提：客户端 Initialize 必须广告 clientCapabilities.auth._meta.gateway=true
//	    {methodId:"gateway", _meta:{gateway:{baseUrl, providerName, headers}}}
//	    内部 gatewayConfig → 合并进 [model_providers.custom-gateway]。
//
// 关键问题：
//
//	Q1  claude 是否广告 providers 能力？providers/list 返回什么？
//	Q2  providers/set 的**字段名**到底是什么？claude 适配器读 params.providerId，
//	    而 acp-go-sdk v0.13.5 的 UnstableSetProviderRequest 序列化成 "id" —— 若真
//	    不一致，Go SDK 的类型化方法会直接失败，必须走裸通道或改 struct。
//	Q3  codex 广告 gateway authMethod 是否需要客户端先广告 auth._meta.gateway？
//	Q4  codex 的 authenticate(gateway) 是否被接受（非 -32601）？
//	Q5  **决定性**：设置路由后，agent 的实际 HTTP 流量是否打到我们指定的
//	    baseUrl？（用本地 mock server 观测）——这是整个特性可行性的唯一硬前提。
//
// 运行：
//
//	go test -tags integration -v -run 'TestACPProviderRoutingProbe' \
//	    -timeout 900s ./internal/ai/
//
// 需要本机安装 claude / codex 且已登录（gateway 路径会绕过登录，但初始化
// 阶段仍可能需要 CLI 存在）。

// providerProbeBackend 描述一个待探测的桥接适配器及其路由机制。
type providerProbeBackend struct {
	ID   string
	Args []string // spawn 命令 + 参数
	CLI  string   // 底层 CLI（可用性检查）
	Mode string   // "providers-set" | "gateway-auth"
}

var providerProbeBackends = []providerProbeBackend{
	{
		ID:   "claude",
		Args: []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp@latest"},
		CLI:  "claude",
		Mode: "providers-set",
	},
	{
		ID:   "codex",
		Args: []string{"npx", "-y", "@agentclientprotocol/codex-acp@latest"},
		CLI:  "codex",
		Mode: "gateway-auth",
	},
}

// providerProbeCaps 构造 Initialize 的客户端能力。advertiseGateway 决定是否
// 广告 auth._meta.gateway=true —— codex 的 getCodexAuthMethods 只在看到它时
// 才把 "gateway" 放进 authMethods（Q3 的核心变量）。
func providerProbeCaps(advertiseGateway bool) acp.ClientCapabilities {
	caps := acp.ClientCapabilities{
		Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		Terminal: true,
	}
	if advertiseGateway {
		caps.Auth = acp.AuthCapabilities{
			Meta: map[string]any{"gateway": true},
		}
	}
	return caps
}

// startProviderProbe 启动桥接适配器并完成 initialize（**不建会话**，便于在
// 建会话之前先设 provider 路由 —— claude 的 providers/set 只对「之后创建或
// 加载的会话」生效）。复用 codebuddy 探针的裸 JSON-RPC 通道。
func startProviderProbe(t *testing.T, ctx context.Context, args []string, caps acp.ClientCapabilities, clientName string) *extProbeSetup {
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

	require.NoError(t, cmd.Start(), "spawn %s", args[0])
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	raw := &extProbeRawConn{pending: map[string]chan map[string]any{}}
	setup := &extProbeSetup{conn: conn, client: client, raw: raw, writer: writer, reader: reader}
	setup.pumpProbeResponses(ctx)

	initCtx, initCancel := context.WithTimeout(ctx, 90*time.Second)
	defer initCancel()
	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
		ClientInfo:         &acp.Implementation{Name: clientName, Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	// 用原始 wire 看 typed struct 会丢弃的字段（providers / auth._meta.gateway）。
	t.Logf("initialize REQUEST (raw): %s", findRawRequest(t, writer.buf.Bytes(), "initialize"))
	t.Logf("initialize RESPONSE (raw): %s", findRawResponse(t, reader.snapshot(), "agentCapabilities"))
	t.Logf("authMethods (%d): %s", len(initResp.AuthMethods), jsonStr(initResp.AuthMethods))

	return setup
}

// newProviderProbeSession 在已 initialize 的连接上建一个会话。
func newProviderProbeSession(t *testing.T, ctx context.Context, setup *extProbeSetup) {
	t.Helper()
	newCtx, newCancel := context.WithTimeout(ctx, 90*time.Second)
	defer newCancel()
	newResp, err := setup.conn.NewSession(newCtx, acp.NewSessionRequest{
		Cwd: acpTestWorkDir(), McpServers: []acp.McpServer{},
	})
	require.NoError(t, err, "session/new")
	setup.session = string(newResp.SessionId)
	cfgJSON, _ := json.Marshal(newResp.ConfigOptions)
	t.Logf("session/new configOptions: %s", cfgJSON)
}

// TestACPProviderRoutingProbe 是入口：逐后端跑各自的路由机制探测。
func TestACPProviderRoutingProbe(t *testing.T) {
	for _, b := range providerProbeBackends {
		b := b
		t.Run(b.ID, func(t *testing.T) {
			if _, err := exec.LookPath("npx"); err != nil {
				t.Skip("npx not available")
			}
			if _, err := exec.LookPath(b.CLI); err != nil {
				t.Skipf("%s CLI not available", b.CLI)
			}

			ctx, cancel := contextWithTimeout(t, 600*time.Second)
			defer cancel()

			switch b.Mode {
			case "providers-set":
				probeClaudeProvidersSet(t, ctx, b)
			case "gateway-auth":
				probeCodexGatewayAuth(t, ctx, b)
			default:
				t.Fatalf("unknown probe mode %q", b.Mode)
			}
		})
	}
}

// probeClaudeProvidersSet 探测 claude 的 providers/list + providers/set（Q1/Q2），
// 然后用本地 mock server 验证流量真的被导过去（Q5）。
func probeClaudeProvidersSet(t *testing.T, ctx context.Context, b providerProbeBackend) {
	setup := startProviderProbe(t, ctx, b.Args, providerProbeCaps(false), "clawbench-provider-probe")

	// Q1: 是否广告 providers 能力（原始 wire）。
	t.Log("──── Q1: capability advertisement ────")
	logWireLinesContaining(t, setup.reader.snapshot(), "providers")
	logWireLinesContaining(t, setup.reader.snapshot(), "providerId")

	// Q1b: providers/list。
	t.Log("──── Q1b: providers/list ────")
	listCtx, listCancel := context.WithTimeout(ctx, 30*time.Second)
	defer listCancel()
	listResp, err := setup.raw.callRaw(listCtx, setup.writer, "providers/list", map[string]any{})
	if err != nil {
		t.Fatalf("Q1b FAILED: no response to providers/list: %v", err)
	}
	if code, msg, isErr := extractJSONRPCError(listResp); isErr {
		t.Logf("Q1b providers/list error code=%v msg=%q raw=%s", code, msg, jsonStr(listResp))
	} else {
		t.Logf("Q1b providers/list OK: %s", jsonStr(listResp))
	}

	// Q2: providers/set 字段名 —— JS 适配器读 providerId，Go SDK 发 id。
	t.Log("──── Q2: providers/set field-name resolution ────")

	// (a) JS schema: providerId
	jsCtx, jsCancel := context.WithTimeout(ctx, 30*time.Second)
	defer jsCancel()
	jsResp, err := setup.raw.callRaw(jsCtx, setup.writer, "providers/set", map[string]any{
		"providerId": "main",
		"apiType":    "anthropic",
		"baseUrl":    "https://api.deepseek.com/anthropic",
		"headers":    map[string]string{"x-api-key": "probe-key-js"},
	})
	require.NoError(t, err, "Q2a FAILED: no response to providers/set (providerId)")
	if code, msg, isErr := extractJSONRPCError(jsResp); isErr {
		t.Logf("Q2a providers/set{providerId} REJECTED code=%v msg=%q raw=%s", code, msg, jsonStr(jsResp))
	} else {
		t.Logf("Q2a providers/set{providerId} ACCEPTED: %s", jsonStr(jsResp))
	}

	// (b) Go SDK schema: id
	idCtx, idCancel := context.WithTimeout(ctx, 30*time.Second)
	defer idCancel()
	idResp, err := setup.raw.callRaw(idCtx, setup.writer, "providers/set", map[string]any{
		"id":      "main",
		"apiType": "anthropic",
		"baseUrl": "https://api.deepseek.com/anthropic",
		"headers": map[string]string{"x-api-key": "probe-key-go"},
	})
	require.NoError(t, err, "Q2b FAILED: no response to providers/set (id)")
	if code, msg, isErr := extractJSONRPCError(idResp); isErr {
		t.Logf("Q2b providers/set{id} REJECTED code=%v msg=%q raw=%s "+
			"(=> acp-go-sdk v0.13.5 UnstableSetProviderRequest 的 json:\"id\" 与适配器期望的 providerId 不一致)",
			code, msg, jsonStr(idResp))
	} else {
		t.Logf("Q2b providers/set{id} ACCEPTED: %s", jsonStr(idResp))
	}

	// (c) Go SDK typed method —— 直接证明类型化调用是否可用。
	typedCtx, typedCancel := context.WithTimeout(ctx, 30*time.Second)
	defer typedCancel()
	_, typedErr := setup.conn.UnstableSetProvider(typedCtx, acp.UnstableSetProviderRequest{
		Id:      "main",
		ApiType: acp.UnstableLlmProtocolAnthropic,
		BaseUrl: "https://api.deepseek.com/anthropic",
		Headers: map[string]any{"x-api-key": "probe-key-typed"},
	})
	if typedErr != nil {
		t.Logf("Q2c typed UnstableSetProvider FAILED: %v", typedErr)
	} else {
		t.Logf("Q2c typed UnstableSetProvider OK")
	}

	// Q5: 路由是否真的生效 —— 指向本地 mock，看有没有请求打进来。
	probeRoutingToLocalMock(t, ctx, b, setup, "providers-set")
}

// probeCodexGatewayAuth 探测 codex 的 gateway auth 路径（Q3/Q4），
// 然后用本地 mock server 验证流量被导过去（Q5）。
func probeCodexGatewayAuth(t *testing.T, ctx context.Context, b providerProbeBackend) {
	// codex 必须先广告 auth._meta.gateway 才会把 gateway 放进 authMethods。
	setup := startProviderProbe(t, ctx, b.Args, providerProbeCaps(true), "clawbench-provider-probe")

	t.Log("──── Q3: gateway authMethod advertisement ────")
	logWireLinesContaining(t, setup.reader.snapshot(), "gateway")

	t.Log("──── Q4: authenticate(gateway) ────")
	authCtx, authCancel := context.WithTimeout(ctx, 30*time.Second)
	defer authCancel()
	authResp, err := setup.raw.callRaw(authCtx, setup.writer, "authenticate", map[string]any{
		"methodId": "gateway",
		"_meta": map[string]any{
			"gateway": map[string]any{
				"baseUrl":      "https://api.deepseek.com",
				"providerName": "clawbench-probe",
				"headers":      map[string]string{"Authorization": "Bearer probe-key"},
			},
		},
	})
	require.NoError(t, err, "Q4 FAILED: no response to authenticate(gateway)")
	if code, msg, isErr := extractJSONRPCError(authResp); isErr {
		t.Logf("Q4 authenticate(gateway) REJECTED code=%v msg=%q raw=%s", code, msg, jsonStr(authResp))
	} else {
		t.Logf("Q4 authenticate(gateway) ACCEPTED: %s", jsonStr(authResp))
	}

	probeRoutingToLocalMock(t, ctx, b, setup, "gateway-auth")
}

// mockUpstream 记录收到的任何 HTTP 请求，用于证明 agent 的流量被打到了我们
// 指定的 baseUrl（Q5 的唯一硬证据）。
type mockUpstream struct {
	mu       sync.Mutex
	requests []mockRequest
}

type mockRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

func (m *mockUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		m.mu.Lock()
		m.requests = append(m.requests, mockRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: r.Header.Clone(),
			Body:   string(body[:n]),
		})
		m.mu.Unlock()
		// 返回一个最小可解析的 200，避免 agent 立刻因协议错误崩溃。
		// 我们只需要"请求到达"这个事实，响应内容不参与判定。
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}
}

func (m *mockUpstream) snapshot() []mockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockRequest, len(m.requests))
	copy(out, m.requests)
	return out
}

// probeRoutingToLocalMock 把路由指向本地 mock，建会话、发起一个 prompt，然后
// 观测 mock 是否收到请求。这是整个特性可行性的决定性证据（Q5）。
func probeRoutingToLocalMock(t *testing.T, ctx context.Context, b providerProbeBackend, setup *extProbeSetup, mode string) {
	t.Helper()
	t.Log("──── Q5: routing to local mock (decisive) ────")

	mock := &mockUpstream{}
	srv := httptest.NewServer(mock.handler())
	defer srv.Close()
	t.Logf("local mock upstream listening at %s", srv.URL)

	// 1) 指向 mock。
	switch mode {
	case "providers-set":
		setCtx, setCancel := context.WithTimeout(ctx, 30*time.Second)
		defer setCancel()
		resp, err := setup.raw.callRaw(setCtx, setup.writer, "providers/set", map[string]any{
			"providerId": "main",
			"apiType":    "anthropic",
			"baseUrl":    srv.URL,
			"headers":    map[string]string{"x-api-key": "probe-key-mock"},
		})
		require.NoError(t, err, "Q5: providers/set to mock failed")
		if code, msg, isErr := extractJSONRPCError(resp); isErr {
			t.Fatalf("Q5 FAILED: providers/set to mock rejected code=%v msg=%q", code, msg)
		}
	case "gateway-auth":
		authCtx, authCancel := context.WithTimeout(ctx, 30*time.Second)
		defer authCancel()
		resp, err := setup.raw.callRaw(authCtx, setup.writer, "authenticate", map[string]any{
			"methodId": "gateway",
			"_meta": map[string]any{
				"gateway": map[string]any{
					"baseUrl":      srv.URL,
					"providerName": "clawbench-probe-mock",
					"headers":      map[string]string{"Authorization": "Bearer probe-key-mock"},
				},
			},
		})
		require.NoError(t, err, "Q5: authenticate(gateway) to mock failed")
		if code, msg, isErr := extractJSONRPCError(resp); isErr {
			t.Fatalf("Q5 FAILED: authenticate(gateway) to mock rejected code=%v msg=%q", code, msg)
		}
	}

	// 2) 建会话（claude 的 provider 配置只对之后创建的会话生效）。
	newProviderProbeSession(t, ctx, setup)
	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })
	streamCh := make(chan StreamEvent, 8192)
	registerProbeSession(t, setup, streamCh)

	// 3) 发起一个 prompt（不等待完成 —— 只要 mock 收到请求即证明路由生效）。
	promptCtx, promptCancel := context.WithCancel(ctx)
	defer promptCancel()
	go func() {
		_, _ = setup.conn.Prompt(promptCtx, acp.PromptRequest{
			SessionId: acp.SessionId(setup.session),
			Prompt:    []acp.ContentBlock{acp.TextBlock("Reply with exactly: CLAWBENCH-ROUTING-PROBE")},
		})
	}()

	// 4) 等 mock 收到请求，最多 60s。
	deadline := time.After(60 * time.Second)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Errorf("Q5 FAILED: local mock received NO request within 60s — routing to a custom "+
				"baseUrl did NOT take effect for %s (mode=%s). The runtime-routing extension is "+
				"either ignored or gated behind something we did not satisfy.", b.ID, mode)
			return
		case <-tick.C:
			if reqs := mock.snapshot(); len(reqs) > 0 {
				promptCancel()
				t.Logf("Q5 *** ROUTING WORKS *** — mock received %d request(s):", len(reqs))
				for i, r := range reqs {
					authHdr := r.Header.Get("Authorization")
					apiKeyHdr := r.Header.Get("x-api-key")
					t.Logf("    [%d] %s %s auth=%q x-api-key=%q body=%s",
						i, r.Method, r.Path, authHdr, apiKeyHdr, truncate(r.Body, 300))
				}
				return
			}
		}
	}
}

// mustJSON 序列化任意值为紧凑 JSON 字符串（失败时返回错误文本）。
func jsonStr(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<marshal error: " + err.Error() + ">"
	}
	return string(b)
}
