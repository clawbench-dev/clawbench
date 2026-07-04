//nolint:noctx // DB parameter, context not applicable
package service

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"clawbench/internal/dbutil"
	"clawbench/internal/model"
)

// AgentDDL creates the agents and agent_api_keys tables.
// Exported so handler tests and other external packages can create these tables
// in their test databases.
const AgentDDL = `
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	icon TEXT NOT NULL DEFAULT '',
	specialty TEXT NOT NULL DEFAULT '',
	backend TEXT NOT NULL,
	command TEXT NOT NULL DEFAULT '',
	thinking_effort TEXT NOT NULL DEFAULT '',
	thinking_effort_levels TEXT NOT NULL DEFAULT '[]',
	preferred_mode TEXT NOT NULL DEFAULT '',
	preferred_model TEXT NOT NULL DEFAULT '',
	preferred_thinking_effort TEXT NOT NULL DEFAULT '',
	system_prompt TEXT NOT NULL DEFAULT '',
	custom_system_prompt TEXT NOT NULL DEFAULT '',
	models TEXT NOT NULL DEFAULT '[]',
	models_auto_detected INTEGER NOT NULL DEFAULT 0,
	source TEXT NOT NULL DEFAULT 'auto',
	sort_order INTEGER NOT NULL DEFAULT 0,
	transport TEXT NOT NULL DEFAULT 'cli',
	acp_command TEXT NOT NULL DEFAULT '',
	acp_available_modes TEXT NOT NULL DEFAULT '[]',
	acp_available_thinking_efforts TEXT NOT NULL DEFAULT '[]',
	acp_available_commands TEXT NOT NULL DEFAULT '[]',
	acp_config_options TEXT NOT NULL DEFAULT '',
	acp_cached_usage_state TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_agents_backend ON agents(backend);
CREATE INDEX IF NOT EXISTS idx_agents_source ON agents(source);
CREATE INDEX IF NOT EXISTS idx_agents_sort ON agents(sort_order);

CREATE TABLE IF NOT EXISTS agent_api_keys (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	agent_id TEXT NOT NULL,
	provider TEXT NOT NULL,
	custom_url TEXT NOT NULL DEFAULT '',
	encrypted_key TEXT NOT NULL,
	key_nonce TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_api_keys_agent_provider
	ON agent_api_keys(agent_id, provider);
`

// LoadAgentsFromDB loads all agents from the database and returns them sorted by ID.
func LoadAgentsFromDB() ([]*model.Agent, error) {
	rows, err := dbRead.Query(`
		SELECT id, name, icon, specialty, backend, command,
			thinking_effort, thinking_effort_levels,
			preferred_mode, preferred_model, preferred_thinking_effort,
			system_prompt, custom_system_prompt, models, models_auto_detected,
			source, sort_order,
			transport, acp_command
		FROM agents ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []*model.Agent
	for rows.Next() {
		a := &model.Agent{}
		var modelsJSON, levelsJSON string
		var modelsAutoDetected int

		err := rows.Scan(
			&a.ID, &a.Name, &a.Icon, &a.Specialty, &a.Backend, &a.Command,
			&a.ThinkingEffort, &levelsJSON,
			&a.PreferredMode, &a.PreferredModel, &a.PreferredThinkingEffort,
			&a.SystemPrompt, &a.CustomSystemPrompt, &modelsJSON, &modelsAutoDetected,
			&a.Source, &a.SortOrder,
			&a.Transport, &a.AcpCommand,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}

		a.ModelsAutoDetected = modelsAutoDetected == 1

		// Parse models JSON
		if modelsJSON != "" && modelsJSON != "[]" {
			var models []model.AgentModel
			if err := json.Unmarshal([]byte(modelsJSON), &models); err == nil {
				a.Models = models
			}
		}

		// Parse thinking effort levels JSON
		if levelsJSON != "" && levelsJSON != "[]" {
			var levels []string
			if err := json.Unmarshal([]byte(levelsJSON), &levels); err == nil {
				a.ThinkingEffortLevels = levels
			}
		}

		agents = append(agents, a)
	}

	return agents, rows.Err()
}

func SaveAgent(db dbutil.Writer, agent *model.Agent) error {
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

	modelsAutoDetected := 0
	if agent.ModelsAutoDetected {
		modelsAutoDetected = 1
	}

	sortOrder := agent.SortOrder
	transport := agent.Transport
	if transport == "" {
		if agent.AcpCommand != "" {
			transport = "acp-stdio"
		} else {
			transport = "cli"
		}
	}

	_, err = db.Exec(`
		INSERT INTO agents (id, name, icon, specialty, backend, command,
			thinking_effort, thinking_effort_levels,
			preferred_mode, preferred_model, preferred_thinking_effort,
			system_prompt, custom_system_prompt, models, models_auto_detected,
			source, sort_order,
			transport, acp_command)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			icon = excluded.icon,
			specialty = excluded.specialty,
			backend = excluded.backend,
			command = excluded.command,
			thinking_effort = excluded.thinking_effort,
			thinking_effort_levels = excluded.thinking_effort_levels,
			preferred_mode = excluded.preferred_mode,
			preferred_model = excluded.preferred_model,
			preferred_thinking_effort = excluded.preferred_thinking_effort,
			system_prompt = excluded.system_prompt,
			custom_system_prompt = excluded.custom_system_prompt,
			models = excluded.models,
			models_auto_detected = excluded.models_auto_detected,
			source = excluded.source,
			sort_order = excluded.sort_order,
			transport = excluded.transport,
			acp_command = excluded.acp_command,
			updated_at = CURRENT_TIMESTAMP
	`, agent.ID, agent.Name, agent.Icon, agent.Specialty, agent.Backend, agent.Command,
		agent.ThinkingEffort, string(levelsJSON),
		agent.PreferredMode, agent.PreferredModel, agent.PreferredThinkingEffort,
		agent.SystemPrompt, agent.CustomSystemPrompt, string(modelsJSON), modelsAutoDetected,
		agent.Source, sortOrder,
		transport, agent.AcpCommand)
	if err != nil {
		return fmt.Errorf("save agent %s: %w", agent.ID, err)
	}
	return nil
}

// DeleteAgent deletes an agent by ID. Cascades to agent_api_keys (requires PRAGMA foreign_keys=ON).
// Returns nil even if the agent doesn't exist.
func DeleteAgent(id string) error {
	// Ensure foreign keys are enforced for cascade delete
	_, _ = WriteExec("PRAGMA foreign_keys = ON")
	_, err := WriteExec("DELETE FROM agents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete agent %s: %w", id, err)
	}
	return nil
}

// PatchAgent updates only the original user-editable fields (preferred_model, preferred_thinking_effort, transport).
// Returns nil even if the agent doesn't exist (no rows affected).
// Kept for backward compatibility — delegates to PatchAgentFields.
func PatchAgent(id, preferredModel, preferredThinkingEffort, transport string) error {
	patch := AgentPatch{
		PreferredModel:          &preferredModel,
		PreferredThinkingEffort: &preferredThinkingEffort,
		Transport:               &transport,
	}
	return PatchAgentFields(id, patch)
}

// AgentPatch holds optional fields for partial agent updates.
// Pointer fields distinguish "not provided" (nil) from "set to empty/zero".
type AgentPatch struct {
	PreferredMode           *string
	PreferredModel          *string
	PreferredThinkingEffort *string
	Transport               *string
	Name                    *string
	Icon                    *string
	Specialty               *string
	CustomSystemPrompt      *string
	SortOrder               *int
}

// PatchAgentFields updates only the non-nil fields in the AgentPatch struct.
// Returns nil even if the agent doesn't exist (no rows affected).
func PatchAgentFields(id string, patch AgentPatch) error { //nolint:gocyclo // multi-field dynamic patch builder
	// Build dynamic SET clause
	setClauses := []string{}
	args := []any{}

	if patch.PreferredMode != nil {
		setClauses = append(setClauses, "preferred_mode = ?")
		args = append(args, *patch.PreferredMode)
	}
	if patch.PreferredModel != nil {
		setClauses = append(setClauses, "preferred_model = ?")
		args = append(args, *patch.PreferredModel)
	}
	if patch.PreferredThinkingEffort != nil {
		setClauses = append(setClauses, "preferred_thinking_effort = ?")
		args = append(args, *patch.PreferredThinkingEffort)
	}
	if patch.Transport != nil {
		transport := *patch.Transport
		if transport == "" {
			transport = "cli"
		}
		setClauses = append(setClauses, "transport = ?")
		args = append(args, transport)
	}
	if patch.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Icon != nil {
		setClauses = append(setClauses, "icon = ?")
		args = append(args, *patch.Icon)
	}
	if patch.Specialty != nil {
		setClauses = append(setClauses, "specialty = ?")
		args = append(args, *patch.Specialty)
	}
	if patch.CustomSystemPrompt != nil {
		setClauses = append(setClauses, "custom_system_prompt = ?", "system_prompt = ?")
		// Compose system_prompt from common prompt + custom_system_prompt
		commonPrompt := model.BuildCommonPrompt()
		custom := *patch.CustomSystemPrompt
		if commonPrompt != "" && custom != "" {
			args = append(args, custom, commonPrompt+"\n\n"+custom)
		} else if commonPrompt != "" {
			args = append(args, custom, commonPrompt)
		} else {
			args = append(args, custom, custom)
		}
	}
	if patch.SortOrder != nil {
		setClauses = append(setClauses, "sort_order = ?")
		args = append(args, *patch.SortOrder)
	}

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	setClauses = append(setClauses, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := "UPDATE agents SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	_, err := WriteExec(query, args...)
	if err != nil {
		return fmt.Errorf("patch agent %s: %w", id, err)
	}
	return nil
}

// LoadAgentsIntoMemory loads agents from DB into the global model.Agents map and model.AgentList slice.
// Also builds the common prompt and prepends it to each agent's system prompt.
func LoadAgentsIntoMemory() error {
	agents, err := LoadAgentsFromDB()
	if err != nil {
		return err
	}

	// Build new map fully before assigning to avoid a window where
	// concurrent HTTP handlers see 0 agents (ISS-302).
	newAgentsMap := make(map[string]*model.Agent, len(agents))
	model.AgentList = agents

	for _, agent := range agents {
		newAgentsMap[agent.ID] = agent
		// Populate runtime-only fields from BackendRegistry
		// (CanRefreshModels and ThinkingEffortLevels are not persisted in DB)
		if spec := model.FindSpecByBackend(agent.Backend); spec != nil {
			if model.CanDiscoverModels(*spec) {
				agent.CanRefreshModels = true
			}
			if len(agent.ThinkingEffortLevels) == 0 && len(spec.ThinkingEffortLevels) > 0 {
				agent.ThinkingEffortLevels = spec.ThinkingEffortLevels
			}
		}
	}

	// Atomically assign the fully-built map so concurrent readers never see an empty map.
	model.Agents = newAgentsMap

	// Sort by ID for deterministic ordering
	sort.Slice(model.AgentList, func(i, j int) bool {
		return model.AgentList[i].ID < model.AgentList[j].ID
	})

	// Build common prompt from embedded rules
	commonPrompt := model.BuildCommonPrompt()

	// Compose SystemPrompt from commonPrompt + CustomSystemPrompt for each agent.
	// This ensures SystemPrompt is always the full composed prompt at runtime,
	// while the DB stores only the user-editable CustomSystemPrompt portion.
	for _, agent := range model.Agents {
		if commonPrompt != "" && agent.CustomSystemPrompt != "" {
			agent.SystemPrompt = commonPrompt + "\n\n" + agent.CustomSystemPrompt
		} else if commonPrompt != "" {
			agent.SystemPrompt = commonPrompt
		}
		// If CustomSystemPrompt is empty but SystemPrompt has content (legacy data),
		// keep SystemPrompt as-is so existing agents don't lose their prompts.
	}

	return nil
}

// DuplicateAgent creates a new agent by cloning an existing one.
// It generates a unique ID (sourceID-copy-timestamp), copies all configuration
// fields from the source, sets source="manual", and saves to DB.
func DuplicateAgent(sourceID, newName string) (*model.Agent, error) {
	source, ok := model.Agents[sourceID]
	if !ok {
		return nil, fmt.Errorf("source agent %s not found", sourceID)
	}

	newID := fmt.Sprintf("%s-copy-%d", sourceID, time.Now().UnixMilli())

	clone := &model.Agent{
		ID:                      newID,
		Name:                    newName,
		Icon:                    source.Icon,
		Specialty:               source.Specialty,
		Backend:                 source.Backend,
		Command:                 source.Command,
		ThinkingEffort:          source.ThinkingEffort,
		ThinkingEffortLevels:    make([]string, len(source.ThinkingEffortLevels)),
		PreferredMode:           source.PreferredMode,
		PreferredModel:          source.PreferredModel,
		PreferredThinkingEffort: source.PreferredThinkingEffort,
		CustomSystemPrompt:      source.CustomSystemPrompt,
		Transport:               source.Transport,
		AcpCommand:              source.AcpCommand,
		Source:                  "manual",
		SortOrder:               source.SortOrder,
	}
	copy(clone.ThinkingEffortLevels, source.ThinkingEffortLevels)
	if len(source.Models) > 0 {
		clone.Models = make([]model.AgentModel, len(source.Models))
		copy(clone.Models, source.Models)
	}

	// Compose SystemPrompt from common prompt + custom
	commonPrompt := model.BuildCommonPrompt()
	if commonPrompt != "" && clone.CustomSystemPrompt != "" {
		clone.SystemPrompt = commonPrompt + "\n\n" + clone.CustomSystemPrompt
	} else if commonPrompt != "" {
		clone.SystemPrompt = commonPrompt
	} else {
		clone.SystemPrompt = clone.CustomSystemPrompt
	}

	if err := SaveAgent(WriteDB(), clone); err != nil {
		return nil, fmt.Errorf("save duplicated agent: %w", err)
	}

	return clone, nil
}

// MigrateCustomSystemPrompt backfills the custom_system_prompt column for agents
// that have a system_prompt but an empty custom_system_prompt. It strips the
// common prompt prefix from the stored system_prompt and stores the remainder
// as custom_system_prompt.
func MigrateCustomSystemPrompt() {
	commonPrompt := model.BuildCommonPrompt()
	if commonPrompt == "" {
		return
	}

	rows, err := dbRead.Query("SELECT id, system_prompt, custom_system_prompt FROM agents WHERE custom_system_prompt = '' AND system_prompt != ''")
	if err != nil {
		slog.Error("migrate custom_system_prompt: query failed", "error", err)
		return
	}

	// Collect all rows first to release the DB connection before executing UPDATEs.
	// This avoids deadlocking with MaxOpenConns=1 (SQLite single-writer).
	type migRow struct {
		id           string
		systemPrompt string
	}
	var toMigrate []migRow
	for rows.Next() {
		var id, systemPrompt, customPrompt string
		if err := rows.Scan(&id, &systemPrompt, &customPrompt); err != nil {
			slog.Warn("migrate custom_system_prompt: scan failed", "error", err)
			continue
		}
		toMigrate = append(toMigrate, migRow{id: id, systemPrompt: systemPrompt})
	}
	if err := rows.Err(); err != nil {
		slog.Warn("migrate custom_system_prompt: row iteration error", "error", err)
	}
	//nolint:sqlclosecheck // must close before UPDATE loop to release DB connection early
	if err := rows.Close(); err != nil {
		slog.Warn("migrate custom_system_prompt: close rows failed", "error", err)
	}

	migrated := 0
	for _, row := range toMigrate {
		// Strip common prompt prefix
		custom := row.systemPrompt
		if strings.HasPrefix(row.systemPrompt, commonPrompt+"\n\n") {
			custom = strings.TrimPrefix(row.systemPrompt, commonPrompt+"\n\n")
		} else if row.systemPrompt == commonPrompt {
			custom = ""
		}

		if _, err := WriteExec("UPDATE agents SET custom_system_prompt = ? WHERE id = ?", custom, row.id); err != nil {
			slog.Warn("migrate custom_system_prompt: update failed", "agent", row.id, "error", err)
			continue
		}
		migrated++
	}
	if migrated > 0 {
		slog.Info("migrated custom_system_prompt", slog.Int("count", migrated))
	}
}
