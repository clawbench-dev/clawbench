package speech

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Tencent Cloud speech synthesis (TextToVoice, API version 2019-08-23).
//
// Auth is per-request signing (TC3-HMAC-SHA256) rather than a bearer token, so
// the SecretId and SecretKey never travel on the wire. The response carries
// the whole audio as base64 — there is no streaming variant on this HTTP
// action, so the provider is not a StreamingSpeechProvider.
//
// One request accepts at most 150 Chinese characters (500 Latin letters), which
// is far below a read-aloud reply, so long text is split into segments and the
// resulting MP3 frames are appended into one file.
const (
	// TencentTTSBaseURL is the fixed speech synthesis endpoint.
	TencentTTSBaseURL = "https://tts.tencentcloudapi.com"

	tencentTTSService    = "tts"
	tencentTTSAction     = "TextToVoice"
	tencentTTSVersion    = "2019-08-23"
	tencentTTSContentTyp = "application/json; charset=utf-8"

	// tencentTTSSegmentRunes stays below the 150-Chinese-character cap with
	// margin: a segment is cut at a sentence boundary when one is available, so
	// the effective limit is what the API sees.
	tencentTTSSegmentRunes = 120

	// tencentTTSTimeout bounds one synthesis request.
	tencentTTSTimeout = 90 * time.Second

	// Tencent's fixed request options for this engine.
	tencentTTSModelType  = 1     // default model
	tencentTTSSampleRate = 16000 // supported by every voice type
)

// TencentTTSProvider implements SpeechProvider against Tencent Cloud TTS.
type TencentTTSProvider struct {
	SecretID  string
	SecretKey string
	// VoiceType is the numeric voice catalog id (101001 = 智瑜).
	VoiceType int
	// Speed is the shared speech-rate multiplier, converted to Tencent's
	// -2…6 scale (see tencentSpeed).
	Speed float64
	// BaseURL is overridable for tests; defaults to TencentTTSBaseURL.
	BaseURL    string
	HTTPClient *http.Client
}

var (
	_ SpeechProvider        = (*TencentTTSProvider)(nil)
	_ CacheIdentityProvider = (*TencentTTSProvider)(nil)
)

// NewTencentTTSProvider creates a provider with the documented defaults.
func NewTencentTTSProvider() *TencentTTSProvider {
	return &TencentTTSProvider{
		VoiceType: 101001,
		Speed:     1,
		BaseURL:   TencentTTSBaseURL,
	}
}

// CacheIdentity covers the settings that change the audio. Credentials are
// excluded — they do not affect the result and this string is hashed into a
// file name.
func (p *TencentTTSProvider) CacheIdentity() string {
	return fmt.Sprintf("tencent|%d|%g", p.VoiceType, p.Speed)
}

func (p *TencentTTSProvider) validate() error {
	if strings.TrimSpace(p.SecretID) == "" {
		return fmt.Errorf("tencent-tts: tts.tencent.secret_id is not configured")
	}
	if strings.TrimSpace(p.SecretKey) == "" {
		return fmt.Errorf("tencent-tts: tts.tencent.secret_key is not configured")
	}
	if p.VoiceType <= 0 {
		return fmt.Errorf("tencent-tts: tts.tencent.voice_type is not configured")
	}
	return nil
}

// tencentTTSRequest is the TextToVoice body. Only the fields this provider
// sets are modeled; subtitles and emotion control are left out.
type tencentTTSRequest struct {
	Text            string  `json:"Text"`
	SessionID       string  `json:"SessionId"`
	Volume          float64 `json:"Volume"`
	Speed           float64 `json:"Speed"`
	ModelType       int     `json:"ModelType"`
	VoiceType       int     `json:"VoiceType"`
	PrimaryLanguage int     `json:"PrimaryLanguage"`
	SampleRate      int     `json:"SampleRate"`
	Codec           string  `json:"Codec"`
}

// tencentTTSResponse is the envelope. Tencent reports authentication and
// parameter failures inside a 200 response, so Response.Error is checked
// before Audio.
type tencentTTSResponse struct {
	Response struct {
		Audio     string `json:"Audio"`
		SessionID string `json:"SessionId"`
		RequestID string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"Response"`
}

// Synthesize renders text into outputPath.
func (p *TencentTTSProvider) Synthesize(ctx context.Context, text string, outputPath string, language string) error {
	if err := p.validate(); err != nil {
		return err
	}

	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tencent-tts: failed to create output directory %s: %w", dir, err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("tencent-tts: failed to create output file: %w", err)
	}

	sink := newAudioSink(f, "tencent-tts")
	synthErr := p.synthesizeText(ctx, text, language, sink)
	if synthErr == nil {
		synthErr = sink.finish()
	}
	if closeErr := f.Close(); synthErr == nil && closeErr != nil {
		synthErr = fmt.Errorf("tencent-tts: failed to close output file: %w", closeErr)
	}
	if synthErr != nil {
		_ = os.Remove(outputPath)
		return synthErr
	}
	if sink.total == 0 {
		_ = os.Remove(outputPath)
		return fmt.Errorf("tencent-tts: server returned no audio data")
	}

	slog.Info(
		"tencent-tts synthesize completed",
		slog.String("output", outputPath),
		slog.Int("voice_type", p.VoiceType),
		slog.Int64("audio_bytes", sink.total),
	)
	return nil
}

func (p *TencentTTSProvider) synthesizeText(ctx context.Context, text, language string, w io.Writer) error {
	for _, seg := range splitSpeechSegments(text, tencentTTSSegmentRunes) {
		if err := p.synthesizeSegment(ctx, seg, language, w); err != nil {
			return err
		}
	}
	return nil
}

func (p *TencentTTSProvider) synthesizeSegment(ctx context.Context, text, language string, w io.Writer) error {
	body := tencentTTSRequest{
		Text:            text,
		SessionID:       uuid.NewString(),
		Volume:          0,
		Speed:           tencentSpeed(p.Speed),
		ModelType:       tencentTTSModelType,
		VoiceType:       p.VoiceType,
		PrimaryLanguage: tencentPrimaryLanguage(language),
		SampleRate:      tencentTTSSampleRate,
		Codec:           mp3Format,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("tencent-tts: failed to encode request: %w", err)
	}

	endpoint := p.BaseURL
	if endpoint == "" {
		endpoint = TencentTTSBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("tencent-tts: failed to create request: %w", err)
	}
	ts := time.Now().Unix()
	req.Header.Set("Content-Type", tencentTTSContentTyp)
	req.Header.Set("X-TC-Action", tencentTTSAction)
	req.Header.Set("X-TC-Version", tencentTTSVersion)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(ts, 10))
	// The host header is derived from the URL Go will actually dial, so a
	// test server (or a proxy base) signs the same host it connects to.
	req.Header.Set("Authorization", tencentAuthorization(p.SecretID, p.SecretKey, req.URL.Host, payload, ts))

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: tencentTTSTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("tencent-tts: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if err != nil {
		return fmt.Errorf("tencent-tts: failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tencent-tts: API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed tencentTTSResponse
	if unmarshalErr := json.Unmarshal(respBody, &parsed); unmarshalErr != nil {
		return fmt.Errorf("tencent-tts: malformed response (HTTP %d): %s", resp.StatusCode, truncateForError(string(respBody)))
	}
	if parsed.Response.Error != nil {
		return fmt.Errorf("tencent-tts: API error %s (request %s): %s",
			parsed.Response.Error.Code, parsed.Response.RequestID, parsed.Response.Error.Message)
	}
	if parsed.Response.Audio == "" {
		return fmt.Errorf("tencent-tts: response carries no audio (request %s)", parsed.Response.RequestID)
	}

	audio, err := base64.StdEncoding.DecodeString(parsed.Response.Audio)
	if err != nil {
		return fmt.Errorf("tencent-tts: failed to decode audio: %w", err)
	}
	if _, err := w.Write(audio); err != nil {
		return fmt.Errorf("tencent-tts: failed to write audio: %w", err)
	}
	return nil
}

// tencentSpeed converts the shared multiplier into Tencent's scale, where 0 is
// 1.0x and the documented points are:
//
//	-2 → 0.6x, -1 → 0.8x, 0 → 1.0x, 1 → 1.2x, 2 → 1.5x, 6 → 2.5x
//
// The curve is piecewise, so linear interpolation between those points is the
// closest this parameter can express; values outside the documented range are
// clamped to the endpoints.
func tencentSpeed(multiplier float64) float64 {
	if multiplier <= 0 || multiplier == 1 {
		return 0
	}
	points := [][2]float64{{0.6, -2}, {0.8, -1}, {1.0, 0}, {1.2, 1}, {1.5, 2}, {2.5, 6}}
	if multiplier <= points[0][0] {
		return points[0][1]
	}
	last := points[len(points)-1]
	if multiplier >= last[0] {
		return last[1]
	}
	for i := 1; i < len(points); i++ {
		lo, hi := points[i-1], points[i]
		if multiplier <= hi[0] {
			ratio := (multiplier - lo[0]) / (hi[0] - lo[0])
			// The API accepts two decimals; rounding here keeps the request
			// free of float noise like 1.5000000000000004.
			return math.Round((lo[1]+ratio*(hi[1]-lo[1]))*100) / 100
		}
	}
	return 0
}

// tencentPrimaryLanguage maps a language code to Tencent's 1=Chinese / 2=English.
func tencentPrimaryLanguage(language string) int {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "en") {
		return 2
	}
	return 1
}

// tencentAuthorization builds the TC3-HMAC-SHA256 Authorization header.
//
// Only content-type, host and x-tc-action are signed — the minimum Tencent
// documents for a request without a temporary token. Timestamps are UTC, and
// the date used for the credential scope is the UTC date of that timestamp.
func tencentAuthorization(secretID, secretKey, host string, payload []byte, ts int64) string {
	date := time.Unix(ts, 0).UTC().Format("2006-01-02")
	signedHeaders := "content-type;host;x-tc-action"
	canonicalHeaders := "content-type:" + tencentTTSContentTyp + "\n" +
		"host:" + host + "\n" +
		"x-tc-action:" + strings.ToLower(tencentTTSAction) + "\n"
	canonicalRequest := "POST\n/\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + sha256Hex(payload)

	stringToSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(ts, 10) + "\n" +
		date + "/" + tencentTTSService + "/tc3_request\n" + sha256Hex([]byte(canonicalRequest))

	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, tencentTTSService)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	return "TC3-HMAC-SHA256 Credential=" + secretID + "/" + date + "/" + tencentTTSService + "/tc3_request, " +
		"SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, msg string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(msg))
	return h.Sum(nil)
}
