package acp

import (
	"context"
	"encoding/json"
)

// UnhandledHandler receives inbound notifications this side could not handle:
// a method it has no implementation for, or a payload that failed to decode —
// for example a session/update variant the union does not model.
//
// Requests are not reported. They still get the -32601 / -32602 answer the
// caller would send anyway, and a hook that cannot answer must not pretend it
// handled them.
//
// ClawBench: this file is not part of upstream coder/acp-go-sdk v0.20.0. The
// generator only writes *_gen.go, so it survives a schema bump; see FORK.md.
type UnhandledHandler func(ctx context.Context, method string, params json.RawMessage)

// SetUnhandledHandler installs the callback. Call it before the first message
// is read (right after NewClientSideConnection); it is not synchronized because
// the connection reads it on its inbound goroutine from then on.
func (c *ClientSideConnection) SetUnhandledHandler(h UnhandledHandler) {
	c.conn.unhandled = h
}
