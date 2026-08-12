package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// sttTestProvider is a deterministic STT provider for tests.
type sttTestProvider struct {
	mu      sync.Mutex
	text    string
	errText string
	lang    string
	audio   string
}

func (f *sttTestProvider) Transcribe(_ context.Context, r io.Reader, lang string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	for range 11 {
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

func TestSTTTranscribeWS_IncrementalAndFinal(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "识别文本"})

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/stt/transcribe/ws"
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageBinary, []byte("AUDIO1")); err != nil {
		t.Fatalf("write audio1: %v", err)
	}

	endCtl, _ := json.Marshal(sttWSControl{Type: sttWSEndCtl})
	if err := conn.Write(ctx, websocket.MessageText, endCtl); err != nil {
		t.Fatalf("write end: %v", err)
	}

	var finalText string
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m sttWSServerMsg
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if m.Type == sttWSDone {
			finalText = m.Final
			break
		}
	}

	if finalText != "识别文本" {
		t.Fatalf("final = %q, want 识别文本", finalText)
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

func TestSTTTranscribeWS_AudioOverLimit(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "x"})

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/stt/transcribe/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	chunk := bytes.Repeat([]byte("A"), 1<<20) // 1MB frames
	sent := 0
	for sent <= sttWSMaxAudioBytes {
		if err := conn.Write(ctx, websocket.MessageBinary, chunk); err != nil {
			break // server closed the connection on cap exceeded
		}
		sent += len(chunk)
	}

	// The server must either send an error frame or close the connection once
	// the cap is exceeded — it must NOT silently keep buffering forever.
	var sawErrorOrClose bool
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			sawErrorOrClose = true
			break
		}
		var m sttWSServerMsg
		if json.Unmarshal(data, &m) == nil && m.Type == sttWSError {
			sawErrorOrClose = true
			break
		}
	}
	if !sawErrorOrClose {
		t.Fatalf("expected server error/close after exceeding audio cap, got none")
	}
}
