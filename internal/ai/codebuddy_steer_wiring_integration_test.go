//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// CodeBuddy steer — 生产接线端到端验证
// ===========================================================================
//
// 目的：单测覆盖了策略与 demux 逻辑，但无法证明 spawnLocked 里的接线正确
// （lockedWriter 是否真的同时服务 SDK 与本通道、stdout filter 的 sink 是否真的
// 挂上了、rawRPC 是否绑定了正确的 conn.Done()）。本测试驱动**生产** ACPConn
// （而非探针自建的 writer）验证这条链路。
//
// 断言：
//  1. ACPConn.CallRaw 能成功发出 session/set_model（SDK 拒绝的非 `_` 方法）；
//  2. 与此同时 SDK 自身的 RPC（如 session/new）照常工作 —— 证明共享写锁没有
//     破坏 SDK，且 demux 没有吞掉 SDK 的响应；
//  3. 未知方法返回 JSON-RPC error（*RawRPCError）而非挂起。
//
// 运行：
//
//	go test -v -run 'TestCodebuddyACP_SteerWiring' -tags integration \
//	    -timeout 300s ./internal/ai/

// steerWiringAgent returns a CodeBuddy ACP agent for the wiring test.
func steerWiringAgent() *model.Agent {
	return &model.Agent{
		ID:         "codebuddy-acp-steer-wiring-test",
		Name:       "CodeBuddy ACP Steer Wiring Test",
		Backend:    "codebuddy",
		Transport:  "acp-stdio",
		AcpCommand: "codebuddy --acp",
		Models:     []model.AgentModel{{ID: "glm-4-plus", Name: "glm-4-plus", Default: true}},
	}
}

// TestCodebuddyACP_SteerWiring_RawChannelOnProductionConn verifies the raw
// JSON-RPC channel works on a connection created by the production spawn path.
func TestCodebuddyACP_SteerWiring_RawChannelOnProductionConn(t *testing.T) {
	requireCodebuddyACP(t)

	agent := steerWiringAgent()
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

	ctx, cancel := contextWithTimeout(t, 180*time.Second)
	defer cancel()

	// Drive one real turn so the production spawn path runs end to end
	// (spawnLocked → lockedWriter → stdoutFilter.SetRawSink → Initialize → NewSession).
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:             "只回复一个词：ok",
		SessionID:          sessionID,
		WorkDir:            acpTestWorkDir(),
		ScheduledExecution: true,
	})
	require.NoError(t, err)
	events := collectACPEvents(t, ch, 180*time.Second)
	requireDoneEvent(t, events)

	// The connection now exists in the pool, created by the production path.
	conn := GetACPConnManager().GetConn(sessionID)
	require.NotNil(t, conn, "production spawn should leave a pooled connection")
	acpSID := conn.AcpSessionID()
	require.NotEmpty(t, acpSID, "the connection should have an ACP session id after a turn")

	// (1) Raw channel: a method the SDK refuses to send (non-`_` prefix).
	//
	// Use session/steer rather than session/set_model: set_model MUTATES session
	// state, and a bogus model id persists into the next turn and breaks it
	// (observed). steer with no running turn is a harmless no-op that still
	// proves the round trip.
	callCtx, callCancel := context.WithTimeout(ctx, 30*time.Second)
	defer callCancel()
	raw, err := conn.CallRaw(callCtx, "session/steer", map[string]any{
		"sessionId": acpSID,
		"contentBlocks": []map[string]any{
			{"type": "text", "text": "clawbench steer wiring probe"},
		},
	})
	require.NoError(t, err, "CallRaw must work on a production connection; "+
		"a failure here means the spawn-time wiring (lockedWriter + filter sink) is broken")
	var res struct {
		Steered bool   `json:"steered"`
		Reason  string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(raw, &res))
	// No turn is running, so the agent must decline cleanly — that IS the proof
	// the request reached the agent's dispatcher and was answered.
	assert.False(t, res.Steered)
	assert.Equal(t, "idle", res.Reason)
	t.Logf("CONFIRMED: session/steer round-tripped on the production connection "+
		"(steered=false reason=%q, as expected with no running turn)", res.Reason)

	// (2) The SDK must still work on the same connection after a raw call:
	// a second real turn proves the shared write lock did not corrupt SDK
	// traffic and that demux did not swallow SDK responses.
	ch2, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:             "只回复一个词：done",
		SessionID:          sessionID,
		WorkDir:            acpTestWorkDir(),
		ScheduledExecution: true,
	})
	require.NoError(t, err)
	events2 := collectACPEvents(t, ch2, 180*time.Second)
	requireDoneEvent(t, events2)

	// The point is that the SDK turn COMPLETED (done event) after a raw call —
	// i.e. the shared write lock did not corrupt SDK traffic and demux did not
	// swallow SDK responses. The model's wording is not what we are testing, so
	// dump it for the record rather than asserting on it.
	content2 := concatACPContent(events2)
	t.Logf("second turn content (informational): %q", truncate(content2, 200))
	for _, e := range events2 {
		if e.Type == "error" || e.Type == "warning" {
			t.Logf("second turn %s event: %s", e.Type, truncate(e.Error+e.Content, 200))
		}
	}
	t.Logf("CONFIRMED: SDK RPCs still work after a raw call on the same connection (done event received)")

	// (3) An unknown method must fail fast with a JSON-RPC error, not hang.
	unknownCtx, unknownCancel := context.WithTimeout(ctx, 20*time.Second)
	defer unknownCancel()
	_, err = conn.CallRaw(unknownCtx, "session/definitely_not_a_method", map[string]any{"sessionId": acpSID})
	require.Error(t, err, "an unknown method should return an error")
	var rpcErr *RawRPCError
	if assert.ErrorAs(t, err, &rpcErr) {
		t.Logf("CONFIRMED: unknown method returns JSON-RPC error %d (%s)", rpcErr.Code, rpcErr.Message)
	}
}
