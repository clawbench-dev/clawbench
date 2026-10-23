package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSchedCancelBackend emits one content delta and then blocks until the turn
// is cancelled, so a scheduled AI-phase run can be interrupted mid-reply.
type mockSchedCancelBackend struct{}

func (m *mockSchedCancelBackend) Name() string { return "test-sched-cancel-order" }

func (m *mockSchedCancelBackend) ExecuteStream(ctx context.Context, _ ai.ChatRequest) (<-chan ai.StreamEvent, error) {
	ch := make(chan ai.StreamEvent, 8)
	go func() {
		defer close(ch)
		ch <- ai.StreamEvent{Type: "content", Content: "partial answer"}
		<-ctx.Done()
	}()
	return ch, nil
}

// TestScheduler_CancelExecution_StampsMessageBeforeTaskEvent pins the ORDER of
// the cancel branch's two visible effects.
//
// `task_update` carries a response preview and message id read back out of the
// assistant row, and that read only sees finalized rows (streaming = 0) — so a
// row still streaming when the event is emitted yields an EMPTY preview. The
// cancel path must therefore finalize (flush + stamp `cancelled:true`) BEFORE
// emitting the event, or every cancelled run's notification and history row
// describe the interrupted reply as if nothing had happened.
func TestScheduler_CancelExecution_StampsMessageBeforeTaskEvent(t *testing.T) {
	ai.RegisterBackend("test-sched-cancel-order", func() ai.AIBackend { return &mockSchedCancelBackend{} })

	setupSchedulerExecDB(t)
	origAgents := model.Agents
	model.Agents = map[string]*model.Agent{
		"cancel-agent": {ID: "cancel-agent", Name: "Cancel Agent", Backend: "test-sched-cancel-order", RuntimeSystemPrompt: "sys"},
	}
	t.Cleanup(func() { model.Agents = origAgents })

	origMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(origMgr) })

	s := NewScheduler()
	t.Cleanup(s.Stop)

	task := &model.ScheduledTask{
		ProjectPath: "/tmp",
		Name:        "Cancel Order",
		CronExpr:    "0 * * * *",
		AgentID:     "cancel-agent",
		Prompt:      "do the thing",
		Status:      "active",
		RepeatMode:  "unlimited",
	}
	require.NoError(t, s.AddTask(task))

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "sched-cancel-order-client", "")

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.executeTask(task, "/tmp", "manual", nil)
	}()

	// Wait until the reply has actually been persisted onto the streaming row,
	// so the cancel interrupts a run that has content to lose.
	var sessionID string
	require.Eventually(t, func() bool {
		return store.ReadDB().QueryRow(
			"SELECT session_id FROM chat_history WHERE role = 'assistant' AND streaming = 1 AND content LIKE '%partial answer%' LIMIT 1",
		).Scan(&sessionID) == nil
	}, 3*time.Second, 20*time.Millisecond, "the streaming row must carry the partial reply before cancelling")

	require.NoError(t, s.CancelExecution(sessionID), "the AI-phase execution must be cancellable")

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not terminate after cancelling the AI phase")
	}

	// The message row must be finalized and stamped as cancelled.
	var streaming int
	var content string
	require.NoError(t, store.ReadDB().QueryRow(
		"SELECT streaming, content FROM chat_history WHERE session_id = ? AND role = 'assistant' ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&streaming, &content))
	assert.Equal(t, 0, streaming, "the cancel path must finalize the streaming row")

	var stored struct {
		Cancelled bool `json:"cancelled"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &stored))
	assert.True(t, stored.Cancelled, "the finalized row must carry cancelled:true")
	assert.Contains(t, content, "partial answer", "the interrupted reply must survive finalization")

	// The task event must have been emitted AFTER that, so its preview and
	// message id describe the row it is actually about.
	var cancelled *ws.TaskUpdateData
	for _, ev := range sub.GetBufferedEvents() {
		if data, ok := ev.Data.(*ws.TaskUpdateData); ok && data.Status == "cancelled" {
			cancelled = data
		}
	}
	require.NotNil(t, cancelled, "the cancel path must emit a cancelled task_update")
	assert.Contains(t, cancelled.ResponsePreview, "partial answer",
		"the event preview must include the interrupted reply (empty means the event was emitted before finalization)")
	assert.Greater(t, cancelled.MessageID, int64(0), "the event must point at the message it describes")
	assert.True(t, strings.Contains(cancelled.ResponsePreviewPlain, "partial answer"),
		"the plain preview must be derived from the same finalized row")
}
