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
	configs := []configOptionSpec{
		{configID: "model", value: req.Model, label: "model"},
		{configID: "thinkingEffort", value: req.ThinkingEffort, label: "thinking_effort"},
		{configID: "mode", value: req.Mode, label: "mode"},
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
		c.close()
	})
	defer stopWatchdog()

	promptStart := time.Now()
	slog.Info("acp conn: conn.Prompt starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	resp, err := conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: acp.SessionId(acpSID),
		Prompt:    prompt,
	})
	slog.Info("acp conn: conn.Prompt done", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "elapsed", time.Since(promptStart), "error", err)
	if err != nil {
		if ctx.Err() != nil {
			slog.Info("acp conn: prompt cancelled", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
			// Only mark this connection dead if it's still the active one. A
			// concurrent respawn may have replaced it (and set alive=true) while
			// this prompt was in flight; we must not clobber that state.
			c.markDeadIfCurrent(conn)
			return ctx.Err()
		}

		if !c.IsAlive() {
			diag := c.collectCrashDiagnostics()
			// See markDeadIfCurrent: only clear alive if this is still the
			// active connection, so a stale prompt can't kill a respawned one.
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

	// PromptResponse.Usage (UNSTABLE): emit metadata and usage_update events
	// so input/output token counts are persisted and shown in the context chip.
	if resp.Usage != nil {
		c.emitPromptResponseUsage(resp.Usage, streamCh)
	}

	return nil
}

// emitPromptResponseUsage emits metadata and usage_update events from a
// PromptResponse.Usage (UNSTABLE ACP feature). The metadata event ensures
// InputTokens/OutputTokens are persisted to chat_metadata and embedded in
// chat_history.content JSON. The usage_update event updates the frontend's
// context usage chip in real time.
func (c *ACPConn) emitPromptResponseUsage(usage *acp.Usage, streamCh chan<- StreamEvent) {
	meta := &Metadata{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
	}
	slog.Info("acp conn: PromptResponse.Usage",
		"clawbench_sid", c.clawbenchSID,
		"input_tokens", usage.InputTokens,
		"output_tokens", usage.OutputTokens,
		"total_tokens", usage.TotalTokens,
		"cached_read_tokens", usage.CachedReadTokens,
		"cached_write_tokens", usage.CachedWriteTokens,
		"thought_tokens", usage.ThoughtTokens)

	// Emit metadata event for persistence (SessionExecutor captures these)
	forwardACPEvent(streamCh, StreamEvent{Type: "metadata", Meta: meta})

	// Also update UsageState so the context chip shows input/output tokens.
	// cachedUsageState may be nil on the first prompt that returns a Usage
	// before any UsageUpdate notification (UNSTABLE feature) — fall back to
	// zero values to avoid a nil pointer dereference.
	c.mu.Lock()
	cached := c.cachedUsageState
	c.mu.Unlock()
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
		Used:              used,
		Size:              size,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		TotalTokens:       usage.TotalTokens,
		CachedReadTokens:  ptrIntVal(usage.CachedReadTokens),
		CachedWriteTokens: ptrIntVal(usage.CachedWriteTokens),
		ThoughtTokens:     ptrIntVal(usage.ThoughtTokens),
		Cost:              cost,
		Currency:          currency,
	}
	forwardACPEvent(streamCh, StreamEvent{Type: "usage_update", Usage: usageState})
	c.SetCachedUsageState(usageState)
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
