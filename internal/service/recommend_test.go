package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/ws"
	"github.com/stretchr/testify/assert"
)

func setupRecommendTest(t *testing.T) (*ws.ClientSubscription, func()) {
	db, teardown := setupTestDBForChatSummary(t)
	_ = db

	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "recommend-client", "")

	cleanup := func() {
		ws.SetManagerForTest(nil)
		teardown()
		model.ConfigInstance = model.Config{}
	}
	return sub, cleanup
}

func TestTriggerChatRecommendation_Disabled(t *testing.T) {
	sub, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{} // RecommendEnabled = false
	model.ConfigInstance.AISummary.API.BaseURL = "https://example.com"

	blocks := []model.ContentBlock{{Type: "text", Text: "conclusion here"}}
	triggerChatRecommendation("sess-rec-disabled", "/test", blocks)

	if evts := sub.GetBufferedEvents(); len(evts) != 0 {
		t.Fatalf("expected no events when recommend disabled, got %d", len(evts))
	}
}

func TestTriggerChatRecommendation_NoAISummary(t *testing.T) {
	sub, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.RecommendEnabled = true // no ai_summary base_url

	blocks := []model.ContentBlock{{Type: "text", Text: "conclusion here"}}
	triggerChatRecommendation("sess-rec-noai", "/test", blocks)

	if evts := sub.GetBufferedEvents(); len(evts) != 0 {
		t.Fatalf("expected no events without ai_summary, got %d", len(evts))
	}
}

func TestTriggerChatRecommendation_EmitsEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Continue by running the tests."}}]}`))
	}))
	defer srv.Close()

	sub, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.RecommendEnabled = true
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"

	blocks := []model.ContentBlock{{Type: "text", Text: "The build passed."}}
	triggerChatRecommendation("sess-rec-emit", "/test", blocks)

	evts := sub.GetBufferedEvents()
	if len(evts) != 1 {
		t.Fatalf("expected 1 chat_recommendation event, got %d", len(evts))
	}
	assert.Equal(t, "chat_recommendation", evts[0].Event)
	data, ok := evts[0].Data.(ws.ChatRecommendationData)
	if !ok {
		t.Fatalf("unexpected data type: %T", evts[0].Data)
	}
	assert.Equal(t, "sess-rec-emit", data.SessionID)
	assert.Equal(t, "Continue by running the tests.", data.Recommendation)
}

func TestTriggerChatRecommendation_EmptyConclusion(t *testing.T) {
	sub, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.RecommendEnabled = true
	model.ConfigInstance.AISummary.API.BaseURL = "https://example.com"

	// No text blocks → empty conclusion → no call, no event
	triggerChatRecommendation("sess-rec-empty", "/test", nil)

	if evts := sub.GetBufferedEvents(); len(evts) != 0 {
		t.Fatalf("expected no events for empty conclusion, got %d", len(evts))
	}
}

func TestRecentConversation_LimitsAndOrders(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-rec-ctx"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 't')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (300, '/test', 'user', 'first', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (301, '/test', 'assistant', '{\"blocks\":[{\"type\":\"text\",\"text\":\"reply1\"}]}', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (302, '/test', 'user', 'second', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (303, '/test', 'user', 'third', ?, 0)", sessionID)

	got := recentConversation(sessionID, 3)
	// Most recent 3 messages: user "third", user "second", assistant conclusion "reply1"
	assert.Equal(t, []string{"reply1", "second", "third"}, got, "should return the most recent n messages in chronological order")

	gotAll := recentConversation(sessionID, 0)
	assert.Len(t, gotAll, 0, "n<=0 should return no context")
}

func TestSaveAndLatestChatRecommendation(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()
	_ = db

	// Persist then read back the latest recommendation.
	SaveChatRecommendation("sess-rec-persist", "/test", "first rec")
	SaveChatRecommendation("sess-rec-persist", "/test", "second rec")

	assert.Equal(t, "second rec", LatestChatRecommendation(context.Background(), "sess-rec-persist"))
	assert.Equal(t, "", LatestChatRecommendation(context.Background(), "no-such-session"))
}
