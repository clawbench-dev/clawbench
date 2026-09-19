package ai

import (
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
