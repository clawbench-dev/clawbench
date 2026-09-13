package backends

import (
	"context"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wireStubInjector is a no-op mid-turn policy used to exercise the registry
// wiring. The external test package defines its own; this internal one is
// needed because the wiring test inspects package-private state.
type wireStubInjector struct{}

func (wireStubInjector) Inject(context.Context, ai.RawRPCTransport, ai.MidTurnInjectRequest) (ai.MidTurnInjectResult, error) {
	return ai.MidTurnInjectResult{}, nil
}

// TestInit_WiresMidTurnCapabilityReporter verifies the init hook in acp_wire.go
// wires model.BackendSupportsMidTurnFn to the registry lookup. This is the
// bridge that lets the queued-bubble action label ("insert into the current
// reply" vs "interrupt and send") be decided in internal/model without that
// package importing the backend registry.
//
// The wiring is load-bearing and silent when broken: a nil function makes
// model.BackendSupportsMidTurn report false for every backend, which degrades
// the label for CodeBuddy without any error.
//
// The plugin is registered under a test-only id and left in place: the registry
// is package-global and shared with the external test package's init
// registrations, so ResetForTest must NOT be called here.
func TestInit_WiresMidTurnCapabilityReporter(t *testing.T) {
	require.NotNil(t, model.BackendSupportsMidTurnFn,
		"init must wire the mid-turn capability reporter")

	const withInject = "wire-test-with-inject"
	const withoutInject = "wire-test-without-inject"
	if Lookup(withInject) == nil {
		Register(&BackendPlugin{ID: withInject, MidTurn: wireStubInjector{}})
	}
	if Lookup(withoutInject) == nil {
		Register(&BackendPlugin{ID: withoutInject})
	}

	assert.True(t, model.BackendSupportsMidTurn(withInject),
		"a backend with an injector must report support")
	assert.False(t, model.BackendSupportsMidTurn(withoutInject),
		"a backend without a policy must not report support")
	assert.False(t, model.BackendSupportsMidTurn("wire-test-nonexistent"))
}

// TestInit_WiresBackendSpecLoader verifies the spec loader bridge used by model
// discovery to build the registry from backend plugins. The external test
// package imports every backend, so their init registrations are visible here
// and the loader must report them.
func TestInit_WiresBackendSpecLoader(t *testing.T) {
	require.NotNil(t, model.LoadBackendSpecs, "init must wire the backend spec loader")
	assert.NotEmpty(t, model.LoadBackendSpecs(),
		"the loader must expose the plugins registered by the imported backends")
}

// TestInit_WiresCLICapabilityReporter verifies the CLI reporter bridge: model
// asks internal/ai whether a backend has a CLI implementation.
func TestInit_WiresCLICapabilityReporter(t *testing.T) {
	require.NotNil(t, model.BackendSupportsCLIFn, "init must wire the CLI capability reporter")
	assert.False(t, model.BackendSupportsCLI("no-such-backend"))
}
