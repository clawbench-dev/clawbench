package service

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/ws"
	"github.com/stretchr/testify/assert"
)

func setupRecommendTest(t *testing.T) (*ws.Manager, *ws.ClientSubscription, func()) {
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
	return mgr, sub, cleanup
}

func TestTriggerChatRecommendation_Disabled(t *testing.T) {
	_, sub, cleanup := setupRecommendTest(t)
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
	_, sub, cleanup := setupRecommendTest(t)
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

	_, sub, cleanup := setupRecommendTest(t)
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
	_, sub, cleanup := setupRecommendTest(t)
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
