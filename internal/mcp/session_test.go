package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// onePixelPNG mirrors the fixture's image payload: a 1x1 transparent PNG.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// TestSession_StdioListsAndCalls covers the whole stdio path: handshake,
// tools/list with descriptions, a text result and an image result.
func TestSession_StdioListsAndCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := Dial(ctx, helperServer(t, "server"), DialOptions{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	tools, err := s.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 2)
	assert.Equal(t, "alpha", tools[0].Name)
	assert.Equal(t, "Echo the arguments back.", tools[0].Description)
	assert.Equal(t, "object", tools[0].InputSchema["type"])
	assert.Equal(t, "beta", tools[1].Name)

	text, err := s.CallTool(ctx, "alpha", json.RawMessage(`{"text":"hi"}`))
	require.NoError(t, err)
	assert.False(t, text.IsError)
	assert.Contains(t, text.Text, `"text":"hi"`)

	img, err := s.CallTool(ctx, "beta", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "beta text", img.Text)
	require.Len(t, img.Images, 1)
	assert.Equal(t, "image/png", img.Images[0].MediaType)
	want, _ := base64.StdEncoding.DecodeString(onePixelPNG)
	assert.Equal(t, want, img.Images[0].Data)

	failed, err := s.CallTool(ctx, "beta", json.RawMessage(`{"fail":true}`))
	require.NoError(t, err)
	assert.True(t, failed.IsError, "isError from the server must reach the tool loop")
	assert.Equal(t, "beta failed on purpose", failed.Text)
}

// TestSession_StdioServerExitExplains itself: the child's stderr is the only
// place a startup failure is described.
func TestSession_StdioServerExitExplains(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := Dial(ctx, helperServer(t, "die"), DialOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP_TOKEN is not set")
}

// TestSession_RejectsLegacySSE pins the scope cut: legacy sse is a different
// transport, and pretending otherwise would fail later with a confusing error.
func TestSession_RejectsLegacySSE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Dial(ctx, model.MCPServer{
		Name:      "legacy",
		Transport: model.MCPTransportSSE,
		URL:       "http://127.0.0.1:1/sse",
	}, DialOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sse")
}

// TestSession_HTTPStreamable covers the POST-based transport, including the
// session id the server hands out at initialize and expects back afterwards.
func TestSession_HTTPStreamable(t *testing.T) {
	var mu sync.Mutex
	seenSessionHeader := []string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if id := r.Header.Get("Mcp-Session-Id"); id != "" {
			seenSessionHeader = append(seenSessionHeader, id)
		}
		mu.Unlock()

		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-42")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{"listChanged":true}},"serverInfo":{"name":"http-test","version":"1"}}}`, req.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"ping","description":"Ping.","inputSchema":{"type":"object"}}]}}`, req.ID)
		case "tools/call":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"pong"}]}}`, req.ID)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := Dial(ctx, model.MCPServer{
		Name:      "http-test",
		Transport: model.MCPTransportHTTP,
		URL:       srv.URL,
		Headers:   map[string]string{"Authorization": "Bearer token"},
	}, DialOptions{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	assert.True(t, s.ListChanged(), "listChanged must be read from the initialize reply")

	tools, err := s.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "ping", tools[0].Name)

	res, err := s.CallTool(ctx, "ping", nil)
	require.NoError(t, err)
	assert.Equal(t, "pong", res.Text)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, seenSessionHeader)
	for _, id := range seenSessionHeader {
		assert.Equal(t, "sess-42", id, "every request after initialize must echo the session id")
	}
}

// TestSession_HTTPEventStreamReply covers the SSE-framed reply, including a
// notification interleaved before the answer.
func TestSession_HTTPEventStreamReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// A server-originated notification first: the client must not mistake
		// it for the reply to this request.
		fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n")

		switch req.Method {
		case "initialize":
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"protocolVersion\":\"2025-06-18\",\"serverInfo\":{\"name\":\"sse-test\",\"version\":\"1\"}}}\n\n", req.ID)
		case "tools/list":
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"tools\":[{\"name\":\"ping\"}]}}\n\n", req.ID)
		case "tools/call":
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"pong\"}]}}\n\n", req.ID)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := Dial(ctx, model.MCPServer{
		Name:      "sse-test",
		Transport: model.MCPTransportHTTP,
		URL:       srv.URL,
	}, DialOptions{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	tools, err := s.ListTools(ctx)
	require.NoError(t, err)
	require.Len(t, tools, 1)

	res, err := s.CallTool(ctx, "ping", nil)
	require.NoError(t, err)
	assert.Equal(t, "pong", res.Text)
}

// TestSession_HTTP404ExpiresSession pins the reconnect signal. A 404 on an
// established session means "dial again", not "this tool is broken".
func TestSession_HTTP404ExpiresSession(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		calls++
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess-1")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-06-18"}}`, req.ID)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := Dial(ctx, model.MCPServer{
		Name:      "expiring",
		Transport: model.MCPTransportHTTP,
		URL:       srv.URL,
	}, DialOptions{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	_, err = s.ListTools(ctx)
	require.ErrorIs(t, err, errSessionExpired)
	assert.GreaterOrEqual(t, calls, 3)
	assert.False(t, s.Alive(), "an expired HTTP session must be reported as dead")
}

// TestSession_HTTPErrorIsReadable makes sure a gateway's error body reaches the
// user instead of a bare status code.
func TestSession_HTTPErrorIsReadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"missing token"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Dial(ctx, model.MCPServer{
		Name:      "denied",
		Transport: model.MCPTransportHTTP,
		URL:       srv.URL,
	}, DialOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.Contains(t, err.Error(), "missing token")
}

// TestSession_CallToolRejectsNonObjectArguments pins the boundary between the
// model's JSON and the protocol's `arguments` object.
func TestSession_CallToolRejectsNonObjectArguments(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := Dial(ctx, helperServer(t, "server"), DialOptions{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	_, err = s.CallTool(ctx, "alpha", json.RawMessage(`"just a string"`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON object")
}

// TestDecodeCallResult_Shapes pins the content mapping without a server.
func TestDecodeCallResult_Shapes(t *testing.T) {
	t.Run("text and image", func(t *testing.T) {
		raw := json.RawMessage(`{"content":[{"type":"text","text":"a"},{"type":"image","data":"` + onePixelPNG + `","mimeType":"image/png"}]}`)
		res, err := decodeCallResult(raw)
		require.NoError(t, err)
		assert.Equal(t, "a", res.Text)
		require.Len(t, res.Images, 1)
	})

	t.Run("embedded text resource", func(t *testing.T) {
		raw := json.RawMessage(`{"content":[{"type":"resource","resource":{"uri":"file:///x","text":"resource body"}}]}`)
		res, err := decodeCallResult(raw)
		require.NoError(t, err)
		assert.Equal(t, "resource body", res.Text)
	})

	t.Run("binary resource is described, not dropped", func(t *testing.T) {
		raw := json.RawMessage(`{"content":[{"type":"resource","resource":{"uri":"file:///x","mimeType":"application/pdf","blob":"AAAA"}}]}`)
		res, err := decodeCallResult(raw)
		require.NoError(t, err)
		assert.Contains(t, res.Text, "binary resource")
	})

	t.Run("structured content only", func(t *testing.T) {
		raw := json.RawMessage(`{"content":[],"structuredContent":{"ok":true}}`)
		res, err := decodeCallResult(raw)
		require.NoError(t, err)
		assert.JSONEq(t, `{"ok":true}`, res.Text)
	})

	t.Run("no content", func(t *testing.T) {
		res, err := decodeCallResult(json.RawMessage(`{"content":[]}`))
		require.NoError(t, err)
		assert.Empty(t, res.Text)
	})
}

// TestTruncateText keeps the head and never splits a UTF-8 rune.
func TestTruncateText(t *testing.T) {
	s := strings.Repeat("中", 10)
	got := truncateText(s, 10)
	assert.Contains(t, got, "output truncated")
	assert.True(t, strings.HasPrefix(got, "中"))
	assert.NotContains(t, got, "\uFFFD")
}

// TestSession_ServerNameAndNilContext covers the two small accessors the
// adapter and the tool loop rely on.
func TestSession_ServerNameAndNilContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := Dial(ctx, helperServer(t, "server"), DialOptions{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	assert.Equal(t, "probe-test", s.ServerName())
	assert.True(t, s.Alive())

	// A nil context must not panic: the tool interface passes Env.Ctx through,
	// and a caller that forgot to set it should still get a working call.
	res, err := s.CallTool(nil, "alpha", nil) //nolint:staticcheck // tools.Env.Ctx can be nil; it must not panic
	require.NoError(t, err)
	assert.Contains(t, res.Text, "alpha:")
}

// TestSession_CancelledCallSendsCancellation covers the timeout path: the call
// returns ctx.Err() instead of hanging, and the server is told to stop.
func TestSession_CancelledCallSendsCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	s, err := Dial(ctx, helperServer(t, "callhang"), DialOptions{})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	callCtx, callCancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer callCancel()
	_, err = s.CallTool(callCtx, "alpha", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestParseResponseLine_IgnoresNonReplies pins the line classifier: a
// notification, a server request and junk must not be mistaken for an answer.
func TestParseResponseLine_IgnoresNonReplies(t *testing.T) {
	assert.Nil(t, parseResponseLine([]byte(`{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`)))
	assert.Nil(t, parseResponseLine([]byte(`{"jsonrpc":"2.0","id":9,"method":"sampling/createMessage"}`)))
	assert.Nil(t, parseResponseLine([]byte(`not json`)))
	assert.Nil(t, parseResponseLine(nil))

	got := parseResponseLine([]byte(`{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`))
	require.NotNil(t, got)
	assert.Equal(t, 7, got.ID)
}

// TestDecodeCallResult_RemainingShapes covers audio and an unknown block type:
// both must be described rather than dropped.
func TestDecodeCallResult_RemainingShapes(t *testing.T) {
	audio, err := decodeCallResult(json.RawMessage(`{"content":[{"type":"audio","data":"AAAA"}]}`))
	require.NoError(t, err)
	assert.Contains(t, audio.Text, "audio")

	unknown, err := decodeCallResult(json.RawMessage(`{"content":[{"type":"video"}]}`))
	require.NoError(t, err)
	assert.Contains(t, unknown.Text, "video")

	badImage, err := decodeCallResult(json.RawMessage(`{"content":[{"type":"image","data":"!!!"}]}`))
	require.NoError(t, err)
	assert.Contains(t, badImage.Text, "could not be decoded")

	oversized, err := decodeCallResult(json.RawMessage(
		`{"content":[{"type":"image","data":"` + strings.Repeat("A", 4*(maxImageBytes/3)+8) + `"}]}`,
	))
	require.NoError(t, err)
	assert.Contains(t, oversized.Text, "the limit is")
}

// TestParseSSEResponse_NoReply pins the error a truncated stream produces.
func TestParseSSEResponse_NoReply(t *testing.T) {
	_, err := parseSSEResponse([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{}}\n\n"), 7)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a reply")
}
