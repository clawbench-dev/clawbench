//go:build integration

package ai

// ===========================================================================
// OpenCode v2 ACP — sub-agent (child session) leak probe (issue #639)
// ===========================================================================
//
// Question: does `opencode acp` project a sub-agent's own conversation into the
// PARENT session's session/update stream, and does ClawBench drop it before it
// reaches the main conversation timeline?
//
// OpenCode v2 runs sub-agents (the `subagent` tool) in child sessions. Unless
// the client opts into its `opencode/child-session-updates` extension channel,
// every child update is emitted as an ordinary session/update on the PARENT
// sessionId, tagged only with `_meta["opencode/child-session"]` and carrying no
// parent tool-call link. ClawBench does not opt in, so the raw wire carries the
// leak; the fix is the guard in mapACPSessionUpdate.
//
// This probe drives a REAL opencode process with ClawBench's production
// handshake, tees the full JSON-RPC wire, and:
//  1. asserts the subagent tool really ran and OpenCode really inlined child
//     frames (otherwise the run is inconclusive and the probe skips),
//  2. feeds a REAL captured child frame through mapACPSessionUpdate and asserts
//     nothing is emitted.
//
// On the unfixed tree step 2 fails — that failure IS the reproduction. On the
// fixed tree the whole probe passes.
//
// Run:
//
//	go test -v -run 'TestOpenCodeACP_ChildSessionLeakProbe' -tags integration \
//	    -timeout 600s ./internal/ai/
//
// Requires the `opencode` CLI (v2.x) installed and authenticated.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

const opencodeChildProbePrompt = `You MUST delegate this task to a subagent using the subagent tool (choose any available subagent listed in the tool description). Do not do the work yourself.
Give the subagent this task: "Create a file named child.txt in the current directory containing the word hello, then list the current directory, and reply with the single word CHILD_DONE."
After the subagent returns, reply with the single word PARENT_DONE.`

// probeChildSessionMetaKey is OpenCode's child-session marker. Defined locally
// (instead of using the production constant) so this probe also compiles — and
// fails at the right step — on a tree that does not have the fix yet.
const probeChildSessionMetaKey = "opencode/child-session"

// probeIsChildSessionUpdate reports whether an update carries OpenCode's
// child-session marker.
func probeIsChildSessionUpdate(u acp.SessionUpdate) bool {
	meta := acpUpdateMeta(u)
	if len(meta) == 0 {
		return false
	}
	_, ok := meta[probeChildSessionMetaKey]
	return ok
}

// opencodeChildProbe is one live opencode ACP session with the raw wire teed.
type opencodeChildProbe struct {
	client *opencodePlanProbeClient
	conn   *acp.ClientSideConnection
	id     string
	outBuf *bytes.Buffer // agent -> client
}

// startOpencodeChildProbe spawns `opencode acp` with ClawBench's production
// handshake (no child-session extension capability) and opens a session in a
// fresh temp directory.
func startOpencodeChildProbe(t *testing.T, ctx context.Context) *opencodeChildProbe {
	t.Helper()

	cmd := exec.CommandContext(ctx, "opencode", "acp")
	agentOut, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	agentIn, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	cmd.Stderr = os.Stderr

	outBuf := &bytes.Buffer{}
	recOut := &recordingReader{src: agentOut, buf: outBuf}
	recIn := &recordingWriter{dst: agentIn, buf: &bytes.Buffer{}}

	client := newOpencodePlanProbeClient()
	filter := newACPStdoutFilter(recOut)
	t.Cleanup(filter.Close)

	conn := acp.NewClientSideConnection(client, recIn, filter)
	filter.SetNotificationSink(client)

	require.NoError(t, cmd.Start(), "spawn opencode acp")
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{
			Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
		ClientInfo: &acp.Implementation{Name: "clawbench-opencode-child-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	// Mirror production wiring so backend-gated logic sees the opencode backend.
	client.connRef = newACPConn(&model.Agent{
		ID: "opencode-child-probe", Backend: "opencode", Transport: "acp-stdio",
	}, "opencode-child-probe-session")

	newCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
	defer cancel2()
	resp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")

	return &opencodeChildProbe{client: client, conn: conn, id: string(resp.SessionId), outBuf: outBuf}
}

// prompt runs the delegation prompt and returns once the turn settles.
func (p *opencodeChildProbe) prompt(ctx context.Context, timeout time.Duration) error {
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := p.conn.Prompt(turnCtx, acp.PromptRequest{
		SessionId: acp.SessionId(p.id),
		Prompt:    []acp.ContentBlock{acp.TextBlock(opencodeChildProbePrompt)},
	})
	return err
}

// opencodeChildProbeStats is what the raw wire shows.
type opencodeChildProbeStats struct {
	childSessionUpdates int // parent-stream frames tagged opencode/child-session (the leak)
	subagentToolCalls   int // tool_call frames for the subagent tool (proof of delegation)
	variants            map[string]int
	// sampleChildFrame is the first leaked frame, kept for the guard assertion.
	sampleChildFrame *acp.SessionNotification
}

// analyzeOpenCodeChildProbe decodes every agent→client JSON-RPC line.
func analyzeOpenCodeChildProbe(buf *bytes.Buffer) opencodeChildProbeStats {
	stats := opencodeChildProbeStats{variants: map[string]int{}}
	for _, raw := range bytes.Split(buf.Bytes(), []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var env struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &env) != nil || env.Method != "session/update" {
			continue
		}
		var n acp.SessionNotification
		if json.Unmarshal(env.Params, &n) != nil {
			continue
		}
		variant := typedVariantName(n.Update)
		stats.variants[variant]++
		if probeIsChildSessionUpdate(n.Update) {
			stats.childSessionUpdates++
			if stats.sampleChildFrame == nil {
				frame := n
				stats.sampleChildFrame = &frame
			}
		}
		if variant == "tool_call" && n.Update.ToolCall != nil &&
			strings.Contains(strings.ToLower(n.Update.ToolCall.Title), "subagent") {
			stats.subagentToolCalls++
		}
	}
	return stats
}

// TestOpenCodeACP_ChildSessionLeakProbe is the probe entry point.
func TestOpenCodeACP_ChildSessionLeakProbe(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode CLI not installed")
	}
	ctx, cancel := contextWithTimeout(t, 480*time.Second)
	defer cancel()

	probe := startOpencodeChildProbe(t, ctx)
	require.NoError(t, probe.prompt(ctx, 360*time.Second), "prompt")
	time.Sleep(2 * time.Second) // let trailing notifications land

	stats := analyzeOpenCodeChildProbe(probe.outBuf)
	t.Logf("variants=%v childSessionUpdates=%d subagentToolCalls=%d",
		stats.variants, stats.childSessionUpdates, stats.subagentToolCalls)

	if stats.subagentToolCalls == 0 {
		t.Skip("model did not call the subagent tool this run — rerun the probe")
	}
	require.Positive(t, stats.childSessionUpdates,
		"OpenCode sent no child-tagged frames — it may have changed the projection; update the probe")
	require.NotNil(t, stats.sampleChildFrame, "no child frame captured")

	// The reproduction: a real child frame fed through ClawBench's own mapping
	// must produce NOTHING. On the unfixed tree this is where the probe fails.
	conn := newACPConn(&model.Agent{ID: "opencode-child-probe", Backend: "opencode"}, "opencode-child-probe-session")
	ch := make(chan StreamEvent, 64)
	mapACPSessionUpdate(stats.sampleChildFrame.Update, ch, context.Background(), conn, nil)
	assert.Zero(t, len(ch), "issue #639: child-session frame leaked into the main timeline: %v", drainStreamEvents(ch))
}
