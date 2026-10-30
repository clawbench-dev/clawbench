package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// providerConfigMu serializes config-file edits. Agent PATCH holds the handler's
// configMutex, but provider updates (PUT /api/providers) do not, so the file
// writer needs its own lock.
var providerConfigMu sync.Mutex

const (
	// backendCodex is the one backend whose provider lives in a config file.
	backendCodex = "codex"

	codexBlockStart = "# >>> clawbench provider (managed by ClawBench) >>>"
	codexBlockEnd   = "# <<< clawbench provider (managed by ClawBench) <<<"
)

// codexState remembers what a config file looked like before ClawBench touched
// it, so unbinding can restore the user's own settings instead of leaving the
// managed provider behind.
type codexState struct {
	Backup                string `json:"backup"`
	OriginalModelProvider string `json:"originalModelProvider"`
	HadModelProvider      bool   `json:"hadModelProvider"`
}

// ProviderConfigPaths lists the files a config-file backend needs ClawBench to
// modify in order to apply a provider. An empty list means the backend is
// environment-driven (or unsupported) and needs no file consent.
func ProviderConfigPaths(backend string) []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	if backend == backendCodex {
		return []string{filepath.Join(home, ".codex", "config.toml")}
	}
	return nil
}

// ProviderBindingConflict lists the agents of the same backend that are bound
// to a DIFFERENT provider.
//
// Config-file backends keep their provider in a single global file, so two
// agents with different providers cannot both be honored — the last write wins
// silently. Rejecting the second binding is the only honest option; the
// alternative would be a session quietly talking to the wrong endpoint.
func ProviderBindingConflict(backend, excludeAgentID string, providerID int64) ([]string, error) {
	rows, err := store.ReadDB().Query(
		`SELECT name FROM agents
		 WHERE backend = ? AND id != ? AND provider_id IS NOT NULL AND provider_id != ?
		 ORDER BY name`, backend, excludeAgentID, providerID,
	)
	if err != nil {
		return nil, fmt.Errorf("provider binding conflict: %w", err)
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

// ApplyProviderConfig writes the provider into the backend's config file when
// that backend needs one. Environment-driven backends are a no-op.
//
// The agent is read live: a caller may hold an older *Agent snapshot.
func ApplyProviderConfig(agentID string) error {
	agent := model.GetAgent(agentID)
	if agent == nil {
		return nil
	}
	if !model.ProviderUsesConfigFile(agent.Backend) {
		return nil
	}
	p := model.AgentProvider(agentID)
	if p == nil {
		return RestoreProviderConfig(agent.Backend)
	}
	return ApplyProviderConfigToBackend(agent.Backend, p)
}

// ApplyProviderConfigToBackend writes the provider into a backend's config file
// without consulting any agent binding. Callers that are about to persist a
// binding use this first, so a failed write cannot leave a binding that points
// at a provider the CLI never learned about.
func ApplyProviderConfigToBackend(backend string, p *model.Provider) error {
	if !model.ProviderUsesConfigFile(backend) || p == nil {
		return nil
	}
	providerConfigMu.Lock()
	defer providerConfigMu.Unlock()

	if backend == backendCodex {
		return applyCodexProvider(p)
	}
	return nil
}

// ReconcileProviderConfigs re-applies (or restores) every config-file backend
// at startup, so a server restart cannot leave a file describing a provider
// that was edited or unbound while the process was down.
func ReconcileProviderConfigs() error {
	agents := model.GetAgentList()
	bound := make(map[string]bool)
	for _, a := range agents {
		if !model.ProviderUsesConfigFile(a.Backend) || a.ProviderID == nil {
			continue
		}
		bound[a.Backend] = true
		if err := ApplyProviderConfig(a.ID); err != nil {
			return err
		}
	}
	if !bound[backendCodex] {
		if err := RestoreProviderConfig(backendCodex); err != nil {
			return err
		}
	}
	return nil
}

// RestoreProviderConfig removes ClawBench's managed block and puts the original
// top-level value back. A no-op when ClawBench never touched the file.
func RestoreProviderConfig(backend string) error {
	paths := ProviderConfigPaths(backend)
	if len(paths) == 0 {
		return nil
	}
	providerConfigMu.Lock()
	defer providerConfigMu.Unlock()

	if backend == backendCodex {
		return restoreCodexProvider(paths[0])
	}
	return nil
}

func applyCodexProvider(p *model.Provider) error {
	paths := ProviderConfigPaths(backendCodex)
	if len(paths) == 0 {
		return fmt.Errorf("codex config path unavailable")
	}
	path := paths[0]

	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read codex config: %w", err)
	}
	content := string(original)

	state, _ := loadConfigState()
	prev, hadState := state[path]
	if !hadState {
		_, origValue, hadKey := findTopLevelKey(stripManagedBlock(content, codexBlockStart, codexBlockEnd), "model_provider")
		prev = codexState{OriginalModelProvider: origValue, HadModelProvider: hadKey}
	}

	next := stripManagedBlock(content, codexBlockStart, codexBlockEnd)
	next = upsertTopLevelKey(next, "model_provider", tomlString("clawbench"))
	next = strings.TrimRight(next, "\n")
	if next != "" {
		next += "\n\n"
	}
	next += codexProviderBlock(p)

	if next == content {
		return nil // already applied
	}

	if len(original) > 0 {
		backup, berr := backupFile(path, original)
		if berr != nil {
			return berr
		}
		prev.Backup = backup
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return err
	}

	state[path] = prev
	return saveConfigState(state)
}

func restoreCodexProvider(path string) error {
	state, err := loadConfigState()
	if err != nil {
		return err
	}
	prev, ok := state[path]
	if !ok {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			delete(state, path)
			return saveConfigState(state)
		}
		return fmt.Errorf("read codex config: %w", err)
	}

	next := stripManagedBlock(string(content), codexBlockStart, codexBlockEnd)
	if prev.HadModelProvider {
		next = upsertTopLevelKey(next, "model_provider", prev.OriginalModelProvider)
	} else {
		next = removeTopLevelKey(next, "model_provider")
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return err
	}
	delete(state, path)
	return saveConfigState(state)
}

// codexProviderBlock renders the managed TOML block. The secret is referenced
// through env_key, never written to disk.
func codexProviderBlock(p *model.Provider) string {
	return codexBlockStart + "\n" +
		"[model_providers.clawbench]\n" +
		"name = " + tomlString(p.Name) + "\n" +
		"base_url = " + tomlString(p.BaseURL) + "\n" +
		`env_key = "CLAWBENCH_PROVIDER_API_KEY"` + "\n" +
		`wire_api = "responses"` + "\n" +
		"requires_openai_auth = false\n" +
		codexBlockEnd + "\n"
}

func tomlString(s string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + escaped + `"`
}

func stripManagedBlock(content, start, end string) string {
	for {
		i := strings.Index(content, start)
		if i < 0 {
			return content
		}
		j := strings.Index(content[i:], end)
		if j < 0 {
			return content
		}
		j += i + len(end)
		// Swallow the newline that follows the end marker.
		if j < len(content) && content[j] == '\n' {
			j++
		}
		content = content[:i] + content[j:]
	}
}

// findTopLevelKey looks for `key = value` before the first table header. TOML
// requires top-level keys to precede any [table], so this is exactly the
// region a top-level assignment may live in.
func findTopLevelKey(content, key string) (lineIndex int, value string, found bool) {
	lines := strings.Split(content, "\n")
	limit := topLevelLimit(lines)
	for i := range limit {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, key) {
			rest := strings.TrimSpace(trimmed[len(key):])
			if strings.HasPrefix(rest, "=") {
				return i, strings.TrimSpace(rest[1:]), true
			}
		}
	}
	return -1, "", false
}

func upsertTopLevelKey(content, key, value string) string {
	lines := strings.Split(content, "\n")
	limit := topLevelLimit(lines)
	if idx, _, found := findTopLevelKey(content, key); found {
		lines[idx] = key + " = " + value
		return strings.Join(lines, "\n")
	}
	insert := append([]string{}, lines[:limit]...)
	insert = append(insert, key+" = "+value)
	insert = append(insert, lines[limit:]...)
	return strings.Join(insert, "\n")
}

func removeTopLevelKey(content, key string) string {
	idx, _, found := findTopLevelKey(content, key)
	if !found {
		return content
	}
	lines := strings.Split(content, "\n")
	lines = append(lines[:idx], lines[idx+1:]...)
	return strings.Join(lines, "\n")
}

// topLevelLimit returns the index of the first table header (or len(lines)).
func topLevelLimit(lines []string) int {
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			return i
		}
	}
	return len(lines)
}

func configStatePath() string {
	return filepath.Join(model.DataDir, "providers", "config-state.json")
}

func loadConfigState() (map[string]codexState, error) {
	out := make(map[string]codexState)
	data, err := os.ReadFile(configStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		// A corrupt state file must not block applying a provider; losing it
		// only means the original value cannot be restored automatically.
		//
		//nolint:nilerr // deliberate fallback: an unreadable sidecar is not fatal
		return make(map[string]codexState), nil
	}
	return out, nil
}

func saveConfigState(state map[string]codexState) error {
	path := configStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// backupFile copies the current content to a sibling backup and prunes old
// backups so a long-lived install cannot accumulate them.
func backupFile(path string, content []byte) (string, error) {
	backup := fmt.Sprintf("%s.clawbench-bak-%d", path, time.Now().Unix())
	if err := os.WriteFile(backup, content, 0o600); err != nil {
		return "", fmt.Errorf("backup %s: %w", path, err)
	}
	pruneBackups(path, 3)
	return backup, nil
}

func pruneBackups(path string, keep int) {
	matches, err := filepath.Glob(path + ".clawbench-bak-*")
	if err != nil || len(matches) <= keep {
		return
	}
	// Glob returns lexicographic order; timestamps sort chronologically.
	for _, old := range matches[:len(matches)-keep] {
		_ = os.Remove(old)
	}
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".clawbench-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
