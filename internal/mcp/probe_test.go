package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// writeScript drops an executable script into a temp dir and returns its path.
// Using a real child process (rather than a fake transport) is the point: the
// stdio path is the one whose framing, stderr capture and process reaping can
// actually break.
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o755))
	return p
}

// stdioServer is a minimal MCP server: it answers initialize and tools/list on
// newline-delimited JSON-RPC and ignores notifications.
const stdioServer = `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"initialize"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"probe-test","version":"9.9"}}}'
      ;;
    *'"tools/list"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"alpha"},{"name":"beta"}]}}'
      ;;
  esac
done
`

func TestProbe_StdioSucceeds(t *testing.T) {
	script := writeScript(t, "server.sh", stdioServer)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{
		Name:      "probe-test",
		Transport: model.MCPTransportStdio,
		Command:   script,
	})

	require.True(t, res.OK, "probe must succeed, got: %s", res.Message)
	assert.Equal(t, "probe-test", res.ServerName)
	assert.Equal(t, "9.9", res.ServerVersion)
	assert.Equal(t, "2024-11-05", res.ProtocolVersion)
	assert.Equal(t, 2, res.ToolCount)
	assert.Equal(t, []string{"alpha", "beta"}, res.Tools)
	assert.Contains(t, res.Message, "2 tool(s)")
	assert.Positive(t, res.DurationMS)
}

// TestProbe_StdioReportsStderr is the case that makes a probe worth having: a
// server that dies on startup usually explains itself on stderr, and without
// capturing it the user sees only "connection closed".
func TestProbe_StdioReportsStderr(t *testing.T) {
	script := writeScript(t, "broken.sh", `#!/bin/sh
echo "fatal: MCP_TOKEN is not set" >&2
exit 1
`)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{
		Name: "broken", Transport: model.MCPTransportStdio, Command: script,
	})

	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "MCP_TOKEN is not set",
		"the child's stderr is the only explanation the user gets")
}

// TestProbe_StdioCommandNotFound asserts the most common configuration mistake
// is reported by name rather than as a generic start failure.
func TestProbe_StdioCommandNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{
		Name: "missing", Transport: model.MCPTransportStdio,
		Command: "clawbench-no-such-mcp-binary",
	})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "not found in PATH")
}

// TestProbe_StdioNonMCPProcess asserts a process that starts but does not speak
// MCP is rejected. This is the failure mode a socket-level check would miss.
func TestProbe_StdioNonMCPProcess(t *testing.T) {
	// Reads input and says nothing useful, then exits.
	script := writeScript(t, "silent.sh", `#!/bin/sh
while IFS= read -r line; do
  printf '%s\n' 'not json at all'
done
`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{
		Name: "silent", Transport: model.MCPTransportStdio, Command: script,
	})
	assert.False(t, res.OK, "a process that never answers initialize must fail")
}

// TestProbe_StdioZeroTools asserts zero tools is still a successful handshake
// but says so plainly: the agent would have nothing to call.
func TestProbe_StdioZeroTools(t *testing.T) {
	script := writeScript(t, "notools.sh", `#!/bin/sh
while IFS= read -r line; do
  case "$line" in
    *'"initialize"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"bare","version":"1"}}}'
      ;;
    *'"tools/list"'*)
      printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
      ;;
  esac
done
`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "bare", Transport: model.MCPTransportStdio, Command: script})
	require.True(t, res.OK)
	assert.Equal(t, 0, res.ToolCount)
	assert.Contains(t, res.Message, "0 tools")
}

func TestProbe_HTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = decodeBody(r, &req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"http-srv","version":"2.0"}}}`)
		case "tools/list":
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"search"}]}}`)
		default:
			// Notifications get an empty 202, as the spec allows.
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{
		Name: "http-srv", Transport: model.MCPTransportHTTP, URL: srv.URL,
	})
	require.True(t, res.OK, res.Message)
	assert.Equal(t, "http-srv", res.ServerName)
	assert.Equal(t, []string{"search"}, res.Tools)
}

// TestProbe_HTTPNotMCP asserts a URL that answers 200 with HTML is rejected
// with a hint, rather than being parsed as a successful handshake.
func TestProbe_HTTPNotMCP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<!doctype html><html>hello</html>")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "web", Transport: model.MCPTransportHTTP, URL: srv.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "not JSON-RPC")
}

// TestProbe_HTTPErrorStatus asserts the body of a non-2xx reply is surfaced:
// a gateway's 401 body names the missing header, the status alone does not.
func TestProbe_HTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"missing Authorization header"}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "auth", Transport: model.MCPTransportHTTP, URL: srv.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "401")
	assert.Contains(t, res.Message, "Authorization")
}

// TestProbe_SSEEnvelope asserts an SSE-framed reply is unwrapped. Streamable
// HTTP servers commonly answer with text/event-stream.
func TestProbe_SSEEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = decodeBody(r, &req)
		w.Header().Set("Content-Type", "text/event-stream")
		switch req.Method {
		case "initialize":
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\",\"serverInfo\":{\"name\":\"sse-srv\",\"version\":\"3\"}}}\n\n")
		case "tools/list":
			fmt.Fprint(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"t1\"}]}}\n\n")
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "sse-srv", Transport: model.MCPTransportSSE, URL: srv.URL})
	require.True(t, res.OK, res.Message)
	assert.Equal(t, "sse-srv", res.ServerName)
	assert.Equal(t, []string{"t1"}, res.Tools)
}

// TestProbe_InvalidServerIsRejected asserts the probe refuses an unvalidatable
// definition instead of attempting to start it.
func TestProbe_InvalidServerIsRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "bad", Transport: "carrier-pigeon"})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "transport")
}

func TestProbe_UnknownTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// ValidateMCPServers rejects this, so the switch's default is unreachable
	// through Probe — but the message must still be sane if it ever is.
	res := Probe(ctx, model.MCPServer{Name: "x", Transport: ""})
	assert.False(t, res.OK)
}

// decodeBody reads a JSON request body into v.
func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
