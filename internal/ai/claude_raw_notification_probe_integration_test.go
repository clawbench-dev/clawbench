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
// Raw session/update notification bypass — real-agent probe
// ===========================================================================
//
// Confirms that an extension session/update variant (Claude's `subagent_spawned`)
// reaches ClawBench's raw notification sink over a real `claude-agent-acp`
// connection, with its payload intact — the thing the SDK's typed SessionUpdate
// union loses (see TestSDKMisclassifiesUnknownSessionUpdateVariant).
//
// Run:
//
//	go test -v -run 'TestClaudeACPRawNotificationProbe' -tags integration \
//	    -timeout 600s ./internal/ai/
//
// Needs `claude` installed and logged in.

// rawNotifProbeClient records extension updates seen by the raw sink.
type rawNotifProbeClient struct {
	*ClawBenchACPClient

	mu       sync.Mutex
	variants map[string]int
	first    []ExtensionUpdate
}

func (c *rawNotifProbeClient) record(u ExtensionUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.variants == nil {
		c.variants = map[string]int{}
	}
	c.variants[u.Variant]++
	if len(c.first) < 5 {
		c.first = append(c.first, u)
	}
}

func (c *rawNotifProbeClient) snapshot() (map[string]int, []ExtensionUpdate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v := make(map[string]int, len(c.variants))
	for k, n := range c.variants {
		v[k] = n
	}
	f := make([]ExtensionUpdate, len(c.first))
	copy(f, c.first)
	return v, f
}

// The probe asks the model to spawn a subagent, which makes claude-agent-acp
// emit subagent_spawned.
const claudeProbeSpawnPrompt = `Use the Agent tool exactly once with subagent_type "general-purpose" and the prompt "Reply with the single word DONE". Then reply with one sentence summarising what the subagent returned.`

func TestClaudeACPRawNotificationProbe(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not installed")
	}

	ctx, cancel := contextWithTimeout(t, 600*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "npx", "-y", "@agentclientprotocol/claude-agent-acp@latest")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	agentOut, err := cmd.StdoutPipe()
	require.NoError(t, err)
	agentIn, err := cmd.StdinPipe()
	require.NoError(t, err)
	cmd.Stderr = nil

	// Real filter so the raw notification tee is exercised end to end.
	filter := newACPStdoutFilter(agentOut)
	defer filter.Close()

	base := NewClawBenchACPClient()
	client := &rawNotifProbeClient{ClawBenchACPClient: base}
	client.SetExtensionUpdateHandler(client.record)

	conn := acp.NewClientSideConnection(client, agentIn, filter)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	filter.SetNotificationSink(client)

	require.NoError(t, cmd.Start(), "spawn claude-agent-acp")
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	initCtx, initCancel := context.WithTimeout(ctx, 60*time.Second)
	defer initCancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
			// The adapter gates its native-subagent session updates on this AIR
			// extension capability (clientSupportsSubagents → clientSupportsAirCapability):
			//   clientCapabilities._meta.jetbrains.air = { version: >=1, capabilities: [...] }
			// Without it, no subagent_spawned/subagent_state_update is emitted at
			// all, so the raw bypass would see nothing. Advertised here to probe
			// whether that is what turns the events on.
			Meta: map[string]any{
				"jetbrains": map[string]any{
					"air": map[string]any{
						"version":      1,
						"capabilities": []string{"nativeSubagentSessions"},
					},
				},
			},
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-raw-notif-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	newCtx, newCancel := context.WithTimeout(ctx, 60*time.Second)
	defer newCancel()
	newResp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: acpTestWorkDir(), McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")

	streamCh := make(chan StreamEvent, 8192)
	go func() {
		for range streamCh {
		}
	}()
	client.RegisterSession(string(newResp.SessionId), streamCh)

	turnCtx, turnCancel := context.WithTimeout(ctx, 300*time.Second)
	defer turnCancel()
	resp, err := conn.Prompt(turnCtx, acp.PromptRequest{
		SessionId: newResp.SessionId,
		Prompt:    []acp.ContentBlock{acp.TextBlock(claudeProbeSpawnPrompt)},
	})
	if err != nil {
		t.Logf("NOTE: prompt error: %v", err)
	}
	t.Logf("stopReason=%s", resp.StopReason)
	time.Sleep(2 * time.Second)

	variants, samples := client.snapshot()
	t.Logf("extension variants seen by the raw sink: %v", variants)
	for i, s := range samples {
		t.Logf("  [%d] variant=%s session=%s params=%.200s", i, s.Variant, s.SessionID, string(s.Params))
	}

	if variants["subagent_spawned"] == 0 {
		t.Logf("NOTE: no subagent_spawned observed — the model may not have spawned, " +
			"or the adapter version does not emit it. See variants above.")
		return
	}
	t.Logf("CONFIRMED: subagent_spawned reached the raw sink over stdio")
}
