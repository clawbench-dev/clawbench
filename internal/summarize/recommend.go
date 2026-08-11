package summarize

import (
	"context"
	"fmt"

	"clawbench/internal/model"
)

// recommendNextStepPrompt is the system prompt for the next-step recommendation
// (对话推荐) feature. It asks the model to produce a single, concise action the
// user can take next, based on the assistant's conclusion.
const recommendNextStepPrompt = `You are a conversation continuation assistant. Based on the AI assistant's conclusion below, suggest exactly ONE concise next step for the user to take.

Requirements:
1. Output only the next-step suggestion — a short, natural-language instruction the user could paste into the chat input to continue.
2. It should be specific and actionable (e.g. a clarifying question, a concrete task, a command to run, or a direction to explore).
3. Do not add markdown, bullet lists, prefixes, quotes, or meta-phrases like "Next step:" or "You could try".
4. Output in the requested language.
5. If there is no reasonable next step, output a single short question inviting the user to clarify or continue.`

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
// assistant's conclusion. It reuses the summarizer's LLM pass with a dedicated
// recommendation prompt. Returns an error if the summarizer backend does not
// support recommendation (e.g. the "simple" extract-conclusion summarizer).
func RecommendNextStep(ctx context.Context, s Summarizer, conclusion, language string) (string, error) {
	pp, ok := s.(recommendPassProvider)
	if !ok {
		return "", fmt.Errorf("summarizer backend does not support next-step recommendation")
	}
	pipeline := NewPipelineWithOpts(pp.DoSummarizePass, recommendNextStepPrompt, SummarizeOption{PreserveMarkdown: false})
	result, err := pipeline.Summarize(ctx, conclusion, language)
	if err != nil {
		return "", err
	}
	return result, nil
}
