package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeConn is a volcConn driven by a fixed list of frames. It lets the session
// tests exercise readLoop and the write guards without a socket, so the cases
// that need an exact frame sequence (malformed frame followed by a good one)
// are deterministic instead of racing a real peer.
type fakeConn struct {
	frames [][]byte
	read   int
	writes [][]byte
	closes int
}

func (c *fakeConn) Read(context.Context) (websocket.MessageType, []byte, error) {
	if c.read >= len(c.frames) {
		return 0, nil, io.EOF
	}
	frame := c.frames[c.read]
	c.read++
	return websocket.MessageBinary, frame, nil
}

func (c *fakeConn) Write(_ context.Context, _ websocket.MessageType, p []byte) error {
	c.writes = append(c.writes, append([]byte(nil), p...))
	return nil
}

func (c *fakeConn) Close(websocket.StatusCode, string) error {
	c.closes++
	return nil
}

func newFakeSession(conn volcConn, resultCap int) *doubaoStreamSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &doubaoStreamSession{
		conn:    conn,
		results: make(chan StreamResult, resultCap),
		done:    make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
		seq:     doubaoFirstAudioSequence,
	}
}

// encodeTestFrame builds a frame with no sequence number and no compression.
func encodeTestFrame(t *testing.T, msgType byte, body string) []byte {
	t.Helper()
	frame, err := volcEncodeFrame(msgType, volcFlagNoSequence, volcSerializationJSON,
		volcCompressionNone, nil, []byte(body))
	if err != nil {
		t.Fatalf("encode frame: %v", err)
	}
	return frame
}

// TestOpenStream_AppliesAudioDefaults pins the fallbacks for an empty
// StreamOptions: the engine rejects a request with a zero rate, so the defaults
// must be filled in before the frame goes out.
func TestOpenStream_AppliesAudioDefaults(t *testing.T) {
	got := make(chan doubaoStreamRequest, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()

		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		msgType, _, _, payload, err := readClientFrame(data)
		if err != nil {
			t.Errorf("decode init frame: %v", err)
			return
		}
		if msgType != volcMsgFullClientRequest {
			t.Errorf("first frame type = %d, want the full client request", msgType)
			return
		}
		var req doubaoStreamRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Errorf("decode init payload: %v", err)
			return
		}
		got <- req
		// Hold the connection open until the client closes it.
		_, _, _ = conn.Read(ctx)
	}))
	defer srv.Close()

	session, err := newDoubao(srv.URL).OpenStream(context.Background(), StreamOptions{})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer session.Close()

	select {
	case req := <-got:
		if req.Audio.Rate != 16000 {
			t.Errorf("rate = %d, want the 16000 default", req.Audio.Rate)
		}
		if req.Audio.Channel != 1 {
			t.Errorf("channel = %d, want the 1 default", req.Audio.Channel)
		}
		if req.Audio.Bits != 16 {
			t.Errorf("bits = %d, want the 16 default", req.Audio.Bits)
		}
		if req.Request.Corpus != nil {
			t.Errorf("corpus = %+v, want nil when no context was supplied", req.Request.Corpus)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server never received the initial frame")
	}
}

// TestOpenStream_DialFailure covers the failed-upgrade path. The response body
// of a refused upgrade has to be drained or the connection leaks.
func TestOpenStream_DialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no websocket here", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newDoubao(srv.URL).OpenStream(context.Background(), StreamOptions{SampleRate: 16000})
	if err == nil {
		t.Fatal("expected an error when the server refuses the upgrade")
	}
	if !strings.Contains(err.Error(), "doubao stream dial") {
		t.Fatalf("error = %q, want it to name the dial", err)
	}
}

// TestVolcDecodeFrame_RejectsMalformedFrames covers every structural way a
// server frame can be wrong. Each of these used to be reachable only as a
// silently shifted field, which turned one bad frame into a wrong transcript.
func TestVolcDecodeFrame_RejectsMalformedFrames(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "header size zero",
			data: []byte{0x10, 0x00, 0x00, 0x00},
			want: "bad header size",
		},
		{
			name: "header size past the frame",
			data: []byte{0x1F, 0x00, 0x00, 0x00},
			want: "bad header size",
		},
		{
			name: "sequence flag without a sequence",
			data: []byte{0x11, 0x01, 0x00, 0x00},
			want: "truncated sequence number",
		},
		{
			name: "no payload length",
			data: []byte{0x11, 0x00, 0x00, 0x00},
			want: "truncated payload length",
		},
		{
			name: "payload length past the frame",
			data: []byte{0x11, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09, 'x'},
			want: "payload size",
		},
		{
			name: "gzip flag on a plain payload",
			data: []byte{0x11, 0x90, 0x01, 0x00, 0x00, 0x00, 0x00, 0x03, 'a', 'b', 'c'},
			want: "gunzip payload",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := volcDecodeFrame(tc.data)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestGzipReader_RejectsNonGzip covers a truncated/garbage gzip body directly:
// the frame decoder reports it, and a caller reading a stored blob must too.
func TestGzipReader_RejectsNonGzip(t *testing.T) {
	if _, err := gzipReader([]byte("definitely not gzip")); err == nil {
		t.Fatal("expected an error for a non-gzip body")
	}
}

// TestSession_WriteGuards covers the two writes that must not reach the wire:
// an empty chunk (nothing to send) and any write after the stream ended.
func TestSession_WriteGuards(t *testing.T) {
	conn := &fakeConn{}
	s := newFakeSession(conn, 8)

	if err := s.Write(nil); err != nil {
		t.Fatalf("an empty chunk should be a no-op, got %v", err)
	}
	if len(conn.writes) != 0 {
		t.Fatalf("an empty chunk wrote %d frame(s)", len(conn.writes))
	}

	if err := s.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if len(conn.writes) != 1 {
		t.Fatalf("Finish wrote %d frame(s), want exactly the end marker", len(conn.writes))
	}

	// A write racing the end marker must be refused, not sent after it.
	if err := s.Write([]byte{1, 2, 3, 4}); err == nil {
		t.Fatal("a write after Finish should fail")
	}
	if len(conn.writes) != 1 {
		t.Fatalf("a rejected write still reached the wire: %d frames", len(conn.writes))
	}
}

// TestSession_FinishAndCloseAreIdempotent covers the shutdown paths a handler
// hits twice: both Finish and Close are called from several places.
func TestSession_FinishAndCloseAreIdempotent(t *testing.T) {
	conn := &fakeConn{}
	s := newFakeSession(conn, 8)

	if err := s.Finish(); err != nil {
		t.Fatalf("first Finish: %v", err)
	}
	if err := s.Finish(); err != nil {
		t.Fatalf("second Finish should be a no-op, got %v", err)
	}
	if len(conn.writes) != 1 {
		t.Fatalf("a repeated Finish sent a second end marker: %d frames", len(conn.writes))
	}

	s.Close()
	s.Close()
	if conn.closes != 1 {
		t.Fatalf("Close called the connection %d times, want 1", conn.closes)
	}

	// After Close nothing may be written at all, not even the end marker.
	if err := s.Write([]byte{1, 2, 3, 4}); err == nil {
		t.Fatal("a write after Close should fail")
	}
	if err := s.Finish(); err != nil {
		t.Fatalf("Finish after Close should be a no-op, got %v", err)
	}
	if len(conn.writes) != 1 {
		t.Fatalf("writes after Close reached the wire: %d frames", len(conn.writes))
	}
}

// TestReadLoop_SkipsUndecodableAndIrrelevantFrames covers the three "ignore and
// keep reading" branches: a malformed frame, a message type the client does not
// consume, and a payload that is not the expected JSON.
func TestReadLoop_SkipsUndecodableAndIrrelevantFrames(t *testing.T) {
	conn := &fakeConn{frames: [][]byte{
		{0x11}, // shorter than a header
		encodeTestFrame(t, 0x5, `{"whatever":true}`), // a type the client ignores
		encodeTestFrame(t, volcMsgFullServerResponse, `not json at all`),
		encodeTestFrame(t, volcMsgAudioServerResponse, `{"result":{"text":"甲"}}`),
	}}
	s := newFakeSession(conn, 8)

	s.readLoop(s.ctx)

	var results []StreamResult
	for r := range s.results {
		results = append(results, r)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want the transcript update and its final marker", results)
	}
	if results[0].Text != "甲" || results[0].Final {
		t.Errorf("first result = %+v", results[0])
	}
	if results[1].Text != "甲" || !results[1].Final {
		t.Errorf("final result = %+v", results[1])
	}
}

// TestReadLoop_RepeatedTextIsNotRepublished covers the dedup: the engine resends
// the cumulative transcript on every packet, so only changes are published.
func TestReadLoop_RepeatedTextIsNotRepublished(t *testing.T) {
	conn := &fakeConn{frames: [][]byte{
		encodeTestFrame(t, volcMsgFullServerResponse, `{"result":{"text":"你好"}}`),
		encodeTestFrame(t, volcMsgFullServerResponse, `{"result":{"text":"你好"}}`),
		encodeTestFrame(t, volcMsgFullServerResponse, `{"result":{"text":"你好世界"}}`),
	}}
	s := newFakeSession(conn, 8)

	s.readLoop(s.ctx)

	var texts []string
	for r := range s.results {
		texts = append(texts, r.Text)
	}
	want := []string{"你好", "你好世界", "你好世界"}
	if strings.Join(texts, "|") != strings.Join(want, "|") {
		t.Fatalf("published texts = %q, want %q", texts, want)
	}
}

// TestReadLoop_ErrorFieldInResponse covers a 200-shaped frame that carries an
// "error" field instead of a 0xF frame: it must end the session with an error,
// not with a clean final transcript.
func TestReadLoop_ErrorFieldInResponse(t *testing.T) {
	conn := &fakeConn{frames: [][]byte{
		encodeTestFrame(t, volcMsgFullServerResponse, `{"error":"quota exceeded"}`),
	}}
	s := newFakeSession(conn, 8)

	s.readLoop(s.ctx)

	var results []StreamResult
	for r := range s.results {
		results = append(results, r)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want only the error", results)
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "quota exceeded") {
		t.Fatalf("error = %v, want it to carry the engine message", results[0].Err)
	}
	if results[0].Final {
		t.Error("an errored session must not also report a final transcript")
	}
}

// TestReadLoop_StopsWhenSessionClosed covers emit's closed-session branch: once
// done is closed the loop must stop instead of blocking on a channel nobody
// reads (the caller closed the session and stopped draining).
func TestReadLoop_StopsWhenSessionClosed(t *testing.T) {
	conn := &fakeConn{frames: [][]byte{
		encodeTestFrame(t, volcMsgFullServerResponse, `{"result":{"text":"甲"}}`),
	}}
	// Unbuffered: the only way the send can succeed is a live receiver.
	s := newFakeSession(conn, 0)
	close(s.done)

	finished := make(chan struct{})
	go func() {
		s.readLoop(s.ctx)
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop blocked on a closed session")
	}
	if _, ok := <-s.results; ok {
		t.Fatal("a closed session must not deliver results")
	}
}

// TestRawJSONText covers the renderings an error frame's fields can take: the
// API sends code as a number in some error classes and as a string in others.
func TestRawJSONText(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"absent", "", ""},
		{"string", `"invalid key"`, "invalid key"},
		{"null", `null`, ""},
		{"number", `45000001`, "45000001"},
		{"object", `{"a":1}`, `{"a":1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rawJSONText(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("rawJSONText(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
