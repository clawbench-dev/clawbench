package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	auto_approve INTEGER NOT NULL DEFAULT 0,
	disabled INTEGER NOT NULL DEFAULT 0,
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
			transport, acp_command, auto_approve, disabled
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
		var modelsAutoDetected, autoApprove, disabled int

		err := rows.Scan(
			&a.ID, &a.Name, &a.Specialty, &a.Backend, &a.Command,
			&a.ThinkingEffort, &levelsJSON,
			&a.PreferredMode, &a.PreferredModel, &a.PreferredThinkingEffort,
			&a.CustomSystemPrompt, &a.Avatar, &modelsJSON, &modelsAutoDetected,
			&a.SortOrder,
			&a.Transport, &a.AcpCommand, &autoApprove, &disabled,
		)
		if err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}

		a.ModelsAutoDetected = modelsAutoDetected == 1
		a.AutoApprove = autoApprove == 1
		a.Disabled = disabled == 1

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
			transport, acp_command, auto_approve, disabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			updated_at = CURRENT_TIMESTAMP
	`, agent.ID, agent.Name, agent.Specialty, agent.Backend, agent.Command,
		agent.ThinkingEffort, string(levelsJSON),
		agent.PreferredMode, agent.PreferredModel, agent.PreferredThinkingEffort,
		agent.CustomSystemPrompt, agent.Avatar, string(modelsJSON), modelsAutoDetected,
		sortOrder,
		transport, agent.AcpCommand, autoApprove, disabled)
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
//   - group chats it is an ACTIVE member of (session_type='group_member',
//     archived=0).
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
// Memberships counts ACTIVE group-member rows. It is a SUBSET of Sessions (each
// active group membership IS one of the agent's group-chat sessions); it is
// reported separately only so the UI can explain that some sessions are groups.
// A left member is archived and no longer counts or blocks.
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

	// Group chats the agent is an active member of. Counted here (not from the
	// group row, which names only the host) and folded into Sessions.
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
	Transport               *string
	Name                    *string
	Specialty               *string
	CustomSystemPrompt      *string
	Avatar                  *string
	SortOrder               *int
	AutoApprove             *bool
	Disabled                *bool
}

// PatchAgentFields updates only the non-nil fields in the AgentPatch struct.
// Returns nil even if the agent doesn't exist (no rows affected).
func PatchAgentFields(id string, patch AgentPatch) error {
	// Build dynamic SET clause
	var setClauses []string
	var args []any

	addSet := func(column string, value any) {
		setClauses = append(setClauses, column+" = ?")
		args = append(args, value)
	}

	if patch.PreferredMode != nil {
		addSet("preferred_mode", *patch.PreferredMode)
	}
	if patch.PreferredModel != nil {
		addSet("preferred_model", *patch.PreferredModel)
	}
	if patch.PreferredThinkingEffort != nil {
		addSet("preferred_thinking_effort", *patch.PreferredThinkingEffort)
	}
	if patch.Transport != nil {
		addSet("transport", normalizeAgentTransport(*patch.Transport))
	}
	if patch.Name != nil {
		if err := ensureAgentNameFree(id, *patch.Name); err != nil {
			return err
		}
		addSet("name", *patch.Name)
	}
	if patch.Specialty != nil {
		addSet("specialty", *patch.Specialty)
	}
	if patch.CustomSystemPrompt != nil {
		// Only the user's own text is stored; the composed prompt is built at
		// read time. Writing it here would freeze the shared prompt into the row.
		addSet("custom_system_prompt", *patch.CustomSystemPrompt)
	}
	if patch.Avatar != nil {
		// Raw SVG string (not a data URI); "" clears it back to the built-in icon.
		addSet("avatar", *patch.Avatar)
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

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	setClauses = append(setClauses, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := "UPDATE agents SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	_, err := store.WriteExec(query, args...)
	if err != nil {
		return fmt.Errorf("patch agent %s: %w", id, err)
	}
	return nil
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
	}
	copy(clone.ThinkingEffortLevels, source.ThinkingEffortLevels)
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
