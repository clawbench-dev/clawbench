package ai

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// acp_background_turn.go adopts a turn the AGENT starts on its own.
//
// A background task (run_in_background Bash, a detached sub-agent) finishes long
// after the user's turn returned. CodeBuddy then opens a NEW turn on the idle
// session: it first announces it with
//
//	session_info_update._meta["codebuddy.ai/unsolicitedTurn"] = {requestId, reason:"background_drain"}
//
// and streams that turn's content immediately afterwards (see
// docs/dev/codebuddy_acp_extensions.md §5, "非用户发起的 drain 轮").
//
// The host must open a turn for it. Without one, every frame of that turn hits
// the "no route for this session" drop in SessionUpdate — the session then goes
// silent forever, which is exactly issue #644: the agent says "I'll wake you up
// when it's done" and nothing ever appears afterwards.
//
// This file only ADOPTS the turn: it registers a stream route and hands the
// channel to the service layer, which owns persistence and the UI. Content
// interpretation is unchanged — the adopted frames go through the normal
// mapping, so tool calls, thinking and usage all behave as in a user turn.

const (
	// metaKeyCodeBuddyUnsolicitedTurn is the CodeBuddy _meta key announcing a
	// turn the agent started by itself.
	metaKeyCodeBuddyUnsolicitedTurn = "codebuddy.ai/unsolicitedTurn"
	// backgroundTurnReasonDrain is the only reason this host adopts. A future
	// reason would need its own policy (and its own consumer), so it is not
	// silently treated as a drain.
	backgroundTurnReasonDrain = "background_drain"
	// sessionEndVariant is CodeBuddy's non-standard "this turn is over" frame.
	// It is read from the RAW params in the SDK's unhandled hook
	// (dispatchSessionUpdateVariant in acp_ext.go), NOT from the typed struct:
	// since schema 1.32 the union rejects a foreign discriminator at decode time
	// (v0.13.5 re-typed it as SessionInfoUpdate). Reading it off the typed path
	// is why issue #644 regressed — an adopted turn never saw its terminal.
	sessionEndVariant = "session_end"
)

// backgroundTurn is one adopted agent-initiated turn.
type backgroundTurn struct {
	// ch is the stream channel the turn's frames are delivered on. It is never
	// closed by this package: the raw notification pump and the SDK's typed
	// callback run on different goroutines, so a close here could race a send
	// and panic. The consumer stops reading and the route is dropped instead.
	ch chan StreamEvent
	// route is the same channel as the send-only view stored in sessionRoutes.
	// Kept so route ownership can be compared without a type conversion.
	route chan<- StreamEvent
	// requestID is the CLI-assigned id of the turn (from the announcement).
	requestID string
	// stopStallWatchdog releases the no-progress watchdog armed for this turn.
	// Idempotent; nil before the watchdog is armed (e.g. a consumer-registered
	// adoption that failed).
	stopStallWatchdog func()
}

// stopWatchdog releases this turn's watchdog if one was armed. Safe on a turn
// that never armed one.
func (t *backgroundTurn) stopWatchdog() {
	if t != nil && t.stopStallWatchdog != nil {
		t.stopStallWatchdog()
	}
}

// codeBuddyUnsolicitedTurn reports the requestId of an agent-initiated turn
// announcement. ok is false for any other _meta shape, including a plain
// session_info_update (title/updatedAt) and a turn announced for a reason this
// host does not handle.
func codeBuddyUnsolicitedTurn(meta map[string]any) (requestID string, ok bool) {
	if len(meta) == 0 {
		return "", false
	}
	raw, present := meta[metaKeyCodeBuddyUnsolicitedTurn]
	if !present {
		return "", false
	}
	payload, isMap := raw.(map[string]any)
	if !isMap {
		return "", false
	}
	if metaString(payload["reason"]) != backgroundTurnReasonDrain {
		return "", false
	}
	id := strings.TrimSpace(metaString(payload["requestId"]))
	if id == "" {
		return "", false
	}
	return id, true
}

// adoptBackgroundTurn opens a stream route for an agent-initiated turn and hands
// it to the service layer. Called from SessionUpdate (the SDK notification
// goroutine) BEFORE the route lookup, which would otherwise drop the whole turn.
//
// It must not block: registering the route is a map write under the client's
// leaf lock, and the consumer is started on its own goroutine.
func (c *ClawBenchACPClient) adoptBackgroundTurn(acpSessionID, requestID string) {
	conn := c.connRef
	if conn == nil {
		return
	}
	handler := backgroundTurnHandler()
	if handler == nil {
		// No consumer is wired (tests, or a build that does not run the service
		// layer). Registering a route anyway would only fill a channel nobody
		// reads, so leave the frames on the existing drop path.
		slog.Debug("acp: agent-initiated turn with no consumer",
			"acp_sid", acpSessionID, "request_id", requestID)
		return
	}

	turn, ok := c.beginBackgroundTurn(acpSessionID, requestID)
	if !ok {
		return
	}
	// CodeBuddy reuses the previous turn's requestId for this turn, so the
	// replay filter would otherwise drop the whole reply as "stale". Whitelist
	// the id for as long as the turn is being consumed.
	conn.noteAdoptedTurnRequestID(requestID)

	sid := conn.clawbenchSID
	slog.Info("acp: adopting agent-initiated turn",
		"clawbench_sid", sid,
		"acp_sid", acpSessionID,
		"request_id", requestID)

	// Arm the same no-progress watchdog a user turn gets.
	//
	// An adopted turn is announced and then streamed with no Prompt() call in
	// flight, so nothing else bounds it: without this an agent that announces a
	// background drain and then goes quiet leaves the turn open until the
	// service layer's backstop deadline (backgroundTurnTimeout, 30m) expires.
	//
	// The judge is unchanged (isStalled): model progress, an in-flight tool and
	// a pending elicitation all suppress it, so a long build or a permission
	// prompt inside a background turn is still protected. Only the timing source
	// is new — reset here, so a stale timestamp from the previous turn cannot
	// fire immediately.
	conn.TouchModelProgress()
	stopWatchdog := conn.startStallWatchdog(context.Background(), func() {
		c.terminateStalledBackgroundTurn(acpSessionID, turn)
	})
	// Publish the stop handle under the same lock RegisterSession reads it with.
	// A user turn can take the session over while this watchdog is being armed;
	// an unsynchronised write here raced that read. If the takeover already
	// happened, the turn is no longer registered — release the watchdog now
	// rather than leaving it to fire against a route that is gone.
	c.mu.Lock()
	if cur, registered := c.backgroundTurns[acpSessionID]; registered && cur == turn {
		turn.stopStallWatchdog = stopWatchdog
	} else {
		stopWatchdog()
	}
	c.mu.Unlock()

	// end is idempotent: the consumer calls it when it finishes, and the
	// deferred call covers an early return (or a panic) inside it.
	var once sync.Once
	end := func() {
		once.Do(func() { c.releaseBackgroundRoute(acpSessionID, turn) })
	}
	go func() {
		defer end()
		handler(sid, turn.ch, end)
	}()
}

// terminateStalledBackgroundTurn closes an adopted turn whose agent stopped
// producing anything.
//
// It delivers the turn's terminal event instead of killing the agent process.
// A user turn kills on stall because its own Prompt RPC would otherwise hang
// forever; an adopted turn has no RPC to unblock, and the agent process is
// shared with the user's session — killing it would take down a working
// conversation to finish a bubble.
func (c *ClawBenchACPClient) terminateStalledBackgroundTurn(acpSessionID string, turn *backgroundTurn) {
	c.mu.Lock()
	cur, ok := c.backgroundTurns[acpSessionID]
	route, routed := c.sessionRoutes[acpSessionID]
	owned := ok && routed && cur == turn && route == turn.route
	if owned {
		// Retire the entry so a late session_end cannot deliver a second
		// terminal. The ROUTE stays until the consumer releases it.
		delete(c.backgroundTurns, acpSessionID)
		if deb, hasDeb := c.debouncers[acpSessionID]; hasDeb {
			deb.flushAll()
		}
	}
	c.mu.Unlock()
	if !owned {
		return
	}
	// The terminal needs a reason, or the service layer records this as a
	// normal completion and the user is left with a reply that stops mid-word
	// and no explanation. ReasonStalled is the honest label: the turn did not
	// end, it was ended — and unlike ReasonTimeout it stays retryable, because
	// the turn budget was never spent.
	deliverBackgroundTurnEnd(turn, ReasonStalled)
}

// beginBackgroundTurn registers the route for an adopted turn. Returns false
// when a route already exists: a user turn owns the session (the agent's premise
// is that the session is idle), so its frames belong to that turn and must not
// be stolen — the service consumer bails out in that case and the frames keep
// flowing to the live turn.
func (c *ClawBenchACPClient) beginBackgroundTurn(acpSessionID, requestID string) (*backgroundTurn, bool) {
	ch := make(chan StreamEvent, streamChanSize)

	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.sessionRoutes[acpSessionID]; exists {
		slog.Info("acp: agent-initiated turn ignored, session already streaming",
			"acp_sid", acpSessionID, "request_id", requestID)
		return nil, false
	}
	turn := &backgroundTurn{ch: ch, route: ch, requestID: requestID}
	c.sessionRoutes[acpSessionID] = ch
	if c.backgroundTurns == nil {
		c.backgroundTurns = make(map[string]*backgroundTurn)
	}
	c.backgroundTurns[acpSessionID] = turn
	c.debouncers[acpSessionID] = newToolCallDebouncer(ch, c.connRef)
	// Mirror RegisterSession: a tool_call always precedes its permission
	// request within the same turn, so entries from an earlier turn are dead
	// weight on a long-lived connection.
	prefix := acpSessionID + ":"
	for k := range c.toolInputs {
		if strings.HasPrefix(k, prefix) {
			delete(c.toolInputs, k)
		}
	}
	return turn, true
}

// releaseBackgroundRoute releases the route opened by beginBackgroundTurn.
//
// The route is only removed when it is still the one this turn installed: a user
// turn may have replaced it (RegisterSession), and deleting that would cut the
// user's own stream. The registry entry is cleared either way.
func (c *ClawBenchACPClient) releaseBackgroundRoute(acpSessionID string, turn *backgroundTurn) {
	// Stop the watchdog first: it delivers its own terminal, and one arriving
	// after the consumer finished would land on a released route.
	turn.stopWatchdog()
	if c.connRef != nil {
		c.connRef.clearAdoptedTurnRequestID(turn.requestID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.backgroundTurns[acpSessionID]; ok && cur == turn {
		delete(c.backgroundTurns, acpSessionID)
	}
	route, routed := c.sessionRoutes[acpSessionID]
	if !routed || route != turn.route {
		return
	}
	delete(c.sessionRoutes, acpSessionID)
	if deb, hasDeb := c.debouncers[acpSessionID]; hasDeb {
		deb.flushAll()
		delete(c.debouncers, acpSessionID)
	}
}

// backgroundTurnEndWait bounds the fallback that waits for room in a full
// channel. Long enough that a consumer doing a slow DB write still gets the
// event; short enough that a channel nobody reads cannot leak a goroutine.
const backgroundTurnEndWait = 30 * time.Second

// closeBackgroundTurnOnSessionEnd delivers the terminal event to an adopted turn
// when the agent reports that it finished. Returns false when this session's
// route no longer belongs to an adopted turn — an ordinary user turn's
// session_end, or one whose route a user turn has since taken over.
//
// It runs on the SDK's notification goroutine, which is what makes the terminal
// ORDERED after the turn's content: the raw stdout tee runs on a different
// goroutine and would overtake frames still queued in the SDK, truncating the
// reply.
func (c *ClawBenchACPClient) closeBackgroundTurnOnSessionEnd(acpSessionID string) bool {
	return c.finishBackgroundTurn(acpSessionID, "", "completed", "")
}

func (c *ClawBenchACPClient) finishBackgroundTurn(acpSessionID, requestID, state, summary string) bool {
	c.mu.Lock()
	turn, ok := c.backgroundTurns[acpSessionID]
	route, routed := c.sessionRoutes[acpSessionID]
	// Ownership check: a user turn may have replaced the route (RegisterSession)
	// while this background turn was still being consumed. Its session_end must
	// not terminate that other turn.
	owned := ok && routed && route == turn.route && (requestID == "" || requestID == turn.requestID)
	if owned {
		// Retire the entry so a later session_end, or a user turn taking the
		// route over, cannot deliver a second terminal. The ROUTE stays until
		// the consumer releases it.
		delete(c.backgroundTurns, acpSessionID)
		// Flush the debouncer's buffered tool_call_update frames ahead of the
		// terminal: the executor stops reading on `done`, so anything still
		// buffered here would be lost with the tool card left mid-status.
		if deb, hasDeb := c.debouncers[acpSessionID]; hasDeb {
			deb.flushAll()
		}
	}
	c.mu.Unlock()
	if !owned {
		return false
	}
	// The turn ended on its own — the watchdog must not also fire and deliver a
	// second terminal.
	turn.stopWatchdog()
	terminal := StreamEvent{Type: "done"}
	switch state {
	case "failed":
		terminal = StreamEvent{Type: "error", Error: "Background turn failed. " + summary}
	case "stopped":
		terminal.Type = "cancelled"
	}
	deliverBackgroundTerminal(turn, terminal)
	return true
}

// deliverBackgroundTurnEnd sends the adopted turn's terminal event.
//
// reason carries why the turn ended when it did not end on its own (a stall
// watchdog firing); empty means the agent reported session_end. The executor
// records the non-empty case as a stalled turn, so the reply is not presented
// as complete.
//
// Never blocks: the caller runs on the SDK's shared notification goroutine,
// where blocking can overflow the queue and kill the connection. If the channel
// is momentarily full the send moves to its own goroutine — the channel is FIFO,
// so `done` still lands after every frame already queued.
func deliverBackgroundTurnEnd(turn *backgroundTurn, reason string) {
	deliverBackgroundTerminal(turn, StreamEvent{Type: "done", Reason: reason})
}

func deliverBackgroundTerminal(turn *backgroundTurn, done StreamEvent) {
	select {
	case turn.ch <- done:
		return
	default:
	}
	go func() {
		select {
		case turn.ch <- done:
		case <-time.After(backgroundTurnEndWait):
			slog.Warn("acp: adopted turn never drained its channel, terminal event dropped")
		}
	}()
}
