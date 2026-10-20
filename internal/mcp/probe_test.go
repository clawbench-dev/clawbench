package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func TestProbe_StdioSucceeds(t *testing.T) {
	srv := helperServer(t, "server")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, srv)

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
	srv := helperServer(t, "die")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, srv)

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
	srv := helperServer(t, "silent")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res := Probe(ctx, srv)
	assert.False(t, res.OK, "a process that never answers initialize must fail")
}

// A stdio server whose launcher spawns a child that inherits the pipes must
// still return promptly.
//
// This is the `uvx mcp-server-time` shape: uvx starts the real server as a
// child, both inherit our stdout/stderr. Killing only the launcher leaves the
// child holding the pipes, so cmd.Wait blocks on the copy goroutines forever —
// the handshake and tools/list had already succeeded, yet the HTTP test
// endpoint never answered and the UI reported a timeout. The probe must reap
// the whole process group instead.
func TestProbe_StdioLauncherWithPipeHoldingChildReturns(t *testing.T) {
	srv := helperServer(t, "spawn")

	// Generous relative to probeWaitDelay (2s) but far below the child's 60s
	// sleep: without the group kill this hangs for the full minute.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan Result, 1)
	go func() { done <- Probe(ctx, srv) }()

	select {
	case res := <-done:
		require.True(t, res.OK, "probe must succeed, got: %s", res.Message)
		assert.Equal(t, 2, res.ToolCount)
	case <-time.After(15 * time.Second):
		t.Fatal("probe did not return — the pipe-holding grandchild kept cmd.Wait blocked")
	}
}

// A launcher whose child inherits the pipes must also unblock on TIMEOUT.
//
// Here the server never answers, so the probe is parked in readResponse when
// the deadline fires. Killing only the leader would leave the child holding
// stdout open, the pending read would never see EOF, and the probe would
// outlive its own context — the exact shape of "test runs forever".
func TestProbe_StdioTimeoutWithPipeHoldingChildReturns(t *testing.T) {
	srv := helperServer(t, "spawnsilent")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan Result, 1)
	go func() { done <- Probe(ctx, srv) }()

	select {
	case res := <-done:
		assert.False(t, res.OK, "a server that never answers must not probe OK")
	case <-time.After(10 * time.Second):
		t.Fatal("probe outlived its context — the pipe-holding grandchild kept the read blocked")
	}
}

// TestProbe_StdioZeroTools asserts zero tools is still a successful handshake
// but says so plainly: the agent would have nothing to call.
func TestProbe_StdioZeroTools(t *testing.T) {
	srv := helperServer(t, "notools")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, srv)
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

// ── Helpers ──────────────────────────────────────────────────────────────────

// firstSSEData extracts the payload of the first `data:` line. A server may
// prefix the stream with event/id/retry lines, so the scan cannot assume the
// payload is the first line.
func TestFirstSSEData(t *testing.T) {
	assert.Nil(t, firstSSEData(nil), "no lines → no payload")
	assert.Nil(t, firstSSEData([]byte("event: message\nretry: 100\n")))
	assert.Equal(t, `{"a":1}`,
		string(firstSSEData([]byte("event: message\ndata: {\"a\":1}\ndata: {\"b\":2}\n"))),
		"only the FIRST data line is the response")
	assert.Equal(t, `{"a":1}`, string(firstSSEData([]byte("data:   {\"a\":1}   \n"))),
		"surrounding whitespace is trimmed")
}

// snippet keeps an error message on one line and bounded: a server's HTML error
// page or a multi-line traceback must not blow up the banner.
func TestSnippet(t *testing.T) {
	assert.Equal(t, "one two three", snippet([]byte("one\n\ttwo   three")))
	assert.Empty(t, snippet(nil))

	long := strings.Repeat("x", 500)
	got := snippet([]byte(long))
	assert.Len(t, []rune(got), 201, "200 characters plus the ellipsis")
	assert.True(t, strings.HasSuffix(got, "…"))
}

// withStderr appends the child's stderr — the only place a stdio server explains
// a startup failure — and leaves the message untouched when it captured nothing.
func TestWithStderr(t *testing.T) {
	assert.Equal(t, "boom", withStderr("boom", &bytes.Buffer{}))

	var buf bytes.Buffer
	buf.WriteString("  missing API_KEY\n")
	got := withStderr("boom", &buf)
	assert.Contains(t, got, "boom")
	assert.Contains(t, got, "server stderr")
	assert.Contains(t, got, "missing API_KEY")
	assert.NotContains(t, got, "\n", "the appended stderr must stay on one line")
}

// Zero tools is a VALID server (it may expose only resources or prompts) but
// almost always a misconfiguration, so the message must say so rather than
// claiming a plain success.
func TestToolsMessage(t *testing.T) {
	assert.Contains(t, toolsMessage(0), "0 tools")
	assert.Contains(t, toolsMessage(3), "3 tool(s)")
}

// The tool list is capped so a server exposing hundreds of tools cannot bloat
// the settings response.
func TestTruncateTools(t *testing.T) {
	small := []string{"a", "b"}
	assert.Equal(t, small, truncateTools(small), "a short list is returned as-is")

	many := make([]string, maxToolsListed+5)
	for i := range many {
		many[i] = fmt.Sprintf("t%d", i)
	}
	assert.Len(t, truncateTools(many), maxToolsListed)
}

func TestEnvPairs(t *testing.T) {
	assert.Nil(t, envPairs(nil))
	assert.Nil(t, envPairs(map[string]string{}))
	assert.Equal(t, []string{"K=v"}, envPairs(map[string]string{"K": "v"}))
}

func TestRPCError_Message(t *testing.T) {
	err := &rpcError{Code: -32601, Message: "method not found"}
	assert.Equal(t, "method not found (code -32601)", err.Error())
}

// ── Failure paths ────────────────────────────────────────────────────────────

// An empty body from a server that answers 200 must be named as such: it is the
// signature of a streamable-HTTP endpoint that expects a different Accept.
func TestProbe_HTTPEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "empty", Transport: model.MCPTransportHTTP, URL: srv.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "empty response body")
}

// A JSON-RPC error reply must be reported as the server's own message, not as
// a parse failure: the code and message are what the user needs to fix it.
func TestProbe_HTTPRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"initialize not supported"}}`)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "rpc-err", Transport: model.MCPTransportHTTP, URL: srv.URL})
	assert.False(t, res.OK)
	assert.Contains(t, res.Message, "initialize not supported")
	assert.Contains(t, res.Message, "-32601")
}

// A tools/list failure still reports the server's identity: the handshake
// succeeded, so telling the user which server answered is useful.
func TestProbe_HTTPToolsListFailsKeepsIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = decodeBody(r, &req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"half","version":"1"}}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "tools exploded")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{Name: "half", Transport: model.MCPTransportHTTP, URL: srv.URL})
	assert.False(t, res.OK)
	assert.Equal(t, "half", res.ServerName, "the handshake did succeed, so report who answered")
	assert.Contains(t, res.Message, "500")
	assert.Contains(t, res.Message, "tools exploded")
}

// Headers are forwarded: a server behind an auth gateway needs them, and a
// silent drop would look like a server-side rejection.
func TestProbe_HTTPForwardsHeaders(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		_ = decodeBody(r, &req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","serverInfo":{"name":"auth-srv","version":"1"}}}`)
		case "tools/list":
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, model.MCPServer{
		Name: "auth-srv", Transport: model.MCPTransportHTTP, URL: srv.URL,
		Headers: map[string]string{"Authorization": "Bearer tok"},
	})
	require.True(t, res.OK, res.Message)
	assert.Equal(t, "Bearer tok", gotAuth)
	assert.Contains(t, res.Message, "0 tools", "a server with no tools is valid but flagged")
}

// tryParseResponse must ignore anything that is not OUR reply: notifications,
// server-initiated requests, blank lines, malformed JSON, and replies to another
// id. Mistaking one for the answer would read the wrong result.
func TestTryParseResponse(t *testing.T) {
	assert.Nil(t, tryParseResponse([]byte("   \n"), 1), "blank")
	assert.Nil(t, tryParseResponse([]byte("not json"), 1))
	assert.Nil(t, tryParseResponse([]byte(`{"jsonrpc":"2.0","method":"x"}`), 1),
		"a notification has no id")
	assert.Nil(t, tryParseResponse([]byte(`{"jsonrpc":"2.0","id":2,"method":"x","result":{}}`), 1),
		"a server-initiated request has a method")
	assert.Nil(t, tryParseResponse([]byte(`{"jsonrpc":"2.0","id":9,"result":{}}`), 1),
		"a reply to another id")

	got := tryParseResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`), 1)
	require.NotNil(t, got)
	assert.Equal(t, 1, got.ID)
	assert.Nil(t, got.Error)
}

// A reply without a trailing newline is still a reply: some servers close the
// stream right after the last message.
func TestReadResponse_LastLineWithoutNewline(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	resp, err := readResponse(r, 1, &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, 1, resp.ID)
}

// A server that closes without replying must say so, and name the cause rather
// than reporting a generic parse failure.
func TestReadResponse_ClosedBeforeReply(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0","method":"noise"}` + "\n"))
	_, err := readResponse(r, 7, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed the connection before replying")
}

// A stdio server that completes the handshake but fails tools/list still reports
// its identity: the connection worked, so naming the server helps the user tell
// "wrong server" apart from "server is broken".
func TestProbe_StdioToolsListFailsKeepsIdentity(t *testing.T) {
	srv := helperServer(t, "half")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Probe(ctx, srv)
	assert.False(t, res.OK)
	assert.Equal(t, "probe-test", res.ServerName, "the handshake succeeded, so report who answered")
	assert.Contains(t, res.Message, "tools/list")
}

// A server that dies immediately must ALWAYS report its stderr. This is a
// regression guard for a race: reading the stderr buffer while the child was
// still exiting returned an empty string on some runs (observed on macOS CI),
// which removed the only explanation the user gets. Repeating the probe makes
// the race far more likely to surface than a single shot does.
func TestProbe_StdioReportsStderr_Repeatedly(t *testing.T) {
	srv := helperServer(t, "die")

	for i := range 20 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		res := Probe(ctx, srv)
		cancel()

		if res.OK {
			t.Fatalf("iteration %d: a broken server must not probe OK", i)
		}
		if !strings.Contains(res.Message, "MCP_TOKEN is not set") {
			t.Fatalf("iteration %d: stderr missing from %q", i, res.Message)
		}
	}
}

// The reap-before-read ordering cannot be pinned behaviorally on Linux: the
// child's stderr copy usually finishes before the probe returns, so removing the
// explicit reap still passes here while failing on macOS CI (where it did). Pin
// it structurally instead — every withStderr call in probeStdio must be
// preceded by a reap, or the buffer can still be racing the copy goroutine.
func TestProbeStdio_ReapsBeforeReadingStderr(t *testing.T) {
	src, err := os.ReadFile("probe.go")
	require.NoError(t, err)

	body := string(src)
	start := strings.Index(body, "func probeStdio(")
	require.GreaterOrEqual(t, start, 0, "probeStdio not found — has it been renamed?")
	end := strings.Index(body[start:], "\n// ── ")
	require.Greater(t, end, 0, "could not delimit probeStdio")
	fn := body[start : start+end]

	// EVERY withStderr(...) must be immediately preceded by its own reap() —
	// "some reap happened earlier" is not enough: the failure path that matters
	// is the FIRST one, and an earlier reap on a different branch does not stop
	// the buffer from racing on this one.
	uses := 0
	for i := range len(fn) {
		if !strings.HasPrefix(fn[i:], "withStderr(") {
			continue
		}
		uses++
		// Look back a short window (the call is a few lines below its reap).
		windowStart := max(0, i-200)
		if !strings.Contains(fn[windowStart:i], "reap()") {
			t.Fatalf("withStderr at offset %d has no reap() immediately before it — "+
				"the stderr buffer can still be racing the copy goroutine", i)
		}
	}
	require.GreaterOrEqual(t, uses, 3, "expected at least three withStderr call sites")

	// The reap must use cmd.Wait, not cmd.Process.Wait. Only cmd.Wait waits for
	// the goroutine that copies the child's stderr into the buffer; Process.Wait
	// waits for the OS process alone, so the buffer can still be empty when it
	// is read. That distinction cannot be tested behaviorally on Linux (the copy
	// almost always wins the race here) but failed on CI, so pin it in source.
	require.NotContains(t, fn, "cmd.Process.Wait()",
		"use cmd.Wait(): Process.Wait does not wait for the stderr copy goroutine")
	require.Contains(t, fn, "cmd.Wait()", "the reap must wait for the stderr copy")
}
