package ai

import (
	"context"
	"log/slog"
)

// AutoResumeBackend wraps an AIBackend and adds ExitPlanMode auto-resume behavior.
// When it detects a tool_use event with Name="ExitPlanMode" and Done=true,
// it cancels the inner CLI process, then starts a new ExecuteStream call with
// the same session ID and prompt="continue" with Resume=true.
// The outer caller sees a single continuous <-chan StreamEvent.
type AutoResumeBackend struct {
	inner AIBackend
}

// Name returns the wrapped backend's name.
func (b *AutoResumeBackend) Name() string {
	return b.inner.Name()
}

// ExecuteStream runs the inner backend and wraps the event stream with
// ExitPlanMode auto-resume logic. If ExitPlanMode is detected, the first
// stream is cancelled and a resume stream is started transparently.
func (b *AutoResumeBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	outerCh := make(chan StreamEvent, streamChanSize)

	// Create a child context for the first stream so we can cancel it
	// independently of the outer context (for ExitPlanMode resume).
	innerCtx, innerCancel := context.WithCancel(ctx)

	innerCh, err := b.inner.ExecuteStream(innerCtx, req)
	if err != nil {
		innerCancel()
		close(outerCh)
		return nil, err
	}

	go b.mergeStreams(ctx, innerCtx, innerCancel, req, innerCh, outerCh)
	return outerCh, nil
}

// mergeStreams handles the two-phase stream merge:
//
//	Phase 1: Forward events from innerCh → outerCh, watch for ExitPlanMode
//	Phase 2: On ExitPlanMode, cancel first stream, start resume, forward resumed events
//
// If no ExitPlanMode is detected, acts as a transparent proxy.
// The "done" event is always forwarded to the outer channel so that
// downstream handlers can detect stream completion explicitly.
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func (b *AutoResumeBackend) mergeStreams(
	ctx context.Context,
	innerCtx context.Context, //nolint:unparam // kept for context propagation consistency
	innerCancel context.CancelFunc,
	origReq ChatRequest,
	innerCh <-chan StreamEvent,
	outerCh chan<- StreamEvent,
) {
	defer close(outerCh)

	exitPlanModeDetected := false

	// Phase 1: forward first stream
	for {
		select {
		case event, ok := <-innerCh:
			if !ok {
				// First stream channel closed normally (no ExitPlanMode).
				// No explicit "done" event was received (the CLIBackend may close
				// the channel after metadata without emitting "done" in some cases).
				// Emit "done" ourselves so the downstream handler can finalize.
				forwardEvent(outerCh, StreamEvent{Type: "done"})
				return
			}

			// Detect ExitPlanMode: CLI hangs in --print mode because
			// ExitPlanMode requires user approval, but there is no interactive UI.
			if !exitPlanModeDetected &&
				event.Type == "tool_use" && event.Tool != nil &&
				event.Tool.Name == "ExitPlanMode" && event.Tool.Done {
				exitPlanModeDetected = true
				slog.Info("ExitPlanMode detected, cancelling CLI to auto-resume",
					slog.String("session", origReq.SessionID))

				// Forward the ExitPlanMode tool_use event
				forwardEvent(outerCh, event)
				// Emit resume_split so handler can finalize DB message
				forwardEvent(outerCh, StreamEvent{Type: "resume_split"})

				// Cancel the inner CLI process
				innerCancel()

				// Drain remaining events from the first stream
				// (captures raw_output, suppresses "done")
				// Check ctx.Done() to avoid blocking indefinitely if CLI is unresponsive (ISS-296)
			drain:
				for {
					select {
					case drainEvent, ok := <-innerCh:
						if !ok {
							break drain
						}
						if drainEvent.Type == "raw_output" {
							forwardEvent(outerCh, drainEvent)
						}
						// Suppress "done" and other events from the cancelled stream
					case <-ctx.Done():
						// Outer context cancelled during drain — stop immediately
						break drain
					}
				}
				goto phase1Done
			}

			// Normal forwarding — "done" means normal completion, forward it
			if event.Type == "done" {
				forwardEvent(outerCh, event)
				return
			}
			forwardEvent(outerCh, event)

		case <-ctx.Done():
			// Outer context cancelled (real user cancel/disconnect)
			return
		}
	}

phase1Done:
	if !exitPlanModeDetected {
		return
	}

	// Phase 2: resume with "continue"
	innerCtx2, innerCancel2 := context.WithCancel(ctx)
	defer innerCancel2()

	resumeReq := ChatRequest{
		Prompt:                "continue",
		SessionID:             origReq.SessionID,
		WorkDir:               origReq.WorkDir,
		SystemPrompt:          origReq.SystemPrompt,
		Model:                 origReq.Model,
		Command:               origReq.Command,
		AgentID:               origReq.AgentID,
		ThinkingEffort:        origReq.ThinkingEffort,
		ScheduledExecution:    origReq.ScheduledExecution,
		Resume:                true,
		AssistantMessageCount: origReq.AssistantMessageCount,
	}

	slog.Info("auto-resume: starting phase 2 (resume with 'continue')",
		slog.String("session", origReq.SessionID),
		slog.String("agent", origReq.AgentID))

	innerCh2, err := b.inner.ExecuteStream(innerCtx2, resumeReq)
	if err != nil {
		slog.Error("failed to start resume stream after ExitPlanMode",
			slog.String("session", origReq.SessionID),
			slog.String("err", err.Error()))
		// Emit done so the handler can finalize
		forwardEvent(outerCh, StreamEvent{Type: "done"})
		return
	}

	// Forward second stream (suppress raw_output, no nested ExitPlanMode detection)
	for {
		select {
		case event2, ok := <-innerCh2:
			if !ok {
				// Channel closed without "done" — emit done ourselves and end
				forwardEvent(outerCh, StreamEvent{Type: "done"})
				return
			}
			if event2.Type == "raw_output" {
				continue // suppress raw_output from resume stream
			}
			if event2.Type == "done" {
				forwardEvent(outerCh, event2) // forward "done" to outer
				return
			}
			forwardEvent(outerCh, event2)

		case <-ctx.Done():
			// Outer context cancelled during resume
			return
		}
	}
}

// forwardEvent sends an event to the channel, dropping if full.
func forwardEvent(ch chan<- StreamEvent, event StreamEvent) {
	select {
	case ch <- event:
	default:
		slog.Warn(
			"auto_resume: event dropped — channel full",
			slog.String("type", event.Type),
		)
	}
}
