package ai

import (
	"context"
	"sort"

	acp "github.com/coder/acp-go-sdk"
)

// LookupACPInitialConfigOptionsFn returns the config options a backend's NEW
// sessions must start with, or nil when it declares none. Set by the backends
// package during init().
var LookupACPInitialConfigOptionsFn func(backendID string) map[string]string

// acpInitialConfigOptions is the nil-safe wrapper around the lookup.
func acpInitialConfigOptions(backendID string) map[string]string {
	if backendID == "" || LookupACPInitialConfigOptionsFn == nil {
		return nil
	}
	return LookupACPInitialConfigOptionsFn(backendID)
}

// applyInitialConfigOptionsLocked applies the backend's startup defaults to a
// session that was just created.
//
// It exists because some agents start every new session in a state the host
// cannot live with: kiro advertises `autopilot=on` ("Agent executes tools
// without confirmation"), which suppresses session/request_permission
// entirely — ClawBench would show no permission card and the user would believe
// every tool call was approved. A config option can only be written AFTER
// session/new, and it resets with every new session, so the write belongs here.
//
// Only options the agent actually advertised are sent: an unknown id answers
// -32602, and a value equal to the current one is skipped so a session that
// already starts correctly costs no round trip.
//
// Caller holds c.mu (createSessionLocked's contract). reapplyConfigOption
// releases it around the RPC, which is the only safe way to send one.
func (c *ACPConn) applyInitialConfigOptionsLocked(ctx context.Context, opts []acp.SessionConfigOption) {
	for _, cfg := range initialConfigSends(acpInitialConfigOptions(c.BackendID()), opts) {
		c.reapplyConfigOption(ctx, c.acpSID, cfg.configID, cfg.value)
	}
}

// initialConfigSends is the pure decision half of applyInitialConfigOptions:
// which declared defaults must actually be sent to this session.
//
// Sorted by id so the RPC order (and the log) is deterministic.
func initialConfigSends(desired map[string]string, opts []acp.SessionConfigOption) []configOptionSpec {
	if len(desired) == 0 {
		return nil
	}
	ids := make([]string, 0, len(desired))
	for id := range desired {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]configOptionSpec, 0, len(ids))
	for _, id := range ids {
		want := desired[id]
		if want == "" {
			continue
		}
		current, advertised := advertisedConfigValue(opts, id)
		if !advertised {
			// The agent did not list the option in session/new (an
			// unauthenticated kiro, for one, advertises almost nothing).
			// Sending it would only produce -32602.
			continue
		}
		if current == want {
			continue
		}
		out = append(out, configOptionSpec{configID: id, value: want, label: "initial_config"})
	}
	return out
}

// advertisedConfigValue reads one option's current value out of a
// session/new (or set_config_option) response, whatever its wire shape.
func advertisedConfigValue(opts []acp.SessionConfigOption, id string) (string, bool) {
	for _, opt := range opts {
		if opt.Select != nil && string(opt.Select.Id) == id {
			return string(opt.Select.CurrentValue), true
		}
		if opt.Boolean != nil && string(opt.Boolean.Id) == id {
			if opt.Boolean.CurrentValue {
				return "true", true
			}
			return "false", true
		}
	}
	return "", false
}
