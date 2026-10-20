//go:build integration

package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// ===========================================================================
// ACP MCP 失败行为探针
// ===========================================================================
//
// 设计文档 §10.4 的前置：`session/new` 携带一个**起不来**的 stdio server 时，
// agent 是 (a) 返回错误、(b) 静默跳过、还是 (c) 阻塞到超时？
//
// 为什么必须先测：配置是全局的、默认 `all`，注入发生在 `spawnLocked` 之后的
// `NewSession`（acp_conn_lifecycle.go:306），而 `NewSession` 失败会让
// `c.alive = false` 并向上返回 error（:311-314）。如果 agent 返回错误，
// **一个写错的 command 会让所有 ACP 智能体的所有新会话全部失败**，
// 包括跟这个 server 毫无关系的智能体。
//
// 三种失败形态，因为它们的降级策略完全不同：
//
//	F1 command 指向不存在的路径      → 进程根本起不来
//	F2 command 存在但立即退出        → 进程起来了又死
//	F3 command 存在但从不响应        → 进程活着，握手超时
//
// 同时跑一个「好 + 坏」混合的用例：如果混合能成功而单个坏失败，说明 agent 会
// 逐条剔除；如果混合也失败，说明一个坏 server 会毒化整轮。
//
// 运行：
//
//	go test -tags integration -v -run 'TestACP_MCPProbeFailure' -timeout 1800s ./internal/ai/

// failureProbeCase 描述一种失败形态。
type failureProbeCase struct {
	ID string
	// Build 返回要注入的 server 描述符。
	Build func(t *testing.T, nodePath, serverScript, workDir string) acp.McpServer
	// Expect 是给人看的预期说明，写进日志。
	Expect string
}

var failureProbeCases = []failureProbeCase{
	{
		ID:     "missing-command",
		Expect: "command 指向不存在的路径",
		Build: func(t *testing.T, _, _, _ string) acp.McpServer {
			return acp.McpServer{Stdio: &acp.McpServerStdio{
				Name:    "clawbench-probe-missing-" + probeNonce,
				Command: "/nonexistent/clawbench-mcp-probe-binary-" + probeNonce,
				Args:    []string{},
				Env:     []acp.EnvVariable{},
			}}
		},
	},
	{
		ID:     "exits-immediately",
		Expect: "command 存在但立即以非零码退出",
		Build: func(t *testing.T, _, _, workDir string) acp.McpServer {
			script := filepath.Join(workDir, "die-immediately.sh")
			// 不读 stdin、不写 stdout，直接退出：模拟配置错误的 server。
			if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
				t.Fatalf("write script: %v", err)
			}
			return acp.McpServer{Stdio: &acp.McpServerStdio{
				Name:    "clawbench-probe-die-" + probeNonce,
				Command: script,
				Args:    []string{},
				Env:     []acp.EnvVariable{},
			}}
		},
	},
	{
		ID:     "never-responds",
		Expect: "command 存在、进程活着但从不响应 initialize",
		Build: func(t *testing.T, _, _, workDir string) acp.McpServer {
			script := filepath.Join(workDir, "hang-forever.sh")
			// 读掉 stdin 但永不输出：MCP 握手会挂住。
			if err := os.WriteFile(script, []byte("#!/bin/sh\ncat > /dev/null\n"), 0o755); err != nil {
				t.Fatalf("write script: %v", err)
			}
			return acp.McpServer{Stdio: &acp.McpServerStdio{
				Name:    "clawbench-probe-hang-" + probeNonce,
				Command: script,
				Args:    []string{},
				Env:     []acp.EnvVariable{},
			}}
		},
	},
}

// failureProbeObservation 汇总一个后端的观测结果。
type failureProbeObservation struct {
	Backend string
	// Results: 失败形态 ID -> 观测到的行为。
	Results map[string]string
	// MixedWorks: 「一个坏 + 一个好」时，好的那个是否仍可用。
	MixedWorks bool
	MixedNote  string
}

// TestACP_MCPProbeFailure 是入口。
func TestACP_MCPProbeFailure(t *testing.T) {
	nodePath, serverScript, backendList := probePrereqs(t)

	for _, b := range backendList {
		b := b
		t.Run(b.ID, func(t *testing.T) {
			skipIfBackendMissing(t, b)

			ctx, cancel := contextWithTimeout(t, 900*time.Second)
			defer cancel()

			SetAutoApproveGetter(func(string) bool { return true })
			t.Cleanup(func() { SetAutoApproveGetter(func(string) bool { return false }) })

			workDir := t.TempDir()
			obs := &failureProbeObservation{Backend: b.ID, Results: map[string]string{}}

			// ── 单个失败形态 ────────────────────────────────────────────
			for _, fc := range failureProbeCases {
				fc := fc
				t.Logf("──────── %s: %s ────────", fc.ID, fc.Expect)
				obs.Results[fc.ID] = probeOneFailure(t, ctx, b, fc, nodePath, serverScript, workDir)
			}

			// ── 混合：一个坏 + 一个好 ──────────────────────────────────
			t.Log("──────── mixed: one broken + one good ────────")
			obs.MixedWorks, obs.MixedNote = probeMixedFailure(t, ctx, b, nodePath, serverScript, workDir)

			obs.report(t, b)
		})
	}
}

// probeOneFailure 注入单个失败形态，返回观测到的行为标签。
func probeOneFailure(t *testing.T, ctx context.Context, b mcpProbeBackend, fc failureProbeCase, nodePath, serverScript, workDir string) string {
	t.Helper()

	server := fc.Build(t, nodePath, serverScript, workDir)

	proc := startMCPProbe(t, ctx, b)
	defer proc.kill()

	start := time.Now()
	// 单独的短超时：我们想区分「快速报错」与「挂到超时」，用 90s 上限。
	sessCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	resp, err := proc.conn.NewSession(sessCtx, acp.NewSessionRequest{
		Cwd:        workDir,
		McpServers: []acp.McpServer{server},
	})
	elapsed := time.Since(start)

	if err != nil {
		label := "ERROR"
		switch {
		case isDeadline(err):
			label = "TIMEOUT"
		case isMethodNotFound(err):
			label = "METHOD_NOT_FOUND"
		}
		t.Logf("RESULT[%s] session/new failed in %.1fs: %s", fc.ID, elapsed.Seconds(), truncate(err.Error(), 400))
		// 关键观察：错误是否点名了出问题的 server？
		if strings.Contains(strings.ToLower(err.Error()), "mcp") {
			t.Logf("RESULT[%s] the error mentions MCP (actionable)", fc.ID)
		} else {
			t.Logf("RESULT[%s] the error does NOT mention MCP (opaque — user cannot tell what broke)", fc.ID)
		}
		return fmt.Sprintf("%s (%.1fs)", label, elapsed.Seconds())
	}

	t.Logf("RESULT[%s] session/new SUCCEEDED in %.1fs — the agent ignored the broken server", fc.ID, elapsed.Seconds())

	// 成功也不代表没问题：确认那个坏 server 的工具确实不在。
	//
	// One SHORT turn, not the usual two-with-retry: this probe only needs to
	// know whether the session is usable, and a broken server can make the
	// first turn hang until the model deadline. Retrying doubled that cost and
	// blew the subtest budget (observed on qoder: 32s session + 2×240s turns).
	sid := string(resp.SessionId)
	sawTool := probeTurnShort(t, ctx, proc.extProbeSetup, sid, fc.ID+"-turn")
	t.Logf("RESULT[%s] turn ran, sawProbeTool=%v (false is expected: this server never worked)", fc.ID, sawTool)
	return fmt.Sprintf("SILENT_SKIP (%.1fs)", elapsed.Seconds())
}

// probeMixedFailure 注入「一个起不来的 + 一个正常的」，看正常那个还能不能用。
//
// 这是最贴近真实配置错误的场景：用户加了一个写错的 server，其他都是好的。
// 若混合失败，说明一个坏 server 会毒化整轮 —— 这正是设计 §10.4 要防的。
func probeMixedFailure(t *testing.T, ctx context.Context, b mcpProbeBackend, nodePath, serverScript, workDir string) (works bool, note string) {
	t.Helper()

	good := probeStdioServer(nodePath, serverScript)
	broken := acp.McpServer{Stdio: &acp.McpServerStdio{
		Name:    "clawbench-probe-broken-" + probeNonce,
		Command: "/nonexistent/clawbench-mcp-probe-binary-" + probeNonce,
		Args:    []string{},
		Env:     []acp.EnvVariable{},
	}}

	proc := startMCPProbe(t, ctx, b)
	defer proc.kill()

	start := time.Now()
	sessCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	resp, err := proc.conn.NewSession(sessCtx, acp.NewSessionRequest{
		Cwd:        workDir,
		McpServers: []acp.McpServer{broken, good},
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Logf("MIXED: session/new failed in %.1fs: %s", elapsed.Seconds(), truncate(err.Error(), 400))
		t.Logf("MIXED VERDICT[%s]: POISONED — one broken server fails the whole session", b.ID)
		return false, fmt.Sprintf("POISONED (%.1fs)", elapsed.Seconds())
	}
	t.Logf("MIXED: session/new succeeded in %.1fs", elapsed.Seconds())

	sid := string(resp.SessionId)
	if warm := probeWarmupDuration(); warm > 0 {
		time.Sleep(warm)
	}
	_, marker := probeTurnMarkerShort(t, ctx, proc.extProbeSetup, sid, "mixed-turn")

	if marker {
		t.Logf("MIXED VERDICT[%s]: TOLERANT — the good server still works alongside a broken one", b.ID)
		return true, "TOLERANT"
	}
	t.Logf("MIXED VERDICT[%s]: DEGRADED — session succeeded but the good server's tool never ran", b.ID)
	return false, "DEGRADED"
}

func (o *failureProbeObservation) report(t *testing.T, b mcpProbeBackend) {
	t.Helper()
	t.Logf("════ FAILURE SUMMARY[%s] ════", b.ID)
	for _, fc := range failureProbeCases {
		t.Logf("  %-18s %s", fc.ID, o.Results[fc.ID])
	}
	t.Logf("  %-18s %v (%s)", "mixed", o.MixedWorks, o.MixedNote)

	// 设计文档要的那句话：一个写错的 command 会不会拖垮无关的智能体。
	t.Logf("  → 降级策略建议：")
	anyError := false
	anySilent := false
	for _, fc := range failureProbeCases {
		v := o.Results[fc.ID]
		if strings.HasPrefix(v, "ERROR") || strings.HasPrefix(v, "TIMEOUT") {
			anyError = true
		}
		if strings.HasPrefix(v, "SILENT_SKIP") {
			anySilent = true
		}
	}
	switch {
	case o.MixedWorks:
		t.Logf("     B 策略可用（逐条剔除）：坏 server 不毒化整轮，好 server 仍工作")
	case anyError && !o.MixedWorks:
		t.Logf("     必须 A 策略（整体失败 + 点名 server）：坏 server 会让 session/new 失败")
	case anySilent && !o.MixedWorks:
		t.Logf("     必须 A 策略 + 前置校验：坏 server 被静默跳过，用户无法察觉缺失")
	default:
		t.Logf("     需要人工判读上面的结果")
	}
	if anySilent {
		t.Logf("     注意：存在 SILENT_SKIP 形态 —— UI 必须能显示「该 server 未连上」")
	}
}

// probeTurnShort runs ONE turn with a 90s cap and reports whether the probe
// tool was visible. Used by the failure probe, where the goal is "is this
// session usable at all", not "can the model be coaxed into calling it".
func probeTurnShort(t *testing.T, ctx context.Context, setup *extProbeSetup, sessionID, phase string) bool {
	t.Helper()
	sawTool, _ := probeTurnMarkerShort(t, ctx, setup, sessionID, phase)
	return sawTool
}

func probeTurnMarkerShort(t *testing.T, ctx context.Context, setup *extProbeSetup, sessionID, phase string) (sawTool, marker bool) {
	t.Helper()
	turnCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	sawTool, marker, _, _ = runMCPProbeTurnDetailed(t, turnCtx, setup, sessionID, phase)
	return sawTool, marker
}

// isDeadline 报告 err 是否由超时引起（含 JSON-RPC 包装过的）。
func isDeadline(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "timeout")
}
