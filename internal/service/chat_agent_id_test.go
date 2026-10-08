package service_test

import (
	"testing"

	"clawbench/internal/service"
)

// TestAddChatMessagePersistsAgentID verifies chat_history.agent_id round-trips
// through the write path (AddChatMessageWithAgent) and the main read path
// (GetChatHistoryPaged). The value is a speaker member row id, not an agent id.
func TestAddChatMessagePersistsAgentID(t *testing.T) {
	setupDB(t)
	project := "/tmp/grouptest"
	sid := helperCreateSession(t, project, "codebuddy", "t")

	id, err := service.AddChatMessageWithAgent(project, "codebuddy", sid, "assistant", `{"blocks":[]}`, nil, false, "", "member-row-1")
	if err != nil {
		t.Fatalf("AddChatMessageWithAgent: %v", err)
	}

	msgs, _, err := service.GetChatHistoryPaged(project, "codebuddy", sid, 0, 0)
	if err != nil {
		t.Fatalf("GetChatHistoryPaged: %v", err)
	}
	found := false
	for _, m := range msgs {
		if m.ID == id {
			found = true
			if m.AgentID != "member-row-1" {
				t.Fatalf("AgentID=%q, want member-row-1", m.AgentID)
			}
		}
	}
	if !found {
		t.Fatalf("message %d not returned", id)
	}
}

// TestGetMessageByIDCarriesAgentID covers the single-message reader (TTS /
// summary / RAG use it).
func TestGetMessageByIDCarriesAgentID(t *testing.T) {
	setupDB(t)
	project := "/tmp/grouptest2"
	sid := helperCreateSession(t, project, "codebuddy", "t")

	id, err := service.AddChatMessageWithAgent(project, "codebuddy", sid, "assistant", `{"blocks":[]}`, nil, false, "", "member-row-2")
	if err != nil {
		t.Fatalf("AddChatMessageWithAgent: %v", err)
	}
	msg, err := service.GetMessageByID(id)
	if err != nil {
		t.Fatalf("GetMessageByID: %v", err)
	}
	if msg.AgentID != "member-row-2" {
		t.Fatalf("AgentID=%q, want member-row-2", msg.AgentID)
	}
}
