package service

import (
	"context"
	"errors"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
)

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
// summary model.
//
// The session history is compressed with the fork-context compressor
// (BuildBtwContext) and sent as the stable, cacheable prefix; the question is
// the rolling tail. Nothing is persisted — the answer exists only for the
// caller to display.
func AnswerBtwQuestion(ctx context.Context, sessionID, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", ErrBtwEmptyQuestion
	}

	// Resolve from config (not the TTS summarizer global): ai_summary is the
	// user's chosen summary model and NewAISummarizer auto-detects the API
	// format. The TTS summarizer may be the "simple" backend, which cannot
	// answer questions.
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}

	// NewAISummarizer returns nil or a real LLM summarizer (both support the ask
	// pass), so ErrOneShotUnsupported is unreachable here and needs no mapping.
	answer, err := summarize.AskAboutContext(ctx, summarizer, BtwSystemPrompt, BuildBtwContext(sessionID), question, btwMaxTokens)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}
