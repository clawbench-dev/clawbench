package service

import (
	"fmt"
	"sync"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// providerConfigMu serializes config-file edits. Agent PATCH holds the handler's
// configMutex, but provider updates (PUT /api/providers) do not, so the file
// writer needs its own lock.
var providerConfigMu sync.Mutex

// ProviderConfigPaths lists the files a config-file backend needs ClawBench to
// modify in order to apply a provider. An empty list means the backend is
// environment-driven (or unsupported) and needs no file consent.
//
// Delegates to the model layer: the same layout also drives the consent dialog's
// display path, and two copies of it drifted apart the moment a backend was
// added.
func ProviderConfigPaths(backend string) []string {
	return model.ProviderConfigPaths(backend)
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
	apply, _ := model.ProviderConfigFileHooks(backend)
	if apply == nil {
		// Declared a config file but no writer: refuse rather than report
		// success, which would persist a binding the CLI never learns about.
		return fmt.Errorf("backend %q has no config-file writer", backend)
	}
	providerConfigMu.Lock()
	defer providerConfigMu.Unlock()
	return apply(p)
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
	// Every config-file backend with no binding left must have its managed block
	// removed: the file is shared with the user and possibly another tool, so a
	// stale block outlives the binding that created it. Iterating the registry
	// (rather than naming one backend) means a new config-file backend is
	// covered without touching this function.
	for _, backend := range model.ConfigFileBackends() {
		if bound[backend] {
			continue
		}
		if err := RestoreProviderConfig(backend); err != nil {
			return err
		}
	}
	// Per-agent config directories are reconciled separately: they are keyed by
	// agent, not by backend, so two agents of one backend cannot collide.
	return ReconcileProviderConfigDirs()
}

// RestoreProviderConfig removes ClawBench's managed block and puts the original
// top-level value back. A no-op when ClawBench never touched the file.
func RestoreProviderConfig(backend string) error {
	if !model.ProviderUsesConfigFile(backend) {
		return nil
	}
	_, restore := model.ProviderConfigFileHooks(backend)
	if restore == nil {
		return fmt.Errorf("backend %q has no config-file restorer", backend)
	}
	providerConfigMu.Lock()
	defer providerConfigMu.Unlock()
	return restore()
}
