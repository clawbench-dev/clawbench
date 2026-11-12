package speech

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Alibaba Cloud Model Studio (百炼 / DashScope) speech synthesis.
//
// The endpoint is the non-real-time synthesis API introduced for the
// qwen-audio / cosyvoice model families:
//
//	POST {base}/api/v1/services/audio/tts/SpeechSynthesizer
//
// It answers with a URL to the rendered audio (24h validity) or, when the
// server chose to inline it, base64 audio in output.audio.data. Both shapes
// are handled here; the format is requested explicitly as mp3 because the rest
// of the pipeline (cache extension, browser playback, device PCM decoding)
// assumes MP3.
//
// Note that qwen3-tts-flash lives on a different path
// (/services/aigc/multimodal-generation/generation) and only produces WAV —
// that model is deliberately not supported, since WAV would be cached under an
// .mp3 name and fail to play.
const (
	// AlibabaTTSBaseURL is the Beijing public endpoint. Singapore uses a
	// different host and a different API key; workspace-scoped accounts use
	// their own host — hence the configurable base.
	AlibabaTTSBaseURL = "https://dashscope.aliyuncs.com"

	alibabaTTSPath = "/api/v1/services/audio/tts/SpeechSynthesizer"

	// alibabaTTSSegmentRunes bounds one request. The API rejects oversized
	// input, and a long reply read in full is several thousand runes; splitting
	// keeps each request small and lets the audio of one segment play while the
	// next is being rendered.
	alibabaTTSSegmentRunes = 500

	// alibabaTTSTimeout bounds one synthesis request.
	alibabaTTSTimeout = 120 * time.Second

	// alibabaTTSDownloadTimeout bounds fetching the rendered audio from the
	// returned OSS URL.
	alibabaTTSDownloadTimeout = 60 * time.Second

	// alibabaTTSMaxAudioBytes caps a downloaded audio file.
	alibabaTTSMaxAudioBytes = 64 << 20
)

// AlibabaTTSProvider implements SpeechProvider against Alibaba Cloud Model
// Studio's non-real-time speech synthesis API.
type AlibabaTTSProvider struct {
	APIKey  string
	BaseURL string // default AlibabaTTSBaseURL; workspace / Singapore hosts go here
	Model   string // e.g. "cosyvoice-v3-flash"
	Voice   string // system / cloned / designed voice name, e.g. "longanyang"
	// Speed is the shared speech-rate multiplier. The API accepts 0.5–2.0;
	// values outside that range are clamped rather than rejected, so a global
	// preference set for another engine still produces audio.
	Speed      float64
	HTTPClient *http.Client
}

// Compile-time assertions.
var (
	_ SpeechProvider        = (*AlibabaTTSProvider)(nil)
	_ CacheIdentityProvider = (*AlibabaTTSProvider)(nil)
)

// NewAlibabaTTSProvider creates a provider with the documented defaults.
func NewAlibabaTTSProvider() *AlibabaTTSProvider {
	return &AlibabaTTSProvider{
		BaseURL: AlibabaTTSBaseURL,
		Model:   "cosyvoice-v3-flash",
		Voice:   "longanyang",
		Speed:   1,
	}
}

// CacheIdentity covers everything that changes the audio. The API key is
// excluded — it does not affect the result and this string is hashed into an
// on-disk file name.
func (p *AlibabaTTSProvider) CacheIdentity() string {
	return fmt.Sprintf("alibaba|%s|%s|%s|%g", p.BaseURL, p.Model, p.Voice, p.Speed)
}

func (p *AlibabaTTSProvider) validate() error {
	if strings.TrimSpace(p.APIKey) == "" {
		return fmt.Errorf("alibaba-tts: tts.alibaba.api_key is not configured")
	}
	if p.Model == "" {
		return fmt.Errorf("alibaba-tts: tts.alibaba.model is not configured")
	}
	if p.Voice == "" {
		return fmt.Errorf("alibaba-tts: tts.alibaba.voice is not configured")
	}
	return nil
}

// synthesisURL completes the configured base into the full endpoint. A base may
// be a bare host, end in /api/v1, or already be the full endpoint.
func (p *AlibabaTTSProvider) synthesisURL() string {
	return ResolveAlibabaTTSSynthesisURL(p.BaseURL)
}

// ResolveAlibabaTTSSynthesisURL completes a user-configured base URL into the
// speech synthesis endpoint. Exported so the connectivity probe resolves it
// identically.
func ResolveAlibabaTTSSynthesisURL(base string) string {
	u := strings.TrimRight(strings.TrimSpace(base), "/")
	if u == "" {
		u = AlibabaTTSBaseURL
	}
	switch {
	case strings.HasSuffix(u, alibabaTTSPath):
		return u
	case strings.HasSuffix(u, "/api/v1"):
		return u + "/services/audio/tts/SpeechSynthesizer"
	default:
		return u + alibabaTTSPath
	}
}

// alibabaTTSRequest is the request body. Only the fields we set are modeled;
// instruction / hot_fix / SSML are deliberately left out — they are
// model-specific and would fail on models that do not accept them.
type alibabaTTSRequest struct {
	Model string `json:"model"`
	Input struct {
		Text   string  `json:"text"`
		Voice  string  `json:"voice"`
		Format string  `json:"format"`
		Rate   float64 `json:"rate,omitempty"`
	} `json:"input"`
}

// alibabaTTSResponse is the answer. DashScope reports failures either through
// the HTTP status or through a non-empty code on a 200 response, so both are
// checked.
type alibabaTTSResponse struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"request_id"`
	Output     struct {
		Audio struct {
			Data string `json:"data"`
			URL  string `json:"url"`
		} `json:"audio"`
	} `json:"output"`
}

// Synthesize renders text into outputPath. It is not a streaming provider:
// the API returns either a finished file URL or the whole base64 payload, so
// there is nothing to forward while it runs.
func (p *AlibabaTTSProvider) Synthesize(ctx context.Context, text string, outputPath string, _ string) error {
	if err := p.validate(); err != nil {
		return err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("alibaba-tts: failed to create output directory %s: %w", dir, err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("alibaba-tts: failed to create output file: %w", err)
	}

	sink := newAudioSink(f, "alibaba-tts")
	synthErr := p.synthesizeText(ctx, text, sink)
	if synthErr == nil {
		synthErr = sink.finish()
	}
	if closeErr := f.Close(); synthErr == nil && closeErr != nil {
		synthErr = fmt.Errorf("alibaba-tts: failed to close output file: %w", closeErr)
	}
	if synthErr != nil {
		_ = os.Remove(outputPath)
		return synthErr
	}
	if sink.total == 0 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("alibaba-tts: server returned no audio data")
	}

	slog.Info(
		"alibaba-tts synthesize completed",
		slog.String("output", outputPath),
		slog.String("model", p.Model),
		slog.String("voice", p.Voice),
		slog.Int64("audio_bytes", sink.total),
	)
	return nil
}

// synthesizeText renders every segment into w, in order. A failing segment
// aborts the whole job: silently skipping it would drop words from the audio.
func (p *AlibabaTTSProvider) synthesizeText(ctx context.Context, text string, w io.Writer) error {
	for _, seg := range splitSpeechSegments(text, alibabaTTSSegmentRunes) {
		if err := p.synthesizeSegment(ctx, seg, w); err != nil {
			return err
		}
	}
	return nil
}

func (p *AlibabaTTSProvider) synthesizeSegment(ctx context.Context, text string, w io.Writer) error {
	var body alibabaTTSRequest
	body.Model = p.Model
	body.Input.Text = text
	body.Input.Voice = p.Voice
	// Explicit mp3: the pipeline assumes it, and relying on a server-side
	// default would silently change format if that default ever moves.
	body.Input.Format = mp3Format
	if p.Speed > 0 && p.Speed != 1 {
		body.Input.Rate = clampFloat(p.Speed, 0.5, 2.0)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("alibaba-tts: failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.synthesisURL(), strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("alibaba-tts: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: alibabaTTSTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("alibaba-tts: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("alibaba-tts: failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("alibaba-tts: API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return p.writeAudioFromResponse(ctx, resp.StatusCode, respBody, w)
}

// writeAudioFromResponse decodes one successful response body and writes its
// audio to w. It is split out of synthesizeSegment to keep the request/response
// plumbing and the payload shapes readable on their own.
func (p *AlibabaTTSProvider) writeAudioFromResponse(ctx context.Context, status int, respBody []byte, w io.Writer) error {
	var parsed alibabaTTSResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("alibaba-tts: malformed response (HTTP %d): %s", status, truncateForError(string(respBody)))
	}
	if parsed.Code != "" || parsed.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("alibaba-tts: API error %s (status %d, request %s): %s",
			parsed.Code, parsed.StatusCode, parsed.RequestID, parsed.Message)
	}

	// Inline base64 is preferred: it avoids a second network hop and cannot
	// expire. The URL form is what non-streaming requests normally return.
	if parsed.Output.Audio.Data != "" {
		audio, decodeErr := base64.StdEncoding.DecodeString(parsed.Output.Audio.Data)
		if decodeErr != nil {
			return fmt.Errorf("alibaba-tts: failed to decode inline audio: %w", decodeErr)
		}
		if _, err := w.Write(audio); err != nil {
			return fmt.Errorf("alibaba-tts: failed to write audio: %w", err)
		}
		return nil
	}
	if parsed.Output.Audio.URL == "" {
		return fmt.Errorf("alibaba-tts: response contains neither audio.data nor audio.url (request %s): %s",
			parsed.RequestID, truncateForError(string(respBody)))
	}
	return p.downloadAudio(ctx, parsed.Output.Audio.URL, w)
}

// downloadAudio fetches the rendered file. The URL is a pre-signed OSS link, so
// it carries its own credentials and must not be sent the API key.
func (p *AlibabaTTSProvider) downloadAudio(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("alibaba-tts: failed to create download request: %w", err)
	}
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: alibabaTTSDownloadTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("alibaba-tts: failed to download audio: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("alibaba-tts: audio download returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	n, err := io.Copy(w, io.LimitReader(resp.Body, alibabaTTSMaxAudioBytes))
	if err != nil {
		return fmt.Errorf("alibaba-tts: failed to read audio body: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("alibaba-tts: downloaded audio is empty")
	}
	return nil
}

// clampFloat keeps v inside [lo, hi].
func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
