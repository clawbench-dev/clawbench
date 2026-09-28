//go:build integration

package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Pi Coding Agent SDK — capability probe integration test
// ===========================================================================
//
// Purpose: answer "can ClawBench add an SDK-based transport for Pi (in addition
// to the existing CLI line-parsing transport)?" with empirical evidence instead
// of documentation reading.
//
// The SDK (`@earendil-works/pi-coding-agent`) is a Node/TypeScript library that
// embeds the agent in-process, so this Go test drives a Node harness
// (test/pi-sdk/pi_sdk_probe.mjs) that exercises every capability and reports one
// NDJSON line per capability point.
//
// Structure follows acp_integration_test.go: capability points are declared as
// constants, an inventory lists them, coverage is validated, and each point is
// independently reported. Assertion policy follows the CodeBuddy SDK probe
// precedent: only the premises that MUST hold for adoption are hard assertions;
// everything else is reported so the gap list is the artifact.
//
// Run:
//
//	cd test/pi-sdk && npm install        # first time only
//	go test -v -run 'TestIntegration_PiSDK' -tags integration \
//	    -timeout 1800s ./internal/ai/
//
// Requires: node on PATH, a logged-in Pi (credentials in ~/.pi/agent/auth.json),
// and npm install done.
//
// Relevant background (verified by running the SDK, not by reading its docs):
//   - The SDK is a genuine in-process embedding API: `createAgentSession()`
//     builds a ModelRuntime + SettingsManager + SessionManager + ResourceLoader
//     and returns an AgentSession; events arrive via session.subscribe().
//   - Pi's existing ClawBench transport is CLI-only (`pi -p --mode json`), and
//     ACP is explicitly disabled (backends/pi/cli.go, acp_register_test.go:168).
//   - The SDK does NOT spawn the `pi` binary; it reads ~/.pi/agent/auth.json.
//     So the availability gate checks credentials, not the CLI on PATH.

// --- SDK test point names ---------------------------------------------------
//
// Mirrors the point ids emitted by test/pi-sdk/pi_sdk_probe.mjs.

const (
	PiSDKBasicQuery                = "basic_query"
	PiSDKTextDeltaStreaming        = "text_delta_streaming"
	PiSDKSessionIDCapture          = "session_id_capture"
	PiSDKCwdHonored                = "cwd_honored"
	PiSDKThinkingEvents            = "thinking_events"
	PiSDKModelReported             = "model_reported"
	PiSDKResumeContinueRecent      = "resume_continue_recent"
	PiSDKResumeByID                = "resume_by_id"
	PiSDKSessionList               = "session_list"
	PiSDKForkSession               = "fork_session"
	PiSDKInMemoryNoPersistence     = "in_memory_no_persistence"
	PiSDKToolcallEndEvents         = "toolcall_end_events"
	PiSDKToolExecutionEvents       = "tool_execution_events"
	PiSDKUsageTokens               = "usage_tokens"
	PiSDKCostUSD                   = "cost_usd"
	PiSDKStopReason                = "stop_reason"
	PiSDKAgentLifecycleEvents      = "agent_lifecycle_events"
	PiSDKAbortMidRun               = "abort_mid_run"
	PiSDKSteerMidRun               = "steer_mid_run"
	PiSDKFollowUpQueue             = "follow_up_queue"
	PiSDKBuiltinToolUse            = "builtin_tool_use"
	PiSDKBuiltinToolNames          = "builtin_tool_names"
	PiSDKToolWhitelist             = "tool_whitelist"
	PiSDKToolExcludelist           = "tool_excludelist"
	PiSDKCustomTool                = "custom_tool"
	PiSDKExtensionToolCallHook     = "extension_tool_call_hook"
	PiSDKExtensionBlockTool        = "extension_block_tool"
	PiSDKExtensionRegisterCommand  = "extension_register_command"
	PiSDKThinkingLevels            = "thinking_levels"
	PiSDKSetThinkingLevel          = "set_thinking_level"
	PiSDKAvailableModels           = "available_models"
	PiSDKSetModelLive              = "set_model_live"
	PiSDKSystemPromptAppend        = "system_prompt_append"
	PiSDKSystemPromptOverride      = "system_prompt_override"
	PiSDKContextFilesOverride      = "context_files_override"
	PiSDKSkillsDiscovery           = "skills_discovery"
	PiSDKPromptTemplates           = "prompt_templates"
	PiSDKSessionStats              = "session_stats"
	PiSDKContextUsage              = "context_usage"
	PiSDKExportJSONL               = "export_jsonl"
	PiSDKExportHTML                = "export_html"
	PiSDKCompaction                = "compaction"
	PiSDKMultipleSessionsIsolated  = "multiple_sessions_isolated"
	PiSDKDisposeIdempotent         = "dispose_idempotent"
	PiSDKConcurrentPromptsRejected = "concurrent_prompts_rejected_or_queued"
)

// piSDKRequiredPoints are the premises that must hold for the SDK to be usable
// as a ClawBench transport at all. Mirrors REQUIRED_POINTS in pi_sdk_probe.mjs.
var piSDKRequiredPoints = map[string]bool{
	PiSDKBasicQuery:           true,
	PiSDKTextDeltaStreaming:   true,
	PiSDKSessionIDCapture:     true,
	PiSDKCwdHonored:           true,
	PiSDKResumeContinueRecent: true,
	PiSDKResumeByID:           true,
	PiSDKBuiltinToolUse:       true,
	PiSDKToolcallEndEvents:    true,
	PiSDKUsageTokens:          true,
	PiSDKAbortMidRun:          true,
}

// piSDKTestPoints lists every capability point the harness reports. Kept in sync
// with pi_sdk_probe.mjs by validatePiSDKTestCoverage plus the runtime drift check.
var piSDKTestPoints = []string{
	// basic
	PiSDKBasicQuery, PiSDKTextDeltaStreaming, PiSDKSessionIDCapture, PiSDKCwdHonored,
	PiSDKThinkingEvents, PiSDKModelReported,
	// session
	PiSDKResumeContinueRecent, PiSDKResumeByID, PiSDKSessionList, PiSDKForkSession,
	PiSDKInMemoryNoPersistence,
	// streaming
	PiSDKToolcallEndEvents, PiSDKToolExecutionEvents, PiSDKUsageTokens, PiSDKCostUSD,
	PiSDKStopReason, PiSDKAgentLifecycleEvents, PiSDKAbortMidRun, PiSDKSteerMidRun,
	PiSDKFollowUpQueue,
	// tools
	PiSDKBuiltinToolUse, PiSDKBuiltinToolNames, PiSDKToolWhitelist, PiSDKToolExcludelist,
	PiSDKCustomTool, PiSDKExtensionToolCallHook, PiSDKExtensionBlockTool,
	PiSDKExtensionRegisterCommand,
	// model
	PiSDKThinkingLevels, PiSDKSetThinkingLevel, PiSDKAvailableModels, PiSDKSetModelLive,
	PiSDKSystemPromptAppend, PiSDKSystemPromptOverride, PiSDKContextFilesOverride,
	// resources
	PiSDKSkillsDiscovery, PiSDKPromptTemplates, PiSDKSessionStats, PiSDKContextUsage,
	PiSDKExportJSONL, PiSDKExportHTML, PiSDKCompaction,
	// robustness
	PiSDKMultipleSessionsIsolated, PiSDKDisposeIdempotent, PiSDKConcurrentPromptsRejected,
}

// --- Harness contract types -------------------------------------------------

// piSDKProbeResult is one NDJSON line from pi_sdk_probe.mjs.
type piSDKProbeResult struct {
	ID       string          `json:"id"`
	Group    string          `json:"group"`
	OK       bool            `json:"ok"`
	Skipped  bool            `json:"skipped"`
	MS       int64           `json:"ms"`
	Required bool            `json:"required"`
	Detail   json.RawMessage `json:"detail"`
}

// piSDKProbeSummary is the final __summary__ line.
type piSDKProbeSummary struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Counts struct {
		Total   int `json:"total"`
		Passed  int `json:"passed"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
	} `json:"counts"`
	FailedIDs      []string `json:"failed_ids"`
	SkippedIDs     []string `json:"skipped_ids"`
	RequiredFailed []string `json:"required_failed_ids"`
	// SDKAsyncCrashes records errors the SDK threw from async callbacks. These
	// are robustness defects in the SDK, surfaced separately from capability gaps.
	SDKAsyncCrashes []struct {
		Kind  string `json:"kind"`
		Error string `json:"error"`
	} `json:"sdk_async_crashes"`
}

// --- Availability gate ------------------------------------------------------

// piSDKProbeDir returns the absolute path to test/pi-sdk.
func piSDKProbeDir(t *testing.T) string {
	t.Helper()
	// This test lives in internal/ai/, so the repo root is two levels up.
	wd, err := os.Getwd()
	require.NoError(t, err, "getwd")
	return filepath.Join(filepath.Dir(filepath.Dir(wd)), "test", "pi-sdk")
}

// requirePiSDKProbeAvailable skips when the harness cannot run at all.
// Mirrors requireACPBackendAvailable: one skip beats a wall of confusing failures.
func requirePiSDKProbeAvailable(t *testing.T) string {
	t.Helper()

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node not available on PATH, skipping Pi SDK probe")
	}

	dir := piSDKProbeDir(t)
	harness := filepath.Join(dir, "pi_sdk_probe.mjs")
	if _, err := os.Stat(harness); err != nil {
		t.Skipf("Pi SDK probe harness missing at %s: %v", harness, err)
	}

	// The SDK dep is ~430MB and intentionally not a repo dependency; require an
	// explicit `npm install` in test/pi-sdk.
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "@earendil-works", "pi-coding-agent")); err != nil {
		t.Skipf("@earendil-works/pi-coding-agent not installed; run `cd test/pi-sdk && npm install`")
	}

	// Liveness + auth check: the SDK is in-process and reads credentials from
	// ~/.pi/agent/auth.json. A module that resolves but has no usable model is
	// just as unusable as one that fails to import, so check both in one probe.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	script := `import('@earendil-works/pi-coding-agent')` +
		`.then(async (m) => {` +
		`  const rt = await m.ModelRuntime.create();` +
		`  const avail = await rt.getAvailable();` +
		`  process.exit(avail.length > 0 ? 0 : 2);` +
		`})` +
		`.catch((e) => { console.error(String(e && e.message || e)); process.exit(1); })`
	check := exec.CommandContext(ctx, nodePath, "-e", script)
	check.Dir = dir
	if out, err := check.CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			t.Skipf("Pi SDK resolved but no authenticated models are available (check ~/.pi/agent/auth.json); skipping")
		}
		t.Skipf("@earendil-works/pi-coding-agent not usable from %s: %v (%s)", dir, err, truncate(string(out), 300))
	}

	return dir
}

// --- Coverage validation ----------------------------------------------------

// validatePiSDKTestCoverage ensures piSDKTestPoints has no duplicates and that
// every required point exists in the inventory. Guards against silently dropping
// a capability from the inventory.
func validatePiSDKTestCoverage(t *testing.T) {
	t.Helper()

	seen := make(map[string]bool, len(piSDKTestPoints))
	var dupes []string
	for _, p := range piSDKTestPoints {
		if seen[p] {
			dupes = append(dupes, p)
		}
		seen[p] = true
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		t.Fatalf("duplicate Pi SDK test points in piSDKTestPoints: %v", dupes)
	}

	// Every required point must exist in the inventory.
	var orphanRequired []string
	for p := range piSDKRequiredPoints {
		if !seen[p] {
			orphanRequired = append(orphanRequired, p)
		}
	}
	if len(orphanRequired) > 0 {
		sort.Strings(orphanRequired)
		t.Fatalf("piSDKRequiredPoints references unknown test points: %v", orphanRequired)
	}
}

// --- Runner -----------------------------------------------------------------

// runPiSDKProbe executes the Node harness and returns parsed results plus the summary.
func runPiSDKProbe(t *testing.T, workDir string, only []string, timeout time.Duration) ([]piSDKProbeResult, *piSDKProbeSummary, string) {
	t.Helper()

	dir := requirePiSDKProbeAvailable(t)
	harness := filepath.Join(dir, "pi_sdk_probe.mjs")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", harness)
	cmd.Dir = dir
	env := append(os.Environ(), "PI_SDK_PROBE_CWD="+workDir)
	if len(only) > 0 {
		env = append(env, "PI_SDK_PROBE_ONLY="+strings.Join(only, ","))
	}
	cmd.Env = env

	var stderr strings.Builder
	cmd.Stderr = &stderr

	stdout, err := cmd.Output()
	// A non-zero exit is expected when a required point fails; the NDJSON on
	// stdout is still authoritative. Only surface the error when there is no
	// usable output at all.
	if err != nil && len(stdout) == 0 {
		t.Fatalf("Pi SDK probe produced no output: %v\nstderr:\n%s", err, truncate(stderr.String(), 2000))
	}

	var results []piSDKProbeResult
	var summary *piSDKProbeSummary
	sc := bufio.NewScanner(strings.NewReader(string(stdout)))
	sc.Buffer(make([]byte, 0, 256*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Logf("skipping non-JSON probe line: %s", truncate(line, 200))
			continue
		}
		switch probe.ID {
		case "__summary__":
			var s piSDKProbeSummary
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				t.Errorf("failed to parse summary line: %v", err)
				continue
			}
			summary = &s
		case "__fatal__":
			t.Fatalf("Pi SDK probe reported a fatal error: %s", truncate(line, 2000))
		default:
			var r piSDKProbeResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Errorf("failed to parse probe result %q: %v", line, err)
				continue
			}
			results = append(results, r)
		}
	}
	require.NoError(t, sc.Err(), "scanning probe output")

	return results, summary, stderr.String()
}

// piSDKWorkDir returns a scratch directory for probe runs. Kept out of the repo
// so Pi does not treat ClawBench itself as the project under test.
func piSDKWorkDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "clawbench-pi-sdk-probe")
	require.NoError(t, os.MkdirAll(dir, 0o755), "create probe workdir")
	return dir
}

// ===========================================================================
// Tests
// ===========================================================================

// TestIntegration_PiSDK is the unified entry point for the Pi Coding Agent SDK
// capability probe.
//
// Assertion policy (matches the ACP / CodeBuddy SDK probe precedent):
//   - required points (piSDKRequiredPoints) are hard-asserted: if one fails, the
//     SDK cannot be adopted and the test must go red;
//   - every other point is reported via t.Logf. Failures there are the *output*
//     of this research — a gap list — not a broken build.
func TestIntegration_PiSDK(t *testing.T) {
	validatePiSDKTestCoverage(t)

	workDir := piSDKWorkDir(t)
	// The full sweep is long: several points build context for compaction, fork
	// sessions, and abort mid-run.
	results, summary, stderr := runPiSDKProbe(t, workDir, nil, 30*time.Minute)

	require.NotNil(t, summary, "probe did not emit a __summary__ line\nstderr:\n%s", truncate(stderr, 3000))
	require.NotEmpty(t, results, "probe emitted no capability results\nstderr:\n%s", truncate(stderr, 3000))

	byID := make(map[string]piSDKProbeResult, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}

	// --- 1) Full capability table (the primary artifact) ---
	t.Log("")
	t.Log("=== Pi Coding Agent SDK capability table ===")
	t.Logf("%-40s %-12s %-6s %s", "POINT", "GROUP", "RESULT", "DETAIL")
	for _, id := range piSDKTestPoints {
		r, ok := byID[id]
		if !ok {
			t.Logf("%-40s %-12s %-6s %s", id, "?", "MISSING", "not reported by harness")
			continue
		}
		status := "PASS"
		switch {
		case r.Skipped:
			status = "SKIP"
		case !r.OK:
			status = "FAIL"
		}
		detail := ""
		if r.Skipped || !r.OK {
			detail = truncate(string(r.Detail), 220)
		}
		t.Logf("%-40s %-12s %-6s %s", id, r.Group, status, detail)
	}
	t.Logf("totals: %d pass / %d fail / %d skip (of %d)",
		summary.Counts.Passed, summary.Counts.Failed, summary.Counts.Skipped, summary.Counts.Total)

	// --- 2) Required points: hard assertions ---
	for id := range piSDKRequiredPoints {
		r, ok := byID[id]
		if !ok {
			t.Errorf("required Pi SDK capability %q was never reported by the harness", id)
			continue
		}
		assert.Truef(t, r.OK,
			"REQUIRED Pi SDK capability %q failed: %s", id, truncate(string(r.Detail), 800))
	}

	// --- 3) Harness self-consistency ---
	assert.Emptyf(t, summary.RequiredFailed,
		"harness reported required failures: %v", summary.RequiredFailed)

	if len(summary.SDKAsyncCrashes) > 0 {
		t.Log("")
		t.Log("=== SDK async-callback crashes (robustness defects, not capability gaps) ===")
		for _, c := range summary.SDKAsyncCrashes {
			t.Logf("  %-20s %s", c.Kind, truncate(c.Error, 200))
		}
	}

	// --- 4) Coverage: harness must report every point in our inventory ---
	var unreported []string
	for _, id := range piSDKTestPoints {
		if _, ok := byID[id]; !ok {
			unreported = append(unreported, id)
		}
	}
	assert.Emptyf(t, unreported,
		"harness did not report these inventory points (drift between pi_sdk_probe.mjs and piSDKTestPoints): %v", unreported)
}

// TestIntegration_PiSDK_CoverageGapReport focuses purely on the gap list: which
// SDK capabilities are unavailable, and does the SDK cover the ACP extension
// surface ClawBench depends on today.
func TestIntegration_PiSDK_CoverageGapReport(t *testing.T) {
	validatePiSDKTestCoverage(t)

	workDir := piSDKWorkDir(t)
	results, summary, stderr := runPiSDKProbe(t, workDir, nil, 30*time.Minute)
	require.NotNil(t, summary, "probe did not emit a __summary__ line\nstderr:\n%s", truncate(stderr, 3000))

	byID := make(map[string]piSDKProbeResult, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}

	// --- Gaps: everything not passing ---
	var gaps, skipped []string
	for _, id := range piSDKTestPoints {
		r, ok := byID[id]
		if !ok {
			gaps = append(gaps, id+" (unreported)")
			continue
		}
		if r.Skipped {
			skipped = append(skipped, id)
		} else if !r.OK {
			gaps = append(gaps, id)
		}
	}
	sort.Strings(gaps)
	sort.Strings(skipped)

	t.Log("")
	t.Log("=== SDK capability GAPS (capabilities the SDK does not provide) ===")
	if len(gaps) == 0 {
		t.Log("  (none — every probed capability passed)")
	}
	for _, id := range gaps {
		r := byID[id]
		t.Logf("  GAP  %-40s %s", id, truncate(string(r.Detail), 300))
	}
	if len(skipped) > 0 {
		t.Log("")
		t.Log("=== Not exercised (deferred / not stably observable) ===")
		for _, id := range skipped {
			t.Logf("  SKIP %-40s %s", id, truncate(string(byID[id].Detail), 200))
		}
	}

	// --- Does the SDK cover the ACP surface ClawBench relies on? ---
	//
	// ClawBench's ACP transport depends on a specific set of capabilities. Map
	// each to the SDK probe point that would have to cover it, and report which
	// have no SDK equivalent.
	t.Log("")
	t.Log("=== ClawBench ACP dependency → Pi SDK coverage ===")

	type dep struct {
		name    string
		covered bool
		note    string
	}
	deps := []dep{
		{
			name:    "new session + id capture",
			covered: byID[PiSDKSessionIDCapture].OK,
			note:    "session.sessionId, no separate handshake",
		},
		{
			name:    "resume / load session",
			covered: byID[PiSDKResumeContinueRecent].OK && byID[PiSDKResumeByID].OK,
			note:    "SessionManager.continueRecent / open(findById)",
		},
		{
			name:    "streaming content deltas",
			covered: byID[PiSDKTextDeltaStreaming].OK,
			note:    "message_update → assistantMessageEvent.text_delta",
		},
		{
			name:    "extended thinking",
			covered: byID[PiSDKThinkingEvents].OK,
			note:    "thinking_start/delta/end events + setThinkingLevel",
		},
		{
			name:    "tool call streaming",
			covered: byID[PiSDKToolcallEndEvents].OK,
			note:    "toolcall_end carries the complete ToolCall",
		},
		{
			name:    "tool execution lifecycle",
			covered: byID[PiSDKToolExecutionEvents].OK,
			note:    "tool_execution_start/update/end",
		},
		{
			name:    "usage / token accounting",
			covered: byID[PiSDKUsageTokens].OK,
			note:    "message_end.message.usage",
		},
		{
			name:    "cost accounting (USD)",
			covered: byID[PiSDKCostUSD].OK,
			note:    "usage.cost.total — unlike the CodeBuddy SDK, Pi reports real USD",
		},
		{
			name:    "cancel / abort mid-run",
			covered: byID[PiSDKAbortMidRun].OK,
			note:    "session.abort() + waitForIdle()",
		},
		{
			name:    "mid-turn injection (ACP session/steer)",
			covered: byID[PiSDKSteerMidRun].OK,
			note:    "session.steer() — the capability the CodeBuddy SDK lacked",
		},
		{
			name:    "permission approval round-trip",
			covered: byID[PiSDKExtensionBlockTool].OK,
			note:    "extension tool_call handler returning {block,reason} replaces requestPermission",
		},
		{
			name:    "model listing / switching",
			covered: byID[PiSDKAvailableModels].OK && byID[PiSDKSetModelLive].OK,
			note:    "ModelRuntime.getAvailable() + session.setModel()",
		},
		{
			name:    "system prompt injection",
			covered: byID[PiSDKSystemPromptAppend].OK && byID[PiSDKSystemPromptOverride].OK,
			note:    "DefaultResourceLoader systemPrompt / appendSystemPrompt overrides",
		},
		{
			name:    "custom in-process tools",
			covered: byID[PiSDKCustomTool].OK,
			note:    "defineTool + customTools option",
		},
		{
			name:    "skills surface",
			covered: byID[PiSDKSkillsDiscovery].OK,
			note:    "loadSkillsFromDir / DefaultResourceLoader skill paths",
		},
		{
			name:    "slash commands / prompt templates",
			covered: byID[PiSDKPromptTemplates].OK,
			note:    "registerCommand + PromptTemplate registry",
		},
		{
			name:    "compaction",
			covered: byID[PiSDKCompaction].OK,
			note:    "session.compact() with automatic threshold/overflow reasons",
		},
	}

	var uncovered []string
	for _, d := range deps {
		mark := "COVERED"
		if !d.covered {
			mark = "NO EQUIV"
			uncovered = append(uncovered, d.name)
		}
		t.Logf("  %-9s %-46s %s", mark, d.name, d.note)
	}

	if len(uncovered) > 0 {
		t.Log("")
		t.Logf("ClawBench dependencies with NO SDK equivalent: %v", uncovered)
	}

	if len(summary.SDKAsyncCrashes) > 0 {
		t.Log("")
		t.Log("=== SDK async-callback crashes (robustness defects) ===")
		for _, c := range summary.SDKAsyncCrashes {
			t.Logf("  %-20s %s", c.Kind, truncate(c.Error, 200))
		}
	}

	// The gap report is informational; only a completely dead harness fails.
	assert.NotZero(t, summary.Counts.Total, "probe reported zero capability points")
}
