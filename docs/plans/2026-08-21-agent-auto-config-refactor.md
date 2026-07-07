# Agent Auto-Discovery Refactor: Zero-Config Enhancement

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make ClawBench zero-config — every startup detects installed AI CLIs, auto-generates configs for new backends, and fills models/thinking-effort from runtime discovery. User YAML only holds what the user customizes.

**Architecture:** Split "user data" (YAML: name, icon, system_prompt, command, models-if-pinned) from "system data" (runtime: models from DiscoverModels cache, thinking_effort_levels from BackendRegistry). On every startup, detect all CLIs, generate minimal YAMLs for new ones, soft-remove agents whose CLI is gone, and merge discovered data into loaded agents.

**Tech Stack:** Go (backend), Vue 3 + TypeScript (frontend — minimal changes), JSON model cache files in `.clawbench/model-cache/`

---

### Task 1: Add model cache layer

**Files:**
- Create: `internal/model/model_cache.go`
- Test: `internal/model/model_cache_test.go`

**Step 1: Write the failing test for model cache read/write**

```go
package model_test

import (
    "os"
    "path/filepath"
    "testing"
    "time"

    "clawbench/internal/model"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestModelCache_ReadWrite(t *testing.T) {
    dir := t.TempDir()

    // Cache is empty initially
    models := model.ReadModelCache(dir, "codebuddy")
    assert.Nil(t, models)

    // Write cache
    written := []model.AgentModel{
        {ID: "glm-5.1", Name: "GLM 5.1", Default: true},
        {ID: "glm-4.7", Name: "GLM 4.7", Default: false},
    }
    require.NoError(t, model.WriteModelCache(dir, "codebuddy", written))

    // Read back
    models = model.ReadModelCache(dir, "codebuddy")
    require.Len(t, models, 2)
    assert.Equal(t, "glm-5.1", models[0].ID)
    assert.True(t, models[0].Default)
    assert.Equal(t, "glm-4.7", models[1].ID)
}

func TestModelCache_CorruptFile(t *testing.T) {
    dir := t.TempDir()

    // Write garbage
    cachePath := filepath.Join(dir, "codebuddy.json")
    require.NoError(t, os.WriteFile(cachePath, []byte("not json"), 0644))

    // Should return nil gracefully
    models := model.ReadModelCache(dir, "codebuddy")
    assert.Nil(t, models)
}

func TestModelCache_EmptyModels(t *testing.T) {
    dir := t.TempDir()

    // Write empty models list — should not create cache file
    err := model.WriteModelCache(dir, "test", nil)
    require.NoError(t, err)

    models := model.ReadModelCache(dir, "test")
    assert.Nil(t, models)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestModelCache -v`
Expected: FAIL — `ReadModelCache` and `WriteModelCache` undefined

**Step 3: Write model cache implementation**

```go
package model

import (
    "encoding/json"
    "os"
    "path/filepath"
    "time"
)

// modelCacheEntry is the on-disk format for the model cache.
type modelCacheEntry struct {
    UpdatedAt string       `json:"updated_at"`
    Models    []AgentModel `json:"models"`
}

// ReadModelCache reads the cached model list for a backend type.
// Returns nil if cache doesn't exist, is corrupt, or has no models.
func ReadModelCache(dir, backend string) []AgentModel {
    path := filepath.Join(dir, backend+".json")
    data, err := os.ReadFile(path)
    if err != nil {
        return nil
    }
    var entry modelCacheEntry
    if err := json.Unmarshal(data, &entry); err != nil {
        return nil
    }
    if len(entry.Models) == 0 {
        return nil
    }
    return entry.Models
}

// WriteModelCache writes the model list for a backend type to cache.
// Does not write if models is empty/nil.
func WriteModelCache(dir, backend string, models []AgentModel) error {
    if len(models) == 0 {
        return nil
    }
    if err := os.MkdirAll(dir, 0755); err != nil {
        return err
    }
    entry := modelCacheEntry{
        UpdatedAt: time.Now().Format(time.RFC3339),
        Models:    models,
    }
    data, err := json.MarshalIndent(entry, "", "  ")
    if err != nil {
        return err
    }
    return os.WriteFile(filepath.Join(dir, backend+".json"), data, 0644)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -run TestModelCache -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/model_cache.go internal/model/model_cache_test.go
git commit -m "feat: add model cache layer for discovered model lists"
```

---

### Task 2: Refactor GenerateAgentYAML to produce minimal YAML

**Files:**
- Modify: `internal/model/discovery.go:98-110` (GenerateAgentYAML)
- Modify: `internal/model/discovery_test.go` (update affected tests)

**Step 1: Write the failing test for minimal YAML generation**

Add to `internal/model/discovery_test.go`:

```go
func TestGenerateAgentYAML_MinimalFormat(t *testing.T) {
    spec := model.BackendSpec{
        ID:        "claude",
        Backend:   "claude",
        DefaultCmd: "claude",
        Name:      "Claude",
        Icon:      "🤖",
        Specialty: "代码编写与推理",
        ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
    }

    data, err := model.GenerateAgentYAML(spec)
    require.NoError(t, err)
    content := string(data)

    // Should contain basic fields
    assert.Contains(t, content, "id: claude")
    assert.Contains(t, content, "name: Claude")
    assert.Contains(t, content, "backend: claude")

    // Should NOT contain models or thinking_effort_levels
    assert.NotContains(t, content, "models:")
    assert.NotContains(t, content, "thinking_effort")
    assert.NotContains(t, content, "system_prompt:")

    // Verify it loads back correctly
    var agent model.Agent
    require.NoError(t, yaml.Unmarshal(data, &agent))
    assert.Equal(t, "claude", agent.ID)
    assert.Empty(t, agent.Models)
    assert.Empty(t, agent.ThinkingEffortLevels)
    assert.Empty(t, agent.SystemPrompt)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestGenerateAgentYAML_MinimalFormat -v`
Expected: FAIL — current YAML includes `models: []`, `system_prompt: ""`, and `thinking_effort_levels`

**Step 3: Update GenerateAgentYAML to produce minimal output**

Change `GenerateAgentYAML` signature — remove `models` param, generate minimal YAML:

```go
// GenerateAgentYAML creates a minimal YAML config for the given backend spec.
// Only id, name, icon, specialty, and backend are written.
// Models, thinking_effort_levels, and system_prompt are NOT written —
// they are filled at runtime from auto-discovery and BackendRegistry.
func GenerateAgentYAML(spec BackendSpec) ([]byte, error) {
    agent := Agent{
        ID:        spec.ID,
        Name:      spec.Name,
        Icon:      spec.Icon,
        Specialty: spec.Specialty,
        Backend:   spec.Backend,
    }
    return yaml.Marshal(agent)
}
```

**Step 4: Update all callers of GenerateAgentYAML**

In `discovery.go` line 157, change:
```go
// Before:
data, err := GenerateAgentYAML(r.spec, DiscoverModels(r.spec))
// After:
data, err := GenerateAgentYAML(r.spec)
```

**Step 5: Update existing tests**

Update `TestGenerateAgentYAML_Format` to not expect Models/SystemPrompt in generated YAML.
Update `TestGenerateAgentYAML_ContainsRequiredFields` to not expect `models: []` or `system_prompt: ""`.
Remove `TestGenerateAgentYAML_WithNilModels` and `TestGenerateAgentYAML_WithModels` (no longer relevant — models are never in generated YAML).

**Step 6: Run all discovery tests**

Run: `go test ./internal/model/ -run "TestGenerate|TestDiscover" -v`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/model/discovery.go internal/model/discovery_test.go
git commit -m "refactor: GenerateAgentYAML produces minimal YAML without models/levels"
```

---

### Task 3: Add FindSpecByBackend helper and SyncDiscoverAgents function

**Files:**
- Modify: `internal/model/discovery.go` (add FindSpecByBackend, rewrite DiscoverAgents as SyncDiscoverAgents)
- Test: `internal/model/discovery_test.go` (add tests)

**Step 1: Write the failing tests**

```go
func TestFindSpecByBackend(t *testing.T) {
    spec := model.FindSpecByBackend("codebuddy")
    require.NotNil(t, spec)
    assert.Equal(t, "codebuddy", spec.Backend)
    assert.Equal(t, "codebuddy", spec.DefaultCmd)
    assert.NotEmpty(t, spec.ListModelsCmd)
}

func TestFindSpecByBackend_NotFound(t *testing.T) {
    spec := model.FindSpecByBackend("nonexistent")
    assert.Nil(t, spec)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestFindSpecByBackend -v`
Expected: FAIL — function doesn't exist

**Step 3: Implement FindSpecByBackend and SyncDiscoverAgents**

Add to `discovery.go`:

```go
// FindSpecByBackend returns the BackendSpec for the given backend type, or nil.
func FindSpecByBackend(backend string) *BackendSpec {
    for i := range BackendRegistry {
        if BackendRegistry[i].Backend == backend {
            return &BackendRegistry[i]
        }
    }
    return nil
}

// SyncDiscoverAgents is called on every startup (not just first-run).
// It does three things:
// 1. Detects all installed CLIs from BackendRegistry.
// 2. Generates minimal YAML for newly found backends (no overwrite).
// 3. Returns a set of backend types whose CLI is currently present.
func SyncDiscoverAgents(dir string) map[string]bool {
    if err := os.MkdirAll(dir, 0755); err != nil {
        slog.Warn("failed to create agents directory", "dir", dir, "error", err)
        return nil
    }

    type result struct {
        spec   BackendSpec
        exists bool
    }
    results := make([]result, len(BackendRegistry))
    var wg sync.WaitGroup
    for i, spec := range BackendRegistry {
        wg.Add(1)
        go func(i int, spec BackendSpec) {
            defer wg.Done()
            results[i] = result{spec: spec, exists: CheckCLIExists(spec.DefaultCmd)}
        }(i, spec)
    }
    wg.Wait()

    present := make(map[string]bool)
    for _, r := range results {
        if r.exists {
            present[r.spec.Backend] = true
        }

        yamlPath := filepath.Join(dir, r.spec.ID+".yaml")

        // Don't overwrite existing files
        if _, err := os.Stat(yamlPath); err == nil {
            continue
        }

        if !r.exists {
            continue
        }

        // New CLI found + no YAML → generate minimal config
        data, err := GenerateAgentYAML(r.spec)
        if err != nil {
            slog.Warn("failed to generate agent YAML", "backend", r.spec.ID, "error", err)
            continue
        }
        if err := os.WriteFile(yamlPath, data, 0644); err != nil {
            slog.Warn("failed to write agent YAML", "path", yamlPath, "error", err)
            continue
        }
        slog.Info("auto-generated agent config", "backend", r.spec.ID, "path", yamlPath)
    }

    return present
}
```

**Step 4: Run tests**

Run: `go test ./internal/model/ -run "TestFindSpecByBackend" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/discovery.go internal/model/discovery_test.go
git commit -m "feat: add FindSpecByBackend and SyncDiscoverAgents for every-startup detection"
```

---

### Task 4: Add MergeDiscoveredData function

**Files:**
- Modify: `internal/model/discovery.go` (add MergeDiscoveredData)
- Test: `internal/model/discovery_test.go` (add tests)

**Step 1: Write the failing test**

```go
func TestMergeDiscoveredData_FillsEmptyModels(t *testing.T) {
    // Setup: load an agent with empty models, merge should fill from cache
    t.Cleanup(func() {
        model.Agents = nil
        model.AgentList = nil
    })

    dir := filepath.Join(t.TempDir(), "agents")
    require.NoError(t, os.MkdirAll(dir, 0755))

    spec := model.BackendSpec{
        ID:        "test-merge",
        Backend:   "test-merge",
        DefaultCmd: "nonexistent",
        Name:      "Test Merge",
        Icon:      "T",
        Specialty: "Testing",
        ThinkingEffortLevels: []string{"low", "medium", "high"},
    }
    data, err := model.GenerateAgentYAML(spec)
    require.NoError(t, err)
    require.NoError(t, os.WriteFile(filepath.Join(dir, "test-merge.yaml"), data, 0644))

    require.NoError(t, model.LoadAgents(dir))

    // Agent has empty models and empty thinking effort levels
    agent := model.Agents["test-merge"]
    require.NotNil(t, agent)
    assert.Empty(t, agent.Models)
    assert.Empty(t, agent.ThinkingEffortLevels)

    // Create a cache with models
    cacheDir := filepath.Join(t.TempDir(), "model-cache")
    cachedModels := []model.AgentModel{
        {ID: "model-a", Name: "Model A", Default: true},
        {ID: "model-b", Name: "Model B", Default: false},
    }
    require.NoError(t, model.WriteModelCache(cacheDir, "test-merge", cachedModels))

    // Merge
    model.MergeDiscoveredData(cacheDir)

    // Agent should now have models and thinking effort levels
    assert.Len(t, agent.Models, 2)
    assert.Equal(t, "model-a", agent.Models[0].ID)
    assert.Equal(t, []string{"low", "medium", "high"}, agent.ThinkingEffortLevels)
}

func TestMergeDiscoveredData_PreservesUserModels(t *testing.T) {
    t.Cleanup(func() {
        model.Agents = nil
        model.AgentList = nil
    })

    dir := filepath.Join(t.TempDir(), "agents")
    require.NoError(t, os.MkdirAll(dir, 0755))

    // Create YAML with user-defined models
    yamlContent := `id: test-preserve
name: Test Preserve
backend: codebuddy
models:
  - id: my-custom-model
    name: My Custom Model
    default: true
`
    require.NoError(t, os.WriteFile(filepath.Join(dir, "test-preserve.yaml"), []byte(yamlContent), 0644))
    require.NoError(t, model.LoadAgents(dir))

    agent := model.Agents["test-preserve"]
    require.NotNil(t, agent)
    require.Len(t, agent.Models, 1)

    // Create cache with different models
    cacheDir := filepath.Join(t.TempDir(), "model-cache")
    cachedModels := []model.AgentModel{
        {ID: "discovered-model", Name: "Discovered", Default: true},
    }
    require.NoError(t, model.WriteModelCache(cacheDir, "codebuddy", cachedModels))

    // Merge
    model.MergeDiscoveredData(cacheDir)

    // User models should be preserved
    assert.Len(t, agent.Models, 1)
    assert.Equal(t, "my-custom-model", agent.Models[0].ID)

    // Thinking effort levels should come from Registry (codebuddy)
    assert.Equal(t, []string{"low", "medium", "high", "xhigh"}, agent.ThinkingEffortLevels)
}

func TestMergeDiscoveredData_SoftRemoveMissingCLI(t *testing.T) {
    t.Cleanup(func() {
        model.Agents = nil
        model.AgentList = nil
    })

    dir := filepath.Join(t.TempDir(), "agents")
    require.NoError(t, os.MkdirAll(dir, 0755))

    // Create YAML for a backend whose CLI is NOT installed
    yamlContent := `id: test-missing
name: Test Missing
backend: nonexistent_backend_type
models:
  - id: some-model
    name: Some Model
    default: true
`
    require.NoError(t, os.WriteFile(filepath.Join(dir, "test-missing.yaml"), []byte(yamlContent), 0644))
    require.NoError(t, model.LoadAgents(dir))

    require.Len(t, model.AgentList, 1)

    // Merge with present backends that does NOT include "nonexistent_backend_type"
    present := map[string]bool{"claude": true, "codebuddy": true}
    cacheDir := filepath.Join(t.TempDir(), "model-cache")
    model.MergeDiscoveredData(cacheDir, present)

    // Agent should be removed from runtime (but YAML still exists)
    assert.Empty(t, model.Agents)
    assert.Empty(t, model.AgentList)

    // YAML file still exists
    _, err := os.Stat(filepath.Join(dir, "test-missing.yaml"))
    assert.NoError(t, err)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run "TestMergeDiscoveredData" -v`
Expected: FAIL — `MergeDiscoveredData` undefined

**Step 3: Implement MergeDiscoveredData**

```go
// MergeDiscoveredData fills models and thinking_effort_levels for loaded agents.
// - Models: uses user-defined models if present; otherwise reads from model cache.
// - ThinkingEffortLevels: always from BackendRegistry by backend type.
// - Present map: if provided, agents whose backend is not in present are soft-removed
//   (removed from AgentList/Agents map, but YAML file is preserved).
func MergeDiscoveredData(cacheDir string, present ...map[string]bool) {
    var presentMap map[string]bool
    if len(present) > 0 {
        presentMap = present[0]
    }

    // Soft-remove agents whose CLI is not present
    if presentMap != nil {
        var keep []*Agent
        for _, agent := range AgentList {
            if !presentMap[agent.Backend] {
                slog.Info("soft-removing agent (CLI not found)", "id", agent.ID, "backend", agent.Backend)
                delete(Agents, agent.ID)
                continue
            }
            keep = append(keep, agent)
        }
        AgentList = keep
    }

    // Fill models and thinking effort levels
    for _, agent := range AgentList {
        spec := FindSpecByBackend(agent.Backend)

        // ThinkingEffortLevels: always from Registry (ignore YAML values)
        if spec != nil {
            agent.ThinkingEffortLevels = spec.ThinkingEffortLevels
        }

        // Models: user-defined takes priority; otherwise use cache
        if len(agent.Models) == 0 {
            cached := ReadModelCache(cacheDir, agent.Backend)
            if len(cached) > 0 {
                agent.Models = cached
            }
        }
    }
}
```

**Step 4: Run tests**

Run: `go test ./internal/model/ -run "TestMergeDiscoveredData" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/discovery.go internal/model/discovery_test.go
git commit -m "feat: add MergeDiscoveredData for runtime model/levels injection"
```

---

### Task 5: Add synchronous model discovery for first-run

**Files:**
- Modify: `internal/model/discovery.go` (add SyncDiscoverModels)
- Test: `internal/model/discovery_test.go`

**Step 1: Write the failing test**

```go
func TestSyncDiscoverModels_CreatesCache(t *testing.T) {
    dir := t.TempDir()
    cacheDir := filepath.Join(dir, "model-cache")

    // SyncDiscoverModels should create cache files for backends
    // that have model discovery support and are installed.
    model.SyncDiscoverModels(cacheDir)

    // We can't assert specific backends exist in CI,
    // but the cache directory should be created.
    // If any CLI is installed, there should be cache files.
    entries, err := os.ReadDir(cacheDir)
    if err != nil {
        // No cache dir = no CLIs with model discovery installed, OK
        return
    }
    // Each cache file should be valid JSON
    for _, e := range entries {
        if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
            continue
        }
        data, err := os.ReadFile(filepath.Join(cacheDir, e.Name()))
        require.NoError(t, err)
        var entry map[string]any
        require.NoError(t, json.Unmarshal(data, &entry))
        assert.Contains(t, entry, "models")
        assert.Contains(t, entry, "updated_at")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/model/ -run TestSyncDiscoverModels -v`
Expected: FAIL — function doesn't exist

**Step 3: Implement SyncDiscoverModels**

```go
// SyncDiscoverModels runs DiscoverModels for all backends that support it
// and writes results to the model cache. This is called synchronously
// on first startup (when cache is empty).
func SyncDiscoverModels(cacheDir string) {
    for _, spec := range BackendRegistry {
        if len(spec.ListModelsCmd) == 0 || spec.ParseModels == nil {
            continue
        }
        models := DiscoverModels(spec)
        if len(models) == 0 {
            continue
        }
        if err := WriteModelCache(cacheDir, spec.Backend, models); err != nil {
            slog.Warn("failed to write model cache", "backend", spec.Backend, "error", err)
        } else {
            slog.Info("cached discovered models", "backend", spec.Backend, "count", len(models))
        }
    }
}
```

**Step 4: Run test**

Run: `go test ./internal/model/ -run TestSyncDiscoverModels -v`
Expected: PASS (or skip if no CLIs installed)

**Step 5: Commit**

```bash
git add internal/model/discovery.go internal/model/discovery_test.go
git commit -m "feat: add SyncDiscoverModels for first-run synchronous model cache"
```

---

### Task 6: Add async model cache refresher

**Files:**
- Modify: `internal/model/discovery.go` (add AsyncRefreshModelCache)

**Step 1: Implement async refresher**

```go
// AsyncRefreshModelCache runs DiscoverModels in a goroutine for all backends
// and updates the model cache + in-memory Agent data. Call this after startup
// is complete — it does not block.
func AsyncRefreshModelCache(cacheDir string) {
    go func() {
        for _, spec := range BackendRegistry {
            if len(spec.ListModelsCmd) == 0 || spec.ParseModels == nil {
                continue
            }
            models := DiscoverModels(spec)
            if len(models) == 0 {
                continue
            }
            if err := WriteModelCache(cacheDir, spec.Backend, models); err != nil {
                slog.Warn("failed to refresh model cache", "backend", spec.Backend, "error", err)
                continue
            }
            slog.Info("refreshed model cache", "backend", spec.Backend, "count", len(models))

            // Update in-memory agents with empty models
            for _, agent := range AgentList {
                if agent.Backend == spec.Backend && len(agent.Models) == 0 {
                    agent.Models = models
                }
            }
        }
    }()
}
```

**Step 2: Commit**

```bash
git add internal/model/discovery.go
git commit -m "feat: add AsyncRefreshModelCache for background model discovery"
```

---

### Task 7: Rewrite main.go startup flow

**Files:**
- Modify: `cmd/server/main.go:462-508` (replace agent loading section)

**Step 1: Replace the agent loading block**

Replace the section from `// Load agent configurations` through `// Initialize and start scheduler` (lines 462-510) with:

```go
// Load agent configurations (set ClawbenchBin first for placeholder replacement)
model.ClawbenchBin = absBinPath
agentsDir := filepath.Join(model.BinDir, "config", "agents")
if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
    agentsDir = filepath.Join("config", "agents")
}

// Model cache directory
modelCacheDir := filepath.Join(model.DataDir, "model-cache")

// 1. Load existing agent YAMLs
if err := model.LoadAgents(agentsDir); err != nil {
    slog.Warn("failed to load agents", slog.String("err", err.Error()))
}

// 2. Detect installed CLIs and generate configs for new backends
present := model.SyncDiscoverAgents(agentsDir)

// 3. Reload agents if any new YAMLs were generated
if len(model.AgentList) == 0 || len(present) > 0 {
    if err := model.LoadAgents(agentsDir); err != nil {
        slog.Warn("failed to reload agents after discovery", slog.String("err", err.Error()))
    }
}

// 4. Synchronous model discovery on first run (no cache exists)
if _, err := os.Stat(modelCacheDir); os.IsNotExist(err) {
    slog.Info("no model cache found, running synchronous discovery")
    model.SyncDiscoverModels(modelCacheDir)
}

// 5. Merge runtime data: fill models from cache, levels from Registry, soft-remove missing CLIs
model.MergeDiscoveredData(modelCacheDir, present)

slog.Info("agents loaded", slog.Int("count", len(model.AgentList)))

// 6. Async: refresh model cache in background (non-blocking)
model.AsyncRefreshModelCache(modelCacheDir)

// Set default agent ID from config, or fall back to first agent
if cfg.DefaultAgent != "" {
    if _, ok := model.Agents[cfg.DefaultAgent]; ok {
        model.DefaultAgentID = cfg.DefaultAgent
    } else {
        availableIDs := make([]string, 0, len(model.AgentList))
        for _, a := range model.AgentList {
            availableIDs = append(availableIDs, a.ID)
        }
        slog.Warn("configured default_agent not found, using first agent",
            slog.String("configured", cfg.DefaultAgent),
            slog.Any("available", availableIDs))
    }
}
if model.DefaultAgentID == "" && len(model.AgentList) > 0 {
    model.DefaultAgentID = model.AgentList[0].ID
}
if model.DefaultAgentID != "" {
    slog.Info("default agent", slog.String("id", model.DefaultAgentID))
} else {
    slog.Warn("no agents available, session creation will fail")
}
```

**Step 2: Run build to verify compilation**

Run: `go build -o /dev/null ./cmd/server`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: rewrite startup flow for every-boot CLI detection and model merging"
```

---

### Task 8: Add DiscoverModels for remaining backends

**Files:**
- Modify: `internal/model/discovery.go` (add parsers for claude, gemini, codex, qoder, vecli)

**Step 1: Research what CLI commands each backend supports**

Check if `claude --list-models`, `gemini --list-models`, etc. work. For each CLI, try the command on the development system and record the output format.

Note: This step requires manual verification on the dev machine. If a CLI doesn't support model listing, skip it — the empty models fallback is acceptable.

**Step 2: Add parser functions and update BackendRegistry**

For each backend that supports model listing, add:
1. A `ParseXxxModels(output string) []AgentModel` function
2. Update the corresponding `BackendRegistry` entry with `ListModelsCmd` and `ParseModels`

Example for claude (if `claude --list-models` works):
```go
{ID: "claude", Backend: "claude", DefaultCmd: "claude", Name: "Claude", Icon: "🤖", Specialty: "代码编写与推理",
    ListModelsCmd: []string{"--list-models"}, ParseModels: ParseClaudeModels,
    ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"}},
```

**Step 3: Add parser tests**

For each new parser, add tests with real CLI output samples.

**Step 4: Update `TestBackendRegistry_ModelDiscoveryConfig`**

The test currently asserts that claude/gemini/codex/qoder/vecli have NO model discovery. After adding parsers, update the test to reflect the new reality.

**Step 5: Run all model tests**

Run: `go test ./internal/model/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/model/discovery.go internal/model/discovery_test.go
git commit -m "feat: add DiscoverModels for claude/gemini/codex/qoder/vecli"
```

---

### Task 9: Clean up shipped YAML configs

**Files:**
- Modify: `config/agents/claude.yaml`
- Modify: `config/agents/codebuddy.yaml`
- Modify: `config/agents/codebuddy-overseas.yaml`
- Modify: `config/agents/opencode.yaml`
- Modify: `config/agents/codex.yaml`
- Modify: `config/agents/pi.yaml`
- Modify: `config/agents/deepseek.yaml`
- Modify: `config/agents/gemini.yaml`
- Modify: `config/agents/qoder.yaml`
- Modify: `config/agents/vecli.yaml`

**Step 1: Remove `thinking_effort_levels` and `thinking_effort` from all YAMLs**

These fields are now filled at runtime from `BackendRegistry`. Remove them from every shipped YAML.

**Step 2: For YAMLs where models are hand-customized, keep them; for YAMLs where models could be auto-discovered, remove the `models` field**

Decision rule:
- `claude.yaml`: Models are hand-written (claude doesn't have DiscoverModels yet) → **keep models**
- `codebuddy.yaml`: Models can be auto-discovered → **remove models** (let runtime fill)
- `codebuddy-overseas.yaml`: Models are hand-customized → **keep models**
- `opencode.yaml`: Models can be auto-discovered → **remove models**
- `codex.yaml`: Models are hand-customized → **keep models**
- `pi.yaml`: Models can be auto-discovered → **remove models**
- `deepseek.yaml`: Models can be auto-discovered → **remove models**
- `gemini.yaml`: Models are hand-written → **keep models**
- `qoder.yaml`: Already `models: []` → **remove the field**
- `vecli.yaml`: Models are hand-written → **keep models**

**Step 3: Example cleaned claude.yaml**

```yaml
id: claude
name: Claude
icon: 🛠️
specialty: 简单编码、日常操作、辅助任务
backend: claude
models:
  - id: claude-sonnet-4-6
    name: Claude Sonnet 4.6
    default: true
  - id: claude-opus-4-6
    name: Claude Opus 4.6
system_prompt: |
  You are a handyman, specialized in simple coding tasks and daily operations.
```

**Step 4: Example cleaned codebuddy.yaml**

```yaml
id: codebuddy
name: 顶梁柱
icon: 🤖
specialty: 通用问答、代码、文档、运维、科研
backend: codebuddy
system_prompt: |
  You are a versatile assistant capable of handling code, documentation, operations, research, and various tasks.
```

**Step 5: Build and test**

Run: `go build -o /dev/null ./cmd/server`
Expected: SUCCESS

**Step 6: Commit**

```bash
git add config/agents/
git commit -m "chore: clean up shipped YAMLs — remove thinking_effort_levels and auto-discoverable models"
```

---

### Task 10: Update existing tests for new flow

**Files:**
- Modify: `internal/model/discovery_test.go`

**Step 1: Update `TestDiscoverAgents_CreatesDirAndYAMLs` and related tests**

The generated YAMLs no longer contain `models: []` or `system_prompt: ""`. Update assertions accordingly.

**Step 2: Update `TestBackendRegistry_ModelDiscoveryConfig`**

If Task 8 added new parsers, update the "should NOT have" list.

**Step 3: Add integration test for full startup flow**

```go
func TestFullStartupFlow(t *testing.T) {
    t.Cleanup(func() {
        model.Agents = nil
        model.AgentList = nil
    })

    dir := filepath.Join(t.TempDir(), "agents")
    cacheDir := filepath.Join(t.TempDir(), "model-cache")
    require.NoError(t, os.MkdirAll(dir, 0755))

    // Simulate: no YAMLs exist, run SyncDiscoverAgents
    present := model.SyncDiscoverAgents(dir)

    // Load generated YAMLs
    require.NoError(t, model.LoadAgents(dir))

    // First-run: sync model discovery
    model.SyncDiscoverModels(cacheDir)

    // Merge runtime data
    model.MergeDiscoveredData(cacheDir, present)

    // Verify: agents with empty models got models from cache (if any CLI is installed)
    // This is best-effort in CI — we just verify no panics
    for _, agent := range model.AgentList {
        assert.NotEmpty(t, agent.ID)
        assert.NotEmpty(t, agent.Backend)
        // ThinkingEffortLevels should come from Registry (may be empty for some backends)
    }
}
```

**Step 4: Run all tests**

Run: `go test ./internal/model/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/model/discovery_test.go
git commit -m "test: update tests for new auto-discovery startup flow"
```

---

### Task 11: End-to-end verification

**Step 1: Build the full binary**

Run: `./build.sh`

**Step 2: Remove model cache to simulate first-run**

Run: `rm -rf .clawbench/model-cache/`

**Step 3: Start the server and check logs**

Run: `./server.sh --fg` (in a separate terminal)

Expected log output:
```
server starting
no model cache found, running synchronous discovery
cached discovered models  backend=codebuddy  count=N
cached discovered models  backend=deepseek   count=N
...
agents loaded  count=N
refreshed model cache  backend=codebuddy  count=N  (async, appears later)
```

**Step 4: Verify API response**

Run: `curl -s http://localhost:20000/api/agents | jq '.agents[] | {id, models: (.models | length), thinkingEffortLevels: (.thinkingEffortLevels | length)}'`

Expected: Each agent should have models (if their CLI is installed) and thinkingEffortLevels (if their backend supports it).

**Step 5: Verify a new CLI auto-appears**

Install a new CLI (e.g., `pip install opencode` if not already installed), restart ClawBench, verify the new agent appears without any manual config.

**Step 6: Verify soft-remove works**

Uninstall a CLI, restart ClawBench, verify the agent is gone from `/api/agents` but the YAML file still exists.

**Step 7: Commit any fixes**

```bash
git add -A
git commit -m "fix: end-to-end verification fixes for auto-discovery refactor"
```
