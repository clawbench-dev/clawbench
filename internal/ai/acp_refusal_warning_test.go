package ai

import (
	"errors"
	"fmt"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmitRefusalWarning_CarriesAgentDetail is the wiring test for the refusal
// path: the extractor can be correct while the emitter silently drops its
// result, leaving the user with an unexplained -32603.
func TestEmitRefusalWarning_CarriesAgentDetail(t *testing.T) {
	conn := &ACPConn{clawbenchSID: "refusal-detail-test"}
	ch := make(chan StreamEvent, 4)

	resp := acp.PromptResponse{
		StopReason: acp.StopReasonRefusal,
		Meta: map[string]any{
			metaKeyCodeBuddyErrorMessage: `{"code":-32603,"message":"Internal error","data":{"details":"Bad substitution: o.gaps.join"}}`,
		},
	}
	conn.emitRefusalWarningIfRefused(resp, ch, "acp-sid")
	require.Len(t, ch, 1)

	event := <-ch
	assert.Equal(t, "warning", event.Type)
	assert.Equal(t, ReasonRefused, event.Reason)
	assert.Equal(t, -32603, event.ErrorCode)
	assert.Equal(t, "agent", event.ErrorSource)
	assert.Equal(t, "Bad substitution: o.gaps.join", event.ErrorDetail)
}

// A refusal with no _meta payload still reports the refusal — the detail is
// additive, never a precondition.
func TestEmitRefusalWarning_NoMetaStillEmitsRefusal(t *testing.T) {
	conn := &ACPConn{clawbenchSID: "refusal-nometa-test"}
	ch := make(chan StreamEvent, 4)

	conn.emitRefusalWarningIfRefused(acp.PromptResponse{StopReason: acp.StopReasonRefusal}, ch, "acp-sid")
	require.Len(t, ch, 1)

	event := <-ch
	assert.Equal(t, ReasonRefused, event.Reason)
	assert.Equal(t, "", event.ErrorDetail)
}

// Non-refusal stop reasons must stay silent: this emitter is the only thing
// standing between an empty turn and a misleading "AI returned no content".
func TestEmitRefusalWarning_IgnoresNonRefusal(t *testing.T) {
	conn := &ACPConn{clawbenchSID: "refusal-ignore-test"}
	ch := make(chan StreamEvent, 4)

	conn.emitRefusalWarningIfRefused(acp.PromptResponse{StopReason: acp.StopReasonEndTurn}, ch, "acp-sid")
	assert.Empty(t, ch)
}

// TestIsACPQuotaError covers the ACP shape of the same failure. OpenCode answers
// over ACP with JSON-RPC -32603 whose data carries errorName=provider.quota
// (captured from 2.2.6 against a 402 mock), so the classification cannot rely on
// an HTTP status the way the CLI frame can.
func TestIsACPQuotaError(t *testing.T) {
	quotaErr := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error: Insufficient Balance",
		Data:    map[string]any{"service": "session", "errorName": "provider.quota"},
	}
	rateLimitErr := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error: Rate limit exceeded, please try again later",
		Data:    map[string]any{"service": "session", "errorName": "provider.rate-limit"},
	}
	plainErr := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error: something else broke",
		Data:    map[string]any{"service": "session", "errorName": "provider.api_error"},
	}
	http402 := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error",
		Data:    map[string]any{"httpStatus": float64(402)},
	}
	// opencode 1.x (the `opencode-ai` package) reports the generic SDK class
	// instead of a provider-level limit type — captured from 1.19.5 against a
	// 402 mock: data={"service":"session","errorName":"APIError"}. Only the
	// message identifies the failure there, so the text fallback is load-bearing.
	v1Quota := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error: Insufficient Balance",
		Data:    map[string]any{"service": "session", "errorName": "APIError"},
	}
	v1Plain := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error: internal server error",
		Data:    map[string]any{"service": "session", "errorName": "APIError"},
	}

	assert.True(t, isACPQuotaError(quotaErr), "provider.quota must classify as quota")
	assert.True(t, isACPQuotaError(rateLimitErr), "provider.rate-limit must classify as quota")
	assert.True(t, isACPQuotaError(http402), "an HTTP 402 must classify as quota")
	assert.True(t, isACPQuotaError(v1Quota), "1.x errorName=APIError must classify by message")
	assert.False(t, isACPQuotaError(v1Plain), "a generic 1.x APIError must stay request_failed")
	assert.False(t, isACPQuotaError(plainErr), "an unrelated internal error must stay request_failed")
	assert.False(t, isACPQuotaError(errors.New("acp: prompt: peer disconnected")),
		"a non-RequestError must not be mistaken for a quota failure")
}

// The classification must survive the wrapping ExecuteStream applies, since
// that is the error value the branch actually inspects.
func TestIsACPQuotaError_ThroughWrappedError(t *testing.T) {
	inner := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error: Insufficient Balance",
		Data:    map[string]any{"errorName": "provider.quota"},
	}
	wrapped := fmt.Errorf("acp: prompt: %w", inner)
	assert.True(t, isACPQuotaError(wrapped))
}

// TestPromptErrorReason pins the mapping helper itself. It does NOT prove that
// ExecuteStream's warning branch calls it — that branch lives behind a real ACP
// connection and is covered only by the `integration`-tagged tests. Kept
// separate from TestIsACPQuotaError so a change to the mapping (not the
// predicate) fails on its own.
func TestPromptErrorReason(t *testing.T) {
	quota := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error: Insufficient Balance",
		Data:    map[string]any{"errorName": "provider.quota"},
	}
	assert.Equal(t, ReasonQuotaExhausted, promptErrorReason(quota))
	assert.Equal(t, ReasonQuotaExhausted, promptErrorReason(fmt.Errorf("acp: prompt: %w", quota)),
		"the wrapped error is what the branch actually receives")

	plain := &acp.RequestError{Code: -32603, Message: "Internal error: bad substitution"}
	assert.Equal(t, ReasonRequestFailed, promptErrorReason(plain))
}

// Some agents send RequestError.Data as a JSON STRING rather than a parsed map
// (see acpErrorDetails). The 402 / errorName fast paths must still work there,
// otherwise only the weaker text match remains.
func TestIsACPQuotaError_StringFormData(t *testing.T) {
	strData := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error",
		Data:    `{"service":"session","errorName":"provider.quota"}`,
	}
	assert.True(t, isACPQuotaError(strData), "errorName inside a JSON-string Data must be recognized")

	str402 := &acp.RequestError{
		Code:    -32603,
		Message: "Internal error",
		Data:    `{"httpStatus":402}`,
	}
	assert.True(t, isACPQuotaError(str402), "httpStatus 402 inside a JSON-string Data must be recognized")

	// Malformed string Data must not panic and must fall through to text match.
	broken := &acp.RequestError{Code: -32603, Message: "Internal error", Data: `not json`}
	assert.False(t, isACPQuotaError(broken))
}
