package mcp

// This file is the live half of the MCP client: probe.go answers "would this
// server work?", and Session keeps one working connection and calls its tools.
//
// The two share the JSON-RPC vocabulary and the process-group helpers, not the
// transport code. A probe is one shot (write, read one reply, kill); a session
// has a reader goroutine, request ids, cancellation and a reaped stderr tail.
// Trying to make one implementation serve both shapes is how the probe's
// careful teardown ends up compromised.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
)

const (
	// sessionProtocolVersion is what a live session asks for. It is newer than
	// the probe's version because streamable HTTP (the transport this client
	// speaks for http servers) only exists from 2025-03-26 onward. Servers
	// negotiate down and the version they answer with is what we send back in
	// the MCP-Protocol-Version header.
	sessionProtocolVersion = "2025-06-18"
	// defaultConnectTimeout bounds one server's handshake. Generous because
	// `npx`/`uvx` launchers download and start an interpreter on first use.
	defaultConnectTimeout = 20 * time.Second
	// defaultCallTimeout bounds one tools/call when the turn itself has no
	// deadline. A browser-automation tool can legitimately run for minutes.
	defaultCallTimeout = 5 * time.Minute
	// maxToolOutputBytes caps the text a single tool call hands to the model.
	maxToolOutputBytes = 256 * 1024
	// maxToolPages bounds tools/list pagination.
	maxToolPages = 20
	// stderrTailBytes is how much of a server's stderr is kept for error
	// messages. A long-lived server writes far more than a one-shot probe, so
	// this is a tail buffer rather than an unbounded bytes.Buffer.
	stderrTailBytes = 8 * 1024
)

// errSessionExpired is returned when the HTTP transport learns the server
// forgot our session (404). The pool treats it as "dial again", not as a tool
// failure the user must act on.
var errSessionExpired = errors.New("MCP session expired")

// ToolDef is one tool as the server declares it.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Image is an inline image returned by a tool.
type Image struct {
	MediaType string
	Data      []byte
}

// CallResult is a decoded tools/call result.
type CallResult struct {
	Text    string
	Images  []Image
	IsError bool
}

// DialOptions tunes one connection.
type DialOptions struct {
	// WorkDir is the stdio server's working directory. MCP servers that resolve
	// relative paths would otherwise inherit the server process's cwd, which is
	// a different project the moment the user switches projects.
	WorkDir string
	// ConnectTimeout bounds the handshake. Zero uses defaultConnectTimeout.
	ConnectTimeout time.Duration
}

// Session is one live MCP connection.
//
// Methods are safe for concurrent use: the transport serializes writes and
// matches replies by id. A Session must be closed when its owner is done.
type Session struct {
	server      model.MCPServer
	tr          transport
	version     string
	listChanged bool
	closeOnce   sync.Once
}

// ServerName is the configured name of the server this session talks to.
func (s *Session) ServerName() string { return s.server.Name }

// ListChanged reports whether the server said its tool list can change while
// the connection is open. Callers that cache the list refresh it per turn only
// when this is true.
func (s *Session) ListChanged() bool { return s.listChanged }

// Dial connects to srv and performs the MCP handshake.
func Dial(ctx context.Context, srv model.MCPServer, opts DialOptions) (*Session, error) {
	if err := model.ValidateMCPServers([]model.MCPServer{srv}); err != nil {
		return nil, err
	}

	timeout := opts.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultConnectTimeout
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		tr  transport
		err error
	)
	switch srv.Transport {
	case model.MCPTransportStdio:
		tr, err = newStdioTransport(srv, opts.WorkDir)
	case model.MCPTransportHTTP:
		tr = newHTTPTransport(srv)
	case model.MCPTransportSSE:
		// Legacy SSE needs a server-to-client event stream and a separate
		// message endpoint; it is a different transport, not a variant of the
		// POST-based one. Failing loudly beats pretending to support it.
		return nil, fmt.Errorf("the legacy sse transport is not supported; use the http transport")
	default:
		return nil, fmt.Errorf("unsupported transport %q", srv.Transport)
	}
	if err != nil {
		return nil, err
	}

	s := &Session{server: srv, tr: tr}
	if err := s.initialize(dialCtx); err != nil {
		_ = tr.close()
		return nil, err
	}
	return s, nil
}

// initialize performs the handshake and records what the server can do.
func (s *Session) initialize(ctx context.Context) error {
	resp, err := s.tr.call(ctx, "initialize", map[string]any{
		"protocolVersion": sessionProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "clawbench",
			"version": "1",
		},
	})
	if err != nil {
		return fmt.Errorf("initialize failed: %w", err)
	}

	var out struct {
		ProtocolVersion string `json:"protocolVersion"`
		Capabilities    struct {
			Tools struct {
				ListChanged bool `json:"listChanged"`
			} `json:"tools"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return fmt.Errorf("initialize reply is not valid: %s", snippet(resp.Result))
	}
	if out.ProtocolVersion == "" {
		return fmt.Errorf("initialize reply has no protocolVersion (not an MCP server?)")
	}
	s.version = out.ProtocolVersion
	s.listChanged = out.Capabilities.Tools.ListChanged
	if h, ok := s.tr.(interface{ setVersion(string) }); ok {
		h.setVersion(out.ProtocolVersion)
	}

	// The spec requires this confirmation before any other request. A failure
	// is not fatal: servers that ignore it still serve tools, and killing the
	// session over a notification would lose a working connection.
	_ = s.tr.notify(ctx, "notifications/initialized", nil)
	return nil
}

// ListTools returns every tool the server exposes, following pagination.
func (s *Session) ListTools(ctx context.Context) ([]ToolDef, error) {
	ctx, cancel := callContext(ctx)
	defer cancel()

	var out []ToolDef
	cursor := ""
	for range maxToolPages {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		resp, err := s.tr.call(ctx, "tools/list", params)
		if err != nil {
			return nil, fmt.Errorf("tools/list failed: %w", err)
		}
		var r struct {
			Tools      []ToolDef `json:"tools"`
			NextCursor string    `json:"nextCursor"`
		}
		if err := json.Unmarshal(resp.Result, &r); err != nil {
			return nil, fmt.Errorf("tools/list reply is not valid: %s", snippet(resp.Result))
		}
		for _, t := range r.Tools {
			if strings.TrimSpace(t.Name) != "" {
				out = append(out, t)
			}
		}
		if r.NextCursor == "" || r.NextCursor == cursor {
			break
		}
		cursor = r.NextCursor
	}
	return out, nil
}

// CallTool invokes one tool and decodes its content.
func (s *Session) CallTool(ctx context.Context, name string, args json.RawMessage) (CallResult, error) {
	ctx, cancel := callContext(ctx)
	defer cancel()

	argObj := map[string]any{}
	if len(bytes.TrimSpace(args)) > 0 {
		if err := json.Unmarshal(args, &argObj); err != nil {
			return CallResult{}, fmt.Errorf("arguments are not a JSON object: %w", err)
		}
	}
	if argObj == nil {
		// `null` unmarshals into a nil map without error; the protocol requires
		// an object, so send an empty one.
		argObj = map[string]any{}
	}

	resp, err := s.tr.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": argObj,
	})
	if err != nil {
		return CallResult{}, err
	}
	return decodeCallResult(resp.Result)
}

// Close terminates the connection and its process, if any.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() { err = s.tr.close() })
	return err
}

// callContext applies the per-call ceiling without shortening a deadline the
// turn already set.
func callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, defaultCallTimeout)
}

// callContent is one entry of a tools/call result's content array.
type callContent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	URI      string `json:"uri"`
	Resource *struct {
		URI      string `json:"uri"`
		MimeType string `json:"mimeType"`
		Text     string `json:"text"`
		Blob     string `json:"blob"`
	} `json:"resource"`
}

// decodeCallResult maps the MCP content array onto what the tool loop can use.
//
// Text is the only thing every model can consume; images ride the existing
// inline-image path. Anything else (audio, embedded resources) is described in
// text rather than dropped silently — the model should know something came back.
func decodeCallResult(raw json.RawMessage) (CallResult, error) {
	var r struct {
		Content           []callContent `json:"content"`
		IsError           bool          `json:"isError"`
		StructuredContent any           `json:"structuredContent"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return CallResult{}, fmt.Errorf("tools/call reply is not valid: %s", snippet(raw))
	}

	var (
		parts  []string
		images []Image
	)
	for _, c := range r.Content {
		text, image := decodeContent(c)
		if text != "" {
			parts = append(parts, text)
		}
		if image != nil {
			images = append(images, *image)
		}
	}

	text := strings.Join(parts, "\n")
	if strings.TrimSpace(text) == "" && len(images) == 0 && r.StructuredContent != nil {
		if b, err := json.Marshal(r.StructuredContent); err == nil {
			text = string(b)
		}
	}
	text = truncateText(text, maxToolOutputBytes)
	return CallResult{Text: text, Images: images, IsError: r.IsError}, nil
}

// decodeContent turns one content block into text, an image, or a description.
func decodeContent(c callContent) (string, *Image) {
	switch c.Type {
	case "text":
		return c.Text, nil
	case "image":
		data, err := base64.StdEncoding.DecodeString(c.Data)
		if err != nil {
			return "(an image was returned but its base64 data could not be decoded)", nil
		}
		return "", &Image{MediaType: c.MimeType, Data: data}
	case "resource":
		return decodeResource(c), nil
	case "audio":
		return "(an audio clip was returned; this client cannot play it)", nil
	default:
		if c.Text != "" {
			return c.Text, nil
		}
		return fmt.Sprintf("(a %s content block was returned)", c.Type), nil
	}
}

// decodeResource describes an embedded resource. Text is passed through; a blob
// is described by name and type, because handing binary bytes to the model as
// text would be corruption, not content.
func decodeResource(c callContent) string {
	if c.Resource == nil {
		return fmt.Sprintf("(an embedded resource %s was returned)", c.URI)
	}
	switch {
	case c.Resource.Text != "":
		return c.Resource.Text
	case c.Resource.Blob != "":
		return fmt.Sprintf("(an embedded binary resource %s of type %s was returned; its bytes are not shown)",
			c.Resource.URI, c.Resource.MimeType)
	default:
		return fmt.Sprintf("(an embedded resource %s was returned)", c.Resource.URI)
	}
}

// truncateText keeps the head and says how much was dropped.
func truncateText(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	// Do not split a UTF-8 rune: the model reads mojibake as corruption.
	cut := limit
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + fmt.Sprintf("\n\n(output truncated: %d bytes dropped)", len(s)-cut)
}

// ── transports ───────────────────────────────────────────────────────────

// transport is the wire beneath a session. call and notify are safe for
// concurrent use; close is idempotent.
type transport interface {
	call(ctx context.Context, method string, params any) (*rpcResponse, error)
	notify(ctx context.Context, method string, params any) error
	close() error
}

// rpcNotification is a JSON-RPC notification: no id, so the server must not
// reply. probe.go's rpcRequest always carries an id, which is wrong for
// notifications; a live session cannot afford a server treating `initialized`
// as a request and waiting for a reply that never comes.
type rpcNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// ── stdio ────────────────────────────────────────────────────────────────

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	out    *bufio.Reader
	stderr *tailBuffer

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int
	pending map[int]chan *rpcResponse
	done    chan struct{}
	err     error

	closeOnce sync.Once
}

func newStdioTransport(srv model.MCPServer, workDir string) (*stdioTransport, error) {
	path, err := exec.LookPath(srv.Command)
	if err != nil {
		return nil, fmt.Errorf("command %q not found in PATH", srv.Command)
	}

	// The process must outlive the dial context: it serves many turns. Close
	// kills the process group, which is what bounds its lifetime.
	cmd := exec.Command(path, srv.Args...) //nolint:noctx // lifetime is bounded by Close, not by the dial deadline
	// Extend the inherited environment rather than replacing it: a server needs
	// PATH/HOME to start, and its own vars are layered on top.
	cmd.Env = append(os.Environ(), envPairs(srv.Env)...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot open stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cannot open stdout: %w", err)
	}

	t := &stdioTransport{
		cmd:     cmd,
		stdin:   stdin,
		out:     bufio.NewReader(stdout),
		stderr:  newTailBuffer(stderrTailBytes),
		pending: make(map[int]chan *rpcResponse),
		done:    make(chan struct{}),
		nextID:  1,
	}
	cmd.Stderr = t.stderr
	// Own process group: a launcher (npx, uvx, pipx) starts an interpreter as a
	// child that inherits our pipes, so killing only the leader leaves the
	// pipes open and Wait blocked.
	setProcessGroup(cmd)
	cmd.WaitDelay = probeWaitDelay

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start %q: %w", srv.Command, err)
	}
	go t.readLoop()
	return t, nil
}

func (t *stdioTransport) readLoop() {
	for {
		line, err := t.out.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if resp := parseResponseLine(line); resp != nil {
				t.deliver(resp)
			}
		}
		if err != nil {
			t.fail(t.closedError(err))
			return
		}
	}
}

// closedError explains a dropped connection with whatever the server printed.
func (t *stdioTransport) closedError(err error) error {
	msg := fmt.Errorf("server closed the connection: %w", err)
	if s := strings.TrimSpace(t.stderr.String()); s != "" {
		msg = fmt.Errorf("%w — server stderr: %s", msg, snippet([]byte(s)))
	}
	return msg
}

// parseResponseLine returns a JSON-RPC reply, or nil for notifications,
// server-initiated requests and malformed lines.
func parseResponseLine(line []byte) *rpcResponse {
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
	// A server request has both method and id; a notification has method only.
	// Neither is a reply to us.
	if probe.Method != "" || probe.ID == nil {
		return nil
	}
	return &rpcResponse{ID: *probe.ID, Result: probe.Result, Error: probe.Error}
}

func (t *stdioTransport) deliver(resp *rpcResponse) {
	t.mu.Lock()
	ch := t.pending[resp.ID]
	delete(t.pending, resp.ID)
	t.mu.Unlock()
	if ch != nil {
		// Buffered: a caller that gave up (ctx cancelled) must not block the
		// reader goroutine.
		select {
		case ch <- resp:
		default:
		}
	}
}

func (t *stdioTransport) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil {
		return
	}
	t.err = err
	close(t.done)
}

func (t *stdioTransport) call(ctx context.Context, method string, params any) (*rpcResponse, error) {
	t.mu.Lock()
	if t.err != nil {
		err := t.err
		t.mu.Unlock()
		return nil, err
	}
	id := t.nextID
	t.nextID++
	ch := make(chan *rpcResponse, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()

	if err := t.write(rpcRequest{JSONRPC: jsonrpcVersion, ID: id, Method: method, Params: params}); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		// Best effort: a server that is still working can stop early. The
		// reply, if it comes, is dropped by id in deliver.
		t.cancel(id)
		return nil, ctx.Err()
	case <-t.done:
		t.mu.Lock()
		err := t.err
		t.mu.Unlock()
		if err == nil {
			err = errors.New("connection closed")
		}
		return nil, err
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	return t.writeRaw(rpcNotification{JSONRPC: jsonrpcVersion, Method: method, Params: params})
}

func (t *stdioTransport) write(req rpcRequest) error { return t.writeRaw(req) }

func (t *stdioTransport) writeRaw(v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.stdin.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write request: %w", err)
	}
	return nil
}

// cancel tells the server this request no longer matters. It runs in its own
// goroutine because the caller is already on the cancellation path and a write
// to a server that stopped reading must not hang the turn.
func (t *stdioTransport) cancel(id int) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = t.writeRaw(rpcNotification{
				JSONRPC: jsonrpcVersion,
				Method:  "notifications/cancelled",
				Params:  map[string]any{"requestId": id, "reason": "cancelled"},
			})
		}()
		select {
		case <-done:
		case <-ctx.Done():
		}
	}()
}

func (t *stdioTransport) close() error {
	t.closeOnce.Do(func() {
		killProcessGroup(t.cmd.Process)
		_ = t.stdin.Close()
		// Wait releases the stderr copy goroutine and the pipes. The process was
		// killed, so a non-nil error is expected and not worth reporting.
		_ = t.cmd.Wait()
	})
	return nil
}

// tailBuffer keeps the last N bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newTailBuffer(limit int) *tailBuffer { return &tailBuffer{max: limit} }

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// ── streamable http ──────────────────────────────────────────────────────

type httpTransport struct {
	client *http.Client
	srv    model.MCPServer

	mu        sync.Mutex
	nextID    int
	sessionID string
	version   string
}

func newHTTPTransport(srv model.MCPServer) *httpTransport {
	// IDs start at 1: JSON-RPC forbids 0 in some server implementations, and a
	// notification without an id decodes as 0 in our reply struct — starting at
	// 0 would make an interleaved notification look like the answer.
	return &httpTransport{client: &http.Client{}, srv: srv, nextID: 1}
}

func (t *httpTransport) setVersion(v string) {
	t.mu.Lock()
	t.version = v
	t.mu.Unlock()
}

func (t *httpTransport) call(ctx context.Context, method string, params any) (*rpcResponse, error) {
	t.mu.Lock()
	id := t.nextID
	t.nextID++
	t.mu.Unlock()

	body, status, contentType, err := t.post(ctx, rpcRequest{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound && t.session() != "" {
		return nil, errSessionExpired
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("HTTP %d: %s", status, snippet(body))
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("server accepted the request without a reply")
	}

	if strings.Contains(contentType, "text/event-stream") {
		return parseSSEResponse(body, id)
	}

	var parsed rpcResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("response is not JSON-RPC (is this an MCP endpoint?): %s", snippet(body))
	}
	if parsed.Error != nil {
		return nil, parsed.Error
	}
	if parsed.ID != id {
		return nil, fmt.Errorf("reply id %d does not match request id %d", parsed.ID, id)
	}
	return &parsed, nil
}

func (t *httpTransport) notify(ctx context.Context, method string, params any) error {
	_, status, _, err := t.post(ctx, rpcNotification{JSONRPC: jsonrpcVersion, Method: method, Params: params})
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

func (t *httpTransport) post(ctx context.Context, payload any) ([]byte, int, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.srv.URL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.srv.Headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	sessionID := t.sessionID
	version := t.version
	t.mu.Unlock()
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if version == "" {
		version = sessionProtocolVersion
	}
	req.Header.Set("MCP-Protocol-Version", version)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, 0, "", fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if id := resp.Header.Get("Mcp-Session-Id"); id != "" {
		t.mu.Lock()
		t.sessionID = id
		t.mu.Unlock()
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, "", fmt.Errorf("read response: %w", err)
	}
	return raw, resp.StatusCode, resp.Header.Get("Content-Type"), nil
}

func (t *httpTransport) session() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

func (t *httpTransport) close() error {
	sessionID := t.session()
	if sessionID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.srv.URL, http.NoBody)
		if err == nil {
			req.Header.Set("Mcp-Session-Id", sessionID)
			if resp, doErr := t.client.Do(req); doErr == nil {
				_ = resp.Body.Close()
			}
		}
	}
	t.client.CloseIdleConnections()
	return nil
}

// parseSSEResponse pulls the reply with the matching id out of an SSE-framed
// POST response. Notifications may be interleaved; they are ignored.
func parseSSEResponse(raw []byte, id int) (*rpcResponse, error) {
	var data bytes.Buffer

	flush := func() (*rpcResponse, bool) {
		payload := bytes.TrimSpace(data.Bytes())
		data.Reset()
		if len(payload) == 0 {
			return nil, false
		}
		var parsed rpcResponse
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, false
		}
		if parsed.ID != id {
			return nil, false
		}
		return &parsed, true
	}

	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			if resp, ok := flush(); ok {
				return resp, nil
			}
			continue
		}
		if after, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.Write(bytes.TrimPrefix(after, []byte(" ")))
		}
	}
	if resp, ok := flush(); ok {
		return resp, nil
	}
	return nil, fmt.Errorf("the event stream ended without a reply to this request")
}
