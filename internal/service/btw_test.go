package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withBtwSummaryModel points the shared AI summary model at a test server.
func withBtwSummaryModel(t *testing.T, baseURL string) {
	t.Helper()
	prev := model.ConfigInstance.AISummary
	model.ConfigInstance.AISummary = model.AISummaryConfig{
		Format: "openai",
		API:    model.APIConfig{BaseURL: baseURL, Key: "test-key"},
	}
	t.Cleanup(func() { model.ConfigInstance.AISummary = prev })
}

// The /btw budget targets 100K tokens at ~80% using the most conservative
// chars-per-token ratio in the codebase (CJK ~1.5). This guards the arithmetic
// and the "must exceed the fork default" relationship: /btw spends a whole
// context window, while the fork injection shares one with the live conversation.
func TestBtwContextBudget_TargetsEightyPercentOf100k(t *testing.T) {
	const targetTokens = 100_000
	const utilization = 0.8
	// Conservative ratio: CJK ~1.5 chars/token (rag.estimateTokens).
	const conservativeCharsPerToken = 1.5

	want := int(float64(targetTokens) * utilization * conservativeCharsPerToken)
	assert.Equal(t, want, btwContextBudgetChars, "120000 chars = 80K tokens at the conservative ratio")
	assert.Greater(t, btwContextBudgetChars, model.DefaultForkContextBudget,
		"/btw should allow more history than the fork injection default")
}

func TestBuildBtwContext_EmptySession(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	// No history → empty context; the question is still answerable.
	assert.Equal(t, "", BuildBtwContext("no-such-session"))
}

func TestBuildBtwContext_IncludesHistoryAndEnvelope(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-btw", "t", TitleSourcePlaceholder)
	insertAutoRenameUserMessage(t, 201, "sess-btw", "为什么并发一高就慢")
	_, err := WriteExec(
		"INSERT INTO chat_history (id, project_id, role, content, session_id, streaming) VALUES (?, 1, 'assistant', ?, ?, 0)",
		202, `{"blocks":[{"type":"text","text":"让我查一下连接池"}]}`, "sess-btw",
	)
	require.NoError(t, err)

	out := BuildBtwContext("sess-btw")
	assert.Contains(t, out, "为什么并发一高就慢")
	assert.Contains(t, out, "让我查一下连接池")
	// Envelope tells the model this is background, not a task to continue.
	assert.Contains(t, out, "side question")
	// Roles are capitalized for the model, matching the handler fork path.
	assert.Contains(t, out, "User: ")
}

func TestAnswerBtwQuestion_EmptyQuestion(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	_, err := AnswerBtwQuestion(context.Background(), "sess", "   ")
	assert.ErrorIs(t, err, ErrBtwEmptyQuestion)
}

func TestAnswerBtwQuestion_ModelNotConfigured(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()
	prev := model.ConfigInstance.AISummary
	model.ConfigInstance.AISummary = model.AISummaryConfig{}
	t.Cleanup(func() { model.ConfigInstance.AISummary = prev })

	_, err := AnswerBtwQuestion(context.Background(), "sess", "why?")
	assert.ErrorIs(t, err, ErrSummaryModelNotConfigured)
}

func TestAnswerBtwQuestion_Success(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		capturedBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  连接池只有 2 个连接。  "}}]}`))
	}))
	defer srv.Close()
	withBtwSummaryModel(t, srv.URL)

	insertSessionWithTitle(t, "sess-answer", "t", TitleSourcePlaceholder)
	insertAutoRenameUserMessage(t, 301, "sess-answer", "上下文里的一句话")

	rec, err := AnswerBtwQuestion(context.Background(), "sess-answer", "连接池是多大？")
	require.NoError(t, err)
	// The answer is trimmed before it reaches the drawer.
	assert.Equal(t, "连接池只有 2 个连接。", rec.Answer)
	assert.Contains(t, capturedBody, "上下文里的一句话")
	assert.Contains(t, capturedBody, "连接池是多大？")
	assert.Contains(t, capturedBody, "8192")
	// The record is persisted with the anchor at the last message (301).
	assert.NotZero(t, rec.ID, "record must be persisted")
	assert.Equal(t, int64(301), rec.AnchorMessageID)
	assert.Equal(t, "sess-answer", rec.SessionID)
	assert.Equal(t, NormalizeProjectPath("/test"), rec.ProjectPath)
	assert.Empty(t, rec.Error)

	stored, err := ListBtwQuestions("sess-answer")
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "连接池是多大？", stored[0].Question)
	assert.Equal(t, "连接池只有 2 个连接。", stored[0].Answer)
	assert.Equal(t, int64(301), stored[0].AnchorMessageID)
}

// A model failure is still recorded: the user asked the question, so the anchor
// must exist and the drawer must be able to explain why it failed.
func TestAnswerBtwQuestion_FailureIsPersisted(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withBtwSummaryModel(t, srv.URL)

	insertSessionWithTitle(t, "sess-fail", "t", TitleSourcePlaceholder)
	insertAutoRenameUserMessage(t, 501, "sess-fail", "一句话")

	rec, err := AnswerBtwQuestion(context.Background(), "sess-fail", "为什么会失败？")
	require.Error(t, err)
	assert.NotZero(t, rec.ID, "a failed question must still be persisted")
	assert.Empty(t, rec.Answer)
	assert.NotEmpty(t, rec.Error, "the failure reason must be stored")
	assert.Equal(t, int64(501), rec.AnchorMessageID)

	stored, listErr := ListBtwQuestions("sess-fail")
	require.NoError(t, listErr)
	require.Len(t, stored, 1)
	assert.NotEmpty(t, stored[0].Error)
}

// Anchor 0: a question asked before the session had any message must still be
// recorded, anchored to 0 (the client renders it at the top of the list).
func TestAnswerBtwQuestion_NoMessagesAnchorsToZero(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	withBtwSummaryModel(t, srv.URL)

	insertSessionWithTitle(t, "sess-empty", "t", TitleSourcePlaceholder)

	rec, err := AnswerBtwQuestion(context.Background(), "sess-empty", "空会话也能问？")
	require.NoError(t, err)
	assert.Equal(t, int64(0), rec.AnchorMessageID)
}

// The list is chronological (oldest first) so the client can group by anchor
// and keep the questions in the order they were asked.
func TestListBtwQuestions_Chronological(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-order", "t", TitleSourcePlaceholder)
	for i, q := range []string{"第一个", "第二个", "第三个"} {
		_, err := WriteExec(
			`INSERT INTO btw_questions (session_id, project_id, anchor_message_id, question, answer)
			 VALUES (?, 1, ?, ?, 'a')`,
			"sess-order", 600+i, q,
		)
		require.NoError(t, err)
	}

	got, err := ListBtwQuestions("sess-order")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"第一个", "第二个", "第三个"}, []string{got[0].Question, got[1].Question, got[2].Question})
}

// The list is scoped to one session.
func TestListBtwQuestions_ScopedToSession(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-a", "t", TitleSourcePlaceholder)
	insertSessionWithTitle(t, "sess-b", "t", TitleSourcePlaceholder)
	for _, sid := range []string{"sess-a", "sess-b"} {
		_, err := WriteExec(
			`INSERT INTO btw_questions (session_id, project_id, anchor_message_id, question) VALUES (?, 1, 1, ?)`,
			sid, "q-"+sid,
		)
		require.NoError(t, err)
	}

	got, err := ListBtwQuestions("sess-a")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "q-sess-a", got[0].Question)
}

// HardDeleteSession must remove the session's /btw rows (no FK to chat_sessions,
// so without the explicit delete they would linger as orphans).
func TestHardDeleteSession_RemovesBtwQuestions(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-del", "t", TitleSourcePlaceholder)
	_, err := WriteExec(
		`INSERT INTO btw_questions (session_id, project_id, anchor_message_id, question) VALUES ('sess-del', 1, 1, 'q')`,
	)
	require.NoError(t, err)

	require.NoError(t, HardDeleteSession("sess-del"))

	var count int
	require.NoError(t, dbRead.QueryRow("SELECT COUNT(*) FROM btw_questions WHERE session_id = 'sess-del'").Scan(&count))
	assert.Equal(t, 0, count, "hard delete must remove btw rows")
}

// Rewind removes the messages after the anchor; /btw markers anchored into that
// removed range must go with them, while a marker at or before the anchor stays.
func TestTruncateSessionAfterMessage_RemovesBtwInRemovedRange(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-rw", "t", TitleSourcePlaceholder)
	for id := int64(701); id <= 704; id++ {
		_, err := WriteExec(
			"INSERT INTO chat_history (id, project_id, role, content, session_id, streaming) VALUES (?, 1, 'assistant', '{}', 'sess-rw', 0)",
			id,
		)
		require.NoError(t, err)
	}
	// Anchored at the anchor message (701, survives) and at a removed one (703).
	for _, anchor := range []int64{701, 703} {
		_, err := WriteExec(
			`INSERT INTO btw_questions (session_id, project_id, anchor_message_id, question) VALUES ('sess-rw', 1, ?, 'q')`,
			anchor,
		)
		require.NoError(t, err)
	}

	_, err := TruncateSessionAfterMessage("sess-rw", 701)
	require.NoError(t, err)

	var anchors []int64
	rows, err := dbRead.Query("SELECT anchor_message_id FROM btw_questions WHERE session_id = 'sess-rw' ORDER BY anchor_message_id")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var a int64
		require.NoError(t, rows.Scan(&a))
		anchors = append(anchors, a)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{701}, anchors, "only the marker inside the removed range is deleted")
}

func TestAnswerBtwQuestion_ModelFailure(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withBtwSummaryModel(t, srv.URL)

	_, err := AnswerBtwQuestion(context.Background(), "sess", "why?")
	if err == nil {
		t.Fatal("expected error on model failure")
	}
	assert.False(t, errors.Is(err, ErrBtwEmptyQuestion))
	assert.False(t, errors.Is(err, ErrSummaryModelNotConfigured))
}

// BuildBtwContext must reuse the fork compressor: oversized assistant entries
// are DROPPED (not truncated), which is what produces the omission notice.
// Users entries are truncated instead, so an overflow made only of user
// messages carries no notice — that asymmetry is the compressor's contract.
func TestBuildBtwContext_OmissionNoticeOnOverflow(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-big", "t", TitleSourcePlaceholder)
	insertAutoRenameUserMessage(t, 401, "sess-big", "最新的一条问题")
	// Five large assistant replies; each is ~50k chars, so only two fit the
	// 120k budget and the rest are dropped.
	big := strings.Repeat("这是一段很长的历史内容。", 4000)
	for i := range 5 {
		_, err := WriteExec(
			"INSERT INTO chat_history (id, project_id, role, content, session_id, streaming) VALUES (?, 1, 'assistant', ?, ?, 0)",
			int64(410+i), `{"blocks":[{"type":"text","text":"`+big+`"}]}`, "sess-big",
		)
		require.NoError(t, err)
	}

	out := BuildBtwContext("sess-big")
	require.NotEmpty(t, out)
	assert.Contains(t, out, "were omitted")
	assert.Contains(t, out, "最新的一条问题")
	// The rendered context stays within the /btw budget.
	assert.LessOrEqual(t, len([]rune(out)), btwContextBudgetChars)
}
