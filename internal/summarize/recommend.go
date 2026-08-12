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
3. If a listed quick command fits the next step, you may reference it (e.g. "用快捷指令「生成测试」"), otherwise ignore them.
4. Do not add markdown, bullet lists, prefixes, quotes, or meta-phrases like "Next step:" or "You could try".
5. Output in the requested language.`

// recommendPassProvider is implemented by LLM summarizers that expose their
// single-pass call (OpenAISummarizer / AnthropicSummarizer). Used to build a
// recommendation pipeline with a dedicated prompt.
type recommendPassProvider interface {
	DoSummarizePass(ctx context.Context, text, systemPrompt string, pass int) (string, error)
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
// text, assistant messages their conclusion) and any available quick commands.
// It reuses the summarizer's LLM pass with a dedicated recommendation prompt.
// Returns an error if the summarizer backend does not support recommendation
// (e.g. the "simple" extract-conclusion summarizer).
func RecommendNextStep(ctx context.Context, s Summarizer, conversation, quickCommands []string, conclusion, language string) (string, error) {
	pp, ok := s.(recommendPassProvider)
	if !ok {
		return "", fmt.Errorf("summarizer backend does not support next-step recommendation")
	}
	pipeline := NewPipelineWithOpts(pp.DoSummarizePass, recommendNextStepPrompt, SummarizeOption{PreserveMarkdown: false})
	input := buildRecommendInput(conversation, quickCommands, conclusion)
	result, err := pipeline.Summarize(ctx, input, language)
	if err != nil {
		return "", err
	}
	return result, nil
}

// buildRecommendInput assembles the recent conversation, available quick
// commands, and the assistant's conclusion into the user-side text fed to the
// recommendation pipeline.
func buildRecommendInput(conversation, quickCommands []string, conclusion string) string {
	var b strings.Builder
	if len(conversation) > 0 {
		b.WriteString("Recent conversation (user messages in full, assistant messages as conclusion):\n")
		for i, m := range conversation {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, m))
		}
		b.WriteString("\n")
	}
	if len(quickCommands) > 0 {
		b.WriteString("Available quick commands (label: command) — you may suggest using one if it fits:\n")
		for _, q := range quickCommands {
			b.WriteString("- " + q + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Assistant's latest conclusion:\n")
	b.WriteString(conclusion)
	return b.String()
}
