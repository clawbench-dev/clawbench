package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// VLLMProvider calls an OpenAI-compatible /v1/audio/transcriptions endpoint
// (vLLM Whisper server). Supports streaming and non-streaming at the handler
// layer; this provider only transcribes a single audio segment.
type VLLMProvider struct {
	BaseURL    string       // e.g. "http://localhost:8000" or ".../v1"
	Model      string       // recognition model, e.g. "openai/whisper-large-v3"
	APIKey     string       // bearer token (may be empty for local servers)
	Language   string       // language code (e.g. "zh")
	HTTPClient *http.Client // injectable for tests
}

// NewVLLMProvider creates a VLLM STT provider.
// baseURL is the OpenAI-compatible API root (with or without trailing "/v1").
func NewVLLMProvider(baseURL, model, apiKey, language string) *VLLMProvider {
	return &VLLMProvider{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Model:    model,
		APIKey:   apiKey,
		Language: language,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// openaiTranscribeResponse is the OpenAI-compatible transcription response.
type openaiTranscribeResponse struct {
	Text string `json:"text"`
}

// transcriptionEndpointPath is the OpenAI-compatible transcription path.
const transcriptionEndpointPath = "/v1/audio/transcriptions"

// transcriptionsURL builds the full endpoint URL from BaseURL.
func (p *VLLMProvider) transcriptionsURL() string {
	return buildSTTEndpointURL(p.BaseURL, transcriptionEndpointPath)
}

// buildSTTEndpointURL appends defaultPath to baseURL, avoiding duplication
// when baseURL already contains the "/v1" prefix.
func buildSTTEndpointURL(baseURL, defaultPath string) string {
	u := strings.TrimRight(baseURL, "/")
	segments := strings.Split(strings.TrimLeft(defaultPath, "/"), "/")
	if strings.HasSuffix(u, "/"+segments[0]) {
		return u + "/" + strings.Join(segments[1:], "/")
	}
	return u + defaultPath
}

// Transcribe recognizes speech from audioReader and returns the text.
func (p *VLLMProvider) Transcribe(ctx context.Context, audioReader io.Reader, language string) (string, error) {
	lang := p.Language
	if language != "" {
		lang = language
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "recording.webm")
	if err != nil {
		return "", fmt.Errorf("stt: create form file: %w", err)
	}
	if _, err = io.Copy(part, audioReader); err != nil {
		return "", fmt.Errorf("stt: write audio: %w", err)
	}
	if err = writer.WriteField("model", p.Model); err != nil {
		return "", fmt.Errorf("stt: write model field: %w", err)
	}
	if lang != "" {
		_ = writer.WriteField("language", lang)
	}
	if err = writer.Close(); err != nil {
		return "", fmt.Errorf("stt: close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transcriptionsURL(), &body)
	if err != nil {
		return "", fmt.Errorf("stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("stt: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var transResp openaiTranscribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&transResp); err != nil {
		return "", fmt.Errorf("stt: decode response: %w", err)
	}

	return transResp.Text, nil
}
