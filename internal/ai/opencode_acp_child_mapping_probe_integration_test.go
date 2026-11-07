//go:build integration

package ai

// ===========================================================================
// OpenCode v2 ACP — sub-agent attribution probe (issue #639 follow-up)
// ===========================================================================
//
// Question: how do we map a child session's frames back to the parent
// `subagent` tool call so they can be folded under the Agent card?
//
// OpenCode's child frames carry only `_meta["opencode/child-session"]`
// {id, parentID, depth, title} — no parent tool-call id. This probe runs a REAL
// opencode session twice (foreground and background subagent) and reports:
//   - the parent subagent tool call(s): id, input (agent/description/background),
//   - the child session id/title from the child frames,
//   - the tool result's rawOutput.metadata.sessionID,
//   - the wire order of parent call / first child frame / parent result.
//
// The point is to decide whether `description == child.title` and
// `metadata.sessionID == child.id` hold, and whether the exact id arrives
// before (background) or after (foreground) the child frames.
//
// Run:
//
//	go test -v -run 'TestOpenCodeACP_ChildMappingProbe' -tags integration \
//	    -timeout 900s ./internal/ai/
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
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

const openCodeChildMappingForegroundPrompt = `You MUST delegate this task to a subagent using the subagent tool. Do not do the work yourself.
Use these arguments:
- agent: pick any available subagent listed in the tool description
- description: "fg mapping probe"
- prompt: "Reply with the single word FG_DONE. Do not use any tools."
- background: false
After the subagent returns, reply with the single word PARENT_DONE.`

const openCodeChildMappingBackgroundPrompt = `You MUST launch a background subagent using the subagent tool with background: true. Do not do the work yourself.
Use these arguments:
- agent: pick any available subagent listed in the tool description
- description: "bg mapping probe"
- prompt: "Reply immediately with the single word BG_DONE. Do not use any tools."
- background: true
After launching it, reply with the single word PARENT_DONE immediately.`

// opencodeChildMappingProbe is one live opencode ACP session with the raw wire
// teed. It reuses the plan probe's auto-approving client.
type opencodeChildMappingProbe struct {
	client *opencodePlanProbeClient
	conn   *acp.ClientSideConnection
	id     string
	outBuf *bytes.Buffer
}

func startOpencodeChildMappingProbe(t *testing.T, ctx context.Context, advertiseChildUpdates bool) *opencodeChildMappingProbe {
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
	caps := acp.ClientCapabilities{
		Fs:       acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true},
		Terminal: true,
	}
	if advertiseChildUpdates {
		caps.Meta = map[string]any{"opencode/child-session-updates": true}
	}
	_, err = conn.Initialize(initCtx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: caps,
		ClientInfo:         &acp.Implementation{Name: "clawbench-opencode-child-mapping-probe", Version: "1.0.0"},
	})
	require.NoError(t, err, "initialize")

	client.connRef = newACPConn(&model.Agent{
		ID: "opencode-child-mapping-probe", Backend: "opencode", Transport: "acp-stdio",
	}, "opencode-child-mapping-probe-session")

	newCtx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
	defer cancel2()
	resp, err := conn.NewSession(newCtx, acp.NewSessionRequest{Cwd: t.TempDir(), McpServers: []acp.McpServer{}})
	require.NoError(t, err, "session/new")

	return &opencodeChildMappingProbe{client: client, conn: conn, id: string(resp.SessionId), outBuf: outBuf}
}

func (p *opencodeChildMappingProbe) prompt(ctx context.Context, timeout time.Duration, text string) error {
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := p.conn.Prompt(turnCtx, acp.PromptRequest{
		SessionId: acp.SessionId(p.id),
		Prompt:    []acp.ContentBlock{acp.TextBlock(text)},
	})
	return err
}

// mappingParentCall is the merged view of one subagent tool call.
type mappingParentCall struct {
	ID          string
	Title       string
	Description string
	Agent       string
	Background  bool
	Status      string
	ResultSID   string // rawOutput.metadata.sessionID
	FirstIndex  int    // first frame that mentioned this id
	ResultIndex int    // index of the terminal frame (status completed/failed)
}

// mappingChildFrame is one child-session-tagged update.
type mappingChildFrame struct {
	Index     int
	Variant   string
	SessionID string
	ParentID  string
	Depth     int
	Title     string
}

type mappingChildNotification struct {
	Index           int
	RootSessionID   string
	ChildSessionID  string
	ParentSessionID string
	Depth           int
	Title           string
	Type            string // "update" or "status"
	Variant         string
	Status          string
}

type openCodeChildMappingStats struct {
	parents       []*mappingParentCall
	childs        []mappingChildFrame
	childNotifies []mappingChildNotification
}

// analyzeOpenCodeChildMapping walks the raw agent→client wire in order.
func analyzeOpenCodeChildMapping(buf *bytes.Buffer) openCodeChildMappingStats {
	stats := openCodeChildMappingStats{}
	byID := map[string]*mappingParentCall{}

	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	for idx, raw := range lines {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var env struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &env) != nil {
			continue
		}
		if env.Method == "opencode/session/child_update" {
			var n struct {
				RootSessionID   string `json:"rootSessionId"`
				ChildSessionID  string `json:"childSessionId"`
				ParentSessionID string `json:"parentSessionId"`
				Depth           int    `json:"depth"`
				Title           string `json:"title"`
				Type            string `json:"type"`
				Status          string `json:"status"`
				Update          struct {
					SessionUpdate string `json:"sessionUpdate"`
				} `json:"update"`
			}
			if json.Unmarshal(env.Params, &n) == nil {
				stats.childNotifies = append(stats.childNotifies, mappingChildNotification{
					Index: idx, RootSessionID: n.RootSessionID, ChildSessionID: n.ChildSessionID,
					ParentSessionID: n.ParentSessionID, Depth: n.Depth, Title: n.Title,
					Type: n.Type, Variant: n.Update.SessionUpdate, Status: n.Status,
				})
			}
			continue
		}
		if env.Method != "session/update" {
			continue
		}
		var n acp.SessionNotification
		if json.Unmarshal(env.Params, &n) != nil {
			continue
		}

		if probeIsChildSessionUpdate(n.Update) {
			meta := acpUpdateMeta(n.Update)
			ns, _ := meta[probeChildSessionMetaKey].(map[string]any)
			frame := mappingChildFrame{
				Index:     idx,
				Variant:   typedVariantName(n.Update),
				SessionID: metaString(ns["id"]),
				ParentID:  metaString(ns["parentID"]),
				Title:     metaString(ns["title"]),
			}
			if d, ok := ns["depth"].(float64); ok {
				frame.Depth = int(d)
			}
			stats.childs = append(stats.childs, frame)
			continue
		}

		switch {
		case n.Update.ToolCall != nil:
			tc := n.Update.ToolCall
			if !strings.Contains(strings.ToLower(tc.Title), "subagent") {
				continue
			}
			call := byID[string(tc.ToolCallId)]
			if call == nil {
				call = &mappingParentCall{ID: string(tc.ToolCallId), Title: tc.Title, FirstIndex: idx}
				byID[call.ID] = call
				stats.parents = append(stats.parents, call)
			}
			mergeMappingParentInput(call, tc.RawInput)
			call.Status = string(tc.Status)

		case n.Update.ToolCallUpdate != nil:
			tcu := n.Update.ToolCallUpdate
			call := byID[string(tcu.ToolCallId)]
			if call == nil {
				// The pending tool_call frame was not matched (different title);
				// only treat updates whose title mentions subagent as parents.
				title := ""
				if tcu.Title != nil {
					title = *tcu.Title
				}
				if !strings.Contains(strings.ToLower(title), "subagent") {
					continue
				}
				call = &mappingParentCall{ID: string(tcu.ToolCallId), Title: title, FirstIndex: idx}
				byID[call.ID] = call
				stats.parents = append(stats.parents, call)
			}
			if tcu.RawInput != nil {
				mergeMappingParentInput(call, tcu.RawInput)
			}
			if tcu.Status != nil {
				call.Status = string(*tcu.Status)
				if *tcu.Status == acp.ToolCallStatusCompleted || *tcu.Status == acp.ToolCallStatusFailed {
					call.ResultIndex = idx
				}
			}
			if m := rawOutputMetadata(tcu.RawOutput); m != nil {
				call.ResultSID = metaString(m["sessionID"])
			}
		}
	}
	return stats
}

func mergeMappingParentInput(call *mappingParentCall, raw any) {
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return
	}
	if v := metaString(m["description"]); v != "" {
		call.Description = v
	}
	if v := metaString(m["agent"]); v != "" {
		call.Agent = v
	}
	if v, ok := m["background"].(bool); ok {
		call.Background = v
	}
}

func rawOutputMetadata(raw any) map[string]any {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	meta, _ := m["metadata"].(map[string]any)
	return meta
}

// TestOpenCodeACP_ChildMappingProbe reports the child-session ↔ parent-tool
// relationship for foreground and background subagents.
func TestOpenCodeACP_ChildMappingProbe(t *testing.T) {
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode CLI not installed")
	}
	ctx, cancel := contextWithTimeout(t, 840*time.Second)
	defer cancel()

	run := func(t *testing.T, name, prompt string, waitAfter time.Duration, advertise bool) openCodeChildMappingStats {
		probe := startOpencodeChildMappingProbe(t, ctx, advertise)
		require.NoError(t, probe.prompt(ctx, 360*time.Second, prompt), "prompt")
		time.Sleep(waitAfter) // let a background child's trailing frames land
		stats := analyzeOpenCodeChildMapping(probe.outBuf)

		t.Logf("=== %s ===", name)
		for _, p := range stats.parents {
			t.Logf("parent id=%s title=%q desc=%q agent=%q background=%v status=%q resultSID=%q firstIdx=%d resultIdx=%d",
				p.ID, p.Title, p.Description, p.Agent, p.Background, p.Status, p.ResultSID, p.FirstIndex, p.ResultIndex)
		}
		for _, c := range stats.childs {
			t.Logf("child  idx=%d variant=%s sid=%s parentSID=%s depth=%d title=%q",
				c.Index, c.Variant, c.SessionID, c.ParentID, c.Depth, c.Title)
		}
		for _, n := range stats.childNotifies {
			t.Logf("notify idx=%d type=%s status=%q variant=%s child=%s parent=%s depth=%d title=%q",
				n.Index, n.Type, n.Status, n.Variant, n.ChildSessionID, n.ParentSessionID, n.Depth, n.Title)
		}
		return stats
	}

	t.Run("foreground", func(t *testing.T) {
		stats := run(t, "foreground", openCodeChildMappingForegroundPrompt, 2*time.Second, false)
		if len(stats.parents) == 0 || len(stats.childs) == 0 {
			t.Skip("model did not use a subagent this run — rerun the probe")
		}
		assertMappingRelationship(t, stats)
	})

	t.Run("background", func(t *testing.T) {
		stats := run(t, "background", openCodeChildMappingBackgroundPrompt, 35*time.Second, true)
		if len(stats.parents) == 0 || len(stats.childs) == 0 {
			t.Skip("model did not launch a background subagent this run — rerun the probe")
		}
		assertMappingRelationship(t, stats)
	})
}

// assertMappingRelationship checks the two invariants the fold-under-card
// implementation depends on:
//  1. child.title equals the parent tool call's input.description,
//  2. the parent result's metadata.sessionID equals the child session id.
func assertMappingRelationship(t *testing.T, stats openCodeChildMappingStats) {
	t.Helper()
	byDesc := map[string]*mappingParentCall{}
	for _, p := range stats.parents {
		if p.Description != "" {
			byDesc[p.Description] = p
		}
	}
	bySID := map[string]*mappingParentCall{}
	for _, p := range stats.parents {
		if p.ResultSID != "" {
			bySID[p.ResultSID] = p
		}
	}
	for _, c := range stats.childs {
		if c.Title == "" {
			continue
		}
		if p := byDesc[c.Title]; p != nil {
			t.Logf("title match: child %q -> parent %s", c.Title, p.ID)
		} else {
			t.Errorf("no parent call has description %q (child %s)", c.Title, c.SessionID)
		}
		if p := bySID[c.SessionID]; p != nil {
			t.Logf("session-id match: child %s -> parent %s", c.SessionID, p.ID)
		}
	}
	// At least one child must be attributable by session id once the result is
	// in (foreground completes before the prompt returns; background returns the
	// id immediately).
	if len(bySID) == 0 {
		t.Error("no parent tool result carried metadata.sessionID")
	}
}
