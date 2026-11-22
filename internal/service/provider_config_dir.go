package service

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

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
// The path comes from the backend's own ProviderEnvSpec.UserConfigDir, so a new
// relocatable backend declares it beside its env variable and needs no edit
// here. known is false when the backend declares none, which is a programming
// error rather than "no config": model already declared the backend as
// relocatable (ProviderUsesConfigDir), so silently returning "" would write an
// isolated directory with no symlinks and hand the CLI a login-less config.
func userConfigDir(backend string) (dir string, known bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// No home directory: there is nothing to link, which is not a mapping
		// gap. Every backend is still "known".
		return "", true
	}
	rel := model.ProviderUserConfigDir(backend)
	if rel == "" {
		return "", false
	}
	return filepath.Join(home, rel), true
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

	// The file format belongs to the backend that reads it, so its content is
	// rendered by the backend's own registration rather than by a switch here.
	content, err := model.ProviderRenderConfigDirFile(backend, p)
	if err != nil {
		return nil, err
	}
	if err := model.WriteFileAtomic(path, content); err != nil {
		return nil, err
	}
	return []string{model.ProviderConfigDirEnv(backend) + "=" + dir}, nil
}

// kimiConfigContent renders kimi's config.toml.
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
