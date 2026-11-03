package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustCompileTimeAssert ensures DoubaoProvider implements STTProvider.
var _ STTProvider = (*DoubaoProvider)(nil)

// doubaoMP3 is a minimal payload carrying the ID3 magic bytes so format
// detection reports "mp3".
var doubaoMP3 = append([]byte("ID3"), []byte("payload")...)

// newDoubao points a provider at a test server.
func newDoubao(baseURL string) *DoubaoProvider {
	return &DoubaoProvider{
		APIKey:     "test-key",
		ResourceID: "volc.seedasr.sauc.duration",
		BaseURL:    baseURL,
		HTTPClient: &http.Client{},
		EnableITN:  true,
		EnablePunc: true,
	}
}

func TestDoubaoTranscribe_Success(t *testing.T) {
	var gotPath, gotKey, gotResource, gotRequestID, gotSequence string
	var gotBody doubaoRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Api-Key")
		gotResource = r.Header.Get("X-Api-Resource-Id")
		gotRequestID = r.Header.Get("X-Api-Request-Id")
		gotSequence = r.Header.Get("X-Api-Sequence")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		// Real success shape: no header at all, only audio_info + result.
		_, _ = w.Write([]byte(`{"audio_info":{"duration":900},"result":{"text":"你好世界"}}`))
	}))
	defer srv.Close()

	text, err := newDoubao(srv.URL).Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "你好世界" {
		t.Fatalf("text = %q, want 你好世界", text)
	}

	if gotPath != doubaoFlashPath {
		t.Fatalf("path = %q, want %q", gotPath, doubaoFlashPath)
	}
	if gotKey != "test-key" {
		t.Fatalf("X-Api-Key = %q, want test-key", gotKey)
	}
	if gotResource != "volc.seedasr.sauc.duration" {
		t.Fatalf("X-Api-Resource-Id = %q", gotResource)
	}
	if gotSequence != "-1" {
		t.Fatalf("X-Api-Sequence = %q, want -1", gotSequence)
	}
	if gotRequestID == "" {
		t.Fatal("X-Api-Request-Id must be set (the API rejects requests without it)")
	}

	// The API takes base64 audio plus an explicit container format.
	if gotBody.Audio.Format != "mp3" {
		t.Fatalf("audio.format = %q, want mp3", gotBody.Audio.Format)
	}
	decoded, err := base64.StdEncoding.DecodeString(gotBody.Audio.Data)
	if err != nil {
		t.Fatalf("audio.data is not valid base64: %v", err)
	}
	if !bytes.Equal(decoded, doubaoMP3) {
		t.Fatalf("audio.data round-trip mismatch: %q", decoded)
	}
	if gotBody.Request.ModelName != "bigmodel" {
		t.Fatalf("request.model_name = %q, want bigmodel", gotBody.Request.ModelName)
	}
	if !gotBody.Request.EnableITN || !gotBody.Request.EnablePunc {
		t.Fatal("expected enable_itn and enable_punc to be true")
	}
}

// TestDoubaoTranscribe_BodyErrorIsSurfaced covers the API's habit of answering
// HTTP 200 with a non-success code in the body — the status code alone says
// nothing about whether the key or resource is valid.
func TestDoubaoTranscribe_BodyErrorIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":45000010,"message":"Invalid X-Api-Key"}}`))
	}))
	defer srv.Close()

	_, err := newDoubao(srv.URL).Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err == nil {
		t.Fatal("expected an error for a non-success body code")
	}
	if !strings.Contains(err.Error(), "45000010") || !strings.Contains(err.Error(), "Invalid X-Api-Key") {
		t.Fatalf("error should carry the code and message, got %v", err)
	}
}

func TestDoubaoTranscribe_EmptyAudio(t *testing.T) {
	_, err := newDoubao("http://127.0.0.1:1").Transcribe(context.Background(), strings.NewReader(""), "zh")
	if err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Fatalf("want empty-audio error, got %v", err)
	}
}

func TestDoubaoTranscribe_UnsupportedFormat(t *testing.T) {
	_, err := newDoubao("http://127.0.0.1:1").Transcribe(context.Background(), strings.NewReader("PLAINTEXT"), "zh")
	if err == nil || !strings.Contains(err.Error(), "unsupported audio format") {
		t.Fatalf("want unsupported-format error, got %v", err)
	}
}

func TestDoubaoTranscribe_ReadError(t *testing.T) {
	_, err := newDoubao("http://127.0.0.1:1").Transcribe(context.Background(), errorReader{}, "zh")
	if err == nil || !strings.Contains(err.Error(), "read audio") {
		t.Fatalf("want read error, got %v", err)
	}
}

func TestNewDoubaoProvider_Defaults(t *testing.T) {
	p := NewDoubaoProvider("k", "")
	if p.BaseURL != DoubaoBaseURL {
		t.Fatalf("BaseURL = %q, want %q", p.BaseURL, DoubaoBaseURL)
	}
	if p.ResourceID != "" {
		t.Fatalf("ResourceID should stay empty so Transcribe applies the default, got %q", p.ResourceID)
	}

	// Empty resource falls back to the duration-billed one at call time.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Resource-Id"); got != DoubaoDefaultResourceID {
			t.Errorf("X-Api-Resource-Id = %q, want %q", got, DoubaoDefaultResourceID)
		}
		_, _ = w.Write([]byte(`{"header":{"code":20000000},"result":{"text":"ok"}}`))
	}))
	defer srv.Close()

	p.BaseURL = srv.URL
	if _, err := p.Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), ""); err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
}

func TestDetectAudioFormat(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"wav", []byte("RIFF....WAVE"), "wav"},
		{"ogg", []byte("OggS\x00\x02"), "ogg"},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01}, "webm"},
		{"mp3-id3", []byte("ID3\x04\x00"), "mp3"},
		{"mp3-frame", []byte{0xFF, 0xFB, 0x90, 0x00}, "mp3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := detectAudioFormat(c.in)
			if err != nil {
				t.Fatalf("detectAudioFormat: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}

	if _, err := detectAudioFormat([]byte("RI")); err == nil {
		t.Fatal("expected an error for a too-short header")
	}
}

// TestDoubaoTranscribe_HTTPStatusError covers a non-200 response whose body is
// not the API's JSON envelope (e.g. a proxy error page).
func TestDoubaoTranscribe_HTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>bad gateway</html>")
	}))
	defer srv.Close()

	_, err := newDoubao(srv.URL).Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err == nil {
		t.Fatal("expected an error for a non-JSON 502 body")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Fatalf("error should mention the status, got %v", err)
	}
}

// TestDoubaoTranscribe_HeaderlessSuccess pins the real success shape (no
// "header" key) — treating a missing header as an error rejected every
// successful transcription.
func TestDoubaoTranscribe_HeaderlessSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"audio_info":{"duration":900},"result":{"text":"你好"}}`))
	}))
	defer srv.Close()

	text, err := newDoubao(srv.URL).Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "你好" {
		t.Fatalf("text = %q, want 你好", text)
	}
}

// TestDoubaoTranscribe_ExplicitSuccessCode covers a body that does carry a
// success code — accepted as well.
func TestDoubaoTranscribe_ExplicitSuccessCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":20000000,"message":"OK"},"result":{"text":"好"}}`))
	}))
	defer srv.Close()

	text, err := newDoubao(srv.URL).Transcribe(context.Background(), strings.NewReader(string(doubaoMP3)), "zh")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "好" {
		t.Fatalf("text = %q, want 好", text)
	}
}
