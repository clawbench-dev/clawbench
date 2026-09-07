package ai

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// ---------------------------------------------------------------------------
// ACPConn Prompt — send prompts and apply config options
// ---------------------------------------------------------------------------

// configOptionSpec describes a config option to set before a prompt.
type configOptionSpec struct {
	configID string // "model", "thinkingEffort", or "mode"
	value    string // the value from ChatRequest
	label    string // log label, e.g. "model", "thinking_effort", "mode"
}

// Prompt sends a prompt on the ACP session and forwards events to streamCh.
func (c *ACPConn) Prompt(ctx context.Context, prompt []acp.ContentBlock, streamCh chan<- StreamEvent, req ChatRequest) error {
	promptTotalStart := time.Now()
	defer func() {
		slog.Info("acp perf: Prompt.total", "clawbench_sid", c.clawbenchSID, "elapsed", time.Since(promptTotalStart))
	}()

	// If a LoadSession replay (sync or acp-load) is in progress, wait for it to
	// finish before sending this prompt. Otherwise loadSessionActive would hijack
	// this prompt's SessionUpdate notifications into the replay buffer instead of
	// routing them to the live stream (streamCh), so the user's reply would never
	// surface. Wait, then proceed — do not corrupt the ongoing replay.
	if err := c.waitForLoadSessionDone(); err != nil {
		return err
	}

	// Clear stale plan state from the previous turn
	c.mu.Lock()
	c.cachedPlanState = nil
	c.mu.Unlock()

	// Reset raw output buffer for this turn. Raw ACP notification payloads
	// are accumulated directly on the ACPConn (not through the channel) to
	// avoid consuming channel buffer space that would cause content events
	// to be dropped. The buffer is flushed to the channel as a single
	// raw_output event after Prompt returns.
	c.ResetRawOutput()

	// Reset the per-turn _meta extension accumulator so stale metadata from a
	// previous turn cannot leak into this one's message-level metadata.
	c.getAndClearMetaAccum()

	c.mu.Lock()
	client := c.client
	conn := c.conn
	acpSID := c.acpSID
	c.lastUsed = time.Now()
	c.mu.Unlock()

	// Reset the stall baseline and in-flight tool state for this turn so the
	// watchdog counts progress from now, and a leftover in-flight tool from a
	// previous turn can't suppress it.
	c.TouchSessionUpdate()
	c.SetToolInFlight(false)

	if conn == nil || acpSID == "" {
		return fmt.Errorf("acp: connection not initialized")
	}

	// Register the stream channel so SessionUpdate callbacks are forwarded
	if client != nil {
		slog.Info("acp conn: RegisterSession starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
		client.RegisterSession(acpSID, streamCh)
		defer client.UnregisterSession(acpSID)
	}

	// Apply config options: model, thinkingEffort, mode
	// Each is skipped if unchanged or unsupported; if a config kills the connection,
	// we return a configKilledConnectionError so the caller can retry.
	// Config ids are resolved to what the agent itself advertised
	// (resolveWireConfigID) with the historical hardcoded ids as fallback —
	// agents differ (claude-agent-acp: "effort" vs legacy "thinkingEffort").
	configs := []configOptionSpec{
		{configID: c.resolveWireConfigID("model", "model"), value: req.Model, label: "model"},
		{configID: c.resolveWireConfigID("thought_level", "thinkingEffort"), value: req.ThinkingEffort, label: "thinking_effort"},
		{configID: c.resolveWireConfigID("mode", "mode"), value: req.Mode, label: "mode"},
	}
	for _, cfg := range configs {
		if cfg.value == "" {
			continue
		}
		if err := c.setConfigOptionWithCrashCheck(ctx, acpSID, cfg); err != nil {
			return err
		}
	}

	// Send prompt. DO NOT add a hard timeout here — see acp_pool.go for rationale.
	// Create a derived context that can be cancelled when the process dies,
	// so conn.Prompt doesn't hang indefinitely if the agent is killed.
	promptCtx, promptCancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.promptCancel = promptCancel
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.promptCancel = nil
		c.mu.Unlock()
		promptCancel()
	}()

	// No-progress watchdog: conn.Prompt has no hard timeout (the agent process
	// is expected to send SessionUpdates), and the idle sweep skips running
	// sessions, so a hung-but-alive agent would otherwise block the session
	// forever. If the prompt makes no progress (no SessionUpdate and no
	// in-flight tool) for the stall window, cancel the prompt and kill the
	// agent process so the connection is not reused in its stuck state.
	stopWatchdog := c.startStallWatchdog(promptCtx, func() {
		promptCancel()
		c.killAndMarkDead() // kill process but preserve acpSID for recovery
	})
	defer stopWatchdog()

	promptStart := time.Now()
	slog.Info("acp conn: conn.Prompt starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	resp, err := conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: acp.SessionId(acpSID),
		Prompt:    prompt,
	})
	slog.Info("acp conn: conn.Prompt done", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "elapsed", time.Since(promptStart), "error", err)

	// Flush accumulated raw ACP notification payloads to the channel as a
	// single raw_output event. This is read by SessionExecutor to persist
	// to ai_raw_responses for debugging. Previously, each ACP notification
	// sent a separate raw_output event through the channel, which consumed
	// channel buffer space and caused content events to be dropped.
	// Flush on both success and error paths so partial output is preserved.
	if rawOutput := c.ResetRawOutput(); rawOutput != "" {
		forwardACPEvent(streamCh, StreamEvent{Type: "raw_output", RawOutput: rawOutput})
	}

	if err != nil {
		if ctx.Err() != nil {
			slog.Info("acp conn: prompt cancelled", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
			c.handlePromptCancel(conn)
			return ctx.Err()
		}
		return c.classifyPromptError(err, conn, acpSID)
	}

	c.emitRefusalWarningIfRefused(resp, streamCh, acpSID)
	c.emitPromptTailMetadata(resp, streamCh)

	return nil
}

// emitRefusalWarningIfRefused surfaces a structured warning event when the
// agent refused the prompt (stopReason="refusal" — model unresolvable, upstream
// error, or declined request). The agent streams NO content blocks for a
// refusal, so without this check the executor would classify the turn as
// "AI returned no content" (reason=empty) — a misleading dead-end that hides
// the real cause and offers no actionable error code.
func (c *ACPConn) emitRefusalWarningIfRefused(resp acp.PromptResponse, streamCh chan<- StreamEvent, acpSID string) {
	if resp.StopReason != acp.StopReasonRefusal {
		return
	}
	slog.Warn("acp conn: prompt refused by agent",
		"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID,
		"stop_reason", resp.StopReason)
	httpStatus := acpHTTPStatusFromMeta(resp.Meta)
	forwardACPEvent(streamCh, StreamEvent{
		Type:        "warning",
		Content:     "AI request refused by the agent (model unavailable or upstream error)",
		Reason:      ReasonRefused,
		ErrorCode:   -32603, // JSON-RPC internal error (matches CodeBuddy refusal rpcCode)
		HTTPStatus:  httpStatus,
		ErrorSource: "agent",
	})
}

// emitPromptTailMetadata emits the turn-final metadata event from a successful
// PromptResponse. PromptResponse.Usage (UNSTABLE) carries token counts and the
// ACP-standard stopReason (Claude/Codex report it here, e.g. "end_turn"), so
// when usage or _meta extensions are present the full metadata + usage_update
// pair is emitted. Without PromptResponse usage/meta, the accumulated per-agent
// _meta from session/update notifications (or the bare stopReason) is still
// persisted so the message record reflects why the turn ended.
func (c *ACPConn) emitPromptTailMetadata(resp acp.PromptResponse, streamCh chan<- StreamEvent) {
	if resp.Usage != nil || len(resp.Meta) > 0 {
		c.emitPromptResponseUsage(resp.Usage, resp.Meta, resp.StopReason, streamCh)
		return
	}
	if acc := c.getAndClearMetaAccum(); acc != nil {
		// No PromptResponse usage/meta, but the turn accumulated per-agent _meta
		// extensions from session/update notifications (e.g. CodeBuddy usage).
		// Persist them so the message-level metadata reflects the turn.
		meta := &Metadata{}
		applyMetaExtractionToMetadata(meta, acc)
		if resp.StopReason != "" {
			meta.StopReason = string(resp.StopReason)
		}
		// Record this turn's requestId as the completed-turn baseline for the
		// replayed-chunk filter in mapACPSessionUpdate (see lastCompletedRequestID).
		c.setLastCompletedRequestID(meta.RequestID)
		forwardACPEvent(streamCh, StreamEvent{Type: "metadata", Meta: meta})
		return
	}
	if resp.StopReason != "" {
		// No usage/meta at all (rare), but still persist the stop reason so the
		// message record is complete.
		forwardACPEvent(streamCh, StreamEvent{Type: "metadata", Meta: &Metadata{
			StopReason: string(resp.StopReason),
		}})
	}
}

// classifyPromptError maps a conn.Prompt error to the appropriate wrapped
// error. When the agent process is dead the crash diagnostics are collected
// and appended so the caller can surface exit code/signal/stderr; a stale
// prompt is prevented from killing a respawned connection via
// markDeadIfCurrent (only clears alive if still the active connection).
func (c *ACPConn) classifyPromptError(err error, conn *acp.ClientSideConnection, acpSID string) error {
	if !c.IsAlive() {
		diag := c.collectCrashDiagnostics()
		c.markDeadIfCurrent(conn)

		slog.Error("acp conn: prompt failed (peer disconnected)",
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID,
			"exit_code", diag.ExitCode, "signal", diag.Signal,
			"ppid", diag.ParentPID, "rss_mb", diag.VMRSSKB/1024, "fds", diag.FDCount,
			"stderr_tail", diag.StderrTail)

		return fmt.Errorf("acp: prompt: %w%s", err, diag.String())
	}

	slog.Warn("acp conn: prompt failed but agent still alive",
		"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "error", err)
	return fmt.Errorf("acp: prompt: %w", err)
}

// acpHTTPStatusFromMeta scans a PromptResponse._meta map for an upstream HTTP
// status. Some agents embed it (e.g. {"httpStatus":500}); returns 0 when absent.
func acpHTTPStatusFromMeta(meta map[string]any) int {
	if meta == nil {
		return 0
	}
	for _, key := range []string{"httpStatus", "http_status", "status"} {
		if v, ok := meta[key]; ok {
			if f, ok := v.(float64); ok {
				return int(f)
			}
			if i, ok := v.(int); ok {
				return i
			}
		}
	}
	// Nested _meta.data / _meta.error payloads.
	if data, ok := meta["data"].(map[string]any); ok {
		return acpHTTPStatusFromMeta(data)
	}
	if data, ok := meta["error"].(map[string]any); ok {
		return acpHTTPStatusFromMeta(data)
	}
	return 0
}

// emitPromptResponseUsage emits metadata and usage_update events from a
// PromptResponse.Usage (UNSTABLE ACP feature), the ACP-standard stopReason,
// and PromptResponse._meta extensions. The metadata event ensures
// InputTokens/OutputTokens (and any per-agent _meta detail) are persisted to
// chat_metadata and embedded in chat_history.content JSON. The usage_update
// event updates the frontend's context usage chip in real time.
//
// respMeta carries the per-agent _meta extensions from the PromptResponse
// (Claude/Codex quota.token_count, CodeBuddy codebuddy.ai/* trace). The
// accumulated turn-level _meta (from session/update notifications) is merged
// in as well so the metadata event reflects the richest observed values.
// stopReason is the ACP-standard PromptResponse.stopReason (Claude/Codex
// report it here, e.g. "end_turn"); it is persisted on the metadata record.
func (c *ACPConn) emitPromptResponseUsage(usage *acp.Usage, respMeta map[string]any, stopReason acp.StopReason, streamCh chan<- StreamEvent) {
	backendID := c.BackendID()

	// Start from PromptResponse.Usage. usage may be nil when only _meta
	// extensions are present (e.g. CodeBuddy reports no PromptResponse.Usage).
	// Capture the turn-level accumulation once and reuse it for both the
	// metadata event and the usage_update payload.
	meta := &Metadata{}
	if usage != nil {
		meta.InputTokens = usage.InputTokens
		meta.OutputTokens = usage.OutputTokens
	}

	// The PromptResponse carries no cost, but agents like Claude report it on
	// the final usage_update of the turn (e.g. cost=0.1568 USD) which reached
	// cachedUsageState. Persist that to the message metadata so
	// chat_metadata.cost_usd is populated for ACP agents too — otherwise only
	// CLI stream parsers ever set CostUSD and ACP rows stay 0.
	c.mu.Lock()
	cachedUsage := c.cachedUsageState
	c.mu.Unlock()
	if cachedUsage != nil && cachedUsage.Cost > 0 {
		meta.CostUSD = cachedUsage.Cost
	}

	// Persist the ACP-standard stop reason (Claude/Codex report it here, not in
	// _meta) so the message record reflects why the turn ended.
	if stopReason != "" {
		meta.StopReason = string(stopReason)
	}
	acc := c.getAndClearMetaAccum()
	// Apply per-agent _meta extensions from the PromptResponse (Claude/Codex
	// quota, CodeBuddy trace) and the turn-level accumulation (CodeBuddy usage
	// from session/update notifications).
	if ext := extractMetaUsage(backendID, respMeta); ext != nil {
		applyMetaExtractionToMetadata(meta, ext)
	}
	if acc != nil {
		applyMetaExtractionToMetadata(meta, acc)
	}
	// Record this turn's requestId as the completed-turn baseline for the
	// replayed-chunk filter in mapACPSessionUpdate (see lastCompletedRequestID).
	// meta.RequestID is the genuine turn requestId here because the filter has
	// already kept any stale previous-turn chunk from polluting metaAccum.
	c.setLastCompletedRequestID(meta.RequestID)

	slog.Info("acp conn: PromptResponse.Usage",
		"clawbench_sid", c.clawbenchSID,
		"has_usage", usage != nil,
		"meta_input_tokens", meta.InputTokens,
		"meta_output_tokens", meta.OutputTokens,
		"meta_total_tokens", meta.TotalTokens,
		"meta_cached_read_tokens", meta.CachedReadTokens,
		"meta_cached_write_tokens", meta.CachedWriteTokens,
		"meta_thought_tokens", meta.ThoughtTokens)

	// Emit metadata event for persistence (SessionExecutor captures these)
	forwardACPEvent(streamCh, StreamEvent{Type: "metadata", Meta: meta})

	// Also update UsageState so the context chip shows input/output tokens.
	// cachedUsageState may be nil on the first prompt that returns a Usage
	// before any UsageUpdate notification (UNSTABLE feature) — fall back to
	// zero values to avoid a nil pointer dereference. cachedUsage was already
	// read above when building the metadata event; reuse it here.
	cached := cachedUsage
	var used, size int
	var cost float64
	var currency string
	if cached != nil {
		used = cached.Used
		size = cached.Size
		cost = cached.Cost
		currency = cached.Currency
	}
	usageState := &UsageState{
		Used:     used,
		Size:     size,
		Cost:     cost,
		Currency: currency,
	}
	if usage != nil {
		usageState.InputTokens = usage.InputTokens
		usageState.OutputTokens = usage.OutputTokens
		usageState.TotalTokens = usage.TotalTokens
		usageState.CachedReadTokens = ptrIntVal(usage.CachedReadTokens)
		usageState.CachedWriteTokens = ptrIntVal(usage.CachedWriteTokens)
		usageState.ThoughtTokens = ptrIntVal(usage.ThoughtTokens)
	}
	// Merge per-agent _meta usage detail (e.g. Claude/Codex quota, CodeBuddy
	// credit) into the usage_update payload so the frontend sees it live.
	if ext := extractMetaUsage(backendID, respMeta); ext != nil {
		applyMetaExtractionToUsageState(usageState, ext)
	}
	if acc != nil {
		applyMetaExtractionToUsageState(usageState, acc)
	}
	// Fallback for the context chip: when no usable window numbers are cached
	// (cachedUsageState nil on the first prompt), backfill used from this turn's
	// usageByCategory breakdown — CodeBuddy's authoritative context occupancy —
	// before forwarding. InputTokens is deliberately NOT used as a fallback: it
	// is the session-cumulative input (per ACP spec), not the current context
	// occupancy, so it can exceed the window and would corrupt the used/size
	// ratio the frontend renders. A size stays 0 until a real notification
	// reports the window (the frontend drops size=0 events, so a fresh session
	// simply shows no context chip until then).
	if usageState.Used <= 0 {
		if sum := sumCategory(usageState.UsageByCategory); sum > 0 {
			usageState.Used = int(sum)
		}
	}
	forwardACPEvent(streamCh, StreamEvent{Type: "usage_update", Usage: usageState})
	// Merge, don't overwrite (same rationale as mapACPSessionUpdate): a turn
	// whose cached window was regressed to 0 must not wipe a previously-known
	// non-zero window still held by the connection.
	c.SetCachedUsageState(MergeUsageState(c.GetCachedUsageState(), usageState))
}

// ptrIntVal dereferences a *int, returning 0 for nil.
func ptrIntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// setConfigOptionWithCrashCheck sets a config option, checking whether it killed
// the connection. Returns a configKilledConnectionError if the connection died
// after the call, so the caller can skip that config on retry.
func (c *ACPConn) setConfigOptionWithCrashCheck(ctx context.Context, acpSID string, cfg configOptionSpec) error {
	if !c.shouldSetConfig(cfg.configID, cfg.value) {
		slog.Debug("acp conn: set_config_option skipped (unchanged)",
			"config_id", cfg.configID, "value", cfg.value,
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
		return nil
	}

	configStart := time.Now()
	slog.Info("acp conn: set_config_option starting",
		"config_id", cfg.configID, "label", cfg.label, "value", cfg.value,
		"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	c.setSessionConfigOption(ctx, acpSID, cfg.configID, cfg.value)
	slog.Info("acp conn: set_config_option done",
		"config_id", cfg.configID, "label", cfg.label, "value", cfg.value,
		"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "elapsed", time.Since(configStart))

	if !c.IsAlive() {
		diag := c.collectCrashDiagnostics()
		slog.Error("acp conn: set_config_option killed connection",
			"config_id", cfg.configID, "label", cfg.label, "value", cfg.value,
			"clawbench_sid", c.clawbenchSID, "acp_sid", acpSID,
			"exit_code", diag.ExitCode, "signal", diag.Signal,
			"ppid", diag.ParentPID, "rss_mb", diag.VMRSSKB/1024, "fds", diag.FDCount,
			"stderr_tail", diag.StderrTail)
		return errConfigKilledConnectionWithDiag(cfg.configID, cfg.value, diag)
	}

	c.markConfigSet(cfg.configID, cfg.value)

	// For mode: also update cache so GET /api/ai/chat returns the correct mode.
	if cfg.configID == "mode" && !c.IsConfigUnsupported("mode") {
		c.UpdateCachedCurrent("mode", cfg.value)
	}

	return nil
}

// loadWaitTimeout bounds how long Prompt will wait for a LoadSession replay to
// finish before failing. Package-level so tests can shrink it.
var loadWaitTimeout = 10 * time.Second

// waitForLoadSessionDone blocks until loadSessionActive is cleared (a LoadSession
// replay finished), up to loadWaitTimeout. It prevents a user prompt's
// notifications from being hijacked into the replay buffer during sync/acp-load.
func (c *ACPConn) waitForLoadSessionDone() error {
	deadline := time.Now().Add(loadWaitTimeout)
	for c.loadSessionActive.Load() {
		if time.Now().After(deadline) {
			return fmt.Errorf("acp: session is still loading (LoadSession replay in progress), try again shortly")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// handlePromptCancel processes a user-initiated prompt cancellation.
// Only marks the connection dead if the ACP process has actually died
// (conn.Done() is closed). When the process is still alive, the connection
// is preserved so the next Prompt can reuse it directly without a slow
// kill+respawn+LoadSession cycle that can timeout (60s).
func (c *ACPConn) handlePromptCancel(conn *acp.ClientSideConnection) {
	c.mu.Lock()
	if !c.isAliveLocked() {
		c.mu.Unlock()
		c.markDeadIfCurrent(conn)
	} else {
		c.mu.Unlock()
		slog.Info("acp conn: prompt cancelled but process still alive, keeping connection",
			"clawbench_sid", c.clawbenchSID, "acp_sid", c.acpSID)
	}
}
