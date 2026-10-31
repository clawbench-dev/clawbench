package stt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// mustCompileTimeAssert ensures DoubaoProvider can stream.
var _ StreamingSTTProvider = (*DoubaoProvider)(nil)

// readClientFrame decodes a frame the client sent, including its sequence.
func readClientFrame(t *testing.T, data []byte) (msgType, flags byte, seq int32, payload []byte) {
	var err error
	t.Helper()
	headerSize := int(data[0]&0x0F) * 4
	msgType = data[1] >> 4
	flags = data[1] & 0x0F
	rest := data[headerSize:]
	if flags&(volcFlagPosSequence|volcFlagNegSequence) != 0 {
		seq = int32(binary.BigEndian.Uint32(rest[:4]))
		rest = rest[4:]
	}
	size := int(binary.BigEndian.Uint32(rest[:4]))
	payload = rest[4 : 4+size]
	if data[2]&0x0F == volcCompressionGzip && len(payload) > 0 {
		payload, err = gzipReader(payload)
		if err != nil {
			t.Fatalf("gunzip client payload: %v", err)
		}
	}
	return msgType, flags, seq, payload
}

// sendServerFrame writes one frame from the fake server.
func sendServerFrame(t *testing.T, conn *websocket.Conn, msgType, flags byte, seq int32, body string) {
	t.Helper()
	frame, err := volcEncodeFrame(msgType, flags, volcSerializationJSON, volcCompressionGzip, &seq, []byte(body))
	if err != nil {
		t.Fatalf("encode server frame: %v", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageBinary, frame); err != nil {
		t.Logf("write server frame: %v", err)
	}
}

// TestDoubaoOpenStream covers the wire protocol: request shape, sequence
// numbering, cumulative text and the final marker.
func TestDoubaoOpenStream(t *testing.T) {
	var gotInit doubaoStreamRequest
	var audioSeqs []int32
	var lastSeq int32
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
			msgType, flags, seq, payload := readClientFrame(t, data)
			switch msgType {
			case volcMsgFullClientRequest:
				if err := json.Unmarshal(payload, &gotInit); err != nil {
					t.Errorf("decode init: %v", err)
				}
				sendServerFrame(t, conn, volcMsgFullServerResponse, volcFlagNoSequence, 0,
					`{"result":{"additions":{"log_id":"log-1"}}}`)
			case volcMsgAudioOnlyRequest:
				if flags == volcFlagNegSequence {
					lastSeq = seq
					sendServerFrame(t, conn, volcMsgFullServerResponse, volcFlagPosSequence, 9,
						`{"result":{"text":"你好世界"}}`)
					return
				}
				audioSeqs = append(audioSeqs, seq)
				sendServerFrame(t, conn, volcMsgFullServerResponse, volcFlagPosSequence, seq,
					`{"result":{"text":"你好"}}`)
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
	if lastSeq >= 0 {
		t.Fatalf("last packet must carry a negative sequence, got %d", lastSeq)
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

	<-done
}

// TestDoubaoOpenStream_RequiresAPIKey covers the guard.
func TestDoubaoOpenStream_RequiresAPIKey(t *testing.T) {
	p := NewDoubaoProvider("", "")
	if _, err := p.OpenStream(context.Background(), StreamOptions{}); err == nil {
		t.Fatal("expected an error for a missing API key")
	}
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
