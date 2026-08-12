package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// OpenAISummarizer implements Summarizer using the OpenAI Chat Completions API format.
// It is compatible with any OpenAI-compatible endpoint (OpenAI, DeepSeek, Groq,
// OpenRouter, Ollama's /v1/chat/completions, etc.).
type OpenAISummarizer struct {
	BaseURL    string       // Full endpoint URL (e.g., "https://api.openai.com/v1/chat/completions")
	Key        string       // API key (sent as Authorization: Bearer <key>)
	Model      string       // Model name (default: "gpt-4o-mini")
	HTTPClient *http.Client // Shared HTTP client with timeout
	gs         summarizePipeline
}

// NewOpenAI creates an OpenAISummarizer with the given configuration.
// Empty model defaults to "gpt-4o-mini".
func NewOpenAI(baseURL, key, model string) *OpenAISummarizer {
	if model == "" {
		model = "gpt-4o-mini"
	}
	s := &OpenAISummarizer{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Key:     key,
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
	s.gs = NewSummarizePipeline(s.DoSummarizePass)
	return s
}

// openaiChatRequest is the request body for OpenAI Chat Completions API.
type openaiChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openaiChatMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	MaxTokens   int                 `json:"max_tokens"`
}

type openaiChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// DoRecommendPass performs the recommendation call using an OpenAI-compatible
// Chat Completions endpoint. The stable prefix (project context + quick
// commands) is sent as its own user message before the rolling conversation
// tail, so providers with automatic prefix caching (OpenAI, DeepSeek, etc.) can
// reuse the stable prefix across turns without reprocessing the whole window.
func (s *OpenAISummarizer) DoRecommendPass(ctx context.Context, systemPrompt, stable, rolling string) (string, error) {
	messages := make([]openaiChatMessage, 0, 3)
	messages = append(messages, openaiChatMessage{Role: "system", Content: systemPrompt})
	if stable != "" {
		messages = append(messages, openaiChatMessage{Role: "user", Content: stable})
	}
	messages = append(messages, openaiChatMessage{Role: "user", Content: rolling})

	reqBody := openaiChatRequest{
		Model:       s.Model,
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   1024,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai recommend request marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("openai recommend request create: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.Key)

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai recommend request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("openai API returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp openaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("openai response decode: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}

	result := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if result == "" {
		return "", fmt.Errorf("openai returned empty recommendation output")
	}
	return result, nil
}

// openaiChatResponse is the response body for OpenAI Chat Completions API.
type openaiChatResponse struct {
	Choices []openaiChoice `json:"choices"`
}

type openaiChoice struct {
	Message openaiChatMessage `json:"message"`
}

// Summarize condenses text for voice output using the OpenAI Chat Completions API.
func (s *OpenAISummarizer) Summarize(ctx context.Context, text string, language string) (string, error) {
	return s.gs.Summarize(ctx, text, language)
}

// DoSummarizePass performs a single summarization pass using the OpenAI Chat Completions API.
// Exported so that initTaskSummarizer can reuse it with a custom pipeline.
func (s *OpenAISummarizer) DoSummarizePass(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
	reqBody := openaiChatRequest{
		Model: s.Model,
		Messages: []openaiChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
		Temperature: 0.3,
		MaxTokens:   1024,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai request marshal (pass %d): %w", pass, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("openai request create (pass %d): %w", pass, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Key != "" {
		req.Header.Set("Authorization", "Bearer "+s.Key)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai request (pass %d): %w", pass, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("openai API returned status %d (pass %d): %s", resp.StatusCode, pass, string(body))
	}

	var chatResp openaiChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("openai response decode (pass %d): %w", pass, err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("openai (pass %d) returned no choices", pass)
	}

	result := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if result == "" {
		return "", fmt.Errorf("openai (pass %d) returned empty output", pass)
	}

	slog.Info(
		"tts summarize pass completed",
		slog.Int("pass", pass),
		slog.String("backend", "openai"),
		slog.String("model", s.Model),
		slog.Int("result_len", len([]rune(result))),
	)

	return result, nil
}
