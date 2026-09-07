//go:build integration

package ai

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Claude ACP Latency Profiling Integration Test
// ---------------------------------------------------------------------------
//
// This test directly exercises the low-level ACP protocol (spawn → initialize →
// new session → set_config_option → prompt) for Claude's bridge adapter
// (`npx -y @agentclientprotocol/claude-agent-acp@latest`), measuring the
// duration of each phase to identify why Claude responses feel slow.
//
// Potential bottleneck areas:
//   1. npx package resolution / download (`@latest` may hit network every run)
//   2. Bridge adapter startup (Node.js boot + spawning claude CLI)
//   3. ACP Initialize handshake
//   4. ACP NewSession creation
//   5. set_config_option RPCs (model, thinkingEffort, mode) — sent sequentially
//   6. Prompt round-trip (time from Prompt() call to first content chunk)
//   7. Total end-to-end latency (from ExecuteStream call to "done" event)

// claudeACPAgent returns a model.Agent configured for Claude ACP transport.
// Model and thinking levels match what the bridge adapter reports via
// NewSession ConfigOptions (claude-sonnet-4-6, thought_level category).
func claudeACPAgent() *model.Agent {
	return &model.Agent{
		ID:         "claude-acp-latency-test",
		Name:       "Claude ACP Latency Test",
		Backend:    "claude",
		Transport:  "acp-stdio",
		AcpCommand: "npx -y @agentclientprotocol/claude-agent-acp@latest",
		Models: []model.AgentModel{
			{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4", Default: true},
		},
		ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
	}
}

// requireClaudeACPAvailable skips the test if Claude CLI and npx are not installed.
func requireClaudeACPAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx not available, skipping Claude ACP latency test")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not available, skipping Claude ACP latency test")
	}
}

// ---------------------------------------------------------------------------
// Test: Phase-by-phase latency profiling (low-level ACP protocol)
// ---------------------------------------------------------------------------

// TestClaudeACP_LatencyProfile directly calls the low-level ACP protocol
// methods (spawn, Initialize, NewSession, SetSessionConfigOption, Prompt)
// and measures the duration of each phase. This isolates the bottleneck
// without ClawBench's higher-level orchestration (event routing, SSE, etc.).
//
// Run with verbose output to see per-phase timings:
//
//	go test -v -run TestClaudeACP_LatencyProfile -tags integration -timeout 300s
func TestClaudeACP_LatencyProfile(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	_ = setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	// ── Phase 1: GetOrCreateConn (spawn + Initialize + NewSession) ──
	t.Log("=== Phase 1: GetOrCreateConn (spawn + Initialize + NewSession) ===")
	phase1Start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	mgr := GetACPConnManager()
	conn, isNew, err := mgr.GetOrCreateConn(ctx, agent, sessionID, acpTestWorkDir())
	phase1Elapsed := time.Since(phase1Start)
	require.NoError(t, err, "GetOrCreateConn failed")
	require.True(t, isNew, "expected new session")
	t.Logf("Phase 1 (GetOrCreateConn): %v", phase1Elapsed)

	// ── Phase 1a: Check if npx resolution was slow ──
	// The spawn time is dominated by `npx -y @agentclientprotocol/claude-agent-acp@latest`.
	// If the package is cached locally, npx resolves quickly (<2s).
	// If not cached or @latest forces a fresh lookup, it can take 5-15s.
	t.Logf("  Agent process PID: %d", conn.ProcessPID())
	t.Logf("  ACP session ID: %s", conn.AcpSID())

	// ── Phase 2: SetSessionConfigOption — model ──
	t.Log("=== Phase 2: SetSessionConfigOption(model) ===")
	phase2Start := time.Now()
	conn.SetSessionConfigOption(ctx, "model", "claude-sonnet-4-6")
	phase2Elapsed := time.Since(phase2Start)
	t.Logf("Phase 2 (set_config model): %v", phase2Elapsed)
	// Note: Claude bridge adapter may restart its internal subprocess on model change.
	// This can take 5-30s if the bridge restarts the claude CLI process.

	// ── Phase 3: SetSessionConfigOption — effort ──
	// Claude bridge adapter reports ConfigOption id="effort" (category=thought_level).
	t.Log("=== Phase 3: SetSessionConfigOption(effort) ===")
	phase3Start := time.Now()
	conn.SetSessionConfigOption(ctx, "effort", "low")
	phase3Elapsed := time.Since(phase3Start)
	t.Logf("Phase 3 (set_config effort): %v", phase3Elapsed)

	// ── Phase 4: SetSessionConfigOption — mode ──
	// Claude bridge adapter default mode is "bypassPermissions", not "code".
	t.Log("=== Phase 4: SetSessionConfigOption(mode) ===")
	phase4Start := time.Now()
	conn.SetSessionConfigOption(ctx, "mode", "bypassPermissions")
	phase4Elapsed := time.Since(phase4Start)
	t.Logf("Phase 4 (set_config mode): %v", phase4Elapsed)

	// ── Phase 5: Prompt — measure time to first content chunk ──
	t.Log("=== Phase 5: Prompt (measure time to first content and total) ===")

	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "回复一个字：好",
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
		// Model and thinking already set on conn — dedup should skip re-sending
	})
	require.NoError(t, err, "ExecuteStream failed")

	var firstContentTime time.Duration
	var doneTime time.Duration
	promptStart := time.Now()
	var gotFirstContent bool
	var contentEvents []StreamEvent

	for event := range ch {
		elapsed := time.Since(promptStart)
		switch event.Type {
		case "content":
			if !gotFirstContent {
				firstContentTime = elapsed
				gotFirstContent = true
				t.Logf("  First content chunk at: %v", firstContentTime)
			}
			contentEvents = append(contentEvents, event)
		case "done":
			doneTime = elapsed
		}
	}

	if !gotFirstContent {
		t.Error("No content events received!")
	} else {
		t.Logf("Phase 5 (time to first content): %v", firstContentTime)
		t.Logf("Phase 5 (total prompt duration): %v", doneTime)
	}

	// ── Summary ──
	t.Log("=== Latency Summary ===")
	t.Logf("  Phase 1 — GetOrCreateConn (spawn+init+new):  %v", phase1Elapsed)
	t.Logf("  Phase 2 — set_config model:                  %v", phase2Elapsed)
	t.Logf("  Phase 3 — set_config effort:                %v", phase3Elapsed)
	t.Logf("  Phase 4 — set_config mode:                   %v", phase4Elapsed)
	t.Logf("  Phase 5 — time to first content:             %v", firstContentTime)
	t.Logf("  Phase 5 — total prompt duration:             %v", doneTime)
	configTotal := phase2Elapsed + phase3Elapsed + phase4Elapsed
	t.Logf("  TOTAL config options overhead:               %v", configTotal)
	total := phase1Elapsed + configTotal + doneTime
	t.Logf("  TOTAL end-to-end:                            %v", total)

	// ── Bottleneck detection ──
	// Flag phases that take suspiciously long
	if phase1Elapsed > 30*time.Second {
		t.Logf("BOTTLENECK: Phase 1 (GetOrCreateConn) took %v — likely npx package resolution or bridge adapter startup", phase1Elapsed)
	}
	if phase2Elapsed > 10*time.Second {
		t.Logf("BOTTLENECK: Phase 2 (set_config model) took %v — Claude bridge may restart subprocess on model change", phase2Elapsed)
	}
	if configTotal > 15*time.Second {
		t.Logf("BOTTLENECK: Total config options took %v — consider sending config options concurrently or skipping unchanged values", configTotal)
	}
	if firstContentTime > 20*time.Second {
		t.Logf("BOTTLENECK: Time to first content %v — model inference startup or context processing is slow", firstContentTime)
	}
}

// ---------------------------------------------------------------------------
// Test: Full ExecuteStream end-to-end latency (high-level)
// ---------------------------------------------------------------------------

// TestClaudeACP_ExecuteStreamE2E measures end-to-end latency through the
// full ExecuteStream pipeline (same path as a real chat request), breaking
// down where time is spent based on logged event types.
func TestClaudeACP_ExecuteStreamE2E(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	_ = env

	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	// ── First prompt (cold start: spawn + init + new session) ──
	t.Log("=== First prompt (cold start) ===")
	coldStart := time.Now()
	events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
	coldElapsed := time.Since(coldStart)
	requireDoneEvent(t, events1)

	content1 := concatACPContent(events1)
	assert.NotEmpty(t, content1, "should receive content on first prompt")
	t.Logf("Cold start (first prompt): %v", coldElapsed)

	// Log event type timeline
	logACPTimeline(t, events1, coldStart)

	// ── Second prompt (warm: connection reuse, no respawn) ──
	t.Log("=== Second prompt (warm reuse) ===")
	warmStart := time.Now()
	events2 := sendACPPrompt(t, backend, sessionID, "再回复一个字：棒", 120*time.Second)
	warmElapsed := time.Since(warmStart)
	requireDoneEvent(t, events2)

	content2 := concatACPContent(events2)
	assert.NotEmpty(t, content2, "should receive content on second prompt")
	t.Logf("Warm reuse (second prompt): %v", warmElapsed)

	logACPTimeline(t, events2, warmStart)

	// ── Compare cold vs warm ──
	t.Logf("Cold start overhead: %v (vs warm: %v, delta: %v)",
		coldElapsed, warmElapsed, coldElapsed-warmElapsed)

	if coldElapsed > 2*warmElapsed {
		t.Logf("NOTE: Cold start is %.1fx slower than warm — connection setup (npx+init+newSession) is a major contributor",
			float64(coldElapsed)/float64(warmElapsed))
	}
}

// ---------------------------------------------------------------------------
// Test: Config option overhead — sequential vs skipped
// ---------------------------------------------------------------------------

// TestClaudeACP_ConfigOverhead measures how much time set_config_option
// adds before each prompt. On subsequent prompts with unchanged config,
// the dedup logic should skip the RPCs entirely.
func TestClaudeACP_ConfigOverhead(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	_ = env

	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	// First prompt — config options will be sent (no dedup yet)
	t.Log("=== First prompt (config sent) ===")
	prompt1Start := time.Now()
	events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
	prompt1Elapsed := time.Since(prompt1Start)
	requireDoneEvent(t, events1)

	// Second prompt — same config, should be deduped
	t.Log("=== Second prompt (config deduped) ===")
	prompt2Start := time.Now()
	events2 := sendACPPrompt(t, backend, sessionID, "再回复一个字：棒", 120*time.Second)
	prompt2Elapsed := time.Since(prompt2Start)
	requireDoneEvent(t, events2)

	t.Logf("Prompt 1 (with config): %v", prompt1Elapsed)
	t.Logf("Prompt 2 (config deduped): %v", prompt2Elapsed)

	// If prompt2 is significantly faster, config overhead is the bottleneck
	if prompt1Elapsed > prompt2Elapsed+5*time.Second {
		t.Logf("Config overhead estimated: %v (prompt1 - prompt2 delta)",
			prompt1Elapsed-prompt2Elapsed)
	}
}

// ---------------------------------------------------------------------------
// Test: Reconnect latency (connection alive, prompt reuse)
// ---------------------------------------------------------------------------

// TestClaudeACP_ReconnectLatency measures how quickly a second prompt
// is processed when the connection is already alive and the session is
// reused. This simulates the common case of sending a follow-up message.
func TestClaudeACP_ReconnectLatency(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	env := setupACPTestEnvForAgent(t, agent)
	_ = env

	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := uuid.New().String()
	cleanupConn(t, sessionID)

	// Establish connection with first prompt
	events1 := sendACPPrompt(t, backend, sessionID, "回复一个字：好", 120*time.Second)
	requireDoneEvent(t, events1)

	// Verify connection is alive
	conn := env.mgr.GetConn(sessionID)
	require.NotNil(t, conn)
	require.True(t, conn.IsAlive(), "connection should be alive after first prompt")

	// Now measure how fast a second prompt starts streaming
	t.Log("=== Measuring reconnect prompt latency ===")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	promptStart := time.Now()
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "再回复一个字：棒",
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
	})
	require.NoError(t, err)

	var firstEventTime time.Duration
	var firstContentTime time.Duration
	var gotFirstEvent, gotFirstContent bool

	for event := range ch {
		elapsed := time.Since(promptStart)
		if !gotFirstEvent {
			firstEventTime = elapsed
			gotFirstEvent = true
			t.Logf("  First SSE event at: %v (type: %s)", firstEventTime, event.Type)
		}
		if event.Type == "content" && !gotFirstContent {
			firstContentTime = elapsed
			gotFirstContent = true
			t.Logf("  First content at:   %v", firstContentTime)
		}
		if event.Type == "done" {
			t.Logf("  Done at:            %v", elapsed)
			break
		}
	}

	if !gotFirstContent {
		t.Error("No content received on reconnect prompt")
	}

	// On a reused connection, first event should be fast (<5s)
	// because there's no spawn/init/newSession overhead.
	if firstEventTime > 5*time.Second {
		t.Logf("SLOW: First SSE event took %v on reused connection — config dedup may not be working, or set_config_option is slow", firstEventTime)
	}
}

// ---------------------------------------------------------------------------
// Test: NPX cache check — is npx downloading the package every time?
// ---------------------------------------------------------------------------

// TestClaudeACP_NPXCacheCheck checks whether npx has the claude-agent-acp
// package cached locally or needs to download it. If @latest forces a
// network hit every spawn, this adds 5-15s of latency.
func TestClaudeACP_NPXCacheCheck(t *testing.T) {
	requireClaudeACPAvailable(t)

	// Check npx cache
	t.Log("=== Checking npx package cache for @agentclientprotocol/claude-agent-acp ===")

	// Run `npx -y @agentclientprotocol/claude-agent-acp@latest --version` twice
	// and compare times. If the second run is much faster, the first was
	// downloading the package.
	firstStart := time.Now()
	ctx1, cancel1 := context.WithTimeout(context.Background(), 60*time.Second)
	cmd1 := exec.CommandContext(ctx1, "npx", "-y", "@agentclientprotocol/claude-agent-acp@latest", "--version")
	output1, err1 := cmd1.CombinedOutput()
	firstElapsed := time.Since(firstStart)
	cancel1()

	if err1 != nil {
		t.Logf("First npx run failed (error: %v, output: %s) — package may not be cached", err1, truncate(string(output1), 200))
	} else {
		t.Logf("First npx run: %v (output: %s)", firstElapsed, truncate(string(output1), 100))
	}

	// Second run — should be cached
	secondStart := time.Now()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	cmd2 := exec.CommandContext(ctx2, "npx", "-y", "@agentclientprotocol/claude-agent-acp@latest", "--version")
	output2, err2 := cmd2.CombinedOutput()
	secondElapsed := time.Since(secondStart)
	cancel2()

	if err2 != nil {
		t.Logf("Second npx run failed (error: %v, output: %s)", err2, truncate(string(output2), 200))
	} else {
		t.Logf("Second npx run: %v (output: %s)", secondElapsed, truncate(string(output2), 100))
	}

	if firstElapsed > 10*time.Second && secondElapsed < 5*time.Second {
		t.Logf("BOTTLENECK DETECTED: npx @latest forces package resolution on first run (%v vs %v). "+
			"Consider pinning the bridge adapter version instead of using @latest.",
			firstElapsed, secondElapsed)
	} else if firstElapsed > 10*time.Second {
		t.Logf("NOTE: Both npx runs were slow (first: %v, second: %v) — bridge adapter startup itself is slow, not just package download",
			firstElapsed, secondElapsed)
	}

	// Check if the package is in npx cache
	homeDir, _ := os.UserHomeDir()
	cachePaths := []string{
		homeDir + "/.npm/_npx",
		homeDir + "/.cache/node/.npm/_npx",
	}
	for _, p := range cachePaths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			t.Logf("npx cache directory exists: %s", p)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Raw ACP protocol latency (bypassing ClawBench backend entirely)
// ---------------------------------------------------------------------------

// TestClaudeACP_RawProtocolLatency directly exercises the ACP Go SDK
// without ClawBench's ACPBackend/ACPConn wrapper. This measures the
// pure protocol overhead to isolate whether the slowness is in:
//   - The ACP protocol itself (bridge adapter + claude CLI)
//   - ClawBench's wrapper (connection pool, config caching, event routing)
func TestClaudeACP_RawProtocolLatency(t *testing.T) {
	requireClaudeACPAvailable(t)

	cmdParts := strings.Fields("npx -y @agentclientprotocol/claude-agent-acp@latest")
	cmdName := cmdParts[0]
	cmdArgs := cmdParts[1:]

	// ── Phase A: Spawn process ──
	t.Log("=== Raw ACP: Spawning bridge adapter ===")
	spawnStart := time.Now()

	cmd := exec.CommandContext(context.Background(), cmdName, cmdArgs...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, OrphanChildEnvVar)

	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe failed")
	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe failed")
	cmd.Stderr = &strings.Builder{}

	err = cmd.Start()
	require.NoError(t, err, "cmd.Start failed")
	spawnElapsed := time.Since(spawnStart)
	t.Logf("Phase A (process spawn): %v (PID: %d)", spawnElapsed, cmd.Process.Pid)

	// Ensure cleanup
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	// ── Phase B: ACP Initialize ──
	t.Log("=== Raw ACP: Initialize handshake ===")
	client := NewClawBenchACPClient()
	conn := acp.NewClientSideConnection(client, stdinPipe, stdoutPipe)
	conn.SetLogger(slog.Default())

	initStart := time.Now()
	initCtx, initCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer initCancel()

	initResp, err := conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs: acp.FileSystemCapabilities{
				ReadTextFile:  true,
				WriteTextFile: true,
			},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-test", Version: "1.0.0"},
	})
	initElapsed := time.Since(initStart)
	require.NoError(t, err, "Initialize failed")
	t.Logf("Phase B (Initialize): %v (protocol_version: %v)", initElapsed, initResp.ProtocolVersion)

	// ── Phase C: NewSession ──
	t.Log("=== Raw ACP: NewSession ===")
	newSessStart := time.Now()
	newSessCtx, newSessCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer newSessCancel()

	workDir, _ := os.Getwd()
	sessResp, err := conn.NewSession(newSessCtx, acp.NewSessionRequest{
		Cwd:        workDir,
		McpServers: []acp.McpServer{},
	})
	newSessElapsed := time.Since(newSessStart)
	require.NoError(t, err, "NewSession failed")
	t.Logf("Phase C (NewSession): %v (session_id: %s)", newSessElapsed, sessResp.SessionId)

	// Log what the session reports
	if sessResp.Modes != nil {
		t.Logf("  Modes: current=%s, available=%d", sessResp.Modes.CurrentModeId, len(sessResp.Modes.AvailableModes))
	}
	t.Logf("  ConfigOptions: %d", len(sessResp.ConfigOptions))
	// Log actual config option IDs and categories so we use the right ones in set_config_option.
	for i, opt := range sessResp.ConfigOptions {
		if opt.Select != nil {
			cat := "<nil>"
			if opt.Select.Category != nil {
				cat = string(*opt.Select.Category)
			}
			var vals []string
			if opt.Select.Options.Ungrouped != nil {
				for _, v := range *opt.Select.Options.Ungrouped {
					vals = append(vals, string(v.Value))
				}
			}
			t.Logf("  ConfigOption[%d]: id=%q category=%s current=%q values=%v", i, opt.Select.Id, cat, opt.Select.CurrentValue, vals)
		}
	}

	// ── Phase D: SetSessionConfigOption (model) ──
	// Use "claude-sonnet-4-6" — the valid model ID reported by the bridge adapter.
	t.Log("=== Raw ACP: SetSessionConfigOption(model) ===")
	configModelStart := time.Now()
	configCtx, configCancel := context.WithTimeout(context.Background(), 30*time.Second)
	_, configErr := conn.SetSessionConfigOption(configCtx, acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: sessResp.SessionId,
			ConfigId:  "model",
			Value:     "claude-sonnet-4-6",
		},
	})
	configCancel()
	configModelElapsed := time.Since(configModelStart)
	if configErr != nil {
		t.Logf("Phase D (set_config model): %v (ERROR: %v)", configModelElapsed, configErr)
	} else {
		t.Logf("Phase D (set_config model): %v", configModelElapsed)
	}

	// ── Phase E: SetSessionConfigOption (effort) ──
	// Claude bridge adapter reports ConfigOption id="effort" (category=thought_level).
	t.Log("=== Raw ACP: SetSessionConfigOption(effort) ===")
	configEffortStart := time.Now()
	configCtx2, configCancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	_, configErr2 := conn.SetSessionConfigOption(configCtx2, acp.SetSessionConfigOptionRequest{
		ValueId: &acp.SetSessionConfigOptionValueId{
			SessionId: sessResp.SessionId,
			ConfigId:  "effort",
			Value:     "low",
		},
	})
	configCancel2()
	configEffortElapsed := time.Since(configEffortStart)
	if configErr2 != nil {
		t.Logf("Phase E (set_config effort): %v (ERROR: %v)", configEffortElapsed, configErr2)
	} else {
		t.Logf("Phase E (set_config effort): %v", configEffortElapsed)
	}

	// ── Phase F: Prompt ──
	t.Log("=== Raw ACP: Prompt ===")
	// Register a channel for session updates
	streamCh := make(chan StreamEvent, 100)
	client.RegisterSession(string(sessResp.SessionId), streamCh)
	defer client.UnregisterSession(string(sessResp.SessionId))

	promptStart := time.Now()
	promptCtx, promptCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer promptCancel()

	// Start Prompt in a goroutine — it blocks until the turn completes
	promptDone := make(chan error, 1)
	go func() {
		_, promptErr := conn.Prompt(promptCtx, acp.PromptRequest{
			SessionId: sessResp.SessionId,
			Prompt:    []acp.ContentBlock{acp.TextBlock("回复一个字：好")},
		})
		promptDone <- promptErr
	}()

	// Collect streaming events
	var firstContentTime time.Duration
	var gotFirstContent bool
	var eventCount int

	collectDone := false
	for !collectDone {
		select {
		case event, ok := <-streamCh:
			if !ok {
				collectDone = true
				break
			}
			eventCount++
			elapsed := time.Since(promptStart)
			if event.Type == "content" && !gotFirstContent {
				firstContentTime = elapsed
				gotFirstContent = true
				t.Logf("  First content chunk at: %v", firstContentTime)
			}
		case promptErr := <-promptDone:
			totalElapsed := time.Since(promptStart)
			if promptErr != nil {
				t.Logf("Phase F (Prompt): %v (ERROR: %v)", totalElapsed, promptErr)
			} else {
				t.Logf("Phase F (Prompt total): %v", totalElapsed)
			}
			if gotFirstContent {
				t.Logf("  Time to first content: %v", firstContentTime)
			}
			collectDone = true
		case <-time.After(120 * time.Second):
			t.Log("Phase F (Prompt): TIMEOUT after 120s")
			collectDone = true
		}
	}
	t.Logf("  Total SSE events received: %d", eventCount)

	// ── Summary ──
	t.Log("=== Raw ACP Latency Summary ===")
	t.Logf("  Phase A — Process spawn:            %v", spawnElapsed)
	t.Logf("  Phase B — Initialize handshake:      %v", initElapsed)
	t.Logf("  Phase C — NewSession:                %v", newSessElapsed)
	t.Logf("  Phase D — set_config(model):         %v", configModelElapsed)
	t.Logf("  Phase E — set_config(effort):          %v", configEffortElapsed)
	if gotFirstContent {
		t.Logf("  Phase F — Time to first content:     %v", firstContentTime)
	}
	setupTotal := spawnElapsed + initElapsed + newSessElapsed
	configTotal := configModelElapsed + configEffortElapsed
	t.Logf("  SETUP TOTAL (spawn+init+new):        %v", setupTotal)
	t.Logf("  CONFIG TOTAL (model+effort):         %v", configTotal)
	t.Logf("  OVERHEAD BEFORE CONTENT:             %v", setupTotal+configTotal)

	// Bottleneck alerts
	if setupTotal > 30*time.Second {
		t.Logf("⚠ SETUP BOTTLENECK: %v — spawn+initialize+newSession is very slow", setupTotal)
		if spawnElapsed > 15*time.Second {
			t.Logf("  → Process spawn alone took %v — npx @latest is likely downloading the package", spawnElapsed)
		}
		if initElapsed > 10*time.Second {
			t.Logf("  → Initialize took %v — bridge adapter or claude CLI startup is slow", initElapsed)
		}
		if newSessElapsed > 10*time.Second {
			t.Logf("  → NewSession took %v — claude CLI initialization during session creation is slow", newSessElapsed)
		}
	}
	if configTotal > 10*time.Second {
		t.Logf("⚠ CONFIG BOTTLENECK: %v — set_config_option RPCs are slow (sent sequentially, each with 30s timeout)", configTotal)
		if configModelElapsed > 5*time.Second {
			t.Logf("  → set_config model alone took %v — Claude bridge may restart subprocess on model change", configModelElapsed)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// logACPTimeline logs the timeline of event types received during a prompt.
func logACPTimeline(t *testing.T, events []StreamEvent, start time.Time) {
	t.Helper()
	t.Log("  Event timeline:")
	for i, event := range events {
		// Only log first few of each type and always log done/error
		if event.Type == "done" || event.Type == "error" || i < 20 {
			elapsed := time.Duration(0) // we don't have per-event timestamps
			_ = elapsed
			t.Logf("    [%d] %s", i, event.Type)
		}
	}
	// Summary by type
	typeCounts := make(map[string]int)
	for _, e := range events {
		typeCounts[e.Type]++
	}
	t.Logf("  Event counts: %v", typeCounts)
}

// truncate is inherited from integration_test.go in the same package.

// TestClaudeACP_MetadataCostE2E drives a real claude ACP bridge through the
// full ACPBackend.ExecuteStream path and verifies that the USD cost claude
// reports on the final usage_update (cost=0.1568 USD in wire captures)
// reaches the turn-final metadata event's CostUSD — the field persisted to
// chat_metadata.cost_usd. Regression test for ACP agents whose cost was only
// propagated to the live usage_state, leaving cost_usd at 0.
func TestClaudeACP_MetadataCostE2E(t *testing.T) {
	requireClaudeACPAvailable(t)

	agent := claudeACPAgent()
	_ = setupACPTestEnvForAgent(t, agent)
	backend, err := NewACPBackend(agent)
	require.NoError(t, err)

	sessionID := acpSessionID()
	cleanupConn(t, sessionID)

	ctx, cancel := contextWithTimeout(t, 240*time.Second)
	defer cancel()
	ch, err := backend.ExecuteStream(ctx, ChatRequest{
		Prompt:    "请只回复一个字：好",
		SessionID: sessionID,
		WorkDir:   acpTestWorkDir(),
	})
	require.NoError(t, err, "ExecuteStream should not return error")

	var usageEvents, metadataEvents []StreamEvent
	for evt := range ch {
		switch evt.Type {
		case "usage_update":
			usageEvents = append(usageEvents, evt)
		case "metadata":
			metadataEvents = append(metadataEvents, evt)
		}
	}

	require.NotEmpty(t, usageEvents, "expected at least one usage_update event")
	var lastCost float64
	for _, evt := range usageEvents {
		if evt.Usage != nil {
			lastCost = evt.Usage.Cost
			t.Logf("usage_update: used=%d size=%d cost=%v currency=%q", evt.Usage.Used, evt.Usage.Size, evt.Usage.Cost, evt.Usage.Currency)
		}
	}

	require.NotEmpty(t, metadataEvents, "expected a metadata event")
	for _, evt := range metadataEvents {
		if evt.Meta == nil {
			continue
		}
		t.Logf("metadata: input=%d output=%d total=%d cachedWrite=%d costUsd=%v stopReason=%q",
			evt.Meta.InputTokens, evt.Meta.OutputTokens, evt.Meta.TotalTokens,
			evt.Meta.CachedWriteTokens, evt.Meta.CostUSD, evt.Meta.StopReason)
	}

	// claude reports cost on usage_update; the metadata event must now carry it
	// so chat_metadata.cost_usd is populated. Model/version dependent — if the
	// agent reported no cost at all (lastCost==0), the assertion is vacuous.
	if lastCost > 0 {
		var metaCost float64
		for _, evt := range metadataEvents {
			if evt.Meta != nil && evt.Meta.CostUSD > 0 {
				metaCost = evt.Meta.CostUSD
			}
		}
		assert.Equal(t, lastCost, metaCost, "usage_update cost must be propagated to the metadata event CostUSD")
		t.Logf("CONFIRMED: claude cost %.6f USD reached message metadata", metaCost)
	} else {
		t.Logf("NOTICE: claude usage_update reported no cost this run — assertion skipped")
	}
}
