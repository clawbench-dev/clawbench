package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
)

// --- triggerChatRecommendation error paths ---

func TestTriggerChatRecommendation_RecommendError(t *testing.T) {
	// AISummary server returns 500 → RecommendNextStep errors → no event.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	sub, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.RecommendEnabled = true
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"

	blocks := []model.ContentBlock{{Type: "text", Text: "conclusion"}}
	triggerChatRecommendation("sess-rec-err", "/test", 21, blocks)

	assert.Empty(t, sub.GetBufferedEvents(), "no event expected when recommendation call fails")
}

func TestTriggerChatRecommendation_NilManager(t *testing.T) {
	// Recommendation succeeds but ws manager is nil → no broadcast, no panic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Continue the work."}}]}`))
	}))
	defer srv.Close()

	_, teardown := setupTestDBForChatSummary(t)
	defer teardown()
	ws.SetManagerForTest(nil)

	model.ConfigInstance = model.Config{}
	model.ConfigInstance.Chat.RecommendEnabled = true
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"

	blocks := []model.ContentBlock{{Type: "text", Text: "conclusion"}}
	assert.NotPanics(t, func() {
		triggerChatRecommendation("sess-rec-nilmgr", "/test", 22, blocks)
	})
}

// --- SaveChatRecommendation error path ---

func TestSaveChatRecommendation_DBError(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	_, _ = db.Exec("DROP TABLE chat_recommendations")

	// Should not panic, just log and return.
	assert.NotPanics(t, func() {
		SaveChatRecommendation("sess-save-err", "/test", 23, "rec")
	})
}

// --- recentConversation ---

func TestRecentConversation_LoadError(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	_, _ = db.Exec("DROP TABLE chat_history")

	assert.Nil(t, recentConversation("sess-rc-load-err", 5))
}

func TestRecentConversation_SkipsOtherRolesAndEmptyText(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	sessionID := "sess-rc-skip"
	_, _ = db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title) VALUES (?, '/test', 'claude', 't')", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (900, '/test', 'user', 'hello', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (901, '/test', 'assistant', '{\"blocks\":[{\"type\":\"text\",\"text\":\"reply\"}]}', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (902, '/test', 'system', 'sysmsg', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (903, '/test', 'user', '   ', ?, 0)", sessionID)
	_, _ = db.Exec("INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (904, '/test', 'user', 'world', ?, 0)", sessionID)

	// system role (902) and empty-text user (903) are skipped; remaining
	// messages returned in chronological order.
	got := recentConversation(sessionID, 10)
	assert.Equal(t, []string{"hello", "reply", "world"}, got)
}

// --- assistantConclusion ---

func TestAssistantConclusion_InvalidJSON(t *testing.T) {
	content := `{"blocks": broken`
	assert.Equal(t, content, assistantConclusion(content))
}

func TestAssistantConclusion_NonBlocksPrefix(t *testing.T) {
	assert.Equal(t, "plain reply", assistantConclusion("plain reply"))
}

// --- askQuestionText ---

func TestAskQuestionText_NonInteractiveToolSkipped(t *testing.T) {
	// PermissionApproval is an auto-expand tool (so it lands in cards.Tools)
	// but its name != AskUserQuestion → the tool is skipped (continue path).
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "PermissionApproval", ID: "t1", Input: map[string]any{}},
	}
	assert.Equal(t, "", askQuestionText(blocks))
}

func TestAskQuestionText_MissingQuestionsKey(t *testing.T) {
	// AskUserQuestion tool with no "questions" input → skipped.
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "AskUserQuestion", ID: "t1", Input: map[string]any{"foo": 1}},
	}
	assert.Equal(t, "", askQuestionText(blocks))
}

func TestAskQuestionText_MarshalError(t *testing.T) {
	// "questions" value cannot be marshaled → skipped.
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "AskUserQuestion", ID: "t1", Input: map[string]any{"questions": func() {}}},
	}
	assert.Equal(t, "", askQuestionText(blocks))
}

func TestAskQuestionText_UnmarshalError(t *testing.T) {
	// "questions" is a string, not a []AskQuestionCard → unmarshal fails → skipped.
	blocks := []model.ContentBlock{
		{Type: "tool_use", Name: "AskUserQuestion", ID: "t1", Input: map[string]any{"questions": "not an array"}},
	}
	assert.Equal(t, "", askQuestionText(blocks))
}

func TestAskQuestionText_RendersOptions(t *testing.T) {
	// Renders question + options; options with empty label are skipped,
	// options with a description get "(desc)" appended.
	blocks := []model.ContentBlock{
		{
			Type: "tool_use",
			Name: "AskUserQuestion",
			ID:   "t1",
			Input: map[string]any{
				"questions": []any{
					map[string]any{
						"question": "Pick one",
						"options": []any{
							map[string]any{"label": ""},
							map[string]any{"label": "Keep", "description": "keep it"},
							map[string]any{"label": "Drop"},
						},
					},
				},
			},
		},
	}
	expected := "\n[AI asks the user to choose]\nQuestion: Pick one\n- Keep (keep it)\n- Drop\n"
	assert.Equal(t, expected, askQuestionText(blocks))
}

// --- quickCommandList error path ---

func TestQuickCommandList_DBError(t *testing.T) {
	db, teardown := setupTestDBForChatSummary(t)
	defer teardown()

	_, _ = db.Exec("DROP TABLE chat_quick_send")

	assert.Nil(t, quickCommandList("/test"))
}
