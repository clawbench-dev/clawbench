package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// TestBuiltinSummaryFromCompaction_RecordsTheFoldedTurnOnly pins the boundary a
// compaction writes.
//
// The old boundary was "the newest finalized message", which marked the turns the
// engine keeps VERBATIM as already-summarized: the next turn skipped them while
// the summary said nothing about them, so the model lost the very context it had
// just been told to keep in mind.
func TestBuiltinSummaryFromCompaction_RecordsTheFoldedTurnOnly(t *testing.T) {
	history := []ai.HistoryMessage{
		{Role: roleUser, Text: "q1", ID: 11},
		{Role: roleAssistant, Text: "a1", ID: 12},
		{Role: roleUser, Text: "q2", ID: 13},
		{Role: roleAssistant, Text: "a2", ID: 14},
	}
	// The engine folded only the first exchange.
	ev := ai.StreamEvent{
		Type:                      "compacted",
		Summary:                   "the user asked about q1",
		CompactedThroughHistory:   1,
		CompactedThroughMessageID: 12,
	}
	s := builtinSummaryFromCompaction(ev, history)
	if s.Text != ev.Summary {
		t.Fatalf("summary text lost: %q", s.Text)
	}
	if s.CoversThroughTurn != 2 {
		t.Fatalf("CoversThroughTurn = %d, want 2 (the first exchange)", s.CoversThroughTurn)
	}
	if s.UpToMessageID != 12 {
		t.Fatalf("UpToMessageID = %d, want 12 (the last FOLDED message, not the newest)", s.UpToMessageID)
	}
	if s.UpToMessageID == 14 {
		t.Fatal("the boundary must not be the newest message: that turn is still in the model's context")
	}
}

// TestBuiltinSummaryFromCompaction_NoUsableBoundaryWritesNone pins the safer
// failure: when the event names nothing the caller can map, no boundary is
// written at all, so the next turn REPLAYS the history instead of dropping it.
func TestBuiltinSummaryFromCompaction_NoUsableBoundaryWritesNone(t *testing.T) {
	history := []ai.HistoryMessage{{Role: roleUser, Text: "q1", ID: 11}}
	cases := []ai.StreamEvent{
		// Nothing folded.
		{Type: "compacted", Summary: "s", CompactedThroughHistory: -1},
		// An index this list cannot explain.
		{Type: "compacted", Summary: "s", CompactedThroughHistory: 7},
	}
	for _, ev := range cases {
		s := builtinSummaryFromCompaction(ev, history)
		if s.UpToMessageID != 0 || s.CoversThroughTurn != 0 {
			t.Fatalf("event %+v must write no boundary, got %+v", ev, s)
		}
	}
}

// TestBuiltinSummaryFromCompaction_FallsBackToTheTurnID pins the id path when the
// event carries only an index — the boundary still lands on the right message.
func TestBuiltinSummaryFromCompaction_FallsBackToTheTurnID(t *testing.T) {
	history := []ai.HistoryMessage{
		{Role: roleUser, Text: "q1", ID: 11},
		{Role: roleAssistant, Text: "a1", ID: 12},
		{Role: roleUser, Text: "q2", ID: 13},
	}
	s := builtinSummaryFromCompaction(ai.StreamEvent{
		Type: "compacted", Summary: "s", CompactedThroughHistory: 0,
	}, history)
	if s.UpToMessageID != 11 || s.CoversThroughTurn != 1 {
		t.Fatalf("expected the first turn's id 11 and 1 covered turn, got %+v", s)
	}
}

// TestAddMetadataUsage_AccumulatesAcrossRounds pins the accounting fix at the executor.
//
// The built-in agent used to send one metadata frame per model round. The
// executor REPLACED its accounting with each frame and the terminal done frame
// only fills empty fields, so a multi-round turn (any turn that calls a tool)
// recorded the LAST round's tokens instead of the total.
func TestAddMetadataUsage_AccumulatesAcrossRounds(t *testing.T) {
	e := &SessionExecutor{}
	dst := &ai.Metadata{Transport: "builtin", Model: "m1"}
	e.addMetadataUsage(dst, &ai.Metadata{InputTokens: 100, OutputTokens: 20})
	e.addMetadataUsage(dst, &ai.Metadata{InputTokens: 300, OutputTokens: 60})
	if dst.InputTokens != 400 || dst.OutputTokens != 80 {
		t.Fatalf("usage must sum across rounds, got %d/%d", dst.InputTokens, dst.OutputTokens)
	}
	// Non-usage fields are still filled only once: the second frame must not
	// erase the transport/model recorded by the first.
	if dst.Transport != "builtin" || dst.Model != "m1" {
		t.Fatalf("identity fields must be preserved, got %+v", dst)
	}
	// The terminal frame then merges its totals on top without double-counting.
	ai.MergeMetadata(dst, &ai.Metadata{InputTokens: 400, OutputTokens: 80, StopReason: "complete"})
	if dst.InputTokens != 400 {
		t.Fatalf("merging the same total must not double it, got %d", dst.InputTokens)
	}
	if dst.StopReason != "complete" {
		t.Fatalf("the stop reason must be recorded, got %q", dst.StopReason)
	}
}

// TestBuiltinTurn_RecordsCumulativeUsage is the end-to-end counterpart: a turn
// with two model rounds (one tool call) must record the SUM of both rounds in the
// assistant message metadata, not the last round's figure.
func TestBuiltinTurn_RecordsCumulativeUsage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	var round atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := round.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"path\":\"a.txt\"}"}}]}}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":100}}`+"\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		if !strings.Contains(string(body), "hello") {
			t.Errorf("the tool result must reach the second round, body: %.300s", body)
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"done"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":2000,"completion_tokens":200}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	withBuiltinAgent(t, srv.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	backend := newBuiltinBackendForTest()
	ch, err := backend.ExecuteStream(context.Background(), ai.ChatRequest{
		AgentID: BuiltinAgentID,
		Prompt:  "read a.txt",
		WorkDir: root,
	})
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}

	// Replay the executor's accounting rules.
	var (
		mu    sync.Mutex
		dst   = &ai.Metadata{}
		done  *ai.Metadata
		metas []*ai.Metadata
	)
	for ev := range ch {
		switch ev.Type {
		case "metadata":
			if ev.Meta != nil {
				metas = append(metas, ev.Meta)
				mu.Lock()
				e := &SessionExecutor{}
				e.addMetadataUsage(dst, ev.Meta)
				mu.Unlock()
			}
		case "done":
			done = ev.Meta
		}
	}
	if done == nil {
		t.Fatal("the turn must end with a done frame")
	}
	ai.MergeMetadata(dst, done)

	if dst.InputTokens != 3000 || dst.OutputTokens != 300 {
		t.Fatalf("expected the cumulative usage (3000/300), got %d/%d via %s",
			dst.InputTokens, dst.OutputTokens, describeMeta(metas))
	}
}

// describeMeta renders the per-round frames for a failure message.
func describeMeta(list []*ai.Metadata) string {
	parts := make([]string, 0, len(list))
	for _, m := range list {
		parts = append(parts, fmt.Sprintf("%d/%d", m.InputTokens, m.OutputTokens))
	}
	return strings.Join(parts, ", ")
}
