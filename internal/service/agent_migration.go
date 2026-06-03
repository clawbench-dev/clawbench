//nolint:noctx,govet // DB parameter, context not applicable; shadowed err is acceptable in sequential blocks
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"clawbench/internal/model"

	"gopkg.in/yaml.v3"
)

// MigrateAgentsFromYAML performs a one-time migration of agent YAML files
// from the given directory into the agents database table.
// If the agents table already has records, migration is skipped (idempotent).
// The migration uses a transaction for atomicity — all agents migrate or none do.
func MigrateAgentsFromYAML(db *sql.DB, agentsDir string) error {
	// Check if agents table already has records
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM agents").Scan(&count); err != nil {
		return fmt.Errorf("check agents count: %w", err)
	}
	if count > 0 {
		return nil // already migrated
	}

	// Read YAML directory
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		// Directory doesn't exist — nothing to migrate
		return nil
	}

	// Collect agents from YAML files
	var agents []*model.Agent
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(agentsDir, entry.Name()))
		if err != nil {
			slog.Warn("YAML migration: failed to read file", "file", entry.Name(), "error", err)
			continue
		}

		var agent model.Agent
		if err := yaml.Unmarshal(data, &agent); err != nil {
			slog.Warn("YAML migration: invalid YAML", "file", entry.Name(), "error", err)
			continue
		}
		if agent.ID == "" {
			slog.Warn("YAML migration: agent missing ID", "file", entry.Name())
			continue
		}

		agent.Source = "auto" // all migrated agents are "auto"
		agents = append(agents, &agent)
	}

	if len(agents) == 0 {
		return nil // nothing to migrate
	}

	// Transactional write: all-or-nothing
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("YAML migration: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, agent := range agents {
		if err := saveAgentTx(tx, agent); err != nil {
			return fmt.Errorf("YAML migration: save agent %s: %w", agent.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("YAML migration: commit: %w", err)
	}

	slog.Info("YAML migration completed", "agents", len(agents))
	return nil
}

// saveAgentTx saves an agent within an existing transaction.
func saveAgentTx(tx *sql.Tx, agent *model.Agent) error {
	modelsJSON, err := json.Marshal(agent.Models)
	if err != nil {
		return fmt.Errorf("marshal models: %w", err)
	}
	levelsJSON, err := json.Marshal(agent.ThinkingEffortLevels)
	if err != nil {
		return fmt.Errorf("marshal thinking_effort_levels: %w", err)
	}

	modelsAutoDetected := 0
	if agent.ModelsAutoDetected {
		modelsAutoDetected = 1
	}

	transport := agent.Transport
	if transport == "" {
		transport = "cli"
	}

	_, err = tx.Exec(`
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
		agent.Source, agent.SortOrder,
		transport, agent.AcpCommand)
	if err != nil {
		return fmt.Errorf("save agent %s: %w", agent.ID, err)
	}
	return nil
}
