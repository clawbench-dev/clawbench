package summarize

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"clawbench/internal/model"
)

// recommendNextStepPrompt is the system prompt for the next-step recommendation
// (推荐回复) feature. It asks the model to produce a single, concise action the
// user can take next, based on the recent conversation context and conclusion.
const recommendNextStepPrompt = `You are a conversation continuation assistant. Based on the recent conversation and the AI assistant's latest conclusion, suggest exactly ONE concise next step for the user to take.

Requirements:
1. Output only the next-step suggestion — a short, natural-language instruction the user could paste into the chat input to continue.
2. It should be specific and actionable (e.g. a clarifying question, a concrete task, a command to run, or a direction to explore). Take the user's recent intent into account.
3. The quick commands listed in the context are the user's frequently-used commands for you to reference, not tools you can call directly. If one fits the next step, recommend it by keeping its original command text as-is so the user can use it directly — do not paraphrase or merely name the label (e.g. output the command itself, not "用快捷指令「生成测试」"); otherwise ignore them.
4. Do not add markdown, bullet lists, prefixes, quotes, or meta-phrases like "Next step:" or "You could try".
5. Output in the requested language.
6. The suggestion is the user's next message sent to the AI assistant, so phrase it as a directive for the AI — never ask the user a question.`

// recommendPassProvider is implemented by LLM summarizers that expose a
// caching-aware recommendation call (OpenAISummarizer / AnthropicSummarizer).
// It splits the payload into a stable prefix (project context + quick commands,
// eligible for prompt caching) and a rolling tail (recent conversation +
// conclusion), so repeated recommendations reuse a cached prefix instead of
// reprocessing the whole window every turn.
type recommendPassProvider interface {
	DoRecommendPass(ctx context.Context, systemPrompt, stable, rolling string) (string, error)
}

// recommendMaxTokens is the output cap shared by the recommendation and title
// passes. They produce one short suggestion, so a small cap keeps latency and
// cost down. AskAboutContext (the /btw path) deliberately overrides it.
const recommendMaxTokens = 1024

// askPassProvider is implemented by LLM summarizers that can answer a one-shot
// question with an explicit output cap. It is the same request channel as
// recommendPassProvider (system + stable prefix + rolling tail, so the stable
// context stays eligible for prompt caching) with maxTokens lifted out of the
// hardcoded recommendation value.
type askPassProvider interface {
	DoAskPass(ctx context.Context, systemPrompt, stable, rolling string, maxTokens int) (string, error)
}

// ErrOneShotUnsupported is returned by AskAboutContext when the summarizer
// backend cannot answer one-shot questions (e.g. the "simple" extract-only
// summarizer). Callers map it to a user-facing "summary model not usable"
// message rather than a generic failure.
var ErrOneShotUnsupported = errors.New("summarizer backend does not support one-shot questions")

// finishReason* are the provider-specific spellings of "the reply hit the
// output cap": OpenAI-compatible endpoints say "length" (a few say
// "max_tokens"), Anthropic says "max_tokens".
const (
	finishReasonLength    = "length"
	finishReasonMaxTokens = "max_tokens"
)

// isTruncated reports whether a finish/stop reason means the reply was cut off
// at max_tokens. An empty or unknown reason is treated as complete: providers
// that do not report one must keep the old behavior.
func isTruncated(reason string) bool {
	return reason == finishReasonLength || reason == finishReasonMaxTokens
}

// askPassDetailProvider is implemented by backends that can also report
// whether the reply was truncated. Callers that need lossless output (the
// transcript proofreader) use it to refuse a prefix instead of storing one.
type askPassDetailProvider interface {
	DoAskPassDetailed(ctx context.Context, systemPrompt, stable, rolling string, maxTokens int) (text string, truncated bool, err error)
}

// ErrOutputTruncated is returned by AskAboutContextLossless when the model hit
// max_tokens. The partial reply is discarded: a proofread transcript that lost
// its tail is worse than no proofread at all, because the minutes would be
// summarized from the truncated copy.
var ErrOutputTruncated = errors.New("summarizer output hit max_tokens")

// AskAboutContextLossless is AskAboutContext for callers that cannot accept a
// prefix of the answer. A reply cut off at max_tokens is an error, not a
// shorter answer.
//
// Backends that cannot report the finish reason fall back to the old
// behavior: a wrong-but-silent success is not made worse here.
func AskAboutContextLossless(ctx context.Context, s Summarizer, systemPrompt, contextBlock, question string, maxTokens int) (string, error) {
	dp, ok := s.(askPassDetailProvider)
	if !ok {
		return AskAboutContext(ctx, s, systemPrompt, contextBlock, question, maxTokens)
	}
	if maxTokens <= 0 {
		maxTokens = recommendMaxTokens
	}
	text, truncated, err := dp.DoAskPassDetailed(ctx, systemPrompt, contextBlock, question, maxTokens)
	if err != nil {
		return "", err
	}
	if truncated {
		return "", fmt.Errorf("%w (max_tokens=%d)", ErrOutputTruncated, maxTokens)
	}
	return text, nil
}

// AskAboutContext answers a one-shot question from a pre-compressed context
// block. It reuses the recommendation pass channel so the (potentially large)
// stable context can hit provider prompt caching across repeated questions in
// the same session.
//
// maxTokens caps the reply; <= 0 falls back to recommendMaxTokens. Returns
// ErrOneShotUnsupported if the backend cannot answer one-shot questions.
func AskAboutContext(ctx context.Context, s Summarizer, systemPrompt, contextBlock, question string, maxTokens int) (string, error) {
	ap, ok := s.(askPassProvider)
	if !ok {
		return "", ErrOneShotUnsupported
	}
	if maxTokens <= 0 {
		maxTokens = recommendMaxTokens
	}
	return ap.DoAskPass(ctx, systemPrompt, contextBlock, question, maxTokens)
}

// NewAISummarizer builds an LLM summarizer from the shared AISummaryConfig.
// Returns nil when no provider is bound (or the legacy endpoint is empty). The
// format comes from the provider's protocol; a legacy config with no format is
// auto-detected from the URL.
func NewAISummarizer(cfg model.AISummaryConfig) Summarizer {
	baseURL, key, format, ok := cfg.SummaryEndpoint()
	if !ok {
		return nil
	}
	if ResolveFormat(baseURL, format) == "anthropic" {
		return NewAnthropic(baseURL, key, cfg.Model)
	}
	return NewOpenAI(baseURL, key, cfg.Model)
}

// ResolveFormat returns the wire format a summarizer will speak for the given
// endpoint: an explicit "anthropic"/"openai" wins, otherwise the URL decides.
// Exported so callers that only report the configuration (the startup log)
// cannot describe a different endpoint than the one actually called.
func ResolveFormat(baseURL, format string) string {
	switch format {
	case "anthropic", "openai":
		return format
	}
	if IsAnthropicURL(baseURL) {
		return "anthropic"
	}
	return "openai"
}

// RecommendNextStep generates a concise next-step suggestion based on the
// assistant's conclusion plus recent conversation context (user messages full
// text, assistant messages their conclusion), quick commands, and project
// context files. The payload is split into a stable prefix (project context +
// quick commands) and a rolling tail (conversation + conclusion) so providers
// with prompt caching (Anthropic cache_control, OpenAI-style automatic prefix
// caching) can reuse the stable prefix across turns.
// Returns an error if the summarizer backend does not support recommendation
// (e.g. the "simple" extract-conclusion summarizer).
func RecommendNextStep(ctx context.Context, s Summarizer, conversation, quickCommands, projectContext []string, conclusion, language string) (string, error) {
	pp, ok := s.(recommendPassProvider)
	if !ok {
		return "", fmt.Errorf("summarizer backend does not support next-step recommendation")
	}
	stable := buildStableContext(quickCommands, projectContext)
	rolling := buildRollingContext(conversation, conclusion)
	prompt := recommendNextStepPrompt + "\n\nOutput in " + languageName(language) + "."
	return pp.DoRecommendPass(ctx, prompt, stable, rolling)
}

// buildStableContext assembles the cacheable prefix: project context files and
// the user's quick commands. This content is stable across recommendation turns
// for a given project, so it is the part eligible for prompt caching. It returns
// an empty string when there is nothing to cache.
func buildStableContext(quickCommands, projectContext []string) string {
	var b strings.Builder
	if len(projectContext) > 0 {
		b.WriteString("Project context (rules and conventions from the project's instruction files):\n")
		for _, c := range projectContext {
			b.WriteString(c + "\n")
		}
		b.WriteString("\n")
	}
	if len(quickCommands) > 0 {
		b.WriteString("以下是我的常用指令，请在合适的时候使用：\n")
		for _, q := range quickCommands {
			b.WriteString("- " + q + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// buildRollingContext assembles the non-cacheable tail: the recent conversation
// (user messages in full, assistant messages as conclusion) plus the assistant's
// latest conclusion. This content changes every turn.
func buildRollingContext(conversation []string, conclusion string) string {
	var b strings.Builder
	if len(conversation) > 0 {
		b.WriteString("Recent conversation (user messages in full, assistant messages as conclusion):\n")
		for i, m := range conversation {
			fmt.Fprintf(&b, "%d. %s\n", i+1, m)
		}
		b.WriteString("\n")
	}
	b.WriteString("Assistant's latest conclusion:\n")
	b.WriteString(conclusion)
	return b.String()
}
