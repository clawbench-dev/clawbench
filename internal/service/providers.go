package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"clawbench/internal/dbutil"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

// ProvidersDDL creates the model-provider table.
//
// A provider is a named endpoint + credential that agents reference by id. The
// definition lives here (one row, one key); an agent only stores the reference,
// so rotating a key is one edit instead of one edit per agent — the same split
// MCP servers use.
const ProvidersDDL = `
CREATE TABLE IF NOT EXISTS providers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL COLLATE NOCASE,
	protocol TEXT NOT NULL DEFAULT 'anthropic',
	base_url TEXT NOT NULL DEFAULT '',
	api_key TEXT NOT NULL DEFAULT '',
	auth_mode TEXT NOT NULL DEFAULT 'auto',
	preset_id TEXT NOT NULL DEFAULT '',
	models TEXT NOT NULL DEFAULT '[]',
	default_model TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_providers_name ON providers(name);
`

var (
	// ErrProviderNotFound is returned when the id does not exist.
	ErrProviderNotFound = errors.New("provider not found")
	// ErrProviderNameTaken is returned when the name collides with another row.
	ErrProviderNameTaken = errors.New("provider name already taken")
	// ErrProviderInUse is returned when agents still reference the provider.
	ErrProviderInUse = errors.New("provider is referenced by agents")
	// ErrProviderUsedBySummary is returned when the shared AI-summary model
	// references the provider and the operation would break it (delete, or a
	// protocol the summarizer cannot speak).
	ErrProviderUsedBySummary = errors.New("provider is used by the AI summary model")
	// ErrProviderInvalid is returned for malformed field values.
	ErrProviderInvalid = errors.New("invalid provider")
)

// ProviderPatch holds optional provider updates. Nil means "leave alone";
// ClearAPIKey is the only way to remove a stored secret.
type ProviderPatch struct {
	Name         *string
	Protocol     *string
	BaseURL      *string
	APIKey       *string
	ClearAPIKey  bool
	AuthMode     *string
	PresetID     *string
	Models       *[]string
	DefaultModel *string
}

// LoadProvidersIntoMemory loads every provider row into the in-memory snapshot
// the spawn paths read. Called once at startup and after every mutation.
func LoadProvidersIntoMemory() error {
	providers, err := listProvidersFromDB(store.ReadDB())
	if err != nil {
		return err
	}
	model.ReplaceProviders(providers)
	return nil
}

func listProvidersFromDB(db dbutil.Reader) ([]*model.Provider, error) {
	rows, err := db.Query(`SELECT id, name, protocol, base_url, api_key, auth_mode, preset_id,
		models, default_model, created_at, updated_at FROM providers`)
	if err != nil {
		return nil, fmt.Errorf("query providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*model.Provider
	for rows.Next() {
		p := &model.Provider{}
		var createdAt, updatedAt sql.NullString
		var modelsJSON string
		if err := rows.Scan(&p.ID, &p.Name, &p.Protocol, &p.BaseURL, &p.APIKey, &p.AuthMode, &p.PresetID,
			&modelsJSON, &p.DefaultModel, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		p.Models = decodeModelIDs(modelsJSON)
		p.CreatedAt = createdAt.String
		p.UpdatedAt = updatedAt.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListProviders returns the current rows in display order.
func ListProviders() ([]*model.Provider, error) {
	return listProvidersFromDB(store.ReadDB())
}

// GetProvider reads one provider from the database.
func GetProvider(id int64) (*model.Provider, error) {
	p := &model.Provider{}
	var createdAt, updatedAt sql.NullString
	var modelsJSON string
	err := store.ReadDB().QueryRow(`SELECT id, name, protocol, base_url, api_key, auth_mode, preset_id,
		models, default_model, created_at, updated_at FROM providers WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Protocol, &p.BaseURL, &p.APIKey, &p.AuthMode, &p.PresetID,
			&modelsJSON, &p.DefaultModel, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProviderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get provider: %w", err)
	}
	p.Models = decodeModelIDs(modelsJSON)
	p.CreatedAt = createdAt.String
	p.UpdatedAt = updatedAt.String
	return p, nil
}

// CreateProvider validates and inserts a new provider, then refreshes the
// in-memory snapshot.
func CreateProvider(p model.Provider) (*model.Provider, error) {
	p = p.Normalized()
	if err := validateProviderFields(p); err != nil {
		return nil, err
	}
	if providerNameTaken(p.Name, 0) {
		return nil, ErrProviderNameTaken
	}

	res, err := store.WriteExec(`INSERT INTO providers (name, protocol, base_url, api_key, auth_mode, preset_id, models, default_model)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, p.Name, p.Protocol, p.BaseURL, p.APIKey, p.AuthMode, p.PresetID,
		encodeModelIDs(p.Models), p.DefaultModel)
	if err != nil {
		if isUniqueConstraint(err) {
			return nil, ErrProviderNameTaken
		}
		return nil, fmt.Errorf("insert provider: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("provider id: %w", err)
	}
	created, err := GetProvider(id)
	if err != nil {
		return nil, err
	}
	if err := LoadProvidersIntoMemory(); err != nil {
		slog.Warn("provider cache reload failed", "error", err)
	}
	return created, nil
}

// UpdateProvider applies a partial update. A protocol change is rejected when
// any bound agent's backend cannot speak the new protocol — otherwise those
// agents would start failing every turn with no way to tell why.
func UpdateProvider(id int64, patch ProviderPatch) (*model.Provider, error) {
	current, err := GetProvider(id)
	if err != nil {
		return nil, err
	}
	next := applyProviderPatch(*current, patch)
	if verr := validateProviderFields(next); verr != nil {
		return nil, verr
	}
	if providerNameTaken(next.Name, id) {
		return nil, ErrProviderNameTaken
	}
	if next.Protocol != current.Protocol {
		if summaryUsesProvider(id) && next.Protocol != model.ProviderProtocolAnthropic && next.Protocol != model.ProviderProtocolOpenAI {
			return nil, ErrProviderUsedBySummary
		}
		incompatible, cerr := agentsIncompatibleWithProvider(id, next.Protocol)
		if cerr != nil {
			return nil, cerr
		}
		if len(incompatible) > 0 {
			return nil, &ProviderProtocolConflictError{AgentNames: incompatible}
		}
	}

	if _, werr := store.WriteExec(`UPDATE providers SET name = ?, protocol = ?, base_url = ?,
		api_key = ?, auth_mode = ?, preset_id = ?, models = ?, default_model = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		next.Name, next.Protocol, next.BaseURL, next.APIKey, next.AuthMode, next.PresetID,
		encodeModelIDs(next.Models), next.DefaultModel, id); werr != nil {
		if isUniqueConstraint(werr) {
			return nil, ErrProviderNameTaken
		}
		return nil, fmt.Errorf("update provider: %w", werr)
	}
	updated, err := GetProvider(id)
	if err != nil {
		return nil, err
	}
	if rerr := LoadProvidersIntoMemory(); rerr != nil {
		slog.Warn("provider cache reload failed", "error", rerr)
	}
	// Config-file backends keep a copy of the endpoint on disk; re-apply so an
	// edited base URL or key reaches them without waiting for a restart.
	if rerr := ReconcileProviderConfigs(); rerr != nil {
		slog.Warn("reconcile provider configs after update failed", "error", rerr)
	}
	return updated, nil
}

// applyProviderPatch overlays the non-nil fields and normalizes the result.
// Split out of UpdateProvider to keep its branch count in budget.
func applyProviderPatch(current model.Provider, patch ProviderPatch) model.Provider {
	next := current
	if patch.Name != nil {
		next.Name = *patch.Name
	}
	if patch.Protocol != nil {
		next.Protocol = *patch.Protocol
	}
	if patch.BaseURL != nil {
		next.BaseURL = *patch.BaseURL
	}
	if patch.AuthMode != nil {
		next.AuthMode = *patch.AuthMode
	}
	if patch.PresetID != nil {
		next.PresetID = *patch.PresetID
	}
	if patch.Models != nil {
		next.Models = *patch.Models
	}
	if patch.DefaultModel != nil {
		next.DefaultModel = *patch.DefaultModel
	}
	if patch.ClearAPIKey {
		next.APIKey = ""
	} else if patch.APIKey != nil && *patch.APIKey != "" {
		next.APIKey = *patch.APIKey
	}
	return next.Normalized()
}

// DeleteProvider removes a provider. It refuses while agents still reference it
// so no agent is left pointing at a missing row.
func DeleteProvider(id int64) error {
	count, err := CountAgentsByProvider(id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrProviderInUse
	}
	if summaryUsesProvider(id) {
		return ErrProviderUsedBySummary
	}
	res, err := store.WriteExec("DELETE FROM providers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return ErrProviderNotFound
	}
	if err := LoadProvidersIntoMemory(); err != nil {
		slog.Warn("provider cache reload failed", "error", err)
	}
	return nil
}

// CountAgentsByProvider counts agents referencing the provider.
func CountAgentsByProvider(id int64) (int, error) {
	var count int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM agents WHERE provider_id = ?", id,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count agents by provider: %w", err)
	}
	return count, nil
}

// ProviderUsageCounts returns how many agents reference each provider, keyed by
// provider id. One query for the whole list (the settings page renders a badge
// per row).
func ProviderUsageCounts() (map[int64]int, error) {
	rows, err := store.ReadDB().Query(
		"SELECT provider_id, COUNT(*) FROM agents WHERE provider_id IS NOT NULL GROUP BY provider_id",
	)
	if err != nil {
		return nil, fmt.Errorf("provider usage counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int64]int)
	for rows.Next() {
		var id int64
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		out[id] = count
	}
	return out, rows.Err()
}

// ProviderAgentNames lists the agents referencing the provider, for error
// details and the settings UI.
func ProviderAgentNames(id int64) ([]string, error) {
	rows, err := store.ReadDB().Query("SELECT name FROM agents WHERE provider_id = ? ORDER BY name", id)
	if err != nil {
		return nil, fmt.Errorf("list provider agents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// ProviderAgentNamesAll is the list-page counterpart: one query for every
// provider, keyed by provider id. The settings list shows who uses each
// provider, so a per-row query would be N round trips for one screen.
func ProviderAgentNamesAll() (map[int64][]string, error) {
	rows, err := store.ReadDB().Query(
		"SELECT provider_id, name FROM agents WHERE provider_id IS NOT NULL ORDER BY name",
	)
	if err != nil {
		return nil, fmt.Errorf("list provider agents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int64][]string)
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = append(out[id], name)
	}
	return out, rows.Err()
}

// ProviderProtocolConflictError reports agents that cannot use the new
// protocol. It is a typed error so the handler can put the names in the
// response detail.
type ProviderProtocolConflictError struct {
	AgentNames []string
}

func (e *ProviderProtocolConflictError) Error() string {
	return fmt.Sprintf("provider protocol is not supported by agents: %s", strings.Join(e.AgentNames, ", "))
}

func agentsIncompatibleWithProvider(providerID int64, protocol string) ([]string, error) {
	rows, err := store.ReadDB().Query(
		"SELECT name, backend FROM agents WHERE provider_id = ? ORDER BY name", providerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list provider agents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var name, backend string
		if err := rows.Scan(&name, &backend); err != nil {
			return nil, err
		}
		if !model.ProviderEnvSupported(backend, protocol) {
			names = append(names, name)
		}
	}
	return names, rows.Err()
}

func validateProviderFields(p model.Provider) error {
	if p.Name == "" {
		return fmt.Errorf("%w: name is required", ErrProviderInvalid)
	}
	if !model.ValidProtocol(p.Protocol) {
		return fmt.Errorf("%w: unknown protocol %q", ErrProviderInvalid, p.Protocol)
	}
	if !model.ValidAuthMode(p.AuthMode) {
		return fmt.Errorf("%w: unknown auth mode %q", ErrProviderInvalid, p.AuthMode)
	}
	return nil
}

func providerNameTaken(name string, excludeID int64) bool {
	var count int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM providers WHERE name = ? AND id != ?", name, excludeID,
	).Scan(&count); err != nil {
		// A lookup failure must not let a duplicate through.
		return true
	}
	return count > 0
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}

// summaryUsesProvider reports whether the shared AI-summary model is bound to
// the provider. The binding lives in config.yaml, so the check reads the
// in-memory config rather than a table.
func summaryUsesProvider(id int64) bool {
	pid := model.ConfigInstance.AISummary.ProviderID
	return pid != nil && *pid == id
}

// encodeModelIDs stores the curated model list as JSON. The column is TEXT so
// an empty list is "[]" rather than NULL, which keeps the read path branch-free.
func encodeModelIDs(models []string) string {
	data, err := json.Marshal(models)
	if err != nil || len(models) == 0 {
		return "[]"
	}
	return string(data)
}

func decodeModelIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}
