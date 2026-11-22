package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// Raw JSON-RPC calls
// ---------------------------------------------------------------------------
//
// Some agents expose private methods the ACP spec does not model, and whose
// names do not follow the "_" extension convention either — CodeBuddy's
// `session/steer`, Hermes's `session/set_model`. The typed SDK surface has no
// method for them and CallExtension refuses the name, so the fork adds
// ClientSideConnection.CallRaw (third_party/acp-go-sdk/rawcall.go).
//
// That keeps ONE JSON-RPC implementation in the process: the request shares the
// SDK's connection, write lock, id counter and pending-response table, so raw
// calls and typed calls can never interleave on the wire or collide on an id.
// This file is only the error-shape adapter the rest of the package expects.

// RawRPCError is a JSON-RPC error object returned by the agent.
type RawRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	// Data is the optional JSON-RPC error data payload. Agents put their
	// actionable cause here (e.g. Hermes returns
	// {"details":"GitHub Copilot is not connected: ..."} for a -32602 model
	// switch), so callers that surface an error to the user should prefer it
	// over the generic Message.
	Data json.RawMessage `json:"data,omitempty"`
}

func (e *RawRPCError) Error() string {
	return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message)
}

// errRawConnClosed is returned when the underlying ACP connection is gone.
var errRawConnClosed = errors.New("acp: raw rpc connection closed")

// acpRawRPC issues JSON-RPC calls whose method names the SDK's typed surface
// does not model. One per ACP connection.
type acpRawRPC struct {
	conn *acp.ClientSideConnection
	done <-chan struct{} // closed when the connection dies
}

// newACPRawRPC binds the raw-call adapter to a live connection.
func newACPRawRPC(conn *acp.ClientSideConnection) *acpRawRPC {
	r := &acpRawRPC{conn: conn}
	if conn != nil {
		r.done = conn.Done()
	}
	return r
}

// CallRaw sends a JSON-RPC request the typed SDK cannot express and waits for
// the matching response.
//
// It never blocks on a mutex held by the caller of ACPConn.CallRaw, and the
// returned error distinguishes transport failure (errRawConnClosed, ctx.Err())
// from a JSON-RPC error object (*RawRPCError).
func (r *acpRawRPC) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if r == nil || r.conn == nil || connClosed(r.done) {
		return nil, errRawConnClosed
	}
	raw, err := r.conn.CallRaw(ctx, method, params)
	if err == nil {
		return raw, nil
	}
	// The SDK renders a cancelled or timed-out request as a JSON-RPC error, so
	// read the context first: callers classify retryability by errors.Is.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if connClosed(r.done) {
		return nil, errRawConnClosed
	}
	return nil, rawCallError(err)
}

// rpcErrorCode reports the JSON-RPC error code carried by err, whichever shape
// it arrived in: *RawRPCError from the raw path, *acp.RequestError from a typed
// call.
func rpcErrorCode(err error) (int, bool) {
	var rawErr *RawRPCError
	if errors.As(err, &rawErr) {
		return rawErr.Code, true
	}
	var reqErr *acp.RequestError
	if errors.As(err, &reqErr) {
		return reqErr.Code, true
	}
	return 0, false
}

// rawCallError converts an SDK call error into the shape this package reports:
// *RawRPCError for a JSON-RPC answer, anything else (transport failure) as-is.
func rawCallError(err error) error {
	var reqErr *acp.RequestError
	if errors.As(err, &reqErr) {
		return &RawRPCError{Code: reqErr.Code, Message: reqErr.Message, Data: reqErr.Data}
	}
	return err
}

// connClosed reports whether done is already closed, without blocking.
func connClosed(done <-chan struct{}) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}
