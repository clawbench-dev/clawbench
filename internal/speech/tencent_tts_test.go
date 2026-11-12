package speech

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestTencentProvider(baseURL string) *TencentTTSProvider {
	p := NewTencentTTSProvider()
	p.SecretID = "AKIDtest"
	p.SecretKey = "secret-test"
	p.VoiceType = 101001
	p.BaseURL = baseURL
	return p
}

// TestTencentTTS_RequestShape verifies the signed request: the TC3 header names
// the right service/date, the action/version headers are present, and the body
// asks for mp3 at a supported sample rate.
func TestTencentTTS_RequestShape(t *testing.T) {
	audio := mp3(strings.Repeat("d", 64))
	var auth, action, version, contentType, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		action = r.Header.Get("X-TC-Action")
		version = r.Header.Get("X-TC-Version")
		contentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"Audio":     base64.StdEncoding.EncodeToString(audio),
				"SessionId": "s",
				"RequestId": "req-1",
			},
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestTencentProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 Credential=AKIDtest/") ||
		!strings.Contains(auth, "/tts/tc3_request") ||
		!strings.Contains(auth, "SignedHeaders=content-type;host;x-tc-action") {
		t.Errorf("unexpected Authorization: %q", auth)
	}
	if action != "TextToVoice" || version != "2019-08-23" {
		t.Errorf("action/version = %q/%q", action, version)
	}
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", contentType)
	}

	var req tencentTTSRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.Codec != "mp3" {
		t.Errorf("Codec = %q, want mp3", req.Codec)
	}
	if req.SampleRate != 16000 || req.ModelType != 1 || req.VoiceType != 101001 {
		t.Errorf("unexpected fixed params: %+v", req)
	}
	if req.PrimaryLanguage != 1 {
		t.Errorf("PrimaryLanguage = %d, want 1 for Chinese", req.PrimaryLanguage)
	}
	if req.SessionID == "" {
		t.Errorf("SessionId must be set")
	}

	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, audio) {
		t.Errorf("output audio mismatch: %d bytes", len(got))
	}
}

// TestTencentTTS_ErrorEnvelope covers the 200 + Response.Error shape Tencent
// uses for auth and parameter failures.
func TestTencentTTS_ErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{
				"Error": map[string]any{
					"Code":    "AuthFailure.SignatureFailure",
					"Message": "signature mismatch",
				},
				"RequestId": "req-2",
			},
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	err := newTestTencentProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "AuthFailure.SignatureFailure") || !strings.Contains(err.Error(), "signature mismatch") {
		t.Errorf("error should carry the vendor code and message, got: %v", err)
	}
}

// TestTencentTTS_SplitsLongText verifies the per-request character cap: text is
// cut into segments instead of being rejected as TextTooLong.
func TestTencentTTS_SplitsLongText(t *testing.T) {
	audio := mp3(strings.Repeat("e", 64))
	var requests, longest int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var req tencentTTSRequest
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		if n := len([]rune(req.Text)); n > longest {
			longest = n
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Response": map[string]any{"Audio": base64.StdEncoding.EncodeToString(audio), "RequestId": "r"},
		})
	}))
	defer srv.Close()

	text := strings.Repeat("这是一句用来测试切分的长文本。", 30) // ~480 runes
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestTencentProvider(srv.URL).Synthesize(context.Background(), text, out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if requests < 2 {
		t.Errorf("expected multiple requests, got %d", requests)
	}
	if longest > tencentTTSSegmentRunes {
		t.Errorf("a segment was %d runes, limit is %d", longest, tencentTTSSegmentRunes)
	}
}

func TestTencentTTS_RequiresCredentials(t *testing.T) {
	p := NewTencentTTSProvider()
	p.SecretID = ""
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "tts.tencent.secret_id") {
		t.Fatalf("expected a config error naming the field, got: %v", err)
	}
}

func TestTencentSpeed(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0},
		{1.0, 0},
		{0.6, -2},
		{0.8, -1},
		{1.2, 1},
		{1.5, 2},
		{2.5, 6},
		{0.5, -2},   // clamped to the low endpoint
		{3.0, 6},    // clamped to the high endpoint
		{1.35, 1.5}, // halfway between 1.2 (1) and 1.5 (2)
	}
	for _, c := range cases {
		if got := tencentSpeed(c.in); got != c.want {
			t.Errorf("tencentSpeed(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestTencentAuthorization is a shape/consistency check: the same inputs must
// produce the same signature, and a different secret must not.
func TestTencentAuthorization(t *testing.T) {
	ts := int64(1760000000) // 2025-10-09T08:53:20Z
	payload := []byte(`{"Text":"hi"}`)
	a := tencentAuthorization("AKID", "secret-a", "tts.tencentcloudapi.com", payload, ts)
	b := tencentAuthorization("AKID", "secret-a", "tts.tencentcloudapi.com", payload, ts)
	if a != b {
		t.Fatal("same inputs must produce the same Authorization")
	}
	c := tencentAuthorization("AKID", "secret-b", "tts.tencentcloudapi.com", payload, ts)
	if a == c {
		t.Fatal("a different secret must change the signature")
	}
	if !strings.Contains(a, "/2025-10-09/tts/tc3_request") {
		t.Errorf("Authorization %q does not carry the UTC date of the timestamp", a)
	}
	if !strings.Contains(a, "Signature=") {
		t.Errorf("Authorization missing signature: %q", a)
	}
}

func TestTencentTTSCacheIdentity(t *testing.T) {
	p := newTestTencentProvider(TencentTTSBaseURL)
	p.Speed = 1.2
	id := p.CacheIdentity()
	if !strings.Contains(id, "101001") || !strings.Contains(id, "1.2") {
		t.Errorf("CacheIdentity = %q", id)
	}
	if strings.Contains(id, "secret-test") {
		t.Errorf("CacheIdentity must not contain the secret: %q", id)
	}
}
