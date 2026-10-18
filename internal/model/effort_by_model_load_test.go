package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadAgentsIntoMemory_PreservesThinkingEffortByModel pins that the LIVE
// agent map is loaded from the same columns as the DB row.
//
// This must be asserted through LoadAgentsIntoMemoryFromDB, not through a
// service-level helper: that is the loader behind every production reload
// (startup, settings rescan, the async model-discovery reload ~18s after boot).
// A column missing from ITS SELECT is invisible until the first reload, at
// which point the per-model effort picks (issue #552) silently revert to the
// agent's own per-model defaults — the exact bug being fixed.
func TestLoadAgentsIntoMemory_PreservesThinkingEffortByModel(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, thinking_effort_by_model)
		VALUES ('opencode', 'OpenCode', 'opencode', '{"glm-5.3":"max","deepseek-v4-pro":"high"}')`)
	require.NoError(t, err)

	require.NoError(t, LoadAgentsIntoMemoryFromDB(db))

	require.Contains(t, Agents, "opencode")
	got := Agents["opencode"].ThinkingEffortByModel
	assert.Equal(t, map[string]string{"glm-5.3": "max", "deepseek-v4-pro": "high"}, got,
		"the live agent map must carry the per-model picks across a reload")
	assert.Equal(t, "max", Agents["opencode"].ThinkingEffortForModel("glm-5.3"))
}

// TestLoadAgentsIntoMemory_ToleratesUnreadableEffortByModel pins that a corrupt
// value degrades to "no picks" instead of failing the whole load: refusing to
// start (or to reload after a discovery run) over one bad column would be far
// worse than losing a default.
func TestLoadAgentsIntoMemory_ToleratesUnreadableEffortByModel(t *testing.T) {
	db := setupTestDBForDiscovery(t)
	isolateAgentGlobals(t)

	_, err := db.Exec(`INSERT INTO agents (id, name, backend, thinking_effort_by_model)
		VALUES ('broken', 'Broken', 'opencode', 'not json')`)
	require.NoError(t, err)

	require.NoError(t, LoadAgentsIntoMemoryFromDB(db))
	require.Contains(t, Agents, "broken")
	assert.Empty(t, Agents["broken"].ThinkingEffortByModel)
}
