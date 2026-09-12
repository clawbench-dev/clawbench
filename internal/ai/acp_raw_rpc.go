package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Raw JSON-RPC side channel
// ---------------------------------------------------------------------------
//
// acp-go-sdk refuses to send JSON-RPC methods whose name does not start with
// "_" (see its validateExtensionMethodName), and ClientSideConnection keeps its
// *Connection unexported with no accessor. Some agents expose private methods
// outside that convention — CodeBuddy's `session/steer`, `session/inject_history`
// and message-queue family are all plain `session/*` names — so there is no way
// to reach them through the SDK.
//
// This file adds a generic raw JSON-RPC channel beside the SDK:
//
//	outbound: both the SDK and this channel write through the SAME lockedWriter,
//	          so their bytes never interleave mid-line.
//	inbound:  acpStdoutFilter tees every agent line to DispatchRawResponse, which
//	          claims only responses whose id is our string prefix. The line is
//	          still forwarded to the SDK, which no-ops unknown ids.
//
// The channel is backend-agnostic; which methods are worth calling is decided by
// a backend-owned policy (see midturn.go).

// rawRPCIDPrefix marks request ids issued by this channel. The ACP SDK issues
// numeric ids (an atomic counter), so a string id with this prefix can never
// collide with one of its in-flight requests.
const rawRPCIDPrefix = "cb-"

// errRawConnClosed is returned when the underlying ACP connection is gone.
var errRawConnClosed = errors.New("acp: raw rpc connection closed")

// lockedWriter serializes every outbound write to the agent's stdin.
//
// The SAME instance must be handed to acp.NewClientSideConnection: the SDK
// serializes its own writes with a private mutex we cannot reach, so sharing one
// writer is the only way to keep SDK requests and raw requests from interleaving
// on the wire.
type lockedWriter struct {
	mu  sync.Mutex
	dst io.Writer
}

// Write implements io.Writer. Safe for concurrent use.
func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dst.Write(p)
}

// RawRPCError is a JSON-RPC error object returned by the agent.
type RawRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RawRPCError) Error() string {
	return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
}

// acpRawRPC correlates ClawBench-issued raw JSON-RPC requests with the agent's
// responses, which are demuxed from stdout by id prefix. One per ACP connection.
type acpRawRPC struct {
	mu      sync.Mutex
	nextID  uint64
	pending map[string]chan json.RawMessage

	w    io.Writer       // the shared lockedWriter (never nil)
	done <-chan struct{} // closed when the connection dies
}

// newACPRawRPC creates a router writing through w and aborting when done closes.
func newACPRawRPC(w io.Writer, done <-chan struct{}) *acpRawRPC {
	return &acpRawRPC{
		pending: make(map[string]chan json.RawMessage),
		w:       w,
		done:    done,
	}
}

// CallRaw sends a JSON-RPC request with a method name the SDK would reject, and
// waits for the matching response.
//
// It never blocks on a mutex held by the caller of ACPConn.CallRaw, and the
// returned error distinguishes transport failure (errRawConnClosed, ctx.Err())
// from a JSON-RPC error object (*RawRPCError).
func (r *acpRawRPC) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	r.mu.Lock()
	r.nextID++
	id := fmt.Sprintf("%s%d", rawRPCIDPrefix, r.nextID)
	// Buffered so DispatchRawResponse never blocks the stdout pump. The channel
	// is removed from pending before delivery, so at most one send can occur.
	ch := make(chan json.RawMessage, 1)
	r.pending[id] = ch
	r.mu.Unlock()

	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	if err != nil {
		r.drop(id)
		return nil, fmt.Errorf("acp: raw rpc marshal %s: %w", method, err)
	}
	b = append(b, '\n')

	if _, err := r.w.Write(b); err != nil {
		r.drop(id)
		return nil, fmt.Errorf("acp: raw rpc write %s: %w", method, err)
	}

	select {
	case line := <-ch:
		var env struct {
			Result json.RawMessage `json:"result"`
			Error  *RawRPCError    `json:"error"`
		}
		if err := json.Unmarshal(line, &env); err != nil {
			return nil, fmt.Errorf("acp: raw rpc decode %s response: %w", method, err)
		}
		if env.Error != nil {
			return nil, env.Error
		}
		return env.Result, nil
	case <-ctx.Done():
		r.drop(id)
		return nil, ctx.Err()
	case <-r.done:
		r.drop(id)
		return nil, errRawConnClosed
	}
}

// drop removes a pending request. Safe to call for an unknown id.
func (r *acpRawRPC) drop(id string) {
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

// DispatchRawResponse inspects one agent output line and, if it is the response
// to a request issued by this channel, delivers it to the waiting caller.
//
// It must never block: it runs on the stdout pump goroutine, and stalling there
// would freeze every subsequent agent message. Lines that are not ours are
// ignored (the SDK still receives them via the tee).
func (r *acpRawRPC) DispatchRawResponse(line []byte) {
	var probe struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(line, &probe); err != nil || len(probe.ID) == 0 {
		return
	}

	// Unmarshal into a string: the SDK's numeric ids fail here, which is exactly
	// how we avoid claiming its responses.
	var id string
	if err := json.Unmarshal(probe.ID, &id); err != nil {
		return
	}
	if !strings.HasPrefix(id, rawRPCIDPrefix) {
		return
	}

	r.mu.Lock()
	ch, ok := r.pending[id]
	if ok {
		delete(r.pending, id)
	}
	r.mu.Unlock()

	if ok {
		// Copy: the scanner's buffer is reused between lines.
		ch <- append(json.RawMessage(nil), line...)
	}
}

// ---------------------------------------------------------------------------
// rawResponseSink — the hook acpStdoutFilter calls for every parsed line
// ---------------------------------------------------------------------------

// rawResponseSink receives every line the stdout filter parses. Implemented by
// *acpRawRPC. Kept as an interface so the filter does not depend on the router
// (and so tests can substitute a recorder).
type rawResponseSink interface {
	DispatchRawResponse(line []byte)
}
