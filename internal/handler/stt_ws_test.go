package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestWSConn dials a throwaway WebSocket server and returns the client
// connection, usable as the server side of an STT WS exchange.
func newTestWSConn(t *testing.T) *websocket.Conn {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		for {
			if _, _, rerr := c.Read(ctx); rerr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// TestSTTTranscribe_MethodNotAllowed covers the requireMethod guard in STTTranscribe.
func TestSTTTranscribe_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stt/transcribe", http.NoBody)
	w := httptest.NewRecorder()
	STTTranscribe(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestSTTTranscribeWS_AcceptError covers the websocket.Accept failure path.
func TestSTTTranscribeWS_AcceptError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stt/transcribe/ws", http.NoBody)
	w := httptest.NewRecorder()
	// A plain (non-upgraded) HTTP request makes websocket.Accept fail. The
	// handler returns without panicking; Accept writes a non-200 error status.
	STTTranscribeWS(w, req)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// TestSTTTranscribeWS_ChunkMsZero covers the chunkMs<=0 fallback branch
// (chunkMs defaults to 1000 when the configured value is not positive).
func TestSTTTranscribeWS_ChunkMsZero(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "识别"})

	origChunk := model.ConfigInstance.STT.ChunkMs
	model.ConfigInstance.STT.ChunkMs = 0
	defer func() { model.ConfigInstance.STT.ChunkMs = origChunk }()

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/stt/transcribe/ws"
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte("AUDIO")))
	endCtl, _ := json.Marshal(sttWSControl{Type: sttWSEndCtl})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, endCtl))

	var gotFinal bool
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m sttWSServerMsg
		require.NoError(t, json.Unmarshal(data, &m))
		if m.Type == sttWSDone {
			gotFinal = true
			break
		}
	}
	assert.True(t, gotFinal)
}

// TestHandleSTTWSMessage_BinaryAppend verifies binary frames are appended to
// the running buffer.
func TestHandleSTTWSMessage_BinaryAppend(t *testing.T) {
	state := &sttStreamState{}
	newAudio := make(chan struct{}, 1)
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, newAudio, conn, websocket.MessageBinary, []byte("AUDIO"))
	assert.NoError(t, err)
	state.mu.Lock()
	assert.Equal(t, "AUDIO", state.buffer.String())
	state.mu.Unlock()
}

// TestHandleSTTWSMessage_SizeLimit verifies that exceeding the audio cap
// returns an error and emits an error frame.
func TestHandleSTTWSMessage_SizeLimit(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(bytes.Repeat([]byte("A"), sttWSMaxAudioBytes))
	state.mu.Unlock()
	newAudio := make(chan struct{}, 1)
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, newAudio, conn, websocket.MessageBinary, []byte("B"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "audio exceeds limit")
}

// TestHandleSTTWSMessage_EndControl verifies the "end" text control marks the
// stream done and returns the end sentinel.
func TestHandleSTTWSMessage_EndControl(t *testing.T) {
	state := &sttStreamState{}
	newAudio := make(chan struct{}, 1)
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, newAudio, conn, websocket.MessageText, []byte(`{"type":"end"}`))
	assert.True(t, errors.Is(err, errSTTWSEnd))
	state.mu.Lock()
	assert.True(t, state.done)
	state.mu.Unlock()
}

// TestHandleSTTWSMessage_NonControlText verifies a text frame that is not an
// "end" control is ignored without error.
func TestHandleSTTWSMessage_NonControlText(t *testing.T) {
	state := &sttStreamState{}
	newAudio := make(chan struct{}, 1)
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, newAudio, conn, websocket.MessageText, []byte(`{"type":"other"}`))
	assert.NoError(t, err)
	state.mu.Lock()
	assert.False(t, state.done)
	state.mu.Unlock()
}

// TestHandleSTTWSMessage_NewAudioFull covers the non-blocking default branch of
// the newAudio signal when the channel is already full.
func TestHandleSTTWSMessage_NewAudioFull(t *testing.T) {
	state := &sttStreamState{}
	newAudio := make(chan struct{}, 1)
	newAudio <- struct{}{} // pre-fill so the send falls through to default
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, newAudio, conn, websocket.MessageBinary, []byte("AUDIO"))
	assert.NoError(t, err)
	state.mu.Lock()
	assert.Equal(t, "AUDIO", state.buffer.String())
	state.mu.Unlock()
}

// TestStreamIncrementalChunk_Done covers the done-short-circuit (stop=true).
func TestStreamIncrementalChunk_Done(t *testing.T) {
	state := &sttStreamState{}
	state.buffer.Write([]byte("AUDIO"))
	state.mu.Lock()
	state.done = true
	state.mu.Unlock()
	conn := newTestWSConn(t)

	stop, abort := streamIncrementalChunk(context.Background(), state, &sttTestProvider{text: "x"}, conn, "zh")
	assert.True(t, stop)
	assert.False(t, abort)
}

// TestStreamIncrementalChunk_Empty covers the empty-segment short-circuit.
func TestStreamIncrementalChunk_Empty(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)

	stop, abort := streamIncrementalChunk(context.Background(), state, &sttTestProvider{text: "x"}, conn, "zh")
	assert.True(t, stop)
	assert.False(t, abort)
}

// TestStreamIncrementalChunk_TranscribeError covers the provider-error path
// (returns without stopping or aborting).
func TestStreamIncrementalChunk_TranscribeError(t *testing.T) {
	state := &sttStreamState{}
	state.buffer.Write([]byte("AUDIO"))
	conn := newTestWSConn(t)

	stop, abort := streamIncrementalChunk(context.Background(), state, &sttTestProvider{errText: "boom"}, conn, "zh")
	assert.False(t, stop)
	assert.False(t, abort)
}

// TestStreamIncrementalChunk_EmptyText covers the empty-transcription path.
func TestStreamIncrementalChunk_EmptyText(t *testing.T) {
	state := &sttStreamState{}
	state.buffer.Write([]byte("AUDIO"))
	conn := newTestWSConn(t)

	stop, abort := streamIncrementalChunk(context.Background(), state, &sttTestProvider{text: ""}, conn, "zh")
	assert.False(t, stop)
	assert.False(t, abort)
}

// TestStreamIncrementalChunk_WriteError covers the write-error path, which
// returns abort=true to cancel the connection.
func TestStreamIncrementalChunk_WriteError(t *testing.T) {
	state := &sttStreamState{}
	state.buffer.Write([]byte("AUDIO"))
	conn := newTestWSConn(t)
	// Closing the connection forces the subsequent write to fail.
	require.NoError(t, conn.Close(websocket.StatusNormalClosure, ""))

	stop, abort := streamIncrementalChunk(context.Background(), state, &sttTestProvider{text: "hello"}, conn, "zh")
	assert.False(t, stop)
	assert.True(t, abort)
}

// TestSTTTranscribeWS_TickerFires covers the ticker.C select branch in the
// incremental goroutine: once the buffered audio is consumed, a subsequent
// tick finds nothing to transcribe and stops the goroutine (stop path).
func TestSTTTranscribeWS_TickerFires(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "增量"})

	origChunk := model.ConfigInstance.STT.ChunkMs
	model.ConfigInstance.STT.ChunkMs = 50
	defer func() { model.ConfigInstance.STT.ChunkMs = origChunk }()

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/stt/transcribe/ws", nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte("AUDIO")))

	// The first tick (via newAudio) should produce an incremental text frame.
	readText := false
	for {
		_, data, rerr := conn.Read(ctx)
		require.NoError(t, rerr)
		var m sttWSServerMsg
		require.NoError(t, json.Unmarshal(data, &m))
		if m.Type == sttWSText {
			readText = true
			break
		}
	}
	assert.True(t, readText)

	// Wait long enough for the ticker (50ms) to fire again; the goroutine then
	// finds no new audio and stops cleanly.
	time.Sleep(300 * time.Millisecond)
	_ = conn.CloseNow()
	time.Sleep(100 * time.Millisecond)
}

// TestSTTTranscribeWS_AbortOnWriteError covers the abort branch of the
// incremental goroutine: a write failure on the WS connection cancels the
// stream. The provider returns a very large transcription whose write overruns
// the (never-read) client socket, forcing the server write to fail.
func TestSTTTranscribeWS_AbortOnWriteError(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	// 64MB payload: exceeds the client receive buffer so the server write blocks
	// and eventually fails, triggering the abort path.
	SetSTTProvider(&sttTestProvider{text: strings.Repeat("x", 64<<20)})

	origChunk := model.ConfigInstance.STT.ChunkMs
	model.ConfigInstance.STT.ChunkMs = 50
	defer func() { model.ConfigInstance.STT.ChunkMs = origChunk }()

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/stt/transcribe/ws", nil)
	require.NoError(t, err)

	// Send audio then never read from the client; the server's large incremental
	// write will block and time out, aborting the incremental goroutine.
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte("AUDIO")))

	// Give the 5s write timeout time to expire (slightly over, plus buffer).
	time.Sleep(5200 * time.Millisecond)
	_ = conn.CloseNow()
}
