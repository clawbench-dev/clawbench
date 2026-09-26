//go:build integration

package ai

import (
	"bufio"
	"context"
	"encoding/json"
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
// CodeBuddy Agent SDK — capability probe integration test
// ===========================================================================
//
// Purpose: answer "can ClawBench add a third transport based on CodeBuddy's
// native Agent SDK?" with empirical evidence instead of documentation reading.
//
// The SDK is a Node library (`@tencent-ai/agent-sdk`), so this Go test drives a
// Node harness (test/codebuddy-sdk/sdk_probe.mjs) that exercises every SDK
// capability and reports one NDJSON line per capability point.
//
// Design mirrors codebuddy_acp_ext_probe_integration_test.go: conclusions are
// recorded as observations; only the premises that MUST hold for adoption are
// hard assertions. Everything else is reported so the gap list is the artifact.
//
// Run:
//
//	cd test/codebuddy-sdk && npm install        # first time only
//	go test -v -run 'TestIntegration_CodeBuddySDK' -tags integration \
//	    -timeout 1800s ./internal/ai/
//
// Requires: node on PATH, a logged-in codebuddy CLI, and npm install done.
//
// Relevant background (verified by reading the SDK source, not its docs):
//   - The SDK spawns the CLI with --input-format=stream-json --output-format=stream-json
//     (NOT --acp): lib/transport/process-transport.js:292-296.
//   - It ships the whole CLI in-package (package/cli/bin/codebuddy + dist-server).
//   - Its control plane is a bespoke protocol (hook_callback / can_use_tool /
//     elicitation_create) in lib/_internal/query-controller.js:56-68 — not ACP JSON-RPC.

// --- SDK test point names ---------------------------------------------------

const (
	SDKBasicQuery          = "basic_query"
	SDKMessageTypes        = "message_types"
	SDKSessionIDCapture    = "session_id_capture"
	SDKCwdHonored          = "cwd_honored"
	SDKResumeContext       = "resume_context"
	SDKForkSession         = "fork_session"
	SDKContinueRecent      = "continue_recent"
	SDKPersistSessionFalse = "persist_session_false"
	SDKResumeAtMessage     = "resume_at_message"
	SDKIncludePartial      = "include_partial_messages"
	SDKStreamingInput      = "streaming_input"
	SDKInterrupt           = "interrupt"
	SDKAbortController     = "abort_controller"
	SDKCanUseToolAllow     = "can_use_tool_allow"
	SDKCanUseToolDeny      = "can_use_tool_deny"
	SDKCanUseToolModify    = "can_use_tool_modify_input"
	SDKPermissionPlan      = "permission_mode_plan"
	SDKPermissionBypass    = "permission_mode_bypass"
	SDKAllowedTools        = "allowed_tools"
	SDKDisallowedTools     = "disallowed_tools"
	SDKToolsWhitelist      = "tools_whitelist"
	SDKHookPreToolUse      = "hook_pre_tool_use"
	SDKHookPostToolUse     = "hook_post_tool_use"
	SDKHookReplaceOutput   = "hook_post_tool_use_replace_output"
	SDKHookUserPrompt      = "hook_user_prompt_submit"
	SDKHookSessionStart    = "hook_session_start"
	SDKHookStop            = "hook_stop"
	SDKHookBlockPreTool    = "hook_block_pre_tool"
	SDKBuiltinToolUse      = "builtin_tool_use"
	SDKBuiltinToolNames    = "builtin_tool_names"
	SDKCustomMCPTool       = "custom_mcp_tool"
	SDKMCPStdioServer      = "mcp_stdio_server"
	SDKMCPToolNaming       = "mcp_tool_naming"
	SDKAgentsDefinition    = "agents_definition"
	SDKSubagentTaskTool    = "subagent_task_tool"
	SDKThinkingOption      = "thinking_option"
	SDKThinkingDisabled    = "thinking_disabled"
	SDKEffortOption        = "effort_option"
	SDKGetAvailableModels  = "get_available_models"
	SDKSetModel            = "set_model"
	SDKAccountInfo         = "account_info"
	SDKSlashCommands       = "slash_commands"
	SDKSkillsExposed       = "skills_exposed"
	SDKPluginsExposed      = "plugins_exposed"
	SDKMCPServerStatus     = "mcp_server_status"
	SDKPermissionModeGet   = "permission_mode_get"
	SDKUsageTokens         = "usage_tokens"
	SDKCostUSDPresent      = "cost_usd_present"
	SDKCacheTokens         = "cache_tokens"
	SDKSettingSourcesNone  = "setting_sources_none"
	SDKSettingSourcesProj  = "setting_sources_project"
	SDKSystemPromptAppend  = "system_prompt_append"
	SDKSystemPromptOver    = "system_prompt_override"
	SDKAdditionalDirs      = "additional_directories"
	SDKMaxTurns            = "max_turns"
	SDKBackgroundDisabled  = "background_tasks_disabled"
	SDKImageInput          = "image_input"
	SDKOutputFormatJSON    = "output_format_json"
	SDKV2CreateSession     = "v2_create_session"
	SDKV2MultiTurn         = "v2_multi_turn"
	SDKV2ResumeSession     = "v2_resume_session"
	SDKV2BackgroundTasks   = "v2_background_tasks"
)

// sdkRequiredPoints are the premises that must hold for the SDK to be usable as
// a ClawBench transport at all. Mirrors REQUIRED_POINTS in sdk_probe.mjs.
var sdkRequiredPoints = map[string]bool{
	SDKBasicQuery:       true,
	SDKSessionIDCapture: true,
	SDKResumeContext:    true,
	SDKBuiltinToolUse:   true,
}

// sdkTestPoints lists every capability point the harness reports. Kept in sync
// with sdk_probe.mjs by validateSDKTestCoverage against the harness itself.
var sdkTestPoints = []string{
	SDKBasicQuery, SDKMessageTypes, SDKSessionIDCapture, SDKCwdHonored,
	SDKResumeContext, SDKForkSession, SDKContinueRecent, SDKPersistSessionFalse, SDKResumeAtMessage,
	SDKIncludePartial, SDKStreamingInput, SDKInterrupt, SDKAbortController,
	SDKCanUseToolAllow, SDKCanUseToolDeny, SDKCanUseToolModify,
	SDKPermissionPlan, SDKPermissionBypass, SDKAllowedTools, SDKDisallowedTools, SDKToolsWhitelist,
	SDKHookPreToolUse, SDKHookPostToolUse, SDKHookReplaceOutput, SDKHookUserPrompt,
	SDKHookSessionStart, SDKHookStop, SDKHookBlockPreTool,
	SDKBuiltinToolUse, SDKBuiltinToolNames, SDKCustomMCPTool, SDKMCPStdioServer, SDKMCPToolNaming,
	SDKAgentsDefinition, SDKSubagentTaskTool,
	SDKThinkingOption, SDKThinkingDisabled, SDKEffortOption,
	SDKGetAvailableModels, SDKSetModel, SDKAccountInfo,
	SDKSlashCommands, SDKSkillsExposed, SDKPluginsExposed, SDKMCPServerStatus, SDKPermissionModeGet,
	SDKUsageTokens, SDKCostUSDPresent, SDKCacheTokens,
	SDKSettingSourcesNone, SDKSettingSourcesProj, SDKSystemPromptAppend, SDKSystemPromptOver, SDKAdditionalDirs,
	SDKMaxTurns, SDKBackgroundDisabled, SDKImageInput, SDKOutputFormatJSON,
	SDKV2CreateSession, SDKV2MultiTurn, SDKV2ResumeSession, SDKV2BackgroundTasks,
}

// --- Harness contract types -------------------------------------------------

// sdkProbeResult is one NDJSON line from sdk_probe.mjs.
type sdkProbeResult struct {
	ID       string          `json:"id"`
	Group    string          `json:"group"`
	OK       bool            `json:"ok"`
	Skipped  bool            `json:"skipped"`
	MS       int64           `json:"ms"`
	Required bool            `json:"required"`
	Detail   json.RawMessage `json:"detail"`
}

// sdkProbeSummary is the final __summary__ line.
type sdkProbeSummary struct {
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
	// SDKAsyncCrashes records errors the SDK threw from async callbacks (e.g.
	// "Transport not started" from a late MCP control message). These are
	// robustness defects in the SDK, surfaced separately from capability gaps.
	SDKAsyncCrashes []struct {
		Kind  string `json:"kind"`
		Error string `json:"error"`
	} `json:"sdk_async_crashes"`
}

// --- Availability gate ------------------------------------------------------

// sdkProbeDir returns the absolute path to test/codebuddy-sdk.
func sdkProbeDir(t *testing.T) string {
	t.Helper()
	// This test lives in internal/ai/, so the repo root is two levels up.
	wd, err := os.Getwd()
	require.NoError(t, err, "getwd")
	return filepath.Join(filepath.Dir(filepath.Dir(wd)), "test", "codebuddy-sdk")
}

// requireSDKProbeAvailable skips when the harness cannot run at all.
// Mirrors requireACPBackendAvailable: one skip beats a wall of confusing failures.
func requireSDKProbeAvailable(t *testing.T) string {
	t.Helper()

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node not available on PATH, skipping CodeBuddy SDK probe")
	}

	dir := sdkProbeDir(t)
	harness := filepath.Join(dir, "sdk_probe.mjs")
	if _, err := os.Stat(harness); err != nil {
		t.Skipf("SDK probe harness missing at %s: %v", harness, err)
	}

	// The SDK dep is 49MB and intentionally not a repo dependency; require an
	// explicit `npm install` in test/codebuddy-sdk.
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "@tencent-ai", "agent-sdk")); err != nil {
		t.Skipf("@tencent-ai/agent-sdk not installed; run `cd test/codebuddy-sdk && npm install`")
	}

	// Cheap liveness check: resolve the module the same way the harness will.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	check := exec.CommandContext(ctx, nodePath, "-e", "import('@tencent-ai/agent-sdk').then(m=>process.exit(typeof m.query==='function'?0:1))")
	check.Dir = dir
	if out, err := check.CombinedOutput(); err != nil {
		t.Skipf("@tencent-ai/agent-sdk not resolvable from %s: %v (%s)", dir, err, truncate(string(out), 300))
	}

	return dir
}

// --- Coverage validation ----------------------------------------------------

// validateSDKTestCoverage ensures sdkTestPoints has no duplicates and that every
// point is classified as required or optional. Guards against silently dropping
// a capability from the inventory.
func validateSDKTestCoverage(t *testing.T) {
	t.Helper()

	seen := make(map[string]bool, len(sdkTestPoints))
	var dupes []string
	for _, p := range sdkTestPoints {
		if seen[p] {
			dupes = append(dupes, p)
		}
		seen[p] = true
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		t.Fatalf("duplicate SDK test points in sdkTestPoints: %v", dupes)
	}

	// Every required point must exist in the inventory.
	var orphanRequired []string
	for p := range sdkRequiredPoints {
		if !seen[p] {
			orphanRequired = append(orphanRequired, p)
		}
	}
	if len(orphanRequired) > 0 {
		sort.Strings(orphanRequired)
		t.Fatalf("sdkRequiredPoints references unknown test points: %v", orphanRequired)
	}
}

// --- Runner -----------------------------------------------------------------

// runSDKProbe executes the Node harness and returns parsed results plus the summary.
func runSDKProbe(t *testing.T, workDir string, only []string, timeout time.Duration) ([]sdkProbeResult, *sdkProbeSummary, string) {
	t.Helper()

	dir := requireSDKProbeAvailable(t)
	harness := filepath.Join(dir, "sdk_probe.mjs")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", harness)
	cmd.Dir = dir
	env := append(os.Environ(), "SDK_PROBE_CWD="+workDir)
	if len(only) > 0 {
		env = append(env, "SDK_PROBE_ONLY="+strings.Join(only, ","))
	}
	cmd.Env = env

	var stderr strings.Builder
	cmd.Stderr = &stderr

	stdout, err := cmd.Output()
	// A non-zero exit is expected when a required point fails; the NDJSON on
	// stdout is still authoritative. Only surface the error when there is no
	// usable output at all.
	if err != nil && len(stdout) == 0 {
		t.Fatalf("SDK probe produced no output: %v\nstderr:\n%s", err, truncate(stderr.String(), 2000))
	}

	var results []sdkProbeResult
	var summary *sdkProbeSummary
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
			var s sdkProbeSummary
			if err := json.Unmarshal([]byte(line), &s); err != nil {
				t.Errorf("failed to parse summary line: %v", err)
				continue
			}
			summary = &s
		case "__fatal__":
			t.Fatalf("SDK probe reported a fatal error: %s", truncate(line, 2000))
		default:
			var r sdkProbeResult
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				t.Errorf("failed to parse probe result %q: %v", line, err)
				continue
			}
			results = append(results, r)
		}
	}
	require.NoError(t, sc.Err(), "scanning probe output")

	// Keep the full stderr for the caller to log on demand.
	return results, summary, stderr.String()
}

// sdkWorkDir returns a scratch directory for probe runs. Kept out of the repo so
// the CLI does not treat ClawBench itself as the project under test.
func sdkWorkDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "clawbench-sdk-probe")
	require.NoError(t, os.MkdirAll(dir, 0o755), "create probe workdir")
	return dir
}

// ===========================================================================
// Tests
// ===========================================================================

// TestIntegration_CodeBuddySDK is the unified entry point for the CodeBuddy
// Agent SDK capability probe.
//
// Assertion policy (matches the ext-probe precedent):
//   - required points (sdkRequiredPoints) are hard-asserted: if one fails, the
//     SDK cannot be adopted and the test must go red;
//   - every other point is reported via t.Logf. Failures there are the *output*
//     of this research — a gap list — not a broken build.
func TestIntegration_CodeBuddySDK(t *testing.T) {
	validateSDKTestCoverage(t)

	workDir := sdkWorkDir(t)
	// The full sweep is long: each point spawns a fresh CLI process.
	results, summary, stderr := runSDKProbe(t, workDir, nil, 30*time.Minute)

	require.NotNil(t, summary, "probe did not emit a __summary__ line\nstderr:\n%s", truncate(stderr, 3000))
	require.NotEmpty(t, results, "probe emitted no capability results\nstderr:\n%s", truncate(stderr, 3000))

	byID := make(map[string]sdkProbeResult, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}

	// --- 1) Full capability table (the primary artifact) ---
	t.Log("")
	t.Log("=== CodeBuddy Agent SDK capability table ===")
	t.Logf("%-36s %-12s %-6s %s", "POINT", "GROUP", "RESULT", "DETAIL")
	for _, id := range sdkTestPoints {
		r, ok := byID[id]
		if !ok {
			t.Logf("%-36s %-12s %-6s %s", id, "?", "MISSING", "not reported by harness")
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
		t.Logf("%-36s %-12s %-6s %s", id, r.Group, status, detail)
	}
	t.Logf("totals: %d pass / %d fail / %d skip (of %d)",
		summary.Counts.Passed, summary.Counts.Failed, summary.Counts.Skipped, summary.Counts.Total)

	// --- 2) Required points: hard assertions ---
	for id := range sdkRequiredPoints {
		r, ok := byID[id]
		if !ok {
			t.Errorf("required SDK capability %q was never reported by the harness", id)
			continue
		}
		assert.Truef(t, r.OK,
			"REQUIRED SDK capability %q failed: %s", id, truncate(string(r.Detail), 800))
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
	for _, id := range sdkTestPoints {
		if _, ok := byID[id]; !ok {
			unreported = append(unreported, id)
		}
	}
	assert.Emptyf(t, unreported,
		"harness did not report these inventory points (drift between sdk_probe.mjs and sdkTestPoints): %v", unreported)
}

// TestIntegration_CodeBuddySDK_CoverageGapReport focuses purely on the gap list:
// which SDK capabilities are unavailable, and does the SDK cover the ACP
// extension surface ClawBench depends on today.
func TestIntegration_CodeBuddySDK_CoverageGapReport(t *testing.T) {
	validateSDKTestCoverage(t)

	workDir := sdkWorkDir(t)
	results, summary, stderr := runSDKProbe(t, workDir, nil, 30*time.Minute)
	require.NotNil(t, summary, "probe did not emit a __summary__ line\nstderr:\n%s", truncate(stderr, 3000))

	byID := make(map[string]sdkProbeResult, len(results))
	for _, r := range results {
		byID[r.ID] = r
	}

	// --- Gaps: everything not passing ---
	var gaps, skipped []string
	for _, id := range sdkTestPoints {
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
		t.Logf("  GAP  %-36s %s", id, truncate(string(r.Detail), 300))
	}
	if len(skipped) > 0 {
		t.Log("")
		t.Log("=== Not exercised (deferred / not stably observable) ===")
		for _, id := range skipped {
			t.Logf("  SKIP %-36s %s", id, truncate(string(byID[id].Detail), 200))
		}
	}

	// --- Does the SDK cover the ACP extension surface ClawBench relies on? ---
	//
	// ClawBench's CodeBuddy integration depends on capabilities that live in the
	// ACP layer today. Map each to the SDK probe point that would have to cover
	// it, and report which have no SDK equivalent.
	t.Log("")
	t.Log("=== ClawBench dependency → SDK coverage ===")

	type dep struct {
		name    string
		covered bool
		note    string
	}
	deps := []dep{
		{
			name:    "mid-turn injection (ACP session/steer)",
			covered: false,
			note:    "no SDK equivalent probed; SDK exposes streaming input, not mid-turn steer",
		},
		{
			name:    "permission approval round-trip",
			covered: byID[SDKCanUseToolAllow].OK && byID[SDKCanUseToolDeny].OK,
			note:    "canUseTool (allow/deny/modify) replaces ACP requestPermission",
		},
		{
			name:    "tool call streaming",
			covered: byID[SDKBuiltinToolUse].OK,
			note:    "assistant message content blocks carry tool_use",
		},
		{
			name:    "extended thinking",
			covered: byID[SDKThinkingOption].OK,
			note:    "thinking content block IS present on this build (TS types include it)",
		},
		{
			name:    "usage / token accounting",
			covered: byID[SDKUsageTokens].OK,
			note:    "result.usage carries input/output + cache token counts",
		},
		{
			name:    "cost accounting (credits, not USD)",
			covered: false,
			note:    "total_cost_usd is 0 and no credit field exists on the result — a real gap",
		},
		{
			name:    "slash commands surface",
			covered: byID[SDKSlashCommands].OK,
			note:    "supportedCommands() returns ~76 commands",
		},
		{
			name:    "skills surface",
			covered: byID[SDKSkillsExposed].OK,
			note:    "system:init.skills — absent on this CLI build; ClawBench scans ~/.codebuddy/skills on disk instead",
		},
		{
			name:    "plugins surface",
			covered: byID[SDKPluginsExposed].OK,
			note:    "system:init.plugins — absent on this CLI build; ClawBench scans ~/.codebuddy/plugins/cache on disk instead",
		},
		{
			name:    "session resume / load",
			covered: byID[SDKResumeContext].OK,
			note:    "options.resume + V2 resumeSession both preserve context",
		},
		{
			name:    "subagent delegation",
			covered: byID[SDKSubagentTaskTool].OK,
			note:    "agents option + Agent/Task tool (tool is named 'Agent' in this build)",
		},
		{
			name:    "custom in-process tools",
			covered: byID[SDKCustomMCPTool].OK,
			note:    "createSdkMcpServer({name,tools}) + tool(name,desc,zodShape,handler)",
		},
		{
			name:    "tool output rewriting (token saving)",
			covered: byID[SDKHookReplaceOutput].OK,
			note:    "PostToolUse updatedToolOutput — probe found it NOT honored on this build",
		},
		{
			name:    "background tasks",
			covered: byID[SDKV2BackgroundTasks].OK,
			note:    "disabled under query(); only the unstable V2 Session can consume cross-turn task events",
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
