package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAutoRenameTest prepares a DB + WS manager and a fake OpenAI-compatible
// summary server that returns titleBody. It restores global config and the WS
// manager on cleanup so tests do not leak into each other.
func setupAutoRenameTest(t *testing.T, handler http.HandlerFunc) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	_, cleanup := setupRecommendTest(t)
	return srv, func() {
		srv.Close()
		cleanup()
	}
}

// titleServer returns a summary server that always answers with body.
func titleServer(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func insertSessionWithTitle(t *testing.T, sessionID, title, source string) {
	t.Helper()
	_, err := WriteExec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, title_source) VALUES (?, '/test', 'claude', ?, ?)",
		sessionID, title, source,
	)
	require.NoError(t, err)
}

func insertAutoRenameUserMessage(t *testing.T, id int64, sessionID, content string) {
	t.Helper()
	_, err := WriteExec(
		"INSERT INTO chat_history (id, project_path, role, content, session_id, streaming) VALUES (?, '/test', 'user', ?, ?, 0)",
		id, content, sessionID,
	)
	require.NoError(t, err)
}

func sessionTitleOf(t *testing.T, sessionID string) string {
	t.Helper()
	var title string
	require.NoError(t, dbRead.QueryRow("SELECT title FROM chat_sessions WHERE id = ?", sessionID).Scan(&title))
	return title
}

// ── CollectSessionUserMessages ──

// A fork/continued session has its source history copied in, so the whole
// history at titling time IS "old messages + the new one" — no special-casing.
func TestCollectSessionUserMessages_IncludesCopiedHistory(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-collect", "t", TitleSourcePlaceholder)
	insertAutoRenameUserMessage(t, 101, "sess-collect", "继承来的旧问题")
	insertAutoRenameUserMessage(t, 102, "sess-collect", "分支上的新问题")

	got := CollectSessionUserMessages("sess-collect", "")
	assert.Equal(t, []string{"继承来的旧问题", "分支上的新问题"}, got)
}

// A queued first message is not in chat_history yet, so it must be appended via
// extraText — otherwise the AI would have nothing to summarize.
func TestCollectSessionUserMessages_AppendsExtraText(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-collect-extra", "t", TitleSourcePlaceholder)

	got := CollectSessionUserMessages("sess-collect-extra", "排队中的第一条")
	assert.Equal(t, []string{"排队中的第一条"}, got)
}

// extraText equal to the last stored message must not be duplicated: on the
// normal send path the message is already persisted, and it is passed as the
// trigger text too.
func TestCollectSessionUserMessages_DedupesTrailingExtraText(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-collect-dup", "t", TitleSourcePlaceholder)
	insertAutoRenameUserMessage(t, 111, "sess-collect-dup", "同一条消息")

	got := CollectSessionUserMessages("sess-collect-dup", "同一条消息")
	assert.Equal(t, []string{"同一条消息"}, got)
}

// ── GenerateSessionTitleFromMessages ──

func TestGenerateSessionTitleFromMessages_NoModelConfigured(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{} // no ai_summary base_url
	_, err := GenerateSessionTitleFromMessages(context.Background(), "sess-nomodel", "")
	assert.ErrorIs(t, err, ErrSummaryModelNotConfigured)
}

func TestGenerateSessionTitleFromMessages_NoUserMessages(t *testing.T) {
	srv, cleanup := setupAutoRenameTest(t, titleServer(`{"choices":[{"message":{"content":"标题"}}]}`))
	defer cleanup()

	insertSessionWithTitle(t, "sess-empty", "t", TitleSourcePlaceholder)
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"

	_, err := GenerateSessionTitleFromMessages(context.Background(), "sess-empty", "")
	assert.ErrorIs(t, err, ErrNoUserMessages)
}

func TestGenerateSessionTitleFromMessages_ReturnsModelTitle(t *testing.T) {
	srv, cleanup := setupAutoRenameTest(t, titleServer(`{"choices":[{"message":{"content":"修复登录超时"}}]}`))
	defer cleanup()

	insertSessionWithTitle(t, "sess-gen", "t", TitleSourcePlaceholder)
	insertAutoRenameUserMessage(t, 121, "sess-gen", "登录总是超时")
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"

	title, err := GenerateSessionTitleFromMessages(context.Background(), "sess-gen", "")
	require.NoError(t, err)
	assert.Equal(t, "修复登录超时", title)
}

// ── SetSessionTitleAutoIfNotCustom (overwrite guard) ──

func TestSetSessionTitleAutoIfNotCustom_OverwritesPlaceholderAndAuto(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	for _, source := range []string{TitleSourcePlaceholder, TitleSourceAuto, ""} {
		sid := "sess-guard-" + source
		insertSessionWithTitle(t, sid, "旧标题", source)
		applied, err := SetSessionTitleAutoIfNotCustom(sid, "AI 标题")
		require.NoError(t, err)
		assert.True(t, applied, "source %q must be overwritable", source)
		assert.Equal(t, "AI 标题", sessionTitleOf(t, sid))
	}
}

// The user renaming mid-flight writes 'custom'; the AI result must not clobber
// it. This is the race the SQL guard exists to close.
func TestSetSessionTitleAutoIfNotCustom_NeverOverwritesCustom(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-guard-custom", "用户起的名字", TitleSourceCustom)
	applied, err := SetSessionTitleAutoIfNotCustom("sess-guard-custom", "AI 标题")
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, "用户起的名字", sessionTitleOf(t, "sess-guard-custom"))
}

// ── autoRenameSession (end to end, synchronous call) ──

func TestAutoRenameSession_AppliesModelTitle(t *testing.T) {
	srv, cleanup := setupAutoRenameTest(t, titleServer(`{"choices":[{"message":{"content":"AI 生成的标题"}}]}`))
	defer cleanup()

	insertSessionWithTitle(t, "sess-auto", "本地标题", TitleSourceAuto)
	insertAutoRenameUserMessage(t, 131, "sess-auto", "帮我看下登录")
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"
	model.ChatAutoRenameEnabled = true
	t.Cleanup(func() { model.ChatAutoRenameEnabled = false })

	autoRenameSession(context.Background(), "sess-auto", "帮我看下登录")
	assert.Equal(t, "AI 生成的标题", sessionTitleOf(t, "sess-auto"))
}

// A model failure must leave the local title untouched (the fallback contract).
func TestAutoRenameSession_ModelFailureKeepsLocalTitle(t *testing.T) {
	srv, cleanup := setupAutoRenameTest(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer cleanup()

	insertSessionWithTitle(t, "sess-auto-fail", "本地标题", TitleSourceAuto)
	insertAutoRenameUserMessage(t, 141, "sess-auto-fail", "问题")
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"
	model.ChatAutoRenameEnabled = true
	t.Cleanup(func() { model.ChatAutoRenameEnabled = false })

	autoRenameSession(context.Background(), "sess-auto-fail", "问题")
	assert.Equal(t, "本地标题", sessionTitleOf(t, "sess-auto-fail"))
}

// A user rename that lands while the model is thinking wins; the AI title is
// discarded. Simulated by writing 'custom' before the rename runs.
func TestAutoRenameSession_UserRenameDuringCallWins(t *testing.T) {
	srv, cleanup := setupAutoRenameTest(t, titleServer(`{"choices":[{"message":{"content":"AI 标题"}}]}`))
	defer cleanup()

	insertSessionWithTitle(t, "sess-auto-race", "本地标题", TitleSourceAuto)
	insertAutoRenameUserMessage(t, 151, "sess-auto-race", "问题")
	model.ConfigInstance = model.Config{}
	model.ConfigInstance.AISummary.API.BaseURL = srv.URL
	model.ConfigInstance.AISummary.Format = "openai"
	model.ChatAutoRenameEnabled = true
	t.Cleanup(func() { model.ChatAutoRenameEnabled = false })

	// The user renames while the (fake, fast) model call is in flight.
	require.NoError(t, SetSessionTitleLocked("sess-auto-race", "用户手动改的"))
	autoRenameSession(context.Background(), "sess-auto-race", "问题")
	assert.Equal(t, "用户手动改的", sessionTitleOf(t, "sess-auto-race"))
}

// ── AutoRenameEnabled gate ──

func TestAutoRenameEnabled_RequiresToggleAndModel(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	model.ConfigInstance = model.Config{}
	model.ChatAutoRenameEnabled = false
	t.Cleanup(func() { model.ChatAutoRenameEnabled = false })

	assert.False(t, AutoRenameEnabled(), "off when toggle is off")

	model.ChatAutoRenameEnabled = true
	assert.False(t, AutoRenameEnabled(), "off when no summary model is configured")

	model.ConfigInstance.AISummary.API.BaseURL = "https://example.com"
	assert.True(t, AutoRenameEnabled())
}

// ScheduleAutoRename is a no-op when the feature is off — callers invoke it
// unconditionally after titling.
func TestScheduleAutoRename_DisabledIsNoop(t *testing.T) {
	_, cleanup := setupRecommendTest(t)
	defer cleanup()

	insertSessionWithTitle(t, "sess-sched-off", "本地标题", TitleSourceAuto)
	model.ConfigInstance = model.Config{}
	model.ChatAutoRenameEnabled = false
	t.Cleanup(func() { model.ChatAutoRenameEnabled = false })

	ScheduleAutoRename("sess-sched-off", "问题")
	assert.Equal(t, "本地标题", sessionTitleOf(t, "sess-sched-off"))
}
