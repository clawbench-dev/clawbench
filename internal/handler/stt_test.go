package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sttTestProvider is a deterministic STT provider for tests.
type sttTestProvider struct {
	text    string
	errText string
	lang    string
	audio   string
}

func (f *sttTestProvider) Transcribe(_ context.Context, r io.Reader, lang string) (string, error) {
	f.lang = lang
	b, _ := io.ReadAll(r)
	f.audio = string(b)
	if f.errText != "" {
		return "", &sttTestError{msg: f.errText}
	}
	return f.text, nil
}

type sttTestError struct{ msg string }

func (e *sttTestError) Error() string { return e.msg }

// makeSTTMultipart builds a multipart body with an audio file field.
// Returns body and the form content type (for header).
func makeSTTMultipart(t *testing.T, audio string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "recording.webm")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(audio))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

// makeSTTMultipartWithLang builds a multipart body with an audio file field
// and a language field. Returns body and the form content type (for header).
func makeSTTMultipartWithLang(t *testing.T, audio, lang string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "recording.webm")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(audio))
	if err := writer.WriteField("language", lang); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

func TestSTTTranscribe_Success(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "你好世界"})

	body, ct := makeSTTMultipart(t, "AUDIO")
	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"text":"你好世界"`) {
		t.Fatalf("response body = %s, want text 你好世界", w.Body.String())
	}
}

func TestSTTTranscribe_MissingFile(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "x"})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestSTTTranscribe_ProviderError(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{errText: "boom"})

	body, ct := makeSTTMultipart(t, "AUDIO")
	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

func TestSTTTranscribe_LanguageAndAudioPassed(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	prov := &sttTestProvider{text: "ok"}
	SetSTTProvider(prov)

	body, ct := makeSTTMultipartWithLang(t, "AUDIOBYTES", "en")
	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if prov.audio != "AUDIOBYTES" {
		t.Fatalf("audio = %q, want AUDIOBYTES", prov.audio)
	}
	if prov.lang != "en" {
		t.Fatalf("lang = %q, want en", prov.lang)
	}
}

func TestSTTTranscribe_BodyTooLarge(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "x"})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "recording.webm")
	chunk := bytes.Repeat([]byte("A"), 1024*1024)
	for i := 0; i < 11; i++ {
		_, _ = part.Write(chunk)
	}
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", w.Code, w.Body.String())
	}
}

func TestSTTTranscribe_NonMultipartContentType(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "x"})

	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
