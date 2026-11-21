package service

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// TestBuildBuiltinHistory_ExcludesSubagentContent pins the isolation rule at
// the replay boundary: the parent's next turn must see the task call and its
// report, not the child's narration or the child's own reads.
func TestBuildBuiltinHistory_ExcludesSubagentContent(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	const sessionID = "s-sub"
	if _, err := store.WriteExec(
		`INSERT OR IGNORE INTO chat_sessions (id, project_id, backend, title) VALUES (?, 0, 'builtin', '')`,
		sessionID,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	content, err := json.Marshal(map[string]any{
		"blocks": []map[string]any{
			{"type": "text", "text": "parent answer"},
			{"type": "text", "text": "child report", "parent_tool_call_id": "call_task"},
			{"type": "tool_use", "id": "call_task", "name": "task", "input": map[string]any{"description": "look"}},
			{"type": "tool_use", "id": "child_read", "name": "read", "input": map[string]any{"path": "a.txt"}, "parent_tool_call_id": "call_task"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	msgID, err := AddChatMessage("/tmp/p", "builtin", sessionID, "assistant", string(content), nil, false, "")
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	if err := UpsertToolCall(msgID, sessionID, "call_task", "task", []byte(`{"description":"look"}`), "child report", "success", "", true, 0); err != nil {
		t.Fatalf("seed task call: %v", err)
	}
	if err := UpsertToolCall(msgID, sessionID, "child_read", "read", []byte(`{"path":"a.txt"}`), "file body", "success", "", true, 0); err != nil {
		t.Fatalf("seed child call: %v", err)
	}

	got := buildBuiltinHistory(sessionID, 0, builtinSummary{})
	if len(got) != 1 {
		t.Fatalf("expected one replayed turn, got %d: %+v", len(got), got)
	}
	if got[0].Text != "parent answer" {
		t.Fatalf("the parent's text must exclude the child's report, got %q", got[0].Text)
	}
	if len(got[0].Tools) != 1 {
		t.Fatalf("only the top-level task call may be replayed, got %+v", got[0].Tools)
	}
	if got[0].Tools[0].Name != "task" || got[0].Tools[0].Output != "child report" {
		t.Fatalf("the task call and its report must be replayed, got %+v", got[0].Tools[0])
	}
}

// TestBuiltinToolCalls_TaskReportKeepsItsOwnCap pins the one output that is a
// deliverable rather than context: a sub-agent's report must not be cut to the
// 2000-rune cap that protects the history from file bodies.
func TestBuiltinToolCalls_TaskReportKeepsItsOwnCap(t *testing.T) {
	setupTestDBForBuiltinHistory(t)

	const sessionID = "s-cap"
	if _, err := store.WriteExec(
		`INSERT OR IGNORE INTO chat_sessions (id, project_id, backend, title) VALUES (?, 0, 'builtin', '')`,
		sessionID,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	content := `{"blocks":[{"type":"tool_use","id":"call_task","name":"task"},{"type":"tool_use","id":"call_read","name":"read"}]}`
	msgID, err := AddChatMessage("/tmp/p", "builtin", sessionID, "assistant", content, nil, false, "")
	if err != nil {
		t.Fatalf("seed message: %v", err)
	}
	long := strings.Repeat("a", 3000)
	if err := UpsertToolCall(msgID, sessionID, "call_task", "task", []byte(`{}`), long, "success", "", true, 0); err != nil {
		t.Fatalf("seed task call: %v", err)
	}
	if err := UpsertToolCall(msgID, sessionID, "call_read", "read", []byte(`{}`), long, "success", "", true, 0); err != nil {
		t.Fatalf("seed read call: %v", err)
	}

	calls := builtinToolCalls([]model.ChatMessage{{ID: msgID, Role: "assistant", Content: content}})
	outputs := map[string]string{}
	for _, c := range calls[msgID] {
		outputs[c.Name] = c.Output
	}
	if strings.Contains(outputs["task"], "...(truncated)") {
		t.Fatalf("the task report must keep its larger cap, got %d runes", len([]rune(outputs["task"])))
	}
	if !strings.Contains(outputs["read"], "...(truncated)") {
		t.Fatalf("a file-body output must still be capped, got %d runes", len([]rune(outputs["read"])))
	}
}

// TestBuildBuiltinHistory_KeepsLegacyPlainText pins the fallback: rows written
// before block storage must still replay as text.
func TestBuildBuiltinHistory_KeepsLegacyPlainText(t *testing.T) {
	setupTestDBForBuiltinHistory(t)
	seedHistory(t, "s-legacy", [][2]string{
		{"user", "question"},
		{"assistant", "plain answer"},
	})

	got := buildBuiltinHistory("s-legacy", 0, builtinSummary{})
	if len(got) != 2 || got[1].Text != "plain answer" {
		t.Fatalf("legacy plain text must still replay, got %+v", got)
	}
}
