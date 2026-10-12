//go:build integration

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// ===========================================================================
// OpenCode ACP — plan / todo panel wire probe
// ===========================================================================
//
// Question: does `opencode acp` push anything the ClawBench PlanPanel
// ("执行计划" checklist) can render? Two candidate channels exist:
//
//	A. The ACP spec `session/update` variant `plan` (full-snapshot replace),
//	   which mapACPSessionUpdate maps to a `plan_update` StreamEvent.
//	B. An ordinary tool call named `todowrite` (OpenCode's TodoWrite tool),
//	   whose rawInput/rawOutput carries the checklist. ClawBench currently maps
//	   the tool name to "TodoWrite" but does NOT translate its payload into a
//	   plan — only CodeBuddy has such a bridge (codebuddy_plan_bridge.go).
//
// This probe drives a REAL opencode process, tees the full JSON-RPC wire in
// both directions, auto-approves permission requests, and dumps:
//   - every distinct session/update variant seen (raw discriminator),
//   - every `plan` / `plan_update` / `plan_removed` frame in full,
//   - every tool_call / tool_call_update frame whose title/kind mentions todo,
//   - any extension variant the SDK misclassifies (raw bypass),
//   - the typed StreamEvents that reached the ClawBench client (plan_update,
//     TodoWrite tool_use/tool_result).
//
// Run:
//
//	go test -v -run 'TestOpenCodeACP_PlanPanelProbe' -tags integration \
//	    -timeout 420s ./internal/ai/
//
// Requires the `opencode` CLI installed and authenticated.

// opencodePlanProbeClient captures everything AND auto-approves permissions so
// the turn can finish unattended. It reuses bypassProbeClient's variant /
// custom-method / typed-variant recording (same package, same build tag).
type opencodePlanProbeClient struct {
	*bypassProbeClient

	permMu       sync.Mutex
	permRequests []string
}

func newOpencodePlanProbeClient() *opencodePlanProbeClient {
	return &opencodePlanProbeClient{bypassProbeClient: newBypassProbeClient()}
}

func (c *opencodePlanProbeClient) permissionRequests() []string {
	c.permMu.Lock()
	defer c.permMu.Unlock()
	return append([]string(nil), c.permRequests...)
}

// RequestPermission auto-selects an "allow" option (the first allow_once /
// allow_always), falling back to the first option. Without this the turn would
// block on a pending permission no frontend can answer.
func (c *opencodePlanProbeClient) RequestPermission(_ context.Context, p acp.RequestPermissionRequest) (acp.RequestPermissionResponse, error) {
	title := ""
	if p.ToolCall.Title != nil {
		title = *p.ToolCall.Title
	}
	c.permMu.Lock()
	c.permRequests = append(c.permRequests, fmt.Sprintf("toolCall=%s title=%q options=%d",
		p.ToolCall.ToolCallId, title, len(p.Options)))
	c.permMu.Unlock()

	for _, o := range p.Options {
		if o.Kind == acp.PermissionOptionKindAllowOnce || o.Kind == acp.PermissionOptionKindAllowAlways {
			return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(o.OptionId)}, nil
		}
	}
	if len(p.Options) > 0 {
		return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeSelected(p.Options[0].OptionId)}, nil
	}
	return acp.RequestPermissionResponse{Outcome: acp.NewRequestPermissionOutcomeCancelled()}, nil
}

// opencodePlanProbeSession is one live opencode ACP session with raw wire tee.
type opencodePlanProbeSession struct {
	client *opencodePlanProbeClient
	conn   *acp.ClientSideConnection
	filter *acpStdoutFilter
	id     string
	outBuf *bytes.Buffer // agent -> client
	inBuf  *bytes.Buffer // client -> agent
}

func startOpencodePlanProbe(t *testing.T, ctx context.Context, cmd *exec.Cmd, client *opencodePlanProbeClient) *opencodePlanProbeSession {
	t.Helper()

	agentOut, err := cmd.StdoutPipe()
	require.NoError(t, err, "stdout pipe")
	agentIn, err := cmd.StdinPipe()
	require.NoError(t, err, "stdin pipe")
	cmd.Stderr = os.Stderr

	outBuf := &bytes.Buffer{}
	inBuf := &bytes.Buffer{}
	recOut := &recordingReader{src: agentOut, buf: outBuf}
	recIn := &recordingWriter{dst: agentIn, buf: inBuf}

	// Tee into the stdout filter as well, so the SDK's misclassified-variant
	// bypass (and custom-method handler) see the same lines the SDK does.
	filter := newACPStdoutFilter(recOut)
	t.Cleanup(filter.Close)

	conn := acp.NewClientSideConnection(client, recIn, filter)
	conn.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
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
		ClientInfo: &acp.Implementation{Name: "clawbench-opencode-plan-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	// Mirror production wiring (acp_conn_lifecycle.go: the client's connRef
	// back-reference). Without it mapACPSessionUpdate sees backendID="" and the
	// opencode plan bridge (and any backend-gated logic) is skipped — a probe
	// harness gap, not a product path.
	connRef := newACPConn(&model.Agent{
		ID: "opencode-plan-probe", Backend: "opencode", Transport: "acp-stdio",
	}, "opencode-plan-probe-session")
	client.connRef = connRef

	// A fresh temp dir keeps the agent's file writes out of the repo. opencode
	// works fine in a non-git directory.
	workDir := t.TempDir()
	newCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
	defer cancel2()
	resp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: workDir, McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")

	return &opencodePlanProbeSession{
		client: client, conn: conn, filter: filter,
		id: string(resp.SessionId), outBuf: outBuf, inBuf: inBuf,
	}
}

func (s *opencodePlanProbeSession) prompt(ctx context.Context, timeout time.Duration, text string) (acp.PromptResponse, error) {
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.conn.Prompt(turnCtx, acp.PromptRequest{
		SessionId: acp.SessionId(s.id),
		Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
	})
}

// sessionUpdateFrame is one decoded session/update wire message.
type sessionUpdateFrame struct {
	Variant string
	Update  json.RawMessage
}

// decodeSessionUpdateFrames pulls the raw discriminator out of every
// session/update line (before the SDK's typed union can misclassify it).
func decodeSessionUpdateFrames(msgs [][]byte) []sessionUpdateFrame {
	var out []sessionUpdateFrame
	for _, m := range msgs {
		var env struct {
			Method string `json:"method"`
			Params struct {
				Update json.RawMessage `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(m, &env) != nil || env.Method != "session/update" {
			continue
		}
		var disc struct {
			SessionUpdate string `json:"sessionUpdate"`
		}
		_ = json.Unmarshal(env.Params.Update, &disc)
		out = append(out, sessionUpdateFrame{Variant: disc.SessionUpdate, Update: env.Params.Update})
	}
	return out
}

// looksLikeTodo reports whether a tool frame is OpenCode's TodoWrite (title or
// kind mentions todo). OpenCode's ACP tool_call titles are human-readable, e.g.
// "TodoWrite" or "Update todos".
func looksLikeTodo(title, kind string) bool {
	l := strings.ToLower(title + " " + kind)
	return strings.Contains(l, "todo")
}

// opencodePlanPrompt asks for a multi-step plan first (forcing TodoWrite) and
// then executes it, so both the tool-call channel and any native plan channel
// have a chance to fire.
const opencodePlanPrompt = `Use the todowrite tool to record a plan with exactly three steps:
1. create a file named a.txt containing the text "a"
2. create a file named b.txt containing the text "b"
3. list the current directory
Then carry out all three steps in order using the write and bash tools.
Finally reply with the single word DONE.`

// TestOpenCodeACP_PlanPanelProbe is the probe entry point.
func TestOpenCodeACP_PlanPanelProbe(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode CLI not installed")
	}
	ctx, cancel := contextWithTimeout(t, 420*time.Second)
	defer cancel()

	client := newOpencodePlanProbeClient()
	cmd := exec.CommandContext(ctx, "opencode", "acp")
	cmd.Env = append(os.Environ(), OrphanChildEnvVar)

	sess := startOpencodePlanProbe(t, ctx, cmd, client)
	t.Logf("opencode ACP session: %s", sess.id)

	streamCh := make(chan StreamEvent, 16384)
	client.RegisterSession(sess.id, streamCh)

	var planEvents []StreamEvent
	var todoToolEvents []StreamEvent
	var allToolNames []string

	promptCtx, promptCancel := context.WithTimeout(ctx, 300*time.Second)
	defer promptCancel()
	promptDone := make(chan struct{})
	var promptResp acp.PromptResponse
	var promptErr error
	go func() {
		promptResp, promptErr = sess.prompt(promptCtx, 300*time.Second, opencodePlanPrompt)
		close(promptDone)
	}()

	waiting := true
	for waiting {
		select {
		case <-promptDone:
			waiting = false
		case evt, ok := <-streamCh:
			if !ok {
				continue
			}
			switch evt.Type {
			case "plan_update":
				planEvents = append(planEvents, evt)
			case "tool_use", "tool_result":
				if evt.Tool != nil {
					allToolNames = append(allToolNames, evt.Type+":"+evt.Tool.Name)
					if strings.EqualFold(evt.Tool.Name, "TodoWrite") {
						todoToolEvents = append(todoToolEvents, evt)
					}
				}
			case "error":
				t.Logf("stream error event: %s", evt.Error)
			}
		case <-time.After(300 * time.Second):
			t.Log("probe: timed out waiting for prompt completion")
			waiting = false
		}
	}
	// Let any trailing frames land.
	time.Sleep(1500 * time.Millisecond)

	rec := &acpRawTraffic{
		fromAgent: parseWireMessages(sess.outBuf),
		toAgent:   parseWireMessages(sess.inBuf),
	}
	frames := decodeSessionUpdateFrames(rec.fromAgent)

	// ── 1. Variant histogram (raw discriminators, pre-SDK) ──────────────────
	t.Log("=== 1. session/update variant histogram (raw wire) ===")
	hist := map[string]int{}
	for _, f := range frames {
		hist[f.Variant]++
	}
	keys := make([]string, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("  %-28s x%d", k, hist[k])
	}

	// ── 2. Native plan frames in full ──────────────────────────────────────
	t.Log("=== 2. native ACP `plan` / `plan_update` / `plan_removed` frames ===")
	nativePlanCount := 0
	for _, f := range frames {
		switch f.Variant {
		case "plan", "plan_update", "plan_removed":
			nativePlanCount++
			t.Logf("  [%s] %s", f.Variant, compactJSON(f.Update))
		}
	}
	if nativePlanCount == 0 {
		t.Log("  <none> — opencode emitted NO native plan session/update")
	}

	// ── 3. TodoWrite tool frames (rawInput / rawOutput) ────────────────────
	t.Log("=== 3. todo-bearing tool_call / tool_call_update frames ===")
	todoFrameCount := 0
	for _, m := range rec.fromAgent {
		var env struct {
			Method string `json:"method"`
			Params struct {
				Update struct {
					SessionUpdate string          `json:"sessionUpdate"`
					ToolCallID    string          `json:"toolCallId"`
					Title         string          `json:"title"`
					Kind          string          `json:"kind"`
					Status        string          `json:"status"`
					RawInput      json.RawMessage `json:"rawInput"`
					RawOutput     json.RawMessage `json:"rawOutput"`
				} `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(m, &env) != nil || env.Method != "session/update" {
			continue
		}
		u := env.Params.Update
		if u.SessionUpdate != "tool_call" && u.SessionUpdate != "tool_call_update" {
			continue
		}
		if !looksLikeTodo(u.Title, u.Kind) {
			continue
		}
		todoFrameCount++
		t.Logf("  [%s] id=%s title=%q kind=%q status=%q",
			u.SessionUpdate, u.ToolCallID, u.Title, u.Kind, u.Status)
		if len(u.RawInput) > 0 {
			t.Logf("      rawInput: %s", compactJSON(u.RawInput))
		}
		if len(u.RawOutput) > 0 {
			t.Logf("      rawOutput: %s", compactJSON(u.RawOutput))
		}
	}
	if todoFrameCount == 0 {
		t.Log("  <none> — no todo-bearing tool frame observed")
	}

	// ── 4. Extension variants / custom methods (SDK bypass) ────────────────
	variants, custom, vSamples, cSamples, typed, typedCalls, typedSkipped := client.snapshot()
	t.Log("=== 4. SDK-bypass extension surface ===")
	t.Logf("  raw extension variants: %v", variants)
	for i, s := range vSamples {
		t.Logf("    [%d] %s params=%.200s", i, s.Variant, string(s.Params))
	}
	t.Logf("  custom notification methods: %v", custom)
	for i, n := range cSamples {
		t.Logf("    [%d] %s params=%.200s", i, n.Method, string(n.Params))
	}
	t.Logf("  typed variants delivered: %v (total %d, guard-skipped %d)", typed, typedCalls, typedSkipped)

	// ── 5. Typed StreamEvents that reached the ClawBench client ────────────
	t.Log("=== 5. typed StreamEvents (what the frontend would receive) ===")
	t.Logf("  plan_update events: %d", len(planEvents))
	for i, evt := range planEvents {
		t.Logf("    [%d] %s", i, mustJSON(t, evt.Plan))
	}
	t.Logf("  TodoWrite tool events: %d", len(todoToolEvents))
	for i, evt := range todoToolEvents {
		t.Logf("    [%d] type=%s name=%s done=%v input=%.200s output=%.200s",
			i, evt.Type, evt.Tool.Name, evt.Tool.Done, evt.Tool.Input, evt.Tool.Output)
	}
	t.Logf("  all tool names seen: %v", allToolNames)
	t.Logf("  permission requests auto-approved: %v", client.permissionRequests())

	// ── 6. Prompt outcome + raw dump ───────────────────────────────────────
	t.Log("=== 6. Prompt outcome ===")
	t.Logf("  stop_reason=%v", promptResp.StopReason)
	if promptErr != nil {
		t.Logf("  prompt error: %v", promptErr)
	}
	require.NoError(t, promptErr, "prompt should complete without error")

	t.Log("=== 7. Full raw JSON-RPC traffic ===")
	rec.dump(t)

	// ── Conclusion ─────────────────────────────────────────────────────────
	t.Log("=== CONCLUSION ===")
	switch {
	case nativePlanCount > 0:
		t.Logf("CONFIRMED: opencode pushes the native ACP `plan` channel (%d frame(s)); "+
			"PlanPanel gets data via the existing plan_update path.", nativePlanCount)
	case todoFrameCount > 0:
		t.Logf("CONFIRMED: opencode has NO native plan frames, but exposes the checklist "+
			"through %d TodoWrite tool frame(s). The opencode_plan_bridge should have "+
			"translated the terminal frames into plan_update events — see §5.", todoFrameCount)
	default:
		t.Log("NOTICE: neither native plan frames nor TodoWrite tool frames observed this run " +
			"(model-dependent) — see §1 histogram and §5 tool names for what did fire.")
	}

	// The bridge (opencode_plan_bridge.go) must turn the todowrite snapshot into
	// plan_update events the PlanPanel can render. One per terminal todowrite
	// invocation; assert at least one when the model used the tool.
	if todoFrameCount > 0 {
		require.NotEmpty(t, planEvents,
			"bridge FAILED: todowrite frames were observed but no plan_update reached the client")
		last := planEvents[len(planEvents)-1]
		require.NotNil(t, last.Plan)
		require.NotEmpty(t, last.Plan.Entries, "bridged plan must carry entries")
		for _, e := range last.Plan.Entries {
			assert.Contains(t, []string{"pending", "in_progress", "completed"}, e.Status,
				"bridged entry status must use the PlanPanel vocabulary")
		}
		t.Logf("CONFIRMED: bridge produced %d plan_update event(s); last snapshot has %d entr(ies)",
			len(planEvents), len(last.Plan.Entries))
	}
}
