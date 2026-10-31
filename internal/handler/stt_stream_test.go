package handler

import (
	"context"
	"encoding/json"
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
	session *fakeStreamSession

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
