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

	promptStart := time.Now()
	slog.Info("acp conn: conn.Prompt starting", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
	_, err := conn.Prompt(promptCtx, acp.PromptRequest{
		SessionId: acp.SessionId(acpSID),
		Prompt:    prompt,
	})
	slog.Info("acp conn: conn.Prompt done", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID, "elapsed", time.Since(promptStart), "error", err)
	if err != nil {
		if ctx.Err() != nil {
			slog.Info("acp conn: prompt cancelled", "clawbench_sid", c.clawbenchSID, "acp_sid", acpSID)
			c.mu.Lock()
			c.alive = false
			c.mu.Unlock()
			return ctx.Err()
		}

		if !c.IsAlive() {
			diag := c.collectCrashDiagnostics()
			c.mu.Lock()
			c.alive = false
			c.mu.Unlock()

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

	return nil
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
		c.UpdateCachedCurrentMode(cfg.value)
	}

	return nil
}
