package summarize

import (
	"context"
	"fmt"
	"strings"

	"clawbench/internal/model"
)

// recommendNextStepPrompt is the system prompt for the next-step recommendation
// (对话推荐) feature. It asks the model to produce a single, concise action the
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

// NewAISummarizer builds an LLM summarizer from the shared AISummaryConfig.
// Returns nil if ai_summary.api.base_url is empty. The format is resolved from
// the explicit format field, or auto-detected from the URL when empty.
func NewAISummarizer(cfg model.AISummaryConfig) Summarizer {
	baseURL := cfg.API.BaseURL
	if baseURL == "" {
		return nil
	}
	switch cfg.Format {
	case "anthropic":
		return NewAnthropic(baseURL, cfg.API.Key, cfg.Model)
	case "openai":
		return NewOpenAI(baseURL, cfg.API.Key, cfg.Model)
	}
	if IsAnthropicURL(baseURL) {
		return NewAnthropic(baseURL, cfg.API.Key, cfg.Model)
	}
	return NewOpenAI(baseURL, cfg.API.Key, cfg.Model)
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
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, m))
		}
		b.WriteString("\n")
	}
	b.WriteString("Assistant's latest conclusion:\n")
	b.WriteString(conclusion)
	return b.String()
}
