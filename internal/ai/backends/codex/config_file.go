package codex

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// codex provider config file
// ---------------------------------------------------------------------------
//
// codex reads its provider from ~/.codex/config.toml — a file the USER owns and
// another tool (a gateway such as Codex++) may also write. Applying a provider
// therefore means maintaining one marked block inside somebody else's document,
// and undoing it means putting back whatever was there before.
//
// This used to live in internal/service behind `if backend == "codex"`, which
// meant a second config-file backend would take the same path and silently
// report success on a file nothing had written. The format (a TOML block, a
// top-level key, a sidecar of originals) is codex's own business, so it lives
// here and reaches the shared layer through model.ProviderEnvSpec.
//
// Contract with the shared layer:
//   - ProviderConfigFileApply is called under service's provider lock.
//   - ProviderConfigFileRestore is called with no provider: it undoes whatever
//     the last apply did, using its own record.
//   - Both find the target path through model.ProviderConfigPaths, so the file
//     layout is declared once.

const (
	// managedProviderKey is the provider id ClawBench writes into codex's own
	// configuration (`model_provider` / `[model_providers.*]`).
	//
	// It is a config contract rather than a display name: changing it would
	// orphan every block already written to disk, so it is deliberately not
	// shared with restartBinaryName even though the two strings coincide.
	managedProviderKey = "clawbench"

	blockStart = "# >>> clawbench provider (managed by ClawBench) >>>"
	blockEnd   = "# <<< clawbench provider (managed by ClawBench) <<<"
)

// configStateMu serializes the sidecar. Applies and restores of different
// agents can interleave (provider edits do not take the handler's config
// mutex), and read-modify-write of one JSON file without a lock loses records.
var configStateMu sync.Mutex

// fileState remembers what a config file looked like before ClawBench touched
// it, so unbinding can restore the user's own settings instead of leaving the
// managed provider behind.
type fileState struct {
	Backup                string `json:"backup"`
	OriginalModelProvider string `json:"originalModelProvider"`
	HadModelProvider      bool   `json:"hadModelProvider"`
}

// ApplyProvider writes the provider into codex's config.toml.
//
// The secret is referenced through env_key, never written to disk.
func ApplyProvider(p *model.Provider) error {
	paths := model.ProviderConfigPaths("codex")
	if len(paths) == 0 {
		return fmt.Errorf("codex config path unavailable")
	}
	path := paths[0]

	original, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read codex config: %w", err)
	}
	content := string(original)

	configStateMu.Lock()
	state, err := loadState()
	if err != nil {
		configStateMu.Unlock()
		// Proceed no further: without the sidecar we cannot record what the
		// user had, and writing the file first would leave it pointing at a
		// provider that can never be restored.
		return fmt.Errorf("load codex config state: %w", err)
	}
	prev, hadState := state[path]
	configStateMu.Unlock()

	// Read the value the file holds NOW rather than trusting the sidecar alone.
	// Another tool (or a hand edit) may have repointed model_provider after our
	// last apply, and that newer value — not the one we recorded back then — is
	// what unbinding has to put back. Keeping the old record made "bind again,
	// then unbind" overwrite the user's edit with a stale original.
	_, current, hadKey := findTopLevelKey(stripManagedBlock(content), "model_provider")
	switch {
	case hadKey && tomlValueIsManagedProvider(current):
		// Still our own value: what the user had is in the sidecar, or — when
		// that record is gone — in the pristine backup.
		if !hadState {
			if value, had, ok := originalModelProviderFromBackup(path); ok {
				prev = fileState{OriginalModelProvider: value, HadModelProvider: had}
			} else {
				prev = fileState{}
			}
		}
	default:
		// The file holds a value ClawBench did not write, so THAT is the
		// original — a stale sidecar entry would overwrite it on unbind.
		//
		// Stored as the raw right-hand side (quotes included): restore writes it
		// back verbatim, so stripping the quotes here produced a bare
		// `model_provider = deepseek`, which TOML rejects.
		prev = fileState{OriginalModelProvider: current, HadModelProvider: hadKey}
	}

	next := upsertTopLevelKey(stripManagedBlock(content), tomlString(managedProviderKey))
	next = strings.TrimRight(next, "\n")
	if next != "" {
		next += "\n\n"
	}
	next += providerBlock(p)

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
	// Record the original state BEFORE rewriting the file. If the sidecar
	// cannot be written, nothing has been changed yet; the other order leaves
	// config.toml pointing at a provider whose original value was never
	// recorded. (A state entry without a managed block is harmless: restore
	// would just put the original value back.)
	state[path] = prev
	configStateMu.Lock()
	err = saveState(state)
	configStateMu.Unlock()
	if err != nil {
		return err
	}
	return model.WriteFileAtomic(path, []byte(next))
}

// RestoreProvider removes ClawBench's managed block and puts the original
// top-level value back. A no-op when ClawBench never touched the file.
func RestoreProvider() error {
	paths := model.ProviderConfigPaths("codex")
	if len(paths) == 0 {
		return nil
	}
	path := paths[0]

	configStateMu.Lock()
	state, err := loadState()
	if err != nil {
		configStateMu.Unlock()
		return err
	}
	prev, hadState := state[path]
	configStateMu.Unlock()

	content, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read codex config: %w", err)
		}
		if !hadState {
			return nil
		}
		configStateMu.Lock()
		defer configStateMu.Unlock()
		delete(state, path)
		return saveState(state)
	}

	next := stripManagedBlock(string(content))
	// Only a value ClawBench itself wrote is ours to put back. Another tool
	// sharing this file may have pointed model_provider somewhere else after our
	// last apply; restoring a remembered original over it would hijack that
	// tool's configuration and break any session it already created under the
	// other provider. So the key is left untouched unless it still reads
	// "clawbench".
	//
	// The managed block is stripped regardless: it declares a provider table
	// that is about to disappear.
	if _, current, hasKey := findTopLevelKey(next, "model_provider"); hasKey &&
		tomlValueIsManagedProvider(current) {
		next = restoreModelProviderKey(next, path, prev, hadState)
	}
	if next != string(content) {
		if err := model.WriteFileAtomic(path, []byte(next)); err != nil {
			return err
		}
		slog.Info("restored codex config file", slog.String("path", path))
	}
	if !hadState {
		return nil
	}
	configStateMu.Lock()
	defer configStateMu.Unlock()
	// Drop the record even when the file already looked restored: keeping it
	// would make the next reconcile re-apply a stale original over a value the
	// user has edited by hand since.
	delete(state, path)
	return saveState(state)
}

// restoreModelProviderKey puts the user's own top-level model_provider back
// after the managed block has been stripped. Only called for a key that still
// holds our own provider id.
func restoreModelProviderKey(next, path string, prev fileState, hadState bool) string {
	switch {
	case hadState && prev.HadModelProvider:
		return upsertTopLevelKey(next, prev.OriginalModelProvider)
	case hadState:
		return removeTopLevelKey(next)
	}
	// No sidecar record (corrupt, deleted, or a different data dir). The
	// backups are the only surviving record of the user's value; without a
	// usable one, drop the key rather than leave it dangling at the managed
	// provider.
	if value, had, ok := originalModelProviderFromBackup(path); ok && had {
		return upsertTopLevelKey(next, value)
	}
	return removeTopLevelKey(next)
}

// originalModelProviderFromBackup recovers the user's top-level model_provider
// from the newest backup that is not itself a ClawBench apply.
//
// A file ClawBench already rewrote is not a record of the original: its
// model_provider is our own "clawbench". Backups taken before the first apply
// are, so they are scanned newest-first and the first non-clawbench one wins.
func originalModelProviderFromBackup(path string) (value string, had bool, ok bool) {
	matches, err := filepath.Glob(path + ".clawbench-bak-*")
	if err != nil || len(matches) == 0 {
		return "", false, false
	}
	// Glob returns lexicographic order, so the newest backup is last.
	for i := len(matches) - 1; i >= 0; i-- {
		data, rerr := os.ReadFile(matches[i])
		if rerr != nil {
			continue
		}
		_, v, found := findTopLevelKey(string(data), "model_provider")
		if found && tomlValueIsManagedProvider(v) {
			continue
		}
		return v, found, true
	}
	return "", false, false
}

func unquoteTomlString(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// tomlValueIsManagedProvider reports whether a raw `model_provider = <raw>`
// right-hand side still holds the id ClawBench writes.
//
// The raw text is whatever followed the `=` on that line, so it may carry a
// trailing comment (`"clawbench" # note`). Leaving the comment in made a value
// we wrote read as foreign: the managed block would be stripped while the key
// stayed on `clawbench`, leaving codex pointing at a provider table that no
// longer exists.
//
// No quote-awareness is needed when cutting the comment: managedProviderKey is
// our own id and cannot contain the '#' that would be needed to tell a comment
// apart from a '#' inside the value.
func tomlValueIsManagedProvider(raw string) bool {
	if i := strings.IndexByte(raw, '#'); i >= 0 {
		raw = raw[:i]
	}
	return unquoteTomlString(strings.TrimSpace(raw)) == managedProviderKey
}

// providerBlock renders the managed TOML block. The secret is referenced
// through env_key, never written to disk.
//
// wire_api comes from the provider: codex defaults to the Responses API, which
// a gateway serving only /chat/completions does not implement. Writing
// "responses" unconditionally made every turn fail against those endpoints with
// nothing in the UI pointing at the cause.
func providerBlock(p *model.Provider) string {
	wire := p.WireAPI
	if !model.ValidWireAPI(wire) {
		wire = model.ProviderWireResponses
	}
	return blockStart + "\n" +
		"[model_providers." + managedProviderKey + "]\n" +
		"name = " + tomlString(p.Name) + "\n" +
		"base_url = " + tomlString(p.BaseURL) + "\n" +
		`env_key = "` + model.ProviderSecretEnvName() + `"` + "\n" +
		"wire_api = " + tomlString(wire) + "\n" +
		"requires_openai_auth = false\n" +
		blockEnd + "\n"
}

func tomlString(s string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	return `"` + escaped + `"`
}

// The markers are this backend's own, so they are not parameters: every
// caller strips exactly the block ApplyProvider writes.
func stripManagedBlock(content string) string {
	for {
		i := strings.Index(content, blockStart)
		if i < 0 {
			return content
		}
		j := strings.Index(content[i:], blockEnd)
		if j < 0 {
			// Half a block: the end marker was deleted by hand. Keeping the
			// remainder would leave two start markers, and the next strip would
			// delete everything between them — including the user's own
			// config. Drop to the end of the file instead.
			return content[:i]
		}
		j += i + len(blockEnd)
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

// upsertTopLevelKey writes the top-level model_provider assignment, replacing
// an existing one in place.
func upsertTopLevelKey(content, value string) string {
	key := "model_provider"
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

// removeTopLevelKey drops the top-level model_provider assignment.
func removeTopLevelKey(content string) string {
	idx, _, found := findTopLevelKey(content, "model_provider")
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

func loadState() (map[string]fileState, error) {
	out := make(map[string]fileState)
	data, err := os.ReadFile(configStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		// Never return a nil map: a caller that logs the error and carries on
		// would panic on the first write, after the config file had already
		// been rewritten.
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		// A corrupt state file must not block applying a provider; losing it
		// only means the original value cannot be restored automatically.
		//
		//nolint:nilerr // deliberate fallback: an unreadable sidecar is not fatal
		return make(map[string]fileState), nil
	}
	return out, nil
}

func saveState(state map[string]fileState) error {
	path := configStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return model.WriteFileAtomic(path, data)
}

// backupFile copies the current content to a sibling backup and prunes old
// backups so a long-lived install cannot accumulate them.
//
// The suffix is nanosecond-resolution on purpose: second-resolution names made
// two edits within the same second overwrite each other, so the pristine copy
// taken before the first apply could be replaced by one that already contained
// our own block — exactly the copy the sidecar-less restore path needs.
func backupFile(path string, content []byte) (string, error) {
	backup := fmt.Sprintf("%s.clawbench-bak-%d", path, time.Now().UnixNano())
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
	// Keep the OLDEST backup plus the newest keep-1. Only the oldest one can
	// predate our first apply, and it is the sole record of the user's original
	// model_provider — dropping it (which "keep the newest N" did on the 4th
	// apply) made the sidecar-less restore fall back to deleting the key.
	drop := matches[1:]
	if len(drop) > keep-1 {
		drop = drop[:len(drop)-(keep-1)]
	} else {
		drop = nil
	}
	// Glob returns lexicographic order; the fixed-width nanosecond timestamps
	// sort chronologically.
	for _, old := range drop {
		_ = os.Remove(old)
	}
}
