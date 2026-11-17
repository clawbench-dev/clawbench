package service

import (
	"context"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// newTerminalTestExecutor builds an executor for a session that already has a
// streaming placeholder, which is the minimum Finalize needs.
func newTerminalTestExecutor(t *testing.T) (*SessionExecutor, string) {
	t.Helper()
	setupExecutorDB(t)
	const agentID = "terminal-test-agent"
	model.Agents = map[string]*model.Agent{
		agentID: {ID: agentID, Name: "Test", Backend: "test"},
	}
	t.Cleanup(func() { model.Agents = nil })

	sid := setupExecutorSession(t, agentID)
	executor := NewSessionExecutor(context.Background(), RunConfig{
		Mode:        ModeInteractive,
		ProjectPath: "/test",
		BackendName: "test",
		SessionID:   sid,
		AgentID:     agentID,
		ChatRequest: ai.ChatRequest{Prompt: "hello"},
	})
	return executor, sid
}

func runTerminalEvents(executor *SessionExecutor, events []ai.StreamEvent) RunResult {
	ch := make(chan ai.StreamEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return executor.RunWithChannel(ch)
}

// TestSessionExecutor_DoneMetadataReachesLedger pins the defect behind "why did
// the agent stop?": the terminal event carries the run's stop reason and its
// TOTALS, and the event loop used to drop both. The ledger then recorded only
// the last round's token counts and an empty stop_reason, so a turn that hit
// its budget was indistinguishable from one that answered.
func TestSessionExecutor_DoneMetadataReachesLedger(t *testing.T) {
	executor, _ := newTerminalTestExecutor(t)

	result := runTerminalEvents(executor, []ai.StreamEvent{
		{Type: "content", Content: "hello"},
		{Type: "metadata", Meta: &ai.Metadata{InputTokens: 100, OutputTokens: 50}},
		{Type: "done", Meta: &ai.Metadata{
			StopReason:   "max_rounds",
			InputTokens:  300,
			OutputTokens: 200,
			TotalTokens:  500,
		}},
	})

	if result.Metadata == nil {
		t.Fatal("expected metadata after the run")
	}
	if result.Metadata.StopReason != "max_rounds" {
		t.Fatalf("the done event's stop reason must be kept, got %q", result.Metadata.StopReason)
	}
	// The per-round event is the richer source for tokens and must win; only
	// what is still missing (the totals) may come from the terminal event.
	if result.Metadata.InputTokens != 100 || result.Metadata.OutputTokens != 50 {
		t.Fatalf("the per-round token counts must not be overwritten, got in=%d out=%d",
			result.Metadata.InputTokens, result.Metadata.OutputTokens)
	}
	if result.Metadata.TotalTokens != 500 {
		t.Fatalf("the terminal totals must be filled in, got %d", result.Metadata.TotalTokens)
	}

	result = executor.Finalize(result, nil)
	var stopReason string
	if err := store.ReadDB().QueryRow(
		"SELECT stop_reason FROM chat_metadata WHERE message_id = ?", result.MsgID,
	).Scan(&stopReason); err != nil {
		t.Fatalf("read metadata row: %v", err)
	}
	if stopReason != "max_rounds" {
		t.Fatalf("stop_reason must reach the ledger, got %q", stopReason)
	}
}

// TestSessionExecutor_SystemMessageIsPersisted pins the budget notice: a
// backend-emitted system line must become a timeline row, not just a WS frame,
// or a reload loses the only explanation of why the turn ended early.
func TestSessionExecutor_SystemMessageIsPersisted(t *testing.T) {
	executor, sid := newTerminalTestExecutor(t)

	const notice = "本轮已达到 100 轮模型往返上限，智能体已提前收尾。"
	runTerminalEvents(executor, []ai.StreamEvent{
		{Type: "content", Content: "partial answer"},
		{Type: "system_message", SystemMessage: &ai.SystemMessageData{Content: notice}},
		{Type: "done"},
	})

	var count int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'system' AND content = ?",
		sid, notice,
	).Scan(&count); err != nil {
		t.Fatalf("query system rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the notice to be persisted once, got %d rows", count)
	}
}

// TestSessionExecutor_SystemMessageWithoutContentIsIgnored pins that a stray
// empty event does not create a blank timeline row.
func TestSessionExecutor_SystemMessageWithoutContentIsIgnored(t *testing.T) {
	executor, sid := newTerminalTestExecutor(t)

	runTerminalEvents(executor, []ai.StreamEvent{
		{Type: "system_message"},
		{Type: "done"},
	})

	var count int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'system'", sid,
	).Scan(&count); err != nil {
		t.Fatalf("query system rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("an empty notice must not be persisted, got %d rows", count)
	}
}
