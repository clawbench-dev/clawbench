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

func TestVolcanoTTS_ValidateCoversRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		mut  func(p *VolcanoTTSProvider)
		want string
	}{
		{"api_key", func(p *VolcanoTTSProvider) { p.APIKey = "" }, "tts.volcano.api_key"},
		{"api_key blank", func(p *VolcanoTTSProvider) { p.APIKey = "  " }, "tts.volcano.api_key"},
		{"voice", func(p *VolcanoTTSProvider) { p.Voice = "" }, "tts.volcano.voice"},
	}
	for _, tc := range cases {
		p := newTestVolcanoProvider("http://unused")
		tc.mut(p)
		if err := p.validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: validate err = %v, want it to name %s", tc.name, err, tc.want)
		}
	}
	if err := newTestVolcanoProvider("http://unused").validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
}

// TestVolcanoTTS_SpeedBecomesAPercentageOffset pins the shared multiplier onto
// the API's -50…100 scale, including both clamps. Asserted on the REQUEST BODY
// the server actually receives — computing the expected value with the same
// expression would pass even if the field were never sent.
func TestVolcanoTTS_SpeedBecomesAPercentageOffset(t *testing.T) {
	cases := []struct {
		speed float64
		want  int
	}{
		{0, 0},    // unset → normal
		{1, 0},    // normal speed is omitted entirely
		{1.5, 50}, // +50%
		{2, 100},  // the fastest documented point
		{3, 100},  // clamped
		{0.5, -50},
		{0.1, -50}, // clamped
	}
	for _, tc := range cases {
		var got volcanoTTSRequest
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &got)
			writeVolcanoChunks(w, mp3(strings.Repeat("s", 32)))
		}))

		p := newTestVolcanoProvider(srv.URL)
		p.Speed = tc.speed
		out := filepath.Join(t.TempDir(), "out.mp3")
		err := p.Synthesize(context.Background(), "你好", out, "zh")
		srv.Close()
		if err != nil {
			t.Fatalf("speed %v: Synthesize: %v", tc.speed, err)
		}
		if got.ReqParams.AudioParams.SpeechRate != tc.want {
			t.Errorf("speed %v → speech_rate %d, want %d", tc.speed, got.ReqParams.AudioParams.SpeechRate, tc.want)
		}
	}
}

func TestVolcanoTTS_HTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("bad api key"))
	}))
	defer srv.Close()

	p := newTestVolcanoProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad api key") {
		t.Fatalf("err = %v, want the status and the body", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("a failed synthesis must not leave an output file behind")
	}
}

// TestVolcanoTTS_EmptyChunksAreSkipped: a heartbeat line with no data must not
// be decoded into an empty write, and must not end the stream.
func TestVolcanoTTS_EmptyChunksAreSkipped(t *testing.T) {
	audio := mp3(strings.Repeat("k", 32))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": ""})
		writeVolcanoChunks(w, audio)
	}))
	defer srv.Close()

	p := newTestVolcanoProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "你好", out, "zh"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(got, audio) {
		t.Errorf("output = %x, want the audio chunk that followed the heartbeat", got)
	}
}

func TestVolcanoTTS_UndecodableChunkIsFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": "!!!!not-base64!!!!"})
	}))
	defer srv.Close()

	p := newTestVolcanoProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "decode audio chunk") {
		t.Fatalf("err = %v, want a chunk decode error", err)
	}
}

func TestVolcanoTTS_RejectsNonMP3Audio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wav := append([]byte("RIFF....WAVE"), make([]byte, mp3SniffPrefixBytes)...)
		writeVolcanoChunks(w, wav)
	}))
	defer srv.Close()

	p := newTestVolcanoProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "你好", out, "zh")
	if err == nil || !strings.Contains(err.Error(), "not mp3") {
		t.Fatalf("err = %v, want a not-mp3 error", err)
	}
}

// TestVolcanoTTS_StreamingForwardsOnlyValidatedAudio pins the ordering the
// player depends on: a chunk reaches chunkCh only after it passed the MP3
// check, so a misconfigured server never streams garbage to the browser.
func TestVolcanoTTS_StreamingForwardsOnlyValidatedAudio(t *testing.T) {
	audio := mp3(strings.Repeat("m", 48))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeVolcanoChunks(w, audio)
	}))
	defer srv.Close()

	p := newTestVolcanoProvider(srv.URL)
	out := filepath.Join(t.TempDir(), "out.mp3")
	ch := make(chan []byte, 4)
	if err := p.SynthesizeStream(context.Background(), "你好", out, "zh", ch); err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}
	close(ch)
	var forwarded []byte
	for b := range ch {
		forwarded = append(forwarded, b...)
	}
	if !bytes.Equal(forwarded, audio) {
		t.Errorf("forwarded %x, want the same audio that was written to the file", forwarded)
	}
}
