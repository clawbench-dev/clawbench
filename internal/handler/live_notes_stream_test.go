package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/stt"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The live-note streaming endpoint has to behave the same for both kinds of
// engine: Doubao streams while the person talks, Whisper cannot and is fed
// accumulated WAV chunks. These tests drive the socket the publisher uses and
// check that a transcript line lands in the database either way — that is the
// contract the client relies on, and it must not depend on which engine is
// configured.

// stubStreamProvider is a streaming engine that answers every write with the
// same text, the way a real one keeps returning the whole utterance.
type stubStreamProvider struct {
	stubSTT
	results chan stt.StreamResult
	writes  int
	// revisions is what a real engine does: it re-sends the whole utterance,
	// longer each time. The relay has to collapse them into ONE line.
	revisions []string
	next      int
}

func newStubStreamProvider(revisions ...string) *stubStreamProvider {
	return &stubStreamProvider{
		stubSTT:   stubSTT{text: revisions[len(revisions)-1]},
		results:   make(chan stt.StreamResult, 16),
		revisions: revisions,
	}
}

func (s *stubStreamProvider) OpenStream(context.Context, stt.StreamOptions) (stt.StreamSession, error) {
	return &stubStreamSession{p: s}, nil
}

type stubStreamSession struct{ p *stubStreamProvider }

func (s *stubStreamSession) Write([]byte) error {
	s.p.writes++
	text := s.p.revisions[s.p.next]
	if s.p.next < len(s.p.revisions)-1 {
		s.p.next++
	}
	select {
	case s.p.results <- stt.StreamResult{Text: text}:
	default:
	}
	return nil
}

func (s *stubStreamSession) Finish() error                    { return nil }
func (s *stubStreamSession) Results() <-chan stt.StreamResult { return s.p.results }
func (s *stubStreamSession) Close()                           { close(s.p.results) }

// streamDial opens the notes socket the way the publisher does.
func streamDial(t *testing.T, srv *httptest.Server, project, token string) *websocket.Conn {
	const channel = "mic"
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") +
		"/api/live-share/notes/stream?token=" + url.QueryEscape(token) +
		"&channel=" + url.QueryEscape(channel) + "&speaker=" + url.QueryEscape("我")

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Cookie": {model.ScopedCookieName("clawbench_project") + "=" + url.QueryEscape(project)},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

// loudPCM is one frame of audio well above the silence floor.
func loudPCM(frames int) []byte {
	out := make([]byte, 0, frames*3200)
	for range frames {
		frame := make([]byte, 3200)
		for j := 0; j < len(frame); j += 2 {
			// A square wave: peak amplitude 0.25 of full scale.
			v := int16(8192)
			frame[j] = byte(uint16(v))
			frame[j+1] = byte(uint16(v) >> 8)
		}
		out = append(out, frame...)
	}
	return out
}

// readUntilLine drains the socket until a committed line arrives.
func readUntilLine(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		require.NoError(t, err)
		var msg map[string]any
		require.NoError(t, json.Unmarshal(data, &msg))
		if msg["type"] == "line" {
			return msg
		}
	}
}

func countNoteSegments(t *testing.T, db *sql.DB, channel string) (int, string) {
	t.Helper()
	var n int
	var text sql.NullString
	err := db.QueryRow(
		`SELECT COUNT(*), MAX(text) FROM live_notes_segments WHERE channel = ?`, channel,
	).Scan(&n, &text)
	require.NoError(t, err)
	return n, text.String
}

func TestLiveNotesStream_BufferedEngineStoresALine(t *testing.T) {
	// The OpenAI-compatible engine cannot stream. It must still produce a line
	// through this endpoint, or every Whisper deployment silently loses its
	// transcript the day the client switches to streaming.
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "这是识别结果"})
	t.Cleanup(func() { SetSTTProvider(prev) })

	srv := httptest.NewServer(http.HandlerFunc(LiveNotesStreamWS))
	t.Cleanup(srv.Close)

	conn := streamDial(t, srv, project, room.Token())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(10)))

	msg := readUntilLine(t, conn)
	assert.Equal(t, "这是识别结果", msg["text"])
	assert.Equal(t, "mic", msg["channel"])

	n, text := countNoteSegments(t, db, "mic")
	assert.Equal(t, 1, n)
	assert.Equal(t, "这是识别结果", text)
}

func TestLiveNotesStream_StreamingEngineStoresALine(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	provider := newStubStreamProvider("现在", "现在让我检查", "现在让我检查一下这个文件")
	prev := GetSTTProvider()
	SetSTTProvider(provider)
	t.Cleanup(func() { SetSTTProvider(prev) })

	srv := httptest.NewServer(http.HandlerFunc(LiveNotesStreamWS))
	t.Cleanup(srv.Close)

	conn := streamDial(t, srv, project, room.Token())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// One frame per revision: the stub answers each Write with the next one, the
	// way a real engine grows the text as it hears more.
	// Spaced out on purpose: revisions that arrive within one tick could not
	// tell "commit when the engine settles" apart from "commit every update".
	for range 3 {
		require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(1)))
		time.Sleep(250 * time.Millisecond)
	}

	msg := readUntilLine(t, conn)
	assert.Equal(t, "现在让我检查一下这个文件", msg["text"])
	// The client merges rows by id and drops any without one. A line message
	// without its id looked correct here and left the meeting panel empty.
	id, _ := msg["id"].(float64)
	assert.Positive(t, int64(id), "the stored row id must reach the client")
	assert.Positive(t, provider.writes, "the frames must reach the streaming engine")

	// One line, not one per revision: a streaming engine re-sends the whole
	// utterance on every update, so committing each one would fill the
	// transcript with prefixes of the same sentence.
	n, text := countNoteSegments(t, db, "mic")
	assert.Equal(t, 1, n)
	assert.Equal(t, "现在让我检查一下这个文件", text)
}

// A streaming engine re-sends the whole session's text on every update. Without
// subtracting what was already stored, the second line repeats the first and the
// transcript grows quadratically.
func TestLiveNotesStream_SecondLineDoesNotRepeatTheFirst(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	provider := newStubStreamProvider("你好今天", "你好今天我们讨论")
	prev := GetSTTProvider()
	SetSTTProvider(provider)
	t.Cleanup(func() { SetSTTProvider(prev) })

	srv := httptest.NewServer(http.HandlerFunc(LiveNotesStreamWS))
	t.Cleanup(srv.Close)

	conn := streamDial(t, srv, project, room.Token())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(1)))
	first := readUntilLine(t, conn)
	assert.Equal(t, "你好今天", first["text"])

	// The engine keeps growing the same cumulative text.
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(1)))
	second := readUntilLine(t, conn)
	assert.Equal(t, "我们讨论", second["text"], "only the new part is a line")

	var texts []string
	rows, err := db.Query(`SELECT text FROM live_notes_segments WHERE channel = 'mic' ORDER BY id`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var t2 string
		require.NoError(t, rows.Scan(&t2))
		texts = append(texts, t2)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"你好今天", "我们讨论"}, texts)
}

// The sentence being spoken when the publisher stops must still be stored.
func TestLiveNotesStream_FinishStoresTheLastLine(t *testing.T) {
	db := setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	provider := newStubStreamProvider("最后一句话")
	prev := GetSTTProvider()
	SetSTTProvider(provider)
	t.Cleanup(func() { SetSTTProvider(prev) })

	srv := httptest.NewServer(http.HandlerFunc(LiveNotesStreamWS))
	t.Cleanup(srv.Close)

	conn := streamDial(t, srv, project, room.Token())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, loudPCM(1)))
	// Stop immediately — no 1.2 s of quiet to satisfy the settle rule.
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end"}`)))

	msg := readUntilLine(t, conn)
	assert.Equal(t, "最后一句话", msg["text"])

	n, text := countNoteSegments(t, db, "mic")
	assert.Equal(t, 1, n)
	assert.Equal(t, "最后一句话", text)
}

func TestLiveNotesStream_RejectsUnknownChannel(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "x"})
	t.Cleanup(func() { SetSTTProvider(prev) })

	srv := httptest.NewServer(http.HandlerFunc(LiveNotesStreamWS))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx,
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live-share/notes/stream?token="+
			url.QueryEscape(room.Token())+"&channel=../../etc/passwd",
		&websocket.DialOptions{HTTPHeader: http.Header{
			"Cookie": {model.ScopedCookieName("clawbench_project") + "=" + url.QueryEscape(project)},
		}})
	if err == nil {
		t.Fatal("an unknown channel must not be accepted")
	}
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestWAVFromPCM_WrapsTheSamplesInAContainer(t *testing.T) {
	// Whisper-style engines take a file, not a byte stream: headerless samples
	// are rejected or transcribed as silence.
	pcm := make([]byte, 320)
	out := wavFromPCM(pcm, 16000)
	require.Len(t, out, 44+len(pcm))
	assert.Equal(t, "RIFF", string(out[0:4]))
	assert.Equal(t, "WAVE", string(out[8:12]))
	assert.Equal(t, "fmt ", string(out[12:16]))
	assert.Equal(t, "data", string(out[36:40]))
	assert.Equal(t, uint32(len(pcm)), uint32(out[40])|uint32(out[41])<<8|uint32(out[42])<<16|uint32(out[43])<<24)
}

// The stream endpoint is reachable without a session cookie only for a valid
// share token; an unknown token must 404 rather than upgrade the socket.
func TestLiveNotesStream_RejectsUnknownToken(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project := projPath("/proj/a")
	_, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{text: "x"})
	t.Cleanup(func() { SetSTTProvider(prev) })

	srv := httptest.NewServer(http.HandlerFunc(LiveNotesStreamWS))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx,
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live-share/notes/stream?token=not-a-real-token&channel=0",
		&websocket.DialOptions{HTTPHeader: http.Header{
			"Cookie": {model.ScopedCookieName("clawbench_project") + "=" + url.QueryEscape(project)},
		}})
	require.Error(t, err, "an unknown share token must not be accepted")
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Without a recognizer there is nothing the endpoint could do with the audio,
// so it refuses before upgrading the socket instead of accepting and dropping.
func TestLiveNotesStream_RefusesWithoutRecognizer(t *testing.T) {
	setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project := projPath("/proj/a")
	room, err := liveRegistry.Create(project, "会议")
	require.NoError(t, err)

	prev := GetSTTProvider()
	SetSTTProvider(nil)
	t.Cleanup(func() { SetSTTProvider(prev) })

	srv := httptest.NewServer(http.HandlerFunc(LiveNotesStreamWS))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx,
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live-share/notes/stream?token="+
			url.QueryEscape(room.Token())+"&channel=mic",
		&websocket.DialOptions{HTTPHeader: http.Header{
			"Cookie": {model.ScopedCookieName("clawbench_project") + "=" + url.QueryEscape(project)},
		}})
	require.Error(t, err, "no recognizer means no stream")
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// commitBufferedIfDue must not call the recognizer for a chunk shorter than its
// ~250 ms floor: those are single frames, and transcribing each one would flood
// the engine and the transcript with fragments.
func TestCommitBufferedIfDue_IgnoresAShortChunk(t *testing.T) {
	stub := &stubSTT{text: "x"}
	prev := GetSTTProvider()
	SetSTTProvider(stub)
	t.Cleanup(func() { SetSTTProvider(prev) })

	relay := &notesStreamRelay{channel: "mic"}
	relay.pending = make([]byte, notesStreamSampleRate/2-2)
	relay.pendingFrom = time.Now().Add(-time.Hour)
	relay.lastVoiceAt = time.Now().Add(-time.Hour)

	relay.commitBufferedIfDue(context.Background())

	assert.Zero(t, stub.calls, "a sub-250 ms chunk must not reach the recognizer")
	assert.NotEmpty(t, relay.pending, "the audio stays buffered for the next frame")
}

// A chunk that is long enough but still being spoken is not committed either:
// committing mid-sentence would split one utterance into several lines.
func TestCommitBufferedIfDue_WaitsForSilence(t *testing.T) {
	stub := &stubSTT{text: "x"}
	prev := GetSTTProvider()
	SetSTTProvider(stub)
	t.Cleanup(func() { SetSTTProvider(prev) })

	relay := &notesStreamRelay{channel: "mic"}
	relay.pending = make([]byte, notesStreamSampleRate) // 500 ms
	relay.pendingFrom = time.Now()
	relay.lastVoiceAt = time.Now() // audio is still arriving

	relay.commitBufferedIfDue(context.Background())

	assert.Zero(t, stub.calls, "a chunk still being spoken must not be committed")
	assert.NotEmpty(t, relay.pending)
}

// A recognizer failure is logged and the audio is dropped rather than retried
// forever: the next chunk starts a new one.
func TestCommitBufferedIfDue_TranscribeFailureDropsTheChunk(t *testing.T) {
	prev := GetSTTProvider()
	SetSTTProvider(&stubSTT{err: errors.New("boom")})
	t.Cleanup(func() { SetSTTProvider(prev) })

	relay := &notesStreamRelay{channel: "mic"}
	relay.pending = make([]byte, notesStreamSampleRate)
	relay.pendingFrom = time.Now().Add(-time.Hour)
	relay.lastVoiceAt = time.Now().Add(-time.Hour)

	assert.NotPanics(t, func() { relay.commitBufferedIfDue(context.Background()) })
	assert.Empty(t, relay.pending, "the failed chunk is consumed, not kept forever")
}
