package ai

import (
	"sync"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetGlobalRegistryForTest resets the global capability registry so each test
// starts with a clean state. The singleton itself is reused (sync.Once would
// prevent re-creation) but its map is cleared.
func resetGlobalRegistryForTest(t *testing.T) *AgentCapabilityRegistry {
	t.Helper()
	reg := GetAgentCapabilityRegistry()
	reg.mu.Lock()
	reg.caps = make(map[string]*AgentCapability)
	reg.mu.Unlock()
	t.Cleanup(func() {
		reg.mu.Lock()
		reg.caps = make(map[string]*AgentCapability)
		reg.mu.Unlock()
	})
	return reg
}

// ── AgentCapability.HasData ──────────────────────────────────────────────────

func TestAgentCapability_HasData(t *testing.T) {
	t.Run("Empty", func(t *testing.T) {
		c := &AgentCapability{}
		assert.False(t, c.HasData())
	})

	t.Run("WithModes", func(t *testing.T) {
		c := &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}}
		assert.True(t, c.HasData())
	})

	t.Run("WithThinkingEfforts", func(t *testing.T) {
		c := &AgentCapability{AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}}}
		assert.True(t, c.HasData())
	})

	t.Run("WithModels", func(t *testing.T) {
		c := &AgentCapability{AvailableModels: []model.AgentModel{{ID: "m1"}}}
		assert.True(t, c.HasData())
	})

	t.Run("WithCommands", func(t *testing.T) {
		c := &AgentCapability{AvailableCommands: []AvailableCommandInfo{{Name: "init"}}}
		assert.True(t, c.HasData())
	})

	t.Run("WithConfigOptionState", func(t *testing.T) {
		c := &AgentCapability{ConfigOptionState: &ConfigOptionState{ConfigID: "x"}}
		assert.True(t, c.HasData())
	})
}

// ── Get / Set via Update / merge ────────────────────────────────────────────

func TestRegistry_Get_ReturnsNilWhenMissing(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	assert.Nil(t, reg.Get("nonexistent"))
}

func TestRegistry_Update_FirstWrite(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	agentCap := &AgentCapability{AvailableModes: []ModeDef{{ID: "ask"}}}
	reg.Update("agent-1", agentCap)

	got := reg.Get("agent-1")
	require.NotNil(t, got)
	assert.Equal(t, []ModeDef{{ID: "ask"}}, got.AvailableModes)
}

func TestRegistry_Update_MergesNonEmptyFields(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("agent-1", &AgentCapability{
		AvailableModes:           []ModeDef{{ID: "ask"}},
		AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}},
	})
	// Subsequent update with only commands — other fields must be preserved
	reg.Update("agent-1", &AgentCapability{
		AvailableCommands: []AvailableCommandInfo{{Name: "init"}},
	})

	got := reg.Get("agent-1")
	require.NotNil(t, got)
	assert.Equal(t, []ModeDef{{ID: "ask"}}, got.AvailableModes)
	assert.Equal(t, []ThinkingEffortDef{{ID: "low"}}, got.AvailableThinkingEfforts)
	assert.Equal(t, []AvailableCommandInfo{{Name: "init"}}, got.AvailableCommands)
}

func TestRegistry_Update_OverwritesNonEmptyFields(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("agent-1", &AgentCapability{AvailableModes: []ModeDef{{ID: "old"}}})
	reg.Update("agent-1", &AgentCapability{AvailableModes: []ModeDef{{ID: "new1"}, {ID: "new2"}}})

	got := reg.Get("agent-1")
	require.NotNil(t, got)
	assert.Equal(t, []ModeDef{{ID: "new1"}, {ID: "new2"}}, got.AvailableModes)
}

func TestRegistry_Update_OverwritesConfigOptionState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("agent-1", &AgentCapability{ConfigOptionState: &ConfigOptionState{ConfigID: "old"}})
	reg.Update("agent-1", &AgentCapability{ConfigOptionState: &ConfigOptionState{ConfigID: "new"}})

	got := reg.Get("agent-1")
	require.NotNil(t, got)
	require.NotNil(t, got.ConfigOptionState)
	assert.Equal(t, "new", got.ConfigOptionState.ConfigID)
}

// ── Update* convenience methods ─────────────────────────────────────────────

func TestRegistry_UpdateModes(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateModes("a1", []ModeDef{{ID: "ask"}, {ID: "code"}})

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableModes, 2)
}

func TestRegistry_UpdateThinkingEfforts(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateThinkingEfforts("a1", []ThinkingEffortDef{{ID: "low"}})

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableThinkingEfforts, 1)
}

func TestRegistry_UpdateModels(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateModels("a1", []model.AgentModel{{ID: "m1", Name: "M1"}})

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableModels, 1)
}

func TestRegistry_UpdateCommands(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateCommands("a1", []AvailableCommandInfo{{Name: "init"}})

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Len(t, got.AvailableCommands, 1)
}

func TestRegistry_UpdateConfigState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateConfigState("a1", &ConfigOptionState{ConfigID: "x"})

	got := reg.Get("a1")
	require.NotNil(t, got)
	require.NotNil(t, got.ConfigOptionState)
	assert.Equal(t, "x", got.ConfigOptionState.ConfigID)
}

// ── ForceUpdate ─────────────────────────────────────────────────────────────

func TestRegistry_ForceUpdate_FirstApply(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	agentCap := &AgentCapability{
		AvailableModes:    []ModeDef{{ID: "code"}},
		AvailableCommands: []AvailableCommandInfo{{Name: "init"}},
	}
	applied := reg.ForceUpdate("a1", agentCap)
	assert.True(t, applied)

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Equal(t, []ModeDef{{ID: "code"}}, got.AvailableModes)
	assert.Equal(t, []AvailableCommandInfo{{Name: "init"}}, got.AvailableCommands)
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestRegistry_ForceUpdate_SecondCallInSameProcessSkipped(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	first := &AgentCapability{AvailableModes: []ModeDef{{ID: "first"}}}
	assert.True(t, reg.ForceUpdate("a1", first))

	second := &AgentCapability{AvailableModes: []ModeDef{{ID: "second"}}}
	assert.False(t, reg.ForceUpdate("a1", second), "second ForceUpdate within same process should be skipped")

	got := reg.Get("a1")
	require.NotNil(t, got)
	// The first one must remain unchanged
	assert.Equal(t, []ModeDef{{ID: "first"}}, got.AvailableModes)
}

func TestRegistry_ForceUpdate_AfterMarkStale_AppliedAgain(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	first := &AgentCapability{AvailableModes: []ModeDef{{ID: "first"}}}
	reg.ForceUpdate("a1", first)

	reg.MarkStale("a1")

	second := &AgentCapability{AvailableModes: []ModeDef{{ID: "second"}}}
	assert.True(t, reg.ForceUpdate("a1", second))

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Equal(t, []ModeDef{{ID: "second"}}, got.AvailableModes)
}

func TestRegistry_ForceUpdate_OnUnknownAgent(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	agentCap := &AgentCapability{AvailableModes: []ModeDef{{ID: "x"}}}
	applied := reg.ForceUpdate("unknown-agent", agentCap)
	assert.True(t, applied)
	require.NotNil(t, reg.Get("unknown-agent"))
}

func TestRegistry_MarkStale_OnUnknown(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	// Should not panic when called on missing agent
	reg.MarkStale("missing")
	assert.Nil(t, reg.Get("missing"))
}

func TestRegistry_ForceUpdateIfNeeded_Delegates(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	modes := []ModeDef{{ID: "ask"}}
	efforts := []ThinkingEffortDef{{ID: "low"}}
	models := []model.AgentModel{{ID: "m1"}}
	cmds := []AvailableCommandInfo{{Name: "init"}}
	cfg := &ConfigOptionState{ConfigID: "mode"}
	applied := reg.ForceUpdateIfNeeded("a1", modes, efforts, models, cmds, cfg, false, false)
	assert.True(t, applied)

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Equal(t, modes, got.AvailableModes)
	assert.Equal(t, efforts, got.AvailableThinkingEfforts)
	assert.Equal(t, models, got.AvailableModels)
	assert.Equal(t, cmds, got.AvailableCommands)
	assert.Equal(t, cfg, got.ConfigOptionState)
}

// ── Get*State methods ───────────────────────────────────────────────────────

func TestRegistry_GetModeState_NoCapability(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	assert.Nil(t, reg.GetModeState("missing", "code"))
}

func TestRegistry_GetModeState_WithAvailableModes(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}, {ID: "code"}},
	})

	ms := reg.GetModeState("a1", "code")
	require.NotNil(t, ms)
	assert.Equal(t, "code", ms.CurrentModeID)
	assert.Len(t, ms.AvailableModes, 2)
}

func TestRegistry_GetModeState_FallbackToConfigOptionState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		ConfigOptionState: &ConfigOptionState{
			ConfigID: "mode",
			Options: []ConfigOptionDef{{
				ID:       "mode",
				Category: "mode",
				Values:   []ConfigOptionValue{{ID: "ask"}, {ID: "code"}},
			}},
		},
	})

	ms := reg.GetModeState("a1", "ask")
	require.NotNil(t, ms)
	assert.Equal(t, "ask", ms.CurrentModeID)
	assert.NotEmpty(t, ms.AvailableModes)
}

func TestRegistry_GetThinkingEffortState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	t.Run("NoCapability", func(t *testing.T) {
		assert.Nil(t, reg.GetThinkingEffortState("missing", "low"))
	})
	t.Run("NoEfforts", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "x"}}})
		assert.Nil(t, reg.GetThinkingEffortState("a1", "low"))
	})
	t.Run("WithEfforts", func(t *testing.T) {
		reg.Update("a2", &AgentCapability{
			AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}, {ID: "high"}},
		})
		es := reg.GetThinkingEffortState("a2", "high")
		require.NotNil(t, es)
		assert.Equal(t, "high", es.CurrentID)
		assert.Len(t, es.AvailableLevels, 2)
	})
}

func TestRegistry_GetModelListState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	t.Run("NoCapability", func(t *testing.T) {
		assert.Nil(t, reg.GetModelListState("missing", "m1"))
	})
	t.Run("NoModels", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "x"}}})
		assert.Nil(t, reg.GetModelListState("a1", "m1"))
	})
	t.Run("WithModels", func(t *testing.T) {
		reg.Update("a2", &AgentCapability{
			AvailableModels: []model.AgentModel{{ID: "m1"}, {ID: "m2"}},
		})
		ml := reg.GetModelListState("a2", "m2")
		require.NotNil(t, ml)
		assert.Equal(t, "m2", ml.CurrentModelID)
		assert.Len(t, ml.Models, 2)
	})
}

func TestRegistry_GetCommands(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	t.Run("NoCapability", func(t *testing.T) {
		assert.Nil(t, reg.GetCommands("missing"))
	})
	t.Run("Empty", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "x"}}})
		assert.Nil(t, reg.GetCommands("a1"))
	})
	t.Run("WithCommands", func(t *testing.T) {
		reg.Update("a2", &AgentCapability{
			AvailableCommands: []AvailableCommandInfo{{Name: "init"}, {Name: "help"}},
		})
		cmds := reg.GetCommands("a2")
		assert.Len(t, cmds, 2)
	})
}

func TestRegistry_GetConfigState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	t.Run("NoCapability", func(t *testing.T) {
		assert.Nil(t, reg.GetConfigState("missing"))
	})
	t.Run("WithConfig", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{ConfigOptionState: &ConfigOptionState{ConfigID: "x"}})
		cs := reg.GetConfigState("a1")
		require.NotNil(t, cs)
		assert.Equal(t, "x", cs.ConfigID)
	})
}

// ── UsageState (agent-level cache) ──────────────────────────────────────────

func TestAgentCapability_HasData_WithCachedUsageState(t *testing.T) {
	c := &AgentCapability{CachedUsageState: &UsageState{Used: 1000, Size: 200000}}
	assert.True(t, c.HasData())
}

func TestRegistry_UpdateUsageState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.UpdateUsageState("a1", &UsageState{Used: 1000, Size: 200000, Cost: 0.35, Currency: "USD"})

	got := reg.Get("a1")
	require.NotNil(t, got)
	require.NotNil(t, got.CachedUsageState)
	assert.Equal(t, 1000, got.CachedUsageState.Used)
	assert.Equal(t, 200000, got.CachedUsageState.Size)
	assert.InDelta(t, 0.35, got.CachedUsageState.Cost, 0.001)
	assert.Equal(t, "USD", got.CachedUsageState.Currency)
}

func TestRegistry_GetUsageState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	t.Run("NoCapability", func(t *testing.T) {
		assert.Nil(t, reg.GetUsageState("missing"))
	})
	t.Run("NoUsage", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
		assert.Nil(t, reg.GetUsageState("a1"))
	})
	t.Run("WithUsage", func(t *testing.T) {
		reg.Update("a2", &AgentCapability{CachedUsageState: &UsageState{Used: 5000, Size: 200000}})
		us := reg.GetUsageState("a2")
		require.NotNil(t, us)
		assert.Equal(t, 5000, us.Used)
		assert.Equal(t, 200000, us.Size)
	})
}

func TestRegistry_Merge_OverwritesUsageState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{CachedUsageState: &UsageState{Used: 1000, Size: 100000}})

	// Merge with new usage — must overwrite
	reg.Update("a1", &AgentCapability{CachedUsageState: &UsageState{Used: 5000, Size: 200000}})
	us := reg.GetUsageState("a1")
	require.NotNil(t, us)
	assert.Equal(t, 5000, us.Used)
	assert.Equal(t, 200000, us.Size)
}

func TestRegistry_Merge_PreservesUsageState(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{CachedUsageState: &UsageState{Used: 1000, Size: 100000}})

	// Merge with non-usage fields — usage must be preserved
	reg.Update("a1", &AgentCapability{AvailableCommands: []AvailableCommandInfo{{Name: "init"}}})
	us := reg.GetUsageState("a1")
	require.NotNil(t, us)
	assert.Equal(t, 1000, us.Used)
}

// ── Helpers (Has*) ──────────────────────────────────────────────────────────

func TestRegistry_HasAvailableModes(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	assert.False(t, reg.HasAvailableModes("missing"))

	reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "x"}}})
	assert.True(t, reg.HasAvailableModes("a1"))

	reg.Update("a2", &AgentCapability{AvailableCommands: []AvailableCommandInfo{{Name: "x"}}})
	assert.False(t, reg.HasAvailableModes("a2"))
}

func TestRegistry_IsModeAvailable(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{
		AvailableModes: []ModeDef{{ID: "ask"}, {ID: "code"}},
	})
	assert.True(t, reg.IsModeAvailable("a1", "code"))
	assert.False(t, reg.IsModeAvailable("a1", "missing"))
	assert.False(t, reg.IsModeAvailable("missing", "code"))
}

func TestRegistry_HasNewAvailableModes(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)

	t.Run("NoCapability_NonEmptyNew", func(t *testing.T) {
		assert.True(t, reg.HasNewAvailableModes("missing", []ModeDef{{ID: "x"}}))
	})
	t.Run("NoCapability_EmptyNew", func(t *testing.T) {
		assert.False(t, reg.HasNewAvailableModes("missing", nil))
	})
	t.Run("AllKnown", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "ask"}, {ID: "code"}}})
		assert.False(t, reg.HasNewAvailableModes("a1", []ModeDef{{ID: "ask"}, {ID: "code"}}))
	})
	t.Run("OneNew", func(t *testing.T) {
		assert.True(t, reg.HasNewAvailableModes("a1", []ModeDef{{ID: "ask"}, {ID: "new"}}))
	})
}

func TestRegistry_HasNewAvailableThinkingEfforts(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)

	t.Run("NoCapability_NonEmptyNew", func(t *testing.T) {
		assert.True(t, reg.HasNewAvailableThinkingEfforts("missing", []ThinkingEffortDef{{ID: "x"}}))
	})
	t.Run("NoCapability_EmptyNew", func(t *testing.T) {
		assert.False(t, reg.HasNewAvailableThinkingEfforts("missing", nil))
	})
	t.Run("AllKnown", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableThinkingEfforts: []ThinkingEffortDef{{ID: "low"}, {ID: "high"}}})
		assert.False(t, reg.HasNewAvailableThinkingEfforts("a1", []ThinkingEffortDef{{ID: "low"}}))
	})
	t.Run("OneNew", func(t *testing.T) {
		assert.True(t, reg.HasNewAvailableThinkingEfforts("a1", []ThinkingEffortDef{{ID: "low"}, {ID: "ultra"}}))
	})
}

func TestRegistry_HasNewAvailableModels(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)

	t.Run("NoCapability_NonEmptyNew", func(t *testing.T) {
		assert.True(t, reg.HasNewAvailableModels("missing", []model.AgentModel{{ID: "x"}}))
	})
	t.Run("NoCapability_EmptyNew", func(t *testing.T) {
		assert.False(t, reg.HasNewAvailableModels("missing", nil))
	})
	t.Run("AllKnown", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModels: []model.AgentModel{{ID: "m1"}}})
		assert.False(t, reg.HasNewAvailableModels("a1", []model.AgentModel{{ID: "m1"}}))
	})
	t.Run("OneNew", func(t *testing.T) {
		assert.True(t, reg.HasNewAvailableModels("a1", []model.AgentModel{{ID: "m1"}, {ID: "m2"}}))
	})
}

// ── Concurrency sanity ──────────────────────────────────────────────────────

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)

	const goroutines = 16
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				agentID := "concurrent-agent"
				reg.Update(agentID, &AgentCapability{
					AvailableModes: []ModeDef{{ID: "ask", Name: "Ask"}},
				})
				_ = reg.Get(agentID)
				_ = reg.HasAvailableModes(agentID)
			}
		}()
	}
	wg.Wait()

	got := reg.Get("concurrent-agent")
	require.NotNil(t, got)
	assert.Equal(t, []ModeDef{{ID: "ask", Name: "Ask"}}, got.AvailableModes)
}

// ── DB helpers (load/save with nil DB) ──────────────────────────────────────

func TestRegistry_SetRegistryDB_AndGet(t *testing.T) {
	origDB := getRegistryDB()
	t.Cleanup(func() { SetRegistryDB(origDB) })

	SetRegistryDB(nil)
	assert.Nil(t, getRegistryDB())
}

func TestRegistry_LoadFromDB_InvalidDB_LogsAndReturns(t *testing.T) {
	// With an uninitialised DB pointer we cannot call LoadFromDB safely,
	// so verify the public surface via direct call. The function is
	// expected to early-return on Query error, which we cannot trigger
	// without a real DB. We instead verify the no-op path through
	// persistAsync (nil DB registered).
	origDB := getRegistryDB()
	t.Cleanup(func() { SetRegistryDB(origDB) })
	SetRegistryDB(nil)

	reg := resetGlobalRegistryForTest(t)
	// Update without DB → persistAsync runs but no-op
	reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "x"}}})
	time.Sleep(50 * time.Millisecond)
}

func TestRegistry_PersistAsync_NilDBIsNoop(t *testing.T) {
	origDB := getRegistryDB()
	t.Cleanup(func() { SetRegistryDB(origDB) })

	SetRegistryDB(nil)

	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "ask"}}})

	// No DB — persistAsync should be a no-op, not panic.
	// We do not call it directly; Update triggers it. Just wait a tick.
	time.Sleep(50 * time.Millisecond)
}

func TestRegistry_PersistAsync_NotInRegistry(t *testing.T) {
	origDB := getRegistryDB()
	t.Cleanup(func() { SetRegistryDB(origDB) })

	// nil DB and agent not registered — should early-return
	reg := resetGlobalRegistryForTest(t)
	reg.persistAsync("missing-agent")
	// Give goroutine time to not run anything
	time.Sleep(20 * time.Millisecond)
}

// ── LoadSession / ListSessions capabilities ─────────────────────────────────

func TestAgentCapability_HasData_WithLoadSession(t *testing.T) {
	v := true
	c := &AgentCapability{LoadSession: &v}
	assert.True(t, c.HasData())
}

func TestAgentCapability_HasData_WithListSessions(t *testing.T) {
	v := true
	c := &AgentCapability{ListSessions: &v}
	assert.True(t, c.HasData())
}

func TestRegistry_GetLoadSession(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	t.Run("NoCapability", func(t *testing.T) {
		assert.False(t, reg.GetLoadSession("missing"))
	})
	t.Run("FalseByDefault", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
		assert.False(t, reg.GetLoadSession("a1"))
	})
	t.Run("True", func(t *testing.T) {
		v := true
		reg.Update("a2", &AgentCapability{LoadSession: &v})
		assert.True(t, reg.GetLoadSession("a2"))
	})
}

func TestRegistry_GetListSessions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	t.Run("NoCapability", func(t *testing.T) {
		assert.False(t, reg.GetListSessions("missing"))
	})
	t.Run("FalseByDefault", func(t *testing.T) {
		reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
		assert.False(t, reg.GetListSessions("a1"))
	})
	t.Run("True", func(t *testing.T) {
		v := true
		reg.Update("a2", &AgentCapability{ListSessions: &v})
		assert.True(t, reg.GetListSessions("a2"))
	})
}

func TestRegistry_UpdateLoadSession(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
	assert.False(t, reg.GetLoadSession("a1"))

	reg.UpdateLoadSession("a1", true)
	assert.True(t, reg.GetLoadSession("a1"))

	reg.UpdateLoadSession("a1", false)
	assert.False(t, reg.GetLoadSession("a1"))
}

func TestRegistry_UpdateListSessions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	reg.Update("a1", &AgentCapability{AvailableModes: []ModeDef{{ID: "code"}}})
	assert.False(t, reg.GetListSessions("a1"))

	reg.UpdateListSessions("a1", true)
	assert.True(t, reg.GetListSessions("a1"))

	reg.UpdateListSessions("a1", false)
	assert.False(t, reg.GetListSessions("a1"))
}

func TestRegistry_Merge_PreservesLoadSession(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	v := true
	reg.Update("a1", &AgentCapability{LoadSession: &v, AvailableModes: []ModeDef{{ID: "code"}}})

	// Merge with non-LoadSession fields — LoadSession must be preserved
	reg.Update("a1", &AgentCapability{AvailableCommands: []AvailableCommandInfo{{Name: "init"}}})
	assert.True(t, reg.GetLoadSession("a1"))
}

func TestRegistry_Merge_OverwritesListSessions(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	v := false
	reg.Update("a1", &AgentCapability{ListSessions: &v, AvailableModes: []ModeDef{{ID: "code"}}})

	// Merge with ListSessions=true — must overwrite
	tv := true
	reg.Update("a1", &AgentCapability{ListSessions: &tv})
	assert.True(t, reg.GetListSessions("a1"))
}

func TestRegistry_ForceUpdateIfNeeded_WithLoadListSession(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	modes := []ModeDef{{ID: "ask"}}
	efforts := []ThinkingEffortDef{{ID: "low"}}
	models := []model.AgentModel{{ID: "m1"}}
	cmds := []AvailableCommandInfo{{Name: "init"}}
	cfg := &ConfigOptionState{ConfigID: "mode"}
	applied := reg.ForceUpdateIfNeeded("a1", modes, efforts, models, cmds, cfg, true, true)
	assert.True(t, applied)

	got := reg.Get("a1")
	require.NotNil(t, got)
	assert.Equal(t, modes, got.AvailableModes)
	require.NotNil(t, got.LoadSession)
	assert.True(t, *got.LoadSession)
	require.NotNil(t, got.ListSessions)
	assert.True(t, *got.ListSessions)
}

func TestRegistry_ForceUpdateIfNeeded_DefaultsFalse(t *testing.T) {
	reg := resetGlobalRegistryForTest(t)
	applied := reg.ForceUpdateIfNeeded("a1", nil, nil, nil, nil, nil, false, false)
	assert.True(t, applied)

	got := reg.Get("a1")
	require.NotNil(t, got)
	if got.LoadSession != nil {
		assert.False(t, *got.LoadSession)
	}
	if got.ListSessions != nil {
		assert.False(t, *got.ListSessions)
	}
}

// ── Singleton behavior ──────────────────────────────────────────────────────

func TestGetAgentCapabilityRegistry_Singleton(t *testing.T) {
	r1 := GetAgentCapabilityRegistry()
	r2 := GetAgentCapabilityRegistry()
	assert.Same(t, r1, r2, "GetAgentCapabilityRegistry must return the same instance")
}
