package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/stt"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStreamSession is a StreamingSTTProvider session stub: every Write yields
// one partial result, Finish yields the final one.
type fakeStreamSession struct {
	results chan stt.StreamResult
	finish  chan struct{}

	mu     sync.Mutex
	writes [][]byte
	once   sync.Once
}

func (s *fakeStreamSession) Write(pcm []byte) error {
	s.mu.Lock()
	s.writes = append(s.writes, append([]byte(nil), pcm...))
	s.mu.Unlock()
	s.results <- stt.StreamResult{Text: "部分"}
	return nil
}

func (s *fakeStreamSession) Finish() error {
	s.once.Do(func() { close(s.finish) })
	s.results <- stt.StreamResult{Text: "部分完整", Final: true}
	close(s.results)
	return nil
}

func (s *fakeStreamSession) Results() <-chan stt.StreamResult { return s.results }
func (s *fakeStreamSession) Close()                           {}

func (s *fakeStreamSession) written() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// fakeStreamProvider implements stt.StreamingSTTProvider.
type fakeStreamProvider struct {
	session stt.StreamSession

	mu   sync.Mutex
	opts stt.StreamOptions
}

func (p *fakeStreamProvider) Transcribe(context.Context, io.Reader, string) (string, error) {
	return "", nil
}

func (p *fakeStreamProvider) OpenStream(_ context.Context, opts stt.StreamOptions) (stt.StreamSession, error) {
	p.mu.Lock()
	p.opts = opts
	p.mu.Unlock()
	return p.session, nil
}

func (p *fakeStreamProvider) options() stt.StreamOptions {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opts
}

// TestSTTTranscribeWS_StreamingProvider covers the live-streaming path: the
// start control frame configures the session, binary frames are relayed as PCM,
// and results are forwarded with replace semantics ("partial") before "done".
func TestSTTTranscribeWS_StreamingProvider(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)

	session := &fakeStreamSession{
		results: make(chan stt.StreamResult, 8),
		finish:  make(chan struct{}),
	}
	provider := &fakeStreamProvider{session: session}
	SetSTTProvider(provider)

	srv := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	require.NoError(t, conn.Write(ctx, websocket.MessageText,
		[]byte(`{"type":"start","sampleRate":16000,"channels":1,"bits":16,"context":"已有文字"}`)))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3, 4}))
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end"}`)))

	var msgs []sttWSServerMsg
	for {
		_, data, rerr := conn.Read(ctx)
		if rerr != nil {
			break
		}
		var m sttWSServerMsg
		require.NoError(t, json.Unmarshal(data, &m))
		msgs = append(msgs, m)
		if m.Type == sttWSDone {
			break
		}
	}

	assert.Equal(t, "已有文字", provider.options().Context, "start frame context must reach the engine")
	assert.Equal(t, [][]byte{{1, 2, 3, 4}}, session.written(), "binary frames must be relayed as PCM")

	require.NotEmpty(t, msgs)
	assert.Equal(t, sttWSPartial, msgs[0].Type)
	assert.Equal(t, "部分", msgs[0].Text)
	last := msgs[len(msgs)-1]
	assert.Equal(t, sttWSDone, last.Type)
	assert.Equal(t, "部分完整", last.Final)
}

// TestSTTTranscribeWS_EndWithoutAudio covers the guard for a client that ends
// the stream before sending any audio.
func TestSTTTranscribeWS_EndWithoutAudio(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&fakeStreamProvider{session: &fakeStreamSession{
		results: make(chan stt.StreamResult, 8),
		finish:  make(chan struct{}),
	}})

	srv := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end"}`)))

	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	var m sttWSServerMsg
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, sttWSDone, m.Type)
	assert.Empty(t, m.Final)
}

// errStreamSession reports a terminal engine failure (bad key, quota, rejected
// frame) on the first audio frame, the way the Doubao session does when it
// receives a 0xF error frame.
type errStreamSession struct {
	results chan stt.StreamResult
	once    sync.Once
}

func (s *errStreamSession) Write([]byte) error {
	s.once.Do(func() {
		s.results <- stt.StreamResult{Err: errors.New("stt: doubao stream error: code 45000001: unauthorized")}
		close(s.results)
	})
	return nil
}

func (s *errStreamSession) Finish() error                    { return nil }
func (s *errStreamSession) Results() <-chan stt.StreamResult { return s.results }
func (s *errStreamSession) Close()                           {}

// TestSTTTranscribeWS_EngineErrorSurfaces covers the failure path: an engine
// error must reach the client as {"type":"error"} instead of a clean
// {"type":"done","final":""} that silently discards the whole recording.
func TestSTTTranscribeWS_EngineErrorSurfaces(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)

	SetSTTProvider(&fakeStreamProvider{session: &errStreamSession{
		results: make(chan stt.StreamResult, 8),
	}})

	srv := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3, 4}))

	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	var m sttWSServerMsg
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, sttWSError, m.Type)
	assert.Contains(t, m.Message, "45000001")
	assert.Contains(t, m.Message, "unauthorized")

	// Nothing else may follow — in particular no empty "done".
	readCtx, cancelRead := context.WithTimeout(ctx, 200*time.Millisecond)
	_, extra, extraErr := conn.Read(readCtx)
	cancelRead()
	assert.Error(t, extraErr, "the relay must not send a final result after an engine error")
	assert.Empty(t, extra)
}

// TestApplySTTStreamOptions covers the overlay rules.
func TestApplySTTStreamOptions(t *testing.T) {
	base := stt.StreamOptions{SampleRate: 16000, Channels: 1, Bits: 16}

	got := applySTTStreamOptions(base, sttWSControl{Type: sttWSStartCtl})
	assert.Equal(t, base, got, "an empty start frame must not clobber the defaults")

	got = applySTTStreamOptions(base, sttWSControl{
		Type: sttWSStartCtl, SampleRate: 8000, Channels: 2, Bits: 8, Context: "上下文",
	})
	assert.Equal(t, 8000, got.SampleRate)
	assert.Equal(t, 2, got.Channels)
	assert.Equal(t, 8, got.Bits)
	assert.Equal(t, "上下文", got.Context)
}

// failingStreamProvider fails to open a session, the way the Doubao adapter does
// when the credentials or the endpoint are wrong.
type failingStreamProvider struct{ err error }

func (p *failingStreamProvider) Transcribe(context.Context, io.Reader, string) (string, error) {
	return "", p.err
}

func (p *failingStreamProvider) OpenStream(context.Context, stt.StreamOptions) (stt.StreamSession, error) {
	return nil, p.err
}

// TestSTTTranscribeWS_OpenStreamFailure: an engine that cannot be dialed must
// reach the client as {"type":"error"} — a silent close looks like a network
// blip and the recording is lost with no explanation.
func TestSTTTranscribeWS_OpenStreamFailure(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&failingStreamProvider{err: errors.New("stt: dial failed: bad key")})

	srv := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	// The session is opened lazily on the first audio frame.
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3, 4}))

	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	var m sttWSServerMsg
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, sttWSError, m.Type)
	assert.Contains(t, m.Message, "bad key")
}

// TestSTTTranscribeWS_IgnoresUnusableControlFrames: a text frame that is not
// JSON, or JSON with an unknown type, is a client-side mistake. It must be
// ignored — killing the stream there would drop the audio the user is still
// speaking.
func TestSTTTranscribeWS_IgnoresUnusableControlFrames(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)

	session := &fakeStreamSession{
		results: make(chan stt.StreamResult, 8),
		finish:  make(chan struct{}),
	}
	SetSTTProvider(&fakeStreamProvider{session: session})

	srv := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()

	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`not json at all`)))
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resume"}`)))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3, 4}))
	require.NoError(t, conn.Write(ctx, websocket.MessageText, []byte(`{"type":"end"}`)))

	var last sttWSServerMsg
	for {
		_, data, rerr := conn.Read(ctx)
		if rerr != nil {
			break
		}
		var m sttWSServerMsg
		require.NoError(t, json.Unmarshal(data, &m))
		last = m
		if m.Type == sttWSDone {
			break
		}
	}

	assert.Equal(t, sttWSDone, last.Type, "the stream must survive unusable control frames")
	assert.Equal(t, "部分完整", last.Final)
	assert.Equal(t, [][]byte{{1, 2, 3, 4}}, session.written(), "audio after a bad control frame is still relayed")
}

// TestSTTStreamRelay_UnknownMessageType: MessageType is an enum, so the default
// arm is only reachable with a value the library cannot produce. It must be
// inert rather than a panic or a closed stream.
func TestSTTStreamRelay_UnknownMessageType(t *testing.T) {
	relay := &sttStreamRelay{provider: &fakeStreamProvider{}}

	done, err := relay.handle(context.Background(), websocket.MessageType(99), []byte("x"))

	assert.NoError(t, err)
	assert.False(t, done)
	assert.Nil(t, relay.session, "an unknown frame must not open a session")
}

// TestSTTStreamRelay_AudioLimit: the total audio budget is checked BEFORE the
// frame reaches the engine, so a runaway client cannot keep the stream (and the
// engine's billing) alive indefinitely.
func TestSTTStreamRelay_AudioLimit(t *testing.T) {
	relay := &sttStreamRelay{
		provider: &fakeStreamProvider{session: &fakeStreamSession{
			results: make(chan stt.StreamResult, 1),
			finish:  make(chan struct{}),
		}},
		total: sttWSMaxAudioBytes, // already at the ceiling
	}

	err := relay.writeAudio(context.Background(), []byte{1})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "audio exceeds limit")
	assert.Nil(t, relay.session, "the over-budget frame must not be forwarded")
}

// TestSTTStreamRelay_ForwardResultsStopsOnDeadConnection: once the client is
// gone, every further write fails. The forwarder must stop on the first failure
// instead of spinning on a dead socket (and instead of blocking the "end"
// handler that waits for it).
func TestSTTStreamRelay_ForwardResultsStopsOnDeadConnection(t *testing.T) {
	connCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		connCh <- c
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()

	serverConn := <-connCh
	serverConn.CloseNow() // the peer is gone; writes must fail from here on

	results := make(chan stt.StreamResult, 1)
	results <- stt.StreamResult{Text: "部分"}
	close(results)

	relay := &sttStreamRelay{
		conn:    serverConn,
		session: &fakeStreamSession{results: results, finish: make(chan struct{})},
	}
	relay.results.Add(1)

	done := make(chan struct{})
	go func() {
		relay.forwardResults()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("forwardResults must return when the connection is gone")
	}
}
