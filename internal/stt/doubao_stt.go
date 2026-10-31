package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Doubao (豆包 / Volcano Engine speech) constants.
const (
	// DoubaoBaseURL is the speech API root. The endpoint is fixed rather than
	// configurable: Volcano only serves speech from this host, so exposing a
	// base URL would only invite a value that cannot work.
	DoubaoBaseURL = "https://openspeech.bytedance.com"

	// doubaoFlashPath is the synchronous "recorded audio, fast" recognition
	// endpoint. It answers with the full transcript in one response, which is
	// what the STTProvider contract needs.
	doubaoFlashPath = "/api/v3/auc/bigmodel/recognize/flash"

	// DoubaoDefaultResourceID is the duration-billed Seed-ASR resource. It is
	// the one most accounts hold; the concurrency-billed sibling
	// (volc.seedasr.sauc.concurrent) is sold separately and fails with
	// "quota exceeded for types: concurrency" when absent.
	DoubaoDefaultResourceID = "volc.seedasr.sauc.duration"

	// doubaoModelName is the only model the bigmodel endpoints accept.
	doubaoModelName = "bigmodel"

	// doubaoSuccessCode is the in-body status returned on success.
	doubaoSuccessCode = 20000000
)

// DoubaoProvider calls the Doubao (Volcano Engine) Seed-ASR flash endpoint.
//
// Auth differs from OpenAI-compatible servers: the key goes in X-Api-Key (not
// Authorization: Bearer) together with the purchased resource ID. Language is
// auto-detected by the model, so the language argument to Transcribe is
// ignored.
type DoubaoProvider struct {
	APIKey     string
	ResourceID string
	BaseURL    string       // overridable for tests; defaults to DoubaoBaseURL
	HTTPClient *http.Client // injectable for tests
}

// NewDoubaoProvider creates a Doubao STT provider.
func NewDoubaoProvider(apiKey, resourceID string) *DoubaoProvider {
	return &DoubaoProvider{
		APIKey:     apiKey,
		ResourceID: resourceID,
		BaseURL:    DoubaoBaseURL,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// doubaoRequest is the flash-endpoint request body.
type doubaoRequest struct {
	User    doubaoUser    `json:"user"`
	Audio   doubaoAudio   `json:"audio"`
	Request doubaoOptions `json:"request"`
}

type doubaoUser struct {
	UID string `json:"uid"`
}

type doubaoAudio struct {
	Format string `json:"format"`
	Data   string `json:"data"` // base64-encoded audio
}

type doubaoOptions struct {
	ModelName  string `json:"model_name"`
	EnableITN  bool   `json:"enable_itn"`
	EnablePunc bool   `json:"enable_punc"`
}

// doubaoResponse is the flash-endpoint response. Success bodies carry only
// audio_info + result (no header at all); failures add a header with a
// non-success code. Treating a missing header as an error would reject every
// successful transcription.
type doubaoResponse struct {
	Header *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"header"`
	Result struct {
		Text string `json:"text"`
	} `json:"result"`
}

// Transcribe recognizes speech from audioReader and returns the text.
func (p *DoubaoProvider) Transcribe(ctx context.Context, audioReader io.Reader, _ string) (string, error) {
	audio, err := io.ReadAll(audioReader)
	if err != nil {
		return "", fmt.Errorf("stt: read audio: %w", err)
	}
	if len(audio) == 0 {
		return "", fmt.Errorf("stt: empty audio")
	}

	format, err := detectAudioFormat(audio)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(doubaoRequest{
		User:    doubaoUser{UID: "clawbench"},
		Audio:   doubaoAudio{Format: format, Data: base64.StdEncoding.EncodeToString(audio)},
		Request: doubaoOptions{ModelName: doubaoModelName, EnableITN: true, EnablePunc: true},
	})
	if err != nil {
		return "", fmt.Errorf("stt: encode request: %w", err)
	}

	base := p.BaseURL
	if base == "" {
		base = DoubaoBaseURL
	}
	resourceID := p.ResourceID
	if resourceID == "" {
		resourceID = DoubaoDefaultResourceID
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+doubaoFlashPath, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", p.APIKey)
	req.Header.Set("X-Api-Resource-Id", resourceID)
	req.Header.Set("X-Api-Request-Id", uuid.NewString())
	req.Header.Set("X-Api-Sequence", "-1")

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("stt: read response: %w", err)
	}

	var parsed doubaoResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("stt: decode response (HTTP %d): %s", resp.StatusCode, truncate(respBody, 256))
	}
	// A success body has no header; a failure body always has one. code 0 in a
	// present header means "no status" and is treated as success too.
	if parsed.Header != nil && parsed.Header.Code != 0 && parsed.Header.Code != doubaoSuccessCode {
		return "", fmt.Errorf("stt: doubao returned code %d (HTTP %d): %s", parsed.Header.Code, resp.StatusCode, parsed.Header.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("stt: doubao returned HTTP %d: %s", resp.StatusCode, truncate(respBody, 256))
	}

	return parsed.Result.Text, nil
}

// detectAudioFormat identifies the container from its magic bytes. The browser
// recorder always produces WebM/Opus, but accepting the other formats keeps
// callers (live notes, curl probes) working without a format field.
func detectAudioFormat(b []byte) (string, error) {
	switch {
	case len(b) >= 4 && bytes.Equal(b[:4], []byte("RIFF")):
		return "wav", nil
	case len(b) >= 4 && bytes.Equal(b[:4], []byte("OggS")):
		return "ogg", nil
	case len(b) >= 4 && bytes.Equal(b[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return "webm", nil
	case len(b) >= 3 && bytes.Equal(b[:3], []byte("ID3")):
		return "mp3", nil
	case len(b) >= 2 && b[0] == 0xFF && b[1]&0xE0 == 0xE0:
		return "mp3", nil
	}
	return "", fmt.Errorf("stt: unsupported audio format (leading bytes % X)", b[:min(4, len(b))])
}

// truncate shortens s to at most n bytes for error messages.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
