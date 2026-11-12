package speech

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestMinimaxProvider(baseURL string) *MinimaxTTSProvider {
	p := NewMinimaxTTSProvider()
	p.APIKey = "mm-key"
	p.BaseURL = baseURL
	p.Model = "speech-02-hd"
	p.Voice = "female-shaonv"
	return p
}

// TestMinimaxTTS_RequestShapeAndHexAudio covers the normal path: GroupId is
// appended as a query parameter, the body asks for mp3/hex, and the hex payload
// is decoded into the output file.
func TestMinimaxTTS_RequestShapeAndHexAudio(t *testing.T) {
	audio := mp3(strings.Repeat("j", 64))

	var gotPath, gotQuery, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("GroupId")
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString(audio), "status": 2},
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
			"trace_id":  "trace-1",
		})
	}))
	defer srv.Close()

	p := newTestMinimaxProvider(srv.URL)
	p.GroupID = "group-42"

	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "你好", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if gotPath != "/v1/t2a_v2" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "group-42" {
		t.Errorf("GroupId query = %q, want group-42", gotQuery)
	}
	if gotAuth != "Bearer mm-key" {
		t.Errorf("Authorization = %q", gotAuth)
	}

	var req minimaxTTSRequest
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.Model != "speech-02-hd" || req.Text != "你好" || req.Stream {
		t.Errorf("unexpected body: %+v", req)
	}
	if req.VoiceSetting.VoiceID != "female-shaonv" || req.VoiceSetting.Speed != 1 {
		t.Errorf("voice_setting = %+v", req.VoiceSetting)
	}
	if req.AudioSetting.Format != mp3Format || req.AudioSetting.Channel != 1 {
		t.Errorf("audio_setting = %+v", req.AudioSetting)
	}
	if req.OutputFormat != "hex" {
		t.Errorf("output_format = %q, want hex", req.OutputFormat)
	}

	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, audio) {
		t.Errorf("output audio = %d bytes, want %d", len(got), len(audio))
	}
}

// TestMinimaxTTS_NoGroupIdWhenUnset keeps the query string clean for accounts
// that authenticate with the key alone.
func TestMinimaxTTS_NoGroupIdWhenUnset(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString(mp3(strings.Repeat("k", 32)))},
			"base_resp": map[string]any{"status_code": 0},
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestMinimaxProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if rawQuery != "" {
		t.Errorf("query = %q, want empty", rawQuery)
	}
}

// TestMinimaxTTS_BaseRespError covers the 200 + base_resp.status_code shape.
func TestMinimaxTTS_BaseRespError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_resp": map[string]any{"status_code": 1004, "status_msg": "invalid api key"},
			"trace_id":  "trace-2",
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	err := newTestMinimaxProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "1004") || !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should carry the vendor code and message, got: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("failed synthesis must not leave an output file")
	}
}

// TestMinimaxTTS_SplitsLongText verifies a long reply is cut into bounded
// requests instead of being sent as one huge one.
func TestMinimaxTTS_SplitsLongText(t *testing.T) {
	audio := mp3(strings.Repeat("l", 64))
	var requests, longest int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var req minimaxTTSRequest
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		if n := len([]rune(req.Text)); n > longest {
			longest = n
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString(audio)},
			"base_resp": map[string]any{"status_code": 0},
		})
	}))
	defer srv.Close()

	text := strings.Repeat("这是一句用来测试切分的长文本。", 200) // ~3200 runes
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestMinimaxProvider(srv.URL).Synthesize(context.Background(), text, out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if requests < 2 {
		t.Errorf("expected multiple requests, got %d", requests)
	}
	if longest > minimaxTTSSegmentRunes {
		t.Errorf("a segment was %d runes, limit is %d", longest, minimaxTTSSegmentRunes)
	}
}

func TestMinimaxTTS_RequiresAPIKey(t *testing.T) {
	p := NewMinimaxTTSProvider()
	p.APIKey = ""
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "tts.minimax.api_key") {
		t.Fatalf("expected a config error naming the field, got: %v", err)
	}
}

func TestMinimaxTTSCacheIdentity(t *testing.T) {
	p := newTestMinimaxProvider(MinimaxTTSBaseURL)
	p.Speed = 1.2
	id := p.CacheIdentity()
	for _, want := range []string{"minimax", "speech-02-hd", "female-shaonv", "1.2"} {
		if !strings.Contains(id, want) {
			t.Errorf("CacheIdentity %q missing %q", id, want)
		}
	}
	if strings.Contains(id, "mm-key") {
		t.Errorf("CacheIdentity must not contain the API key: %q", id)
	}
}

func TestMinimaxTTS_ValidateCoversEveryRequiredField(t *testing.T) {
	cases := []struct {
		name string
		mut  func(p *MinimaxTTSProvider)
		want string
	}{
		{"api_key", func(p *MinimaxTTSProvider) { p.APIKey = "" }, "tts.minimax.api_key"},
		{"api_key blank", func(p *MinimaxTTSProvider) { p.APIKey = "  " }, "tts.minimax.api_key"},
		{"model", func(p *MinimaxTTSProvider) { p.Model = "" }, "tts.minimax.model"},
		{"voice", func(p *MinimaxTTSProvider) { p.Voice = "" }, "tts.minimax.voice"},
	}
	for _, tc := range cases {
		p := newTestMinimaxProvider("http://unused")
		tc.mut(p)
		if err := p.validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: validate err = %v, want it to name %s", tc.name, err, tc.want)
		}
	}
	if err := newTestMinimaxProvider("http://unused").validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

func TestMinimaxTTS_SynthesisURLHandlesEveryBaseShape(t *testing.T) {
	cases := []struct {
		name  string
		base  string
		group string
		want  string
	}{
		{"empty base falls back to the default", "", "", MinimaxTTSBaseURL + minimaxTTSPath},
		{"trailing slashes are trimmed", "https://api.example.com/", "", "https://api.example.com" + minimaxTTSPath},
		{"group id is a query parameter", "https://api.example.com", "g-1", "https://api.example.com" + minimaxTTSPath + "?GroupId=g-1"},
		{"existing query is preserved", "https://api.example.com?foo=bar", "g-1", "https://api.example.com" + minimaxTTSPath + "?GroupId=g-1&foo=bar"},
	}
	for _, tc := range cases {
		p := newTestMinimaxProvider(tc.base)
		p.GroupID = tc.group
		if got := p.synthesisURL(); got != tc.want {
			t.Errorf("%s: synthesisURL() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMinimaxTTS_HTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	p := newTestMinimaxProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v, want the status and the body", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("a failed synthesis must not leave an output file behind")
	}
}

func TestMinimaxTTS_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	p := newTestMinimaxProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "malformed response") {
		t.Fatalf("err = %v, want a malformed-response error", err)
	}
}

func TestMinimaxTTS_MissingAudioNamesTheTrace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
			"trace_id":  "trace-missing",
		})
	}))
	defer srv.Close()

	p := newTestMinimaxProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "no audio") || !strings.Contains(err.Error(), "trace-missing") {
		t.Fatalf("err = %v, want a no-audio error naming the trace", err)
	}
}

func TestMinimaxTTS_RejectsNonHexAudio(t *testing.T) {
	// A base64 payload (what a different vendor returns) must fail with a decode
	// error rather than being written out as garbage.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": "SUQzBAAAAAA=", "status": 2},
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
			"trace_id":  "trace-hex",
		})
	}))
	defer srv.Close()

	p := newTestMinimaxProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "decode hex audio") || !strings.Contains(err.Error(), "trace-hex") {
		t.Fatalf("err = %v, want a hex decode error naming the trace", err)
	}
}

func TestMinimaxTTS_RejectsNonMP3Audio(t *testing.T) {
	// Valid hex, but a WAV container: the sink refuses to cache it under .mp3.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wav := append([]byte("RIFF....WAVE"), make([]byte, mp3SniffPrefixBytes)...)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":      map[string]any{"audio": hex.EncodeToString(wav), "status": 2},
			"base_resp": map[string]any{"status_code": 0, "status_msg": "success"},
		})
	}))
	defer srv.Close()

	p := newTestMinimaxProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "not mp3") {
		t.Fatalf("err = %v, want a not-mp3 error", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("a rejected payload must not leave an output file behind")
	}
}
