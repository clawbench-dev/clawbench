// Package model — unified agent + model refresh.
//
// This file replaces a five-step startup sequence that each step re-queried the
// database and reloaded global state:
//
//	SyncDiscoverAgentsDB   → detect CLIs, insert new agents, sync acp_command
//	LoadYamlAgents         → insert agents from config/agents/*.yaml
//	SyncDiscoverModels     → probe every backend for its model list
//	MergeDiscoveredDataDB  → three passes of SQL, then a full memory reload
//	AsyncRefreshModelCache → probe every backend AGAIN in a goroutine
//
// Beyond the duplicate work, that sequence had two structural problems. First,
// there were two independent implementations of "load agents into memory and
// compose the system prompt" — this file's and service.LoadAgentsIntoMemory —
// so which one won depended on call order. Second, the same discovery probes ran
// twice per boot (once synchronously, once in the background), and neither run
// was cached.
//
// RefreshAgents is now the only entry point. It performs detection, discovery,
// persistence and the memory reload in one pass, and it is what both startup and
// the manual rescan endpoint call.
package model

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"clawbench/internal/dbutil"
	"gopkg.in/yaml.v3"
)

// RefreshOptions configures RefreshAgents.
type RefreshOptions struct {
	// ConfigDir is the directory holding an optional agents/ subdirectory of
	// YAML agent definitions. Empty disables YAML loading.
	ConfigDir string
	// SkipDiscovery leaves the model lists untouched and only re-runs CLI
	// detection. Used by callers that want agent presence refreshed without
	// paying for every backend probe.
	SkipDiscovery bool
}

// RefreshResult reports what a refresh changed. Callers log it; tests assert on it.
type RefreshResult struct {
	// PresentCLIs is the set of backends whose CLI was detected on PATH.
	PresentCLIs map[string]bool
	// DiscoveredModels is the freshly probed model list per backend.
	DiscoveredModels map[string][]AgentModel
	// InsertedAgents lists agent IDs created by this refresh.
	InsertedAgents []string
	// ModelRefreshedBackends lists the backends whose discovered model list was
	// written to at least one agent row. Named for backends, not agent IDs,
	// because one probe covers every agent of that backend.
	ModelRefreshedBackends []string
	// LoadedAgents is the number of agents now in memory.
	LoadedAgents int
}

// RefreshAgents brings the agents table and the in-memory agent set in line with
// what is installed and what the backends report, then returns what changed.
//
// Order matters and is the reason this is one function rather than five:
//
//  1. Detect installed CLIs and insert agents for newly present backends. Must
//     precede discovery so a freshly installed backend gets its models in the
//     same pass.
//  2. Load YAML-defined agents, which are not in the backend registry and so are
//     never detected by step 1.
//  3. Probe each backend for its model list, and persist the result for agents
//     whose model list is auto-managed.
//  4. Reload from the database, which is the single source of truth for agent
//     configuration, and populate runtime-only fields.
//
// The database is authoritative: a refresh never overwrites a user-defined model
// list, a customized name, or a customized command.
func RefreshAgents(db dbutil.Writer, opts RefreshOptions) (*RefreshResult, error) {
	result := &RefreshResult{
		PresentCLIs:      make(map[string]bool),
		DiscoveredModels: make(map[string][]AgentModel),
	}

	// Step 1: CLI detection and agent insertion.
	present, inserted := detectAndInsertAgents(db)
	result.PresentCLIs = present
	result.InsertedAgents = inserted

	// Step 2: YAML-defined agents.
	if opts.ConfigDir != "" {
		yamlInserted := LoadYamlAgents(db, opts.ConfigDir)
		result.InsertedAgents = append(result.InsertedAgents, yamlInserted...)
	}

	// Step 3: model discovery, persisted for auto-managed agents.
	if !opts.SkipDiscovery {
		discovered, updated := discoverAndPersistModels(db)
		result.DiscoveredModels = discovered
		result.ModelRefreshedBackends = updated
	}

	// Step 3b: spec-derived thinking effort levels. These are not user-editable
	// (AgentPatch has no field for them), so the column should always mirror the
	// backend spec. Without this write a spec that gains a level would keep the
	// stale value in the DB, and the in-memory fallback below only fills an EMPTY
	// list — so the new level would never appear.
	syncThinkingEffortLevels(db)

	// Step 4: reload memory from the database.
	if err := LoadAgentsIntoMemoryFromDB(db); err != nil {
		return result, err
	}
	result.LoadedAgents = len(AgentList)

	slog.Info("agents refreshed",
		"present", len(result.PresentCLIs),
		"inserted", len(result.InsertedAgents),
		"models_updated", len(result.ModelRefreshedBackends),
		"total", result.LoadedAgents)
	return result, nil
}

// syncThinkingEffortLevels writes each agent's spec-declared thinking effort
// levels to the database. The values are derived, never user-set, so overwriting
// is safe.
func syncThinkingEffortLevels(db dbutil.Writer) {
	rows, err := db.Query("SELECT id, backend FROM agents")
	if err != nil {
		slog.Warn("failed to query agents for thinking effort levels", "error", err)
		return
	}
	type ref struct{ id, backend string }
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.id, &r.backend); err == nil {
			refs = append(refs, r)
		}
	}
	_ = rows.Close()

	for _, r := range refs {
		spec := FindSpecByBackend(r.backend)
		if spec == nil || len(spec.ThinkingEffortLevels) == 0 {
			continue
		}
		levelsJSON, err := json.Marshal(spec.ThinkingEffortLevels)
		if err != nil {
			continue
		}
		if _, err := db.Exec("UPDATE agents SET thinking_effort_levels = ? WHERE id = ?",
			string(levelsJSON), r.id); err != nil {
			slog.Warn("failed to update thinking_effort_levels", "id", r.id, "error", err)
		}
	}
}

// detectAndInsertAgents probes PATH for every registered backend, inserts an
// agent for each newly present one, and syncs spec-derived infrastructure fields
// (acp_command, transport) on existing records.
//
// Returns the set of present backends and the IDs of inserted agents.
//
// A per-backend failure is logged and skipped rather than aborting the refresh:
// one unreadable row must not stop the other backends from being detected.
func detectAndInsertAgents(db dbutil.Writer) (map[string]bool, []string) {
	registry := GetBackendRegistry()
	presence := probeBackendPresence(registry)

	present := make(map[string]bool, len(registry))
	var inserted []string

	for i, spec := range registry {
		if presence[i] {
			present[spec.Backend] = true
		}
		if id, ok := syncOrInsertAgent(db, spec, presence[i]); ok {
			inserted = append(inserted, id)
		}
	}

	// Backends with DB records but no registry entry (wizard-created, YAML-only)
	// are still "present" from the caller's perspective.
	rows, err := db.Query("SELECT DISTINCT backend FROM agents")
	if err != nil {
		return present, inserted
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var backend string
		if err := rows.Scan(&backend); err == nil {
			present[backend] = true
		}
	}
	return present, inserted
}

// probeBackendPresence checks PATH for every registered backend in parallel.
// Each check spawns a process, and the registry is large enough that serial
// probing would dominate startup.
func probeBackendPresence(registry []BackendSpec) []bool {
	presence := make([]bool, len(registry))
	var wg sync.WaitGroup
	for i, spec := range registry {
		wg.Add(1)
		go func(i int, spec BackendSpec) {
			defer wg.Done()
			presence[i] = spec.NoCLI ||
				CheckCLIExists(spec.DefaultCmd) ||
				(spec.AltCmd != "" && CheckCLIExists(spec.AltCmd))
		}(i, spec)
	}
	wg.Wait()
	return presence
}

// syncOrInsertAgent reconciles one backend with the database: an existing record
// gets its spec-derived infrastructure fields synced, a newly present backend
// gets a fresh agent row. It reports the inserted agent ID, if any.
func syncOrInsertAgent(db dbutil.Writer, spec BackendSpec, exists bool) (string, bool) {
	var count int
	var existingAcpCommand, existingTransport string
	err := db.QueryRow(
		"SELECT COUNT(*), COALESCE(acp_command, ''), COALESCE(transport, '') FROM agents WHERE backend = ?",
		spec.Backend,
	).Scan(&count, &existingAcpCommand, &existingTransport)
	if err != nil {
		slog.Warn("failed to query agents table", "backend", spec.Backend, "error", err)
		return "", false
	}

	if count > 0 {
		if syncAgentInfrastructure(db, spec, existingAcpCommand, existingTransport) {
			slog.Info("synced agent infrastructure from backend spec", "backend", spec.Backend)
		}
		return "", false
	}

	if !exists {
		return "", false
	}

	agent := &Agent{
		ID:        spec.ID,
		Name:      spec.Name,
		Specialty: spec.Specialty,
		Backend:   spec.Backend,
	}
	if spec.AcpCommand != "" {
		agent.AcpCommand = spec.AcpCommand
		agent.Transport = "acp-stdio"
	}
	if err := saveAgentToDB(db, agent); err != nil {
		slog.Warn("failed to insert discovered agent", "backend", spec.ID, "error", err)
		return "", false
	}
	slog.Info("auto-inserted agent", "backend", spec.ID)
	return agent.ID, true
}

// syncAgentInfrastructure updates the fields derived from a BackendSpec when they
// drift. User-customized name and command are deliberately left alone.
func syncAgentInfrastructure(db dbutil.Writer, spec BackendSpec, existingAcpCommand, existingTransport string) bool {
	updates := map[string]interface{}{}

	// Sync acp_command in both directions: a newly added ACP command must be
	// recorded, and a removed one must be cleared so a stale value cannot make
	// the agent advertise a transport it no longer supports.
	if spec.AcpCommand != existingAcpCommand {
		updates["acp_command"] = spec.AcpCommand
	}
	switch {
	case spec.AcpCommand != "" && existingTransport == "cli":
		updates["transport"] = "acp-stdio"
	case spec.AcpCommand == "" && existingTransport == "acp-stdio":
		updates["transport"] = "cli"
	}

	if len(updates) == 0 {
		return false
	}

	setClauses := make([]string, 0, len(updates))
	args := make([]interface{}, 0, len(updates)+1)
	for _, col := range sortedKeys(updates) {
		setClauses = append(setClauses, col+" = ?")
		args = append(args, updates[col])
	}
	args = append(args, spec.Backend)

	if _, err := db.Exec("UPDATE agents SET "+strings.Join(setClauses, ", ")+" WHERE backend = ?", args...); err != nil {
		slog.Warn("failed to sync agent infrastructure", "backend", spec.Backend, "error", err)
		return false
	}
	return true
}

// sortedKeys returns map keys in a stable order, so generated SQL and its
// argument list do not vary between runs.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// discoverAndPersistModels probes every backend that has a model source and
// writes the result to the agents whose model list is auto-managed.
//
// Two row shapes are written:
//
//   - models_auto_detected = 1 — discovery already owns this list, so refresh it.
//   - models_auto_detected = 0 with an EMPTY list — nobody has populated the row
//     yet (a backend detected in this same refresh, or a wizard-created agent
//     whose discovery never ran). Fill it and mark it auto-managed.
//
// A non-empty list with flag 0 is the user's own choice and is never touched.
// The empty-and-flag-0 case must be included: step 1 inserts agents with an empty
// list and no flag, so excluding it would leave every freshly installed backend
// with an empty model picker until a manual refresh.
func discoverAndPersistModels(db dbutil.Writer) (map[string][]AgentModel, []string) {
	discovered := make(map[string][]AgentModel)
	var updated []string

	for _, backend := range RegisteredModelSources() {
		models := DiscoverModels(backend)
		if len(models) == 0 {
			continue
		}
		discovered[backend] = models

		modelsJSON, err := json.Marshal(models)
		if err != nil {
			slog.Warn("failed to marshal discovered models", "backend", backend, "error", err)
			continue
		}
		res, err := db.Exec(
			`UPDATE agents SET models = ?, models_auto_detected = 1
			 WHERE backend = ? AND (models_auto_detected = 1 OR models IS NULL OR models = '[]' OR models = 'null')`,
			string(modelsJSON), backend,
		)
		if err != nil {
			slog.Warn("failed to persist discovered models", "backend", backend, "error", err)
			continue
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			updated = append(updated, backend)
		}
	}
	return discovered, updated
}

// saveAgentToDB inserts a minimal agent record.
func saveAgentToDB(db dbutil.Writer, agent *Agent) error {
	modelsJSON, err := json.Marshal(agent.Models)
	if err != nil {
		return err
	}
	// json.Marshal(nil slice) yields "null"; normalize so queries matching on
	// '[]' see a consistent value.
	if string(modelsJSON) == "null" {
		modelsJSON = []byte("[]")
	}
	levelsJSON, err := json.Marshal(agent.ThinkingEffortLevels)
	if err != nil {
		return err
	}

	transport := agent.Transport
	if transport == "" {
		if agent.AcpCommand != "" {
			transport = "acp-stdio"
		} else {
			transport = "cli"
		}
	}
	autoApprove := 0
	if agent.AutoApprove {
		autoApprove = 1
	}

	_, err = db.Exec(`INSERT INTO agents (id, name, specialty, backend, command,
		thinking_effort, thinking_effort_levels,
		preferred_mode, preferred_model, preferred_thinking_effort,
		custom_system_prompt, models, models_auto_detected, sort_order,
		transport, acp_command, auto_approve)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.Name, agent.Specialty, agent.Backend, agent.Command,
		agent.ThinkingEffort, string(levelsJSON), agent.PreferredMode, agent.PreferredModel, agent.PreferredThinkingEffort,
		agent.CustomSystemPrompt, string(modelsJSON), agent.ModelsAutoDetected, agent.SortOrder,
		transport, agent.AcpCommand, autoApprove)
	return err
}

// yamlAgent represents an agent definition in config/agents/*.yaml.
type yamlAgent struct {
	ID                      string       `yaml:"id"`
	Name                    string       `yaml:"name"`
	Specialty               string       `yaml:"specialty"`
	Backend                 string       `yaml:"backend"`
	Command                 string       `yaml:"command"`
	ThinkingEffort          string       `yaml:"thinking_effort"`
	ThinkingEffortLevels    []string     `yaml:"thinking_effort_levels"`
	PreferredMode           string       `yaml:"preferred_mode"`
	PreferredModel          string       `yaml:"preferred_model"`
	PreferredThinkingEffort string       `yaml:"preferred_thinking_effort"`
	CustomSystemPrompt      string       `yaml:"custom_system_prompt"`
	Transport               string       `yaml:"transport"`
	AcpCommand              string       `yaml:"acp_command"`
	Models                  []AgentModel `yaml:"models"`
	SortOrder               int          `yaml:"sort_order"`
}

// LoadYamlAgents reads <configDir>/agents/*.yaml and inserts agents that are not
// already in the database. This is the escape hatch for agents with no backend
// registry entry (for example the acp-mock used by E2E tests).
//
// Existing records are never overwritten: YAML is a bootstrap, not a sync source.
// Returns the IDs of inserted agents.
func LoadYamlAgents(db dbutil.Writer, configDir string) []string {
	agentsDir := filepath.Join(configDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read agents config dir", "path", agentsDir, "error", err)
		}
		return nil
	}

	var inserted []string
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

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = ?", ya.ID).Scan(&count); err != nil {
			slog.Warn("failed to query agents table", "id", ya.ID, "error", err)
			continue
		}
		if count > 0 {
			continue
		}

		agent := &Agent{
			ID:                      ya.ID,
			Name:                    ya.Name,
			Specialty:               ya.Specialty,
			Backend:                 ya.Backend,
			Command:                 ya.Command,
			ThinkingEffort:          ya.ThinkingEffort,
			ThinkingEffortLevels:    ya.ThinkingEffortLevels,
			PreferredMode:           ya.PreferredMode,
			PreferredModel:          ya.PreferredModel,
			PreferredThinkingEffort: ya.PreferredThinkingEffort,
			CustomSystemPrompt:      ya.CustomSystemPrompt,
			Transport:               ya.Transport,
			AcpCommand:              ya.AcpCommand,
			Models:                  ya.Models,
			SortOrder:               ya.SortOrder,
			// A YAML agent that declares no models is opting into discovery.
			ModelsAutoDetected: len(ya.Models) == 0,
		}
		if err := saveAgentToDB(db, agent); err != nil {
			slog.Warn("failed to insert yaml agent", "id", ya.ID, "error", err)
			continue
		}
		inserted = append(inserted, agent.ID)
		slog.Info("loaded agent from yaml config", "id", ya.ID, "file", entry.Name())
	}
	return inserted
}

// LoadAgentsIntoMemoryFromDB loads agents from the database into the global
// Agents map and AgentList, then populates the runtime-only fields that are not
// persisted: CanRefreshModels, SupportsCLI, SupportsMidTurn, ThinkingEffortLevels
// and the composed SystemPrompt.
//
// The new map is built completely before assignment so a concurrent reader never
// observes an empty agent set (ISS-302).
func LoadAgentsIntoMemoryFromDB(db dbutil.Reader) error {
	agents, err := loadAgentsFromDBRows(db)
	if err != nil {
		return err
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })

	newAgentsMap := make(map[string]*Agent, len(agents))

	for _, agent := range agents {
		if spec := FindSpecByBackend(agent.Backend); spec != nil {
			agent.CanRefreshModels = HasModelSource(agent.Backend)
			if len(agent.ThinkingEffortLevels) == 0 && len(spec.ThinkingEffortLevels) > 0 {
				agent.ThinkingEffortLevels = spec.ThinkingEffortLevels
			}
		}
		agent.SupportsCLI = BackendSupportsCLI(agent.Backend)
		agent.SupportsMidTurn = BackendSupportsMidTurn(agent.Backend)
		agent.RuntimeSystemPrompt = ComposeSystemPrompt(agent.CustomSystemPrompt)
		newAgentsMap[agent.ID] = agent
	}

	Agents = newAgentsMap
	AgentList = agents
	return nil
}

// ComposeSystemPrompt builds the runtime prompt from the shared prefix and the
// user-editable portion.
//
// The stored prompt column is deliberately ignored. It used to hold a composed
// prompt from an earlier scheme, which meant a frozen copy of the shared prompt
// was appended after the freshly composed one and silently overrode it. Only
// custom_system_prompt holds user text now, and the shared prefix is always
// applied here, so a change to the built-in prompt takes effect on every read.
func ComposeSystemPrompt(customPrompt string) string {
	commonPrompt := BuildCommonPrompt()
	switch {
	case commonPrompt != "" && customPrompt != "":
		return commonPrompt + "\n\n" + customPrompt
	case commonPrompt != "":
		return commonPrompt
	default:
		return customPrompt
	}
}

// loadAgentsFromDBRows reads every agent row.
func loadAgentsFromDBRows(db dbutil.Reader) ([]*Agent, error) {
	rows, err := db.Query(`SELECT id, name, specialty, backend, command,
		thinking_effort, thinking_effort_levels,
		preferred_mode, preferred_model, preferred_thinking_effort,
		custom_system_prompt, models, models_auto_detected, sort_order,
		transport, acp_command, auto_approve
		FROM agents ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var agents []*Agent
	for rows.Next() {
		agent := &Agent{}
		var modelsJSON, levelsJSON string
		var autoDetected, autoApprove int

		if err := rows.Scan(&agent.ID, &agent.Name, &agent.Specialty,
			&agent.Backend, &agent.Command, &agent.ThinkingEffort, &levelsJSON,
			&agent.PreferredMode, &agent.PreferredModel, &agent.PreferredThinkingEffort,
			&agent.CustomSystemPrompt, &modelsJSON, &autoDetected,
			&agent.SortOrder, &agent.Transport, &agent.AcpCommand, &autoApprove); err != nil {
			return nil, err
		}

		agent.ModelsAutoDetected = autoDetected == 1
		agent.AutoApprove = autoApprove == 1

		if err := json.Unmarshal([]byte(modelsJSON), &agent.Models); err != nil {
			agent.Models = nil
		}
		if err := json.Unmarshal([]byte(levelsJSON), &agent.ThinkingEffortLevels); err != nil {
			agent.ThinkingEffortLevels = nil
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}
