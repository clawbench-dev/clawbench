package handler

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/store"
	"clawbench/internal/ws"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tinyMP3Base64 is a 0.3 s 440 Hz sine, 24 kHz mono, 48 kbps MP3 generated with
// ffmpeg. Embedded so the streaming tests need no external tools or fixtures.
const tinyMP3Base64 = "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"

func tinyMP3() []byte {
	raw, err := base64.StdEncoding.DecodeString(tinyMP3Base64)
	if err != nil {
		panic("mp3 fixture is corrupt: " + err.Error())
	}
	return raw
}

// --- speakSummary / PCM streaming ---

// fixtureProvider writes the real MP3 fixture and streams it in chunks.
type fixtureProvider struct {
	synthErr error
	// garbage makes SynthesizeStream emit bytes that are not an MP3, so the
	// decoder rejects the stream.
	garbage bool
}

func (p *fixtureProvider) Synthesize(_ context.Context, _, outputPath, _ string) error {
	if p.synthErr != nil {
		return p.synthErr
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, tinyMP3(), 0o644)
}

func (p *fixtureProvider) SynthesizeStream(ctx context.Context, _, outputPath, _ string,
	chunkCh chan<- []byte,
) error {
	if p.synthErr != nil {
		return p.synthErr
	}
	raw := []byte("not an mp3 at all")
	if !p.garbage {
		raw = tinyMP3()
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outputPath, raw, 0o644); err != nil {
		return err
	}
	for len(raw) > 0 {
		n := 512
		if n > len(raw) {
			n = len(raw)
		}
		select {
		case chunkCh <- raw[:n]:
		case <-ctx.Done():
			return ctx.Err()
		}
		raw = raw[n:]
	}
	return nil
}

func setupSpeakSummary(t *testing.T, provider speech.SpeechProvider, mp3Path string) (*httptest.Server, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		speakSummary(r.Context(), conn, provider, "你好", mp3Path, "zh")
	})
	srv := httptest.NewServer(mux)
	return srv, srv.Close
}

func dialStream(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):]+"/stream", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// drainFrames reads until a terminal frame, returning the JSON frames in order
// and every binary byte received in between.
func drainFrames(t *testing.T, conn *websocket.Conn) ([]map[string]any, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var frames []map[string]any
	var audio []byte
	for {
		mt, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read frame: %v (frames so far: %v)", err, frames)
		}
		if mt == websocket.MessageBinary {
			audio = append(audio, data...)
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("frame is not JSON: %v", err)
		}
		frames = append(frames, frame)
		if frame[ttsWSType] == ttsWSDone || frame[ttsWSType] == strError {
			return frames, audio
		}
	}
}

func TestSpeakSummary_StreamsCachedMP3(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "cached.mp3")
	if err := os.WriteFile(mp3Path, tinyMP3(), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	provider := &fixtureProvider{synthErr: context.Canceled}
	srv, cleanup := setupSpeakSummary(t, provider, mp3Path)
	defer cleanup()

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, audio := drainFrames(t, conn)
	if len(frames) < 2 {
		t.Fatalf("expected audio format frame + done, got %v", frames)
	}
	if frames[0][ttsWSType] != speechTypeAudio {
		t.Fatalf("first frame = %v, want audio format", frames[0])
	}
	if frames[0]["sampleRate"].(float64) != 24000 {
		t.Errorf("sampleRate = %v, want 24000", frames[0]["sampleRate"])
	}
	if frames[0]["channels"].(float64) != 1 || frames[0]["bits"].(float64) != 16 {
		t.Errorf("format = %v, want mono 16-bit", frames[0])
	}
	if len(audio) < 8000 {
		t.Errorf("pcm bytes = %d, want roughly 14400", len(audio))
	}
	if frames[len(frames)-1][ttsWSType] != ttsWSDone {
		t.Errorf("last frame = %v, want done", frames[len(frames)-1])
	}
}

func TestSpeakSummary_StreamsSynthesizedMP3(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "fresh.mp3") // does not exist yet

	provider := &fixtureProvider{}
	srv, cleanup := setupSpeakSummary(t, provider, mp3Path)
	defer cleanup()

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, audio := drainFrames(t, conn)
	if frames[0][ttsWSType] != speechTypeAudio {
		t.Fatalf("first frame = %v, want audio format", frames[0])
	}
	if len(audio) < 8000 {
		t.Errorf("pcm bytes = %d, want roughly 14400", len(audio))
	}
	if frames[len(frames)-1][ttsWSType] != ttsWSDone {
		t.Errorf("last frame = %v, want done", frames[len(frames)-1])
	}
	// The stream must also leave the synthesized file behind for the cache.
	if info, err := os.Stat(mp3Path); err != nil || info.Size() == 0 {
		t.Errorf("synthesized mp3 not cached: %v", err)
	}
}

func TestSpeakSummary_NonStreamingProviderFallsBackToFile(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "file.mp3")

	// fileOnlyProvider implements SpeechProvider but NOT StreamingSpeechProvider.
	provider := &fileOnlyProvider{}
	srv, cleanup := setupSpeakSummary(t, provider, mp3Path)
	defer cleanup()

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, audio := drainFrames(t, conn)
	if !provider.called {
		t.Error("Synthesize should be used when the provider cannot stream")
	}
	if len(audio) < 8000 {
		t.Errorf("pcm bytes = %d, want roughly 14400", len(audio))
	}
	if frames[len(frames)-1][ttsWSType] != ttsWSDone {
		t.Errorf("last frame = %v, want done", frames[len(frames)-1])
	}
}

// fileOnlyProvider writes the fixture without streaming support.
type fileOnlyProvider struct{ called bool }

func (p *fileOnlyProvider) Synthesize(_ context.Context, _, outputPath, _ string) error {
	p.called = true
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, tinyMP3(), 0o644)
}

func TestSpeakSummary_SynthesizeFailure(t *testing.T) {
	dir := t.TempDir()
	providerErr := &failingFileProvider{}
	srv, cleanup := setupSpeakSummary(t, providerErr, filepath.Join(dir, "x.mp3"))
	defer cleanup()

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, _ := drainFrames(t, conn)
	if len(frames) != 1 || frames[0][ttsWSType] != strError {
		t.Fatalf("frames = %v, want a single error frame", frames)
	}
	if frames[0][messageKey] != "synthesize failed" {
		t.Errorf("message = %v, want synthesize failed", frames[0][messageKey])
	}
}

type failingFileProvider struct{}

func (failingFileProvider) Synthesize(context.Context, string, string, string) error {
	return context.DeadlineExceeded
}

func TestSpeakSummary_CorruptCacheEmitsDecodeError(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "broken.mp3")
	if err := os.WriteFile(mp3Path, []byte("definitely not an mp3"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	srv, cleanup := setupSpeakSummary(t, &fixtureProvider{}, mp3Path)
	defer cleanup()

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, _ := drainFrames(t, conn)
	if len(frames) != 1 || frames[0][messageKey] != "decode failed" {
		t.Fatalf("frames = %v, want a single decode failed error", frames)
	}
}

func TestSpeakSummary_StreamingGarbageEmitsDecodeError(t *testing.T) {
	dir := t.TempDir()
	provider := &fixtureProvider{garbage: true}
	srv, cleanup := setupSpeakSummary(t, provider, filepath.Join(dir, "garbage.mp3"))
	defer cleanup()

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, _ := drainFrames(t, conn)
	if len(frames) != 1 || frames[0][messageKey] != "decode failed" {
		t.Fatalf("frames = %v, want a single decode failed error", frames)
	}
}

func TestDownmixFrames(t *testing.T) {
	stereo := make([]byte, 4*2) // two frames of L/R int16
	binary.LittleEndian.PutUint16(stereo[0:], 1000)
	binary.LittleEndian.PutUint16(stereo[2:], 3000)
	binary.LittleEndian.PutUint16(stereo[4:], 65535)
	binary.LittleEndian.PutUint16(stereo[6:], 65535)

	mono := make([]byte, 2*2)
	downmixFrames(stereo, mono, 2)

	if got := binary.LittleEndian.Uint16(mono[0:]); got != 2000 {
		t.Errorf("frame 0 = %d, want 2000", got)
	}
	// Offset-binary averaging must not wrap: (65535+65535)/2 stays 65535.
	if got := binary.LittleEndian.Uint16(mono[2:]); got != 65535 {
		t.Errorf("frame 1 = %d, want 65535", got)
	}
}

// --- VoiceTurnWS protocol ---

func setupVoiceTurn(t *testing.T) (*testEnv, *httptest.Server, func()) {
	t.Helper()
	env, teardown := setupTestEnv(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/voice/turn/ws", middleware.Auth(VoiceTurnWS))
	server := httptest.NewServer(mux)

	cleanup := func() {
		server.Close()
		teardown()
	}
	return env, server, cleanup
}

func dialVoiceTurn(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):]+"/api/voice/turn/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// dialVoiceTurnAsProject connects with a project cookie, so the handler can
// check that the session belongs to it.
func dialVoiceTurnAsProject(t *testing.T, srv *httptest.Server, projectPath string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{HTTPHeader: http.Header{
		"Cookie": []string{model.ScopedCookieName("clawbench_project") + "=" + url.QueryEscape(projectPath)},
	}}
	conn, _, err := websocket.Dial(ctx, "ws"+srv.URL[len("http"):]+"/api/voice/turn/ws", opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func sendText(t *testing.T, conn *websocket.Conn, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(payload)); err != nil {
		t.Fatalf("write %s: %v", payload, err)
	}
}

func readFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mt, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if mt != websocket.MessageText {
		t.Fatalf("expected a text frame, got binary (%d bytes)", len(data))
	}
	var frame map[string]any
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("frame is not JSON: %v", err)
	}
	return frame
}

func TestVoiceTurnWS_SayWithoutTextIsRejected(t *testing.T) {
	_, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	conn := dialVoiceTurn(t, srv)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"say","sessionId":"whatever","text":""}`)
	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "no speech" {
		t.Fatalf("frame = %v, want no speech error", frame)
	}
}

func TestVoiceTurnWS_SayUnknownSession(t *testing.T) {
	_, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	conn := dialVoiceTurn(t, srv)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"say","sessionId":"missing-session","text":"你好"}`)

	if frame := readFrame(t, conn); frame[ttsWSType] != "final" || frame[sttWSText] != "你好" {
		t.Fatalf("first frame = %v, want the echo of the spoken text", frame)
	}
	// The session is resolved before the thinking phase is announced: an
	// unknown session must fail without telling the device a turn is running.
	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "session not found" {
		t.Fatalf("second frame = %v, want session not found", frame)
	}
}

func TestVoiceTurnWS_ReplayUnknownMessage(t *testing.T) {
	_, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	conn := dialVoiceTurn(t, srv)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"replay","messageId":999999}`)
	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "empty reply" {
		t.Fatalf("frame = %v, want empty reply error", frame)
	}
}

func TestVoiceTurnWS_EndWithoutAudio(t *testing.T) {
	_, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	conn := dialVoiceTurn(t, srv)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"listen","sessionId":"missing-session"}`)
	sendText(t, conn, `{"type":"end"}`)
	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "no speech" {
		t.Fatalf("frame = %v, want no speech error", frame)
	}
}

// TestVoiceTurnWS_SayPersistsAndAnnouncesUserMessage covers the voice channel's
// own copy of the "persist + announce" step.
//
// RunTurn only writes the assistant placeholder, so a turn started from this
// WebSocket used to leave the user's sentence out of chat_history entirely and
// broadcast no user_message: the web UI never showed the question, and a reload
// did not bring it back. The session here points at a backend that does not
// exist, so RunTurn fails fast after the persistence step and the test does not
// spawn a real agent.
func TestVoiceTurnWS_SayPersistsAndAnnouncesUserMessage(t *testing.T) {
	env, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	prevMgr := ws.GetManager()
	ws.SetManagerForTest(ws.NewManagerForTest())
	t.Cleanup(func() { ws.SetManagerForTest(prevMgr) })
	mgr := ws.GetManager()

	const clientID = "voice-user-message-client"
	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, clientID, "")
	require.NotNil(t, sub, "subscribing the observer client must succeed")

	sessionID := "voice-turn-user-message"
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, ?, 'ghost-backend', 'voice', 'ghost-agent', 'default', '', 'chat', 0)`,
		sessionID, store.ProjectIDForTest(t, env.ProjectDir),
	)
	require.NoError(t, err)
	mgr.StreamHub().Subscribe(clientID, sessionID)

	conn := dialVoiceTurn(t, srv)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"say","sessionId":"`+sessionID+`","text":"你好"}`)

	// final -> phase -> error(turn failed): the error frame proves RunTurn
	// returned, so the assertions below do not race the turn.
	if frame := readFrame(t, conn); frame[ttsWSType] != "final" || frame[sttWSText] != "你好" {
		t.Fatalf("first frame = %v, want the echo of the spoken text", frame)
	}
	if frame := readFrame(t, conn); frame[ttsWSType] != speechTypePhase {
		t.Fatalf("second frame = %v, want the thinking phase", frame)
	}
	if frame := readFrame(t, conn); frame[ttsWSType] != strError || frame[messageKey] != "turn failed" {
		t.Fatalf("third frame = %v, want turn failed", frame)
	}

	var content string
	require.NoError(t,
		db.QueryRow(`SELECT content FROM chat_history WHERE session_id = ? AND role = 'user'`, sessionID).Scan(&content),
		"the spoken sentence must be persisted as a user message")
	assert.Equal(t, "你好", content)

	announced := false
	for _, ev := range sub.GetBufferedEvents() {
		data, ok := ev.Data.(ws.ChatStreamData)
		if !ok || data.EventType != "user_message" {
			continue
		}
		if payload, ok := data.Payload.(map[string]any); ok && payload["content"] == "你好" {
			announced = true
		}
	}
	assert.True(t, announced, "a user_message event must be broadcast to the session's subscribers")
}

func TestVoiceTurnWS_BusySessionIsRefused(t *testing.T) {
	env, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	prevWait := voiceTurnBusyWait
	voiceTurnBusyWait = 150 * time.Millisecond
	t.Cleanup(func() { voiceTurnBusyWait = prevWait })

	sessionID := "voice-turn-busy"
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, ?, 'ghost-backend', 'busy', 'ghost-agent', 'default', '', 'chat', 0)`,
		sessionID, store.ProjectIDForTest(t, env.ProjectDir),
	)
	require.NoError(t, err)

	// Another turn owns the session (the web UI is streaming a reply).
	if _, ok := service.TryClaimSessionRun(sessionID); !ok {
		t.Fatal("pre-claim must succeed")
	}
	defer service.FinishSessionRun(sessionID)

	conn := dialVoiceTurn(t, srv)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"say","sessionId":"`+sessionID+`","text":"你好"}`)

	if frame := readFrame(t, conn); frame[ttsWSType] != "final" {
		t.Fatalf("first frame = %v, want the transcript echo", frame)
	}
	if frame := readFrame(t, conn); frame[ttsWSType] != speechTypePhase {
		t.Fatalf("second frame = %v, want the thinking phase", frame)
	}
	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "session busy" {
		t.Fatalf("third frame = %v, want session busy", frame)
	}

	// A refused turn must not persist anything: no orphan question without an answer.
	var users int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM chat_history WHERE session_id = ? AND role = 'user'`, sessionID,
	).Scan(&users))
	assert.Equal(t, 0, users, "a refused turn must not persist the user message")
}

func TestVoiceTurnWS_GroupSessionIsRejected(t *testing.T) {
	env, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	sessionID := "voice-turn-group"
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, ?, 'group', 'group', '', 'default', '', 'group', 0)`,
		sessionID, store.ProjectIDForTest(t, env.ProjectDir),
	)
	require.NoError(t, err)

	conn := dialVoiceTurn(t, srv)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"say","sessionId":"`+sessionID+`","text":"你好"}`)

	if frame := readFrame(t, conn); frame[ttsWSType] != "final" {
		t.Fatalf("first frame = %v, want the transcript echo", frame)
	}
	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "group session not supported" {
		t.Fatalf("second frame = %v, want the group rejection", frame)
	}
}

func TestVoiceTurnWS_ForeignProjectSessionIsRejected(t *testing.T) {
	env, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	sessionID := "voice-turn-foreign"
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, ?, 'ghost-backend', 'foreign', 'ghost-agent', 'default', '', 'chat', 0)`,
		sessionID, store.ProjectIDForTest(t, env.ProjectDir),
	)
	require.NoError(t, err)

	otherProject := filepath.Join(env.WatchDir, "other-project")
	require.NoError(t, os.MkdirAll(otherProject, 0o755))

	conn := dialVoiceTurnAsProject(t, srv, otherProject)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"say","sessionId":"`+sessionID+`","text":"你好"}`)

	if frame := readFrame(t, conn); frame[ttsWSType] != "final" {
		t.Fatalf("first frame = %v, want the transcript echo", frame)
	}
	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "session not found" {
		t.Fatalf("second frame = %v, want the ownership rejection", frame)
	}
}

// --- voiceTurnSpeak: the replay/turn speech path ---

// setupSpeakMessage seeds one assistant message and returns its id, so
// voiceTurnSpeak has a real row to read its spoken text from.
func setupSpeakMessage(t *testing.T, sessionID, text string) int64 {
	t.Helper()
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, 1, 'claude', 'voice', 'a', 'default', '', 'chat', 0)`, sessionID,
	)
	require.NoError(t, err)
	msgID, err := service.AddChatMessage("", "claude", sessionID, "assistant",
		`{"blocks":[{"type":"text","text":"`+text+`"}]}`, nil, false, "")
	require.NoError(t, err)
	return msgID
}

// setupSpeakServer wires voiceTurnSpeak behind a WebSocket the way the handler
// does, so the test drives the real frame sequence.
func setupSpeakServer(t *testing.T, projectPath string, messageID int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		voiceTurnSpeak(r.Context(), conn, projectPath, messageID, "zh")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestVoiceTurnSpeak_StreamsTheSummary(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	dir := env.ProjectDir
	prevProvider := GetSpeechProvider()
	SetSpeechProvider(&fixtureProvider{})
	t.Cleanup(func() { SetSpeechProvider(prevProvider) })

	const sessionID = "voice-speak-ok"
	msgID := setupSpeakMessage(t, sessionID, "回答内容")
	srv := setupSpeakServer(t, dir, msgID)

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, audio := drainFrames(t, conn)
	require.NotEmpty(t, frames)
	assert.Equal(t, ttsWSSummary, frames[0][ttsWSType], "the summary frame comes before any audio")
	assert.Equal(t, float64(msgID), frames[0]["messageId"])
	assert.Equal(t, ttsWSDone, frames[len(frames)-1][ttsWSType])
	assert.NotEmpty(t, audio, "the synthesized audio must be streamed as binary frames")
}

// A message whose blocks carry no readable text cannot be spoken: the device
// gets an explicit error instead of an empty stream that looks like success.
func TestVoiceTurnSpeak_EmptyReplyIsReported(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	dir := env.ProjectDir
	prevProvider := GetSpeechProvider()
	SetSpeechProvider(&fixtureProvider{})
	t.Cleanup(func() { SetSpeechProvider(prevProvider) })

	const sessionID = "voice-speak-empty"
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, 1, 'claude', 'voice', 'a', 'default', '', 'chat', 0)`, sessionID,
	)
	require.NoError(t, err)
	msgID, err := service.AddChatMessage("", "claude", sessionID, "assistant", `{"blocks":[]}`, nil, false, "")
	require.NoError(t, err)
	srv := setupSpeakServer(t, dir, msgID)

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, audio := drainFrames(t, conn)
	require.Len(t, frames, 1)
	assert.Equal(t, strError, frames[0][ttsWSType])
	assert.Equal(t, "empty reply", frames[0][messageKey])
	assert.Empty(t, audio)
}

// --- voiceTurnTranscribe ---

func TestVoiceTurnTranscribe_ReturnsProviderText(t *testing.T) {
	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "识别结果"})
	t.Cleanup(func() { SetSTTProvider(prev) })

	assert.Equal(t, "识别结果", voiceTurnTranscribe(context.Background(), []byte{1, 2, 3}))
}

func TestVoiceTurnTranscribe_SkipsProviderWithoutAudio(t *testing.T) {
	prev := GetSTTProvider()
	stub := &stubSTT{text: "never asked"}
	SetSTTProvider(stub)
	t.Cleanup(func() { SetSTTProvider(prev) })

	assert.Equal(t, "", voiceTurnTranscribe(context.Background(), nil))
	assert.Zero(t, stub.calls, "an empty utterance must not reach the recognizer")
}

func TestVoiceTurnTranscribe_SwallowsProviderError(t *testing.T) {
	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{err: errors.New("boom")})
	t.Cleanup(func() { SetSTTProvider(prev) })

	assert.Equal(t, "", voiceTurnTranscribe(context.Background(), []byte{1}),
		"a failed recognition yields no speech, not a panic")
}

func TestVoiceTurnTranscribe_WithoutProvider(t *testing.T) {
	prev := GetSTTProvider()
	SetSTTProvider(nil)
	t.Cleanup(func() { SetSTTProvider(prev) })

	assert.Equal(t, "", voiceTurnTranscribe(context.Background(), []byte{1}))
}

// Replay takes a message id from the device, so it needs the same ownership
// check as the say path: a logged-in device must not be able to make the server
// read out another project's reply.
func TestVoiceTurnWS_ReplayForeignProjectSessionIsRejected(t *testing.T) {
	env, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	sessionID := "voice-replay-foreign"
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, agent_source, model, session_type, archived)
		 VALUES (?, ?, 'ghost-backend', 'foreign', 'ghost-agent', 'default', '', 'chat', 0)`,
		sessionID, store.ProjectIDForTest(t, env.ProjectDir),
	)
	require.NoError(t, err)

	otherProject := filepath.Join(env.WatchDir, "other-project")
	require.NoError(t, os.MkdirAll(otherProject, 0o755))

	conn := dialVoiceTurnAsProject(t, srv, otherProject)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"replay","sessionId":"`+sessionID+`","messageId":1}`)

	frame := readFrame(t, conn)
	if frame[ttsWSType] != strError || frame[messageKey] != "session not found" {
		t.Fatalf("frame = %v, want the ownership rejection", frame)
	}
}

// Replay inside the caller's own project is allowed and proceeds to speech.
func TestVoiceTurnWS_ReplayOwnProjectSession(t *testing.T) {
	env, srv, cleanup := setupVoiceTurn(t)
	defer cleanup()

	prevProvider := GetSpeechProvider()
	SetSpeechProvider(&fixtureProvider{})
	t.Cleanup(func() { SetSpeechProvider(prevProvider) })

	sessionID := "voice-replay-own"
	msgID := setupSpeakMessage(t, sessionID, "已保存的回答")
	// setupSpeakMessage seeds under project 1; point the row at the test project
	// so the cookie check matches.
	_, err := store.UnsafeDBForTest().Exec(
		`UPDATE chat_sessions SET project_id = ? WHERE id = ?`,
		store.ProjectIDForTest(t, env.ProjectDir), sessionID,
	)
	require.NoError(t, err)

	conn := dialVoiceTurnAsProject(t, srv, env.ProjectDir)
	defer func() { _ = conn.CloseNow() }()

	sendText(t, conn, `{"type":"replay","sessionId":"`+sessionID+`","messageId":`+strconv.FormatInt(msgID, 10)+`}`)

	// Drain to the terminal frame: the speech stream keeps writing the MP3 into
	// the project's .clawbench/generated/tts while it runs, and returning early
	// leaves the test's TempDir cleanup racing that write (Windows reports
	// "The directory is not empty").
	frames, _ := drainFrames(t, conn)
	require.NotEmpty(t, frames)
	if frames[0][ttsWSType] != ttsWSSummary {
		t.Fatalf("first frame = %v, want the summary of the replayed message", frames[0])
	}
	assert.Equal(t, ttsWSDone, frames[len(frames)-1][ttsWSType])
}

// silentProvider reports a successful synthesis but writes no file, which is
// the only way to reach the "open audio failed" branch: a real provider that
// succeeds always leaves an MP3 behind.
type silentProvider struct{}

func (silentProvider) Synthesize(context.Context, string, string, string) error { return nil }

// A missing MP3 after a "successful" synthesis is reported as "open audio
// failed" rather than closing the socket with no explanation.
func TestSpeakSummary_MissingFileReportsOpenFailure(t *testing.T) {
	srv, cleanup := setupSpeakSummary(t, silentProvider{}, filepath.Join(t.TempDir(), "absent.mp3"))
	defer cleanup()

	conn := dialStream(t, srv)
	defer func() { _ = conn.CloseNow() }()

	frames, audio := drainFrames(t, conn)
	require.Len(t, frames, 1)
	assert.Equal(t, strError, frames[0][ttsWSType])
	assert.Equal(t, "open audio failed", frames[0][messageKey])
	assert.Empty(t, audio)
}

// --- emitVoiceTurnTerminal ---

// collectEvents subscribes a throwaway client and returns the chat-stream events
// broadcast while fn runs.
func collectEvents(t *testing.T, sessionID string, fn func()) []string {
	t.Helper()
	prevMgr := ws.GetManager()
	ws.SetManagerForTest(ws.NewManagerForTest())
	t.Cleanup(func() { ws.SetManagerForTest(prevMgr) })
	mgr := ws.GetManager()

	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "voice-terminal-observer", "")
	require.NotNil(t, sub)
	mgr.StreamHub().Subscribe("voice-terminal-observer", sessionID)

	fn()

	var types []string
	for _, ev := range sub.GetBufferedEvents() {
		if data, ok := ev.Data.(ws.ChatStreamData); ok {
			types = append(types, data.EventType)
		}
	}
	return types
}

// A user-cancelled turn must still close the timeline: the device that started
// it may have no subscription, so nobody else would send the terminal event.
func TestEmitVoiceTurnTerminal_CancelledTurnClosesTheBubble(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	const sessionID = "voice-terminal-cancelled"
	types := collectEvents(t, sessionID, func() {
		emitVoiceTurnTerminal(sessionID, service.TurnResult{CancelReason: "user"})
	})
	assert.Contains(t, types, "cancelled")
	assert.NotContains(t, types, "done", "a cancelled turn must not also look completed")
}

// A clean turn reports completion.
func TestEmitVoiceTurnTerminal_CleanTurnEmitsDone(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	const sessionID = "voice-terminal-clean"
	types := collectEvents(t, sessionID, func() {
		emitVoiceTurnTerminal(sessionID, service.TurnResult{})
	})
	assert.Contains(t, types, "done")
}

// A turn that ended with an error emits neither: the error was already reported
// on the voice socket, and claiming completion would close the web bubble as if
// the answer had arrived.
func TestEmitVoiceTurnTerminal_FailedTurnEmitsNothing(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	const sessionID = "voice-terminal-failed"
	types := collectEvents(t, sessionID, func() {
		emitVoiceTurnTerminal(sessionID, service.TurnResult{Err: "boom"})
	})
	assert.NotContains(t, types, "done")
	assert.NotContains(t, types, "cancelled")
}

// A client that hangs up mid-stream must not keep the handler writing forever:
// pumpPCM stops at the first failed frame write. This is the branch that keeps a
// dropped device from pinning a goroutine for the length of the audio.
func TestSpeakSummary_ClientGoneStopsTheStream(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "cached.mp3")
	require.NoError(t, os.WriteFile(mp3Path, tinyMP3(), 0o644))

	done := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		speakSummary(r.Context(), conn, &fixtureProvider{}, "你好", mp3Path, "zh")
		close(done)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	conn := dialStream(t, srv)
	// Hang up immediately: the handler is still inside the summary/audio write
	// sequence, so at least one frame write has nowhere to go.
	_ = conn.CloseNow()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the handler kept streaming to a closed connection")
	}
}
