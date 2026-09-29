package service

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"
)

// Session auto-rename (会话自动命名).
//
// This is the AI-summarization layer that sits ON TOP of the synchronous local
// auto-title in chat.go's maybeAutoTitleSessionTx. The local title is written
// first (so the session has a name immediately); when the user has enabled
// chat.auto_rename_enabled and the shared ai_summary model is configured, this
// layer asynchronously replaces it with an AI summary of the session's user
// messages.
//
// It deliberately reuses the SAME title-generation core as the manual
// "generate title" button (handler.ServeGenerateSessionTitle →
// GenerateSessionTitleFromMessages). Manual and automatic are the same
// capability reached two ways; a second implementation would drift (prompt,
// model selection, truncation), so there is exactly one.

// titleGenerateTimeout bounds the background AI call. Mirrors the 60s ceiling
// the manual generate-title endpoint applies (and the recommendation pass).
const titleGenerateTimeout = 60 * time.Second

// CollectSessionUserMessages returns the plain text of every user message in a
// session, oldest first, with extraText appended when it adds something the DB
// does not already have.
//
// The whole history is returned on purpose: a fork or a continued task session
// copies the source history in, so "all user messages at this moment" is
// already "the old messages plus the new one" — exactly what those flows need
// summarized, with no special-casing here. extraText covers the one case the
// DB cannot: a queued first message, which lives in queued_messages until it is
// dequeued.
func CollectSessionUserMessages(sessionID, extraText string) []string {
	messages, err := GetMessagesBySessionID(sessionID)
	if err != nil {
		slog.Warn("auto-rename: failed to read session messages",
			slog.String("session", sessionID), slog.String("err", err.Error()))
		return nil
	}
	out := make([]string, 0, len(messages)+1)
	last := ""
	for _, m := range messages {
		if m.Role != roleUser {
			continue
		}
		text := strings.TrimSpace(ExtractPlainText(m.Content))
		if text == "" {
			continue
		}
		out = append(out, text)
		last = text
	}
	if extra := strings.TrimSpace(extraText); extra != "" && extra != last {
		out = append(out, extra)
	}
	return out
}

// ErrSummaryModelNotConfigured / ErrNoUserMessages are the expected "nothing to
// do" outcomes, distinguishable from a real model failure via errors.Is so
// callers do not match on message text.
var (
	ErrSummaryModelNotConfigured = errors.New("summary model not configured")
	ErrNoUserMessages            = errors.New("no user messages to summarize")
)

// ConfigSummaryModelMissing reports whether the shared AI summary model is
// unconfigured — the single source of truth for that check, shared by the
// manual generate-title endpoint and the automatic rename path.
func ConfigSummaryModelMissing() bool {
	return model.ConfigInstance.AISummary.API.BaseURL == ""
}

// IsNoUserMessagesError reports whether err is the "nothing to summarize"
// outcome, which the manual endpoint maps to a 400 rather than a 500.
func IsNoUserMessagesError(err error) bool {
	return errors.Is(err, ErrNoUserMessages)
}

// GenerateSessionTitleFromMessages is the single AI title-generation core, used
// by BOTH the manual generate-title endpoint and the automatic rename path. It
// collects the session's user messages (plus an optional trigger message) and
// asks the shared ai_summary model for a title.
//
// Returns an error when the summary model is not configured, when there is no
// user text to summarize, or when the model call fails — callers on the
// automatic path treat all of these as "keep the local title".
func GenerateSessionTitleFromMessages(ctx context.Context, sessionID, extraText string) (string, error) {
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}
	userMessages := CollectSessionUserMessages(sessionID, extraText)
	if len(userMessages) == 0 {
		return "", ErrNoUserMessages
	}
	language := model.ConfigInstance.Language
	if language == "" {
		language = model.DefaultLanguage
	}
	return summarize.GenerateSessionTitle(ctx, summarizer, userMessages, language)
}

// AutoRenameEnabled reports whether the automatic AI rename is both switched on
// and actually usable (the shared summary model is configured). Without the
// model there is nothing to call, so the feature is effectively off.
func AutoRenameEnabled() bool {
	return model.ChatAutoRenameEnabled && !ConfigSummaryModelMissing()
}

// ScheduleAutoRename starts the background AI rename for a session whose local
// title was just written. It is a no-op unless the feature is enabled and
// usable, so callers can invoke it unconditionally after titling.
//
// triggerText is the message that caused the title write; it is appended to the
// collected history so a queued (not-yet-persisted) message is included.
func ScheduleAutoRename(sessionID, triggerText string) {
	if !AutoRenameEnabled() {
		return
	}
	// Detached from any request/session context: the AI call legitimately
	// outlives the turn that triggered it, and a session cancel must not abort
	// a rename the user explicitly opted into.
	go autoRenameSession(context.Background(), sessionID, triggerText)
}

// autoRenameSession generates a title and, if the session has not been renamed
// by the user in the meantime, persists and broadcasts it. Runs detached, so it
// is panic-guarded and applies its own timeout.
func autoRenameSession(ctx context.Context, sessionID, triggerText string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("auto-rename goroutine panicked",
				slog.String("session", sessionID),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, titleGenerateTimeout)
	defer cancel()

	title, err := GenerateSessionTitleFromMessages(ctx, sessionID, triggerText)
	if err != nil {
		// Expected when the model is unconfigured or the call failed: keep the
		// local title. Logged at debug so a misconfigured model is diagnosable
		// without spamming the log on every message.
		slog.Debug("auto-rename skipped",
			slog.String("session", sessionID), slog.String("err", err.Error()))
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}

	// Atomic guard: the AI call takes seconds, during which the user may have
	// renamed the session by hand (title_source='custom'). Never clobber that.
	applied, err := SetSessionTitleAutoIfNotCustom(sessionID, title)
	if err != nil {
		slog.Warn("auto-rename: failed to persist title",
			slog.String("session", sessionID), slog.String("err", err.Error()))
		return
	}
	if !applied {
		// The user renamed the session while the model was working — their
		// choice wins.
		slog.Debug("auto-rename: session renamed by user, keeping their title",
			slog.String("session", sessionID))
		return
	}

	BroadcastSessionTitleUpdate(sessionID, title)
}

// SetSessionTitleAutoIfNotCustom persists an auto-generated title unless the
// session's title was deliberately chosen (title_source='custom'). The guard is
// in the UPDATE's WHERE clause, not a prior SELECT, so it cannot race a
// concurrent manual rename. Returns whether the row was updated.
func SetSessionTitleAutoIfNotCustom(sessionID, title string) (bool, error) {
	res, err := WriteExec(
		"UPDATE chat_sessions SET title = ?, title_source = ? WHERE id = ? AND COALESCE(title_source, '') <> ?",
		title, TitleSourceAuto, sessionID, TitleSourceCustom,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// BroadcastSessionTitleUpdate tells connected clients that a session's title
// changed outside the normal rename flow, so an open chat header and a mounted
// session list pick it up. The rename endpoint itself relies on the client that
// made the change; this event exists for changes the client did not initiate.
func BroadcastSessionTitleUpdate(sessionID, title string) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "session_title_update",
		Data: ws.SessionTitleUpdateData{
			SessionID:   sessionID,
			Title:       title,
			ProjectPath: GetSessionProjectPath(sessionID),
		},
	})
}
