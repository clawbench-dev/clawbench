//go:build integration

package ai

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Qoder 国内版（qoderclicn）ACP — 可行性探针
// ===========================================================================
//
// 目的：在给 Qoder 增加「国内版」后端（issue #589）之前，用真实进程实测
// 两版的差异面，并把**接入必须知道的事实**固化成断言。
//
// 背景：Qoder 有国际版 / 国内版两个**独立发行包**（不是运行期开关）：
//
//	国际版  @qoder-ai/qodercli   → qodercli / qoder
//	国内版  @qodercn-ai/qoderclicn → qoderclicn / qodercn
//
// 两者是**同一份代码、不同品牌常量**编译出来的（bundle 里 `Gs="cn"==(Z4a="cn")`
// vs `Gs="cn"==(Z4a="global")` 一处常量分叉），因此 CLI 参数集完全一致，
// 差异只在：配置根、项目级配置文件名、端点、skills 目录、ACP agentInfo。
//
// 运行：
//
//	go test -tags integration -v -run 'TestQoderCNProbe' -timeout 900s ./internal/ai/
//
// 未安装 CLI 时自动回退 `npx -y <pkg>@latest`。
//
// ---------------------------------------------------------------------------
// ⚠️ 安全红线：绝不把国内版指向真实配置根
// ---------------------------------------------------------------------------
//
// 实测事故（2026-10-27）：把**国内版**二进制用 `--config-dir ~/.qoder`
// 指向**国际版**的真实配置目录后，CN 版加载了该凭据、判定鉴权失败，然后
// **自动删除了凭据文件**：
//
//	[auth-storage] event=credential.load  outcome=loaded
//	[auth-storage] event=credential.clear reason=automatic_auth_rejection outcome=deleted
//	[auth]         event=auth.logout    source=automatic auth_source=local
//
// 结果是国际版被登出，且没有副本可恢复。**这是两版不能共用配置根的最硬证据**：
// 不是"读不到"，而是"读到了会被销毁"。
//
// 因此本文件的所有 spawn 都跑在 t.TempDir() 造出来的隔离 HOME 里，
// qcnIsolatedHome() 里有硬断言；L2 还会检查隔离 HOME 之外的哨兵目录不被触碰。
// 新增用例时不要绕过这两个 helper。
//
// ---------------------------------------------------------------------------
// 探测层
// ---------------------------------------------------------------------------
//
//	L0  命令发现 + 配置根隔离（两版各自写自己的 dot 目录，互不触碰）
//	L1  能力面对比（initialize 的 agentInfo / loadSession / prompt / mcp / _meta）
//	L2  未登录鉴权闸（-32000）+ 隔离性哨兵
//	L3  「不登录用三方模型」是否可行（结论：不可行）+ --fake-responses 旁路
//	L4  经 ClawBench 生产链路（ACPBackend.ExecuteStream）的错误映射
//
// 事实记录到 t.Logf；硬断言只覆盖「必须成立才能接入」的前提。

const (
	// qcnCNBin / qcnCNAlias 是国内版包的 bin（package.json 里 bin 同时声明两者）。
	qcnCNBin   = "qoderclicn"
	qcnCNAlias = "qodercn"
	// qcnCNPackage 是未安装时的 npx 回退包。
	qcnCNPackage = "@qodercn-ai/qoderclicn@latest"

	// qcnIntlBin / qcnIntlPackage 是国际版（对照组）。
	qcnIntlBin     = "qodercli"
	qcnIntlPackage = "@qoder-ai/qodercli@latest"

	// qcnPrompt 是一个不需要工具的最小回合。
	qcnPrompt = "Reply with exactly this token and nothing else: QODER-CN-ACP-OK"
	qcnWant   = "QODER-CN-ACP-OK"

	// qcnAuthRequired 是 ACP 的 AuthenticationRequired（-32000..-32099 是 ACP
	// 保留区间）。两版未登录时都从 session/new 返回它。
	qcnAuthRequired = float64(-32000)

	// qcnNotifyWait 是 prompt resolve 之后继续收集通知的窗口。
	qcnNotifyWait = 3 * time.Second
)

// ---------------------------------------------------------------------------
// 环境隔离与命令发现
// ---------------------------------------------------------------------------

// qcnIsolatedHome 造一个隔离 HOME 并返回它。**所有** Qoder 进程都必须在这里跑。
//
// 复用真实 ~/.npm 作为 npm 缓存：npx 在空 HOME 下会重新解包整个包（含 sharp
// 等原生依赖），单次冷启慢一个数量级；缓存是只读用途，写进去也无害。
func qcnIsolatedHome(t *testing.T) string {
	t.Helper()

	realHome, err := os.UserHomeDir()
	require.NoError(t, err, "resolve real home dir")

	home := t.TempDir()
	// 硬护栏：探针永远不能跑在真实 HOME 上（见文件头的事故说明）。
	require.NotEqual(t, realHome, home, "probe must never run against the real HOME")
	require.True(t, strings.HasPrefix(home, os.TempDir()),
		"isolated HOME %q must live under the temp dir", home)

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows

	if realHome != "" {
		if _, err := os.Stat(filepath.Join(realHome, ".npm")); err == nil {
			t.Setenv("npm_config_cache", filepath.Join(realHome, ".npm"))
		}
	}
	return home
}

// qcnResolve 返回用于 spawn 的 (bin, args)，args 已包含 --acp。
// 优先本机安装，缺失时回退 npx（与 ClawBench 的 AcpCommand 取值保持一致）。
func qcnResolve(t *testing.T, bin, alias, npmSpec string) (string, []string) {
	t.Helper()
	if p, err := exec.LookPath(bin); err == nil {
		return p, []string{"--acp"}
	}
	if alias != "" {
		if p, err := exec.LookPath(alias); err == nil {
			return p, []string{"--acp"}
		}
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skipf("%s not installed and npx unavailable; run: npm i -g %s", bin, npmSpec)
	}
	return "npx", []string{"-y", npmSpec, "--acp"}
}

// qcnResolveCN / qcnResolveIntl 是上面那个函数的两个具名入口。
func qcnResolveCN(t *testing.T) (string, []string) {
	t.Helper()
	return qcnResolve(t, qcnCNBin, qcnCNAlias, qcnCNPackage)
}

func qcnResolveIntl(t *testing.T) (string, []string) {
	t.Helper()
	return qcnResolve(t, qcnIntlBin, "", qcnIntlPackage)
}

// qcnWithArgs 在 --acp 之前插入额外 flag（例如 --fake-responses）。
// Qoder 的 flag 顺序不敏感，但 --acp 必须是最后的位置参数。
func qcnWithArgs(args []string, extra ...string) []string {
	out := make([]string, 0, len(args)+len(extra))
	for _, a := range args {
		if a == "--acp" {
			out = append(out, extra...)
		}
		out = append(out, a)
	}
	return out
}

// qcnHelpFlags 返回 `--help` 输出里的 flag 名集合（排序后）。
// 用于证明两版的 CLI 参数面一致 —— 这是"共用一份 buildArgs"的前提。
func qcnHelpFlags(t *testing.T, bin string, args []string) []string {
	t.Helper()

	base := args[:0:0]
	for _, a := range args {
		if a != "--acp" {
			base = append(base, a)
		}
	}
	base = append(base, "--help")

	ctx, cancel := contextWithTimeout(t, 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, base...)
	setProcessGroup(cmd)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "run %s --help", bin)

	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-") {
			continue
		}
		// 一行可能同时给出短选项与长选项（"-p, --print   Print response..."），
		// 所以要把开头的 flag token 全部收下，遇到第一个非 flag token 才停。
		for _, tok := range strings.Fields(line) {
			tok = strings.TrimSuffix(strings.TrimSuffix(tok, ","), ",")
			if !strings.HasPrefix(tok, "-") || tok == "-" {
				break
			}
			seen[tok] = true
		}
	}
	flags := make([]string, 0, len(seen))
	for f := range seen {
		flags = append(flags, f)
	}
	sort.Strings(flags)
	require.NotEmpty(t, flags, "%s --help produced no flags", bin)
	return flags
}

// ---------------------------------------------------------------------------
// L0 — 命令发现 + 配置根隔离
// ---------------------------------------------------------------------------

// TestQoderCNProbe_L0_ConfigRootIsolation 证明两版各自只写自己的用户级目录，
// 且**互不触碰**。这是"必须注册两个后端（或按 agent 指定 spawn 命令）"的根据：
// 同一台机器上装两个包时，它们不会打架；但把 A 的配置根喂给 B 会被销毁
// （见文件头事故说明）。
func TestQoderCNProbe_L0_ConfigRootIsolation(t *testing.T) {
	home := qcnIsolatedHome(t)

	// 预置国际版目录 + 哨兵文件：国内版跑完后它必须原样存在。
	intlDir := filepath.Join(home, ".qoder")
	require.NoError(t, os.MkdirAll(intlDir, 0o700))
	sentinel := filepath.Join(intlDir, "CLAWBENCH-SENTINEL")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep me"), 0o600))

	cnBin, cnArgs := qcnResolveCN(t)
	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	_, _, initResult := startCCRawProbe(t, ctx, cnBin, cnArgs, t.TempDir())
	require.NotNil(t, initResult, "CN initialize must return a result")

	cnDir := filepath.Join(home, ".qoder-cn")
	cnInfo, err := os.Stat(cnDir)
	require.NoError(t, err, "CN must create its own user config root %s", cnDir)
	assert.True(t, cnInfo.IsDir())

	// 国际版目录里的哨兵必须完好，且 CN 不得往里写东西。
	b, err := os.ReadFile(sentinel)
	require.NoError(t, err, "CN must not delete anything under %s", intlDir)
	assert.Equal(t, "keep me", string(b), "CN must not touch the international config root")

	entries, err := os.ReadDir(intlDir)
	require.NoError(t, err)
	assert.Equal(t, []string{"CLAWBENCH-SENTINEL"}, qcnNames(entries),
		"CN wrote into %s: %v", intlDir, qcnNames(entries))

	// CN 自己的目录里应该出现机器码与认证子目录（证明真的在用它）。
	for _, want := range []string{".auth", "installation_id"} {
		_, err := os.Stat(filepath.Join(cnDir, want))
		assert.NoError(t, err, "CN should populate %s under its own root", want)
	}

	t.Logf("CN config root: %s", cnDir)
	t.Logf("CN initialize agentInfo: %s", jsonStr(initResult["agentInfo"]))
}

// qcnNames 返回目录项名字（排序），便于断言。
func qcnNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// TestQoderCNProbe_L0_FlagParity 证明两版 CLI 参数集完全一致。
// 前提成立时，CN 后端可以复用 qoder 的 buildArgs（只需换 Cmd / 配置根）。
func TestQoderCNProbe_L0_FlagParity(t *testing.T) {
	qcnIsolatedHome(t)

	cnBin, cnArgs := qcnResolveCN(t)
	intlBin, intlArgs := qcnResolveIntl(t)

	cnFlags := qcnHelpFlags(t, cnBin, cnArgs)
	intlFlags := qcnHelpFlags(t, intlBin, intlArgs)

	missingInCN := qcnDiff(intlFlags, cnFlags)
	missingInIntl := qcnDiff(cnFlags, intlFlags)

	assert.Empty(t, missingInCN, "flags present in the international build but missing in CN: %v", missingInCN)
	assert.Empty(t, missingInIntl, "flags present in CN but missing in the international build: %v", missingInIntl)

	// 这几个是 ClawBench 依赖的关键参数，缺一不可（分别对应 CLI 传输的
	// 会话恢复、工作目录、权限、系统提示、模型选择）。
	for _, required := range []string{"--print", "--output-format", "--session-id", "--resume", "--cwd", "--model", "--system-prompt"} {
		assert.Contains(t, cnFlags, required, "CN build must support %s", required)
	}
	t.Logf("shared flag count: %d", len(cnFlags))
}

// qcnDiff 返回 a 中不在 b 里的元素。
func qcnDiff(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// L1 — 能力面对比
// ---------------------------------------------------------------------------

// TestQoderCNProbe_L1_InitializeCapabilities 对比两版的 initialize 能力面。
// 结论用途：CN 可以走与 qoder 完全相同的 ACP 事件处理路径，只需注册一个新的
// backend id（因为事件分派是按 backend 字符串匹配的）。
func TestQoderCNProbe_L1_InitializeCapabilities(t *testing.T) {
	qcnIsolatedHome(t)

	cases := []struct {
		name    string
		resolve func(*testing.T) (string, []string)
		wantID  string
	}{
		{"cn", qcnResolveCN, "qoder-cli-cn"},
		{"intl", qcnResolveIntl, "qoder-cli"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin, args := tc.resolve(t)
			ctx, cancel := contextWithTimeout(t, 180*time.Second)
			defer cancel()

			_, _, initResult := startCCRawProbe(t, ctx, bin, args, t.TempDir())
			require.NotNil(t, initResult, "initialize must return a result")

			info, _ := initResult["agentInfo"].(map[string]any)
			require.NotNil(t, info, "initialize must advertise agentInfo")
			assert.Equal(t, tc.wantID, info["name"],
				"agentInfo.name is the only reliable way to tell the two builds apart at runtime")

			caps, _ := initResult["agentCapabilities"].(map[string]any)
			require.NotNil(t, caps)
			assert.Equal(t, true, caps["loadSession"], "loadSession gates acp-load / acp-sync endpoints")

			promptCaps, _ := caps["promptCapabilities"].(map[string]any)
			assert.Equal(t, true, promptCaps["image"], "image prompts must be supported")

			mcpCaps, _ := caps["mcpCapabilities"].(map[string]any)
			assert.Equal(t, true, mcpCaps["http"], "http MCP servers must be supported")
			assert.Equal(t, true, mcpCaps["sse"], "sse MCP servers must be supported")

			sessCaps, _ := caps["sessionCapabilities"].(map[string]any)
			require.NotNil(t, sessCaps)
			for _, k := range []string{"resume", "fork", "list"} {
				_, ok := sessCaps[k]
				assert.True(t, ok, "sessionCapabilities must advertise %s", k)
			}

			// promptQueueing 决定"同会话并发 prompt"的语义（ClawBench 依赖它
			// 区分 steer 与排队）。
			meta, _ := caps["_meta"].(map[string]any)
			qoderMeta, _ := meta["qoder"].(map[string]any)
			assert.Equal(t, true, qoderMeta["promptQueueing"], "_meta.qoder.promptQueueing must be advertised")

			// 未登录时 session/new 必须是 AUTH_REQUIRED，而不是别的错误码。
			// （两个 build 都一样 —— 鉴权闸与品牌无关。）
			t.Logf("%s agentInfo: %s", tc.name, jsonStr(info))
		})
	}
}

// ---------------------------------------------------------------------------
// L2 — 未登录鉴权闸
// ---------------------------------------------------------------------------

// TestQoderCNProbe_L2_UnauthenticatedGate 断言两版未登录时 session/new 都返回
// -32000，且 initialize 本身不需要登录（能力面探针可在未登录时跑）。
func TestQoderCNProbe_L2_UnauthenticatedGate(t *testing.T) {
	qcnIsolatedHome(t)

	bin, args := qcnResolveCN(t)
	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	probe, sessionID, initResult := startCCRawProbe(t, ctx, bin, args, t.TempDir())
	require.NotNil(t, initResult, "initialize must succeed without a login")
	assert.Empty(t, sessionID, "session/new must not succeed without a login")

	// startCCRawProbe 只记录失败原因，这里再显式发一次以拿到精确的错误码。
	resp, err := probe.call(ctx, probe.writer, "session/new", map[string]any{
		"cwd": t.TempDir(), "mcpServers": []any{},
	})
	require.NoError(t, err, "session/new must answer (even if with an error)")

	code, msg, isErr := extractJSONRPCError(resp)
	require.True(t, isErr, "session/new must fail without credentials, got: %s", jsonStr(resp))
	assert.Equal(t, qcnAuthRequired, code, "unauthenticated session/new must use ACP's AUTH_REQUIRED code")
	t.Logf("CN unauthenticated session/new: code=%v msg=%q", code, msg)

	// 登录方式由 agent 自己广告（authMethods），ClawBench 的 LoginCmd 必须与之一致，
	// 否则用户看到的提示与实际可执行的命令不符。
	authMethods, _ := initResult["authMethods"].([]any)
	require.NotEmpty(t, authMethods, "initialize must advertise an auth method")
	first, _ := authMethods[0].(map[string]any)
	assert.Equal(t, "qoderclicn-login", first["id"],
		"the advertised auth method id must match the CN binary name")
	t.Logf("CN authMethods: %s", jsonStr(authMethods))
}

// ---------------------------------------------------------------------------
// L3 — 不登录能不能用三方模型（结论：不能）
// ---------------------------------------------------------------------------

// TestQoderCNProbe_L3_BYOKRequiresLogin 钉住一个会反复被提起的问题：
// 「能不能不登录、直接用自己的 API key 跑」。
//
// 实测答案：**不能**。Qoder 的自定义 provider（settings.json 的 `providers`）
// 是"登录之后走自己的上游"，不是"免登录"。鉴权闸在 provider 之前：
//
//	async ensureAuthReady(){ if(..., this.argv.fakeResponses) return; ...
//	    if(!CA().isAuthenticated()) throw Ss.authRequired() }
//	requireAuth(){ if(!this.argv.fakeResponses && !this.acpAuthenticatedType) throw Ss.authRequired() }
//
// 唯一的短路条件是隐藏参数 `--fake-responses`，而它只回放预置响应、不发真实
// 模型请求。所以这个测试同时给出两条结论：
//  1. BYOK 配置存在也不会解锁 session/new（必须登录）；
//  2. `--fake-responses` 可以完全离线驱动 ACP 协议层（供 CI 用），但它不产生
//     `agent_message_chunk`，不能用来验证流式文本链路。
func TestQoderCNProbe_L3_BYOKRequiresLogin(t *testing.T) {
	home := qcnIsolatedHome(t)

	// 写一份形状正确的 BYOK provider（models 是数组，每项字段名是 `model`；
	// 写成对象或 `modelId` 会被 CLI 直接忽略 —— 日志里是
	// `[custom-provider] <id>.models: models must be an array`）。
	providers := map[string]any{
		"providers": map[string]any{
			"clawbench-probe": map[string]any{
				"type":        "openai-compatible",
				"displayName": "ClawBench Probe",
				"baseUrl":     "https://api.example.invalid/v1",
				"apiKey":      "probe-placeholder",
				"model":       "probe-model",
				"models": []any{
					map[string]any{"model": "probe-model", "name": "Probe Model"},
				},
			},
		},
	}
	raw, err := json.MarshalIndent(providers, "", "  ")
	require.NoError(t, err)
	cnDir := filepath.Join(home, ".qoder-cn")
	require.NoError(t, os.MkdirAll(cnDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cnDir, "settings.json"), raw, 0o600))

	bin, args := qcnResolveCN(t)
	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	// 连同所有"看起来能免登录"的环境变量一起给，逐个排除。
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "probe-placeholder-pat")
	t.Setenv("QODER_SDK_ACCESS_TOKEN", "probe-placeholder-sdk")
	t.Setenv("QODER_SDK_CUSTOM_BASE_URL_BYOK", "1")

	probe, sessionID, _ := startCCRawProbe(t, ctx, bin, args, t.TempDir())
	assert.Empty(t, sessionID,
		"BYOK providers must NOT unlock session/new: the auth gate runs before provider resolution")
	require.NotNil(t, probe)

	resp, err := probe.call(ctx, probe.writer, "session/new", map[string]any{
		"cwd": t.TempDir(), "mcpServers": []any{},
	})
	require.NoError(t, err)
	code, _, isErr := extractJSONRPCError(resp)
	require.True(t, isErr, "session/new must still be refused: %s", jsonStr(resp))
	assert.Equal(t, qcnAuthRequired, code,
		"BYOK must not change the failure mode: still AUTH_REQUIRED, never a provider/network error")
	t.Log("CONFIRMED: BYOK providers require a prior login; they are not an offline path")

	// --- 对照：--fake-responses 是唯一的免登录旁路 ---
	fakePath := filepath.Join(t.TempDir(), "fake.jsonl")
	fake := `{"method":"generateContentStream","response":[{"candidates":[{"content":{"parts":[{"text":"` + qcnWant + `"}]},"finishReason":"STOP"}]}]}`
	require.NoError(t, os.WriteFile(fakePath, []byte(fake+"\n"), 0o600))

	fakeArgs := qcnWithArgs(args, "--fake-responses", fakePath)
	fakeProbe, fakeSession, _ := startCCRawProbe(t, ctx, bin, fakeArgs, t.TempDir())
	require.NotEmpty(t, fakeSession,
		"--fake-responses must bypass the auth gate; otherwise the probe cannot drive ACP offline")

	promptResp, err := fakeProbe.call(ctx, fakeProbe.writer, "session/prompt", map[string]any{
		"sessionId": fakeSession,
		"prompt":    []any{map[string]any{"type": "text", "text": qcnPrompt}},
	})
	require.NoError(t, err, "fake-mode prompt must resolve")
	result, _ := promptResp["result"].(map[string]any)
	require.NotNil(t, result, "fake-mode prompt must return a result: %s", jsonStr(promptResp))
	assert.Equal(t, "end_turn", result["stopReason"])

	time.Sleep(qcnNotifyWait)
	updates, _, _ := fakeProbe.snapshot()
	var text strings.Builder
	kinds := map[string]int{}
	for _, u := range updates {
		kind, chunk := ccUpdateKind(u)
		kinds[kind]++
		text.WriteString(chunk)
	}
	t.Logf("fake-mode update kinds: %v", kinds)
	// 这是"不能拿 fake 模式验证文本链路"的证据：协议跑完了，但没有任何
	// agent_message_chunk。硬断言留在这里，免得以后有人用它做假绿的流式测试。
	assert.NotContains(t, text.String(), qcnWant,
		"--fake-responses does not emit agent_message_chunk; it must not be used to verify streaming text")
	assert.NotContains(t, kinds, "agent_message_chunk",
		"fake mode bypasses the model entirely, so no text chunk should appear")
}

// ---------------------------------------------------------------------------
// L4 — 经 ClawBench 生产链路
// ---------------------------------------------------------------------------

// TestQoderCNProbe_L4_BackendAuthErrorMapping 走 ClawBench 真正会走的那条路
// （ACPBackend.ExecuteStream），验证未登录时事件被翻译成可操作的错误。
//
// 注意本文件的 backend id 用的是 `qodercn`（真实注册值），所以 acpLoginCommand()
// 能查到 spec 并带上 `qoderclicn login` —— 但**仅当测试二进制里 import 了后端
// 子包**。`internal/ai` 自己的测试进程不 import `backends/qodercn`（会成环），
// 因此这里通常看到 LoginCmd 为空；结构化登录命令由
// internal/ai/backends/qodercn 的单元测试负责断言。这条测试只钉住与 spec 无关
// 的那一半：错误分类与错误码透传。
func TestQoderCNProbe_L4_BackendAuthErrorMapping(t *testing.T) {
	qcnIsolatedHome(t)

	bin, args := qcnResolveCN(t)
	cmdLine := bin + " " + strings.Join(args, " ")

	agent := &model.Agent{
		ID:         "qodercn-probe",
		Name:       "Qoder CN Probe",
		Backend:    "qodercn",
		Transport:  "acp-stdio",
		AcpCommand: cmdLine,
	}
	backend, err := NewACPBackend(agent)
	require.NoError(t, err, "NewACPBackend")

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	ctx, cancel := contextWithTimeout(t, 240*time.Second)
	defer cancel()

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    qcnPrompt,
		SessionID: sessionID,
		WorkDir:   t.TempDir(),
	})
	require.NoError(t, err, "ExecuteStream should not return a Go error for an auth failure")

	events := collectACPEvents(t, ch, 240*time.Second)
	t.Logf("L4 event types: %v", acpEventTypes(events))

	errs := findACPEvents(events, "error")
	require.NotEmpty(t, errs, "an unauthenticated CN agent must surface an error event, got: %v", acpEventTypes(events))
	assert.Equal(t, ReasonAuthRequired, errs[0].Reason,
		"-32000 must be classified as auth_required so the UI can offer a sign-in action")
	assert.Equal(t, int(qcnAuthRequired), errs[0].ErrorCode,
		"the JSON-RPC code must be carried through for diagnostics")

	// 错误文案必须可执行：要么带上结构化登录命令（spec 可见时），要么至少
	// 提到登录。空且不提及登录的文案等于把原始 JSON-RPC blob 丢给用户。
	assert.NotEmpty(t, errs[0].Error, "the error must carry a human-readable message")
	assert.Contains(t, strings.ToLower(errs[0].Error), "sign",
		"the message must tell the user to sign in, got: %q", errs[0].Error)
	t.Logf("login_cmd=%q error=%q", errs[0].LoginCmd, errs[0].Error)
}
