package ai

import (
	"context"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testAgentID returns an agent ID unique to this test so the process-global
// capability registry (a singleton with no Reset) cannot leak state between
// tests or be polluted by them.
func testAgentID(t *testing.T) string {
	t.Helper()
	id := "model-guard-" + t.Name()
	t.Cleanup(func() {
		GetAgentCapabilityRegistry().mu.Lock()
		delete(GetAgentCapabilityRegistry().caps, id)
		GetAgentCapabilityRegistry().mu.Unlock()
	})
	return id
}

// newGuardConn builds the minimal ACPConn that keepUnlistedModel needs: an
// agent (for AgentID) and nothing else.
func newGuardConn(agentID string) *ACPConn {
	return &ACPConn{agent: &model.Agent{ID: agentID, Backend: "commandcode"}}
}

// TestKeepUnlistedModel is the regression guard for the BYOK reroute bug.
//
// Concrete failure it prevents: Command Code with a BYOK provider pins
// "minimax/minimax-m2.7" in ~/.commandcode/config.json, but its ACP surface
// advertises only the vendor's 92-model catalog. ClawBench used to push whatever
// the session had stored (the UI default, "claude-sonnet-5-5") over that pin,
// which rerouted the request to the vendor gateway and failed with
// `POST /alpha/generate → 401 Invalid 'Authorization' header`.
func TestKeepUnlistedModel(t *testing.T) {
	t.Run("keeps an out-of-band model the agent never offered", func(t *testing.T) {
		id := testAgentID(t)
		GetAgentCapabilityRegistry().UpdateModels(id, []model.AgentModel{
			{ID: "claude-sonnet-5-5"}, {ID: "gpt-5"},
		})

		conn := newGuardConn(id)
		assert.True(t, conn.keepUnlistedModel("minimax/minimax-m2.7"),
			"a BYOK-pinned model absent from the catalog must be left alone")
	})

	t.Run("applies a model the agent does offer", func(t *testing.T) {
		id := testAgentID(t)
		GetAgentCapabilityRegistry().UpdateModels(id, []model.AgentModel{
			{ID: "claude-sonnet-5-5"}, {ID: "gpt-5"},
		})

		conn := newGuardConn(id)
		assert.False(t, conn.keepUnlistedModel("gpt-5"),
			"an advertised model must still be switchable")
	})

	t.Run("applies the selection when the list is not known yet", func(t *testing.T) {
		// Critical: on the first turn the agent's capabilities may not have been
		// extracted yet. Treating "unknown" as "unavailable" would silently skip
		// model selection for every backend on every new session.
		id := testAgentID(t)
		conn := newGuardConn(id)
		assert.False(t, conn.keepUnlistedModel("anything"),
			"an unobserved model list must not block the stored selection")
	})

	t.Run("does not block when there is no agent", func(t *testing.T) {
		conn := &ACPConn{}
		assert.False(t, conn.keepUnlistedModel("x"))
	})

	// claude-agent-acp advertises TIER ALIASES (opus / sonnet / haiku) while the
	// picker and PATCH carry the CLI's concrete ids. Comparing raw IDs made every
	// concrete id look "unlisted", so applyModelSelection returned early and the
	// switch was silently disabled: chip changed, PATCH returned OK, next turn
	// ran the old model. The adapter resolves a concrete id back to its tier, so
	// "the tier is advertised" is the correct expressibility test.
	t.Run("applies a concrete id whose tier the agent advertises", func(t *testing.T) {
		id := testAgentID(t)
		GetAgentCapabilityRegistry().UpdateModels(id, []model.AgentModel{
			{ID: "opus", Name: "Opus 4.6"},
			{ID: "sonnet", Name: "Sonnet 4.6"},
			{ID: "haiku", Name: "Haiku 4.5"},
		})

		conn := newGuardConn(id)
		assert.False(t, conn.keepUnlistedModel("claude-sonnet-4-6"),
			"the concrete CLI id is expressible because its tier is advertised")
		assert.False(t, conn.keepUnlistedModel("claude-opus-4-5"))
		assert.False(t, conn.keepUnlistedModel("haiku"))
	})

	// The tier fallback must not turn the guard into a no-op: a value whose tier
	// is NOT advertised is still an out-of-band pin.
	t.Run("still keeps a value whose tier is not advertised", func(t *testing.T) {
		id := testAgentID(t)
		GetAgentCapabilityRegistry().UpdateModels(id, []model.AgentModel{{ID: "sonnet"}})

		conn := newGuardConn(id)
		assert.True(t, conn.keepUnlistedModel("minimax/minimax-m2.7"),
			"no advertised tier matches, so this is still an out-of-band pin")
		assert.True(t, conn.keepUnlistedModel("claude-opus-4-5"),
			"opus is not in the list; its concrete id must not sneak through")
	})

	// A parameterized variant is a distinct selectable model, not a tier: it must
	// not be waved through by the tier fallback.
	t.Run("a parameterized variant is not covered by its tier", func(t *testing.T) {
		id := testAgentID(t)
		GetAgentCapabilityRegistry().UpdateModels(id, []model.AgentModel{{ID: "opus"}})

		conn := newGuardConn(id)
		assert.True(t, conn.keepUnlistedModel("opus[1m]"),
			"opus[1m] is its own model; the bare opus entry does not represent it")
	})
}

// TestApplyModelSelectionSkipsUnlistedModel pins the WIRING, not just the
// predicate: applyModelSelection must consult keepUnlistedModel before it
// reaches the switch mechanism.
//
// The assertion is "no RPC was attempted", recorded through the existing
// setConfigOptionFn test double. Asserting only "returns nil" would pass even
// with the guard removed, because setSessionConfigOption silently no-ops on a
// dead connection — that variant of this test survived the mutation.
func TestApplyModelSelectionSkipsUnlistedModel(t *testing.T) {
	id := testAgentID(t)
	GetAgentCapabilityRegistry().UpdateModels(id, []model.AgentModel{{ID: "gpt-5"}})

	calls := 0
	// A live-looking connection: setConfigOptionWithCrashCheck reports "killed
	// connection" on a dead one, so the second half of this test needs alive=true
	// and a non-nil conn to reach the test double at all.
	conn := NewACPConnForTest(&model.Agent{ID: id, Backend: "commandcode"}, "sid")
	conn.alive = true
	conn.conn = newLiveConnForTest(t)
	conn.SetConfigOptionFnForTest(func(_ context.Context, _, configID, value string) error {
		calls++
		return nil
	})

	// The unlisted (BYOK) value must never reach the agent.
	require.NoError(t, conn.applyModelSelection(context.Background(), "sid", "minimax/minimax-m2.7"))
	assert.Equal(t, 0, calls, "an out-of-band model must not be pushed over ACP")

	// A value the agent DOES offer must still be applied — otherwise the guard
	// would silently disable model switching for everyone.
	require.NoError(t, conn.applyModelSelection(context.Background(), "sid", "gpt-5"))
	assert.Equal(t, 1, calls, "an advertised model must still be switched")
}

// TestIsModelAvailableAndHasModelList pins the two registry queries the guard
// depends on, in particular that "unknown" and "known but missing" differ.
func TestIsModelAvailableAndHasModelList(t *testing.T) {
	id := testAgentID(t)
	reg := GetAgentCapabilityRegistry()

	assert.False(t, reg.HasModelList(id), "no capability entry yet")
	assert.False(t, reg.IsModelAvailable(id, "x"), "no capability entry yet")

	reg.UpdateModels(id, []model.AgentModel{{ID: "a"}})
	require.True(t, reg.HasModelList(id))
	assert.True(t, reg.IsModelAvailable(id, "a"))
	assert.False(t, reg.IsModelAvailable(id, "b"))
}
