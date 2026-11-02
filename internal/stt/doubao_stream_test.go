package stt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// mustCompileTimeAssert ensures DoubaoProvider can stream.
var _ StreamingSTTProvider = (*DoubaoProvider)(nil)

// readClientFrame decodes a frame the client sent, including its sequence.
//
// It returns an error instead of failing the test itself: it runs on the fake
// server's goroutine, where t.Fatalf only calls runtime.Goexit on THAT goroutine
// — the test is marked failed, the handler disappears, and the client then
// reports an unrelated "websocket: close 1006" (or hangs until the deadline).
// The caller logs the error and returns, so the failure names the real problem.
func readClientFrame(data []byte) (msgType, flags byte, seq int32, payload []byte, err error) {
	if len(data) < 4 {
		return 0, 0, 0, nil, fmt.Errorf("frame too short for a header: %d byte(s)", len(data))
	}
	headerSize := int(data[0]&0x0F) * 4
	if headerSize < 4 || headerSize > len(data) {
		return 0, 0, 0, nil, fmt.Errorf("frame header size %d does not fit %d byte(s)", headerSize, len(data))
	}
	msgType = data[1] >> 4
	flags = data[1] & 0x0F
	rest := data[headerSize:]
	if flags&(volcFlagPosSequence|volcFlagNegSequence) != 0 {
		if len(rest) < 4 {
			return 0, 0, 0, nil, fmt.Errorf("frame claims a sequence but only has %d byte(s)", len(rest))
		}
		seq = int32(binary.BigEndian.Uint32(rest[:4]))
		rest = rest[4:]
	}
	if len(rest) < 4 {
		return 0, 0, 0, nil, fmt.Errorf("frame has no payload length: %d byte(s) left", len(rest))
	}
	size := int(binary.BigEndian.Uint32(rest[:4]))
	if size < 0 || size > len(rest)-4 {
		return 0, 0, 0, nil, fmt.Errorf("frame payload size %d does not fit %d byte(s)", size, len(rest)-4)
	}
	payload = rest[4 : 4+size]
	if data[2]&0x0F == volcCompressionGzip && len(payload) > 0 {
		payload, err = gzipReader(payload)
		if err != nil {
			return 0, 0, 0, nil, fmt.Errorf("gunzip client payload: %w", err)
		}
	}
	return msgType, flags, seq, payload, nil
}

// sendServerFrame writes one frame from the fake server. Like readClientFrame it
// returns an error rather than calling t.Fatalf on the handler goroutine; a write
// failure is only logged, because a client that has already gone away is not a
// protocol bug.
func sendServerFrame(t *testing.T, conn *websocket.Conn, msgType, flags byte, seq int32, body string) error {
	t.Helper()
	// The sequence field is only on the wire when the flags carry it; encoding
	// one for a no-sequence frame shifts the payload length by four bytes.
	var seqPtr *int32
	if flags&(volcFlagPosSequence|volcFlagNegSequence) != 0 {
		seqPtr = &seq
	}
	frame, err := volcEncodeFrame(msgType, flags, volcSerializationJSON, volcCompressionGzip, seqPtr, []byte(body))
	if err != nil {
		return fmt.Errorf("encode server frame: %w", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
		t.Logf("write server frame: %v", err)
	}
	return nil
}

// TestDoubaoOpenStream covers the wire protocol: request shape, sequence
// numbering, cumulative text and the final marker.
func TestDoubaoOpenStream(t *testing.T) {
	var gotInit doubaoStreamRequest
	var audioSeqs []int32
	var lastSeq int32
	var framesAfterEnd int
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()

		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			msgType, flags, seq, payload, err := readClientFrame(data)
			if err != nil {
				t.Errorf("decode client frame: %v", err)
				return
			}
			switch msgType {
			case volcMsgFullClientRequest:
				if err := json.Unmarshal(payload, &gotInit); err != nil {
					t.Errorf("decode init: %v", err)
				}
				if err := sendServerFrame(t, conn, volcMsgFullServerResponse, volcFlagNoSequence, 0,
					`{"result":{"additions":{"log_id":"log-1"}}}`); err != nil {
					t.Errorf("send init ack: %v", err)
					return
				}
			case volcMsgAudioOnlyRequest:
				if flags == volcFlagNegSequence {
					lastSeq = seq
					if err := sendServerFrame(t, conn, volcMsgFullServerResponse, volcFlagPosSequence, 9,
						`{"result":{"text":"你好世界"}}`); err != nil {
						t.Errorf("send final frame: %v", err)
						return
					}
					// Nothing may follow the end marker. Watch briefly so an
					// audio frame sent after Finish is caught rather than
					// silently accepted.
					watchCtx, cancelWatch := context.WithTimeout(ctx, 200*time.Millisecond)
					for {
						_, more, rerr := conn.Read(watchCtx)
						if rerr != nil {
							break
						}
						framesAfterEnd++
						_ = more
					}
					cancelWatch()
					return
				}
				audioSeqs = append(audioSeqs, seq)
				if err := sendServerFrame(t, conn, volcMsgFullServerResponse, volcFlagPosSequence, seq,
					`{"result":{"text":"你好"}}`); err != nil {
					t.Errorf("send audio ack: %v", err)
					return
				}
			}
		}
	}))
	defer srv.Close()

	p := newDoubao(srv.URL)
	p.EndWindowSize = 900

	session, err := p.OpenStream(context.Background(), StreamOptions{
		SampleRate: 16000, Channels: 1, Bits: 16, Context: "我们在讨论 Elasticsearch",
	})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer session.Close()

	if err := session.Write([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if err := session.Write([]byte{5, 6, 7, 8}); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if err := session.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	var results []StreamResult
	deadline := time.After(10 * time.Second)
	for {
		select {
		case r, ok := <-session.Results():
			if !ok {
				goto assert
			}
			results = append(results, r)
		case <-deadline:
			t.Fatal("timed out waiting for results")
		}
	}

assert:
	<-done // the server goroutine owns audioSeqs/lastSeq/framesAfterEnd
	if gotInit.Request.ModelName != "bigmodel" {
		t.Errorf("model_name = %q", gotInit.Request.ModelName)
	}
	if gotInit.Audio.Format != "pcm" || gotInit.Audio.Rate != 16000 ||
		gotInit.Audio.Bits != 16 || gotInit.Audio.Channel != 1 {
		t.Errorf("audio config = %+v", gotInit.Audio)
	}
	if gotInit.Request.ResultType != "full" {
		t.Errorf("result_type = %q, want full (deltas cannot be shown by replacement)", gotInit.Request.ResultType)
	}
	if gotInit.Request.EndWindowSize != 900 {
		t.Errorf("end_window_size = %d, want 900", gotInit.Request.EndWindowSize)
	}
	if gotInit.Request.Corpus == nil || gotInit.Request.Corpus.Context == "" {
		t.Fatal("corpus.context should carry the caller's context")
	}
	// The API takes context as a JSON *string*, not an object.
	var ctxDoc map[string]any
	if err := json.Unmarshal([]byte(gotInit.Request.Corpus.Context), &ctxDoc); err != nil {
		t.Fatalf("corpus.context is not a JSON string: %v", err)
	}
	if ctxDoc["context_type"] != "dialog_ctx" {
		t.Errorf("context_type = %v", ctxDoc["context_type"])
	}

	// Audio packets start at 2: the server assigns 1 to the init frame.
	if len(audioSeqs) != 2 || audioSeqs[0] != 2 || audioSeqs[1] != 3 {
		t.Fatalf("audio sequences = %v, want [2 3]", audioSeqs)
	}
	// The end marker is the sequence *after* the last audio packet, negated.
	// Merely being negative would also accept -(last audio seq), which reuses a
	// sequence the engine already saw.
	wantEnd := -(audioSeqs[len(audioSeqs)-1] + 1)
	if lastSeq != wantEnd {
		t.Fatalf("end sequence = %d, want %d", lastSeq, wantEnd)
	}
	if framesAfterEnd != 0 {
		t.Fatalf("%d frame(s) sent after the end marker", framesAfterEnd)
	}

	if len(results) < 2 {
		t.Fatalf("want at least 2 results, got %v", results)
	}
	if results[0].Text != "你好" || results[0].Final {
		t.Errorf("first result = %+v", results[0])
	}
	final := results[len(results)-1]
	if final.Text != "你好世界" || !final.Final {
		t.Errorf("final result = %+v", final)
	}
}

// TestDoubaoOpenStream_RequiresAPIKey covers the guard.
func TestDoubaoOpenStream_RequiresAPIKey(t *testing.T) {
	p := NewDoubaoProvider("", "")
	if _, err := p.OpenStream(context.Background(), StreamOptions{}); err == nil {
		t.Fatal("expected an error for a missing API key")
	}
}

// TestDoubaoOpenStream_ErrorFrame covers the failure path: the engine reports
// bad credentials / quota as a 0xF frame, which must surface as an error rather
// than a clean end with an empty transcript.
func TestDoubaoOpenStream_ErrorFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := context.Background()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			msgType, _, _, _, err := readClientFrame(data)
			if err != nil {
				t.Errorf("decode client frame: %v", err)
				return
			}
			switch msgType {
			case volcMsgFullClientRequest:
				if err := sendServerFrame(t, conn, volcMsgFullServerResponse, volcFlagNoSequence, 0,
					`{"result":{"additions":{"log_id":"log-1"}}}`); err != nil {
					t.Errorf("send init ack: %v", err)
					return
				}
			case volcMsgAudioOnlyRequest:
				if err := sendServerFrame(t, conn, volcMsgError, volcFlagNoSequence, 0,
					`{"error":"invalid api key","code":45000001,"message":"unauthorized"}`); err != nil {
					t.Errorf("send error frame: %v", err)
				}
				return
			}
		}
	}))
	defer srv.Close()

	p := newDoubao(srv.URL)
	session, err := p.OpenStream(context.Background(), StreamOptions{SampleRate: 16000, Channels: 1, Bits: 16})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer session.Close()

	if err := session.Write([]byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var results []StreamResult
	deadline := time.After(10 * time.Second)
	for {
		select {
		case r, ok := <-session.Results():
			if !ok {
				goto assert
			}
			results = append(results, r)
		case <-deadline:
			t.Fatal("timed out waiting for results")
		}
	}

assert:
	if len(results) == 0 {
		t.Fatal("the error frame produced no result")
	}
	last := results[len(results)-1]
	if last.Err == nil {
		t.Fatalf("error frame must surface as StreamResult.Err, got %+v", last)
	}
	if last.Final {
		t.Error("an errored session must not also report a final transcript")
	}
	for _, want := range []string{"45000001", "unauthorized", "invalid api key"} {
		if !strings.Contains(last.Err.Error(), want) {
			t.Errorf("error %q should mention %q", last.Err, want)
		}
	}
}

// TestDoubaoErrorFromPayload covers the shapes an error frame can take: the
// message must keep whatever the engine reported, including the raw body when
// it is not JSON at all.
func TestDoubaoErrorFromPayload(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "code, message and error",
			body: `{"error":"invalid api key","code":45000001,"message":"unauthorized"}`,
			want: "stt: doubao stream error: code 45000001: unauthorized: invalid api key",
		},
		{
			name: "string code",
			body: `{"code":"45000030","message":"resource not authorized"}`,
			want: "stt: doubao stream error: code 45000030: resource not authorized",
		},
		{
			name: "null code is skipped",
			body: `{"code":null,"message":"quota exceeded"}`,
			want: "stt: doubao stream error: quota exceeded",
		},
		{
			name: "undecodable body is reported verbatim",
			body: `upstream said no`,
			want: "stt: doubao stream error: upstream said no",
		},
		{
			name: "empty body still names the frame",
			body: `{}`,
			want: "stt: doubao stream error: {}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := doubaoErrorFromPayload([]byte(tc.body))
			if got == nil {
				t.Fatal("expected an error")
			}
			if got.Error() != tc.want {
				t.Errorf("error = %q, want %q", got.Error(), tc.want)
			}
		})
	}
}

// blockingConn is a volcConn whose Write blocks until the session context is
// cancelled or the test releases it.
type blockingConn struct {
	writeStarted chan struct{}
	release      chan struct{}
	closeOnce    sync.Once
	closed       chan struct{}
}

func newBlockingConn() *blockingConn {
	return &blockingConn{
		writeStarted: make(chan struct{}),
		release:      make(chan struct{}),
		closed:       make(chan struct{}),
	}
}

func (c *blockingConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	<-ctx.Done()
	return 0, nil, ctx.Err()
}

func (c *blockingConn) Write(ctx context.Context, _ websocket.MessageType, _ []byte) error {
	select {
	case <-c.writeStarted:
	default:
		close(c.writeStarted)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.release:
		return nil
	}
}

func (c *blockingConn) Close(websocket.StatusCode, string) error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

// TestDoubaoSession_CloseDoesNotWaitForStalledWrite covers shutdown: when the
// upstream never accepts a frame, Close (and therefore the handler tearing the
// WebSocket down) must not sit on the write for the full 15s timeout.
func TestDoubaoSession_CloseDoesNotWaitForStalledWrite(t *testing.T) {
	conn := newBlockingConn()
	ctx, cancel := context.WithCancel(context.Background())
	s := &doubaoStreamSession{
		conn:    conn,
		results: make(chan StreamResult, 8),
		done:    make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
		seq:     doubaoFirstAudioSequence,
	}

	writeErr := make(chan error, 1)
	go func() { writeErr <- s.Write([]byte{1, 2, 3, 4}) }()

	select {
	case <-conn.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Write never reached the connection")
	}

	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Close blocked on an in-flight write")
	}

	select {
	case err := <-writeErr:
		if err == nil {
			t.Fatal("the aborted write should report an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write did not return after Close")
	}
	<-conn.closed
}

// TestDoubaoContextPayload covers truncation and the wrapping shape.
func TestDoubaoContextPayload(t *testing.T) {
	if got := doubaoContextPayload("   "); got != "" {
		t.Fatalf("blank context should produce nothing, got %q", got)
	}

	long := make([]rune, doubaoContextRuneLimit+50)
	for i := range long {
		long[i] = '字'
	}
	var doc struct {
		ContextType string `json:"context_type"`
		ContextData []struct {
			Speaker string `json:"speaker"`
			Text    string `json:"text"`
		} `json:"context_data"`
	}
	if err := json.Unmarshal([]byte(doubaoContextPayload(string(long))), &doc); err != nil {
		t.Fatalf("context is not valid JSON: %v", err)
	}
	if len([]rune(doc.ContextData[0].Text)) != doubaoContextRuneLimit {
		t.Fatalf("context should be truncated to %d runes, got %d",
			doubaoContextRuneLimit, len([]rune(doc.ContextData[0].Text)))
	}
	if doc.ContextData[0].Speaker != "user" {
		t.Errorf("speaker = %q", doc.ContextData[0].Speaker)
	}
}

// TestVolcDecodeFrame covers both frame layouts — with and without a sequence
// number — because assuming the wrong one shifts every later field.
func TestVolcDecodeFrame(t *testing.T) {
	body := `{"result":{"text":"hi"}}`

	withSeq, err := volcEncodeFrame(volcMsgAudioServerResponse, volcFlagPosSequence,
		volcSerializationJSON, volcCompressionGzip, new(int32), []byte(body))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	msgType, payload, err := volcDecodeFrame(withSeq)
	if err != nil || msgType != volcMsgAudioServerResponse || string(payload) != body {
		t.Fatalf("decode with seq: type=%d payload=%q err=%v", msgType, payload, err)
	}

	noSeq, err := volcEncodeFrame(volcMsgFullServerResponse, volcFlagNoSequence,
		volcSerializationJSON, volcCompressionNone, nil, []byte(body))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	msgType, payload, err = volcDecodeFrame(noSeq)
	if err != nil || msgType != volcMsgFullServerResponse || string(payload) != body {
		t.Fatalf("decode without seq: type=%d payload=%q err=%v", msgType, payload, err)
	}

	if _, _, err := volcDecodeFrame([]byte{0x11}); err == nil {
		t.Fatal("expected an error for a truncated frame")
	}
}

// TestReadClientFrame_ReportsMalformedFrames pins the decoder's own error path.
// A malformed client frame used to panic on the slice bounds (or, for a bad gzip
// body, call t.Fatalf from the fake server's goroutine), which surfaced to the
// test as "websocket: close 1006" instead of naming the bad header.
func TestReadClientFrame_ReportsMalformedFrames(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"shorter than a header", []byte{0x11}, "too short"},
		{"header size past the frame", []byte{0x1F, 0x10, 0x10, 0x00}, "header size"},
		{"no payload length", []byte{0x11, 0x10, 0x10, 0x00}, "payload length"},
		{
			"payload length past the frame",
			[]byte{0x11, 0x10, 0x10, 0x00, 0x00, 0x00, 0x00, 0x09, 'x'},
			"payload size",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := readClientFrame(tc.data)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}
