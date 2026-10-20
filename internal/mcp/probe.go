// Package mcp implements just enough of the Model Context Protocol client to
// answer one question: "would this server actually work if an agent connected
// to it?"
//
// It is deliberately NOT a general MCP client. It performs the handshake and
// lists tools, then stops — that is exactly what an agent does on connect, and
// it is the only check that proves the server is usable. Configuring an MCP
// server has several failure modes that are invisible until something connects
// (a command that starts but speaks the wrong protocol, a URL that answers 200
// with HTML, a server that accepts the socket and never replies), so a
// connectivity test that only checked "can I open a socket" would report
// success for most of them.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"clawbench/internal/model"
)

// protocolVersion is the MCP revision this client speaks. Servers negotiate
// down, so sending a recent one is safe; sending nothing is not.
const protocolVersion = "2024-11-05"

// jsonrpcVersion is the only JSON-RPC version this probe speaks. It is a
// constant because every request must agree: a server rejects a mismatched
// version outright, and a typo at one call site would look like a server bug.
const jsonrpcVersion = "2.0"

// maxResponseBytes bounds a single JSON-RPC reply. A server that streams an
// unbounded body must not be able to exhaust memory here.
const maxResponseBytes = 8 << 20

// maxToolsListed caps how many tool names are reported back. The full list can
// be hundreds of entries; the UI only shows a summary.
const maxToolsListed = 20

// Result is the outcome of a probe. It is a value, not an error, because every
// failure mode is something the user needs to read (which server, what went
// wrong) rather than an internal error to log.
type Result struct {
	// OK is true only when the handshake completed AND tools were listed.
	OK bool `json:"ok"`
	// Message is a human-readable one-liner, always populated.
	Message string `json:"message"`
	// ServerName / ServerVersion come from the server's initialize reply.
	ServerName    string `json:"server_name,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
	// ProtocolVersion is the version the server agreed to.
	ProtocolVersion string `json:"protocol_version,omitempty"`
	// ToolCount is the number of tools the server exposes.
	ToolCount int `json:"tool_count"`
	// Tools is a truncated list of tool names (first maxToolsListed).
	Tools []string `json:"tools,omitempty"`
	// DurationMS is how long the whole probe took.
	DurationMS int64 `json:"duration_ms"`
}

// rpcRequest is a JSON-RPC 2.0 request. ID 0 is never used so a server that
// replies with `id: 0` cannot be mistaken for an unsolicited notification.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

// initializeResult is the subset of the initialize reply this package reads.
type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

// toolsListResult is the subset of tools/list this package reads.
type toolsListResult struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

// Probe connects to srv, performs the MCP handshake and lists its tools.
//
// The caller supplies the timeout; nothing here blocks longer than it. Every
// failure returns a Result with OK=false and a message naming the concrete
// cause — never a bare "failed".
func Probe(ctx context.Context, srv model.MCPServer) Result {
	start := time.Now()
	finish := func(r Result) Result {
		r.DurationMS = time.Since(start).Milliseconds()
		return r
	}

	if err := model.ValidateMCPServers([]model.MCPServer{srv}); err != nil {
		return finish(Result{Message: err.Error()})
	}

	switch srv.Transport {
	case model.MCPTransportStdio:
		r := probeStdio(ctx, srv)
		return finish(r)
	case model.MCPTransportHTTP, model.MCPTransportSSE:
		// The two share one implementation: the probe is a one-shot request and
		// does not consume the streaming half of the SSE transport, so the only
		// difference is which Accept header the server tolerates — and the
		// request advertises both.
		r := probeHTTP(ctx, srv)
		return finish(r)
	default:
		// ValidateMCPServers already rejected this, so reaching here means the
		// two disagree — say so rather than guessing a transport.
		return finish(Result{Message: fmt.Sprintf("unsupported transport %q", srv.Transport)})
	}
}

// ── stdio ────────────────────────────────────────────────────────────────

// probeStdio starts the command and speaks newline-delimited JSON-RPC over its
// stdin/stdout. MCP stdio framing is one JSON object per line.
func probeStdio(ctx context.Context, srv model.MCPServer) Result {
	path, err := exec.LookPath(srv.Command)
	if err != nil {
		return Result{Message: fmt.Sprintf("command %q not found in PATH", srv.Command)}
	}

	cmd := exec.CommandContext(ctx, path, srv.Args...)
	// Inherit the process environment, then layer the server's own vars on top.
	// A server that needs PATH/HOME (almost all do) breaks if the environment
	// is replaced instead of extended.
	cmd.Env = append(os.Environ(), envPairs(srv.Env)...)

	stdin, pipeErr := cmd.StdinPipe()
	if pipeErr != nil {
		return Result{Message: fmt.Sprintf("cannot open stdin: %v", pipeErr)}
	}
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return Result{Message: fmt.Sprintf("cannot open stdout: %v", pipeErr)}
	}
	// Capture stderr: a server that dies on startup explains itself there, and
	// without this the user sees only "connection closed".
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if startErr := cmd.Start(); startErr != nil {
		return Result{Message: fmt.Sprintf("cannot start %q: %v", srv.Command, startErr)}
	}
	// Always reap. Kill is harmless after a clean exit and guarantees no
	// orphaned child if an early return skipped the graceful path.
	//
	// cmd.Wait (NOT cmd.Process.Wait) is what makes the stderr buffer safe to
	// read: os/exec copies the child's stderr into that buffer from its own
	// goroutine, and only cmd.Wait waits for that copy to finish. Using
	// Process.Wait waits for the OS process alone, so the buffer could still be
	// empty at the instant we read it — which is exactly the "connection closed
	// with no explanation" the user then sees. Reproduced on CI (iteration 8 of
	// TestProbe_StdioReportsStderr_Repeatedly).
	//
	// Once Wait has run the process cannot be waited on again, so the guard is
	// required: both the failure paths and the deferred cleanup call this.
	reaped := false
	reap := func() {
		if reaped {
			return
		}
		reaped = true
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	defer reap()

	reader := bufio.NewReader(stdout)
	call := func(req rpcRequest) (*rpcResponse, error) {
		line, marshalErr := json.Marshal(req)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, writeErr := stdin.Write(append(line, '\n')); writeErr != nil {
			return nil, fmt.Errorf("write request: %w", writeErr)
		}
		return readResponse(reader, req.ID, &stderr)
	}

	init, initResp, err := doInitialize(call)
	if err != nil {
		// Reap before reading stderr (see the reap comment above).
		reap()
		return Result{Message: withStderr(err.Error(), &stderr)}
	}
	// The spec requires the client to confirm with `notifications/initialized`
	// before any other request. A notification carries no id, so there is no
	// reply to wait for.
	if _, writeErr := stdin.Write(append(mustJSON(rpcRequest{
		JSONRPC: jsonrpcVersion, Method: "notifications/initialized",
	}), '\n')); writeErr != nil {
		reap()
		return Result{Message: withStderr(fmt.Sprintf("send initialized: %v", writeErr), &stderr)}
	}

	tools, err := doToolsList(call)
	if err != nil {
		reap()
		return Result{
			Message:         withStderr(err.Error(), &stderr),
			ServerName:      init.ServerInfo.Name,
			ServerVersion:   init.ServerInfo.Version,
			ProtocolVersion: initResp,
		}
	}
	return Result{
		OK:              true,
		Message:         toolsMessage(len(tools)),
		ServerName:      init.ServerInfo.Name,
		ServerVersion:   init.ServerInfo.Version,
		ProtocolVersion: initResp,
		ToolCount:       len(tools),
		Tools:           truncateTools(tools),
	}
}

// readResponse reads lines until it finds the reply with the matching id,
// skipping notifications and server-initiated requests (which this client does
// not implement but must not mistake for the answer).
func readResponse(r *bufio.Reader, id int, stderr *bytes.Buffer) (*rpcResponse, error) {
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			if len(bytes.TrimSpace(line)) > 0 {
				// Some servers omit the trailing newline on the last message.
				if resp := tryParseResponse(line, id); resp != nil {
					return resp, nil
				}
			}
			return nil, fmt.Errorf("server closed the connection before replying: %w", err)
		}
		if resp := tryParseResponse(line, id); resp != nil {
			return resp, nil
		}
		// Not our reply (a notification, a server request, or blank) — keep
		// reading. The deadline comes from ctx, not from a line counter.
		_ = stderr
	}
}

// tryParseResponse returns the response when the line is a JSON-RPC reply with
// the wanted id, and nil for anything else (including malformed lines).
func tryParseResponse(line []byte, id int) *rpcResponse {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var probe struct {
		ID     *int            `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return nil
	}
	// A server-initiated REQUEST has both method and id; a notification has
	// method only. Neither is our reply.
	if probe.Method != "" {
		return nil
	}
	if probe.ID == nil || *probe.ID != id {
		return nil
	}
	return &rpcResponse{ID: *probe.ID, Result: probe.Result, Error: probe.Error}
}

// ── http / sse ───────────────────────────────────────────────────────────

// probeHTTP posts JSON-RPC to the URL. Used for both the http and sse
// transports: the difference only matters for streaming notifications, which a
// one-shot probe does not consume.
func probeHTTP(ctx context.Context, srv model.MCPServer) Result {
	client := &http.Client{Timeout: 20 * time.Second}

	post := func(req rpcRequest) (*rpcResponse, error) {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, err
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json, text/event-stream")
		for k, v := range srv.Headers {
			httpReq.Header.Set(k, v)
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			// Include a slice of the body: a 401 from a gateway explains the
			// problem far better than the status code alone.
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(raw))
		}

		payload := raw
		// An SSE-framed reply arrives as `event: message\ndata: {...}`. Pull the
		// first data line out; a plain JSON body passes through unchanged.
		if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
			payload = firstSSEData(raw)
		}
		if len(payload) == 0 {
			return nil, fmt.Errorf("empty response body")
		}
		var parsed rpcResponse
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, fmt.Errorf("response is not JSON-RPC (is this an MCP endpoint?): %s", snippet(payload))
		}
		if parsed.Error != nil {
			return nil, parsed.Error
		}
		return &parsed, nil
	}

	init, initResp, err := doInitialize(post)
	if err != nil {
		return Result{Message: err.Error()}
	}

	// Best effort: the initialized notification. Some servers ignore it on
	// streamable HTTP; a failure here is not fatal, so the error is dropped.
	_, _ = post(rpcRequest{JSONRPC: jsonrpcVersion, ID: 99, Method: "notifications/initialized"})

	tools, err := doToolsList(post)
	if err != nil {
		return Result{
			Message:         err.Error(),
			ServerName:      init.ServerInfo.Name,
			ServerVersion:   init.ServerInfo.Version,
			ProtocolVersion: initResp,
		}
	}
	return Result{
		OK:              true,
		Message:         toolsMessage(len(tools)),
		ServerName:      init.ServerInfo.Name,
		ServerVersion:   init.ServerInfo.Version,
		ProtocolVersion: initResp,
		ToolCount:       len(tools),
		Tools:           truncateTools(tools),
	}
}

// firstSSEData returns the JSON payload of the first `data:` line.
func firstSSEData(raw []byte) []byte {
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if after, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			return bytes.TrimSpace(after)
		}
	}
	return nil
}

// snippet shortens a body for an error message and strips newlines so it stays
// on one line in the UI.
func snippet(b []byte) string {
	const maxSnippetLen = 200
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > maxSnippetLen {
		return s[:maxSnippetLen] + "…"
	}
	return s
}

// withStderr appends the child's stderr to a message when it captured any.
// This is where a stdio server explains a startup failure (missing env var,
// wrong protocol version, a Python traceback), so dropping it would leave the
// user with an opaque "connection closed".
func withStderr(msg string, stderr *bytes.Buffer) string {
	s := strings.TrimSpace(stderr.String())
	if s == "" {
		return msg
	}
	return msg + " — server stderr: " + snippet([]byte(s))
}

// ── shared handshake ─────────────────────────────────────────────────────

type rpcCall func(rpcRequest) (*rpcResponse, error)

// doInitialize performs the initialize handshake and returns the parsed result
// plus the protocol version the server agreed to.
func doInitialize(call rpcCall) (*initializeResult, string, error) {
	resp, err := call(rpcRequest{
		JSONRPC: jsonrpcVersion,
		ID:      1,
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "clawbench-mcp-probe",
				"version": "1",
			},
		},
	})
	if err != nil {
		return nil, "", fmt.Errorf("initialize failed: %w", err)
	}
	var out initializeResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return nil, "", fmt.Errorf("initialize reply is not valid: %s", snippet(resp.Result))
	}
	// A server that answers initialize without a protocolVersion is not
	// speaking MCP. Catch it here rather than failing later with a confusing
	// tools/list error.
	if out.ProtocolVersion == "" {
		return nil, "", fmt.Errorf("initialize reply has no protocolVersion (not an MCP server?)")
	}
	return &out, out.ProtocolVersion, nil
}

// doToolsList asks for the tool list, which is the second half of what an agent
// does on connect. A server that handshakes but cannot list tools is not usable.
func doToolsList(call rpcCall) ([]string, error) {
	resp, err := call(rpcRequest{JSONRPC: jsonrpcVersion, ID: 2, Method: "tools/list"})
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}
	var out toolsListResult
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return nil, fmt.Errorf("tools/list reply is not valid: %s", snippet(resp.Result))
	}
	names := make([]string, 0, len(out.Tools))
	for _, t := range out.Tools {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	return names, nil
}

func toolsMessage(n int) string {
	if n == 0 {
		// Zero tools is a valid MCP server (it may expose only resources or
		// prompts), but the user should know the agent will have nothing to
		// call — that is almost always a misconfiguration.
		return "connected, but the server exposes 0 tools"
	}
	return fmt.Sprintf("connected — %d tool(s) available", n)
}

func truncateTools(names []string) []string {
	if len(names) <= maxToolsListed {
		return names
	}
	return names[:maxToolsListed]
}

func envPairs(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// mustJSON marshals a value that is known to be marshalable. It is only used
// for the fixed-shape `initialized` notification.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// Unreachable: rpcRequest has no unmarshalable fields.
		return []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	}
	return b
}
