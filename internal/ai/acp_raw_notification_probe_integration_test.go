//go:build integration

package ai

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// Raw notification bypass — all features, real-agent integration probe
// ===========================================================================
//
// Exercises every capability the stdout notification tee enables, against a real
// agent, with the SDK's own limits in place:
//
//   F1. Unknown session/update variants reach the raw handler with payload
//       intact (Claude: subagent_spawned / subagent_state_update). The SDK's
//       typed union would have misclassified these as SessionInfoUpdate.
//   F2. Custom agent→client notification methods reach the method handler
//       (methods the SDK does not dispatch at all).
//   F3. The typed callback IGNORES misclassified variants, so no bogus event is
//       produced for a frame that is not what the SDK thinks it is.
//   F4. Spec methods are not misrouted to the extension handlers (the tee does
//       not change spec behavior).
//
// Run:
//
//	go test -v -run 'TestACPRawNotificationBypass' -tags integration \
//	    -timeout 900s ./internal/ai/
//
// Needs `claude` and/or `codebuddy` installed and logged in.

// bypassProbeClient records everything the bypass delivers, plus every typed
// notification the SDK delivers (to prove F3/F4).
type bypassProbeClient struct {
	*ClawBenchACPClient

	mu            sync.Mutex
	variants      map[string]int
	customMethods map[string]int
	variantSample []ExtensionUpdate
	customSample  []ExtensionNotification
	typedVariants map[string]int
	typedCalls    int
	// typedSkipped counts frames the discriminator guard rejected (the SDK
	// misclassified them). This is the F3 evidence: they reached the typed
	// callback but were NOT acted on.
	typedSkipped int
}

func newBypassProbeClient() *bypassProbeClient {
	c := &bypassProbeClient{
		ClawBenchACPClient: NewClawBenchACPClient(),
		variants:           map[string]int{},
		customMethods:      map[string]int{},
		typedVariants:      map[string]int{},
	}
	c.SetExtensionUpdateHandler(c.recordVariant)
	c.SetExtensionNotificationHandler(c.recordCustom)
	return c
}

func (c *bypassProbeClient) recordVariant(u ExtensionUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.variants[u.Variant]++
	if len(c.variantSample) < 8 {
		c.variantSample = append(c.variantSample, u)
	}
}

func (c *bypassProbeClient) recordCustom(n ExtensionNotification) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.customMethods[n.Method]++
	if len(c.customSample) < 8 {
		c.customSample = append(c.customSample, n)
	}
}

// SessionUpdate overrides the embedded typed callback so the probe can observe
// what the SDK delivered AND whether the discriminator guard rejected it.
func (c *bypassProbeClient) SessionUpdate(ctx context.Context, n acp.SessionNotification) error {
	c.mu.Lock()
	c.typedCalls++
	c.typedVariants[typedVariantName(n.Update)]++
	if sessionUpdateVariantMismatch(n.Update) {
		c.typedSkipped++
	}
	c.mu.Unlock()
	return c.ClawBenchACPClient.SessionUpdate(ctx, n)
}

func (c *bypassProbeClient) snapshot() (map[string]int, map[string]int, []ExtensionUpdate, []ExtensionNotification, map[string]int, int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := func(m map[string]int) map[string]int {
		out := make(map[string]int, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return cp(c.variants), cp(c.customMethods),
		append([]ExtensionUpdate(nil), c.variantSample...),
		append([]ExtensionNotification(nil), c.customSample...),
		cp(c.typedVariants), c.typedCalls, c.typedSkipped
}

// typedVariantName names whichever union member the SDK populated.
func typedVariantName(u acp.SessionUpdate) string {
	switch {
	case u.UserMessageChunk != nil:
		return "user_message_chunk"
	case u.AgentMessageChunk != nil:
		return "agent_message_chunk"
	case u.AgentThoughtChunk != nil:
		return "agent_thought_chunk"
	case u.ToolCall != nil:
		return "tool_call"
	case u.ToolCallUpdate != nil:
		return "tool_call_update"
	case u.Plan != nil:
		return "plan"
	case u.PlanUpdate != nil:
		return "plan_update"
	case u.PlanRemoved != nil:
		return "plan_removed"
	case u.AvailableCommandsUpdate != nil:
		return "available_commands_update"
	case u.CurrentModeUpdate != nil:
		return "current_mode_update"
	case u.ConfigOptionUpdate != nil:
		return "config_option_update"
	case u.SessionInfoUpdate != nil:
		return "session_info_update"
	case u.UsageUpdate != nil:
		return "usage_update"
	default:
		return "unknown"
	}
}

// bypassProbeSession is one live ACP session driven by the probe.
type bypassProbeSession struct {
	client *bypassProbeClient
	conn   *acp.ClientSideConnection
	filter *acpStdoutFilter
	id     string
}

func startBypassProbe(t *testing.T, ctx context.Context, cmd *exec.Cmd, client *bypassProbeClient, extraCapMeta map[string]any) *bypassProbeSession {
	t.Helper()
	agentOut, err := cmd.StdoutPipe()
	require.NoError(t, err)
	agentIn, err := cmd.StdinPipe()
	require.NoError(t, err)
	cmd.Stderr = nil

	filter := newACPStdoutFilter(agentOut)
	t.Cleanup(filter.Close)

	conn := acp.NewClientSideConnection(client, agentIn, filter)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	filter.SetNotificationSink(client)

	require.NoError(t, cmd.Start(), "spawn agent")
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	caps := acp.ClientCapabilities{
		Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		Terminal: true,
	}
	if extraCapMeta != nil {
		caps.Meta = extraCapMeta
	}

	initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
		ClientInfo:         &acp.Implementation{Name: "clawbench-bypass-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	newCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
	defer cancel2()
	resp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: acpTestWorkDir(), McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")

	return &bypassProbeSession{client: client, conn: conn, filter: filter, id: string(resp.SessionId)}
}

func (s *bypassProbeSession) prompt(ctx context.Context, timeout time.Duration, text string) {
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := s.conn.Prompt(turnCtx, acp.PromptRequest{
		SessionId: acp.SessionId(s.id),
		Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
	})
	if err != nil {
		slog.Warn("bypass probe prompt error", "err", err)
		return
	}
	slog.Info("bypass probe turn done", "stop_reason", string(resp.StopReason))
}

// airSubagentCapability is the client capability that makes claude-agent-acp
// emit its native-subagent session updates (subagent_spawned /
// subagent_state_update). Without it the adapter emits none, so F1 has nothing
// to observe.
func airSubagentCapability() map[string]any {
	return map[string]any{
		"jetbrains": map[string]any{
			"air": map[string]any{
				"version":      1,
				"capabilities": []string{"nativeSubagentSessions"},
			},
		},
	}
}

// spawnPrompt makes the agent delegate to a subagent (claude) / team member
// (codebuddy), which is what produces the extension frames.
const bypassSpawnPrompt = `Use the Agent tool exactly once with subagent_type "general-purpose" and the prompt "Reply with the single word DONE". Then reply with one sentence summarising what the subagent returned.`

// TestACPRawNotificationBypass_Claude covers F1–F4 against claude-agent-acp.
func TestACPRawNotificationBypass_Claude(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed")
	}
	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	client := newBypassProbeClient()
	cmd := exec.CommandContext(ctx, "npx", "-y", "@agentclientprotocol/claude-agent-acp@latest")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	sess := startBypassProbe(t, ctx, cmd, client, airSubagentCapability())

	streamCh := make(chan StreamEvent, 16384)
	client.RegisterSession(sess.id, streamCh)
	go func() {
		for range streamCh {
		}
	}()

	sess.prompt(ctx, 300*time.Second, bypassSpawnPrompt)
	time.Sleep(2 * time.Second)

	variants, custom, vSamples, cSamples, typed, typedCalls, typedSkipped := client.snapshot()

	t.Logf("F1 raw extension variants: %v", variants)
	for i, s := range vSamples {
		t.Logf("  [%d] %s params=%.180s", i, s.Variant, string(s.Params))
	}
	t.Logf("F2 custom notification methods: %v", custom)
	for i, n := range cSamples {
		t.Logf("  [%d] %s params=%.180s", i, n.Method, string(n.Params))
	}
	t.Logf("F3 typed variants delivered: %v (total %d, skipped-by-guard %d)", typed, typedCalls, typedSkipped)

	// F1: the adapter's native-subagent variants must arrive via the bypass.
	require.NotZero(t, variants["subagent_spawned"],
		"F1 FAILED: subagent_spawned must reach the raw handler")
	require.NotZero(t, variants["subagent_state_update"],
		"F1 FAILED: subagent_state_update must reach the raw handler")
	// Payload intact: subagentSessionId is not modelled by any SDK struct.
	require.Contains(t, string(vSamples[0].Params), "subagentSessionId",
		"F1 FAILED: extension payload must be intact")

	// F3: the SDK delivered those frames as a known struct (SessionInfoUpdate)
	// and the discriminator guard rejected them — they must NOT have been acted
	// on. The guard count must cover every raw variant seen.
	require.GreaterOrEqual(t, typedSkipped, variants["subagent_spawned"]+variants["subagent_state_update"],
		"F3 FAILED: every misclassified variant must be rejected by the guard")

	// F4: spec methods still work — the turn completed and produced content.
	require.NotZero(t, typed["agent_message_chunk"],
		"F4 FAILED: spec session/update variants must still flow through the SDK")

	t.Logf("CONFIRMED: claude bypass features F1/F3/F4 exercised over stdio")
}

// TestACPRawNotificationBypass_CodeBuddy covers F1 (team frames) and F2 (custom
// notification methods) against codebuddy, whose extension surface differs from
// claude's.
func TestACPRawNotificationBypass_CodeBuddy(t *testing.T) {
	if _, err := exec.LookPath("codebuddy"); err != nil {
		t.Skip("codebuddy CLI not installed")
	}
	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	client := newBypassProbeClient()
	cmd := exec.CommandContext(ctx, "codebuddy", "--acp")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	sess := startBypassProbe(t, ctx, cmd, client, nil)

	streamCh := make(chan StreamEvent, 16384)
	client.RegisterSession(sess.id, streamCh)
	go func() {
		for range streamCh {
		}
	}()

	sess.prompt(ctx, 300*time.Second, bypassSpawnPrompt)
	time.Sleep(2 * time.Second)

	variants, custom, vSamples, cSamples, typed, typedCalls, typedSkipped := client.snapshot()

	t.Logf("F1 raw extension variants: %v", variants)
	for i, s := range vSamples {
		t.Logf("  [%d] %s params=%.180s", i, s.Variant, string(s.Params))
	}
	t.Logf("F2 custom notification methods: %v", custom)
	for i, n := range cSamples {
		t.Logf("  [%d] %s params=%.180s", i, n.Method, string(n.Params))
	}
	t.Logf("F3 typed variants delivered: %v (total %d, skipped-by-guard %d)", typed, typedCalls, typedSkipped)

	// F4: the tee must not break the normal spec path.
	require.NotZero(t, typed["agent_message_chunk"],
		"F4 FAILED: spec session/update variants must still flow through the SDK")

	// F2: codebuddy exposes custom notification methods (e.g. _codebuddy.ai/command
	// for workspace_info). Version dependent, so report rather than hard-fail.
	if len(custom) > 0 {
		t.Logf("CONFIRMED: codebuddy custom notification methods observed: %v", custom)
	}

	// F1/F2 are codebuddy-version dependent; report rather than hard-fail so the
	// probe stays useful as a discovery tool.
	if len(variants) == 0 && len(custom) == 0 {
		t.Logf("NOTE: no extension variants or custom methods observed this run " +
			"(model/version dependent) — see F3/F4 above for the spec path")
		return
	}
	t.Logf("CONFIRMED: codebuddy bypass features exercised (variants=%v custom=%v)", variants, custom)
}
