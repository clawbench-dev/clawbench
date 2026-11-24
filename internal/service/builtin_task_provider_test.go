package service

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// builtinAgentClient is called with an agent that already passed
// builtinAgentAvailable, but it re-checks the provider: the endpoint is chosen
// here, and a nil provider would otherwise be dereferenced. Keeping the check
// local means the function is safe to call on its own.
func TestBuiltinAgentClient_RequiresAProvider(t *testing.T) {
	withAgentMap(t)

	_, _, err := builtinAgentClient(&model.Agent{
		ID:      BuiltinAgentID,
		Name:    "ClawBench Agent",
		Backend: builtinAgentBackendID,
	})
	require.Error(t, err, "an agent without a bound provider has no endpoint to call")
}
