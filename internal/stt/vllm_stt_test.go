package stt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustCompileTimeAssert ensures VLLMProvider implements STTProvider.
var _ STTProvider = (*VLLMProvider)(nil)

func newVLLM(baseURL, model string) *VLLMProvider {
	return &VLLMProvider{
		BaseURL:    baseURL,
		Model:      model,
		Language:   "zh",
		HTTPClient: &http.Client{},
	}
}

func TestVLLMTranscribe_Success(t *testing.T) {
	var gotPath string
	var gotContentType string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		_ = r.ParseMultipartForm(1 << 20)
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("missing file field: %v", err)
		}
		defer file.Close()
		var buf strings.Builder
		b := make([]byte, 32)
		for {
			n, err := file.Read(b)
			buf.Write(b[:n])
			if err != nil {
				break
			}
		}
		gotBody = []byte(buf.String())
		if got := r.FormValue("model"); got != "whisper-model" {
			t.Fatalf("model field = %q, want whisper-model", got)
		}
		if got := r.FormValue("language"); got != "zh" {
			t.Fatalf("language field = %q, want zh", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好世界"}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "whisper-model")
	text, err := p.Transcribe(context.Background(), strings.NewReader("AUDIOBYTES"), "zh")
	if err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if text != "你好世界" {
		t.Fatalf("text = %q, want 你好世界", text)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Fatalf("content-type = %q, want multipart/form-data", gotContentType)
	}
	if string(gotBody) != "AUDIOBYTES" {
		t.Fatalf("audio body = %q, want AUDIOBYTES", string(gotBody))
	}
}

func TestVLLMTranscribe_BaseURLWithV1(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL+"/v1", "m")
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", gotPath)
	}
}

func TestVLLMTranscribe_APIKeySent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	p.APIKey = "secret-key"
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want Bearer secret-key", gotAuth)
	}
}

func TestVLLMTranscribe_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestVLLMTranscribe_EmptyText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	text, err := p.Transcribe(context.Background(), strings.NewReader("x"), "")
	if err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}
