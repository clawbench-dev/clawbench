package speech

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sseEvent builds one SSE frame for a speech.audio.delta event.
func sseEvent(t *testing.T, audio []byte) string {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"type":  "speech.audio.delta",
		"audio": base64.StdEncoding.EncodeToString(audio),
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return "event: speech.audio.delta\ndata: " + string(payload) + "\n\n"
}

// newTestProvider returns a provider pointed at srv with the minimum config.
func newTestProvider(t *testing.T, srv *httptest.Server) *OpenAITTSProvider {
	t.Helper()
	p := NewOpenAITTSProvider()
	p.BaseURL = srv.URL
	p.Model = "gpt-4o-mini-tts"
	p.Voice = "alloy"
	return p
}

// mp3HeaderBytes is a minimal valid MP3 frame prefix (0xFF 0xFB = MPEG-1
// Layer III frame sync). The provider now sniffs the leading bytes against
// response_format=mp3, so fixtures must be real MP3, not arbitrary text.
var mp3HeaderBytes = []byte{0xFF, 0xFB, 0x90, 0x00}

// mp3 returns an MP3 fixture that begins with a valid frame sync.
func mp3(rest string) []byte {
	return append(append([]byte{}, mp3HeaderBytes...), rest...)
}

func TestResolveOpenAISpeechURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare host", "https://api.openai.com", "https://api.openai.com/v1/audio/speech"},
		{"trailing slash", "https://api.openai.com/", "https://api.openai.com/v1/audio/speech"},
		{"v1 base", "https://api.openai.com/v1", "https://api.openai.com/v1/audio/speech"},
		{"v1 trailing slash", "https://api.openai.com/v1/", "https://api.openai.com/v1/audio/speech"},
		{"full endpoint", "https://api.openai.com/v1/audio/speech", "https://api.openai.com/v1/audio/speech"},
		{"local host", "http://localhost:8000/v1", "http://localhost:8000/v1/audio/speech"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveOpenAISpeechURL(tc.in); got != tc.want {
				t.Fatalf("ResolveOpenAISpeechURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestOpenAITTS_ValidateRequiresConfig(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*OpenAITTSProvider)
		wantSub string
	}{
		{"missing base_url", func(p *OpenAITTSProvider) { p.BaseURL = "" }, "base_url"},
		{"missing model", func(p *OpenAITTSProvider) { p.Model = "" }, "model"},
		{"missing voice", func(p *OpenAITTSProvider) { p.Voice = "" }, "voice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &OpenAITTSProvider{BaseURL: "https://x/v1", Model: "m", Voice: "v"}
			tc.mutate(p)

			out := filepath.Join(t.TempDir(), "out.mp3")
			err := p.Synthesize(context.Background(), "hi", out, "en")
			if err == nil {
				t.Fatal("expected an error for incomplete config")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not name the missing field %q", err, tc.wantSub)
			}
			if _, statErr := os.Stat(out); statErr == nil {
				t.Fatal("no output file should be created when config is incomplete")
			}
		})
	}
}

func TestOpenAITTS_SSEStreamWritesFileAndChunks(t *testing.T) {
	chunk1 := mp3("first-audio-chunk")
	chunk2 := []byte("second-audio-chunk")

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseEvent(t, chunk1))
		_, _ = io.WriteString(w, "event: speech.audio.done\ndata: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}\n\n")
		_, _ = io.WriteString(w, sseEvent(t, chunk2))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	p.APIKey = "sk-test"

	out := filepath.Join(t.TempDir(), "out.mp3")
	ch := make(chan []byte, 8)
	if err := p.SynthesizeStream(context.Background(), "hello world", out, "en", ch); err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}

	// File must contain both chunks concatenated — the done event carries no
	// audio and must not corrupt the stream.
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if want := string(chunk1) + string(chunk2); string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}

	// Each chunk must also have been forwarded to the channel.
	close(ch)
	var got [][]byte
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 2 {
		t.Fatalf("got %d chunks, want 2: %q", len(got), got)
	}
	if !bytes.Equal(got[0], chunk1) || !bytes.Equal(got[1], chunk2) {
		t.Fatalf("chunks = %q, want [%q %q]", got, chunk1, chunk2)
	}

	// Request contract: SSE requested, mp3 format, auth header, model + voice.
	if gotBody["stream_format"] != "sse" {
		t.Errorf("stream_format = %v, want sse", gotBody["stream_format"])
	}
	if gotBody["response_format"] != "mp3" {
		t.Errorf("response_format = %v, want mp3 (MSE playback path)", gotBody["response_format"])
	}
	if gotBody["model"] != "gpt-4o-mini-tts" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["voice"] != "alloy" {
		t.Errorf("voice = %v", gotBody["voice"])
	}
	if gotBody["input"] != "hello world" {
		t.Errorf("input = %v", gotBody["input"])
	}
}

func TestOpenAITTS_SSERejectedFallsBackToAudio(t *testing.T) {
	rawAudio := mp3("raw-mp3-bytes")
	var formats []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		format, _ := body["stream_format"].(string)
		formats = append(formats, format)

		if format == "sse" {
			// Mirrors tts-1 / tts-1-hd rejecting stream_format=sse.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"stream_format sse is not supported for this model"}}`)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(rawAudio)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "hi", out, "en"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	if len(formats) != 2 || formats[0] != "sse" || formats[1] != "audio" {
		t.Fatalf("stream_format attempts = %v, want [sse audio]", formats)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !bytes.Equal(data, rawAudio) {
		t.Fatalf("file = %q, want %q", data, rawAudio)
	}
}

// A compatible server may ignore stream_format=sse and answer with a raw audio
// body anyway. The Content-Type is authoritative, so no retry should occur and
// the body must still land in the file.
func TestOpenAITTS_ServerIgnoresStreamFormat(t *testing.T) {
	rawAudio := mp3("plain-audio")
	requests := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(rawAudio)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "hi", out, "en"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if requests != 1 {
		t.Fatalf("made %d requests, want 1 (Content-Type already said audio)", requests)
	}
	data, _ := os.ReadFile(out)
	if !bytes.Equal(data, rawAudio) {
		t.Fatalf("file = %q, want %q", data, rawAudio)
	}
}

func TestOpenAITTS_ErrorStatusIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Non-400 so the SSE->audio fallback does not kick in.
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "hi", out, "en")
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("error %q should carry the status and the upstream message", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("a failed synthesis must not leave an output file behind")
	}
}

// An empty 200 body is a failure, not a silent success: the caller would
// otherwise cache a zero-byte file and playback would fail with no explanation.
func TestOpenAITTS_EmptyBodyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "hi", out, "en")
	if err == nil {
		t.Fatal("expected an error for an empty audio body")
	}
	if !strings.Contains(err.Error(), "no audio") {
		t.Fatalf("error = %q, want it to mention the missing audio", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("the empty output file should have been removed")
	}
}

// Events that carry no audio — `speech.audio.done`, an audio-less delta, and
// the `[DONE]` sentinel — must be skipped, and a `:` comment keepalive must not
// be mistaken for data. None of these may push an empty frame onto the chunk
// channel, which would surface as a zero-length WebSocket binary frame.
func TestOpenAITTS_SkipsAudioLessSSEFrames(t *testing.T) {
	good := mp3("good-audio")

	var body strings.Builder
	body.WriteString(": keepalive comment\n\n")
	body.WriteString("data: {\"type\":\"speech.audio.delta\"}\n\n")                // no audio field
	body.WriteString("data: {\"type\":\"speech.audio.delta\",\"audio\":\"\"}\n\n") // empty audio
	body.WriteString("data: [DONE]\n\n")
	body.WriteString(sseEvent(t, good))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body.String())
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	ch := make(chan []byte, 8)
	if err := p.SynthesizeStream(context.Background(), "hi", out, "en", ch); err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}
	data, _ := os.ReadFile(out)
	if !bytes.Equal(data, good) {
		t.Fatalf("file = %q, want %q", data, good)
	}

	close(ch)
	var got [][]byte
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 1 {
		t.Fatalf("got %d chunks, want exactly 1 (audio-less events must not be forwarded): %q", len(got), got)
	}
	if !bytes.Equal(got[0], good) {
		t.Fatalf("chunk = %q, want %q", got[0], good)
	}
}

// A non-standard `speech.audio.delta` payload must fail fast. The spec types
// `audio` as a base64 STRING; a server that nests it as an object
// ({"audio":{"data":"…"}}) is non-compliant, and silently discarding the line
// turns a clear format error into a 0-byte file plus a generic "no audio"
// message. Fail loudly, and name the malformed payload.
func TestOpenAITTS_NonStandardDeltaFailsFast(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"nested audio object", `{"type":"speech.audio.delta","audio":{"data":"QUJD"}}`},
		{"not json", `not-json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: "+tc.payload+"\n\n")
			}))
			defer srv.Close()

			p := newTestProvider(t, srv)
			out := filepath.Join(t.TempDir(), "out.mp3")
			err := p.Synthesize(context.Background(), "hi", out, "en")
			if err == nil {
				t.Fatal("expected a hard error for a non-standard delta payload")
			}
			// The error must flag the malformed line, not masquerade as the
			// generic no-audio failure that hides the real cause.
			if !strings.Contains(err.Error(), "malformed") {
				t.Fatalf("error = %q, want it to flag the malformed SSE payload", err)
			}
			if strings.Contains(err.Error(), "no audio") {
				t.Fatalf("error = %q, must not degrade into the generic no-audio failure", err)
			}
			if _, statErr := os.Stat(out); statErr == nil {
				t.Fatal("a failed synthesis must not leave an output file behind")
			}
		})
	}
}

// The stream_format sent first must follow the spec's per-model capability.
// tts-1 / tts-1-hd explicitly do NOT support stream_format=sse, so requesting
// it is a spec violation — and a server that answers 200 + SSE anyway defeats
// the 400-based fallback, stranding the client on a stream it cannot parse.
func TestOpenAITTS_ModelSelectsInitialStreamFormat(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"gpt-4o-mini-tts", "sse"},
		{"tts-1", "audio"},
		{"tts-1-hd", "audio"},
		{"tts-1-1106", "audio"},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			var formats []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				formats = append(formats, body["stream_format"].(string))
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = w.Write(mp3("audio"))
			}))
			defer srv.Close()

			p := newTestProvider(t, srv)
			p.Model = tc.model
			out := filepath.Join(t.TempDir(), "out.mp3")
			if err := p.Synthesize(context.Background(), "hi", out, "en"); err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			if len(formats) != 1 || formats[0] != tc.want {
				t.Fatalf("stream_format attempts = %v, want exactly [%s]", formats, tc.want)
			}
		})
	}
}

func TestModelSupportsSSE(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"gpt-4o-mini-tts", true},
		{"tts-1", false},
		{"tts-1-hd", false},
		{"tts-1-1106", false},
		{"tts-1-hd-1106", false},
		{"some-custom-model", true},
	}
	for _, tc := range cases {
		if got := modelSupportsSSE(tc.model); got != tc.want {
			t.Errorf("modelSupportsSSE(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

// The request asks for response_format=mp3. A non-compliant server that
// ignores it and returns WAV (or another format) must fail fast and name what
// it actually sent — otherwise the bytes are cached as a .mp3, served as
// audio/mpeg, and playback fails with no explanation ("silent" again, just
// with a non-empty file this time).
func TestOpenAITTS_NonMP3ResponseFailsFast(t *testing.T) {
	wav := append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 32)...)
	ogg := append([]byte("OggS"), make([]byte, 32)...)
	unknown := []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77}

	cases := []struct {
		name       string
		body       []byte
		ct         string
		wantPhrase string
	}{
		{"wav body", wav, "audio/mpeg", "WAV"},
		{"ogg body", ogg, "audio/mpeg", "Ogg"},
		{"unrecognized body", unknown, "audio/mpeg", "expected mp3"},
		{"json error body", []byte(`{"error":{"message":"boom"}}`), "audio/mpeg", "expected mp3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.ct)
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()

			p := newTestProvider(t, srv)
			out := filepath.Join(t.TempDir(), "out.mp3")
			err := p.Synthesize(context.Background(), "hi", out, "en")
			if err == nil {
				t.Fatal("expected a hard error for a non-mp3 response")
			}
			if !strings.Contains(err.Error(), tc.wantPhrase) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.wantPhrase)
			}
			if _, statErr := os.Stat(out); statErr == nil {
				t.Fatal("a failed synthesis must not leave an output file behind")
			}
		})
	}
}

// Both legitimate MP3 framings must pass the sniffer: an ID3v2 tag ("ID3") or
// a bare MPEG frame sync (0xFF 0xEx/0xFx).
func TestOpenAITTS_AcceptsMP3Framings(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"id3v2 tag", append([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), make([]byte, 16)...)},
		{"frame sync", mp3("body")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = w.Write(tc.body)
			}))
			defer srv.Close()

			p := newTestProvider(t, srv)
			out := filepath.Join(t.TempDir(), "out.mp3")
			if err := p.Synthesize(context.Background(), "hi", out, "en"); err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			data, _ := os.ReadFile(out)
			if !bytes.Equal(data, tc.body) {
				t.Fatalf("file = %x, want %x", data, tc.body)
			}
		})
	}
}

// A non-mp3 payload arriving over SSE (the reported "headerless PCM" case)
// must be caught by the same sniffer.
func TestOpenAITTS_NonMP3OverSSEFailsFast(t *testing.T) {
	pcm := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sseEvent(t, pcm))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(context.Background(), "hi", out, "en")
	if err == nil {
		t.Fatal("expected a hard error for non-mp3 audio over SSE")
	}
	if !strings.Contains(err.Error(), "expected mp3") {
		t.Fatalf("error = %q, want it to flag the missing mp3 header", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Fatal("a failed synthesis must not leave an output file behind")
	}
}

func TestOpenAITTS_SpeedOmittedWhenUnset(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(mp3("x"))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.Synthesize(context.Background(), "hi", out, "en"); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if _, present := gotBody["speed"]; present {
		t.Fatalf("speed should be omitted when unset, body = %v", gotBody)
	}

	p.Speed = 1.5
	if err := p.Synthesize(context.Background(), "hi", out, "en"); err != nil {
		t.Fatalf("Synthesize with speed: %v", err)
	}
	if gotBody["speed"] != 1.5 {
		t.Fatalf("speed = %v, want 1.5", gotBody["speed"])
	}
}

// CacheIdentity must distinguish configurations that change the audio, and
// must not leak the API key (it is hashed into an on-disk file name).
func TestOpenAITTS_CacheIdentity(t *testing.T) {
	base := &OpenAITTSProvider{BaseURL: "https://api.openai.com/v1", Model: "m", Voice: "alloy", Speed: 1.0, APIKey: "sk-secret"}

	otherVoice := *base
	otherVoice.Voice = "verse"
	otherModel := *base
	otherModel.Model = "tts-1"
	otherHost := *base
	otherHost.BaseURL = "https://other.example/v1"
	otherKey := *base
	otherKey.APIKey = "sk-different"
	// Speed is sent in the request body, so it changes the audio.
	otherSpeed := *base
	otherSpeed.Speed = 1.5

	if base.CacheIdentity() == otherVoice.CacheIdentity() {
		t.Error("voice change must change the cache identity")
	}
	if base.CacheIdentity() == otherModel.CacheIdentity() {
		t.Error("model change must change the cache identity")
	}
	if base.CacheIdentity() == otherHost.CacheIdentity() {
		t.Error("endpoint change must change the cache identity")
	}
	if base.CacheIdentity() == otherSpeed.CacheIdentity() {
		t.Error("speed change must change the cache identity")
	}
	if base.CacheIdentity() != otherKey.CacheIdentity() {
		t.Error("the API key does not affect the audio and must not change the identity")
	}
	if strings.Contains(base.CacheIdentity(), "sk-secret") {
		t.Fatal("the API key must not appear in the cache identity")
	}
}

// The chunk channel is optional: a nil channel means "write the file only",
// which is how Synthesize delegates to SynthesizeStream.
func TestOpenAITTS_NilChunkChannelWritesFileOnly(t *testing.T) {
	audio := mp3("file-only-audio")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(audio)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	out := filepath.Join(t.TempDir(), "out.mp3")
	if err := p.SynthesizeStream(context.Background(), "hi", out, "en", nil); err != nil {
		t.Fatalf("SynthesizeStream: %v", err)
	}
	data, _ := os.ReadFile(out)
	if !bytes.Equal(data, audio) {
		t.Fatalf("file = %q, want %q", data, audio)
	}
}

func TestOpenAITTS_ContextCancelStopsRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	p := newTestProvider(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := filepath.Join(t.TempDir(), "out.mp3")
	err := p.Synthesize(ctx, "hi", out, "en")
	if err == nil {
		t.Fatal("expected an error from a canceled context")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("error = %q, want a wrapped request failure", err)
	}
}

// The compile-time assertion is the real guard; this keeps it visible in the
// test report and fails loudly if the interface method is renamed.
func TestOpenAITTS_ImplementsStreamingAndIdentity(t *testing.T) {
	var p any = NewOpenAITTSProvider()
	if _, ok := p.(StreamingSpeechProvider); !ok {
		t.Error("OpenAITTSProvider must implement StreamingSpeechProvider")
	}
	if _, ok := p.(CacheIdentityProvider); !ok {
		t.Error("OpenAITTSProvider must implement CacheIdentityProvider")
	}
	if fmt.Sprintf("%T", p) != "*speech.OpenAITTSProvider" {
		t.Errorf("unexpected provider type %T", p)
	}
}
