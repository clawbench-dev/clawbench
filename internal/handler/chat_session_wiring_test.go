package handler

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSwitchSessionEffortToModel_ForwardsOnlyWhenNotDeferred pins a WIRING
// invariant that has no observable behavior without a live ACP connection:
// the new model's effort must not be pushed while the model switch itself was
// deferred (agent busy / timed out).
//
// Why it matters: on a deferred switch the agent is still running the PREVIOUS
// model, so sending the new level now changes the effort of a session that has
// not switched yet — and may be rejected for a level the old model does not
// offer. The DB write still happens; only the RPC is withheld.
//
// Source guard rather than a behavioral test because forwardSessionConfigOption
// is a no-op when the session has no connection, which is always true in tests.
func TestSwitchSessionEffortToModel_ForwardsOnlyWhenNotDeferred(t *testing.T) {
	src, err := os.ReadFile("chat_session.go")
	require.NoError(t, err)

	start := bytes.Index(src, []byte("func switchSessionEffortToModel("))
	require.GreaterOrEqual(t, start, 0, "switchSessionEffortToModel must exist")
	// Bound the slice to THIS function: an unbounded [\s\S]*? match would also
	// accept a gate that lives in some later function.
	rest := src[start:]
	end := bytes.Index(rest, []byte("\n}\n"))
	require.Greater(t, end, 0, "function must have a top-level closing brace")
	body := string(rest[:end])

	assert.Contains(t, body, "if !deferred {",
		"the effort RPC must be gated on !deferred")
	assert.Contains(t, body, "persistContextStateThinkingEffortChange(sessionID, effort, modelID)",
		"the pick must still be persisted for the next turn")
}
