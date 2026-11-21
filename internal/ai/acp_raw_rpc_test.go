package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Raw JSON-RPC calls (no real agent)
// ---------------------------------------------------------------------------
//
// The transport itself is the SDK's: these tests pin the two things the
// adapter adds — it reaches methods the typed surface does not model, and it
// reports failures in the shape the rest of the package classifies.

// rawRPCPair wires a real ClientSideConnection to a scripted agent. reply is
// called once with the first request line the agent reads; returning "" means
// "never answer".
func rawRPCPair(t *testing.T, reply func(req map[string]json.RawMessage) string) *acpRawRPC {
	t.Helper()
	clientToAgentR, clientToAgentW := io.Pipe()
	agentToClientR, agentToClientW := io.Pipe()

	client := &recordingACPClient{ClawBenchACPClient: NewClawBenchACPClient()}
	conn := acp.NewClientSideConnection(client, clientToAgentW, agentToClientR)

	go func() {
		sc := bufio.NewScanner(clientToAgentR)
		if !sc.Scan() {
			return
		}
		var req map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			return
		}
		out := reply(req)
		if out == "" {
			return
		}
		_, _ = io.WriteString(agentToClientW, out+"\n")
	}()
	return newACPRawRPC(conn)
}

// TestACPRawRPC_ReachesTypedlessMethod is the reason the fork has CallRaw: a
// name that neither the typed surface nor CallExtension accepts still reaches
// the agent, and the response comes back to the caller.
func TestACPRawRPC_ReachesTypedlessMethod(t *testing.T) {
	rpc := rawRPCPair(t, func(req map[string]json.RawMessage) string {
		var method string
		_ = json.Unmarshal(req["method"], &method)
		assert.Equal(t, "session/steer", method, "the raw call must carry the method verbatim")
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"steered":true}}`, req["id"])
	})

	raw, err := rpc.CallRaw(context.Background(), "session/steer", map[string]any{"sessionId": "s"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"steered":true}`, string(raw))
}

// TestACPRawRPC_MapsJSONRPCError pins the error shape callers switch on: the
// code, the message and the data payload all survive the conversion.
func TestACPRawRPC_MapsJSONRPCError(t *testing.T) {
	rpc := rawRPCPair(t, func(req map[string]json.RawMessage) string {
		return fmt.Sprintf(
			`{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"bad model","data":{"details":"not connected"}}}`,
			req["id"],
		)
	})

	_, err := rpc.CallRaw(context.Background(), "session/set_model", map[string]any{})
	require.Error(t, err)

	var rpcErr *RawRPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, -32602, rpcErr.Code)
	assert.Equal(t, "bad model", rpcErr.Message)
	assert.JSONEq(t, `{"details":"not connected"}`, string(rpcErr.Data))
}

// TestACPRawRPC_TimeoutStaysContextError: the SDK renders a timed-out request
// as a JSON-RPC error, which would make every caller's errors.Is check miss.
// The adapter restores the context error.
func TestACPRawRPC_TimeoutStaysContextError(t *testing.T) {
	rpc := rawRPCPair(t, func(map[string]json.RawMessage) string { return "" })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := rpc.CallRaw(ctx, "session/steer", map[string]any{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	var rpcErr *RawRPCError
	assert.NotErrorAs(t, err, &rpcErr, "a timeout is not a JSON-RPC answer from the agent")
}

// TestACPRawRPC_NoConnection: without a live connection the caller gets the
// sentinel it already classifies as retryable.
func TestACPRawRPC_NoConnection(t *testing.T) {
	rpc := newACPRawRPC(nil)
	_, err := rpc.CallRaw(context.Background(), "session/steer", nil)
	assert.ErrorIs(t, err, errRawConnClosed)
}
