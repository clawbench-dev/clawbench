package ai

import (
	"testing"

	"clawbench/internal/ai/acptest"
	"clawbench/internal/model"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The refusal detail comes from a backend-declared hook, so a backend that
// declares none must fall back to the generic copy rather than reporting an
// empty detail — and, crucially, must not be given a CodeBuddy-specific parse.
//
// This is the guard that the shared prompt loop no longer knows which backend
// hides its cause where: it asks, and an unknown backend answers "nothing".
func TestRefusalDetail_OnlyDeclaredBackendsContribute(t *testing.T) {
	// An agent whose backend declares no RefusalDetail hook.
	conn := &ACPConn{clawbenchSID: "refusal-hookless", agent: &model.Agent{Backend: "cursor"}}
	ch := make(chan StreamEvent, 4)

	resp := acp.PromptResponse{
		StopReason: acp.StopReasonRefusal,
		Meta: acptest.Meta(map[string]any{
			// Spelled out on purpose, not via a constant: this frame is CodeBuddy's
			// private key fed to a backend that declares no hook. If the core knew
			// the key by name, this assertion could pass for the wrong reason.
			"codebuddy.ai/errorMessage": `{"code":-32603,"message":"Internal error","data":{"details":"should not be read here"}}`,
		}),
	}
	conn.emitRefusalWarningIfRefused(resp, ch, "acp-sid")
	require.Len(t, ch, 1)

	event := <-ch
	assert.Equal(t, "warning", event.Type)
	assert.Equal(t, ReasonRefused, event.Reason)
	assert.Empty(t, event.ErrorDetail,
		"a backend that declares no refusal hook must not have its _meta parsed as CodeBuddy's")
}

// A connection with no agent at all (an internal/test construction) must not
// panic and must still report the refusal: the detail is additive, never a
// precondition.
func TestRefusalDetail_NilAgentStillEmitsRefusal(t *testing.T) {
	conn := &ACPConn{clawbenchSID: "refusal-nil-agent"}
	ch := make(chan StreamEvent, 4)

	conn.emitRefusalWarningIfRefused(acp.PromptResponse{StopReason: acp.StopReasonRefusal}, ch, "acp-sid")
	require.Len(t, ch, 1)
	assert.Equal(t, ReasonRefused, (<-ch).Reason)
}
