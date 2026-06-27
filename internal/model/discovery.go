//nolint:govet // shadowed err is acceptable in sequential blocks
package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// --- Discovery function registry ---

// discoverFuncs maps backend ID → model discovery function.
// Populated by backend sub-packages via RegisterDiscoverModelsFunc in init().
var (
	discoverFuncs   = make(map[string]func() []AgentModel)
	discoverFuncsMu sync.RWMutex
)

// RegisterDiscoverModelsFunc registers a model discovery function for a backend.
// Called by backend sub-packages in their init() functions.
func RegisterDiscoverModelsFunc(backendID string, fn func() []AgentModel) {
	discoverFuncsMu.Lock()
	defer discoverFuncsMu.Unlock()
	discoverFuncs[backendID] = fn
}

// lookupDiscoverFunc returns the registered discovery function for a backend, or nil.
func lookupDiscoverFunc(backendID string) func() []AgentModel {
	discoverFuncsMu.RLock()
	defer discoverFuncsMu.RUnlock()
	return discoverFuncs[backendID]
}

// BackendSpec defines a known AI backend for auto-discovery.
type BackendSpec struct {
	ID                   string   // agent id, e.g. "claude"
	Backend              string   // backend type, e.g. "claude"
	DefaultCmd           string   // command to detect on PATH, e.g. "claude"
	AltCmd               string   // fallback CLI command (e.g. "deepseek" when primary is "codewhale"); used for detection if DefaultCmd not found
	NoCLI                bool     // if true, this backend has no CLI (e.g. mock); always considered "present"
	Name                 string   // display name, e.g. "Claude"
	Icon                 string   // emoji icon, e.g. "🤖"
	Specialty            string   // short description, e.g. "代码编写与推理"
	ThinkingEffortLevels []string // supported thinking effort levels, e.g. ["low","medium","high"]; nil = not supported
	AcpCommand           string   // ACP spawn command for acp-stdio transport, e.g. "kimi --acp"; empty = no ACP support
	EmbeddedSubDir       string   // subdirectory under .clawbench/ for embedded binary, e.g. "pi"; empty = no embedded binary
	EmbeddedVersionFile  string   // filename for fast version lookup under EmbeddedSubDir, e.g. "VERSION"; empty = no version file
	EmbeddedGitHubRepo   string   // GitHub repo for release downloads, e.g. "anomalyco/opencode"; empty = no auto-download
	EmbeddedArchMapping  string   // arch name mapping in archive names, e.g. "amd64=x64"; empty = no mapping
	SortOrder            int      // display/registration order for deterministic BackendRegistry ordering
}

// LoadBackendSpecs is set by the backends package at init time to provide
// BackendSpec entries from all registered backend plugins. Uses function-variable
// injection to avoid import cycles (model cannot import backends).
var LoadBackendSpecs func() []BackendSpec

// BackendRegistry lists all known AI backends for auto-discovery.
// Populated lazily from backend plugins via GetBackendRegistry().
// Direct reads should use GetBackendRegistry() to ensure initialization.
var BackendRegistry []BackendSpec

var backendRegistryOnce sync.Once

// GetBackendRegistry returns the populated BackendRegistry, initializing it
// lazily on first call from backend plugin specs. This ensures all backend
// sub-package init()s have completed before the registry is built.
func GetBackendRegistry() []BackendSpec {
	backendRegistryOnce.Do(func() {
		if LoadBackendSpecs != nil {
			BackendRegistry = LoadBackendSpecs()
		}
	})
	return BackendRegistry
}

// CheckCLIExists checks whether a CLI command is available on the system.
// It first tries `cmd --version` with a 5-second timeout.
// If that fails, it falls back to exec.LookPath — some CLIs (especially Node.js ones)
// may return non-zero exit codes for --version when run without a TTY or in certain
// environments, but the binary itself is still present and functional.
// For backends with EmbeddedSubDir, also checks the embedded binary.
func CheckCLIExists(cmd string) bool {
	if cmd == "" {
		return false
	}

	// Check for embedded binary (e.g. .clawbench/pi/pi)
	if spec := FindBackendSpecByDefaultCmd(cmd); spec != nil && spec.EmbeddedSubDir != "" {
		if EmbeddedBinaryPath(spec.EmbeddedSubDir) != "" {
			return true
		}
	}

	// Primary check: run `cmd --version`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, cmd, "--version").Run()
	if err == nil {
		return true
	}

	// Fallback: check if the binary exists on PATH
	// This handles cases where --version fails (non-zero exit, timeout, etc.)
	// but the CLI is actually installed and usable for its primary function.
	if _, lookupErr := exec.LookPath(cmd); lookupErr == nil {
		slog.Warn("CLI --version failed but binary found on PATH, keeping agent",
			"cmd", cmd, "version_error", err)
		return true
	}

	slog.Warn("CLI not found on PATH",
		"cmd", cmd, "version_error", err)
	return false
}

// CheckCLIExistsErr returns an error describing why the CLI is not available,
// or nil if the CLI is available. This is used for more specific error reporting.
// For backends with EmbeddedSubDir, also checks the embedded binary.
func CheckCLIExistsErr(cmd string) error {
	if cmd == "" {
		return fmt.Errorf("empty command")
	}

	// Check for embedded binary
	if spec := FindBackendSpecByDefaultCmd(cmd); spec != nil && spec.EmbeddedSubDir != "" {
		if EmbeddedBinaryPath(spec.EmbeddedSubDir) != "" {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, cmd, "--version").Run()
	if err == nil {
		return nil
	}

	_, lookupErr := exec.LookPath(cmd)
	if lookupErr == nil {
		// Binary exists but --version failed — CLI is still available
		return nil
	}

	return fmt.Errorf("CLI %q not found on PATH: %w", cmd, lookupErr)
}

// DiscoverModels runs the CLI's model-list command and returns parsed models.
// Returns nil if the CLI doesn't support model listing or if the command fails.
// Errors are logged but not propagated — model discovery is best-effort.
// This is a variable so it can be overridden in tests.
var DiscoverModels = discoverModels

func discoverModels(spec BackendSpec) []AgentModel {
	// Check the discovery function registry (populated by backend sub-packages)
	if fn := lookupDiscoverFunc(spec.Backend); fn != nil {
		models := fn()
		if len(models) > 0 {
			slog.Info("model discovery succeeded (registry)", "backend", spec.ID, "models", len(models))
		}
		return models
	}
	return nil
}

// FindSpecByBackend returns the BackendSpec for the given backend type, or nil.
func FindSpecByBackend(backend string) *BackendSpec {
	registry := GetBackendRegistry()
	for i := range registry {
		if registry[i].Backend == backend {
			return &registry[i]
		}
	}
	return nil
}

// FindBackendSpecByDefaultCmd returns the BackendSpec whose DefaultCmd matches, or nil.
func FindBackendSpecByDefaultCmd(cmd string) *BackendSpec {
	registry := GetBackendRegistry()
	for i := range registry {
		if registry[i].DefaultCmd == cmd {
			return &registry[i]
		}
	}
	return nil
}

// CanDiscoverModels returns true if the spec supports model discovery via the registry.
func CanDiscoverModels(spec BackendSpec) bool {
	return lookupDiscoverFunc(spec.Backend) != nil
}

// SyncDiscoverModels runs DiscoverModels for all backends that support it
// and returns the results as a map keyed by backend type.
// This is called synchronously on first startup (when no DB models exist yet).
func SyncDiscoverModels() map[string][]AgentModel {
	result := make(map[string][]AgentModel)
	for _, spec := range GetBackendRegistry() {
		if !CanDiscoverModels(spec) {
			continue
		}
		models := DiscoverModels(spec)
		if len(models) == 0 {
			continue
		}
		result[spec.Backend] = models
		slog.Info("discovered models", "backend", spec.Backend, "count", len(models))
	}
	return result
}

// AsyncRefreshModelCache runs DiscoverModels in a goroutine for all backends
// and updates in-memory Agent data + DB. Call this after startup — it does not block.
func AsyncRefreshModelCache(db *sql.DB) {
	go func() {
		for _, spec := range GetBackendRegistry() {
			if !CanDiscoverModels(spec) {
				continue
			}
			models := DiscoverModels(spec)
			if len(models) == 0 {
				continue
			}
			slog.Info("refreshed discovered models", "backend", spec.Backend, "count", len(models))

			// Update in-memory and DB for agents whose models were auto-detected (not user-defined)
			modelsJSON, _ := json.Marshal(models)
			for _, agent := range AgentList {
				if agent.Backend == spec.Backend && agent.ModelsAutoDetected {
					agent.Models = models
					if db != nil {
						if _, err := db.Exec("UPDATE agents SET models = ? WHERE id = ?",
							string(modelsJSON), agent.ID); err != nil {
							slog.Warn("failed to persist refreshed models to DB", "id", agent.ID, "error", err)
						}
					}
				}
			}
		}
	}()
}

// --- Embedded binary detection ---

// EmbeddedBinaryPath returns the absolute path to the embedded binary under
// .clawbench/{subDir}/, or empty string if not found.
// subDir is the BackendSpec.EmbeddedSubDir value (e.g. "pi").
func EmbeddedBinaryPath(subDir string) string {
	exePath, err := os.Executable()
	if err != nil {
		slog.Error("failed to get executable path", "error", err)
		return ""
	}
	baseDir := filepath.Dir(exePath)
	for _, name := range []string{subDir, subDir + ".exe"} {
		p := filepath.Join(baseDir, ".clawbench", subDir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// EmbeddedBinaryVersion extracts the version for an embedded binary.
// First reads .clawbench/{subDir}/{versionFile} (fast), then falls back to
// running {binary} --version.
func EmbeddedBinaryVersion(subDir, versionFile string) string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	baseDir := filepath.Dir(exePath)

	// Fast path: read version file
	if versionFile != "" {
		vf := filepath.Join(baseDir, ".clawbench", subDir, versionFile)
		if data, err := os.ReadFile(vf); err == nil {
			v := strings.TrimSpace(string(data))
			if v != "" {
				return v
			}
		}
	}

	// Slow path: run binary --version
	binPath := EmbeddedBinaryPath(subDir)
	if binPath == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// --- DB-based agent discovery and merge ---

// SyncDiscoverAgentsDB is the DB-based replacement for SyncDiscoverAgents.
// It detects installed CLIs from BackendRegistry and writes new agents to the database
// instead of YAML files. Existing DB records are never overwritten.
// It also checks for embedded binaries (backends with EmbeddedSubDir).
// Returns a set of backend types whose CLI is currently present.
func SyncDiscoverAgentsDB(db *sql.DB) map[string]bool { //nolint:gocognit,gocyclo // multi-backend DB agent discovery
	type result struct {
		spec   BackendSpec
		exists bool
	}
	registry := GetBackendRegistry()
	results := make([]result, len(registry))
	var wg sync.WaitGroup
	for i, spec := range registry {
		wg.Add(1)
		go func(i int, spec BackendSpec) {
			defer wg.Done()
			exists := spec.NoCLI || CheckCLIExists(spec.DefaultCmd) || (spec.AltCmd != "" && CheckCLIExists(spec.AltCmd))
			results[i] = result{spec: spec, exists: exists}
		}(i, spec)
	}
	wg.Wait()

	// Also check for embedded binaries (backends with EmbeddedSubDir)
	embeddedPaths := make(map[string]string) // backend → embedded binary path
	for i, r := range results {
		if r.spec.EmbeddedSubDir != "" && !r.exists {
			if p := EmbeddedBinaryPath(r.spec.EmbeddedSubDir); p != "" {
				results[i] = result{spec: r.spec, exists: true}
				embeddedPaths[r.spec.Backend] = p
			}
		}
	}

	present := make(map[string]bool)

	for _, r := range results {
		if r.exists {
			present[r.spec.Backend] = true
		}

		// Skip auto-creation for backends only found via embedded binary.
		// The setup wizard handles agent creation with API key + model config.
		// Auto-creating from embedded binary would leave a broken agent (no API key).
		if r.spec.EmbeddedSubDir != "" {
			if _, ok := embeddedPaths[r.spec.Backend]; ok {
				// Only auto-create if the CLI is genuinely installed on PATH (not just embedded)
				if _, lookupErr := exec.LookPath(r.spec.DefaultCmd); lookupErr != nil {
					continue
				}
			}
		}

		// Check if DB already has an agent for this backend
		var count int
		var existingAcpCommand string
		err := db.QueryRow("SELECT COUNT(*), COALESCE(acp_command, '') FROM agents WHERE backend = ?", r.spec.Backend).Scan(&count, &existingAcpCommand)
		if err != nil {
			slog.Warn("failed to query agents table", "backend", r.spec.Backend, "error", err)
			continue
		}
		if count > 0 {
			// Update spec-derived fields if they changed in BackendSpec
			// (e.g., deepseek renamed to CodeWhale, ACP command changed)
			updates := map[string]interface{}{}
			var existingName, existingIcon, existingCommand string
			_ = db.QueryRow("SELECT COALESCE(name,''), COALESCE(icon,''), COALESCE(command,'') FROM agents WHERE backend = ?", r.spec.Backend).Scan(&existingName, &existingIcon, &existingCommand)

			if r.spec.Name != "" && existingName != r.spec.Name {
				updates["name"] = r.spec.Name
			}
			if r.spec.Icon != "" && existingIcon != r.spec.Icon {
				updates["icon"] = r.spec.Icon
			}
			// Update command if the primary CLI changed (e.g., "deepseek" → "codewhale")
			if r.spec.DefaultCmd != "" && existingCommand != "" && existingCommand != r.spec.DefaultCmd && CheckCLIExists(r.spec.DefaultCmd) {
				updates["command"] = r.spec.DefaultCmd
			}
			// Sync acp_command: update when spec changes (including clearing
			// stale values when ACP support is removed from a backend).
			if r.spec.AcpCommand != existingAcpCommand {
				updates["acp_command"] = r.spec.AcpCommand
			}

			if len(updates) > 0 {
				setClauses := make([]string, 0, len(updates))
				args := make([]interface{}, 0, len(updates)+1)
				for col, val := range updates {
					setClauses = append(setClauses, col+" = ?")
					args = append(args, val)
				}
				args = append(args, r.spec.Backend)
				query := "UPDATE agents SET " + strings.Join(setClauses, ", ") + " WHERE backend = ? AND source = 'auto'"
				if _, updateErr := db.Exec(query, args...); updateErr != nil {
					slog.Warn("failed to update auto-discovered agent", "backend", r.spec.Backend, "error", updateErr)
				} else {
					slog.Info("updated auto-discovered agent", "backend", r.spec.Backend, "updates", updates)
				}
			}
			continue // Don't overwrite other existing DB fields
		}

		if !r.exists {
			continue
		}

		// New CLI found + no DB record → insert minimal agent
		agent := &Agent{
			ID:        r.spec.ID,
			Name:      r.spec.Name,
			Icon:      r.spec.Icon,
			Specialty: r.spec.Specialty,
			Backend:   r.spec.Backend,
			Source:    "auto",
		}

		// Set command to embedded binary path for backends with embedded binaries
		if p, ok := embeddedPaths[r.spec.Backend]; ok {
			agent.Command = p
		}

		// Store ACP command info from BackendSpec (transport defaults to "cli")
		if r.spec.AcpCommand != "" {
			agent.AcpCommand = r.spec.AcpCommand
		}

		if err := saveAgentToDB(db, agent); err != nil {
			slog.Warn("failed to insert agent to DB", "backend", r.spec.ID, "error", err)
			continue
		}
		slog.Info("auto-inserted agent to DB", "backend", r.spec.ID)
	}

	// Include backends that have existing DB records but are not in BackendRegistry
	// (e.g., wizard-created agents, manual agents, mock backend).
	// This ensures MergeDiscoveredDataDB doesn't soft-delete them.
	rows, err := db.Query("SELECT DISTINCT backend FROM agents")
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var backend string
			if err := rows.Scan(&backend); err == nil && !present[backend] {
				present[backend] = true
			}
		}
	}

	return present
}

// saveAgentToDB inserts a minimal agent record into the database.
func saveAgentToDB(db *sql.DB, agent *Agent) error {
	modelsJSON, err := json.Marshal(agent.Models)
	if err != nil {
		return fmt.Errorf("marshal models: %w", err)
	}
	// json.Marshal(nil slice) produces "null" instead of "[]" — normalize to "[]"
	if string(modelsJSON) == "null" {
		modelsJSON = []byte("[]")
	}
	levelsJSON, err := json.Marshal(agent.ThinkingEffortLevels)
	if err != nil {
		return fmt.Errorf("marshal thinking_effort_levels: %w", err)
	}

	transport := agent.Transport
	if transport == "" {
		transport = "cli"
	}

	_, err = db.Exec(`INSERT INTO agents (id, name, icon, specialty, backend, command,
		thinking_effort, thinking_effort_levels, preferred_model, preferred_thinking_effort,
		system_prompt, models, models_auto_detected, source, sort_order,
		transport, acp_command)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.Name, agent.Icon, agent.Specialty, agent.Backend, agent.Command,
		agent.ThinkingEffort, string(levelsJSON), agent.PreferredModel, agent.PreferredThinkingEffort,
		agent.SystemPrompt, string(modelsJSON), agent.ModelsAutoDetected, agent.Source, agent.SortOrder,
		transport, agent.AcpCommand)
	return err
}

// yamlAgent represents the YAML structure for agent config files in config/agents/.
// This supports manually-defined agents (e.g., acp-mock for E2E testing) that are
// not in BackendRegistry and thus not auto-discovered by SyncDiscoverAgentsDB.
type yamlAgent struct {
	ID                      string       `yaml:"id"`
	Name                    string       `yaml:"name"`
	Icon                    string       `yaml:"icon"`
	Specialty               string       `yaml:"specialty"`
	Backend                 string       `yaml:"backend"`
	Command                 string       `yaml:"command"`
	ThinkingEffort          string       `yaml:"thinking_effort"`
	ThinkingEffortLevels    []string     `yaml:"thinking_effort_levels"`
	PreferredModel          string       `yaml:"preferred_model"`
	PreferredThinkingEffort string       `yaml:"preferred_thinking_effort"`
	SystemPrompt            string       `yaml:"system_prompt"`
	Transport               string       `yaml:"transport"`
	AcpCommand              string       `yaml:"acp_command"`
	Models                  []AgentModel `yaml:"models"`
	SortOrder               int          `yaml:"sort_order"`
}

// LoadYamlAgents reads agent definitions from config/agents/*.yaml and inserts
// them into the database if they don't already exist. This allows manually-defined
// agents (e.g., acp-mock for E2E testing) to be loaded without requiring an entry
// in BackendRegistry.
func LoadYamlAgents(db *sql.DB, configDir string) {
	agentsDir := filepath.Join(configDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read agents config dir", "path", agentsDir, "error", err)
		}
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(agentsDir, entry.Name()))
		if err != nil {
			slog.Warn("failed to read agent yaml", "file", entry.Name(), "error", err)
			continue
		}

		var ya yamlAgent
		if err := yaml.Unmarshal(data, &ya); err != nil {
			slog.Warn("failed to parse agent yaml", "file", entry.Name(), "error", err)
			continue
		}

		if ya.ID == "" || ya.Backend == "" {
			slog.Warn("agent yaml missing id or backend", "file", entry.Name())
			continue
		}

		// Check if agent already exists in DB
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = ?", ya.ID).Scan(&count)
		if err != nil {
			slog.Warn("failed to query agents table", "id", ya.ID, "error", err)
			continue
		}
		if count > 0 {
			continue // Don't overwrite existing DB records
		}

		agent := &Agent{
			ID:                      ya.ID,
			Name:                    ya.Name,
			Icon:                    ya.Icon,
			Specialty:               ya.Specialty,
			Backend:                 ya.Backend,
			Command:                 ya.Command,
			ThinkingEffort:          ya.ThinkingEffort,
			ThinkingEffortLevels:    ya.ThinkingEffortLevels,
			PreferredModel:          ya.PreferredModel,
			PreferredThinkingEffort: ya.PreferredThinkingEffort,
			SystemPrompt:            ya.SystemPrompt,
			Transport:               ya.Transport,
			AcpCommand:              ya.AcpCommand,
			Models:                  ya.Models,
			SortOrder:               ya.SortOrder,
			Source:                  "manual",
			ModelsAutoDetected:      len(ya.Models) == 0,
		}

		if err := saveAgentToDB(db, agent); err != nil {
			slog.Warn("failed to insert yaml agent to DB", "id", ya.ID, "error", err)
			continue
		}
		slog.Info("loaded agent from yaml config", "id", ya.ID, "file", entry.Name())
	}
}

// MergeDiscoveredDataDB is the DB-based replacement for MergeDiscoveredData.
// It performs three operations:
// 1. Soft-delete: DELETE auto-source agents whose backend is not in the present map
// 2. Fill ThinkingEffortLevels from BackendRegistry and update DB
// 3. Fill Models from cache for agents with empty models and update DB
// 4. Reload in-memory state from DB
func MergeDiscoveredDataDB(db *sql.DB, discoveredModels map[string][]AgentModel, present map[string]bool) { //nolint:gocognit,gocyclo // multi-step data merge
	// Step 1: Soft-delete auto agents whose CLI is not present
	if present != nil {
		// Build list of present backends for SQL
		presentBackends := make([]string, 0, len(present))
		for backend := range present {
			presentBackends = append(presentBackends, backend)
		}

		// Delete auto-source agents whose backend is NOT in present
		if len(presentBackends) > 0 {
			// Build placeholders
			placeholders := make([]string, len(presentBackends))
			args := make([]any, len(presentBackends)+1)
			args[0] = "auto" // source
			for i, b := range presentBackends {
				placeholders[i] = "?"
				args[i+1] = b
			}
			query := fmt.Sprintf("DELETE FROM agents WHERE source = ? AND backend NOT IN (%s)",
				strings.Join(placeholders, ","))
			result, err := db.Exec(query, args...)
			if err != nil {
				slog.Warn("failed to soft-delete missing CLI agents", "error", err)
			} else if rows, _ := result.RowsAffected(); rows > 0 {
				slog.Info("soft-deleted agents with missing CLIs", "count", rows)
			}
		} else {
			// No backends present — delete all auto agents
			result, err := db.Exec("DELETE FROM agents WHERE source = ?", "auto")
			if err != nil {
				slog.Warn("failed to soft-delete all auto agents", "error", err)
			} else if rows, _ := result.RowsAffected(); rows > 0 {
				slog.Info("soft-deleted all auto agents (no CLIs present)", "count", rows)
			}
		}
	}

	// Step 2: Fill ThinkingEffortLevels from BackendRegistry and update DB
	rows, err := db.Query("SELECT id, backend FROM agents")
	if err != nil {
		slog.Warn("failed to query agents for merge", "error", err)
		return
	}
	type agentRef struct {
		ID      string
		Backend string
	}
	var agentRefs []agentRef
	for rows.Next() {
		var ref agentRef
		if err := rows.Scan(&ref.ID, &ref.Backend); err != nil {
			continue
		}
		agentRefs = append(agentRefs, ref)
	}
	_ = rows.Close()

	for _, ref := range agentRefs {
		spec := FindSpecByBackend(ref.Backend)
		if spec == nil {
			continue
		}

		// Update ThinkingEffortLevels
		levelsJSON, _ := json.Marshal(spec.ThinkingEffortLevels)
		if _, err := db.Exec("UPDATE agents SET thinking_effort_levels = ? WHERE id = ?",
			string(levelsJSON), ref.ID); err != nil {
			slog.Warn("failed to update thinking_effort_levels", "id", ref.ID, "error", err)
		}
	}

	// Step 3: Fill Models from discovered results for agents with empty models
	rows, err = db.Query("SELECT id, backend, COALESCE(models, '[]') FROM agents WHERE (models IS NULL OR models = '[]' OR models = 'null') AND models_auto_detected = 0")
	if err != nil {
		slog.Warn("failed to query agents for model fill", "error", err)
		return
	}
	type agentModelRef struct {
		ID      string
		Backend string
	}
	var modelRefs []agentModelRef
	for rows.Next() {
		var ref agentModelRef
		var modelsStr string
		if err := rows.Scan(&ref.ID, &ref.Backend, &modelsStr); err != nil {
			continue
		}
		modelRefs = append(modelRefs, ref)
	}
	_ = rows.Close()

	for _, ref := range modelRefs {
		cached, ok := discoveredModels[ref.Backend]
		if !ok || len(cached) == 0 {
			continue
		}
		modelsJSON, _ := json.Marshal(cached)
		if _, err := db.Exec("UPDATE agents SET models = ?, models_auto_detected = 1 WHERE id = ?",
			string(modelsJSON), ref.ID); err != nil {
			slog.Warn("failed to update models from discovery", "id", ref.ID, "error", err)
		}
	}

	// Step 4: Reload in-memory state from DB
	agents, err := loadAgentsFromDBRows(db)
	if err != nil {
		slog.Warn("failed to reload agents from DB after merge", "error", err)
		return
	}

	Agents = make(map[string]*Agent)
	AgentList = agents
	for _, agent := range agents {
		Agents[agent.ID] = agent
		// Set CanRefreshModels from BackendRegistry (runtime only, not persisted)
		if spec := FindSpecByBackend(agent.Backend); spec != nil {
			agent.CanRefreshModels = CanDiscoverModels(*spec)
		}
	}

	// Build common prompt and prepend to each agent's system prompt
	commonPrompt := BuildCommonPrompt()
	for _, agent := range Agents {
		if commonPrompt != "" && agent.SystemPrompt != "" {
			agent.SystemPrompt = commonPrompt + "\n\n" + agent.SystemPrompt
		} else if commonPrompt != "" {
			agent.SystemPrompt = commonPrompt
		}
	}
}

// loadAgentsFromDBRows loads agents from the database into Agent structs.
func loadAgentsFromDBRows(db *sql.DB) ([]*Agent, error) {
	rows, err := db.Query(`SELECT id, name, icon, specialty, backend, command,
		thinking_effort, thinking_effort_levels, preferred_model, preferred_thinking_effort,
		system_prompt, models, models_auto_detected, source, sort_order,
		transport, acp_command
		FROM agents ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var agents []*Agent
	for rows.Next() {
		agent := &Agent{}
		var modelsJSON, levelsJSON string
		var autoDetected int

		err := rows.Scan(&agent.ID, &agent.Name, &agent.Icon, &agent.Specialty,
			&agent.Backend, &agent.Command, &agent.ThinkingEffort, &levelsJSON,
			&agent.PreferredModel, &agent.PreferredThinkingEffort,
			&agent.SystemPrompt, &modelsJSON, &autoDetected,
			&agent.Source, &agent.SortOrder,
			&agent.Transport, &agent.AcpCommand)
		if err != nil {
			return nil, err
		}

		agent.ModelsAutoDetected = autoDetected == 1

		if err := json.Unmarshal([]byte(modelsJSON), &agent.Models); err != nil {
			agent.Models = nil
		}
		if err := json.Unmarshal([]byte(levelsJSON), &agent.ThinkingEffortLevels); err != nil {
			agent.ThinkingEffortLevels = nil
		}

		agents = append(agents, agent)
	}
	return agents, nil
}
