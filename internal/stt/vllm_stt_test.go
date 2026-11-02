package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, fmt.Errorf("read boom") }

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
		body, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read file field: %v", err)
		}
		gotBody = body
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

// A non-2xx answer must be an *APIError so callers can tell an auth failure
// from a missing model without matching on the message text. The message shape
// is kept for logs.
func TestVLLMTranscribe_Non200IsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	_, err := p.Transcribe(context.Background(), strings.NewReader("x"), "")

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("Status = %d, want 401", apiErr.Status)
	}
	if !strings.Contains(apiErr.Body, "bad key") {
		t.Fatalf("Body = %q, want the server body", apiErr.Body)
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("Error() = %q, want it to name the status", err.Error())
	}
}

// The multipart filename defaults to the container the frontend records, and a
// caller uploading another container (the mp3 connectivity probe) must be able
// to say so: some gateways pick the decoder from the extension.
func TestVLLMTranscribe_Filename(t *testing.T) {
	cases := []struct {
		name string
		set  string
		want string
	}{
		{"default", "", "recording.webm"},
		{"override", "probe.mp3", "probe.mp3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotName string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, header, err := r.FormFile("file")
				if err != nil {
					t.Fatalf("missing file field: %v", err)
				}
				gotName = header.Filename
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"text":"ok"}`))
			}))
			defer srv.Close()

			p := newVLLM(srv.URL, "m")
			p.Filename = tc.set
			if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err != nil {
				t.Fatalf("Transcribe error: %v", err)
			}
			if gotName != tc.want {
				t.Fatalf("filename = %q, want %q", gotName, tc.want)
			}
		})
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

func TestNewVLLMProvider(t *testing.T) {
	p := NewVLLMProvider("http://x/v1/", "m", "k", "zh")
	if p.BaseURL != "http://x/v1" {
		t.Fatalf("BaseURL = %q, want http://x/v1", p.BaseURL)
	}
	if p.Model != "m" {
		t.Fatalf("Model = %q, want m", p.Model)
	}
	if p.APIKey != "k" {
		t.Fatalf("APIKey = %q, want k", p.APIKey)
	}
	if p.Language != "zh" {
		t.Fatalf("Language = %q, want zh", p.Language)
	}
	if p.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}
	if p.HTTPClient.Timeout == 0 {
		t.Fatal("HTTPClient Timeout is zero")
	}
}

func TestVLLMTranscribe_AudioReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not be sent when audio read fails")
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	if _, err := p.Transcribe(context.Background(), errorReader{}, ""); err == nil {
		t.Fatal("expected error for audio read failure, got nil")
	}
}

func TestVLLMTranscribe_RequestCreateError(t *testing.T) {
	p := newVLLM("http://exa mple.com", "m")
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestVLLMTranscribe_MissingClientUsesDefault(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	p.HTTPClient = nil
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", gotPath)
	}
}

func TestVLLMTranscribe_RequestFailed(t *testing.T) {
	p := newVLLM("http://127.0.0.1:1", "m")
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected error for failed request, got nil")
	}
}

func TestVLLMTranscribe_DecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected error for invalid response JSON, got nil")
	}
}

func TestVLLMTranscribe_LanguageOverride(t *testing.T) {
	var gotLanguage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		gotLanguage = r.FormValue("language")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	p.Language = "en"
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), "ja"); err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if gotLanguage != "ja" {
		t.Fatalf("language field = %q, want ja", gotLanguage)
	}
}
