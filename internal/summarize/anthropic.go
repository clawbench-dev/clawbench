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

// AnthropicSummarizer implements Summarizer using the Anthropic Messages API format.
type AnthropicSummarizer struct {
	BaseURL    string       // Full endpoint URL (e.g., "https://api.anthropic.com/v1/messages")
	Key        string       // API key (sent as x-api-key header)
	Model      string       // Model name (default: "claude-3-5-haiku-latest")
	HTTPClient *http.Client // Shared HTTP client with timeout
	gs         summarizePipeline
}

// NewAnthropic creates an AnthropicSummarizer with the given configuration.
// Empty model defaults to "claude-3-5-haiku-latest".
func NewAnthropic(baseURL, key, model string) *AnthropicSummarizer {
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}
	s := &AnthropicSummarizer{
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

// anthropicRequest is the request body for the Anthropic Messages API.
type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the response body for the Anthropic Messages API.
type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// anthropicCacheControl marks a content block as a prompt-cache breakpoint.
type anthropicCacheControl struct {
	Type string `json:"type"`
}

// anthropicRecommendRequest is the request body for the recommendation pass,
// where the user turn carries multiple content blocks so the stable prefix can
// be marked with cache_control.
type anthropicRecommendRequest struct {
	Model       string                      `json:"model"`
	System      string                      `json:"system"`
	Messages    []anthropicRecommendMessage `json:"messages"`
	MaxTokens   int                         `json:"max_tokens"`
	Temperature float64                     `json:"temperature"`
}

type anthropicRecommendMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

// minCacheableRunes is the minimum size of the stable prefix before prompt
// caching is enabled. Providers enforce their own minimum cacheable sizes; a
// tiny prefix below this threshold would only waste a cache write, so we skip
// cache_control for small stable contexts.
const minCacheableRunes = 1024

// DoRecommendPass performs the recommendation call using the Anthropic Messages
// API. The stable prefix (project context + quick commands) is sent as the first
// user content block marked with cache_control: ephemeral so it is cached across
// turns; the rolling conversation tail follows uncached. Returns an error if the
// recommendation request or response fails.
func (s *AnthropicSummarizer) DoRecommendPass(ctx context.Context, systemPrompt, stable, rolling string) (string, error) {
	content := make([]anthropicContentBlock, 0, 2)
	if len([]rune(stable)) >= minCacheableRunes {
		content = append(content, anthropicContentBlock{
			Type:         blockTypeText,
			Text:         stable,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		})
	} else if stable != "" {
		content = append(content, anthropicContentBlock{Type: blockTypeText, Text: stable})
	}
	content = append(content, anthropicContentBlock{Type: blockTypeText, Text: rolling})

	reqBody := anthropicRecommendRequest{
		Model:  s.Model,
		System: systemPrompt,
		Messages: []anthropicRecommendMessage{
			{Role: roleUser, Content: content},
		},
		MaxTokens:   1024,
		Temperature: 0.3,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("anthropic recommend request marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("anthropic recommend request create: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if s.Key != "" {
		req.Header.Set("x-api-key", s.Key)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic recommend request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("anthropic API returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("anthropic response decode: %w", err)
	}

	var resultBuilder strings.Builder
	for _, block := range chatResp.Content {
		if block.Type == blockTypeText {
			resultBuilder.WriteString(block.Text)
		}
	}
	result := strings.TrimSpace(resultBuilder.String())
	if result == "" {
		return "", fmt.Errorf("anthropic returned empty recommendation output")
	}
	return result, nil
}

// Summarize condenses text for voice output using the Anthropic Messages API.
func (s *AnthropicSummarizer) Summarize(ctx context.Context, text string, language string) (string, error) {
	return s.gs.Summarize(ctx, text, language)
}

// DoSummarizePass performs a single summarization pass using the Anthropic Messages API.
// Exported so that initTaskSummarizer can reuse it with a custom pipeline.
func (s *AnthropicSummarizer) DoSummarizePass(ctx context.Context, text, systemPrompt string, pass int) (string, error) {
	reqBody := anthropicRequest{
		Model:  s.Model,
		System: systemPrompt,
		Messages: []anthropicMessage{
			{Role: roleUser, Content: text},
		},
		MaxTokens:   1024,
		Temperature: 0.3,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("anthropic request marshal (pass %d): %w", pass, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("anthropic request create (pass %d): %w", pass, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if s.Key != "" {
		req.Header.Set("x-api-key", s.Key)
	}

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("anthropic request (pass %d): %w", pass, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("anthropic API returned status %d (pass %d): %s", resp.StatusCode, pass, string(body))
	}

	var chatResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("anthropic response decode (pass %d): %w", pass, err)
	}

	// Extract text from content blocks
	var resultBuilder strings.Builder
	for _, block := range chatResp.Content {
		if block.Type == blockTypeText {
			resultBuilder.WriteString(block.Text)
		}
	}
	result := strings.TrimSpace(resultBuilder.String())
	if result == "" {
		return "", fmt.Errorf("anthropic (pass %d) returned empty output", pass)
	}

	slog.Info(
		"tts summarize pass completed",
		slog.Int("pass", pass),
		slog.String("backend", "anthropic"),
		slog.String("model", s.Model),
		slog.Int("result_len", len([]rune(result))),
	)

	return result, nil
}
