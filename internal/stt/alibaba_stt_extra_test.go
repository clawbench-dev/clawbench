package stt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// errReader fails on the first read, which is how a caller hands us a broken
// upload stream.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestAlibabaTranscribeRequiresAPIKey(t *testing.T) {
	p := NewAlibabaProvider("   ")
	_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Fatalf("err = %v, want an API-key error", err)
	}
}

func TestAlibabaTranscribeReadError(t *testing.T) {
	p := NewAlibabaProvider("k")
	_, err := p.Transcribe(context.Background(), errReader{err: errors.New("disk on fire")}, "")
	if err == nil || !strings.Contains(err.Error(), "read audio") {
		t.Fatalf("err = %v, want a read-audio error", err)
	}
	if !strings.Contains(err.Error(), "disk on fire") {
		t.Errorf("err = %v, want the underlying cause kept", err)
	}
}

func TestAlibabaTranscribeRejectsUnbuildableRequest(t *testing.T) {
	// A base that cannot be parsed as a URL fails in http.NewRequest, before
	// any network call — the provider must report it rather than dial.
	p := NewAlibabaProvider("k")
	p.BaseURL = "://not-a-url"
	_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err == nil || !strings.Contains(err.Error(), "create request") {
		t.Fatalf("err = %v, want a create-request error", err)
	}
}

func TestAlibabaTranscribeFallsBackToDefaultClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("k")
	p.BaseURL = srv.URL
	p.HTTPClient = nil // exercises the built-in client branch
	p.FileModel = "qwen3-asr-flash"

	text, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "ok" {
		t.Fatalf("text = %q, want ok", text)
	}
}

func TestAlibabaTranscribeTransportError(t *testing.T) {
	p := NewAlibabaProvider("k")
	p.BaseURL = "http://127.0.0.1:1" // nothing listens here
	_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Fatalf("err = %v, want a request-failed error", err)
	}
}

func TestAlibabaTranscribeMalformedResponses(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"not json", `{"choices":`, "decode response"},
		{"no choices", `{"choices":[]}`, "no choices"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			p := NewAlibabaProvider("k")
			p.BaseURL = srv.URL
			p.HTTPClient = srv.Client()
			p.FileModel = "qwen3-asr-flash"
			_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestAlibabaTranscribeTruncatedBodyIsAnError(t *testing.T) {
	// A Content-Length longer than the bytes actually written makes the client
	// fail while reading the body; that must not be reported as a transcript.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"choices":`))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("k")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()
	p.FileModel = "qwen3-asr-flash"
	_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("err = %v, want a read-response error", err)
	}
}

func TestAudioMIMEType(t *testing.T) {
	cases := map[string]string{
		"wav":  "audio/wav",
		"mp3":  "audio/mpeg",
		"ogg":  "audio/ogg",
		"webm": "audio/webm",
		"flac": "application/octet-stream",
	}
	for format, want := range cases {
		if got := audioMIMEType(format); got != want {
			t.Errorf("audioMIMEType(%q) = %q, want %q", format, got, want)
		}
	}
}

func TestAlibabaOpenStreamDialFailure(t *testing.T) {
	p := NewAlibabaProvider("k")
	p.BaseURL = "http://127.0.0.1:1"
	_, err := p.OpenStream(context.Background(), StreamOptions{})
	if err == nil || !strings.Contains(err.Error(), "stream dial") {
		t.Fatalf("err = %v, want a dial error", err)
	}
}

func TestAlibabaOpenStreamDefaultsEmptyStreamModel(t *testing.T) {
	var gotRun alibabaTaskRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()
		if _, data, err := conn.Read(ctx); err == nil {
			if err := json.Unmarshal(data, &gotRun); err != nil {
				t.Errorf("decode run-task: %v", err)
			}
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"header":{"event":"task-started"},"payload":{}}`))
		_, _, _ = conn.Read(ctx)
	}))
	defer srv.Close()

	p := &AlibabaProvider{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()}
	session, err := p.OpenStream(context.Background(), StreamOptions{})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	session.Close()

	if gotRun.Payload.Model != AlibabaDefaultStreamModel {
		t.Fatalf("model = %q, want the default %q", gotRun.Payload.Model, AlibabaDefaultStreamModel)
	}
}

func TestAlibabaWaitStartedHandshakeFailures(t *testing.T) {
	// Malformed frames are skipped (the engine may push keep-alives we do not
	// decode), then the real event decides the outcome.
	conn := &fakeConn{frames: [][]byte{
		[]byte(`not json`),
		[]byte(`{"header":{"event":"task-started"}}`),
	}}
	if err := alibabaWaitStarted(context.Background(), conn); err != nil {
		t.Fatalf("alibabaWaitStarted = %v, want nil", err)
	}

	// Running out of frames means the socket died before the task started.
	if err := alibabaWaitStarted(context.Background(), &fakeConn{}); err == nil || !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("err = %v, want a handshake error", err)
	}
}

func TestAlibabaEventErrorWithoutReason(t *testing.T) {
	err := alibabaEventError(&alibabaServerEvent{})
	if err == nil || !strings.Contains(err.Error(), "without a reason") {
		t.Fatalf("err = %v, want a without-a-reason error", err)
	}
}

func TestAlibabaSessionWriteAndFinishGuards(t *testing.T) {
	conn := &fakeConn{frames: [][]byte{[]byte(`{"header":{"event":"task-started"}}`)}}
	session := &alibabaStreamSession{
		conn:    conn,
		results: make(chan StreamResult, 1),
		done:    make(chan struct{}),
		ctx:     context.Background(),
		cancel:  func() {},
		taskID:  "task-1",
	}

	// Empty audio is a no-op, not a frame.
	if err := session.Write(nil); err != nil {
		t.Fatalf("Write(nil) = %v, want nil", err)
	}
	if len(conn.writes) != 0 {
		t.Fatalf("writes = %d, want 0 after an empty Write", len(conn.writes))
	}

	if err := session.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatalf("second Finish = %v, want nil", err)
	}
	if len(conn.writes) != 1 {
		t.Fatalf("writes = %d, want exactly one finish instruction", len(conn.writes))
	}

	if err := session.Write([]byte{0x01}); err == nil || !strings.Contains(err.Error(), "already finished") {
		t.Fatalf("Write after Finish = %v, want an already-finished error", err)
	}
}

func TestAlibabaSessionCloseIsIdempotent(t *testing.T) {
	conn := &fakeConn{}
	session := &alibabaStreamSession{
		conn:    conn,
		results: make(chan StreamResult, 1),
		done:    make(chan struct{}),
		ctx:     context.Background(),
		cancel:  func() {},
	}
	session.Close()
	session.Close()
	if conn.closes != 1 {
		t.Fatalf("closes = %d, want 1 (second Close is a no-op)", conn.closes)
	}
}

func TestAlibabaReadLoopReportsEngineFailure(t *testing.T) {
	conn := &fakeConn{frames: [][]byte{
		[]byte(`{"header":{"event":"task-failed","error_code":"QUOTA"}}`),
	}}
	session := &alibabaStreamSession{
		conn:    conn,
		results: make(chan StreamResult, 4),
		done:    make(chan struct{}),
		ctx:     context.Background(),
		cancel:  func() {},
	}
	session.readLoop(context.Background())

	var results []StreamResult
	for res := range session.results {
		results = append(results, res)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one error result", results)
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "QUOTA") {
		t.Fatalf("err = %v, want the engine's code", results[0].Err)
	}
	if results[0].Final {
		t.Error("error result marked Final, want false")
	}
}

// TestAlibabaTranscribe_MalformedResponse: a body that is not the envelope must
// be reported as such, not treated as an empty transcript.
func TestAlibabaTranscribe_MalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>gateway error</html>"))
	}))
	defer srv.Close()

	p := NewAlibabaProvider("k")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	_, err := p.Transcribe(context.Background(), strings.NewReader(string(wavProbe())), "")
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("err = %v, want a decode error", err)
	}
}

// TestAlibabaOpenStream_DialFailure covers the dial branch: a closed port must
// surface as a dial error naming the stream, not a hang.
func TestAlibabaOpenStream_DialFailure(t *testing.T) {
	p := NewAlibabaProvider("k")
	p.BaseURL = "http://127.0.0.1:1"
	_, err := p.OpenStream(context.Background(), StreamOptions{})
	if err == nil || !strings.Contains(err.Error(), "alibaba stream dial") {
		t.Fatalf("err = %v, want a dial failure", err)
	}
}

// TestAlibabaOpenStream_UnreadableHandshake: a server that accepts the socket
// but never answers the task-started handshake must fail within the handshake
// window, not hang the recording.
func TestAlibabaOpenStream_UnreadableHandshake(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_, _, _ = conn.Read(context.Background())
		// Answer nothing: the handshake wait must time out on its own.
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	p := NewAlibabaProvider("k")
	p.BaseURL = srv.URL
	p.HTTPClient = srv.Client()

	_, err := p.OpenStream(context.Background(), StreamOptions{})
	if err == nil || !strings.Contains(err.Error(), "handshake") {
		t.Fatalf("err = %v, want a handshake failure", err)
	}
}
