package stt

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Tencent Cloud speech recognition (语音识别 / ASR).
//
// Two APIs sit behind one pair of credentials:
//
//   - Whole-file transcription uses 一句话识别 (SentenceRecognition), a signed
//     JSON POST. It accepts at most 60 s / 3 MB per request.
//   - Live recognition uses the realtime WebSocket (see tencent_stream.go).
//
// Auth is not a bearer token: every request is signed with the account's
// SecretId/SecretKey pair (TC3-HMAC-SHA256 for HTTP, HMAC-SHA1 for the
// WebSocket), and the realtime API additionally needs the numeric AppID in the
// URL path. Language is encoded in the engine model name (16k_zh, 16k_en, …),
// so there is no separate language field.
const (
	// TencentASRBaseURL is the signed JSON API root for 一句话识别.
	TencentASRBaseURL = "https://asr.tencentcloudapi.com"

	// TencentASRWSBaseURL is the realtime recognition host.
	TencentASRWSBaseURL = "wss://asr.cloud.tencent.com"

	// TencentCAMBaseURL is the CAM API root used to read the account APPID out
	// of the key pair.
	TencentCAMBaseURL = "https://cam.tencentcloudapi.com"

	// TencentAPIVersion is the SentenceRecognition version.
	TencentAPIVersion = "2019-06-14"

	// TencentDefaultModel is the 16 kHz Mandarin engine. The same name is used
	// as EngSerViceType by the file API and engine_model_type by the realtime
	// API, so one field drives both.
	TencentDefaultModel = "16k_zh"

	// tencentSignService is the TC3 signing service name for ASR.
	tencentSignService = "asr"

	// tencentSentencePath is the signed POST path. The API accepts "/" only.
	tencentSentencePath = "/"

	// tencentMaxAudioBytes keeps the base64 payload under the documented 3 MB
	// cap (base64 inflates by 4/3, so 2 MiB raw encodes to ~2.7 MB).
	tencentMaxAudioBytes = 2 << 20

	// tencentStreamAudioPath is the realtime endpoint path prefix.
	tencentStreamAudioPath = "/asr/v2/"

	// tencentCAMService / tencentCAMAction / tencentCAMVersion describe the CAM
	// call that returns the account APPID (GetUserAppId).
	tencentCAMService = "cam"
	tencentCAMAction  = "GetUserAppId"
	tencentCAMVersion = "2019-01-16"
	// tencentCAMRegion is required by the CAM frontend even though the call is
	// account-wide; any valid region answers the same thing.
	tencentCAMRegion = "ap-guangzhou"

	// tencentAppIDLookupTimeout bounds the APPID lookup so a slow account API
	// cannot hold up the first recording.
	tencentAppIDLookupTimeout = 5 * time.Second
)

// Container names shared with the other providers' format detection.
const (
	audioFormatWAV  = "wav"
	audioFormatMP3  = "mp3"
	audioFormatOGG  = "ogg"
	audioFormatWebM = "webm"
)

// TencentProvider calls Tencent Cloud speech recognition.
type TencentProvider struct {
	SecretID  string
	SecretKey string
	AppID     string // numeric account AppID, required by the realtime API only
	Model     string // engine model type, e.g. 16k_zh, 16k_en, 16k_zh_dialect

	// BaseURL / WSBaseURL are the HTTP and WebSocket roots. Tencent serves
	// speech from these two hosts only; they are fields so a test can point
	// them at a local server, not user-facing settings.
	BaseURL   string
	WSBaseURL string

	// CAMBaseURL is the account API root used by EffectiveAppID. A field for
	// the same reason as the two above.
	CAMBaseURL string

	HTTPClient *http.Client

	// resolvedAppID caches the APPID read from the key pair (see
	// EffectiveAppID). Providers are rebuilt whenever the config changes, so a
	// cached value never outlives the credentials it was read with.
	appIDMu       sync.Mutex
	resolvedAppID string

	// Recognition options shared by both APIs.
	EnableITN     bool // convert_num_mode: 1 = smart Arabic numerals, 0 = Chinese numerals
	EnableDDC     bool // filter_modal: drop filler words (1 = partial)
	EndWindowSize int  // vad_silence_time in ms (500–2000), realtime only
}

// NewTencentProvider creates a Tencent Cloud STT provider.
func NewTencentProvider(secretID, secretKey, appID string) *TencentProvider {
	return &TencentProvider{
		SecretID:      secretID,
		SecretKey:     secretKey,
		AppID:         appID,
		Model:         TencentDefaultModel,
		BaseURL:       TencentASRBaseURL,
		WSBaseURL:     TencentASRWSBaseURL,
		HTTPClient:    &http.Client{Timeout: 60 * time.Second},
		EnableITN:     true,
		EndWindowSize: 1000,
	}
}

// model returns the configured engine or the 16 kHz Mandarin default.
func (p *TencentProvider) model() string {
	if strings.TrimSpace(p.Model) != "" {
		return p.Model
	}
	return TencentDefaultModel
}

// EffectiveAppID returns the account APPID the realtime API needs.
//
// It is read from the key pair rather than trusted from the settings: the
// console prints the UIN right next to the APPID, both are long numbers, and a
// pasted UIN passes every other check (the file API never looks at the AppID)
// only to fail every recording with a 4002 that just says "check the AppID".
// A typed value is used when the lookup itself is refused — a key scoped to
// ASR alone cannot read account information.
func (p *TencentProvider) EffectiveAppID(ctx context.Context) (string, error) {
	p.appIDMu.Lock()
	defer p.appIDMu.Unlock()
	if p.resolvedAppID != "" {
		return p.resolvedAppID, nil
	}
	if err := p.requireCredentials(); err != nil {
		return "", err
	}

	lookupCtx, cancel := context.WithTimeout(ctx, tencentAppIDLookupTimeout)
	defer cancel()
	appID, err := p.lookupAppID(lookupCtx)
	if err != nil {
		if typed := strings.TrimSpace(p.AppID); typed != "" {
			slog.Warn("stt: tencent AppID lookup failed, using the configured value",
				slog.String("error", err.Error()))
			p.resolvedAppID = typed
			return typed, nil
		}
		return "", fmt.Errorf("stt: tencent AppID is not configured and could not be read from the credentials: %w", err)
	}
	if typed := strings.TrimSpace(p.AppID); typed != "" && typed != appID {
		slog.Info("stt: tencent AppID in the settings is not this key pair's account APPID, using the account's",
			slog.String("configured", typed), slog.String("account", appID))
	}
	p.resolvedAppID = appID
	return appID, nil
}

// tencentAppIDResponse is the GetUserAppId answer. Errors arrive with HTTP 200.
type tencentAppIDResponse struct {
	Response struct {
		AppID int64 `json:"AppId"`
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"Response"`
}

// lookupAppID asks CAM which account the key pair belongs to.
func (p *TencentProvider) lookupAppID(ctx context.Context) (string, error) {
	base := p.CAMBaseURL
	if base == "" {
		base = TencentCAMBaseURL
	}
	body := []byte("{}")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+tencentSentencePath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("stt: create appid request: %w", err)
	}
	now := time.Now()
	req.Header.Set("Content-Type", tencentJSONContentType)
	req.Header.Set("X-TC-Action", tencentCAMAction)
	req.Header.Set("X-TC-Version", tencentCAMVersion)
	req.Header.Set("X-TC-Region", tencentCAMRegion)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
	req.Header.Set("Authorization", tencentTC3Authorization(
		p.SecretID, p.SecretKey, tencentCAMService, tencentCAMAction,
		req.URL.Host, string(body), now,
	))

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: appid lookup failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("stt: read appid response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Body: string(respBody)}
	}
	var parsed tencentAppIDResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("stt: decode appid response (HTTP %d): %s", resp.StatusCode, truncate(respBody))
	}
	if e := parsed.Response.Error; e != nil && e.Code != "" {
		return "", fmt.Errorf("stt: appid lookup returned %s: %s", e.Code, e.Message)
	}
	if parsed.Response.AppID <= 0 {
		return "", fmt.Errorf("stt: appid lookup returned no AppId")
	}
	return strconv.FormatInt(parsed.Response.AppID, 10), nil
}

// tencentSentenceRequest is the SentenceRecognition body.
type tencentSentenceRequest struct {
	EngSerViceType string `json:"EngSerViceType"`
	SourceType     int    `json:"SourceType"`
	VoiceFormat    string `json:"VoiceFormat"`
	Data           string `json:"Data"`
	DataLen        int    `json:"DataLen"`
	ConvertNumMode int    `json:"ConvertNumMode"`
	FilterModal    int    `json:"FilterModal"`
}

// tencentSentenceResponse is the signed POST answer. Errors also arrive with
// HTTP 200, so Response.Error is the authoritative failure signal.
type tencentSentenceResponse struct {
	Response struct {
		Result    string `json:"Result"`
		RequestID string `json:"RequestId"`
		Error     *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"Response"`
}

// Transcribe recognizes a complete audio file through 一句话识别.
func (p *TencentProvider) Transcribe(ctx context.Context, audioReader io.Reader, _ string) (string, error) {
	if err := p.requireCredentials(); err != nil {
		return "", err
	}
	body, err := p.sentenceRequestBody(audioReader)
	if err != nil {
		return "", err
	}
	return p.postSentence(ctx, body)
}

// requireCredentials rejects a provider that was never configured, so the
// failure names the missing field instead of coming back as a signature error.
func (p *TencentProvider) requireCredentials() error {
	if strings.TrimSpace(p.SecretID) == "" || strings.TrimSpace(p.SecretKey) == "" {
		return errors.New("stt: tencent SecretId and SecretKey are required")
	}
	return nil
}

// sentenceRequestBody reads the clip, validates it against the API's format and
// size limits, and builds the signed POST body.
func (p *TencentProvider) sentenceRequestBody(audioReader io.Reader) ([]byte, error) {
	audio, err := io.ReadAll(audioReader)
	if err != nil {
		return nil, fmt.Errorf("stt: read audio: %w", err)
	}
	if len(audio) == 0 {
		return nil, errors.New("stt: empty audio")
	}
	if len(audio) > tencentMaxAudioBytes {
		return nil, fmt.Errorf("stt: audio is %d bytes; tencent sentence recognition accepts at most %d", len(audio), tencentMaxAudioBytes)
	}
	format, err := detectAudioFormat(audio)
	if err != nil {
		return nil, err
	}
	voiceFormat, err := tencentVoiceFormat(format)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(tencentSentenceRequest{
		EngSerViceType: p.model(),
		SourceType:     1, // audio inline in Data
		VoiceFormat:    voiceFormat,
		Data:           base64.StdEncoding.EncodeToString(audio),
		DataLen:        len(audio),
		ConvertNumMode: boolToInt(p.EnableITN),
		FilterModal:    boolToInt(p.EnableDDC),
	})
	if err != nil {
		return nil, fmt.Errorf("stt: encode request: %w", err)
	}
	return body, nil
}

// postSentence sends the signed request and decodes the answer.
func (p *TencentProvider) postSentence(ctx context.Context, body []byte) (string, error) {
	base := p.BaseURL
	if base == "" {
		base = TencentASRBaseURL
	}
	u := strings.TrimRight(base, "/") + tencentSentencePath

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("stt: create request: %w", err)
	}
	// The signed headers must match the canonical request exactly, so the
	// content type is set to the same literal the signer used. One timestamp
	// serves both the header and the signature: signing a different second
	// than the one sent is an immediate SignatureFailure.
	now := time.Now()
	req.Header.Set("Content-Type", tencentJSONContentType)
	req.Header.Set("X-TC-Action", "SentenceRecognition")
	req.Header.Set("X-TC-Version", TencentAPIVersion)
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(now.Unix(), 10))
	req.Header.Set("Authorization", tencentTC3Authorization(
		p.SecretID, p.SecretKey, tencentSignService, "SentenceRecognition",
		req.URL.Host, string(body), now,
	))

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
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
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Status: resp.StatusCode, Body: string(respBody)}
	}

	var parsed tencentSentenceResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("stt: decode response (HTTP %d): %s", resp.StatusCode, truncate(respBody))
	}
	if e := parsed.Response.Error; e != nil && e.Code != "" {
		return "", fmt.Errorf("stt: tencent returned %s: %s (request %s)", e.Code, e.Message, parsed.Response.RequestID)
	}
	return parsed.Response.Result, nil
}

// tencentJSONContentType is the exact content type the signer canonicalizes.
const tencentJSONContentType = "application/json; charset=utf-8"

// tencentVoiceFormat maps a detected container to the VoiceFormat the API
// accepts. WebM is deliberately absent from Tencent's list: the API decodes
// containers, not bare Opus frames, so a WebM upload is rejected here instead
// of coming back as a decode error. The browser never sends one — dictation
// records WAV for this engine.
func tencentVoiceFormat(format string) (string, error) {
	switch format {
	case audioFormatWAV:
		return audioFormatWAV, nil
	case audioFormatMP3:
		return audioFormatMP3, nil
	case audioFormatOGG:
		return "ogg-opus", nil
	case audioFormatWebM:
		// The web client converts its recording to WAV before uploading, so this
		// is only reachable from an API client that posts a WebM file directly.
		return "", errors.New("stt: tencent sentence recognition does not accept webm audio; send wav/pcm/ogg-opus/mp3/m4a/aac/amr instead")
	}
	return "", fmt.Errorf("stt: tencent does not accept the %q container", format)
}

// boolToInt maps a switch onto Tencent's 0/1 integer options.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// tencentTC3Authorization builds the TC3-HMAC-SHA256 Authorization header.
//
// The canonical request is built from the same literals that are put on the
// wire (method, "/", empty query, content-type, host, x-tc-action). Any
// mismatch — a different content type, an extra signed header — produces a
// SignatureFailure from the server rather than a useful error, so the two must
// be changed together.
func tencentTC3Authorization(secretID, secretKey, service, action, host, payload string, ts time.Time) string {
	date := ts.UTC().Format("2006-01-02")
	signedHeaders := "content-type;host;x-tc-action"
	canonicalHeaders := "content-type:" + tencentJSONContentType + "\n" +
		"host:" + host + "\n" +
		"x-tc-action:" + strings.ToLower(action) + "\n"
	canonicalRequest := "POST\n" + tencentSentencePath + "\n\n" + canonicalHeaders + "\n" +
		signedHeaders + "\n" + sha256Hex([]byte(payload))
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := "TC3-HMAC-SHA256\n" + strconv.FormatInt(ts.Unix(), 10) + "\n" +
		credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	return "TC3-HMAC-SHA256 Credential=" + secretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

// sha256Hex returns the lowercase hex SHA-256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// hmacSHA256 returns HMAC-SHA256(key, msg).
func hmacSHA256(key []byte, msg string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(msg))
	return mac.Sum(nil)
}
