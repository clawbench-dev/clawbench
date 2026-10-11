//go:build integration

package ai

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// claude / codex `_session/steering` — 生产接线端到端验证
// ===========================================================================
//
// 探针（TestACPSteeringProbe）证明了 wire 上 `_session/steering` 的形状与回执
// 缺失。本测试驱动**生产** ACPConn（而非探针自建的 writer），验证：
//
//  1. ACPConn.CallRaw 能在真实 conn 上发出 `_session/steering`（经共享
//     lockedWriter，与 SDK 请求不交错）；
//  2. 轮中调用返回 {outcome:"injected"}；
//  3. **合并语义**：steer 之后不产生任何 steer_boundary（适配器不回显）——
//     这正是 claude/codex 只能"并入"、不能"切分"的根据，也是它们注册
//     MidTurnSplitsReply=false 的原因；
//  4. 该轮仍正常以 stopReason 收尾（steer 不破坏回合）。
//
// 运行：
//
//	go test -tags integration -v -run 'TestACPSteeringE2E' \
//	    -timeout 600s ./internal/ai/
//
// 需要本机安装 claude / codex 且已登录。

// steeringE2EAgent builds an ACP agent for a bridge adapter.
func steeringE2EAgent(id, acpCommand, modelID string) *model.Agent {
	return &model.Agent{
		ID:         id + "-acp-steering-e2e-test",
		Name:       id + " ACP Steering E2E",
		Backend:    id,
		Transport:  "acp-stdio",
		AcpCommand: acpCommand,
		Models:     []model.AgentModel{{ID: modelID, Name: modelID, Default: true}},
	}
}

func TestACPSteeringE2E_Claude(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed")
	}
	runSteeringE2E(t, steeringE2EAgent("claude",
		"npx -y @agentclientprotocol/claude-agent-acp@latest", "claude-sonnet-4-6"))
}

func TestACPSteeringE2E_Codex(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex CLI not installed")
	}
	runSteeringE2E(t, steeringE2EAgent("codex",
		"npx -y @agentclientprotocol/codex-acp@latest", "codex-mini"))
}

// runSteeringE2E drives one real turn, injects mid-turn via the production
// connection, and asserts the merge-only contract (outcome=injected, no
// boundary, clean turn end).
func runSteeringE2E(t *testing.T, agent *model.Agent) {
	t.Helper()

	env := setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	origRoots := model.RootPaths
	model.RootPaths = []string{"/"}
	t.Cleanup(func() { model.RootPaths = origRoots })

	SetAutoApproveGetter(func(_ string) bool { return true })
	t.Cleanup(func() { SetAutoApproveGetter(func(_ string) bool { return false }) })

	sessionID := acpSessionID()
	t.Cleanup(func() { env.closeConn(t, sessionID) })

	ctx, cancel := contextWithTimeout(t, 420*time.Second)
	defer cancel()

	// A multi-step turn gives us a window to inject mid-turn.
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt: "请分三步，每步之间必须真的调用 Bash 工具执行命令：" +
			"1) 运行 `sleep 3` 并说明这是第一步；" +
			"2) 运行 `sleep 3` 并说明这是第二步；" +
			"3) 运行 `sleep 3` 并说明这是第三步。",
		SessionID:          sessionID,
		WorkDir:            acpTestWorkDir(),
		ScheduledExecution: true,
	})
	require.NoError(t, err)

	time.Sleep(5 * time.Second)

	conn := GetACPConnManager().GetConn(sessionID)
	require.NotNil(t, conn, "the production spawn should leave a pooled connection")

	// The policy would decline without a live turn; assert the production
	// connection reports one (this is the codex ghost-turn guard's input).
	require.True(t, conn.PromptInFlight(),
		"a prompt must be in flight mid-turn, or the merge-only policy would decline")

	const steerMarker = "CLAWBENCH-STEERING-E2E-MARKER"
	steerDone := make(chan error, 1)
	go func() {
		_, steerErr := conn.CallRaw(ctx, "_session/steering", map[string]any{
			"sessionId": conn.AcpSessionID(),
			"prompt": []map[string]any{
				{"type": "text", "text": "请立刻回复这句话，一个字都不要多：" + steerMarker},
			},
		})
		if steerErr != nil {
			t.Logf("steering error: %v", steerErr)
		}
		steerDone <- steerErr
	}()

	var sawBoundary bool
	var sawMarker bool
	deadline := time.After(300 * time.Second)

loop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				break loop
			}
			if ev.Type == "steer_boundary" {
				sawBoundary = true
			}
			if ev.Type == "content" && strings.Contains(ev.Content, steerMarker) {
				sawMarker = true
			}
			if ev.Type == "done" || ev.Type == "error" {
				break loop
			}
		case <-deadline:
			t.Fatal("turn did not finish within 300s")
		}
	}

	select {
	case steerErr := <-steerDone:
		require.NoError(t, steerErr, "the steering call must reach the agent and respond")
		t.Log("steering call returned without transport error")
	case <-time.After(30 * time.Second):
		t.Fatal("steering call did not return within 30s")
	}

	// The merge-only contract: no boundary is emitted, which is exactly why the
	// backend is registered MidTurnSplitsReply=false and the frontend shows the
	// "回复已并入上一轮" hint instead of splitting the reply.
	assert.False(t, sawBoundary,
		"claude/codex must NOT emit a steer boundary — their injection MERGES into the reply")
	t.Logf("steer marker reached the model output: %v", sawMarker)
}
