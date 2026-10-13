package ai

import (
	"context"
	"log/slog"
	"time"
)

// modelSwitchTimeout bounds a session/set_model RPC.
//
// The call runs BEFORE Prompt installs promptCancel and the stall watchdog, so
// there is nothing else to interrupt it. Agents that honor set_model rebuild
// their session agent synchronously and may probe the network first (Hermes'
// _switch_model does ~10s of I/O cold), so this is generous but finite — a hung
// agent must not wedge the whole turn.
//
// A package variable so tests can shrink it.
var modelSwitchTimeout = 30 * time.Second

// modelSwitchMethodFor returns the JSON-RPC method the connection's backend
// honors for a model switch, or "" for the default config-option path.
func (c *ACPConn) modelSwitchMethodFor() string {
	if LookupACPModelSwitchMethodFn == nil {
		return ""
	}
	return LookupACPModelSwitchMethodFn(c.BackendID())
}

// applyModelSelection applies a model selection using the mechanism the
// backend declared.
//
// The default is the config-option path (session/set_config_option, configId
// "model"), which is what most agents implement. A backend that declares
// ACPModelSwitchSetModel instead accepts the config option but never acts on
// it, so its selection must be sent as session/set_model — otherwise the switch
// is a silent no-op and the agent keeps running its previous model.
//
// Callers must NOT hold c.mu (CallRaw takes it).
func (c *ACPConn) applyModelSelection(ctx context.Context, acpSID, value string) error {
	if value == "" {
		return nil
	}
	if method := c.modelSwitchMethodFor(); method != "" {
		return c.applyModelViaSetModel(ctx, acpSID, method, value)
	}
	return c.setConfigOptionWithCrashCheck(ctx, acpSID, configOptionSpec{
		configID: c.resolveWireConfigID("model", "model"),
		value:    value,
		label:    "model",
	})
}

// applyModelViaSetModel sends the model switch over the raw JSON-RPC channel.
//
// Dedup mirrors the config-option path (shouldSetConfig keys on "model"): the
// RPC can cost seconds, and Prompt applies the selection on EVERY turn, so
// re-sending an unchanged value would tax every message.
//
// On failure the value is deliberately NOT recorded, so the next turn retries —
// a transient rejection (e.g. the agent was busy) must not permanently pin the
// session to a model the user did not choose.
//
// Callers must NOT hold c.mu.
func (c *ACPConn) applyModelViaSetModel(ctx context.Context, acpSID, method, value string) error {
	if !c.shouldSetConfig("model", value) {
		slog.Debug("acp conn: set_model skipped (unchanged)",
			"method", method, "value", value,
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
		return nil
	}

	callCtx, cancel := context.WithTimeout(ctx, modelSwitchTimeout)
	defer cancel()

	start := time.Now()
	slog.Info("acp conn: set_model starting",
		"method", method, "value", value,
		"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	_, err := c.CallRaw(callCtx, method, map[string]any{
		"sessionId": acpSID,
		"modelId":   value,
	})
	slog.Info("acp conn: set_model done",
		"method", method, "value", value, "elapsed", time.Since(start), "error", err,
		"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)

	if err != nil {
		return err
	}

	// A set_model that killed the process is a retryable connection crash, same
	// as the config-option path — surface it as such so the caller respawns.
	if !c.IsAlive() {
		diag := c.collectCrashDiagnostics()
		slog.Error("acp conn: set_model killed connection",
			"method", method, "value", value,
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID,
			"exit_code", diag.ExitCode, "signal", diag.Signal,
			"ppid", diag.ParentPID, "rss_mb", diag.VMRSSKB/1024, "fds", diag.FDCount,
			"stderr_tail", diag.StderrTail)
		return errConfigKilledConnectionWithDiag("model", value, diag)
	}

	c.markConfigSet("model", value)
	// Keep the session-level cached value in sync so re-emitted WS model state
	// and GET /api/ai/chat report the model the user actually selected.
	c.UpdateCachedCurrent("model", value)
	return nil
}

// reapplyModelSelection re-applies a model after a ResumeSession.
//
// The caller holds c.mu (see ensureAliveWithSession), and the raw channel takes
// it internally, so the set_model branch must unlock around the call — the same
// unlock → RPC → relock dance as reapplyConfigOption. The config-option branch
// already does its own unlock, so it is called as-is.
func (c *ACPConn) reapplyModelSelection(ctx context.Context, acpSID, value string) {
	if value == "" || !c.alive || !c.isAliveLocked() {
		return
	}
	method := c.modelSwitchMethodFor()
	if method == "" {
		c.reapplyConfigOption(ctx, acpSID, c.resolveWireConfigIDLocked("model", "model"), value)
		return
	}

	c.mu.Unlock()
	err := c.applyModelViaSetModel(ctx, acpSID, method, value)
	c.mu.Lock()
	if err != nil {
		slog.Warn("acp conn: re-applying model after resume failed",
			"method", method, "value", value, "error", err,
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	}
}
