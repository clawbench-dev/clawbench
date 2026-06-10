//nolint:noctx // DB parameter, context not applicable
package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

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
	preferred_model TEXT NOT NULL DEFAULT '',
	preferred_thinking_effort TEXT NOT NULL DEFAULT '',
	system_prompt TEXT NOT NULL DEFAULT '',
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
func LoadAgentsFromDB(db *sql.DB) ([]*model.Agent, error) {
	rows, err := db.Query(`
		SELECT id, name, icon, specialty, backend, command,
			thinking_effort, thinking_effort_levels,
			preferred_model, preferred_thinking_effort,
			system_prompt, models, models_auto_detected,
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
			&a.PreferredModel, &a.PreferredThinkingEffort,
			&a.SystemPrompt, &modelsJSON, &modelsAutoDetected,
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

// SaveAgent inserts or updates an agent in the database (upsert).
// DBExec is the minimal interface for DB operations that work with both *sql.DB and *sql.Tx.
type DBExec interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func SaveAgent(db DBExec, agent *model.Agent) error {
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
		transport = "cli"
	}

	_, err = db.Exec(`
		INSERT INTO agents (id, name, icon, specialty, backend, command,
			thinking_effort, thinking_effort_levels,
			preferred_model, preferred_thinking_effort,
			system_prompt, models, models_auto_detected,
			source, sort_order,
			transport, acp_command)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			icon = excluded.icon,
			specialty = excluded.specialty,
			backend = excluded.backend,
			command = excluded.command,
			thinking_effort = excluded.thinking_effort,
			thinking_effort_levels = excluded.thinking_effort_levels,
			preferred_model = excluded.preferred_model,
			preferred_thinking_effort = excluded.preferred_thinking_effort,
			system_prompt = excluded.system_prompt,
			models = excluded.models,
			models_auto_detected = excluded.models_auto_detected,
			source = excluded.source,
			sort_order = excluded.sort_order,
			transport = excluded.transport,
			acp_command = excluded.acp_command,
			updated_at = CURRENT_TIMESTAMP
	`, agent.ID, agent.Name, agent.Icon, agent.Specialty, agent.Backend, agent.Command,
		agent.ThinkingEffort, string(levelsJSON),
		agent.PreferredModel, agent.PreferredThinkingEffort,
		agent.SystemPrompt, string(modelsJSON), modelsAutoDetected,
		agent.Source, sortOrder,
		transport, agent.AcpCommand)
	if err != nil {
		return fmt.Errorf("save agent %s: %w", agent.ID, err)
	}
	return nil
}

// DeleteAgent deletes an agent by ID. Cascades to agent_api_keys (requires PRAGMA foreign_keys=ON).
// Returns nil even if the agent doesn't exist.
func DeleteAgent(db *sql.DB, id string) error {
	// Ensure foreign keys are enforced for cascade delete
	_, _ = db.Exec("PRAGMA foreign_keys = ON")
	_, err := db.Exec("DELETE FROM agents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete agent %s: %w", id, err)
	}
	return nil
}

// PatchAgent updates only the user-editable fields (preferred_model, preferred_thinking_effort, transport).
// Returns nil even if the agent doesn't exist (no rows affected).
func PatchAgent(db *sql.DB, id, preferredModel, preferredThinkingEffort, transport string) error {
	if transport == "" {
		transport = "cli"
	}
	_, err := db.Exec(`
		UPDATE agents
		SET preferred_model = ?, preferred_thinking_effort = ?, transport = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		preferredModel, preferredThinkingEffort, transport, id)
	if err != nil {
		return fmt.Errorf("patch agent %s: %w", id, err)
	}
	return nil
}

// LoadAgentsIntoMemory loads agents from DB into the global model.Agents map and model.AgentList slice.
// Also builds the common prompt and prepends it to each agent's system prompt.
func LoadAgentsIntoMemory(db *sql.DB) error {
	agents, err := LoadAgentsFromDB(db)
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
		// Also allow refresh if agent has a provider with KnownModels
		if !agent.CanRefreshModels {
			if providerSpec := findProviderSpecForAgent(agent.ID); providerSpec != nil && len(providerSpec.KnownModels) > 0 {
				agent.CanRefreshModels = true
			}
		}
		// Populate Models from provider KnownModels if agent has no models
		// and the agent was created via setup wizard with a known provider
		if len(agent.Models) == 0 {
			populateModelsFromProvider(agent)
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

	// Prepend common prompt to each agent's system prompt
	for _, agent := range model.Agents {
		if commonPrompt != "" && agent.SystemPrompt != "" {
			agent.SystemPrompt = commonPrompt + "\n\n" + agent.SystemPrompt
		} else if commonPrompt != "" {
			agent.SystemPrompt = commonPrompt
		}
	}

	return nil
}

// populateModelsFromProvider fills an agent's Models from the ProviderRegistry's
// KnownModels if the agent has no models. It looks up the agent's provider from
// the agent_api_keys table. This is used after setup wizard creates an agent
// (which stores no models) and on subsequent server restarts.
func populateModelsFromProvider(agent *model.Agent) {
	spec := findProviderSpecForAgent(agent.ID)
	if spec == nil || len(spec.KnownModels) == 0 {
		return
	}
	agent.Models = model.KnownModelsToAgentModels(spec.KnownModels)
	agent.ModelsAutoDetected = true
}

// FindProviderSpecForAgent looks up the provider for an agent from the agent_api_keys table
// and returns the corresponding ProviderSpec. Exported so handler can use it for model refresh.
func FindProviderSpecForAgent(agentID string) *model.ProviderSpec {
	return findProviderSpecForAgent(agentID)
}

// findProviderSpecForAgent looks up the provider for an agent from the agent_api_keys table.
func findProviderSpecForAgent(agentID string) *model.ProviderSpec {
	if DB == nil {
		return nil
	}
	var providerID string
	if err := DB.QueryRow("SELECT provider FROM agent_api_keys WHERE agent_id = ?", agentID).Scan(&providerID); err != nil {
		return nil
	}
	return model.FindProviderSpec(providerID)
}
