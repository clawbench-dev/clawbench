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

	origChunk := model.ConfigInstance.STT.VLLM.ChunkMs
	model.ConfigInstance.STT.VLLM.ChunkMs = 0
	defer func() { model.ConfigInstance.STT.VLLM.ChunkMs = origChunk }()

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

// assertReadsText waits for one incremental text frame.
func assertReadsText(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		require.NoError(t, err)
		var m sttWSServerMsg
		require.NoError(t, json.Unmarshal(data, &m))
		if m.Type == sttWSText {
			return
		}
	}
}

// TestHandleSTTWSMessage_BinaryAppend verifies binary frames are appended to
// the running buffer, and that speech above the floor is noticed.
func TestHandleSTTWSMessage_BinaryAppend(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)

	require.NoError(t, handleSTTWSMessage(state, conn, websocket.MessageBinary, loudPCM(1)[:200]))
	state.mu.Lock()
	defer state.mu.Unlock()
	assert.Len(t, state.buffer.Bytes(), 200)
	assert.Positive(t, state.voiceBytes)
	assert.True(t, state.voiceSeen)
}

// TestHandleSTTWSMessage_SilenceIsNotSpeech verifies that a frame below the
// floor does not count as speech — the client's keepalive sends one such frame
// every couple of seconds and must not make the engine transcribe silence.
func TestHandleSTTWSMessage_SilenceIsNotSpeech(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)

	require.NoError(t, handleSTTWSMessage(state, conn, websocket.MessageBinary, make([]byte, 200)))
	state.mu.Lock()
	defer state.mu.Unlock()
	assert.Zero(t, state.voiceBytes)
	assert.False(t, state.voiceSeen)
}

// TestHandleSTTWSMessage_StartControlRecordsRate verifies the start frame's
// sample rate is kept: it labels the WAV container, and a wrong rate is not an
// error the engine reports — it is garbled audio.
func TestHandleSTTWSMessage_StartControlRecordsRate(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)

	require.NoError(t, handleSTTWSMessage(state, conn, websocket.MessageText, []byte(`{"type":"start","sampleRate":48000}`)))
	state.mu.Lock()
	defer state.mu.Unlock()
	assert.Equal(t, 48000, state.sampleRate)
}

// TestHandleSTTWSMessage_SizeLimit verifies that exceeding the audio cap
// returns an error and emits an error frame.
func TestHandleSTTWSMessage_SizeLimit(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(bytes.Repeat([]byte("A"), sttWSMaxAudioBytes))
	state.mu.Unlock()
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, conn, websocket.MessageBinary, []byte("B"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "audio exceeds limit")
}

// TestHandleSTTWSMessage_EndControl verifies the "end" text control marks the
// stream done and returns the end sentinel.
func TestHandleSTTWSMessage_EndControl(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, conn, websocket.MessageText, []byte(`{"type":"end"}`))
	assert.True(t, errors.Is(err, errSTTWSEnd))
	state.mu.Lock()
	assert.True(t, state.done)
	state.mu.Unlock()
}

// TestHandleSTTWSMessage_NonControlText verifies a text frame that is not a
// control is ignored without error.
func TestHandleSTTWSMessage_NonControlText(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)

	err := handleSTTWSMessage(state, conn, websocket.MessageText, []byte(`{"type":"other"}`))
	assert.NoError(t, err)
	state.mu.Lock()
	assert.False(t, state.done)
	state.mu.Unlock()
}

// TestTranscribeBufferedChunk_WrapsWAV is the regression test for continuous
// conversation on an OpenAI-compatible engine: the bytes handed to the provider
// must be a container it can decode. Headerless PCM uploaded as "recording.webm"
// is what made the mode recognize nothing at all.
func TestTranscribeBufferedChunk_WrapsWAV(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(3)) // 300 ms: over the minimum chunk
	state.lastVoiceAt = time.Now().Add(-2 * time.Second)
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	provider := &sttTestProvider{text: "你好"}
	conn := newTestWSConn(t)

	abort := transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 200)
	assert.False(t, abort)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Len(t, provider.audio, 9600+44)
	assert.True(t, strings.HasPrefix(provider.audio, "RIFF"), "engine must receive a WAV container")
}

// TestTranscribeBufferedChunk_WaitsForPause verifies a chunk that is still
// being spoken is not taken: cutting mid-sentence transcribes half a word, and
// this path appends text so nothing corrects it afterwards.
func TestTranscribeBufferedChunk_WaitsForPause(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(5)) // 500 ms: over the minimum, under the floor
	state.lastVoiceAt = time.Now()
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	provider := &sttTestProvider{text: "x"}
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 1000))

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Empty(t, provider.audio)
}

// TestTranscribeBufferedChunk_TooShort verifies a couple of stray frames do not
// open a chunk — the client's voice gate sends a keepalive frame while nothing
// is being said.
func TestTranscribeBufferedChunk_TooShort(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(1)) // 100 ms
	state.lastVoiceAt = time.Now().Add(-2 * time.Second)
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	provider := &sttTestProvider{text: "x"}
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 1000))

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Empty(t, provider.audio)
}

// TestTranscribeBufferedChunk_PauseEndsChunk verifies a pause closes the chunk
// well before the forced floor: this is the normal path, and it is what makes
// the transcript appear while the user is still in the conversation.
func TestTranscribeBufferedChunk_PauseEndsChunk(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(5)) // 500 ms, then the speaker stopped
	state.lastVoiceAt = time.Now().Add(-2 * time.Second)
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	provider := &sttTestProvider{text: "你好"}
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 10000))

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.NotEmpty(t, provider.audio)
}

// TestTranscribeBufferedChunk_ForcesLongChunk verifies a chunk that reaches the
// configured interval is taken even without a pause, so continuous speech still
// produces text.
func TestTranscribeBufferedChunk_ForcesLongChunk(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(15)) // 1.5 s with no pause
	state.lastVoiceAt = time.Now()
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	provider := &sttTestProvider{text: "你好"}
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 200))

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, "你好", provider.text)
	assert.NotEmpty(t, provider.audio)
}

// TestBufferedForceCutMs covers the two constraints a forced cut has to satisfy:
// long enough for the engine to have context, short enough that the client's
// pause-to-send timer (restarted by each transcript) cannot expire first.
func TestBufferedForceCutMs(t *testing.T) {
	// A tiny configured interval is raised to the floor.
	assert.Equal(t, sttBufferedMinForceMs, bufferedForceCutMs(200, 3))
	// A large one is pulled back under the client's pause-to-send interval.
	assert.Less(t, bufferedForceCutMs(10000, 3), 3000)
	// Pause-to-send disabled (0) leaves the configured value alone.
	assert.Equal(t, 10000, bufferedForceCutMs(10000, 0))
}

// TestSTTTranscribeWS_FinalTextIsWholeRecording verifies the opt-in final pass
// is a re-transcription of everything, not just the last chunk: the client
// replaces its transcript with it, so a chunk-only final would lose the earlier
// lines.
func TestSTTTranscribeWS_FinalTextIsWholeRecording(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	provider := &sttTestProvider{text: "整段"}
	SetSTTProvider(provider)

	origChunk := model.ConfigInstance.STT.VLLM.ChunkMs
	origRecheck := model.ConfigInstance.STT.VLLM.FinalRecheck
	model.ConfigInstance.STT.VLLM.ChunkMs = 200
	model.ConfigInstance.STT.VLLM.FinalRecheck = true
	defer func() {
		model.ConfigInstance.STT.VLLM.ChunkMs = origChunk
		model.ConfigInstance.STT.VLLM.FinalRecheck = origRecheck
	}()

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/stt/transcribe/ws", nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	startCtl, _ := json.Marshal(sttWSControl{Type: sttWSStartCtl, SampleRate: 16000, Channels: 1, Bits: 16})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, startCtl))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(5)))
	endCtl, _ := json.Marshal(sttWSControl{Type: sttWSEndCtl})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, endCtl))

	for {
		_, data, rerr := conn.Read(ctx)
		require.NoError(t, rerr)
		var m sttWSServerMsg
		require.NoError(t, json.Unmarshal(data, &m))
		if m.Type != sttWSDone {
			continue
		}
		assert.Equal(t, "整段", m.Final)
		break
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.True(t, strings.HasPrefix(provider.audio, "RIFF"))
}

// TestSTTTranscribeWS_FinalRecheckOffByDefault verifies the default: the chunks
// are all the client gets. The whole recording is not transcribed a second time,
// so "done" carries no final and the text the client accumulated stands.
func TestSTTTranscribeWS_FinalRecheckOffByDefault(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	provider := &sttTestProvider{text: "分段"}
	SetSTTProvider(provider)

	origChunk := model.ConfigInstance.STT.VLLM.ChunkMs
	origRecheck := model.ConfigInstance.STT.VLLM.FinalRecheck
	model.ConfigInstance.STT.VLLM.ChunkMs = 200
	model.ConfigInstance.STT.VLLM.FinalRecheck = false
	defer func() {
		model.ConfigInstance.STT.VLLM.ChunkMs = origChunk
		model.ConfigInstance.STT.VLLM.FinalRecheck = origRecheck
	}()

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/stt/transcribe/ws", nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	startCtl, _ := json.Marshal(sttWSControl{Type: sttWSStartCtl, SampleRate: 16000, Channels: 1, Bits: 16})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, startCtl))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(5)))

	// Wait for the chunk itself rather than sleeping: the chunk ends on 800 ms
	// of silence, so receiving the text proves the wait was long enough.
	assertReadsText(t, conn)

	endCtl, _ := json.Marshal(sttWSControl{Type: sttWSEndCtl})
	require.NoError(t, conn.Write(ctx, websocket.MessageText, endCtl))

	for {
		_, data, rerr := conn.Read(ctx)
		require.NoError(t, rerr)
		var m sttWSServerMsg
		require.NoError(t, json.Unmarshal(data, &m))
		if m.Type != sttWSDone {
			continue
		}
		assert.Empty(t, m.Final, "no whole-recording pass while the switch is off")
		break
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, 1, provider.calls, "only the chunk is transcribed")
}

// TestTranscribeBufferedChunk_DropsSilence verifies a chunk with no speech is
// discarded instead of uploaded: Whisper invents text for a silent clip, and in
// continuous-conversation mode that text would be sent as a message.
func TestTranscribeBufferedChunk_DropsSilence(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(make([]byte, 8000))
	state.mu.Unlock()
	provider := &sttTestProvider{text: "谢谢观看"}
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 1000))

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Empty(t, provider.audio, "silence must not reach the engine")
}

// TestTranscribeBufferedChunk_DropsLoneKeepalive verifies a single loud frame
// does not make a chunk worth uploading: the client sends one keepalive frame
// every couple of seconds regardless of the gate, and a buffered engine answers
// that with invented text.
func TestTranscribeBufferedChunk_DropsLoneKeepalive(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(3))
	state.voiceBytes = 3200 // one 100 ms frame
	state.lastVoiceAt = time.Now().Add(-2 * time.Second)
	state.mu.Unlock()
	provider := &sttTestProvider{text: "谢谢观看"}
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 200))

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Empty(t, provider.audio)
}

// TestTranscribeBufferedChunk_Done covers the done-short-circuit.
func TestTranscribeBufferedChunk_Done(t *testing.T) {
	state := &sttStreamState{}
	state.buffer.Write(loudPCM(3))
	state.mu.Lock()
	state.done = true
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	provider := &sttTestProvider{text: "x"}
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 1000))
	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Empty(t, provider.audio)
}

// TestTranscribeBufferedChunk_TranscribeError covers the provider-error path.
func TestTranscribeBufferedChunk_TranscribeError(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(3))
	state.lastVoiceAt = time.Now().Add(-2 * time.Second)
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	conn := newTestWSConn(t)

	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, &sttTestProvider{errText: "boom"}, conn, "zh", 200))
}

// feedBufferedChunk stages one pause-terminated chunk of speech.
func feedBufferedChunk(state *sttStreamState) {
	state.mu.Lock()
	state.buffer.Write(loudPCM(3))
	state.lastVoiceAt = time.Now().Add(-2 * time.Second)
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
}

// TestTranscribeBufferedChunk_FailsAfterStreak verifies a real engine outage is
// surfaced instead of leaving the mode "listening" and silent forever, and that
// a single hiccup does not end the conversation.
func TestTranscribeBufferedChunk_FailsAfterStreak(t *testing.T) {
	state := &sttStreamState{}
	provider := &sttTestProvider{errText: "bad key"}
	conn := newTestWSConn(t)

	for i := 1; i < sttBufferedFailStreakLimit; i++ {
		feedBufferedChunk(state)
		assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 200),
			"hiccup %d must not end the conversation", i)
	}
	feedBufferedChunk(state)
	assert.True(t, transcribeBufferedChunkIfDue(context.Background(), state, provider, conn, "zh", 200),
		"a streak of failures must surface")
}

// TestTranscribeBufferedChunk_SuccessResetsStreak verifies the streak counts
// CONSECUTIVE failures: a hiccup followed by working chunks must not add up to
// an error later.
func TestTranscribeBufferedChunk_SuccessResetsStreak(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)
	failing := &sttTestProvider{errText: "bad key"}
	working := &sttTestProvider{text: "你好"}

	for i := 1; i < sttBufferedFailStreakLimit; i++ {
		feedBufferedChunk(state)
		assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, failing, conn, "zh", 200))
	}
	feedBufferedChunk(state)
	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, working, conn, "zh", 200))

	feedBufferedChunk(state)
	assert.False(t, transcribeBufferedChunkIfDue(context.Background(), state, failing, conn, "zh", 200),
		"the streak restarted, so one failure is a hiccup again")
}

// TestTranscribeBufferedChunk_WriteError covers the write-error path, which
// returns abort=true to cancel the connection.
func TestTranscribeBufferedChunk_WriteError(t *testing.T) {
	state := &sttStreamState{}
	state.mu.Lock()
	state.buffer.Write(loudPCM(3))
	state.lastVoiceAt = time.Now().Add(-2 * time.Second)
	state.voiceBytes = 1 << 20
	state.mu.Unlock()
	conn := newTestWSConn(t)
	// Closing the connection forces the subsequent write to fail.
	require.NoError(t, conn.Close(websocket.StatusNormalClosure, ""))

	assert.True(t, transcribeBufferedChunkIfDue(context.Background(), state, &sttTestProvider{text: "hello"}, conn, "zh", 200))
}

// TestSTTTranscribeWS_SurvivesIdleTicks is the regression test for continuous
// conversation: that socket stays open across whole minutes of silence, so a
// tick that finds nothing to transcribe must not end the incremental loop —
// otherwise the mode hears the first sentence and nothing after it.
func TestSTTTranscribeWS_SurvivesIdleTicks(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "增量"})

	origChunk := model.ConfigInstance.STT.VLLM.ChunkMs
	model.ConfigInstance.STT.VLLM.ChunkMs = 50
	defer func() { model.ConfigInstance.STT.VLLM.ChunkMs = origChunk }()

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/stt/transcribe/ws", nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(3)))
	assertReadsText(t, conn)

	// Several ticks with nothing to transcribe: the loop must keep running.
	time.Sleep(900 * time.Millisecond)

	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(3)))
	assertReadsText(t, conn)
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

	origChunk := model.ConfigInstance.STT.VLLM.ChunkMs
	model.ConfigInstance.STT.VLLM.ChunkMs = 50
	defer func() { model.ConfigInstance.STT.VLLM.ChunkMs = origChunk }()

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/stt/transcribe/ws", nil)
	require.NoError(t, err)

	// Send audio then never read from the client; the server's large incremental
	// write will block and time out, aborting the incremental goroutine.
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(3)))

	// Give the 5s write timeout time to expire (slightly over, plus buffer).
	time.Sleep(5200 * time.Millisecond)
	_ = conn.CloseNow()
}

// A malformed control frame must not kill the audio path: the client's next
// binary frame still has to land. Dropping the connection here would end a
// recording over one bad text frame.
func TestHandleSTTWSMessage_MalformedControlIsIgnored(t *testing.T) {
	state := &sttStreamState{}
	conn := newTestWSConn(t)

	require.NoError(t, handleSTTWSMessage(state, conn, websocket.MessageText, []byte("{not json")))

	require.NoError(t, handleSTTWSMessage(state, conn, websocket.MessageBinary, loudPCM(1)[:200]))
	state.mu.Lock()
	defer state.mu.Unlock()
	assert.Len(t, state.buffer.Bytes(), 200, "audio after a bad control frame must still be recorded")
	assert.False(t, state.done)
}
