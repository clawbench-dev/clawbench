package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
)

// BtwQuestionsDDL creates the table holding "/btw" side questions.
//
// Each row records one side question asked while the user was at a particular
// point in the conversation. anchor_message_id is the message the question was
// asked "after" — the last message that existed when the question was sent, so
// the chat list can render a marker at that position even after the reply
// finishes streaming. 0 means the session had no messages yet (the marker then
// sits at the top of the list).
//
// Both the answer and the error are stored: a failed question is still a thing
// the user asked, and the marker must appear for it too (the drawer then shows
// why it failed rather than silently omitting it).
//
// No foreign key to chat_history: anchor_message_id is a position marker, and
// rewind legitimately deletes the anchored message while the /btw record is
// cleaned up by the same statement. session_id is indexed so every
// session-deletion path can clean up in one statement.
const BtwQuestionsDDL = `
CREATE TABLE IF NOT EXISTS btw_questions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id TEXT NOT NULL,
	project_path TEXT NOT NULL DEFAULT '',
	anchor_message_id INTEGER NOT NULL DEFAULT 0,
	question TEXT NOT NULL,
	answer TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_btw_questions_session ON btw_questions(session_id, anchor_message_id, id);
`

// BtwQuestion is one stored "/btw" side question.
type BtwQuestion struct {
	ID              int64  `json:"id"`
	SessionID       string `json:"sessionId"`
	ProjectPath     string `json:"projectPath"`
	AnchorMessageID int64  `json:"anchorMessageId"`
	Question        string `json:"question"`
	Answer          string `json:"answer"`
	Model           string `json:"model"`
	Error           string `json:"error"`
	CreatedAt       string `json:"createdAt"`
}

// btwContextBudgetChars bounds the session history handed to the summary model
// for a "/btw" question.
//
// "/btw" reuses the fork-context compressor (L0 structural trimming + L1
// priority tail window, see fork_context.go), so the budget has the same
// character semantics. The value targets a 100K-token context window at ~80%
// utilization: 80,000 tokens. ClawBench has no tokenizer or per-model context
// metadata, so the conversion uses the most conservative ratio found in the
// codebase (rag.estimateTokens: CJK ~1.5 chars/token) — 80,000 x 1.5 = 120,000
// characters, which cannot exceed 80K tokens for any content mix.
//
// Deliberately a fixed constant rather than a config key: /btw is a convenience
// path and the budget only needs to be "large enough to be safe", not tuned.
const btwContextBudgetChars = 120000

// btwMaxTokens caps the "/btw" answer. The reply length is deliberately not
// treated as a tunable: a large fixed value is enough for a side answer, and
// Anthropic's max_tokens is a required field so "unlimited" is not expressible.
const btwMaxTokens = 8192

// BtwSystemPrompt is the system prompt for the "/btw" side question. The model
// is a summary model, not the session's agent: it has no tools, no file access,
// and must not pretend otherwise. It answers strictly from the supplied
// conversation context.
const BtwSystemPrompt = `You are a helpful assistant answering a side question about an ongoing conversation between a user and an AI coding agent.

Rules:
1. Answer the user's question directly and concisely, using the conversation history provided as context.
2. The conversation history is background material only. Do NOT continue the conversation, do NOT perform or pretend to perform any task, and do NOT call tools — you have none.
3. If the history does not contain enough information to answer, say so plainly and answer from general knowledge if you can, making clear which part is not from the conversation.
4. Use Markdown for formatting when it improves readability (lists, tables, code blocks).
5. Reply in the same language as the user's question.`

// BuildBtwContext renders the session's history for a "/btw" question, bounded
// by btwContextBudgetChars. It reuses the fork-context compressor so /btw and
// fork/rewind cannot drift apart in how they trim and prioritize history.
//
// Returns "" when the session has no renderable history; the caller still asks
// the question, just without context.
func BuildBtwContext(sessionID string) string {
	return BuildForkContextWithOptions(sessionID, ForkContextOptions{
		Header:            "[Below is the current conversation between the user and the AI coding agent. Use it as background to answer the user's side question.]\n\n",
		Footer:            "[End of conversation history. Now answer the user's side question.]\n\n",
		CapitalizeRoles:   true,
		PlainTextFallback: true,
		BudgetChars:       btwContextBudgetChars,
	})
}

// ErrBtwEmptyQuestion is returned when the question has no non-whitespace text.
var ErrBtwEmptyQuestion = errors.New("btw question is empty")

// AnswerBtwQuestion answers a side question about a session using the shared AI
// summary model, and records it.
//
// The session history is compressed with the fork-context compressor
// (BuildBtwContext) and sent as the stable, cacheable prefix; the question is
// the rolling tail.
//
// The record is persisted whether the model call succeeds or fails: a failed
// question is still something the user asked, and the chat list must be able to
// show its marker (the drawer then explains the failure). The anchor is the
// last message id at ask time, so the marker lands where the user was.
//
// The returned BtwQuestion is the stored row. On failure both the row and the
// error are returned — callers that only care about the error can ignore the row.
func AnswerBtwQuestion(ctx context.Context, sessionID, question string) (BtwQuestion, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return BtwQuestion{}, ErrBtwEmptyQuestion
	}

	anchorID := lastMessageID(sessionID)
	projectPath := GetSessionProjectPath(sessionID)

	// Resolve from config (not the TTS summarizer global): ai_summary is the
	// user's chosen summary model and NewAISummarizer auto-detects the API
	// format. The TTS summarizer may be the "simple" backend, which cannot
	// answer questions.
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		rec := persistBtwQuestion(sessionID, projectPath, anchorID, question, "", "", ErrSummaryModelNotConfigured.Error())
		return rec, ErrSummaryModelNotConfigured
	}

	modelName := model.ConfigInstance.AISummary.Model

	// NewAISummarizer returns nil or a real LLM summarizer (both support the ask
	// pass), so ErrOneShotUnsupported is unreachable here and needs no mapping.
	answer, err := summarize.AskAboutContext(ctx, summarizer, BtwSystemPrompt, BuildBtwContext(sessionID), question, btwMaxTokens)
	if err != nil {
		rec := persistBtwQuestion(sessionID, projectPath, anchorID, question, "", modelName, err.Error())
		return rec, err
	}
	answer = strings.TrimSpace(answer)
	return persistBtwQuestion(sessionID, projectPath, anchorID, question, answer, modelName, ""), nil
}

// lastMessageID returns the highest chat_history id for a session, or 0 when
// the session has no messages. This is the /btw anchor: the position the user
// was looking at when they asked.
func lastMessageID(sessionID string) int64 {
	var id int64
	_ = dbRead.QueryRowContext(context.Background(),
		"SELECT COALESCE(MAX(id), 0) FROM chat_history WHERE session_id = ?", sessionID).Scan(&id)
	return id
}

// persistBtwQuestion inserts a /btw record and returns it with its assigned id
// and created_at. A write failure is logged rather than returned: the answer is
// already in hand, and losing the anchor row must not turn a successful
// question into an error the user sees. The row is still returned (with id 0)
// so the caller can render it.
func persistBtwQuestion(sessionID, projectPath string, anchorID int64, question, answer, modelName, errMsg string) BtwQuestion {
	rec := BtwQuestion{
		SessionID:       sessionID,
		ProjectPath:     projectPath,
		AnchorMessageID: anchorID,
		Question:        question,
		Answer:          answer,
		Model:           modelName,
		Error:           errMsg,
	}
	res, err := WriteExec(
		`INSERT INTO btw_questions (session_id, project_path, anchor_message_id, question, answer, model, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, projectPath, anchorID, question, answer, modelName, errMsg,
	)
	if err != nil {
		slog.Error("failed to persist btw question", "session_id", sessionID, "err", err)
		return rec
	}
	if id, idErr := res.LastInsertId(); idErr == nil {
		rec.ID = id
	}
	// created_at is written by SQLite's CURRENT_TIMESTAMP default, so read it
	// back rather than writing a Go time (which would break the string ordering
	// every other table relies on).
	if rec.ID > 0 {
		_ = dbRead.QueryRowContext(context.Background(),
			"SELECT created_at FROM btw_questions WHERE id = ?", rec.ID).Scan(&rec.CreatedAt)
	}
	return rec
}

// ListBtwQuestions returns a session's /btw records in chronological order,
// oldest first. The chat list groups them by anchor to render its markers.
func ListBtwQuestions(sessionID string) ([]BtwQuestion, error) {
	rows, err := dbRead.QueryContext(context.Background(),
		`SELECT id, session_id, project_path, anchor_message_id, question, answer, model, error, created_at
		 FROM btw_questions WHERE session_id = ? ORDER BY id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query btw questions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BtwQuestion
	for rows.Next() {
		var r BtwQuestion
		var createdAt time.Time
		if err := rows.Scan(&r.ID, &r.SessionID, &r.ProjectPath, &r.AnchorMessageID, &r.Question, &r.Answer, &r.Model, &r.Error, &createdAt); err != nil {
			return nil, fmt.Errorf("failed to scan btw question: %w", err)
		}
		r.CreatedAt = formatBtwTime(createdAt)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// formatBtwTime renders a stored timestamp as RFC3339. The frontend formats it
// for display; sending the raw value keeps the timezone decision in one place.
func formatBtwTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
