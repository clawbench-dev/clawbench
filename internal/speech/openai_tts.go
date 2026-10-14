package speech

import (
	"bufio"
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

// openAISpeechPath is the OpenAI-compatible speech synthesis path.
const openAISpeechPath = "/v1/audio/speech"

// maxSSELineBytes bounds a single SSE `data:` line. Audio chunks arrive
// base64-encoded, so one line is roughly 4/3 of a chunk — a few KB in
// practice. The generous ceiling only guards against a malformed or hostile
// response growing an unbounded buffer inside bufio.Scanner.
const maxSSELineBytes = 1 << 20

// openAITTSTimeout is the overall request timeout. It sits above the handler's
// own synthesize budget so the handler's context is normally the thing that
// fires; this is a backstop for a server that accepts the connection and then
// stalls without ever closing it.
const openAITTSTimeout = 150 * time.Second

// OpenAITTSProvider implements SpeechProvider and StreamingSpeechProvider
// against an OpenAI-compatible POST /v1/audio/speech endpoint.
//
// Two streaming shapes exist on that endpoint and this provider handles both:
//
//   - `stream_format: "sse"` — Server-Sent Events, one `speech.audio.delta`
//     event per chunk carrying base64 audio, terminated by `speech.audio.done`.
//     Only newer models support it; `tts-1` / `tts-1-hd` reject it.
//   - `stream_format: "audio"` — the raw audio body, chunked by HTTP. This is
//     the endpoint default and works on every model.
//
// Because "sse" can be rejected outright, the provider starts with it and
// transparently retries with "audio" on a 400. A compatible server that merely
// ignores the parameter is detected from the response Content-Type instead of
// the status code, so no retry is needed in that case.
//
// The spec names `tts-1` / `tts-1-hd` as NOT supporting `stream_format: "sse"`,
// so those models skip the SSE attempt entirely (see modelSupportsSSE). Asking
// anyway is a spec violation, and a server that answers 200 + SSE instead of
// 400 would strand the client on a stream it should never have requested.
type OpenAITTSProvider struct {
	// BaseURL is the OpenAI-compatible API root, e.g.
	// "https://api.openai.com/v1" or "https://host/v1/audio/speech".
	BaseURL string
	// APIKey is the bearer token. May be empty for a local server.
	APIKey string
	// Model is the TTS model name (e.g. "gpt-4o-mini-tts"). User-supplied —
	// there is deliberately no default, since a wrong guess fails at request
	// time with a confusing upstream error.
	Model string
	// Voice is the voice name ("alloy", …) or a custom voice id. Free text:
	// the built-in set is not exhaustive across compatible servers.
	Voice string
	// Speed is the speech rate multiplier (OpenAI accepts 0.25–4.0). <= 0
	// omits the parameter and lets the server default it.
	Speed float64
	// HTTPClient is injectable for tests. When nil a client with
	// openAITTSTimeout is used.
	HTTPClient *http.Client
}

// Compile-time assertions: OpenAI TTS streams, is usable as a plain provider
// for callers that only need a file, and reports its cache identity.
var (
	_ SpeechProvider          = (*OpenAITTSProvider)(nil)
	_ StreamingSpeechProvider = (*OpenAITTSProvider)(nil)
	_ CacheIdentityProvider   = (*OpenAITTSProvider)(nil)
)

// CacheIdentity reports the endpoint, model, voice and rate. BaseURL is
// included because two different compatible servers serving the same model
// name can produce different audio. APIKey is deliberately excluded: it does
// not affect the audio, and this string is hashed into an on-disk file name.
func (p *OpenAITTSProvider) CacheIdentity() string {
	return fmt.Sprintf("openai|%s|%s|%s|%g", p.BaseURL, p.Model, p.Voice, p.Speed)
}

// NewOpenAITTSProvider creates a provider with no defaults — every field is
// user configuration, so an unconfigured provider fails loudly in validate
// rather than silently synthesizing with a guessed model.
func NewOpenAITTSProvider() *OpenAITTSProvider {
	return &OpenAITTSProvider{}
}

// validate reports the first missing required setting. The messages name the
// config path so a user can find the field in the settings panel.
func (p *OpenAITTSProvider) validate() error {
	if p.BaseURL == "" {
		return fmt.Errorf("openai-tts: tts.openai.base_url is not configured")
	}
	if p.Model == "" {
		return fmt.Errorf("openai-tts: tts.openai.model is not configured")
	}
	if p.Voice == "" {
		return fmt.Errorf("openai-tts: tts.openai.voice is not configured")
	}
	return nil
}

// speechURL completes BaseURL into the full /v1/audio/speech endpoint.
func (p *OpenAITTSProvider) speechURL() string {
	return ResolveOpenAISpeechURL(p.BaseURL)
}

// ResolveOpenAISpeechURL completes a user-configured base URL into the full
// /v1/audio/speech endpoint. It accepts a bare host, a base ending in /v1, or
// the full endpoint — the same shapes the other OpenAI-compatible settings
// accept. Exported so the connectivity probe resolves the URL identically.
func ResolveOpenAISpeechURL(baseURL string) string {
	u := strings.TrimRight(baseURL, "/")
	switch {
	case strings.HasSuffix(u, openAISpeechPath):
		return u
	case strings.HasSuffix(u, "/v1"):
		return u + "/audio/speech"
	default:
		return u + openAISpeechPath
	}
}

// speechRequestBody is the JSON body for POST /v1/audio/speech.
type speechRequestBody struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format"`
	Speed          float64 `json:"speed,omitempty"`
	StreamFormat   string  `json:"stream_format"`
}

// Synthesize generates audio to outputPath without streaming. It shares the
// implementation with SynthesizeStream so the two cannot diverge.
func (p *OpenAITTSProvider) Synthesize(ctx context.Context, text string, outputPath string, language string) error {
	return p.SynthesizeStream(ctx, text, outputPath, language, nil)
}

// SynthesizeStream generates audio to outputPath while forwarding each audio
// chunk to chunkCh. The caller is responsible for closing chunkCh after this
// method returns.
//
// language is ignored: the API infers the spoken language from the input text
// and has no language parameter.
func (p *OpenAITTSProvider) SynthesizeStream(ctx context.Context, text string, outputPath string, _ string, chunkCh chan<- []byte) error {
	if err := p.validate(); err != nil {
		return err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("openai-tts: failed to create output directory %s: %w", dir, err)
	}

	slog.Info(
		"openai-tts stream synthesize",
		slog.String("output", outputPath),
		slog.String("model", p.Model),
		slog.String("voice", p.Voice),
		slog.Int("text_len", len([]rune(text))),
	)

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("openai-tts: failed to create output file: %w", err)
	}

	var writer io.Writer = f
	if chunkCh != nil {
		writer = io.MultiWriter(f, &chunkWriter{ch: chunkCh})
	}

	written, err := p.synthesize(ctx, text, writer)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(outputPath)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("openai-tts: failed to close output file: %w", err)
	}
	if written == 0 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("openai-tts: server returned no audio data")
	}

	slog.Info(
		"openai-tts stream synthesize completed",
		slog.String("output", outputPath),
		slog.Int64("audio_bytes", written),
	)
	return nil
}

// synthesize performs the request, preferring SSE and falling back to the raw
// audio body. It returns the number of audio bytes written to w.
func (p *OpenAITTSProvider) synthesize(ctx context.Context, text string, w io.Writer) (int64, error) {
	// Only ask for SSE when the model is documented to support it. Requesting
	// it for tts-1 / tts-1-hd is a spec violation, and a server that ignores
	// the spec and returns 200 + SSE anyway would defeat the 400 fallback
	// below, leaving the client parsing a stream it never should have asked
	// for.
	streamFormat := "audio"
	if modelSupportsSSE(p.Model) {
		streamFormat = "sse"
	}

	resp, err := p.doRequest(ctx, text, streamFormat)
	if err != nil {
		return 0, err
	}

	// A 400 on the SSE attempt means the model does not support
	// stream_format=sse. Retry with the raw audio body, which every model
	// supports. The body is fully drained first so the connection can be
	// reused. This only applies when we actually asked for SSE — a 400 on an
	// `audio` request is a genuine error and must surface as one.
	if streamFormat == "sse" && resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		slog.Warn(
			"openai-tts: sse stream_format rejected, retrying with audio",
			slog.Int("status", resp.StatusCode),
			slog.String("body", strings.TrimSpace(string(body))),
		)
		resp, err = p.doRequest(ctx, text, "audio")
		if err != nil {
			return 0, err
		}
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("openai-tts: API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// A compatible server may ignore stream_format and answer with audio
	// directly; the Content-Type is the authoritative signal for which shape
	// arrived (checking the request parameter would misread that server).
	if isEventStream(resp.Header.Get("Content-Type")) {
		return streamSSEAudio(resp.Body, w)
	}

	n, err := io.Copy(w, resp.Body)
	if err != nil {
		return n, fmt.Errorf("openai-tts: failed to read audio body: %w", err)
	}
	return n, nil
}

// doRequest issues one POST /v1/audio/speech and returns the live response.
// The caller owns resp.Body.
func (p *OpenAITTSProvider) doRequest(ctx context.Context, text, streamFormat string) (*http.Response, error) {
	body := speechRequestBody{
		Model:          p.Model,
		Input:          text,
		Voice:          p.Voice,
		ResponseFormat: "mp3", // mp3 keeps the existing MSE playback path (audio/mpeg)
		StreamFormat:   streamFormat,
	}
	if p.Speed > 0 {
		body.Speed = p.Speed
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai-tts: failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.speechURL(), strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("openai-tts: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if streamFormat == "sse" {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/octet-stream")
	}
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: openAITTSTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai-tts: request failed: %w", err)
	}
	return resp, nil
}

// isEventStream reports whether a Content-Type denotes SSE.
func isEventStream(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

// modelSupportsSSE reports whether a TTS model accepts stream_format="sse".
//
// The OpenAI spec states that `tts-1` and `tts-1-hd` (and their dated
// snapshots) do NOT support SSE streaming. Every other model — including
// `gpt-4o-mini-tts` and compatible-server models unknown to us — is assumed to
// support it, so the capability is denied by name rather than allow-listed.
func modelSupportsSSE(model string) bool {
	switch model {
	case "tts-1", "tts-1-hd", "tts-1-1106", "tts-1-hd-1106":
		return false
	default:
		return true
	}
}

// sseAudioEvent is the subset of a `speech.audio.delta` event we consume.
// The `type` field is read but not required — a compatible server may omit it.
//
// `audio` is a base64 STRING per the spec. A non-string value (notably the
// nested object `{"audio":{"data":"…"}}` some servers emit) fails to unmarshal
// and is treated as fatal — see streamSSEAudio.
type sseAudioEvent struct {
	Type  string `json:"type"`
	Audio string `json:"audio"`
}

// streamSSEAudio decodes `speech.audio.delta` events from an SSE body and
// writes each chunk to w, returning the total bytes written.
//
// Events that legitimately carry no audio (`speech.audio.done`, an audio-less
// delta, the `[DONE]` sentinel) and non-`data:` lines are skipped. But a
// `data:` line that fails to decode is FATAL, not skipped: the spec types
// `audio` as a base64 string, so a payload we cannot parse is a non-compliant
// server, and swallowing it would turn a clear format error into a 0-byte file
// plus a misleading "no audio data" message. Failing here names the offending
// payload so the provider bug is visible in seconds.
func streamSSEAudio(r io.Reader, w io.Writer) (int64, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)

	var total int64
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var ev sseAudioEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return total, fmt.Errorf("openai-tts: malformed speech.audio.delta payload %q: %w", truncateForError(payload), err)
		}

		chunk, err := base64.StdEncoding.DecodeString(ev.Audio)
		if err != nil {
			return total, fmt.Errorf("openai-tts: failed to decode audio chunk: %w", err)
		}
		if len(chunk) == 0 {
			continue
		}
		n, err := w.Write(chunk)
		total += int64(n)
		if err != nil {
			return total, fmt.Errorf("openai-tts: failed to write audio chunk: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return total, fmt.Errorf("openai-tts: failed to read event stream: %w", err)
	}
	return total, nil
}

// truncateForError bounds a payload echoed into an error message. A malformed
// line can be large (base64 audio), and the message is logged and surfaced.
func truncateForError(s string) string {
	const maxLen = 256
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
