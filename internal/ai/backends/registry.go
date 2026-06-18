package backends

import (
	"fmt"
	"sort"
	"sync"

	"clawbench/internal/model"
)

var (
	plugins   = make(map[string]*BackendPlugin)
	pluginsMu sync.RWMutex
)

// Register adds a backend plugin to the global registry.
// Typically called in a sub-package's init() function.
// Duplicate registration panics (programming error).
func Register(p *BackendPlugin) {
	pluginsMu.Lock()
	defer pluginsMu.Unlock()
	if _, exists := plugins[p.ID]; exists {
		panic(fmt.Sprintf("backend plugin already registered: %s", p.ID))
	}
	plugins[p.ID] = p
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
func ResetForTest() {
	pluginsMu.Lock()
	defer pluginsMu.Unlock()
	plugins = make(map[string]*BackendPlugin)
}

// LookupACPRemaps returns the ACP input remapping map for the given backend.
// Falls back to genericACPRemaps if the backend has no ACP plugin or no
// non-empty InputRemaps registered.
func LookupACPRemaps(backendID string) map[string]string {
	pluginsMu.RLock()
	defer pluginsMu.RUnlock()
	if p, ok := plugins[backendID]; ok && p.ACP != nil && len(p.ACP.InputRemaps) > 0 {
		return p.ACP.InputRemaps
	}
	return genericACPRemaps
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

// genericACPRemaps is the fallback ACP input normalization map.
// Migrated from common_stream.go's "generic_acp" entry.
var genericACPRemaps = map[string]string{
	"oldString": "old_string", "newString": "new_string",
	"dirPath": "path", "filePath": "file_path",
	"cellIndex": "cell_index", "cellType": "cell_type",
}
