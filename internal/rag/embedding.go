package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// EmbeddingClient calls an OpenAI-compatible /v1/embeddings endpoint.
type EmbeddingClient struct {
	BaseURL    string
	Model      string
	APIKey     string
	HTTPClient *http.Client
	dim        atomic.Int64 // auto-detected embedding dimension
}

// NormalizeEmbeddingBaseURL normalizes a user-provided OpenAI-compatible base URL
// for the /v1/embeddings endpoint. It returns a canonical root (without a trailing
// "/v1" or "/v1/embeddings" suffix) that callers append "/v1/embeddings" to.
//
// Tolerances handled:
//   - empty / whitespace-only input → error
//   - missing scheme (e.g. "localhost:11434") → "http://" is prepended
//   - non-http(s) scheme (e.g. "ftp://", "file://") → error
//   - trailing "/v1" or "/v1/embeddings" → stripped to avoid double "/v1" paths
//   - a pre-existing "/v1/embeddings" full endpoint → reduced back to the root
func NormalizeEmbeddingBaseURL(baseURL string) (string, error) {
	u := strings.TrimSpace(baseURL)
	if u == "" {
		return "", fmt.Errorf("embedding base URL is required")
	}

	// Default to http when no scheme is present.
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}

	parsed, err := url.Parse(u)
	if err != nil {
		return "", fmt.Errorf("invalid embedding base URL %q: %w", baseURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q in embedding base URL %q (must be http or https)", parsed.Scheme, baseURL)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("embedding base URL %q is missing a host", baseURL)
	}

	// Strip a trailing "/v1" or "/v1/embeddings" so callers can safely append "/v1/embeddings".
	path := strings.TrimSuffix(parsed.Path, "/")
	switch {
	case path == "/v1/embeddings" || strings.HasSuffix(path, "/v1/embeddings"):
		path = strings.TrimSuffix(path, "/v1/embeddings")
	case path == "/v1" || strings.HasSuffix(path, "/v1"):
		path = strings.TrimSuffix(path, "/v1")
	}
	parsed.Path = path

	return strings.TrimRight(parsed.String(), "/"), nil
}

// NewEmbeddingClient creates a new embedding client.
// baseURL is the OpenAI-compatible API root (e.g. "http://localhost:11434").
// model is the embedding model name.
// apiKey is the bearer token (may be empty for local servers).
func NewEmbeddingClient(baseURL, model, apiKey string) *EmbeddingClient {
	normalized, err := NormalizeEmbeddingBaseURL(baseURL)
	if err != nil {
		slog.Warn("rag: invalid embedding base URL, using as-is", slog.String("base_url", baseURL), slog.String("err", err.Error()))
		normalized = strings.TrimRight(baseURL, "/")
	}
	return &EmbeddingClient{
		BaseURL: normalized,
		Model:   model,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// openaiEmbedRequest is the request body for POST /v1/embeddings.
type openaiEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// openaiEmbeddingData is one item in the response "data" array.
type openaiEmbeddingData struct {
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// openaiEmbedResponse is the response body for POST /v1/embeddings.
type openaiEmbedResponse struct {
	Data []openaiEmbeddingData `json:"data"`
}

// openaiModelsResponse is the response body for GET /v1/models.
type openaiModelsResponse struct {
	Data []openaiModelInfo `json:"data"`
}

type openaiModelInfo struct {
	ID string `json:"id"`
}

// Embed generates an embedding vector for the given text.
func (c *EmbeddingClient) Embed(ctx context.Context, text string) ([]float64, error) {
	results, err := c.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embed: no results returned")
	}
	return results[0], nil
}

// EmbedBatch generates embeddings for multiple texts in a single API call.
// Returns embeddings in the same order as input texts.
func (c *EmbeddingClient) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) { //nolint:gocyclo // batch embedding with retry
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := openaiEmbedRequest{
		Model: c.Model,
		Input: texts,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	url := c.BaseURL + "/v1/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding API returned status %d: %s", resp.StatusCode, string(body))
	}

	var embedResp openaiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}

	if len(embedResp.Data) == 0 {
		return nil, fmt.Errorf("embedding API returned empty data for model %s", c.Model)
	}

	// Sort by index to ensure order matches input
	embeddings := make([][]float64, len(texts))
	for _, d := range embedResp.Data {
		if d.Index < 0 || d.Index >= len(embeddings) {
			return nil, fmt.Errorf("embedding API returned out-of-range index %d", d.Index)
		}
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("embedding API returned empty embedding at index %d for model %s", d.Index, c.Model)
		}
		embeddings[d.Index] = d.Embedding
	}

	// Auto-detect dimension from first response
	if c.dim.Load() == 0 && len(embeddings) > 0 && len(embeddings[0]) > 0 {
		c.dim.Store(int64(len(embeddings[0])))
		slog.Info("rag: auto-detected embedding dimension", slog.Int("dim", len(embeddings[0])))
	}

	return embeddings, nil
}

// IsHealthy checks if the embedding service is reachable and the configured model is available.
// Returns (reachable, modelAvailable, error).
// Gracefully handles servers that do not implement /v1/models.
func (c *EmbeddingClient) IsHealthy(ctx context.Context) (bool, bool, error) {
	url := c.BaseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return false, false, fmt.Errorf("create health request: %w", err)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, false, nil // Not reachable
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// Server does not implement /v1/models (e.g. some Ollama versions)
		// Assume reachable and model available if we got a 404
		return true, true, nil
	}

	if resp.StatusCode != http.StatusOK {
		return false, false, fmt.Errorf("models API returned status %d", resp.StatusCode)
	}

	var modelsResp openaiModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		// Can decode but not models list — assume reachable, model might be there
		return true, false, fmt.Errorf("decode models response: %w", err)
	}

	for _, m := range modelsResp.Data {
		if m.ID == c.Model || strings.HasPrefix(m.ID, c.Model+":") {
			return true, true, nil
		}
	}

	slog.Warn(
		"embedding service reachable but model not found",
		slog.String("model", c.Model),
		slog.Int("available_models", len(modelsResp.Data)),
	)
	return true, false, nil
}

// Dim returns the auto-detected embedding dimension, or 0 if not yet detected.
func (c *EmbeddingClient) Dim() int {
	return int(c.dim.Load())
}
