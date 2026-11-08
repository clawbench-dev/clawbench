package ai

import (
	"context"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"clawbench/internal/model"
)

// acp_background_turn_test.go covers the adoption of agent-initiated turns
// (CodeBuddy background_drain, issue #644).
//
// The contract under test is narrow but the failure mode is total: without
// adoption every frame of that turn is dropped on an idle session, so the user
// sees a session that silently never answers again.

// unsolicitedTurnUpdate builds the announcement CodeBuddy sends before it starts
// streaming a background-drain turn.
func unsolicitedTurnUpdate(requestID, reason string) acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{
			SessionUpdate: "session_info_update",
			Meta: map[string]any{
				metaKeyCodeBuddyUnsolicitedTurn: map[string]any{
					"requestId": requestID,
					"reason":    reason,
				},
			},
		},
	}
}

// agentTextUpdate builds one assistant content frame.
func agentTextUpdate(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			SessionUpdate: "agent_message_chunk",
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: text},
			},
		},
	}
}

// awaitContent reads events until a content event arrives, ignoring the
// bookkeeping frames the mapping emits around it (e.g. thinking_done when the
// model moves from reasoning to text).
func awaitContent(t *testing.T, events <-chan StreamEvent) StreamEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type == "content" {
				return ev
			}
		case <-deadline:
			t.Fatal("content of the turn never arrived")
		}
	}
}

func TestCodeBuddyUnsolicitedTurn_ParsesOnlyDrainAnnouncements(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want string
		ok   bool
	}{
		{
			name: "drain announcement",
			meta: map[string]any{metaKeyCodeBuddyUnsolicitedTurn: map[string]any{
				"requestId": "cli-req-1", "reason": "background_drain",
			}},
			want: "cli-req-1",
			ok:   true,
		},
		{
			// A plain session_info_update (title/updatedAt) must stay a no-op.
			name: "no marker",
			meta: map[string]any{"codebuddy.ai/requestId": "x"},
		},
		{
			name: "unknown reason",
			meta: map[string]any{metaKeyCodeBuddyUnsolicitedTurn: map[string]any{
				"requestId": "cli-req-1", "reason": "something_else",
			}},
		},
		{
			name: "missing request id",
			meta: map[string]any{metaKeyCodeBuddyUnsolicitedTurn: map[string]any{
				"reason": "background_drain",
			}},
		},
		{
			name: "marker is not an object",
			meta: map[string]any{metaKeyCodeBuddyUnsolicitedTurn: "background_drain"},
		},
		{name: "nil meta"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := codeBuddyUnsolicitedTurn(tc.meta)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("codeBuddyUnsolicitedTurn = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestSessionUpdate_UnsolicitedTurn_AdoptsTurnAndRoutesContent is the core
// regression: the announcement must open a route BEFORE the lookup that used to
// drop it, so the content frames that follow land somewhere.
func TestSessionUpdate_UnsolicitedTurn_AdoptsTurnAndRoutesContent(t *testing.T) {
	type adoption struct {
		sid    string
		events <-chan StreamEvent
	}
	adopted := make(chan adoption, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	SetBackgroundTurnCallback(func(sid string, events <-chan StreamEvent, end func()) {
		adopted <- adoption{sid: sid, events: events}
		<-release
		end() // release the route, then report that the consumer is done
		close(finished)
	})
	t.Cleanup(func() { SetBackgroundTurnCallback(nil) })

	client := NewClawBenchACPClient()
	client.connRef = &ACPConn{clawbenchSID: "cb-1"}

	ctx := context.Background()
	if err := client.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: "acp-1",
		Update:    unsolicitedTurnUpdate("cli-req-1", "background_drain"),
	}); err != nil {
		t.Fatalf("SessionUpdate(announcement): %v", err)
	}

	var got adoption
	select {
	case got = <-adopted:
	case <-time.After(2 * time.Second):
		t.Fatal("agent-initiated turn was not adopted: no consumer was invoked")
	}
	if got.sid != "cb-1" {
		t.Fatalf("consumer got session %q, want the ClawBench session id", got.sid)
	}

	// The frames of that turn must reach the adopted channel instead of hitting
	// the "no route" drop.
	if err := client.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: "acp-1",
		Update:    agentTextUpdate("background task finished"),
	}); err != nil {
		t.Fatalf("SessionUpdate(content): %v", err)
	}
	if ev := awaitContent(t, got.events); ev.Content != "background task finished" {
		t.Fatalf("adopted stream got %+v, want the turn's content", ev)
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not return")
	}
	// The route is released once the consumer is done, so a later turn is not
	// routed into an abandoned channel.
	client.mu.Lock()
	_, stillRouted := client.sessionRoutes["acp-1"]
	client.mu.Unlock()
	if stillRouted {
		t.Fatal("adopted route was not released after the consumer finished")
	}
}

// TestSessionUpdate_UnsolicitedTurn_KeepsLiveTurnOwnership: the agent's premise
// is that the session is idle. If a user turn is streaming, its route must win —
// adopting would steal the frames of the live turn.
func TestSessionUpdate_UnsolicitedTurn_KeepsLiveTurnOwnership(t *testing.T) {
	adopted := make(chan struct{}, 1)
	SetBackgroundTurnCallback(func(string, <-chan StreamEvent, func()) { adopted <- struct{}{} })
	t.Cleanup(func() { SetBackgroundTurnCallback(nil) })

	client := NewClawBenchACPClient()
	client.connRef = &ACPConn{clawbenchSID: "cb-1"}

	live := make(chan StreamEvent, 8)
	client.RegisterSession("acp-1", live)

	if err := client.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "acp-1",
		Update:    unsolicitedTurnUpdate("cli-req-1", "background_drain"),
	}); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	select {
	case <-adopted:
		t.Fatal("adopted a turn while a live turn owned the session")
	case <-time.After(100 * time.Millisecond):
	}

	// The live turn still receives its frames.
	if err := client.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "acp-1",
		Update:    agentTextUpdate("live turn text"),
	}); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if ev := awaitContent(t, live); ev.Content != "live turn text" {
		t.Fatalf("live turn got %+v", ev)
	}
}

// sessionEndUpdate builds what the SDK hands to SessionUpdate for CodeBuddy's
// non-standard `session_end`: its union matches on the mere presence of
// `sessionUpdate`, so the frame arrives as a SessionInfoUpdate whose
// SessionUpdate field still holds the real discriminator and whose payload is
// gone.
func sessionEndUpdate() acp.SessionUpdate {
	return acp.SessionUpdate{
		SessionInfoUpdate: &acp.SessionSessionInfoUpdate{SessionUpdate: sessionEndVariant},
	}
}

// TestSessionEnd_ClosesOnlyAdoptedTurns: session_end is the adopted turn's only
// terminal signal, but it is also emitted at the end of ordinary turns. Only an
// adopted turn may be closed by it.
func TestSessionEnd_ClosesOnlyAdoptedTurns(t *testing.T) {
	client := NewClawBenchACPClient()
	client.connRef = &ACPConn{clawbenchSID: "cb-1"}
	ctx := context.Background()

	// Not adopted: nothing happens (an ordinary turn terminates on its
	// PromptResponse instead).
	if err := client.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: "acp-1", Update: sessionEndUpdate(),
	}); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if client.closeBackgroundTurnOnSessionEnd("acp-1") {
		t.Fatal("session_end claimed a turn that was never adopted")
	}

	turn, ok := client.beginBackgroundTurn("acp-1", "cli-req-1")
	if !ok {
		t.Fatal("beginBackgroundTurn refused an idle session")
	}
	if err := client.SessionUpdate(ctx, acp.SessionNotification{
		SessionId: "acp-1", Update: sessionEndUpdate(),
	}); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}

	select {
	case ev := <-turn.ch:
		if ev.Type != "done" {
			t.Fatalf("adopted turn got %+v, want a terminal done event", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("session_end did not terminate the adopted turn")
	}
}

// TestSessionEnd_IgnoresTurnWhoseRouteWasTakenOver: once a user turn replaces the
// route (RegisterSession), that turn's own session_end must not terminate the
// adopted turn that lost the route.
func TestSessionEnd_IgnoresTurnWhoseRouteWasTakenOver(t *testing.T) {
	client := NewClawBenchACPClient()
	client.connRef = &ACPConn{clawbenchSID: "cb-1"}

	turn, ok := client.beginBackgroundTurn("acp-1", "cli-req-1")
	if !ok {
		t.Fatal("beginBackgroundTurn refused an idle session")
	}

	// A user turn takes the session over. RegisterSession closes the adopted
	// turn out (so it does not hang), which is the terminal event we expect here.
	live := make(chan StreamEvent, 8)
	client.RegisterSession("acp-1", live)

	select {
	case ev := <-turn.ch:
		if ev.Type != "done" {
			t.Fatalf("adopted turn got %+v, want it closed on takeover", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("takeover did not close the adopted turn")
	}

	// A later session_end belongs to the live turn and must not be reported as
	// closing an adopted one.
	if client.closeBackgroundTurnOnSessionEnd("acp-1") {
		t.Fatal("session_end closed a turn whose route had been taken over")
	}
	select {
	case ev := <-live:
		t.Fatalf("live turn received an unexpected event: %+v", ev)
	default:
	}
}

// TestReplayFilter_AdoptedTurnRequestIDBypasses covers the production-only
// defect behind issue #644's follow-up: CodeBuddy reuses the PREVIOUS turn's
// requestId for the turn it starts on its own, so the replay filter dropped the
// whole reply and the user saw a lone tool card with no text.
func TestReplayFilter_AdoptedTurnRequestIDBypasses(t *testing.T) {
	conn := &ACPConn{agent: &model.Agent{Backend: "codebuddy"}}
	const rid = "reused-request-id"
	conn.setLastCompletedRequestID(rid)

	meta := map[string]any{metaKeyCodeBuddyRequestID: rid}
	if !isReplayedTurnMeta(conn, "codebuddy", meta) {
		t.Fatal("baseline should classify a completed turn's id as a replay")
	}

	conn.noteAdoptedTurnRequestID(rid)
	if isReplayedTurnMeta(conn, "codebuddy", meta) {
		t.Fatal("an adopted turn's own requestId must not be treated as a replay")
	}

	// A different id is still filtered: the whitelist is one id, not a switch.
	other := map[string]any{metaKeyCodeBuddyRequestID: "some-other-id"}
	if isReplayedTurnMeta(conn, "codebuddy", other) {
		t.Fatal("an unrelated id must keep its normal replay treatment")
	}

	conn.clearAdoptedTurnRequestID(rid)
	if !isReplayedTurnMeta(conn, "codebuddy", meta) {
		t.Fatal("clearing the marker must restore the filter")
	}
}
