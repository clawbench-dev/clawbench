package speech

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Volcano Engine (火山引擎 / 豆包) large-model speech synthesis, v3
// unidirectional HTTP API:
//
//	POST {base}/api/v3/tts/unidirectional
//
// The response is a chunked stream of JSON objects, each carrying a base64
// audio fragment and code 0; the stream ends when the HTTP body ends. That
// makes this provider a StreamingSpeechProvider: audio reaches the player while
// the rest of the reply is still being rendered.
//
// Auth is a single API key from the speech console (the same header the Volcano
// STT engine uses), plus the resource id that matches the voice type: official
// 2.0 voices use seed-tts-2.0, cloned voices use seed-icl-2.0.
const (
	// VolcanoTTSBaseURL is the fixed speech API root. Volcano serves speech
	// only from this host, so exposing a base URL would invite values that
	// cannot work; it stays a field for tests.
	VolcanoTTSBaseURL = "https://openspeech.bytedance.com"

	volcanoTTSPath = "/api/v3/tts/unidirectional"

	// volcanoTTSSegmentRunes bounds one request. The API rejects oversized
	// text; splitting also keeps the time-to-first-audio short.
	volcanoTTSSegmentRunes = 500

	// volcanoTTSTimeout bounds one synthesis request.
	volcanoTTSTimeout = 120 * time.Second

	// volcanoTTSCodeOK is the only success code on this endpoint: every audio
	// fragment carries it, and the end of the stream is the end of the body.
	volcanoTTSCodeOK = 0
)

// VolcanoTTSProvider implements SpeechProvider and StreamingSpeechProvider.
type VolcanoTTSProvider struct {
	APIKey     string
	ResourceID string // seed-tts-2.0 (official voices) / seed-icl-2.0 (cloned)
	Voice      string // speaker id
	// Speed is the shared speech-rate multiplier, converted to the API's
	// -50…100 scale (0 = normal speed).
	Speed      float64
	BaseURL    string // overridable for tests; defaults to VolcanoTTSBaseURL
	HTTPClient *http.Client
}

var (
	_ SpeechProvider          = (*VolcanoTTSProvider)(nil)
	_ StreamingSpeechProvider = (*VolcanoTTSProvider)(nil)
	_ CacheIdentityProvider   = (*VolcanoTTSProvider)(nil)
)

// NewVolcanoTTSProvider creates a provider with the documented defaults.
func NewVolcanoTTSProvider() *VolcanoTTSProvider {
	return &VolcanoTTSProvider{
		ResourceID: "seed-tts-2.0",
		Voice:      "zh_female_vv_uranus_bigtts",
		Speed:      1,
		BaseURL:    VolcanoTTSBaseURL,
	}
}

// CacheIdentity covers everything that changes the audio. The API key is
// excluded — it identifies the account, not the voice, and this string is
// hashed into a file name.
func (p *VolcanoTTSProvider) CacheIdentity() string {
	return fmt.Sprintf("volcano|%s|%s|%g", p.ResourceID, p.Voice, p.Speed)
}

func (p *VolcanoTTSProvider) validate() error {
	if strings.TrimSpace(p.APIKey) == "" {
		return fmt.Errorf("volcano-tts: tts.volcano.api_key is not configured")
	}
	if p.Voice == "" {
		return fmt.Errorf("volcano-tts: tts.volcano.voice is not configured")
	}
	return nil
}

// volcanoTTSRequest is the v3 request body.
type volcanoTTSRequest struct {
	ReqParams struct {
		Text        string `json:"text"`
		Speaker     string `json:"speaker"`
		AudioParams struct {
			Format     string `json:"format"`
			SampleRate int    `json:"sample_rate"`
			SpeechRate int    `json:"speech_rate,omitempty"`
		} `json:"audio_params"`
	} `json:"req_params"`
}

// volcanoTTSChunk is one line of the response stream.
type volcanoTTSChunk struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

// Synthesize renders text into outputPath without forwarding chunks.
func (p *VolcanoTTSProvider) Synthesize(ctx context.Context, text string, outputPath string, language string) error {
	return p.SynthesizeStream(ctx, text, outputPath, language, nil)
}

// SynthesizeStream renders text into outputPath while forwarding each audio
// fragment to chunkCh. The caller closes chunkCh after this returns.
//
// language is ignored: the speaker fixes the language and the model follows the
// text.
func (p *VolcanoTTSProvider) SynthesizeStream(ctx context.Context, text string, outputPath string, _ string, chunkCh chan<- []byte) error {
	if err := p.validate(); err != nil {
		return err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("volcano-tts: failed to create output directory %s: %w", dir, err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("volcano-tts: failed to create output file: %w", err)
	}

	// The chunk writer sits behind the sink so only bytes that passed the MP3
	// check are forwarded to the player.
	var dst io.Writer = f
	if chunkCh != nil {
		dst = io.MultiWriter(f, &chunkWriter{ch: chunkCh})
	}
	sink := newAudioSink(dst, "volcano-tts")

	synthErr := p.synthesizeText(ctx, text, sink)
	if synthErr == nil {
		synthErr = sink.finish()
	}
	if closeErr := f.Close(); synthErr == nil && closeErr != nil {
		synthErr = fmt.Errorf("volcano-tts: failed to close output file: %w", closeErr)
	}
	if synthErr != nil {
		_ = os.Remove(outputPath)
		return synthErr
	}
	if sink.total == 0 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("volcano-tts: server returned no audio data")
	}

	slog.Info(
		"volcano-tts stream synthesize completed",
		slog.String("output", outputPath),
		slog.String("voice", p.Voice),
		slog.Int64("audio_bytes", sink.total),
	)
	return nil
}

func (p *VolcanoTTSProvider) synthesizeText(ctx context.Context, text string, w io.Writer) error {
	for _, seg := range splitSpeechSegments(text, volcanoTTSSegmentRunes) {
		if err := p.synthesizeSegment(ctx, seg, w); err != nil {
			return err
		}
	}
	return nil
}

func (p *VolcanoTTSProvider) synthesizeSegment(ctx context.Context, text string, w io.Writer) error {
	var body volcanoTTSRequest
	body.ReqParams.Text = text
	body.ReqParams.Speaker = p.Voice
	body.ReqParams.AudioParams.Format = mp3Format
	body.ReqParams.AudioParams.SampleRate = 24000
	if p.Speed > 0 && p.Speed != 1 {
		// speech_rate is a percentage offset: 100 = 2x, -50 = 0.5x.
		body.ReqParams.AudioParams.SpeechRate = int(clampFloat((p.Speed-1)*100, -50, 100))
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("volcano-tts: failed to encode request: %w", err)
	}

	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = VolcanoTTSBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+volcanoTTSPath, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("volcano-tts: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", p.APIKey)
	req.Header.Set("X-Api-Resource-Id", p.ResourceID)
	req.Header.Set("X-Api-Request-Id", uuid.NewString())

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: volcanoTTSTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("volcano-tts: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("volcano-tts: API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return p.readChunks(resp.Body, w)
}

// readChunks consumes the chunked response and writes each audio fragment to w.
//
// Every fragment carries code 0; a non-zero code is a failure. The stream has
// no separate terminal event — it ends when the body ends — so a connection
// dropped mid-body surfaces as a read error instead of a silently short file.
//
// Decoding uses a JSON stream decoder rather than splitting on newlines: the
// server writes one JSON object per chunk, but a decoder also copes with two
// objects arriving back-to-back without a separator. A payload that cannot be
// parsed is fatal rather than skipped — it means the endpoint is not speaking
// this API, and skipping it would turn a protocol error into a short audio file.
func (p *VolcanoTTSProvider) readChunks(r io.Reader, w io.Writer) error {
	dec := json.NewDecoder(r)
	for {
		var chunk volcanoTTSChunk
		if err := dec.Decode(&chunk); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("volcano-tts: failed to decode response stream: %w", err)
		}
		if chunk.Code != volcanoTTSCodeOK {
			return fmt.Errorf("volcano-tts: API error %d: %s", chunk.Code, chunk.Message)
		}
		if chunk.Data == "" {
			continue
		}
		audio, err := base64.StdEncoding.DecodeString(chunk.Data)
		if err != nil {
			return fmt.Errorf("volcano-tts: failed to decode audio chunk: %w", err)
		}
		if len(audio) == 0 {
			continue
		}
		if _, err := w.Write(audio); err != nil {
			return fmt.Errorf("volcano-tts: failed to write audio: %w", err)
		}
	}
}
