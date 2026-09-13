//go:build integration

package ai

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// 端到端：steer → 边界事件真的从生产链路冒出来
// ===========================================================================
//
// 探针（TestCodebuddyACP_ExtProbe_SteerSplitBoundary）证明了 wire 上存在边界帧。
// 本测试验证**生产链路**把它变成 StreamEvent：
//
//	ACPBackend.ExecuteStream
//	  → ACPConn.CallRaw(session/steer)   （经 MidTurnInjector 策略注册 echo）
//	  → ClawBenchACPClient.SessionUpdate  （stdout filter tee → demux）
//	  → mapACPSessionUpdate 的 UserMessageChunk 分支
//	  → StreamEvent{Type:"steer_boundary", SteerBoundary.ClientUserMessageID}
//
// 这是"截断为两条"能否真正工作的关键：单测覆盖了 gating 逻辑，但只有这里能
// 证明 spawnLocked 的接线（filter sink + rawRPC + connRef）让 echo 真的被认领。
//
// 运行：
//
//	go test -tags integration -v -run 'TestCodebuddyACP_SteerSplitE2E' \
//	    -timeout 400s ./internal/ai/
func TestCodebuddyACP_SteerSplitE2E_BoundaryReachesStream(t *testing.T) {
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

	ctx, cancel := contextWithTimeout(t, 300*time.Second)
	defer cancel()

	// Start a multi-step turn so there is a window to inject mid-turn.
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

	// Give the turn a moment to start producing content.
	time.Sleep(4 * time.Second)

	conn := GetACPConnManager().GetConn(sessionID)
	require.NotNil(t, conn, "the production spawn should leave a pooled connection")

	const injectID = "clawbench-split-e2e-1"

	// Drive the injection the way the production policy does: register the echo
	// we expect, then send session/steer on the raw channel. We call the two
	// steps directly rather than going through LookupMidTurnInjectorFn because
	// that function variable is wired by the `backends` package's init, which
	// this test binary does not import (importing it from package ai would be a
	// cycle). The chain under test — echo registration → stdout filter tee →
	// mapACPSessionUpdate → StreamEvent — is identical either way.
	conn.ExpectSteerEcho(injectID)

	injectDone := make(chan error, 1)
	go func() {
		_, injectErr := conn.CallRaw(ctx, "session/steer", map[string]any{
			"sessionId": conn.AcpSessionID(),
			"contentBlocks": []map[string]any{
				{"type": "text", "text": "请立刻回复这句话，一个字都不要多：CLAWBENCH-SPLIT-E2E-MARKER"},
			},
			"clientUserMessageId": injectID,
		})
		if injectErr != nil {
			t.Logf("injection error: %v", injectErr)
		}
		injectDone <- injectErr
	}()

	// Collect the stream until it ends, looking for the boundary event.
	var sawBoundary bool
	var boundaryID string
	deadline := time.After(240 * time.Second)
	contentAfterBoundary := 0

loop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				break loop
			}
			if ev.Type == "steer_boundary" && ev.SteerBoundary != nil {
				sawBoundary = true
				boundaryID = ev.SteerBoundary.ClientUserMessageID
				t.Logf("CONFIRMED: steer_boundary event reached the stream, clientUserMessageID=%q", boundaryID)
				continue
			}
			if sawBoundary && ev.Type == "content" {
				contentAfterBoundary++
			}
			if ev.Type == "done" || ev.Type == "error" {
				break loop
			}
		case <-deadline:
			t.Fatal("turn did not finish within 240s")
		}
	}

	// Drain the injector result (it should have returned long before the turn ended).
	select {
	case injectErr := <-injectDone:
		if injectErr != nil {
			t.Logf("NOTE: session/steer returned an error (%v) — if it was declined the "+
				"boundary cannot be observed. Re-run; this is a timing artifact, not a "+
				"wiring failure.", injectErr)
		} else {
			t.Log("session/steer accepted")
		}
	case <-time.After(30 * time.Second):
		t.Log("NOTE: injector goroutine did not report within 30s")
	}

	if !sawBoundary {
		t.Fatalf("FAILED: no steer_boundary event reached the stream. This means the production "+
			"wiring (spawnLocked → stdoutFilter.SetRawSink → mapACPSessionUpdate UserMessageChunk) "+
			"did not turn the agent's echo into an event. injectedID=%q", injectID)
	}

	assert.Equal(t, injectID, boundaryID,
		"the boundary must carry the clientUserMessageId we injected, so the "+
			"service layer can anchor the 'after' half to the right question")

	// Content must continue AFTER the boundary, otherwise there is nothing to
	// split into a second message.
	t.Logf("content events after boundary: %d", contentAfterBoundary)
	assert.Greater(t, contentAfterBoundary, 0,
		"the assistant must keep producing content after the injection, otherwise "+
			"there is no 'after' half and the split would produce an empty bubble")
}
