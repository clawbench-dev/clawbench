package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/store"

	"clawbench/internal/dbutil"
	"clawbench/internal/model"
)

// ErrAgentNameTaken is returned when an agent name collides with a different
// agent's name. Agent names are globally unique (decision #50): the roster,
// group-member pickers and routing all key off the display name, so two
// indistinguishable entries are a real hazard.
var ErrAgentNameTaken = errors.New("agent name already taken")

// ErrAgentNameReserved is returned when an agent name collides with the
// configured group-chat user nickname (chat.user_nickname). The nickname is a
// routing target, so an agent sharing it would be unreachable (and would shadow
// the human in group routing). The collision is rejected in BOTH directions —
// this sentinel covers the agent side; the config side rejects a nickname that
// matches an existing agent.
var ErrAgentNameReserved = errors.New("agent name conflicts with the user nickname")

// ErrAcpCommandRequired is returned when a custom ACP agent is created without
// a spawn command. Without one the agent would advertise ACP transport but have
// nothing to spawn (Agent.SupportsACP() is exactly AcpCommand != "").
var ErrAcpCommandRequired = errors.New("acp command is required")

// ErrAcpCommandTooLong is returned when the spawn command exceeds
// MaxAcpCommandBytes.
var ErrAcpCommandTooLong = errors.New("acp command is too long")

// IsUserNickname reports whether name equals the configured group-chat user
// nickname. Empty names never collide (callers validate non-empty separately),
// and an unset nickname falls back to the default so the guard holds even before
// config is applied.
func IsUserNickname(name string) bool {
	return name != "" && name == groupUserTarget()
}

// AgentNameTaken reports whether any agent OTHER than excludeID already uses
// name. The exclusion is essential: SaveAgent is an ON CONFLICT(id) upsert that
// built-in backend registration calls repeatedly, so comparing against all rows
// would make an agent reject its own re-registration.
//
// Matching is exact and case-sensitive (the existing name convention: the UI
// validates length but never folds case). An empty name is never "taken" —
// callers validate non-empty separately.
func AgentNameTaken(name, excludeID string) bool {
	if name == "" {
		return false
	}
	var count int
	err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE name = ? AND id != ?",
		name, excludeID,
	).Scan(&count)
	if err != nil {
		// A lookup failure must not silently allow a duplicate; treat it as
		// "taken" so the write is refused rather than corrupting uniqueness.
		return true
	}
	return count > 0
}

// AgentDDL creates the agents table.
// Exported so handler tests and other external packages can create these tables
// in their test databases.
const AgentDDL = `
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	icon TEXT NOT NULL DEFAULT '',  -- deprecated: kept for SQLite <3.35 compat; not read or written
	specialty TEXT NOT NULL DEFAULT '',
	backend TEXT NOT NULL,
	command TEXT NOT NULL DEFAULT '',
	thinking_effort TEXT NOT NULL DEFAULT '',
	thinking_effort_levels TEXT NOT NULL DEFAULT '[]',
	preferred_mode TEXT NOT NULL DEFAULT '',
	preferred_model TEXT NOT NULL DEFAULT '',
	preferred_thinking_effort TEXT NOT NULL DEFAULT '',
	thinking_effort_by_model TEXT NOT NULL DEFAULT '{}',
	custom_system_prompt TEXT NOT NULL DEFAULT '',
	avatar TEXT NOT NULL DEFAULT '',
	models TEXT NOT NULL DEFAULT '[]',
	models_auto_detected INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	transport TEXT NOT NULL DEFAULT 'cli',
	acp_command TEXT NOT NULL DEFAULT '',
	acp_available_modes TEXT NOT NULL DEFAULT '[]',
	acp_available_thinking_efforts TEXT NOT NULL DEFAULT '[]',
	acp_available_commands TEXT NOT NULL DEFAULT '[]',
	acp_available_models TEXT NOT NULL DEFAULT '[]',
	acp_config_options TEXT NOT NULL DEFAULT '',
	acp_mcp_http INTEGER NOT NULL DEFAULT 0,
	acp_mcp_sse INTEGER NOT NULL DEFAULT 0,
	auto_approve INTEGER NOT NULL DEFAULT 0,
	disabled INTEGER NOT NULL DEFAULT 0,
	project_id INTEGER DEFAULT NULL,  -- NULL = global; value = project-only (see model.Agent.ProjectID)
	mcp_servers TEXT NOT NULL DEFAULT '{"mode":"all","names":[]}',  -- per-agent MCP policy (see model.MCPServerPolicy)
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_agents_backend ON agents(backend);
CREATE INDEX IF NOT EXISTS idx_agents_sort ON agents(sort_order);
`

// LoadAgentsFromDB loads all agents from the database and returns them sorted by ID.
func LoadAgentsFromDB() ([]*model.Agent, error) {
	rows, err := store.ReadDB().Query(`
		SELECT id, name, specialty, backend, command,
			thinking_effort, thinking_effort_levels,
			preferred_mode, preferred_model, preferred_thinking_effort,
			custom_system_prompt, avatar, models, models_auto_detected,
			sort_order,
			transport, acp_command, auto_approve, disabled, project_id,
			thinking_effort_by_model, mcp_servers
		FROM agents ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var agents []*model.Agent
	for rows.Next() {
		a := &model.Agent{}
		var modelsJSON, levelsJSON, effortByModelJSON, mcpServersJSON string
		var modelsAutoDetected, autoApprove, disabled int
		var projectID sql.NullInt64

		err := rows.Scan(
			&a.ID, &a.Name, &a.Specialty, &a.Backend, &a.Command,
			&a.ThinkingEffort, &levelsJSON,
			&a.PreferredMode, &a.PreferredModel, &a.PreferredThinkingEffort,
			&a.CustomSystemPrompt, &a.Avatar, &modelsJSON, &modelsAutoDetected,
			&a.SortOrder,
			&a.Transport, &a.AcpCommand, &autoApprove, &disabled, &projectID,
			&effortByModelJSON, &mcpServersJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}

		a.ModelsAutoDetected = modelsAutoDetected == 1
		a.AutoApprove = autoApprove == 1
		a.Disabled = disabled == 1
		if projectID.Valid {
			id := projectID.Int64
			a.ProjectID = &id
		}

		parseAgentJSONColumns(a, modelsJSON, levelsJSON, effortByModelJSON)
		// Unreadable policy keeps the zero value, which Normalized() reads as
		// "all" — the same default a fresh row gets.
		if mcpServersJSON != "" && mcpServersJSON != jsonNull {
			var policy model.MCPServerPolicy
			if err := json.Unmarshal([]byte(mcpServersJSON), &policy); err == nil {
				a.MCPServers = policy
			}
		}

		agents = append(agents, a)
	}

	return agents, rows.Err()
}

// parseAgentJSONColumns decodes the three JSON-encoded agent columns into the
// model. Unreadable values are left at their zero value rather than aborting
// the load: they only degrade a default, and refusing to start would be worse.
func parseAgentJSONColumns(a *model.Agent, modelsJSON, levelsJSON, effortByModelJSON string) {
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

	// Parse the per-model effort picks (issue #552).
	if effortByModelJSON != "" && effortByModelJSON != "{}" && effortByModelJSON != jsonNull {
		var byModel map[string]string
		if err := json.Unmarshal([]byte(effortByModelJSON), &byModel); err == nil && len(byModel) > 0 {
			a.ThinkingEffortByModel = byModel
		}
	}
}

// SaveAgent persists an agent. Only the user's own prompt text is durable: the
// composed prompt (shared + custom) is built at read time and never stored.
// Writing it here is what let a stale copy of the shared prompt outlive the
// template it came from and override later changes.
func SaveAgent(db dbutil.Writer, agent *model.Agent) error {
	modelsJSON, err := json.Marshal(agent.Models)
	if err != nil {
		return fmt.Errorf("marshal models: %w", err)
	}
	// json.Marshal(nil slice) produces "null" instead of "[]" — normalize to "[]"
	if string(modelsJSON) == jsonNull {
		modelsJSON = []byte("[]")
	}
	levelsJSON, err := json.Marshal(agent.ThinkingEffortLevels)
	if err != nil {
		return fmt.Errorf("marshal thinking_effort_levels: %w", err)
	}
	// Normalize nil to "{}" so the column never holds the string "null".
	effortByModelJSON, err := json.Marshal(agent.ThinkingEffortByModel)
	if err != nil {
		return fmt.Errorf("marshal thinking_effort_by_model: %w", err)
	}
	if string(effortByModelJSON) == jsonNull {
		effortByModelJSON = []byte("{}")
	}

	modelsAutoDetected := 0
	if agent.ModelsAutoDetected {
		modelsAutoDetected = 1
	}

	sortOrder := agent.SortOrder
	transport := agent.Transport
	if transport == "" {
		if agent.AcpCommand != "" {
			transport = transportACPStdio
		} else {
			transport = transportCLI
		}
	}

	autoApprove := 0
	if agent.AutoApprove {
		autoApprove = 1
	}

	disabled := 0
	if agent.Disabled {
		disabled = 1
	}

	// NULL scopes the agent globally; a value scopes it to that project. Passed
	// as any so a nil *int64 binds SQL NULL rather than 0 (0 is a real project
	// id — the reserved global sentinel — but the column distinguishes NULL).
	var projectID any
	if agent.ProjectID != nil {
		projectID = *agent.ProjectID
	}

	if IsUserNickname(agent.Name) {
		return fmt.Errorf("save agent %s: %w", agent.ID, ErrAgentNameReserved)
	}
	if AgentNameTaken(agent.Name, agent.ID) {
		return fmt.Errorf("save agent %s: %w", agent.ID, ErrAgentNameTaken)
	}

	_, err = db.Exec(`
		INSERT INTO agents (id, name, specialty, backend, command,
			thinking_effort, thinking_effort_levels,
			preferred_mode, preferred_model, preferred_thinking_effort,
			custom_system_prompt, avatar, models, models_auto_detected,
			sort_order,
			transport, acp_command, auto_approve, disabled, project_id,
			thinking_effort_by_model)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			specialty = excluded.specialty,
			backend = excluded.backend,
			command = excluded.command,
			thinking_effort = excluded.thinking_effort,
			thinking_effort_levels = excluded.thinking_effort_levels,
			preferred_mode = excluded.preferred_mode,
			preferred_model = excluded.preferred_model,
			preferred_thinking_effort = excluded.preferred_thinking_effort,
			custom_system_prompt = excluded.custom_system_prompt,
			avatar = excluded.avatar,
			models = excluded.models,
			models_auto_detected = excluded.models_auto_detected,
			sort_order = excluded.sort_order,
			transport = excluded.transport,
			acp_command = excluded.acp_command,
			auto_approve = excluded.auto_approve,
			disabled = excluded.disabled,
			project_id = excluded.project_id,
			thinking_effort_by_model = excluded.thinking_effort_by_model,
			updated_at = CURRENT_TIMESTAMP
	`, agent.ID, agent.Name, agent.Specialty, agent.Backend, agent.Command,
		agent.ThinkingEffort, string(levelsJSON),
		agent.PreferredMode, agent.PreferredModel, agent.PreferredThinkingEffort,
		agent.CustomSystemPrompt, agent.Avatar, string(modelsJSON), modelsAutoDetected,
		sortOrder,
		transport, agent.AcpCommand, autoApprove, disabled, projectID,
		string(effortByModelJSON))
	if err != nil {
		return fmt.Errorf("save agent %s: %w", agent.ID, err)
	}
	return nil
}

// DeleteAgent deletes an agent by ID (requires PRAGMA foreign_keys=ON).
// Returns nil even if the agent doesn't exist.
func DeleteAgent(id string) error {
	// Ensure foreign keys are enforced for cascade delete
	_, _ = store.WriteExec("PRAGMA foreign_keys = ON")
	_, err := store.WriteExec("DELETE FROM agents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete agent %s: %w", id, err)
	}
	return nil
}

// AgentUsage reports how many things still reference an agent, so deletion can
// be refused while any remain and the config panel can display the counts.
//
// Sessions is the number of conversations the agent takes part in:
//   - 1:1 chats (session_type='chat', archived included), plus
//   - group chats it is a member of (session_type='group_member', not archived;
//     a member the user disabled still counts — it is in the group, just not
//     scheduled).
//
// A group's timeline row (session_type='group') names only its host (or, in
// free mode, its first member); every OTHER member exists only as a hidden
// group_member row. So a group is counted through the membership row, not by
// matching the group row's agent_id — otherwise a plain member's groups would
// never appear in their session count. Task-execution sessions
// (session_type='scheduled') are excluded on purpose: deleting a task only
// ARCHIVES its execution sessions, so counting them would make the session
// count impossible to clear (a permanent deletion deadlock).
//
// Tasks counts scheduled_tasks rows (any status).
//
// Memberships counts group-member rows that are not archived. It is a SUBSET of
// Sessions (each membership IS one of the agent's group-chat sessions); it is
// reported separately only so the UI can explain that some sessions are groups.
// A member the user disabled still counts and still blocks deletion: disabling
// only takes it off the schedule, the agent's context is still bound to that
// group.
type AgentUsageCounts struct {
	Sessions    int
	Tasks       int
	Memberships int
}

// Total returns the count used by the deletion guard. Memberships is already
// included in Sessions, so it is not added again.
func (u AgentUsageCounts) Total() int {
	return u.Sessions + u.Tasks
}

// GetAgentUsage returns the session / task / membership counts for an agent.
func GetAgentUsage(agentID string) (AgentUsageCounts, error) {
	var u AgentUsageCounts

	// 1:1 chats (archived included — an archived chat still references the agent).
	err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE agent_id = ? AND session_type = ?",
		agentID, store.SessionTypeChat,
	).Scan(&u.Sessions)
	if err != nil {
		return u, fmt.Errorf("count chat sessions for agent %s: %w", agentID, err)
	}

	// Group chats the agent is a member of (members the user disabled count too —
	// disabling only takes them off the schedule). Counted here (not from the group
	// row, which names only the host) and folded into Sessions.
	err = store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM chat_sessions WHERE agent_id = ? AND session_type = ? AND archived = 0",
		agentID, store.SessionTypeGroupMember,
	).Scan(&u.Memberships)
	if err != nil {
		return u, fmt.Errorf("count group memberships for agent %s: %w", agentID, err)
	}
	u.Sessions += u.Memberships

	err = store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM scheduled_tasks WHERE agent_id = ?",
		agentID,
	).Scan(&u.Tasks)
	if err != nil {
		return u, fmt.Errorf("count tasks for agent %s: %w", agentID, err)
	}

	return u, nil
}

// GetAllAgentUsage returns the same counts as GetAgentUsage for EVERY agent in
// three grouped queries. The list endpoint (GET /api/agents) uses this instead
// of calling GetAgentUsage per agent: the latter is 3 queries each (N+1), and
// the read pool is tiny, so a 14-agent list would issue ~42 sequential scans.
// Agents with no rows are absent from the map (callers treat missing as zero).
func GetAllAgentUsage() (map[string]AgentUsageCounts, error) {
	out := make(map[string]AgentUsageCounts)

	scan := func(query string, apply func(u *AgentUsageCounts, count int), args ...any) error {
		rows, err := store.ReadDB().Query(query, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var agentID string
			var count int
			if err := rows.Scan(&agentID, &count); err != nil {
				return err
			}
			u := out[agentID]
			apply(&u, count)
			out[agentID] = u
		}
		return rows.Err()
	}

	// 1:1 chats (archived included).
	if err := scan(
		"SELECT agent_id, COUNT(*) FROM chat_sessions WHERE agent_id != '' AND session_type = ? GROUP BY agent_id",
		func(u *AgentUsageCounts, c int) { u.Sessions = c },
		store.SessionTypeChat,
	); err != nil {
		return nil, fmt.Errorf("count chat sessions by agent: %w", err)
	}
	// Active group memberships: each is one group chat the agent takes part in,
	// so it folds into Sessions (see GetAgentUsage). Also kept as Memberships
	// for the UI.
	if err := scan(
		"SELECT agent_id, COUNT(*) FROM chat_sessions WHERE agent_id != '' AND session_type = ? AND archived = 0 GROUP BY agent_id",
		func(u *AgentUsageCounts, c int) { u.Memberships = c; u.Sessions += c },
		store.SessionTypeGroupMember,
	); err != nil {
		return nil, fmt.Errorf("count group memberships by agent: %w", err)
	}
	if err := scan(
		"SELECT agent_id, COUNT(*) FROM scheduled_tasks WHERE agent_id != '' GROUP BY agent_id",
		func(u *AgentUsageCounts, c int) { u.Tasks = c },
	); err != nil {
		return nil, fmt.Errorf("count tasks by agent: %w", err)
	}

	return out, nil
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
	// ThinkingEffortForModel sets (non-nil level) or clears (nil level) the
	// user's effort pick for ONE model. Separate from PreferredThinkingEffort
	// so a per-model pick never overwrites the model-independent default.
	ThinkingEffortForModel *ThinkingEffortModelPatch
	Transport              *string
	// AcpCommand overrides the ACP spawn command. Only meaningful for
	// transport=acp-stdio; a non-empty value is what makes SupportsACP() true,
	// and the spawn path reads it straight from the agent (issue #557). The
	// handler forces the transport in the same patch, so this can never leave
	// an agent advertising ACP without a command.
	AcpCommand         *string
	Name               *string
	Specialty          *string
	CustomSystemPrompt *string
	Avatar             *string
	SortOrder          *int
	AutoApprove        *bool
	Disabled           *bool
	// MCPServers replaces the agent's MCP enablement policy. Stored as JSON;
	// only the policy (mode + names), never the server definitions.
	MCPServers *model.MCPServerPolicy
	// ProjectID scopes the agent to that project when non-nil.
	ProjectID *int64
	// ClearProjectID sets project_id to NULL (global). A *int64 alone cannot
	// distinguish "field not provided" (leave untouched) from "clear to NULL",
	// so clearing gets its own flag. Mutually exclusive with ProjectID.
	ClearProjectID bool
}

// ThinkingEffortModelPatch is a per-model effort write: ModelID is required;
// a nil Level REMOVES the entry (falls back to the global default), a non-nil
// empty Level does the same explicitly.
type ThinkingEffortModelPatch struct {
	ModelID string
	Level   *string
}

// PatchAgentFields updates only the non-nil fields in the AgentPatch struct.
// Returns nil even if the agent doesn't exist (no rows affected).
func PatchAgentFields(id string, patch AgentPatch) error {
	setClauses, args, err := buildAgentSetClauses(id, patch)
	if err != nil {
		return err
	}

	// The per-model effort map is a JSON object, so it cannot be expressed as a
	// simple column assignment: read-modify-write it first, in its own
	// transaction (the write mutex cannot be held across the UPDATE below).
	// Failing here aborts before any other field is written, which is the
	// stricter order: the caller sees an error and no partial edit.
	if patch.ThinkingEffortForModel != nil {
		if err := patchThinkingEffortForModel(id, patch.ThinkingEffortForModel); err != nil {
			return err
		}
	}

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	setClauses = append(setClauses, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := "UPDATE agents SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	if _, err := store.WriteExec(query, args...); err != nil {
		return fmt.Errorf("patch agent %s: %w", id, err)
	}
	return nil
}

// buildAgentSetClauses translates the non-nil patch fields into column
// assignments. Name uniqueness is checked here, before anything is written.
func buildAgentSetClauses(id string, patch AgentPatch) ([]string, []any, error) {
	var setClauses []string
	var args []any

	addSet := func(column string, value any) {
		setClauses = append(setClauses, column+" = ?")
		args = append(args, value)
	}

	for _, f := range plainStringAgentFields(patch) {
		if f.value != nil {
			addSet(f.column, *f.value)
		}
	}
	if patch.Transport != nil {
		addSet("transport", normalizeAgentTransport(*patch.Transport))
	}
	if patch.Name != nil {
		if err := ensureAgentNameFree(id, *patch.Name); err != nil {
			return nil, nil, err
		}
		addSet("name", *patch.Name)
	}
	if patch.SortOrder != nil {
		addSet("sort_order", *patch.SortOrder)
	}
	if patch.AutoApprove != nil {
		addSet("auto_approve", boolToInt(*patch.AutoApprove))
	}
	if patch.Disabled != nil {
		addSet("disabled", boolToInt(*patch.Disabled))
	}
	if patch.MCPServers != nil {
		encoded, err := encodeMCPPolicy(*patch.MCPServers)
		if err != nil {
			return nil, nil, err
		}
		addSet("mcp_servers", encoded)
	}
	setClauses, args = applyProjectScopeSet(patch, setClauses, args)
	return setClauses, args, nil
}

// agentStringField is one column that is written verbatim from the patch.
type agentStringField struct {
	column string
	value  *string
}

// plainStringAgentFields lists the patch fields that need no transformation
// beyond "nil means leave alone". Order is the historical write order; it only
// affects the generated SQL text, not the outcome.
func plainStringAgentFields(patch AgentPatch) []agentStringField {
	return []agentStringField{
		{"preferred_mode", patch.PreferredMode},
		{"preferred_model", patch.PreferredModel},
		{"preferred_thinking_effort", patch.PreferredThinkingEffort},
		{"acp_command", patch.AcpCommand},
		{"specialty", patch.Specialty},
		// Only the user's own text is stored; the composed prompt is built at
		// read time. Writing it here would freeze the shared prompt into the row.
		{"custom_system_prompt", patch.CustomSystemPrompt},
		// Raw SVG string (not a data URI); "" clears it back to the built-in icon.
		{"avatar", patch.Avatar},
	}
}

// encodeMCPPolicy renders a per-agent MCP policy for storage.
//
// Normalized so a row never stores an empty mode (which would read back as
// "none" through a future code path that forgets Normalized()), and Names is
// forced non-nil so the stored JSON always carries the field.
func encodeMCPPolicy(policy model.MCPServerPolicy) (string, error) {
	policy = policy.Normalized()
	if policy.Names == nil {
		policy.Names = []string{}
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("encode mcp policy: %w", err)
	}
	return string(encoded), nil
}

// patchThinkingEffortForModel applies one per-model effort write.
//
// Read-modify-write inside a single transaction under the store write mutex,
// so two concurrent clicks (or two tabs) cannot lose each other's entries.
// A missing agent row is NOT an error here: the caller's other patch fields
// report that case the same way (no rows affected), and the read below simply
// finds nothing.
func patchThinkingEffortForModel(id string, p *ThinkingEffortModelPatch) error {
	if p.ModelID == "" {
		return nil
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return fmt.Errorf("patch agent %s effort by model: %w", id, err)
	}
	// Source order matters: defers run LIFO, so Unlock is registered FIRST and
	// therefore runs LAST. That keeps the write mutex held while the transaction
	// is rolled back on an early return; releasing the mutex first would let a
	// concurrent writer grab another pooled connection and stall on SQLite's
	// write lock for the whole busy_timeout. Rolling back an already-committed
	// tx is a no-op, so every return path — including the missing-row one — is
	// covered.
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	byModel, err := readEffortByModel(tx, id)
	if err != nil {
		return err
	}
	if p.Level == nil || *p.Level == "" {
		delete(byModel, p.ModelID)
	} else {
		byModel[p.ModelID] = *p.Level
	}
	encoded, err := json.Marshal(byModel)
	if err != nil {
		return fmt.Errorf("patch agent %s effort by model: marshal: %w", id, err)
	}
	res, err := tx.ExecContext(
		context.Background(),
		"UPDATE agents SET thinking_effort_by_model = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		string(encoded), id,
	)
	if err != nil {
		return fmt.Errorf("patch agent %s effort by model: write: %w", id, err)
	}
	// The UPDATE above matches nothing when the row vanished between the read
	// and the write; committing would then report a successful save of nothing.
	if n, raErr := res.RowsAffected(); raErr == nil && n == 0 {
		return fmt.Errorf("patch agent %s effort by model: %w", id, sql.ErrNoRows)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("patch agent %s effort by model: commit: %w", id, err)
	}
	return nil
}

// readEffortByModel loads one agent's per-model effort map for a read-modify-
// write inside an open transaction.
//
// A missing agent row is returned as sql.ErrNoRows ON PURPOSE: the caller would
// otherwise commit a successful save of nothing, telling the user their pick was
// remembered when it was not (they would only find out after a restart).
func readEffortByModel(tx *sql.Tx, id string) (map[string]string, error) {
	var raw string
	if err := tx.QueryRowContext(context.Background(),
		"SELECT COALESCE(thinking_effort_by_model, '{}') FROM agents WHERE id = ?", id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("patch agent %s effort by model: %w", id, sql.ErrNoRows)
		}
		return nil, fmt.Errorf("patch agent %s effort by model: read: %w", id, err)
	}
	byModel := map[string]string{}
	if raw == "" || raw == jsonNull || raw == "{}" {
		return byModel, nil
	}
	if err := json.Unmarshal([]byte(raw), &byModel); err != nil {
		// A corrupt value must not permanently block the feature: the map only
		// ever holds user picks, so rebuilding it loses at most those picks,
		// while refusing to write would make the control dead.
		slog.Warn("agent effort by model: stored value unreadable, rebuilding", "agent", id, "err", err)
		byModel = map[string]string{}
	}
	return byModel, nil
}

// nextAgentSortOrder returns one past the highest current sort_order, so a
// newly created agent lands at the end of the list rather than at 0 (where it
// would tie with every never-reordered agent and be ordered by its random ID).
func nextAgentSortOrder() int {
	var maxOrder sql.NullInt64
	// A read failure is not worth failing the create over: 0 keeps the old
	// behavior (the list is then ordered by (sort_order, id)).
	_ = store.ReadDB().QueryRow("SELECT MAX(sort_order) FROM agents").Scan(&maxOrder)
	if !maxOrder.Valid {
		return 0
	}
	return int(maxOrder.Int64) + 1
}

// ReorderAgents persists a manual drag order: the i-th id in ids becomes that
// agent's sort_order (0 = top). One transaction, so a crash cannot leave the
// list half-renumbered. Ids that do not match any row are ignored (UPDATE with
// no match is a no-op); rows absent from ids keep their current sort_order.
//
// The settings page posts the complete visible order, so every agent it shows
// is renumbered. Rows it does not show (e.g. scoped to another project) keep
// their number and are placed by the (sort_order, id) load order.
func ReorderAgents(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	ctx := context.Background()
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE agents SET sort_order = ? WHERE id = ?", i, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// applyProjectScopeSet appends the project_id assignment for a patch: an
// explicit NULL when clearing, the new id when scoping, and nothing when the
// field was not provided. Split out of PatchAgentFields to keep its branch
// count in budget.
func applyProjectScopeSet(patch AgentPatch, setClauses []string, args []any) ([]string, []any) {
	if patch.ClearProjectID {
		// Explicit NULL: clearing the scope must not collide with the
		// "not provided" case, which leaves the column untouched.
		return append(setClauses, "project_id = NULL"), args
	}
	if patch.ProjectID != nil {
		return append(setClauses, "project_id = ?"), append(args, *patch.ProjectID)
	}
	return setClauses, args
}

// ensureAgentNameFree returns ErrAgentNameTaken (wrapped with the agent id)
// when name is already used by a different agent. Split out of
// PatchAgentFields to keep its branch count in budget.
func ensureAgentNameFree(id, name string) error {
	if IsUserNickname(name) {
		return fmt.Errorf("patch agent %s: %w", id, ErrAgentNameReserved)
	}
	if AgentNameTaken(name, id) {
		return fmt.Errorf("patch agent %s: %w", id, ErrAgentNameTaken)
	}
	return nil
}

// boolToInt maps a bool to the 0/1 integer SQLite stores for a flag column.
// Split out of PatchAgentFields to keep its branch count in budget.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// normalizeAgentTransport maps an empty transport to the CLI default. Split out
// of PatchAgentFields to keep its branch count in budget.
func normalizeAgentTransport(transport string) string {
	if transport == "" {
		return transportCLI
	}
	return transport
}

// LoadAgentsIntoMemory loads agents from the database into the global
// model.Agents map and model.AgentList, populating runtime-only fields and
// composing each agent's system prompt.
//
// This delegates to model.LoadAgentsIntoMemoryFromDB so there is exactly one
// implementation of the load-and-compose step. Previously this function and
// model.MergeDiscoveredDataDB both did it, with subtly different SQL and prompt
// handling, and which one took effect depended on call order.
func LoadAgentsIntoMemory() error {
	return model.LoadAgentsIntoMemoryFromDB(store.ReadDB())
}

// newAgentCopyID mints an ID for a duplicated agent: "<backend>-<8-hex>".
//
// The base is the source's *backend* (a controlled BackendSpec.ID constant
// such as "codebuddy"), not its ID. Backends never contain a copy suffix and
// are not user-editable (AgentPatch has no Backend field), so copying a copy
// cannot compound the name the way "<sourceID>-copy-<ts>" did — that scheme
// grew the ID by a suffix per generation until it exceeded the 128-char limit
// the API enforces on agent IDs.
//
// The suffix is random rather than a timestamp because SaveAgent upserts with
// ON CONFLICT(id) DO UPDATE: two copies minted in the same millisecond would
// share an ID and the second would silently overwrite the first.
func newAgentCopyID(backend string) string {
	b := make([]byte, 4)
	// crypto/rand.Read always fills b or returns an error; a failure here is
	// unrecoverable, so panic rather than mint a predictable ID.
	if _, err := rand.Read(b); err != nil {
		panic("agent copy: crypto/rand.Read failed: " + err.Error())
	}
	return backend + "-" + hex.EncodeToString(b)
}

// DuplicateAgent creates a new agent by cloning an existing one.
// It generates a unique ID ("<backend>-<8-hex>"), copies all configuration
// fields from the source, and saves to DB.
func DuplicateAgent(sourceID, newName string) (*model.Agent, error) {
	source := model.GetAgent(sourceID)
	if source == nil {
		return nil, fmt.Errorf("source agent %s not found", sourceID)
	}

	newID := newAgentCopyID(source.Backend)

	clone := &model.Agent{
		ID:                      newID,
		Name:                    newName,
		Specialty:               source.Specialty,
		Backend:                 source.Backend,
		Command:                 source.Command,
		ThinkingEffort:          source.ThinkingEffort,
		ThinkingEffortLevels:    make([]string, len(source.ThinkingEffortLevels)),
		PreferredMode:           source.PreferredMode,
		PreferredModel:          source.PreferredModel,
		PreferredThinkingEffort: source.PreferredThinkingEffort,
		CustomSystemPrompt:      source.CustomSystemPrompt,
		Avatar:                  source.Avatar,
		Transport:               source.Transport,
		AcpCommand:              source.AcpCommand,
		SortOrder:               source.SortOrder,
		AutoApprove:             source.AutoApprove,
		// ProjectID IS copied: duplicating a project-only agent should produce
		// another project-only agent. Inheriting the scope matches the user's
		// mental model (the copy belongs where the original does); the caller
		// can always clear it afterwards.
		ProjectID: source.ProjectID,
		// Disabled is deliberately NOT copied: a duplicate is a new, usable
		// agent — inheriting "disabled" would hand the user a copy that is
		// invisible in every picker with no hint why.
	}
	copy(clone.ThinkingEffortLevels, source.ThinkingEffortLevels)
	// Per-model effort picks ARE copied: they describe this backend+model pair
	// (the models are the same for the clone), so dropping them would make the
	// copy start from the agent's own per-model defaults — the exact behavior
	// issue #552 removes. Unlike Disabled, this is not a usability flag.
	if len(source.ThinkingEffortByModel) > 0 {
		clone.ThinkingEffortByModel = make(map[string]string, len(source.ThinkingEffortByModel))
		for m, lvl := range source.ThinkingEffortByModel {
			clone.ThinkingEffortByModel[m] = lvl
		}
	}
	if len(source.Models) > 0 {
		clone.Models = make([]model.AgentModel, len(source.Models))
		copy(clone.Models, source.Models)
	}

	// The composed prompt is not stored; SaveAgent persists the custom text and
	// the shared prompt is composed at read time.
	if err := SaveAgent(store.WriteDB(), clone); err != nil {
		return nil, fmt.Errorf("save duplicated agent: %w", err)
	}

	// Compose the runtime prompt now. The clone goes straight into the live
	// Agents map, and nothing recomposes it until the next reload, so leaving
	// it empty would make the new agent run with no system prompt at all.
	clone.RuntimeSystemPrompt = model.ComposeSystemPrompt(clone.CustomSystemPrompt)

	return clone, nil
}

// MaxAcpCommandBytes bounds the stored ACP spawn command. The value reaches
// exec after being split on whitespace, so this only guards the DB row and the
// settings input — it is not a security boundary (any command typed here is
// equivalent to the PTY the terminal API already grants the same user).
const MaxAcpCommandBytes = 4096

// CreateCustomACPAgent creates an agent for an ACP server that has no backend
// registry entry (issue #557): the user supplies a name and a spawn command and
// the agent rides the existing ACP transport unchanged.
//
// The backend is always model.CustomACPBackend rather than a user-supplied
// slug, and that is load-bearing: model.RefreshAgents rewrites acp_command for
// every row whose backend matches a registered spec (`WHERE backend = ?`), so a
// custom agent that reused e.g. "claude" would have its command silently
// replaced by the bundled adapter on the next rescan or startup. A backend ID
// that appears in no registry entry is never touched by that sync.
//
// Everything spec-derived then degrades exactly as it already does for the
// built-in backends that ship without ACP mapping data: no CLI transport, no
// model discovery, and LoadSession/ListSessions reported unsupported until the
// agent advertises them itself.
func CreateCustomACPAgent(name, acpCommand string) (*model.Agent, error) {
	trimmed := strings.TrimSpace(acpCommand)
	if trimmed == "" {
		return nil, ErrAcpCommandRequired
	}
	if len(trimmed) > MaxAcpCommandBytes {
		return nil, ErrAcpCommandTooLong
	}

	agent := &model.Agent{
		ID:         newAgentCopyID(model.CustomACPBackend),
		Name:       name,
		Backend:    model.CustomACPBackend,
		Transport:  transportACPStdio,
		AcpCommand: trimmed,
		// Land at the end of the user's list instead of tying at 0 with every
		// never-reordered agent (the tiebreak is the random ID, which would
		// scatter it into the middle of the list).
		SortOrder: nextAgentSortOrder(),
	}
	// A fresh agent has no custom prompt, so its runtime copy is the shared
	// prefix only. Composing it here keeps the agent usable immediately: it goes
	// straight into the live map and nothing recomposes it until the next load.
	agent.RuntimeSystemPrompt = model.ComposeSystemPrompt("")

	if err := SaveAgent(store.WriteDB(), agent); err != nil {
		return nil, fmt.Errorf("save custom acp agent: %w", err)
	}
	return agent, nil
}
