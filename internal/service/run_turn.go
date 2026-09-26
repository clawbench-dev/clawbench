package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/ws"
)

// run_turn.go holds the ONE implementation of "run a single AI turn".
//
// It exists because that flow used to be written three times — in the
// /api/ai/chat handler, in the queue/push execution path, and in the scheduler —
// and the copies drifted. The drift is not hypothetical: the enqueue self-heal
// was added to only one copy, and the chat handler's request builder carried a
// resume guard the queue path lacked. A fix applied to one copy silently missed
// the others, so every behavioral guarantee had to be re-established per copy.
//
// The API is deliberately two-phase (start + finalize) because the scheduler
// must handle an aborted turn specially: on cancel it flushes, finalizes orphan
// rows, and unregisters the active stream itself instead of calling Finalize.
// Exposing the executor handle lets that caller keep its behavior while still
// sharing the whole start-up path.

// TurnSpec describes one AI turn to execute.
type TurnSpec struct {
	// Ctx is the execution context. runTurnStart derives a per-turn child
	// context from it so "interrupt and send" can stop just this turn while a
	// caller that runs several turns (the drain loop) keeps its context alive.
	Ctx context.Context

	// Mode selects interactive vs scheduled semantics inside the executor.
	Mode ExecutionMode

	// Identity / location.
	ProjectPath string
	BackendName string
	SessionID   string
	AgentID     string

	// ChatReq is the already-built request. Callers build it via
	// BuildChatRequest / BuildChatRequestForQueue so resume and fork handling
	// stays in one place.
	ChatReq ai.ChatRequest

	// FileDir is the working directory handed to the executor.
	FileDir string

	// LocalizeError formats user-facing error strings. Nil means "use the raw
	// error text" (the scheduler and the service-layer paths do this; the HTTP
	// handler passes its i18n implementation).
	LocalizeError func(err error, key string, args map[string]any) string

	// Scheduled-only fields, forwarded to the executor so it can record
	// progress against the task execution row.
	TaskID      int64
	ExecutionID int64
	TriggerType string

	// DrainOnFinalize controls whether Finalize drains leftover events from the
	// stream channel. The interactive paths pass their channel (tool calls the
	// debouncer flushed after the event loop exited are still pending); the
	// scheduler historically passed nil, skipping the drain.
	//
	// NOTE: this preserves the pre-existing difference rather than silently
	// changing scheduled-task behavior. The two should probably be unified —
	// see the follow-up noted in the plan — but that is a behavior change and
	// does not belong in a pure dedupe.
	DrainOnFinalize bool

	// OnStarted is invoked once the turn is genuinely under way: the backend is
	// created, the stream has started, the placeholder exists, and stream_start
	// has been broadcast — but BEFORE the blocking event loop runs.
	//
	// The scheduler needs this hook: it emits a "running" task event, and that
	// event is only truthful after the turn starts. runTurnStart cannot return
	// early to let the caller emit it, because its final step is the blocking
	// RunWithChannel; a caller that emitted "running" after runTurnStart returned
	// would send it once the task had already finished, so subscribers saw
	// started → completed with no running in between.
	//
	// Nil means no callback.
	OnStarted func()
}

// TurnResult is the outcome of one AI turn.
//
// CancelReason uses the executor's vocabulary ("user", "interrupt", "cancel",
// "disconnect", ""); callers map it to their own result type. Err is a
// user-facing message (localized when LocalizeError was provided).
type TurnResult struct {
	CancelReason string
	Err          string
	Empty        bool

	// ReceivedTerminal is true when the backend sent a terminal event
	// (done/error). False means the event channel closed without one — the
	// agent process likely crashed — which callers must treat as a failure
	// rather than a clean finish.
	ReceivedTerminal bool

	// AbnormalReason is non-empty when the turn ended abnormally in a way that
	// is worth resuming (see classifyTurnAbnormality). It is empty both for a
	// clean finish and for failures that must NOT be resumed (user cancel,
	// timeout, panic, …), so callers can treat "" as "do not auto-continue".
	//
	// Only ever set on the finalized path: an early failure (backend create /
	// stream start / placeholder) returns earlyFails without going through
	// runTurnFinalize, because those failures are deterministic and retrying
	// them would spin.
	AbnormalReason string

	// MsgID is the streaming assistant placeholder row id (0 if it could not be
	// created).
	MsgID int64
}

// activeTurn is a started-but-not-yet-finalized turn. It carries the handles a
// caller needs to finish (or abort) the turn itself.
type activeTurn struct {
	executor   *SessionExecutor
	eventCh    <-chan ai.StreamEvent
	turnCtx    context.Context
	turnCancel context.CancelFunc
	msgID      int64
	runResult  RunResult
	spec       TurnSpec
	earlyFails TurnResult
}

// started reports whether the turn got as far as running the event loop. A turn
// that failed early has no executor and must not be finalized.
func (at *activeTurn) started() bool { return at.executor != nil }

// release cancels the per-turn context. Callers must invoke it AFTER reading
// turnCtx.Err() for outcome classification — cancelling first would make every
// turn look like a user cancel.
func (at *activeTurn) release() {
	if at.turnCancel != nil {
		at.turnCancel()
	}
}

// Localization keys for the two early failures. Kept here so every caller
// surfaces the same key for the same failure.
const (
	reasonBackendCreateFailed = "BackendCreateFailed"
	reasonStreamStartFailed   = "StreamStartFailed"
)

// localize renders an error message via the caller's localizer, falling back to
// the raw error text when none was supplied.
//
// The caller owns the wording on purpose: the HTTP handler localizes via its
// i18n table, while the service paths prefix the raw error ("create backend: …",
// "start stream: …"). Both are user-visible strings with existing tests, so the
// shared implementation must not impose one format.
func (s TurnSpec) localize(err error, key string, args map[string]any) string {
	if s.LocalizeError != nil {
		return s.LocalizeError(err, key, args)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// serviceLocalizeError reproduces the service layer's historical error wording.
// Used by the queue/push execution path so its messages stay byte-identical
// after the run logic moved into the shared implementation.
func serviceLocalizeError(_ error, key string, args map[string]any) string {
	errText := ""
	if args != nil {
		if e, ok := args["Error"]; ok {
			if es, ok := e.(string); ok {
				errText = es
			}
		}
	}
	switch key {
	case reasonBackendCreateFailed:
		return "create backend: " + errText
	case reasonStreamStartFailed:
		return "start stream: " + errText
	default:
		return errText
	}
}

// failTurn emits the error event, persists a warning row, and returns the
// result. Shared by both early-failure branches (backend creation and stream
// start) so they cannot drift in what they report or persist.
func (s TurnSpec) failTurn(err error, key string) TurnResult {
	errMsg := s.localize(err, key, map[string]any{"Error": err.Error()})
	emitDrainEvent(s.SessionID, ai.StreamEvent{Type: eventTypeError, Error: errMsg})
	errContent, _ := json.Marshal(map[string]any{
		contentKeyBlocks: []any{map[string]string{
			contentKeyType:   blockTypeWarning,
			contentKeyText:   errMsg,
			contentKeyReason: ai.ReasonBackendExit,
		}},
	})
	if _, saveErr := AddChatMessage(s.ProjectPath, s.BackendName, s.SessionID, roleAssistant, string(errContent), nil, false, ""); saveErr != nil {
		slog.Error("failed to save error message", slog.String("err", saveErr.Error()))
	}
	return TurnResult{Err: errMsg}
}

// runTurnStart performs the first phase of a turn: create the backend, start the
// stream, create the streaming placeholder, register the per-turn cancel, and
// run the event loop.
//
// The returned activeTurn always carries a usable executor unless the turn
// failed early, in which case activeTurn.earlyFails is set and the caller must
// return it directly (no executor was created).
func runTurnStart(spec TurnSpec) *activeTurn {
	runStart := time.Now()

	// Per-turn context: lets "interrupt and send" stop just THIS turn while the
	// caller's context stays alive for the next queued message.
	turnCtx, turnCancel := context.WithCancel(spec.Ctx)
	RegisterSessionTurnCancel(spec.SessionID, turnCancel)

	// NOTE: turnCancel is deliberately NOT deferred here. The outcome
	// classification reads turnCtx.Err() to tell a user cancel from a normal
	// finish, so cancelling before that read would misreport every turn as
	// cancelled. Callers release it via activeTurn.release().
	at := &activeTurn{turnCtx: turnCtx, turnCancel: turnCancel, spec: spec}

	sessionTransport := GetSessionTransport(spec.SessionID)
	slog.Info("acp perf: executeStreamRun.start",
		"session_id", spec.SessionID,
		"backend", spec.BackendName,
		"agent_id", spec.AgentID,
		"transport", sessionTransport,
		"resume", spec.ChatReq.Resume,
		"mode", int(spec.Mode))

	backend, err := ai.NewBackendForAgentWithTransport(spec.BackendName, spec.AgentID, sessionTransport)
	if err != nil {
		slog.Error("failed to create backend",
			slog.String("backend", spec.BackendName), slog.String("err", err.Error()))
		at.earlyFails = spec.failTurn(err, reasonBackendCreateFailed)
		return at
	}

	// If the session's transport says acp-stdio but the agent fell back to CLI,
	// clear the stale override so later messages stop warning.
	if sessionTransport == transportACPStdio {
		if _, ok := backend.(*ai.ACPBackend); !ok {
			_ = UpdateSessionTransport(spec.SessionID, "")
		}
	}

	slog.Info("acp perf: executeStreamRun.ExecuteStream_start",
		"session_id", spec.SessionID,
		"transport", sessionTransport,
		"after_backend_create", time.Since(runStart))

	eventCh, err := backend.ExecuteStream(turnCtx, spec.ChatReq)
	if err != nil {
		slog.Error("failed to start stream", slog.String("err", err.Error()))
		at.earlyFails = spec.failTurn(err, reasonStreamStartFailed)
		return at
	}
	at.eventCh = eventCh

	// Create the streaming placeholder. Its id is what FinalizeStreamingMessage
	// updates, so a failure here means the reply has nowhere to land.
	emptyContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{}})
	streamingMsgID, err := AddChatMessage(spec.ProjectPath, spec.BackendName, spec.SessionID,
		roleAssistant, string(emptyContent), nil, true, "")
	if err != nil {
		slog.Error("failed to create streaming assistant placeholder",
			slog.String("session", spec.SessionID),
			slog.String("err", err.Error()))
		at.earlyFails = spec.failTurn(err, reasonStreamStartFailed)
		// The stream is already producing into eventCh but no executor will
		// consume it. Drain in the background until the producer closes it, or
		// the producer goroutine blocks forever on a full channel and leaks.
		drainEventChannel(eventCh)
		return at
	}
	at.msgID = streamingMsgID
	slog.Info("chat: created streaming assistant placeholder",
		slog.String("session", spec.SessionID),
		slog.Int64("streamingMsgID", streamingMsgID))

	// Broadcast stream_start so subscribed clients (including ones that opened
	// the session mid-stream) learn the streaming message id and can create a
	// placeholder if none exists yet. This makes the assistant bubble purely
	// data-driven: any client, at any time, sees a placeholder whenever the DB
	// has a streaming=1 row or a stream_start event arrives.
	ws.EmitToSession(spec.SessionID, ai.StreamEvent{
		Type:        "stream_start",
		StreamStart: &ai.StreamStartData{MessageID: streamingMsgID},
	})

	execCfg := RunConfig{
		Mode:               spec.Mode,
		ProjectPath:        spec.ProjectPath,
		BackendName:        spec.BackendName,
		SessionID:          spec.SessionID,
		AgentID:            spec.AgentID,
		ChatRequest:        spec.ChatReq,
		FileDir:            spec.FileDir,
		StreamingMessageID: streamingMsgID,
		LocalizeError:      spec.LocalizeError,
		TaskID:             spec.TaskID,
		ExecutionID:        spec.ExecutionID,
		TriggerType:        spec.TriggerType,
	}
	at.executor = NewSessionExecutor(turnCtx, execCfg)
	// The turn is under way but not yet run. Fired before the blocking event
	// loop so a caller's "started" notification precedes the work it describes.
	if spec.OnStarted != nil {
		spec.OnStarted()
	}
	at.runResult = at.executor.RunWithChannel(eventCh)
	// The turn is over: its cancel reason has been read, so stop advertising it
	// as interruptible. Anything arriving now belongs to the next turn.
	UnregisterSessionTurnCancel(spec.SessionID)
	return at
}

// runTurnFinalize performs the second phase: persist to DB, emit metadata, and
// classify the outcome.
func (at *activeTurn) runTurnFinalize() TurnResult {
	finalizeCh := at.eventCh
	if !at.spec.DrainOnFinalize {
		finalizeCh = nil
	}
	at.runResult = at.executor.Finalize(at.runResult, finalizeCh)
	emitDrainEvent(at.spec.SessionID, ai.StreamEvent{Type: contentKeyMetadata, Meta: at.runResult.Metadata})

	runResult := at.runResult
	result := TurnResult{
		CancelReason:     runResult.CancelReason,
		ReceivedTerminal: runResult.ReceivedTerminal,
		MsgID:            at.msgID,
	}
	switch {
	case runResult.CancelReason == cancelReasonUser:
		result.CancelReason = runResult.CancelReason
	case runResult.CancelReason == cancelReasonInterrupt:
		// "interrupt and send": the drain loop must KEEP the queue and move on
		// to the next message (see drainHandleTerminal). Passing the reason
		// through (instead of folding it into "cancel") is what tells it that.
		result.CancelReason = runResult.CancelReason
	case at.turnCtx.Err() == context.Canceled:
		result.CancelReason = "cancel"
	case at.turnCtx.Err() == context.DeadlineExceeded:
		result.Err = "AI response timed out (30 min)"
	case runResult.Empty:
		result.Empty = true
	}

	// Classify AFTER the switch so it sees the executor's raw cancel reason and
	// the finalized blocks (the warning blocks that carry a backend reason are
	// only present after Finalize). Auto-continue policy lives in
	// classifyTurnAbnormality — see auto_continue.go for the exclusions.
	result.AbnormalReason = classifyTurnAbnormality(
		runResult.CancelReason, runResult.ReceivedTerminal, runResult.Empty, runResult.Blocks)

	slog.Info("ai stream run done",
		slog.String("session", at.spec.SessionID),
		slog.Int("blocks", len(runResult.Blocks)),
		slog.String("cancel_reason", runResult.CancelReason),
		slog.Int("wall_ms", runResult.WallMs),
		slog.Int("mode", int(at.spec.Mode)))

	return result
}

// runTurn runs one complete AI turn. It does NOT send a terminal WS event — the
// caller decides what terminal event to emit, because that differs between the
// interactive drain loop and the scheduler.
func runTurn(spec TurnSpec) TurnResult {
	at := runTurnStart(spec)
	// Released after classification (runTurnFinalize reads turnCtx.Err()) and
	// also on the early-failure paths, so the context never leaks.
	defer at.release()
	if !at.started() {
		return at.earlyFails
	}
	return at.runTurnFinalize()
}

// drainEventChannel consumes a stream channel in the background until the
// producer closes it.
//
// Needed on paths that abandon an already-started stream (early failure after
// ExecuteStream succeeded): parser sends are not context-aware, so a producer
// that fills the channel with no consumer blocks forever, leaking the goroutine
// and holding the agent process open.
func drainEventChannel(ch <-chan ai.StreamEvent) {
	go func() {
		for range ch {
		}
	}()
}

// resolveFileDir resolves a project path to an absolute working directory.
// ACP ResumeSession/NewSession fails when handed cwd="", so every caller must
// pass an absolute path. Shared so the three entry points cannot diverge.
func resolveFileDir(projectPath string) string {
	if absDir, err := filepath.Abs(projectPath); err == nil {
		return absDir
	}
	return projectPath
}

// RunTurn executes one complete AI turn. Exported entry point for
// internal/handler, which needs to run a turn with its own i18n localizer;
// behaviorally identical to the internal runTurn.
func RunTurn(spec TurnSpec) TurnResult {
	return runTurn(spec)
}

// ResolveFileDir resolves a project path to an absolute working directory.
// Exported for internal/handler, which must pass the same working directory the
// service paths use or ACP resume breaks.
func ResolveFileDir(projectPath string) string {
	return resolveFileDir(projectPath)
}
