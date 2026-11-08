package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/ws"
)

// background_turn.go adopts a turn the AGENT started on its own.
//
// A background task (run_in_background Bash, a detached sub-agent) finishes long
// after the user's turn returned. CodeBuddy then opens a NEW turn on the idle
// session and streams its content — see internal/ai/acp_background_turn.go for
// the protocol side. Without a consumer that whole turn is dropped and the
// session goes silent forever (issue #644: the agent promises "I'll wake you up
// when it's done" and nothing ever appears).
//
// The AI layer hands over a stream channel with the frames already routed to it.
// Everything below is the ordinary turn lifecycle minus the parts that need a
// user message: placeholder row, stream_start, event loop, finalize, terminal
// events. The result is an ordinary assistant message in the timeline, so the
// frontend needs no special case.

// backgroundTurnTimeout bounds an adopted turn. It normally ends on the agent's
// own session_end frame; this is only the backstop that keeps a missed terminal
// from leaving a bubble streaming forever. It matches the interactive turn cap.
const backgroundTurnTimeout = 30 * time.Minute

// RunBackgroundTurn consumes an agent-initiated turn and writes it into the
// session timeline.
//
// events is the stream channel the ACP layer routed the turn's frames to; end
// releases that route and is idempotent (the ACP layer also calls it when this
// function returns, so an early return cannot leak the route).
func RunBackgroundTurn(sessionID string, events <-chan ai.StreamEvent, end func()) {
	if end != nil {
		defer end()
	}
	if sessionID == "" || events == nil {
		return
	}

	target, ok := resolveBackgroundTurnTarget(sessionID)
	if !ok {
		return
	}
	projectPath, backend, timeline := target.projectPath, target.backend, target.timeline
	speakerID, isGroupMember := target.speakerID, target.isGroupMember

	// Claim the id that owns the session's activity. A group run registers its
	// runner under the GROUP id, so claiming the member row would succeed while
	// a discussion is in flight and inject this turn into the middle of it —
	// claiming the group is what makes "is it idle" and "take it" one step.
	// A user turn may also have started in the same instant the agent announced
	// its own; the agent's premise is that the session is idle, so losing that
	// race is safe: the live turn owns the stream route (the ACP layer refused
	// to adopt when a route already existed) and these frames belong to it.
	claimID := sessionID
	if isGroupMember {
		claimID = timeline
	}
	ctx, claimed := claimBackgroundTurn(sessionID, claimID)
	if !claimed {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, backgroundTurnTimeout)
	defer cancel()
	// Mirror the interactive terminal path: retire the runner without emitting
	// its "completed" here, because the terminal block below emits it only when
	// this turn wins the once-per-run push guard. retire is idempotent so the
	// normal path can release the claim BEFORE handing over queued messages
	// (EnsureConsumer cannot claim a session that still looks running).
	retired := false
	retire := func() {
		if retired {
			return
		}
		retired = true
		SetSessionRunning(claimID, false, true)
	}
	defer retire()

	// Wait for the turn's first frame before creating anything: a turn that
	// produces nothing at all (the agent announces and closes immediately) must
	// not leave a blank assistant bubble behind.
	first, ok := awaitFirstBackgroundEvent(ctx, events)
	if !ok {
		return
	}
	if first.Type == eventTypeDone || first.Type == eventTypeError || first.Type == statusCancelled {
		// Nothing was produced: forward the terminal frame as-is (done / error /
		// cancelled) so the client closes the run, but create no row.
		ws.EmitToSession(timeline, first)
		return
	}

	emptyContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{}})
	msgID, err := AddChatMessageWithAgent(projectPath, backend, timeline,
		roleAssistant, string(emptyContent), nil, true, "", speakerID)
	if err != nil {
		slog.Error("background turn: failed to create streaming placeholder",
			"session", sessionID, "err", err.Error())
		return
	}
	emitDrainEvent(timeline, ai.StreamEvent{
		Type:        "stream_start",
		StreamStart: &ai.StreamStartData{MessageID: msgID, SpeakerID: speakerID},
	})

	executor := NewSessionExecutor(ctx, RunConfig{
		Mode:               ModeInteractive,
		ProjectPath:        projectPath,
		BackendName:        backend,
		SessionID:          sessionID,
		TimelineSessionID:  timeline,
		SpeakerID:          speakerID,
		AgentID:            ResolveAgentID(sessionID, ""),
		StreamingMessageID: msgID,
		// A group member's turn is summarized once by the orchestrator at the
		// end of the discussion; summarizing per member here would run one LLM
		// call per background turn on the shared group timeline (decision #55).
		SuppressSummarization: isGroupMember,
	})
	result := executor.RunWithChannel(replayFirstBackgroundEvent(ctx, first, events))
	result = executor.Finalize(result, nil)

	// Retire BEFORE the terminal event, mirroring the interactive path
	// (handler/chat.go markDoneAndSendFinal): a client that reloads on the
	// terminal event must not still see the session flagged as running.
	retire()

	emitBackgroundTurnTerminal(timeline, result)

	// Messages queued while this turn was running have no drain loop behind
	// them; hand them to the normal consumer so they are not stranded until the
	// next manual send. The claim must be released first, or EnsureConsumer
	// finds the session still "running" and declines to start a consumer.
	//
	// Members are excluded: a member row is not a standalone chat, and
	// EnsureConsumer's single-agent branch would answer the group's queued
	// message with that member alone (the group orchestrator owns that path).
	if !isGroupMember {
		EnsureConsumer(sessionID)
	}

	slog.Info("background turn: finished",
		"session", sessionID, "msg_id", msgID,
		"cancel_reason", result.CancelReason, "empty", result.Empty)
}

// awaitFirstBackgroundEvent blocks until the adopted turn produces its first
// frame (or the turn is cancelled).
func awaitFirstBackgroundEvent(ctx context.Context, events <-chan ai.StreamEvent) (ai.StreamEvent, bool) {
	select {
	case ev, ok := <-events:
		return ev, ok
	case <-ctx.Done():
		return ai.StreamEvent{}, false
	}
}

// replayFirstBackgroundEvent re-yields the already-consumed first frame, then
// forwards the rest and closes the output so the executor sees an ordinary
// stream. The forwarder exits on ctx — the turn ended or was cancelled — so it
// cannot outlive the turn.
func replayFirstBackgroundEvent(ctx context.Context, first ai.StreamEvent, rest <-chan ai.StreamEvent) <-chan ai.StreamEvent {
	out := make(chan ai.StreamEvent, 1)
	out <- first
	go func() {
		defer close(out)
		for {
			select {
			case ev, ok := <-rest:
				if !ok {
					return
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// claimBackgroundTurn claims the id that owns the session's activity, reporting
// whether this caller may run the turn.
func claimBackgroundTurn(sessionID, claimID string) (context.Context, bool) {
	ctx, claimed := TryClaimSessionRun(claimID)
	if !claimed {
		slog.Info("background turn: session already running, dropping agent-initiated turn",
			"session", sessionID, "claim", claimID)
	}
	return ctx, claimed
}

// emitBackgroundTurnTerminal closes the adopted turn's bubble.
//
// A cancelled turn must never report "completed" — the user just stopped it and
// CancelSession has already announced the cancel. A clean turn follows the
// interactive sequence: push (which also claims the once-per-run guard) and only
// then the "completed" broadcast, so a concurrent cancel cannot be contradicted.
func emitBackgroundTurnTerminal(timeline string, result RunResult) {
	emitDrainEvent(timeline, ai.StreamEvent{Type: contentKeyMetadata, Meta: result.Metadata})
	if result.CancelReason != "" {
		ws.EmitToSession(timeline, ai.StreamEvent{Type: statusCancelled})
		return
	}
	ws.EmitToSession(timeline, ai.StreamEvent{Type: eventTypeDone})
	if EmitSessionPushNotification(timeline, statusCompleted) {
		EmitSessionEventWSOnly(timeline, statusCompleted, false)
	}
}

// resolveBackgroundTurnTarget resolves where an adopted turn's message belongs.
//
// The timeline is where the message becomes visible: a group member's background
// turn belongs to the group, not to its hidden member row, and it must carry the
// member as its speaker.
//
// ok is false when the session is gone (archived or deleted while the background
// task ran): there is nobody to write to, and the ACP layer's sends are
// non-blocking, so the frames are dropped rather than blocking the agent.
func resolveBackgroundTurnTarget(sessionID string) (backgroundTurnTarget, bool) {
	projectPath := GetSessionProjectPathAnyPath(sessionID)
	backend := GetSessionBackend(sessionID)
	if projectPath == "" || backend == "" {
		slog.Info("background turn: session unavailable, dropping agent-initiated turn",
			"session", sessionID, "backend", backend)
		return backgroundTurnTarget{}, false
	}
	timeline := ResolveGroupTimelineID(sessionID)
	if timeline != sessionID {
		return backgroundTurnTarget{
			projectPath: projectPath, backend: backend,
			timeline: timeline, speakerID: sessionID, isGroupMember: true,
		}, true
	}
	return backgroundTurnTarget{projectPath: projectPath, backend: backend, timeline: timeline}, true
}

// backgroundTurnTarget describes where an adopted turn's message belongs.
type backgroundTurnTarget struct {
	projectPath   string
	backend       string
	timeline      string
	speakerID     string
	isGroupMember bool
}
