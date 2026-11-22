package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"clawbench/internal/model"
)

// Per-agent config directories.
//
// A backend with a relocatable config directory (kimi's KIMI_SHARE_DIR, pi's
// PI_CODING_AGENT_DIR, codebuddy's CODEBUDDY_CONFIG_DIR) gets one private
// directory per agent under ClawBench's data dir. ClawBench writes only the one
// file it owns inside that directory; every other entry is symlinked back to
// the user's real config directory.
//
// Why symlinks rather than copies: those directories hold the login state
// (kimi's credentials/, codebuddy's 18 GB of caches and session blobs) and the
// user's skills. Copying would duplicate gigabytes and freeze the login at
// copy time; symlinking keeps them live and costs nothing.
//
// Why a directory rather than editing the user's file (the codex approach):
// the user's file is a single global one, so two agents of the same backend
// cannot point at different providers. A private directory makes "one provider
// per agent" real instead of last-write-wins.

// providerConfigDirRoot is where per-agent config directories live.
func providerConfigDirRoot() string {
	return filepath.Join(model.DataDir, "providers", "agents")
}

// Register the config-directory writer with the model layer. model cannot
// import service (service imports model), so the spawn path reaches the writer
// through this seam.
func init() {
	model.ConfigDirEnvForAgent = applyProviderConfigDir
}

const (
	// backendCodebuddy appears in both the read (user config dir) and write
	// (config-dir writer) switches; a typo in either would silently disable the
	// relocation for that backend.
	backendCodebuddy = "codebuddy"
	// jsonKeyName is the model-display-name key inside the generated JSON
	// config documents.
	jsonKeyName = "name"
)

// ProviderConfigDir returns the private config directory for one agent.
//
// The name is the agent id, which is already a filesystem-safe slug
// (agent-<hex> / user-chosen ids are validated elsewhere). It is intentionally
// NOT hashed: a directory the user cannot recognize in a stack trace is worse
// than one they can.
func ProviderConfigDir(agentID string) string {
	return filepath.Join(providerConfigDirRoot(), agentID)
}

// userConfigDir returns the directory the backend would use without
// ClawBench's override — the thing we symlink the user's own data back from.
//
// known is false when this function has no mapping for the backend. That is a
// programming error, not "no config": model already declared the backend as
// relocatable (ProviderUsesConfigDir), so silently returning "" would write an
// isolated directory with no symlinks and hand the CLI a login-less config.
// Callers that gate on the model spec must treat !known as an error; the
// literal callers below ignore it because they name a mapped backend.
func userConfigDir(backend string) (dir string, known bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// No home directory: there is nothing to link, which is not a mapping
		// gap. Every backend is still "known".
		return "", true
	}
	switch backend {
	case "kimi":
		return filepath.Join(home, ".kimi"), true
	case "pi":
		return filepath.Join(home, ".pi", "agent"), true
	case backendCodebuddy:
		return filepath.Join(home, ".codebuddy"), true
	case "claude":
		// CLAUDE_CONFIG_DIR relocates the whole ~/.claude tree.
		return filepath.Join(home, ".claude"), true
	}
	return "", false
}

// ensureProviderConfigDir materializes the agent's config directory: it creates
// the directory and links every entry of the user's own config dir into it,
// except the file ClawBench owns.
//
// Idempotent: an existing link is left alone, a stale one is repointed, and a
// user's real file that happens to sit at the owned name is preserved under a
// backup suffix rather than silently overwritten (it cannot be — the owned file
// is written by the caller afterwards).
func ensureProviderConfigDir(backend, agentID string) (string, error) {
	if !model.ProviderUsesConfigDir(backend) {
		return "", fmt.Errorf("backend %q has no relocatable config directory", backend)
	}
	dir := ProviderConfigDir(agentID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create provider config dir: %w", err)
	}

	owned := model.ProviderConfigDirFile(backend)
	source, known := userConfigDir(backend)
	if !known {
		return "", fmt.Errorf("backend %q declares a relocatable config directory but service has no user-directory mapping for it", backend)
	}
	if source == "" {
		return dir, nil
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		// A user who never ran the CLI has no config dir: nothing to link, and
		// the file ClawBench writes is all the CLI needs.
		if os.IsNotExist(err) {
			return dir, nil
		}
		return "", fmt.Errorf("read user config dir: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == owned {
			// The one file we own. The user's own copy stays untouched in their
			// directory; ClawBench's version replaces it for this agent only.
			continue
		}
		link := filepath.Join(dir, name)
		target := filepath.Join(source, name)
		if err := ensureSymlink(link, target); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// ensureSymlink points link at target, replacing a stale link but never a real
// directory or file the user may have put there.
func ensureSymlink(link, target string) error {
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if current, rerr := os.Readlink(link); rerr == nil && current == target {
				return nil
			}
			if rerr := os.Remove(link); rerr != nil {
				return fmt.Errorf("replace config link %s: %w", link, rerr)
			}
		} else {
			// A real entry. It is either something the CLI created inside the
			// isolated directory (fine — leave it, it is this agent's own) or a
			// leftover; either way it is not ours to delete.
			return nil
		}
	}
	if err := os.Symlink(target, link); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("link %s -> %s: %w", link, target, err)
	}
	return nil
}

// applyProviderConfigDir writes the provider into the agent's private config
// file and returns the environment entries that point the CLI at it.
//
// The returned env is what makes the directory take effect: the directory alone
// changes nothing, the variable is what the CLI reads.
func applyProviderConfigDir(backend, agentID string, p *model.Provider) ([]string, error) {
	if !model.ProviderUsesConfigDir(backend) || p == nil {
		return nil, nil
	}
	dir, err := ensureProviderConfigDir(backend, agentID)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, model.ProviderConfigDirFile(backend))

	var content []byte
	switch backend {
	case "kimi":
		content, err = kimiConfigContent(p)
	case "pi":
		content, err = piModelsContent(p)
	case backendCodebuddy:
		content, err = codebuddyModelsContent(p)
	case "claude":
		content, err = claudeSettingsContent(p)
	default:
		return nil, fmt.Errorf("backend %q has no config-dir writer", backend)
	}
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, content); err != nil {
		return nil, err
	}
	return []string{model.ProviderConfigDirEnv(backend) + "=" + dir}, nil
}

// kimiConfigContent renders kimi's config.toml.
//
// The provider type is chosen from the provider's protocol: kimi reads
// "openai_legacy" for OpenAI-compatible endpoints and "anthropic" for
// Anthropic-compatible ones. The key is written in clear text because kimi's
// config has no ${ENV} reference syntax (verified: a literal "${VAR}" is sent
// as the key and the endpoint answers 401).
func kimiConfigContent(p *model.Provider) ([]byte, error) {
	providerType := "openai_legacy"
	if p.Protocol == model.ProviderProtocolAnthropic {
		providerType = "anthropic"
	}
	id := model.ProviderConfigProviderID("kimi")
	models := providerModelIDs(p)
	if len(models) == 0 {
		return nil, fmt.Errorf("provider %q has no models", p.Name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by ClawBench for one agent. Edits are overwritten.\n")
	fmt.Fprintf(&b, "default_model = %s\n\n", tomlString(models[0]))

	for _, m := range models {
		fmt.Fprintf(&b, "[models.%s]\n", tomlKey(m))
		fmt.Fprintf(&b, "provider = %s\n", tomlString(id))
		fmt.Fprintf(&b, "model = %s\n", tomlString(m))
		fmt.Fprintf(&b, "max_context_size = 128000\n\n")
	}

	fmt.Fprintf(&b, "[providers.%s]\n", tomlKey(id))
	fmt.Fprintf(&b, "type = %s\n", tomlString(providerType))
	fmt.Fprintf(&b, "base_url = %s\n", tomlString(p.BaseURL))
	fmt.Fprintf(&b, "api_key = %s\n", tomlString(p.APIKey))
	return []byte(b.String()), nil
}

// tomlKey renders a TOML bare key when possible and a quoted key otherwise.
// Model ids routinely contain dots ("deepseek-v4.1-flash"), which TOML reads as
// a nested table — an unquoted "models.deepseek-v4.1-flash" would silently
// create three levels and the model would not be found.
func tomlKey(s string) string {
	if s == "" {
		return tomlString(s)
	}
	for _, r := range s {
		bare := r == '-' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9')
		if !bare {
			return tomlString(s)
		}
	}
	return s
}

// piModelsContent renders pi's models.json.
//
// pi resolves a model as "<provider>/<model>", so the provider key is ours and
// the picker hands out the prefixed form (see ProviderModelIDFor).
func piModelsContent(p *model.Provider) ([]byte, error) {
	id := model.ProviderConfigProviderID("pi")
	api := "openai-completions"
	if p.Protocol == model.ProviderProtocolAnthropic {
		api = "anthropic-messages"
	}
	models := providerModelIDs(p)
	if len(models) == 0 {
		return nil, fmt.Errorf("provider %q has no models", p.Name)
	}
	declared := make([]map[string]any, 0, len(models))
	for _, m := range models {
		declared = append(declared, map[string]any{"id": m, jsonKeyName: m})
	}
	doc := map[string]any{
		"providers": map[string]any{
			id: map[string]any{
				jsonKeyName: "ClawBench " + p.Name,
				"baseUrl":   p.BaseURL,
				// pi expands $ENV in apiKey, so the secret stays in ClawBench's
				// database and is handed to the process through the environment.
				"apiKey": "$" + model.ProviderSecretEnvName(),
				"api":    api,
				"models": declared,
			},
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// codebuddyModelsContent renders CodeBuddy's models.json.
//
// Each model carries its own url and apiKey, and that url WINS over
// CODEBUDDY_BASE_URL (verified). apiKey supports ${ENV}, so the secret is not
// written to disk.
func codebuddyModelsContent(p *model.Provider) ([]byte, error) {
	models := providerModelIDs(p)
	if len(models) == 0 {
		return nil, fmt.Errorf("provider %q has no models", p.Name)
	}
	declared := make([]map[string]any, 0, len(models))
	for _, m := range models {
		declared = append(declared, map[string]any{
			"id":               m,
			jsonKeyName:        m,
			"provider":         "openai",
			"url":              p.BaseURL,
			"apiKey":           "${" + model.ProviderSecretEnvName() + "}",
			"supportsToolCall": true,
		})
	}
	return json.MarshalIndent(map[string]any{"models": declared}, "", "  ")
}

// claudeSettingsContent renders Claude Code's settings.json.
//
// Only the `env` block is ClawBench's: that is where the CLI reads the endpoint
// and credential from (the ACP adapter merges it into the session). Everything
// else in the file — permissions, model overrides, plugins — belongs to the
// user, so it is carried over from the user's own settings.json.
//
// Carrying it over matters: the isolated directory is a NEW directory, so a file
// holding only `env` would silently drop the user's approval rules and plugin
// list for this agent, which reads as "ClawBench turned my permissions off".
//
// The key is written in clear text: verified that a "${VAR}" reference is sent
// verbatim (the endpoint answered 401), so there is no reference syntax to use.
func claudeSettingsContent(p *model.Provider) ([]byte, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		// Without a base URL there is nothing to redirect: the CLI would keep
		// talking to its own default endpoint while the binding looked applied.
		return nil, fmt.Errorf("provider %q has no base URL, which claude needs", p.Name)
	}

	doc := make(map[string]any)
	if base, ok := readUserClaudeSettings(); ok {
		for k, v := range base {
			doc[k] = v
		}
	}

	env := map[string]any{"ANTHROPIC_BASE_URL": p.BaseURL}
	// Keep any other variables the user had in their env block (a proxy, a
	// feature flag): the endpoint and credential are the only keys ClawBench
	// owns, and replacing the whole block would turn those off.
	if prev, ok := doc["env"].(map[string]any); ok {
		for k, v := range prev {
			if k == "ANTHROPIC_BASE_URL" || k == "ANTHROPIC_API_KEY" || k == "ANTHROPIC_AUTH_TOKEN" {
				continue
			}
			env[k] = v
		}
	}
	// Which variable carries the credential follows the provider's auth mode: a
	// gateway that only accepts x-api-key must not be sent a Bearer token.
	if p.AuthMode == model.ProviderAuthBearer {
		env["ANTHROPIC_AUTH_TOKEN"] = p.APIKey
	} else {
		env["ANTHROPIC_API_KEY"] = p.APIKey
	}
	doc["env"] = env

	return json.MarshalIndent(doc, "", "  ")
}

// readUserClaudeSettings loads the user's own ~/.claude/settings.json so the
// per-agent copy can preserve everything ClawBench does not own.
//
// A missing or unreadable file is not an error: it only means there is nothing
// to carry over, and the isolated file still gets the env block that makes the
// provider work.
func readUserClaudeSettings() (map[string]any, bool) {
	dir, _ := userConfigDir("claude") // "claude" is always mapped
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	return doc, true
}

// providerModelIDs is the provider's curated list, falling back to its default
// model when the list is empty. Config-file backends must declare at least one
// model: a provider block with no models is invisible to the CLI.
func providerModelIDs(p *model.Provider) []string {
	out := make([]string, 0, len(p.Models))
	for _, id := range p.Models {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	if len(out) == 0 && strings.TrimSpace(p.DefaultModel) != "" {
		out = append(out, strings.TrimSpace(p.DefaultModel))
	}
	return out
}

// RestoreProviderConfigDir removes an agent's private config directory.
//
// Called when a binding is cleared or the agent is deleted. A missing directory
// is not an error: most agents never had a binding.
func RestoreProviderConfigDir(agentID string) error {
	dir := ProviderConfigDir(agentID)
	if dir == "" || dir == providerConfigDirRoot() {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove provider config dir: %w", err)
	}
	return nil
}

// ApplyProviderConfigDirToAgent is the exported entry point for the binding
// handler: it writes the file BEFORE the binding is persisted, so a failed write
// cannot leave an agent pointing at a provider the CLI never learned about.
func ApplyProviderConfigDirToAgent(backend, agentID string, p *model.Provider) error {
	if !model.ProviderUsesConfigDir(backend) {
		// Never return nil here: the caller treats a nil error as "the file is
		// in place" and persists the binding. A silent success would leave an
		// agent bound to a provider the CLI never learns about.
		return fmt.Errorf("backend %q has no relocatable config directory", backend)
	}
	providerConfigMu.Lock()
	defer providerConfigMu.Unlock()
	_, err := applyProviderConfigDir(backend, agentID, p)
	return err
}

// ReconcileProviderConfigDirs rewrites every agent's private config file at
// startup, so a provider edit made while the server was down cannot leave a
// stale endpoint on disk. Directories of unbound agents are removed.
//
// Best effort per agent, like DeleteProjectData: every agent is visited even
// after one fails, and the errors are summarized at the end. Stopping at the
// first failure would leave the remaining agents on stale endpoints — and, far
// worse, would skip the cleanup of unbound/deleted agents entirely, leaving
// their plaintext API keys on disk after the user unbound or deleted them.
// Those files cannot be repaired by a later retry the user knows about, so the
// cleanup half especially must not be abandoned.
func ReconcileProviderConfigDirs() error {
	agents := model.GetAgentList()
	bound := make(map[string]bool, len(agents))
	var errs []error

	for _, a := range agents {
		if !model.ProviderUsesConfigDir(a.Backend) {
			continue
		}
		if a.ProviderID == nil {
			if err := RestoreProviderConfigDir(a.ID); err != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", a.ID, err))
			}
			continue
		}
		p := model.AgentProvider(a.ID)
		if p == nil {
			continue
		}
		// A credential-only provider (no endpoint, the backend's own official
		// service) never uses a config file: remove any leftover from an earlier
		// endpoint binding and leave the directory unmarked so it can be pruned.
		if model.ProviderOwnServiceMode(a.Backend, p) {
			if err := RestoreProviderConfigDir(a.ID); err != nil {
				errs = append(errs, fmt.Errorf("restore %s: %w", a.ID, err))
			}
			continue
		}
		bound[a.ID] = true
		if _, err := applyProviderConfigDir(a.Backend, a.ID, p); err != nil {
			// Keep the directory marked as bound: a write failure must not turn
			// into "prune it", which would delete the agent's working config
			// because of a transient permission error.
			errs = append(errs, fmt.Errorf("apply %s: %w", a.ID, err))
		}
	}

	if err := pruneOrphanConfigDirs(bound); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		slog.Warn("provider config dir reconcile had failures",
			slog.Int("failed", len(errs)), slog.Int("agents", len(agents)))
		return errors.Join(errs...)
	}
	return nil
}

// pruneOrphanConfigDirs removes directories whose agent no longer exists, so a
// deleted agent cannot leave an API key on disk forever.
func pruneOrphanConfigDirs(bound map[string]bool) error {
	root := providerConfigDirRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if bound[name] {
			continue
		}
		if model.GetAgent(name) != nil {
			// Still bound to another config-dir backend; its own reconcile
			// pass owns it.
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return fmt.Errorf("prune provider config dir %s: %w", name, err)
		}
	}
	return nil
}
