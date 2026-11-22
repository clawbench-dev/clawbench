package acp

import (
	"context"
	"encoding/json"
)

// CallRaw sends a JSON-RPC request with any method name.
//
// CallExtension is the documented way to reach an extension, but it insists on
// the "_" prefix. Agents also expose private methods that never adopted the
// convention — CodeBuddy's `session/steer`, Hermes's `session/set_model` — and
// the typed surface models neither, so there is no other way to send them.
//
// Everything else is the SDK's own machinery: the connection's write mutex
// serializes the request against in-flight SDK traffic, the shared id counter
// rules out a collision, and the pending map routes the response back here. A
// JSON-RPC error comes back as *RequestError.
//
// ClawBench: this file is not part of upstream coder/acp-go-sdk v0.20.0. The
// generator only writes *_gen.go, so it survives a schema bump; see FORK.md.
func (c *ClientSideConnection) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return SendRequest[json.RawMessage](c.conn, ctx, method, params)
}
