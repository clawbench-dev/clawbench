package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/model"
)

// ModelSwitchError describes a rejected model switch, so a caller (the HTTP
// layer) can tell a transient rejection from a permanent one without parsing
// error strings itself.
type ModelSwitchError struct {
	// Code is the JSON-RPC error code, or 0 when the failure was not a
	// JSON-RPC error (e.g. a timeout or a dead connection).
	Code int
	// Detail is the human-readable cause, preferring the agent's own
	// data.details over its generic message.
	Detail string
	// Retryable marks a TRANSIENT rejection: the agent was busy, the call timed
	// out, or the connection dropped. The caller should keep the user's choice
	// and let the next prompt retry it. A non-retryable failure (e.g. the
	// provider is not connected) would fail identically on retry.
	Retryable bool
}

func (e *ModelSwitchError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("model switch failed (%d): %s", e.Code, e.Detail)
	}
	return "model switch failed: " + e.Detail
}

// AsModelSwitchError returns the *ModelSwitchError in err's chain, if any.
func AsModelSwitchError(err error) (*ModelSwitchError, bool) {
	var mse *ModelSwitchError
	if errors.As(err, &mse) {
		return mse, true
	}
	return nil, false
}

// IsModelSwitchRetryable reports whether a failed model switch is transient and
// should be retried on the next prompt rather than reported as a hard failure.
func IsModelSwitchRetryable(err error) bool {
	mse, ok := AsModelSwitchError(err)
	return ok && mse.Retryable
}

// ApplyModel applies a model selection to this connection's live session.
//
// Exported for the click-time path (PATCH /api/ai/session/update): the picker
// wants immediate feedback instead of the selection silently deferring to the
// next message. It dispatches exactly like the pre-prompt path
// (applyModelSelection), so both agree on the mechanism and on the dedup key.
//
// Returns nil when there is nothing to do (no ACP session yet, or the model is
// already set) or when the switch succeeded. Returns *ModelSwitchError when the
// agent rejected it.
//
// Callers must NOT hold c.mu (CallRaw takes it).
func (c *ACPConn) ApplyModel(ctx context.Context, modelID string) error {
	acpSID := c.AcpSID()
	if acpSID == "" {
		// Spawned but no session yet: the persisted value is what the first
		// Prompt will apply, so there is nothing to push now.
		return nil
	}
	if err := c.applyModelSelection(ctx, acpSID, modelID); err != nil {
		return classifyModelSwitchFailure(err)
	}
	return nil
}

// classifyModelSwitchFailure wraps a raw switch failure in a *ModelSwitchError.
func classifyModelSwitchFailure(err error) error {
	if err == nil {
		return nil
	}
	var rawErr *RawRPCError
	switch {
	case errors.As(err, &rawErr):
		detail := rawRPCErrorDetail(rawErr.Data)
		if detail == "" {
			detail = rawErr.Message
		}
		// Hermes raises -32603 for BOTH "Session is busy" AND a failed rebuild
		// (a ValueError from rebuilding the agent), so the code alone cannot
		// mean "busy" — only the wording is transient.
		busy := rawErr.Code == -32603 &&
			(isBusyText(rawErr.Message) || isBusyText(detail))
		return &ModelSwitchError{Code: rawErr.Code, Detail: detail, Retryable: busy}
	case errors.Is(err, context.DeadlineExceeded):
		// A slow agent (Hermes' cold model switch does ~10s of I/O) or a wedged
		// one. Transient: keep the choice and retry at the next prompt.
		return &ModelSwitchError{Detail: "timed out", Retryable: true}
	case errors.Is(err, context.Canceled):
		// The caller went away (client disconnect) mid-RPC. Transient by the
		// same reasoning as a timeout — nothing about the request was wrong.
		return &ModelSwitchError{Detail: "canceled", Retryable: true}
	case errors.Is(err, errRawConnClosed):
		return &ModelSwitchError{Detail: "connection closed", Retryable: true}
	case isConfigKilledConnection(err):
		// The switch crashed the process. The next prompt respawns and re-applies
		// the session model, so this is retryable rather than a hard failure.
		return &ModelSwitchError{Detail: err.Error(), Retryable: true}
	default:
		return &ModelSwitchError{Detail: err.Error()}
	}
}

// isBusyText reports whether an agent message means "a turn is running".
//
// DELIBERATELY a loose substring match. The alternative (an exact match on
// Hermes' sentence) would break the moment the agent rewords it, and a false
// NEGATIVE is worse than a false positive here: misreading "busy" as hard would
// 422 the user and discard a choice that a retry would have applied. The cost of
// a false positive is a silent defer that never applies — so if you widen this,
// keep it anchored to the busy concept rather than matching arbitrary text.
func isBusyText(s string) bool {
	return strings.Contains(strings.ToLower(s), "busy")
}

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
	if c.keepUnlistedModel(value) {
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

// keepUnlistedModel reports whether value must be left alone because it is a
// deliberate pin the agent's advertised list cannot express.
//
// The agent's model list is authoritative for what it can run through ACP, but
// it is NOT the set of values a session may legitimately be pinned to. Two
// concrete cases, and the second is why the tier check exists:
//
//   - Command Code with a BYOK provider: the provider and model live in
//     ~/.commandcode/config.json, and availableModels() always returns the
//     vendor catalog, so "minimax/minimax-m2.7" is absent from all 92 advertised
//     options. Pushing an advertised model over that binding does not "switch
//     models" — it silently reroutes the next request to the vendor gateway,
//     which rejects the BYOK placeholder key with `POST /alpha/generate → 401`.
//
//   - Claude (claude-agent-acp): the adapter advertises TIER ALIASES — `opus`,
//     `sonnet`, `haiku` — while the picker and PATCH carry the CLI's concrete
//     ids (`claude-sonnet-4-6`), because model.ResolveModels aligns an alias to
//     its CLI skeleton entry and keeps the concrete id. The IDs therefore never
//     match, and an ID-equality check silently disabled model switching for the
//     whole backend (UI changed, PATCH returned OK, the next turn ran the old
//     model). The adapter's set_config_option resolves a concrete id back to its
//     tier (`resolveModelPreference`), so "the value's tier is advertised" is
//     exactly the right condition for expressibility.
//
// The rule is therefore "never overwrite a selection the agent did not offer",
// where "offer" means the value OR its tier is in the list. A value that matches
// neither either came from out-of-band configuration (leave it) or is stale (the
// next explicit user selection will replace it, and that value WILL be in the
// list).
//
// Unknown list (capabilities not extracted yet) returns false so the first turn
// still applies the stored selection — refusing there would skip the switch for
// every backend on every new session.
func (c *ACPConn) keepUnlistedModel(value string) bool {
	agentID := c.AgentID()
	if agentID == "" {
		return false
	}
	reg := GetAgentCapabilityRegistry()
	if !reg.HasModelList(agentID) {
		return false
	}
	if reg.IsModelAvailable(agentID, value) {
		return false
	}
	// Tier-alias advertised list: a concrete CLI id is still expressible when
	// the agent offers its tier.
	if tier, ok := model.TierAliasForModelID(value); ok && reg.IsModelAvailable(agentID, tier) {
		return false
	}
	slog.Info("acp conn: keeping model not offered by the agent (out-of-band pin)",
		"agent_id", agentID, "model", value,
		"clawbench_sid", c.clawbenchSID)
	return true
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
	// Serialize check → send → record. A click-time apply (PATCH) can run
	// concurrently with the pre-prompt apply on the same connection; without
	// this both would pass shouldSetConfig and send session/set_model twice. The
	// agent rejects the second with "Session is busy", and Prompt would then
	// abort the whole turn over a switch that had already succeeded.
	c.modelSwitchMu.Lock()
	defer c.modelSwitchMu.Unlock()

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
	// Same rule as applyModelSelection: a value the agent never offered is an
	// out-of-band pin (BYOK), and replaying it over the agent's own list would
	// reroute the next turn. Safe under c.mu — keepUnlistedModel only takes the
	// leaf stateMu.
	if c.keepUnlistedModel(value) {
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
