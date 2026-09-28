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
		"INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (?, '/test', 'assistant', ?, ?, 0)",
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

	answer, err := AnswerBtwQuestion(context.Background(), "sess-answer", "连接池是多大？")
	require.NoError(t, err)
	// The answer is trimmed before it reaches the drawer.
	assert.Equal(t, "连接池只有 2 个连接。", answer)
	assert.Contains(t, capturedBody, "上下文里的一句话")
	assert.Contains(t, capturedBody, "连接池是多大？")
	assert.Contains(t, capturedBody, "8192")
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
			"INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (?, '/test', 'assistant', ?, ?, 0)",
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
