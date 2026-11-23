package backends

import (
	"fmt"
	"sort"
	"sync"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

var (
	plugins   = make(map[string]*BackendPlugin)
	pluginsMu sync.RWMutex
)

// Register adds a backend plugin to the global registry and forwards its
// capabilities to the registries that consume them.
// Typically called in a sub-package's init() function.
// Duplicate registration panics (programming error).
//
// Forwarding here (rather than in each sub-package) is what makes one init()
// enough for a new backend. The alternative — every backend calling
// ai.RegisterBackend + model.RegisterModelSource + ListSessionsFromDiskRegister
// itself — meant a backend could be half registered, and that the set of calls
// drifted between backends.
func Register(p *BackendPlugin) {
	pluginsMu.Lock()
	if _, exists := plugins[p.ID]; exists {
		pluginsMu.Unlock()
		panic(fmt.Sprintf("backend plugin already registered: %s", p.ID))
	}
	plugins[p.ID] = p
	pluginsMu.Unlock()

	// In-process factory (nil for every backend that spawns an agent).
	if p.Implementation != nil && p.Implementation.NewBackend != nil {
		ai.RegisterBackend(p.ID, p.Implementation.NewBackend)
	}

	if p.ModelSource != nil {
		model.RegisterModelSource(p.ModelSource)
	}
	if p.ListSessions != nil {
		ai.ListSessionsFromDiskRegister(p.ID, p.ListSessions)
	}
	if p.Spec.MCPUnsupported {
		model.RegisterMCPUnsupported(p.ID)
	}
	if p.OrphanCmdline.Binary != "" && p.OrphanCmdline.Arg != "" {
		ai.RegisterOrphanCmdline(p.OrphanCmdline.Binary, p.OrphanCmdline.Arg)
	}
	if p.Spec.MCPRejectsSSE {
		model.RegisterMCPRejectsSSE(p.ID)
	}
	if p.Spec.MCPAttachesAsync {
		model.RegisterMCPAttachesAsync(p.ID)
	}
	if p.Spec.ListSessionsOmitsEmptyTitle {
		model.RegisterListSessionsOmitsEmptyTitle(p.ID)
	}
	if p.ProviderEnv != nil {
		model.RegisterProviderEnvSpec(p.ID, *p.ProviderEnv)
	}
}

// Lookup returns the backend plugin for the given ID, or nil if not found.
func Lookup(id string) *BackendPlugin {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	return plugins[id]
}

// All returns all registered backend plugins.
func All() []*BackendPlugin {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	result := make([]*BackendPlugin, 0, len(plugins))
	for _, p := range plugins {
		result = append(result, p)
	}
	return result
}

// AllSpecs returns BackendSpec for all registered backends.
func AllSpecs() []model.BackendSpec {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	specs := make([]model.BackendSpec, 0, len(plugins))
	for _, p := range plugins {
		specs = append(specs, p.Spec)
	}
	return specs
}

// AllSpecsSorted returns BackendSpec for all registered backends, sorted by SortOrder.
func AllSpecsSorted() []model.BackendSpec {
	specs := AllSpecs()
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].SortOrder < specs[j].SortOrder
	})
	return specs
}

// ResetForTest clears the registry. For testing only.
//
// It returns a restore function that reinstates the plugin snapshot taken
// BEFORE the clear, so callers must use it and defer the restore:
//
//	restore := backends.ResetForTest()
//	defer restore()
//
// The snapshot is the real one registered by the backend init()s, which is what
// clears the ordering hazard: this package's tests used to wipe the global
// registry and never put it back, so every other test in the same binary only
// passed while Go ran the files in registry_test.go's order. Under -shuffle
// (or a future rename) dozens of unrelated assertions failed for a reason that
// had nothing to do with the code under test.
//
// The restore is pointer-copy based — plugins are not mutated by ResetForTest —
// so nested helper registrations still see the same plugin values.
func ResetForTest() func() {
	pluginsMu.Lock()
	snapshot := make(map[string]*BackendPlugin, len(plugins))
	for id, p := range plugins {
		snapshot[id] = p
	}
	plugins = make(map[string]*BackendPlugin)
	pluginsMu.Unlock()

	return func() {
		pluginsMu.Lock()
		defer pluginsMu.Unlock()
		plugins = snapshot
	}
}

// LookupACPRemaps returns the ACP input remapping map for the given backend.
// Falls back to ai.GenericACPRemaps when the backend declares no table (no ACP
// plugin, or an empty one).
func LookupACPRemaps(backendID string) map[string]string {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil && len(p.ACP.InputRemaps) > 0 {
		return p.ACP.InputRemaps
	}
	return ai.GenericACPRemaps
}

// LookupACPToolCallIDPrefixes returns the ACP toolCallID prefix map for the given backend.
// Returns nil if the backend has no ACP plugin or no ToolCallIDPrefixes.
func LookupACPToolCallIDPrefixes(backendID string) map[string]string {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil {
		return p.ACP.ToolCallIDPrefixes
	}
	return nil
}

// LookupACPTitlePrefixes returns the ACP tool-TITLE prefix map for the given
// backend. Returns nil if the backend has no ACP plugin or no ToolTitlePrefixes.
func LookupACPTitlePrefixes(backendID string) map[string]string {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil {
		return p.ACP.ToolTitlePrefixes
	}
	return nil
}

// LookupACPModelSwitchMethod returns the JSON-RPC method the backend honors for
// a model switch, or "" when the backend uses the default config-option path.
func LookupACPModelSwitchMethod(backendID string) string {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil {
		return p.ACP.ModelSwitchMethod
	}
	return ""
}

// LookupACPToolCallWrapperPrefixes returns the title prefixes that mark a
// tool_call frame as a wrapper around the real call (dropped by the host), or
// nil when the backend emits no wrapper frames.
func LookupACPToolCallWrapperPrefixes(backendID string) []string {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil {
		return p.ACP.ToolCallWrapperTitlePrefixes
	}
	return nil
}

// LookupACPModesMirrorThoughtLevel reports whether the backend's `modes` block
// duplicates its `thought_level` config option (so the mode surface must be
// suppressed). Unknown backends return false.
func LookupACPModesMirrorThoughtLevel(backendID string) bool {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil {
		return p.ACP.ModesMirrorThoughtLevel
	}
	return false
}

// LookupACPBehavior returns the backend's ACP event-mapping hooks, or nil when
// it declares none (the generic implementation applies).
func LookupACPBehavior(backendID string) *ai.ACPBehavior {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok {
		return p.Behavior
	}
	return nil
}

// LookupACPSpawnHooks returns the backend's spawn-time argv/env customization,
// or nil when it declares none.
func LookupACPSpawnHooks(backendID string) *ai.ACPSpawnHooks {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil {
		return p.ACP.SpawnHooks
	}
	return nil
}

// LookupPluginCommands returns the backend's extra-command scanner, or nil.
func LookupPluginCommands(backendID string) func() []ai.AvailableCommandInfo {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok {
		return p.PluginCommands
	}
	return nil
}

// LookupMidTurnInjector returns the backend's mid-turn injection policy, or nil
// when the backend cannot inject a message into a running turn.
func LookupMidTurnInjector(backendID string) ai.MidTurnInjector {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok {
		return p.MidTurn
	}
	return nil
}

// LookupInProcessMidTurnInjector returns the mid-turn policy of a backend whose
// turn loop runs in this process, or nil.
func LookupInProcessMidTurnInjector(backendID string) ai.InProcessMidTurnInjector {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok {
		return p.MidTurnInProcess
	}
	return nil
}
