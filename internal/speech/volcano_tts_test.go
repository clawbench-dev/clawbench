package speech

import (
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

func newTestVolcanoProvider(baseURL string) *VolcanoTTSProvider {
	p := NewVolcanoTTSProvider()
	p.APIKey = "key-test"
	p.BaseURL = baseURL
	return p
}

// writeVolcanoChunks writes the newline-delimited response shape: audio chunks
// with code 0, terminated by the success code.
func writeVolcanoChunks(w io.Writer, chunks ...[]byte) {
	for _, c := range chunks {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "",
			"data":    base64.StdEncoding.EncodeToString(c),
		})
	}
}

// TestVolcanoTTS_StreamsChunks covers the primary path: the response arrives as
// JSON lines and each fragment is both written to the file and forwarded to the
// streaming channel.
func TestVolcanoTTS_StreamsChunks(t *testing.T) {
	first := mp3(strings.Repeat("f", 32))
	second := []byte(strings.Repeat("g", 32))

	var gotPath, gotKey, gotResource, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Api-Key")
		gotResource = r.Header.Get("X-Api-Resource-Id")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		writeVolcanoChunks(w, first, second)
	}))
	defer srv.Close()

	chunks := make(chan []byte, 8)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestVolcanoProvider(srv.URL).SynthesizeStream(context.Background(), "你好", out, "zh", chunks); err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}
	close(chunks)

	if gotPath != "/api/v3/tts/unidirectional" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "key-test" || gotResource != "seed-tts-2.0" {
		t.Errorf("headers = %q/%q", gotKey, gotResource)
	}

	var req volcanoTTSRequest
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if req.ReqParams.Text != "你好" || req.ReqParams.Speaker != "zh_female_vv_uranus_bigtts" {
		t.Errorf("unexpected body: %+v", req.ReqParams)
	}
	if req.ReqParams.AudioParams.Format != "mp3" || req.ReqParams.AudioParams.SampleRate != 24000 {
		t.Errorf("audio params = %+v", req.ReqParams.AudioParams)
	}

	var forwarded int
	for c := range chunks {
		forwarded += len(c)
	}
	if forwarded != len(first)+len(second) {
		t.Errorf("forwarded %d bytes, want %d", forwarded, len(first)+len(second))
	}

	got, _ := os.ReadFile(out)
	if string(got) != string(first)+string(second) {
		t.Errorf("output audio mismatch: %d bytes", len(got))
	}
}

// TestVolcanoTTS_ErrorCode covers an in-body error code.
func TestVolcanoTTS_ErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    40000001,
			"message": "invalid speaker",
		})
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	err := newTestVolcanoProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "40000001") || !strings.Contains(err.Error(), "invalid speaker") {
		t.Errorf("error should carry the vendor code and message, got: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("failed synthesis must not leave an output file")
	}
}

// TestVolcanoTTS_MalformedLineIsFatal: a non-JSON line means the endpoint is
// not speaking this API, and must not be silently skipped.
func TestVolcanoTTS_MalformedLineIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>not this api</html>\n")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "out.mp3")
	err := newTestVolcanoProvider(srv.URL).Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "failed to decode response stream") {
		t.Fatalf("expected a decode error, got: %v", err)
	}
}

// TestVolcanoTTS_RequiresCredentials verifies the app id / access key pair is
// checked before any request is made.
func TestVolcanoTTS_RequiresCredentials(t *testing.T) {
	p := NewVolcanoTTSProvider()
	p.APIKey = ""
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "tts.volcano.api_key") {
		t.Fatalf("expected a config error naming the field, got: %v", err)
	}
}

func TestVolcanoTTS_SplitsLongText(t *testing.T) {
	audio := mp3(strings.Repeat("h", 64))
	var requests, longest int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var req volcanoTTSRequest
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		if n := len([]rune(req.ReqParams.Text)); n > longest {
			longest = n
		}
		writeVolcanoChunks(w, audio)
	}))
	defer srv.Close()

	text := strings.Repeat("这是一句用来测试切分的长文本。", 60) // ~960 runes
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := newTestVolcanoProvider(srv.URL).Synthesize(context.Background(), text, out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if requests < 2 {
		t.Errorf("expected multiple requests, got %d", requests)
	}
	if longest > volcanoTTSSegmentRunes {
		t.Errorf("a segment was %d runes, limit is %d", longest, volcanoTTSSegmentRunes)
	}
}

func TestVolcanoTTSCacheIdentity(t *testing.T) {
	p := newTestVolcanoProvider(VolcanoTTSBaseURL)
	p.Speed = 1.5
	id := p.CacheIdentity()
	for _, want := range []string{"volcano", "seed-tts-2.0", "zh_female_vv_uranus_bigtts", "1.5"} {
		if !strings.Contains(id, want) {
			t.Errorf("CacheIdentity %q missing %q", id, want)
		}
	}
	if strings.Contains(id, "key-test") {
		t.Errorf("CacheIdentity must not contain the access key: %q", id)
	}
}
