package ai

import (
	"context"
	"os/exec"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

// acp_background_turn_watchdog_test.go covers the no-progress watchdog on an
// adopted (agent-initiated) turn.
//
// The failure this prevents: CodeBuddy announces a background drain, streams
// some content, then goes quiet. An adopted turn has no Prompt() call in
// flight, so nothing bounds it — before this, the bubble stayed "streaming"
// until the service layer's 30-minute backstop fired, and the user saw a reply
// that simply stopped mid-sentence.

// awaitDone reads events until the terminal arrives, returning it. Bookkeeping
// frames (thinking_done and friends) are skipped.
func awaitDone(t *testing.T, events <-chan StreamEvent, within time.Duration) StreamEvent {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("the turn's channel closed without a terminal event")
			}
			if ev.Type == "done" {
				return ev
			}
		case <-deadline:
			t.Fatalf("no terminal event within %s: the turn was left open", within)
		}
	}
}

// adoptStalledTurn drives one adoption against a connection whose watchdog
// window is 150ms, returning the adopted channel and the consumer's release
// latch. The consumer blocks until released or until the test ends, so the
// route stays owned by the adopted turn for the whole assertion.
type adoptedTurn struct {
	events  <-chan StreamEvent
	release chan struct{}
	done    chan struct{}
}

func adoptTurnWithWatchdog(t *testing.T) (*ClawBenchACPClient, adoptedTurn) {
	t.Helper()
	turn := adoptedTurn{release: make(chan struct{}), done: make(chan struct{})}
	adopted := make(chan adoptedTurn, 1)
	adoptedCh := make(chan (<-chan StreamEvent), 1)

	SetBackgroundTurnCallback(func(_ string, events <-chan StreamEvent, end func()) {
		adoptedCh <- events
		<-turn.release
		end()
		close(turn.done)
	})
	t.Cleanup(func() {
		SetBackgroundTurnCallback(nil)
		// Unblock a consumer the test abandoned, so the goroutine does not
		// outlive the test and race the next one's callback registration.
		select {
		case <-turn.release:
		default:
			close(turn.release)
		}
	})

	conn := &ACPConn{clawbenchSID: "cb-bg"}
	// Short window: the production default is 30 minutes, which no test can pay.
	conn.stallTimeout = 150 * time.Millisecond
	client := NewClawBenchACPClient()
	client.connRef = conn

	if err := client.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "acp-bg",
		Update:    unsolicitedTurnUpdate("cli-req-bg", "background_drain"),
	}); err != nil {
		t.Fatalf("SessionUpdate(announcement): %v", err)
	}

	select {
	case events := <-adoptedCh:
		turn.events = events
	case <-time.After(2 * time.Second):
		t.Fatal("agent-initiated turn was never adopted")
	}
	_ = adopted
	return client, turn
}

// TestBackgroundTurn_StallDeliversTimeoutTerminal is the core case: content
// arrives, then the agent goes silent. The turn must end on its own within the
// watchdog window, and it must report WHY — a silent `done` would be recorded
// as a normal completion and the user would see a truncated reply with no
// explanation.
func TestBackgroundTurn_StallDeliversTimeoutTerminal(t *testing.T) {
	_, turn := adoptTurnWithWatchdog(t)

	start := time.Now()
	ev := awaitDone(t, turn.events, 5*time.Second)
	elapsed := time.Since(start)

	if ev.Reason != ReasonStalled {
		t.Fatalf("terminal reason = %q, want %q: a stalled turn must not be reported as a normal completion", ev.Reason, ReasonStalled)
	}
	// The watchdog window is 150ms with a 15ms tick. Anything near the 30-minute
	// production backstop means the watchdog never ran and the service-layer
	// deadline is what closed the turn.
	if elapsed > 3*time.Second {
		t.Fatalf("turn took %s to close, want the 150ms watchdog window: the watchdog did not fire", elapsed)
	}
}

// TestBackgroundTurn_SessionEndStopsWatchdog: a turn that ends properly must
// not ALSO produce a watchdog terminal. Two terminals would make the executor
// finalize twice, and the second would land on an already-released route.
func TestBackgroundTurn_SessionEndStopsWatchdog(t *testing.T) {
	client, turn := adoptTurnWithWatchdog(t)
	if !client.closeBackgroundTurnOnSessionEnd("acp-bg") {
		t.Fatal("session_end did not close the adopted turn")
	}
	ev := awaitDone(t, turn.events, 2*time.Second)
	if ev.Reason != "" {
		t.Fatalf("a turn that ended on its own reported reason %q, want empty", ev.Reason)
	}

	// Wait past the watchdog window: the armed watchdog must have been stopped
	// by the session_end, so no second terminal arrives.
	select {
	case extra, ok := <-turn.events:
		if ok {
			t.Fatalf("a second event arrived after the terminal: %+v", extra)
		}
	case <-time.After(500 * time.Millisecond):
	}
}

// TestBackgroundTurn_UserTakeoverDisarmsWatchdog: when a user turn takes the
// session over, the adopted turn is closed deliberately. Its watchdog must be
// disarmed too, or it would fire against the route the user turn now owns.
func TestBackgroundTurn_UserTakeoverDisarmsWatchdog(t *testing.T) {
	client, turn := adoptTurnWithWatchdog(t)

	// RegisterSession is the takeover: it closes out the adopted turn and
	// installs the user turn's route.
	live := make(chan StreamEvent, 8)
	client.RegisterSession("acp-bg", live)

	ev := awaitDone(t, turn.events, 2*time.Second)
	// RegisterSession delivered that terminal itself; reason is empty because a
	// takeover is not a stall.
	if ev.Reason != "" {
		t.Fatalf("takeover terminal reason = %q, want empty", ev.Reason)
	}

	select {
	case extra := <-live:
		t.Fatalf("the disarmed watchdog wrote into the user turn's route: %+v", extra)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestBackgroundTurn_ToolInFlightIsNotAStall: a background turn running a long
// tool (a build, a test suite) produces no model output for minutes at a time.
// It must not be killed for that.
func TestBackgroundTurn_ToolInFlightIsNotAStall(t *testing.T) {
	client, turn := adoptTurnWithWatchdog(t)
	client.connRef.SetToolInFlight(true)

	select {
	case ev, ok := <-turn.events:
		if ok && ev.Type == "done" {
			t.Fatalf("watchdog ended a turn with a tool in flight: %+v", ev)
		}
	case <-time.After(600 * time.Millisecond):
	}

	// Clearing the flag re-arms the judgement: the turn is now genuinely stalled
	// and must close.
	client.connRef.SetToolInFlight(false)
	ev := awaitDone(t, turn.events, 3*time.Second)
	if ev.Reason != ReasonStalled {
		t.Fatalf("terminal reason = %q, want %q once the tool finished", ev.Reason, ReasonStalled)
	}
}

// TestBackgroundTurn_StallDoesNotKillTheAgentProcess: the agent process is
// shared with the user's session. A user turn kills on stall because its own
// Prompt RPC would otherwise hang; an adopted turn has no RPC to unblock, so
// ending the bubble must not take the conversation down with it.
func TestBackgroundTurn_StallDoesNotKillTheAgentProcess(t *testing.T) {
	client, turn := adoptTurnWithWatchdog(t)
	conn := client.connRef

	// A real (already-exited) process stands in for the agent. Both kill paths
	// — killAndMarkDead and close — reap it and then set c.cmd = nil, so a
	// surviving c.cmd is direct evidence that the stall path left the process
	// alone. alive is useless here: it is false on a fresh conn anyway.
	sentinel := exec.Command("true")
	if err := sentinel.Start(); err != nil {
		t.Fatalf("start sentinel process: %v", err)
	}
	_ = sentinel.Wait()
	conn.mu.Lock()
	conn.cmd = sentinel
	conn.mu.Unlock()

	ev := awaitDone(t, turn.events, 5*time.Second)
	if ev.Reason != ReasonStalled {
		t.Fatalf("terminal reason = %q, want %q", ev.Reason, ReasonStalled)
	}

	conn.mu.Lock()
	stillThere := conn.cmd == sentinel
	conn.mu.Unlock()
	if !stillThere {
		t.Fatal("the stall watchdog killed the shared agent process: a background turn must only end its own stream")
	}
}
