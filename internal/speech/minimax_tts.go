package speech

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MiniMax speech synthesis (T2A v2).
//
//	POST {base}/v1/t2a_v2[?GroupId=<group>]
//
// The response carries the whole clip as a HEX string in data.audio (MiniMax's
// `output_format: "hex"` shape — not base64), so the provider decodes hex and
// writes it out. There is no streaming path here: the clip is requested as a
// whole, which keeps one failure mode instead of an SSE parser.
//
// Older accounts (notably on the China site) require the GroupId query
// parameter; newer ones accept the API key alone, so it is optional.
const (
	// MinimaxTTSBaseURL is the China site. The international site uses a
	// different host (and a different key), hence the configurable base.
	MinimaxTTSBaseURL = "https://api.minimax.chat"

	minimaxTTSPath = "/v1/t2a_v2"

	// minimaxTTSSegmentRunes bounds one request. The API accepts ~10k
	// characters, but a long read is better rendered in bounded requests: a
	// single huge request takes long enough that one hiccup loses the whole
	// clip.
	minimaxTTSSegmentRunes = 2000

	// minimaxTTSTimeout bounds one synthesis request.
	minimaxTTSTimeout = 120 * time.Second
)

// MinimaxTTSProvider implements SpeechProvider against MiniMax T2A v2.
type MinimaxTTSProvider struct {
	APIKey  string
	GroupID string // optional; older accounts require it as a query parameter
	BaseURL string // default MinimaxTTSBaseURL; international hosts go here
	Model   string // e.g. "speech-02-hd"
	Voice   string // voice id, e.g. "female-shaonv"
	// Speed is the shared speech-rate multiplier. The API accepts 0.5–2.0;
	// values outside that range are clamped.
	Speed      float64
	HTTPClient *http.Client
}

var (
	_ SpeechProvider        = (*MinimaxTTSProvider)(nil)
	_ CacheIdentityProvider = (*MinimaxTTSProvider)(nil)
)

// NewMinimaxTTSProvider creates a provider with the documented defaults.
func NewMinimaxTTSProvider() *MinimaxTTSProvider {
	return &MinimaxTTSProvider{
		BaseURL: MinimaxTTSBaseURL,
		Model:   "speech-02-hd",
		Voice:   "female-shaonv",
		Speed:   1,
	}
}

// CacheIdentity covers everything that changes the audio. The API key and group
// id are excluded — they identify the account, not the voice, and this string is
// hashed into a file name.
func (p *MinimaxTTSProvider) CacheIdentity() string {
	return fmt.Sprintf("minimax|%s|%s|%g", p.Model, p.Voice, p.Speed)
}

func (p *MinimaxTTSProvider) validate() error {
	if strings.TrimSpace(p.APIKey) == "" {
		return fmt.Errorf("minimax-tts: tts.minimax.api_key is not configured")
	}
	if p.Model == "" {
		return fmt.Errorf("minimax-tts: tts.minimax.model is not configured")
	}
	if p.Voice == "" {
		return fmt.Errorf("minimax-tts: tts.minimax.voice is not configured")
	}
	return nil
}

// synthesisURL builds the endpoint, appending GroupId when configured.
func (p *MinimaxTTSProvider) synthesisURL() string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = MinimaxTTSBaseURL
	}
	// Parse the base on its own, then attach the path — concatenating first
	// would put the path INSIDE an existing query string
	// ("…?foo=bar" + "/v1/t2a_v2" becomes "…?foo=bar/v1/t2a_v2").
	u, err := url.Parse(base)
	if err != nil {
		return base + minimaxTTSPath
	}
	// Accept a base that already carries the full endpoint.
	if !strings.HasSuffix(u.Path, minimaxTTSPath) {
		u.Path = strings.TrimRight(u.Path, "/") + minimaxTTSPath
	}
	if p.GroupID != "" {
		q := u.Query()
		q.Set("GroupId", p.GroupID)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// minimaxTTSRequest is the T2A v2 body. Optional fields (emotion, pronunciation
// dictionary, subtitles) are deliberately omitted: they are model-specific and
// would fail on models that do not accept them.
type minimaxTTSRequest struct {
	Model        string `json:"model"`
	Text         string `json:"text"`
	Stream       bool   `json:"stream"`
	VoiceSetting struct {
		VoiceID string  `json:"voice_id"`
		Speed   float64 `json:"speed"`
		Vol     float64 `json:"vol"`
		Pitch   int     `json:"pitch"`
	} `json:"voice_setting"`
	AudioSetting struct {
		SampleRate int    `json:"sample_rate"`
		Bitrate    int    `json:"bitrate"`
		Format     string `json:"format"`
		Channel    int    `json:"channel"`
	} `json:"audio_setting"`
	OutputFormat string `json:"output_format"`
}

// minimaxTTSResponse is the non-streaming answer. Errors are reported in
// base_resp, not through the HTTP status.
type minimaxTTSResponse struct {
	Data struct {
		Audio  string `json:"audio"`
		Status int    `json:"status"`
	} `json:"data"`
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	TraceID string `json:"trace_id"`
}

// Synthesize renders text into outputPath.
func (p *MinimaxTTSProvider) Synthesize(ctx context.Context, text string, outputPath string, _ string) error {
	if err := p.validate(); err != nil {
		return err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("minimax-tts: failed to create output directory %s: %w", dir, err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("minimax-tts: failed to create output file: %w", err)
	}

	sink := newAudioSink(f, "minimax-tts")
	synthErr := p.synthesizeText(ctx, text, sink)
	if synthErr == nil {
		synthErr = sink.finish()
	}
	if closeErr := f.Close(); synthErr == nil && closeErr != nil {
		synthErr = fmt.Errorf("minimax-tts: failed to close output file: %w", closeErr)
	}
	if synthErr != nil {
		_ = os.Remove(outputPath)
		return synthErr
	}
	if sink.total == 0 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("minimax-tts: server returned no audio data")
	}

	slog.Info(
		"minimax-tts synthesize completed",
		slog.String("output", outputPath),
		slog.String("model", p.Model),
		slog.String("voice", p.Voice),
		slog.Int64("audio_bytes", sink.total),
	)
	return nil
}

func (p *MinimaxTTSProvider) synthesizeText(ctx context.Context, text string, w io.Writer) error {
	for _, seg := range splitSpeechSegments(text, minimaxTTSSegmentRunes) {
		if err := p.synthesizeSegment(ctx, seg, w); err != nil {
			return err
		}
	}
	return nil
}

func (p *MinimaxTTSProvider) synthesizeSegment(ctx context.Context, text string, w io.Writer) error {
	var body minimaxTTSRequest
	body.Model = p.Model
	body.Text = text
	body.Stream = false
	body.VoiceSetting.VoiceID = p.Voice
	body.VoiceSetting.Vol = 1
	body.VoiceSetting.Pitch = 0
	body.VoiceSetting.Speed = 1
	if p.Speed > 0 {
		body.VoiceSetting.Speed = clampFloat(p.Speed, 0.5, 2.0)
	}
	body.AudioSetting.SampleRate = 32000
	body.AudioSetting.Bitrate = 128000
	body.AudioSetting.Format = mp3Format
	body.AudioSetting.Channel = 1
	// Explicit hex: the decoder below only understands hex, and relying on a
	// server-side default would turn a format change into a decode error.
	body.OutputFormat = "hex"

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("minimax-tts: failed to encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.synthesisURL(), strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("minimax-tts: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: minimaxTTSTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("minimax-tts: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if err != nil {
		return fmt.Errorf("minimax-tts: failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("minimax-tts: API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed minimaxTTSResponse
	if unmarshalErr := json.Unmarshal(respBody, &parsed); unmarshalErr != nil {
		return fmt.Errorf("minimax-tts: malformed response (HTTP %d): %s", resp.StatusCode, truncateForError(string(respBody)))
	}
	if parsed.BaseResp.StatusCode != 0 {
		return fmt.Errorf("minimax-tts: API error %d (trace %s): %s",
			parsed.BaseResp.StatusCode, parsed.TraceID, parsed.BaseResp.StatusMsg)
	}
	if parsed.Data.Audio == "" {
		return fmt.Errorf("minimax-tts: response carries no audio (trace %s)", parsed.TraceID)
	}

	// MiniMax returns the audio as a hex string, not base64. A base64 payload
	// fails here with a decode error naming the problem instead of being
	// written out as garbage.
	audio, err := hex.DecodeString(parsed.Data.Audio)
	if err != nil {
		return fmt.Errorf("minimax-tts: failed to decode hex audio (trace %s): %w", parsed.TraceID, err)
	}
	if _, err := w.Write(audio); err != nil {
		return fmt.Errorf("minimax-tts: failed to write audio: %w", err)
	}
	return nil
}
