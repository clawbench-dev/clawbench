package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// activeStreams tracks all in-flight SessionExecutor instances. It lets the
// graceful-shutdown path (FlushStreamingNow) force a final persistence of
// accumulated blocks (including thinking) for every actively streaming session,
// so a server restart mid-stream loses at most the last few hundred ms instead
// of the whole tail. Entries are registered in NewSessionExecutor and removed
// when the executor finishes (after Finalize has persisted the final content).
var activeStreams sync.Map // key: sessionID (string), value: *SessionExecutor

// ExecutionMode distinguishes between interactive chat and task execution.
type ExecutionMode int

// Sentinel errors for RunResult.Err
var (
	errBackendCreate = errors.New("failed to create AI backend")
)

const (
	// ModeInteractive is for normal user-driven chat sessions with WS streaming.
	ModeInteractive ExecutionMode = iota
	// ModeScheduled is for automated task execution without a user present.
	ModeScheduled

	// contentKeyBlocks is the JSON key for content blocks in serialized messages.
	contentKeyBlocks = "blocks"
	// contentKeyMetadata is the JSON key for response metadata.
	contentKeyMetadata = "metadata"
	// cancelReasonUser is the cancel reason when the user explicitly cancels.
	cancelReasonUser = "user"
	// cancelReasonInterrupt stops the CURRENT turn so the next queued message can
	// run, without ending the session or dropping the queue. Set by
	// InterruptSessionTurn (the "interrupt and send" action on a queued message).
	//
	// It is deliberately distinct from cancelReasonUser: the user did not
	// abandon the reply, they redirected it, so the interrupted turn must NOT be
	// stamped "cancelled" and the queue must survive.
	cancelReasonInterrupt = "interrupt"
	// cancelReasonRestart is set during graceful server shutdown before the
	// session context is cancelled — the executor persists a restart warning so
	// the frontend shows the interrupted-response banner after reload.
	cancelReasonRestart = "restart"
	// blockTypeWarning is the content block type for warning messages.
	blockTypeWarning = "warning"
	// blockTypeThinking is the content block type for thinking (reasoning) text.
	blockTypeThinking = "thinking"
	// eventTypeContentReset clears accumulated blocks from a failed Prompt before retry.
	eventTypeContentReset = "content_reset"
	// eventTypeDone is the stream event type for stream completion.
	eventTypeDone = "done"

	// transportACPStdio is the ACP stdio transport type.
	transportACPStdio = "acp-stdio"
	// transportCLI is the CLI transport type.
	transportCLI = "cli"
	// eventTypeError is the stream event type for errors.
	eventTypeError = "error"
	// eventTypeSessionUpdate is the stream event type for session updates.
	eventTypeSessionUpdate = "session_update"
	// eventTypeUserMessage announces a persisted user message to every session
	// subscriber (cross-device sync). Used by the direct send, the queue/push
	// path and auto-continue.
	eventTypeUserMessage = "user_message"
	// eventTypeToolUse is the stream event type for tool calls.
	eventTypeToolUse = "tool_use"
	// eventTypeToolResult is the stream event type for tool results.
	eventTypeToolResult = "tool_result"
	// roleAssistant is the assistant role for chat messages.
	roleAssistant = "assistant"
	// roleUser is the user role for chat messages.
	roleUser = "user"
	// contentKeyText is the JSON key for text in content blocks.
	contentKeyText = "text"
	// contentKeyType is the JSON key for type in content blocks.
	contentKeyType = "type"
	// contentKeyReason is the JSON key for reason in content blocks.
	contentKeyReason = "reason"

	// flushInterval rate-limits streaming persistence of the assistant message.
	// ACP backends emit bursts of incremental events (thinking/content deltas)
	// at thousands per minute; flushing full-block JSON + SQLite on every N
	// events saturates the consumer and the 512-slot stream channel fills,
	// dropping events. Persisting at most once per 500ms keeps the DB fresh
	// for reload-on-refresh without stalling the event loop.
	//
	// The flush aggregates three kinds of writes that used to happen per-event:
	//   - tool-call upserts (chat_tool_calls) — tracked by pendingToolCalls
	//   - context-state persistence (chat_sessions.context_state) — tracked by
	//     pendingContextPatches
	//   - the streaming row content (chat_history.content) — rewritten only when
	//     the marshaled content changed (lastWrittenContent comparison)
	// Consolidating them into the same 500ms window turns thousands of
	// per-event SQLite writes into a handful of batched ones, so the event-loop
	// goroutine no longer stalls the consumer on full-block JSON marshal +
	// SQLite, and the 512-slot stream channel stops dropping events.
	flushInterval = 500 * time.Millisecond

	// wsCoalesceInterval bounds how long a content/thinking delta may sit in the
	// coalescer before it is pushed to WS clients.
	//
	// Deliberately separate from flushInterval and much shorter: the 500ms window
	// is about DB write amplification, where a stale row is harmless (the
	// frontend renders from WS, not the DB). This one is about frame count, and
	// it directly bounds how long a token can be invisible on screen.
	//
	// 50ms matches the existing tool-call debounce (ai/acp_debounce.go) and sits
	// well under the frontend's own 300ms render throttle, so coalescing adds no
	// perceptible latency and cannot cause an extra render.
	wsCoalesceInterval = 50 * time.Millisecond
	// waitStreamsPollInterval is the polling period for WaitStreamsDrained.
	// Far below the 500ms flush window and the shutdown deadline, so it adds
	// no meaningful latency to a graceful stop.
	waitStreamsPollInterval = 25 * time.Millisecond

	// slowFinalizeThreshold is how long Finalize may take before its phase
	// breakdown is logged. Finalize sits between the agent stopping and the
	// terminal WS event the frontend waits on, so a slow one is exactly the
	// "cancel hangs for seconds" symptom. Normal finalize is well under this.
	slowFinalizeThreshold = 500 * time.Millisecond

	// slowSummarizeThreshold is how long triggerChatSummarization may take
	// before it is reported. It is called synchronously from Finalize, so its
	// duration is added directly to how long a cancel takes to appear finished.
	slowSummarizeThreshold = 200 * time.Millisecond

	// slowCancelThreshold is how long CancelSession may spend on the caller's
	// goroutine (the WS read loop) before it is reported. Cancelling the agent
	// context is instant; anything beyond that is event bookkeeping and push
	// I/O, which is what makes the cancel request itself feel slow.
	slowCancelThreshold = 200 * time.Millisecond
)

// finalizeTimer accumulates per-phase durations for one Finalize call and logs
// them as a single structured line. Finalize runs on the critical path of a
// user cancel: the UI cannot clear its "stopping" state until the terminal
// event is emitted after Finalize returns. Splitting the phases is what
// distinguishes "the agent ignored the cancel" from "the agent stopped
// instantly and we then spent seconds writing to disk".
//
// Phases are recorded individually rather than as a running total so a caller
// can add a new phase without renumbering the existing ones.
type finalizeTimer struct {
	sessionID string
	start     time.Time
	phases    []any // slog attrs, in execution order
}

func newFinalizeTimer(sessionID string) *finalizeTimer {
	return &finalizeTimer{sessionID: sessionID, start: time.Now()}
}

// phase records the duration of one named step. The returned func is called
// when the step finishes.
func (ft *finalizeTimer) phase(name string) func() {
	began := time.Now()
	return func() {
		ft.phases = append(ft.phases, slog.Duration(name, time.Since(began)))
	}
}

// done logs the phase breakdown when the total exceeds slowFinalizeThreshold.
// Below the threshold the log would be noise: Finalize is on every turn's
// completion path, and a fast one needs no explanation.
func (ft *finalizeTimer) done(cancelReason string, blockCount int) {
	total := time.Since(ft.start)
	if total < slowFinalizeThreshold {
		return
	}
	attrs := make([]any, 0, 4+len(ft.phases))
	attrs = append(attrs,
		slog.String("session", ft.sessionID),
		slog.String("cancel_reason", cancelReason),
		slog.Int("blocks", blockCount),
		slog.Duration("total", total),
	)
	attrs = append(attrs, ft.phases...)
	slog.Warn("finalize: slow", attrs...)
}

// RunConfig configures a single SessionExecutor execution.
type RunConfig struct {
	Mode ExecutionMode

	// --- Common fields ---
	ProjectPath        string
	BackendName        string
	SessionID          string
	AgentID            string
	ChatRequest        ai.ChatRequest
	FileDir            string
	StreamingMessageID int64 // ID of the streaming assistant message placeholder (for tool call DB upsert)

	// --- ModeInteractive only ---
	// LocalizeError formats error messages for display.
	// If nil, err.Error() is used. The handler provides an i18n implementation;
	// the scheduler provides nil (raw error strings).
	LocalizeError func(err error, key string, args map[string]any) string

	// --- ModeScheduled only ---
	TaskID      int64  // associated scheduled_tasks.id (0 for interactive)
	ExecutionID int64  // associated task_executions.id (0 for interactive)
	TriggerType string // "auto" | "manual"
}

// RunResult captures the outcome of a single SessionExecutor execution.
type RunResult struct {
	// Err is non-nil if the execution failed to start or encountered a fatal error.
	Err error
	// CancelReason is "user", "disconnect", or "" (normal completion).
	CancelReason string
	// Empty is true if the AI produced no content blocks.
	Empty bool
	// ReceivedTerminal is true if a "done" or "error" event was received from
	// the backend. False indicates the channel closed without a terminal event,
	// which typically means the CLI process crashed (OOM, SIGKILL).
	ReceivedTerminal bool

	// Blocks is the final accumulated content blocks from the AI response.
	Blocks []model.ContentBlock
	// Metadata contains token usage, cost, duration, and other response metadata.
	Metadata *ai.Metadata

	// WallMs is the wall-clock duration of the execution in milliseconds.
	WallMs int
	// FirstContentMs is the time to first content event for performance diagnosis.
	FirstContentMs int
	// MsgID is the database message ID after finalization (0 if not yet finalized).
	MsgID int64
}

// SessionExecutor handles the full lifecycle of a single AI session execution.
// It unifies the event loop logic for both interactive chat and tasks,
// with mode-specific behavior controlled by RunConfig.
//
// The caller is responsible for:
//   - Creating and managing the context (including cancel functions)
//   - Setting session running state (TrySetSessionRunning / SetSessionRunning)
//   - Handling post-execution logic (WS terminal events, drain loop, task status updates)
type SessionExecutor struct {
	cfg RunConfig
	ctx context.Context

	// mu guards the accumulated state below. It is normally owned by the single
	// event-loop goroutine (handleNonTerminalEvent/buildResult/Finalize all run
	// there), but FlushStreamingNow on the graceful-shutdown path reads the same
	// state concurrently from another goroutine — the mutex makes that read safe.
	mu sync.Mutex

	// Internal state accumulated during execution
	blocks           []model.ContentBlock
	responseMetadata *ai.Metadata
	receivedTerminal bool
	wallStart        int64 // unix millis at execution start
	// toolStarts tracks the start time of each tool call (by tool ID) so the
	// wall-clock duration can be computed when the tool completes.
	toolStarts map[string]time.Time
	// lastFlush is the last time flushStreamingMessage wrote to the DB.
	// Used to rate-limit streaming persistence (flushInterval) so a burst of
	// incremental events (e.g. ACP thinking deltas) does not saturate the
	// consumer with full-block JSON marshal + SQLite writes.
	lastFlush time.Time
	// forceIncludeThinking is set by the graceful-shutdown flush
	// (FlushStreamingNow → flushStreamingLocked(true)). Once set, subsequent
	// rate-limited flushes keep thinking in the content instead of stripping it
	// — otherwise a flush(false) racing the process exit would overwrite the
	// just-persisted thinking with a thinking-less body while chat_thinking
	// already holds records the frontend would never lazy-load.
	forceIncludeThinking bool

	// pendingToolCalls tracks tool-call IDs whose DB row (chat_tool_calls)
	// has not yet been upserted. Per-event upsert calls were moved into the
	// 500ms flush window. Storing IDs (not pointers into e.blocks) is safe
	// across append reallocations — the flush re-scans e.blocks for the latest
	// block state. The set makes the batch idempotent when a tool receives many
	// incremental updates before the flush runs.
	pendingToolCalls map[string]struct{}

	// pendingContextPatches accumulates mode/thinking-effort/usage state changes
	// that need persisting into chat_sessions.context_state. Deduplicated by
	// field key so a burst of usage_update events writes once per flush window.
	pendingContextPatches map[string]string

	// lastWrittenContent is the content JSON most recently written to the
	// streaming row. flushStreamingLocked skips the full-row UPDATE when the
	// freshly-marshaled content equals this value, so an unchanged stream does
	// not re-marshal + re-write every 500ms. Comparing marshaled output (rather
	// than a dirty flag) is robust to direct e.blocks mutations in tests and
	// keeps the write count proportional to actual changes.
	lastWrittenContent string

	// thinkingFlushed tracks, per thinking block (by think_id), how much of its
	// text has been persisted to chat_thinking as incremental seq chunks. The
	// streaming flush appends only b.Text[flushedBytes:] at the next seq, so a
	// long running thinking block (tens of KB) is never re-written in full on
	// every 500ms window. Persisted in memory (single writer inside the event
	// loop, under e.mu) — not re-derived from the DB on each flush.
	thinkingFlushed map[string]*thinkingFlushState

	// coalescer merges consecutive content/thinking deltas into one WS frame.
	// Driven exclusively by the event-loop goroutine (handleNonTerminalEvent and
	// the ticker in RunWithChannel), so it needs no lock of its own.
	coalescer *streamCoalescer
}

// thinkingFlushState is the per-block incremental flush cursor. nextSeq is the
// seq value the next delta chunk will be stored at; flushedBytes is the length
// of b.Text already persisted (everything before it is durable).
type thinkingFlushState struct {
	nextSeq      int
	flushedBytes int
}

// NewSessionExecutor creates a new executor for the given configuration.
// The caller retains ownership of the context — the executor does NOT derive
// a new context with its own cancel function. This prevents double-cancel
// hierarchies where the cancellation infrastructure can't reach the executor's
// inner context.
func NewSessionExecutor(ctx context.Context, cfg RunConfig) *SessionExecutor {
	e := &SessionExecutor{
		cfg:                   cfg,
		ctx:                   ctx,
		toolStarts:            make(map[string]time.Time),
		pendingToolCalls:      make(map[string]struct{}),
		pendingContextPatches: make(map[string]string),
		thinkingFlushed:       make(map[string]*thinkingFlushState),
	}
	// The coalescer forwards merged deltas through the real WS fan-out. Injected
	// as a callback so the coalescer stays free of executor state and can be
	// unit-tested on its own.
	e.coalescer = &streamCoalescer{emit: e.emitStreamEvent}
	// Register so graceful shutdown can flush this stream's accumulated blocks.
	// Removed by unregisterActiveStream once the executor has finished.
	activeStreams.Store(cfg.SessionID, e)
	return e
}

// emitStreamEvent is the coalescer's sink: the actual per-event WS fan-out.
// Split out from forwardEvent so the coalescer's buffer boundary and the
// transport are separable (and so emit never re-enters the coalescer).
func (e *SessionExecutor) emitStreamEvent(event ai.StreamEvent) {
	ws.EmitToSession(e.cfg.SessionID, event)
}

// flushCoalesced pushes any buffered delta to WS clients immediately.
func (e *SessionExecutor) flushCoalesced() {
	e.coalescer.flush()
}

// unregisterActiveStream removes the executor from the active-streams registry.
// Called after the executor has finished (RunWithChannel terminal or Finalize),
// so a graceful shutdown does not flush an already-finalized stream.
func (e *SessionExecutor) unregisterActiveStream() {
	activeStreams.Delete(e.cfg.SessionID)
}

// FlushStreamingNow forces a final persistence of accumulated content for every
// actively streaming session. It is called by the server's graceful-shutdown
// path (SIGINT/SIGTERM) BEFORE the HTTP server is drained, so a restart loses
// only the content that arrived within the last rate-limit window.
//
// Each active executor is flushed with includeThinking=true: thinking blocks are
// written into the streaming row content (slimmed) and recorded into
// chat_thinking, so a restarted server keeps the reasoning content that the
// per-500ms flush skips. The flush is mutex-guarded against the event-loop
// goroutine and sets a sticky flag so no racing rate-limited flush can strip
// the thinking back out.
//
// This is a one-shot best-effort snapshot: executors that finish concurrently
// after being iterated still finalize normally; executors that keep streaming
// while the process is shutting down are left as streaming=1 rows, which the
// startup orphan-cleanup marks as cancelled (preserving whatever was flushed).
func FlushStreamingNow() {
	activeStreams.Range(func(key, value any) bool {
		if e, ok := value.(*SessionExecutor); ok {
			e.flushStreamingLocked(true)
		}
		return true
	})
}

// WaitStreamsDrained blocks until every active SessionExecutor has finished
// (i.e. Finalize has persisted the streaming=0 completion marker to the DB),
// or ctx is done. It is called by the graceful-shutdown path AFTER
// FlushStreamingNow so the AI goroutines get a chance to drain their event
// channels and finalize the final few hundred ms of output instead of having
// the process exit mid-Finalize (which leaves streaming=1 rows behind).
//
// The activeStreams registry has exactly the right semantics for this wait:
// entries are registered in NewSessionExecutor (after the streaming placeholder
// row exists) and removed by Finalize AFTER FinalizeStreamingMessage has set
// streaming=0. So an empty registry means every known stream has been fully
// persisted.
//
// One caveat: RunWithChannel's deferred unregister runs between the event loop
// exiting and Finalize running, so an executor can be briefly absent from the
// registry before its streaming=0 is written. If this wait lands in that tiny
// window it may return "drained" early. That is safe in the shutdown sequence
// because FlushStreamingNow has already snapshotted the streaming row (with
// thinking) and Finalize does not depend on the AI process staying alive —
// the stream still finalizes while the process shuts down.
//
// Polling is used instead of a condition variable because the registry is a
// sync.Map with no add/remove hooks; 25ms is far below the 500ms flush window
// and a 5s shutdown deadline, so it adds no meaningful latency.
func WaitStreamsDrained(ctx context.Context) {
	for {
		empty := true
		activeStreams.Range(func(_, _ any) bool {
			empty = false
			return false // stop iteration at first entry
		})
		if empty {
			return
		}
		select {
		case <-ctx.Done():
			slog.Warn("WaitStreamsDrained: deadline reached with streams still active",
				slog.Int("active", activeStreamCount()))
			return
		case <-time.After(waitStreamsPollInterval):
		}
	}
}

// activeStreamCount returns the number of entries in the active-streams
// registry, used for shutdown diagnostics.
func activeStreamCount() int {
	n := 0
	activeStreams.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

// WaitSessionStreamDrained blocks until the SessionExecutor for the given
// session has finished (its Finalize has persisted the streaming=0 marker), or
// the timeout elapses. Unlike WaitStreamsDrained (which waits for ALL sessions),
// this waits only for one session's executor.
//
// It is used by the rewind handler: CancelSession cancels the Go context but is
// asynchronous — the executor goroutine still drains its event channel and runs
// Finalize afterwards. Truncating the history in that window lets a late
// FinalizeStreamingMessage / UpdateStreamingMessage land on the preserved anchor
// row. Waiting for the executor to unregister closes that window.
//
// On timeout it logs a warning and returns (best-effort — same exposure as
// Archive/Destroy which do not wait at all).
func WaitSessionStreamDrained(sessionID string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		if _, ok := activeStreams.Load(sessionID); !ok {
			return
		}
		if time.Now().After(deadline) {
			slog.Warn("WaitSessionStreamDrained: deadline reached with session stream still active",
				slog.String("session", sessionID))
			return
		}
		time.Sleep(waitStreamsPollInterval)
	}
}

// handleNonTerminalEvent processes a single non-terminal stream event.
//
//nolint:gocyclo // multiple event-type branches (content_reset, tool, metadata, context-state, flush gate) are inherently branchy
func (e *SessionExecutor) handleNonTerminalEvent(event ai.StreamEvent) {
	// No flush-before-dispatch here on purpose. Every client-visible emission in
	// this executor goes through forwardEvent → coalescer.add, and the coalescer
	// itself flushes any buffered delta before emitting a non-delta event. That
	// makes the ordering invariant (a barrier must not overtake buffered text)
	// hold by construction, in one place, rather than being re-asserted at each
	// call site. Types below that return without forwarding (session_capture,
	// compact_detected) emit nothing, so they cannot reorder anything.

	// content_reset: clear accumulated blocks from a failed Prompt before retry.
	// Sent by ACPBackend.ExecuteStream when the first Prompt fails due to peer
	// disconnect and the retry Prompt will re-emit the full response. Without
	// this, AccumulateBlock would append the retry's content onto the stale
	// partial content from the first attempt, producing duplicated text.
	if event.Type == eventTypeContentReset {
		e.mu.Lock()
		slog.Warn("session executor: content_reset, clearing accumulated blocks",
			slog.String("session", e.cfg.SessionID),
			slog.Int("blocks_before", len(e.blocks)))
		e.blocks = nil
		e.responseMetadata = nil
		e.lastFlush = time.Time{}
		e.toolStarts = make(map[string]time.Time)
		e.pendingToolCalls = make(map[string]struct{})
		e.pendingContextPatches = make(map[string]string)
		e.lastWrittenContent = ""
		e.thinkingFlushed = make(map[string]*thinkingFlushState)
		e.mu.Unlock()
		// Reset the streaming message in DB to empty so stale partial content
		// doesn't persist if the retry Prompt fails or the server crashes.
		emptyContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{}}) // safe: known structure
		if err := UpdateStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, string(emptyContent)); err != nil {
			slog.Error("failed to reset streaming message after content_reset",
				slog.String("session", e.cfg.SessionID),
				slog.String("err", err.Error()))
		}
		// Delete stale tool call rows from the first (failed) Prompt.
		// The retry will re-insert them as fresh entries via upsertToolCallToDB.
		if e.cfg.StreamingMessageID > 0 {
			if _, err := WriteExec("DELETE FROM chat_tool_calls WHERE message_id = ?", e.cfg.StreamingMessageID); err != nil {
				slog.Error("failed to delete stale tool calls after content_reset",
					slog.Int64("message_id", e.cfg.StreamingMessageID),
					slog.String("err", err.Error()))
			}
			// Also delete thinking rows the periodic flush wrote for the first
			// (failed) Prompt. Without this, a crash after the retry would leave
			// the stale thinking behind — the frontend would lazy-load it by the
			// (unchanged) message_id + think_id and show reasoning from the
			// failed attempt.
			if _, err := WriteExec("DELETE FROM chat_thinking WHERE message_id = ?", e.cfg.StreamingMessageID); err != nil {
				slog.Error("failed to delete stale thinking after content_reset",
					slog.Int64("message_id", e.cfg.StreamingMessageID),
					slog.String("err", err.Error()))
			}
		}
		// Forward to WS clients so the frontend clears its rendered partial content.
		e.forwardEvent(event)
		return
	}

	// session_capture: persist external session ID
	if event.Type == "session_capture" {
		if event.Content != "" {
			e.captureExternalSessionID(event.Content)
		}
		return
	}

	// steer_boundary: a mid-turn injected message just entered the running turn.
	// Split the assistant reply here so the injected question renders between the
	// "before" and "after" halves instead of below a reply that is still
	// streaming. Handled before the generic forward/accumulate path because the
	// split itself owns persisting and announcing the new row.
	if event.Type == "steer_boundary" {
		e.splitAtSteerBoundary(event)
		return
	}

	// compact_detected: the agent just compacted this session's context. Persist
	// the flag so the NEXT turn re-injects the system prompt, then swallow the
	// event — it is an internal signal, not client-visible content.
	if event.Type == "compact_detected" {
		MarkSessionCompacted(e.cfg.SessionID)
		return
	}

	// Inject per-tool duration into completion events before forwarding,
	// so WS clients and AccumulateBlock both see it.
	if event.Type == eventTypeToolUse || event.Type == eventTypeToolResult {
		e.trackToolDuration(&event)
	}

	// Forward event to WS clients via StreamHub
	e.forwardEvent(event)

	// Accumulate block. Guarded so FlushStreamingNow (shutdown goroutine) can
	// read e.blocks concurrently without a data race.
	e.mu.Lock()
	ai.AccumulateBlock(&e.blocks, event)
	// Queue tool-call upserts for the next flush window instead of writing per
	// event — a burst of incremental tool_use updates would otherwise issue one
	// SQLite write per event and stall the consumer.
	if event.Type == eventTypeToolUse || event.Type == eventTypeToolResult {
		if event.Tool != nil && event.Tool.ID != "" {
			e.pendingToolCalls[event.Tool.ID] = struct{}{}
		}
	}
	e.mu.Unlock()

	// metadata capture
	if event.Type == contentKeyMetadata && event.Meta != nil {
		e.mu.Lock()
		e.responseMetadata = event.Meta
		e.mu.Unlock()
		if event.Meta.SessionID != "" {
			e.captureExternalSessionID(event.Meta.SessionID)
		}
	}

	// Context-state persistence (mode/thinking-effort/usage) is deferred to the
	// next flush window so a burst of usage_update events writes once instead of
	// per event. The event is queued into the pending map; the flush writes
	// chat_sessions.context_state atomically.
	if event.Type == "mode_update" || event.Type == "thinking_effort_update" || event.Type == "usage_update" {
		e.persistContextStateToPending(event)
	}

	// Incremental persistence (rate-limited). Persisting every N events is too
	// aggressive for ACP backends that emit bursts of incremental deltas — the
	// full-block JSON marshal + SQLite write stalls the consumer and the stream
	// channel fills, dropping events. Persist at most once per flushInterval.
	// Not under e.mu: flushStreamingLocked takes e.mu itself, and lastFlush is
	// only touched by this single event-loop goroutine.
	if time.Since(e.lastFlush) >= flushInterval {
		e.flushStreamingMessage()
		e.lastFlush = time.Now()
	}
}

// forwardEvent forwards an event to WS clients via StreamHub, coalescing
// consecutive content/thinking deltas into a single frame.
// Context-state persistence (mode, thinking effort, usage) is deferred to the
// flush window via persistContextStateToPending — see handleNonTerminalEvent.
func (e *SessionExecutor) forwardEvent(event ai.StreamEvent) {
	forwardEvent := event
	if (event.Type == eventTypeToolUse || event.Type == eventTypeToolResult) && event.Tool != nil {
		meta := ai.ExtractToolCallMeta(event)
		forwardEvent.ToolMeta = &meta
	}

	// Deltas are buffered and merged; every other type is an ordering barrier
	// the coalescer flushes before emitting. The ordering guarantee lives in the
	// coalescer, so this stays a single call.
	e.coalescer.add(forwardEvent)
}

// RunWithChannel executes the event loop against a pre-built event channel.
// This is the core event processing logic shared by both interactive and scheduled modes.
// The caller is responsible for creating the backend and obtaining the event channel.
func (e *SessionExecutor) RunWithChannel(eventCh <-chan ai.StreamEvent) RunResult {
	e.wallStart = time.Now().UnixMilli()
	wallStart := time.Now()

	// flushTicker guarantees that sparse-but-ongoing streams (e.g. a long tool
	// call with few content events) still get persisted periodically, even when
	// no event trips the rate-limited flush in handleNonTerminalEvent.
	flushTicker := time.NewTicker(flushInterval)
	defer flushTicker.Stop()

	// coalesceTicker releases buffered content/thinking deltas on a short window.
	// Separate from flushTicker: that one bounds DB write amplification (500ms,
	// a stale row is invisible to the user), this one bounds how long a token
	// can stay off-screen.
	coalesceTicker := time.NewTicker(wsCoalesceInterval)
	defer coalesceTicker.Stop()

	// The terminal "done"/"error"/"cancelled" events are NOT emitted from here —
	// the handler (handler/chat.go markDoneAndSendFinal) and the scheduler send
	// them after RunWithChannel AND Finalize have returned. Any delta still
	// buffered at that point would arrive AFTER the terminal event, and the
	// frontend has already left streaming state by then, so its content handler
	// would drop it (no streaming message to append to). Flushing on exit closes
	// that window.
	//
	// A defer (rather than a flush before each of the three returns below)
	// covers every exit path, including ones added later. It is safe after the
	// return value is computed: flushing only performs WS fan-out and does not
	// touch e.blocks or any field buildResult reads.
	defer e.flushCoalesced()
	// NOTE: unregistration is deferred to Finalize (after streaming=0 is
	// written). Registering here in NewSessionExecutor and unregistering only
	// there keeps the activeStreams registry a faithful "stream not yet fully
	// persisted" set, so WaitStreamsDrained can reliably wait for finalization
	// during graceful shutdown. Unregistering on RunWithChannel exit would open
	// a window where the executor is absent from the registry while its
	// Finalize (the actual durability point) has not run yet.

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed without a terminal event — CLI process crash
				return e.buildResult(false, wallStart)
			}
			if event.Type == eventTypeDone || event.Type == eventTypeError {
				e.receivedTerminal = true
				// For "error" events, AccumulateBlock handles them.
				// We process the error event but still finalize.
				if event.Type == eventTypeError {
					ai.AccumulateBlock(&e.blocks, event)
					e.upsertToolCallToDB(event)
				}
				return e.buildResult(true, wallStart)
			}

			e.handleNonTerminalEvent(event)

		case <-e.ctx.Done():
			return e.buildResult(e.receivedTerminal, wallStart)

		case <-flushTicker.C:
			if len(e.blocks) > 0 {
				e.flushStreamingMessage()
				e.lastFlush = time.Now()
			}

		case <-coalesceTicker.C:
			// Release buffered deltas. A no-op when nothing is pending (a long
			// tool call with no output), so this costs a nil check per window.
			e.flushCoalesced()
		}
	}
}

// postProcessBlocks applies finalize post-processing on blocks:
// clawbench-ask-question conversion, rejected-tool removal, thinking-block merging.
// Shared by buildResult and Finalize to prevent divergence.
// NOTE: persistAskToolCalls must be called separately after Finalize
// uses postProcessBlocks, to avoid double-persisting from buildResult.
func (e *SessionExecutor) postProcessBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Ask-question detection (interactive mode only)
	if e.cfg.Mode == ModeInteractive {
		if ai.StringsContainsAnyBlock(blocks, "<clawbench-ask-question") {
			blocks = ai.ConvertAskQuestionBlocks(blocks)
		}
	}

	// Common block post-processing (idempotent, cheap)
	blocks = ai.RemoveRejectedToolBlocks(blocks)
	blocks = ai.MergeConsecutiveThinkingBlocks(blocks)

	return blocks
}

// persistAskToolCalls writes converted AskUserQuestion tool blocks to
// the chat_tool_calls table. These blocks were created by
// ConvertAskQuestionBlocks and missed the normal upsertToolCallToDB
// path during the event loop. Must be called after every postProcessBlocks
// call that writes blocks to the DB (currently Finalize).
func (e *SessionExecutor) persistAskToolCalls(blocks []model.ContentBlock) {
	if e.cfg.StreamingMessageID <= 0 || e.cfg.SessionID == "" {
		return
	}
	for i := range blocks {
		b := &blocks[i]
		if b.Type == "tool_use" && strings.HasPrefix(b.ID, "ask-") && b.Name == "AskUserQuestion" {
			inputJSON, _ := json.Marshal(b.Input)
			if err := UpsertToolCall(
				e.cfg.StreamingMessageID, e.cfg.SessionID,
				b.ID, b.Name, inputJSON,
				b.Output, b.Status, b.Summary, b.Done, b.DurationMs,
			); err != nil {
				slog.Warn("upsert converted AskUserQuestion tool call failed",
					slog.String("toolID", b.ID),
					slog.String("err", err.Error()))
			}
		}
	}
}

// buildResult constructs the final RunResult from the executor's accumulated state.
func (e *SessionExecutor) buildResult(receivedTerminal bool, wallStart time.Time) RunResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	wallMs := int(time.Since(wallStart).Milliseconds())

	// Apply finalize post-processing on blocks
	blocks := e.postProcessBlocks(e.blocks)

	// Inject WallMs into metadata
	if e.responseMetadata == nil {
		e.responseMetadata = &ai.Metadata{}
	}
	e.responseMetadata.WallMs = wallMs
	if extID := GetExternalSessionID(e.cfg.SessionID); extID != "" {
		e.responseMetadata.SessionID = extID
	}

	// Determine cancel reason (interactive mode only)
	cancelReason := ""
	if e.cfg.Mode == ModeInteractive {
		cancelReason = GetAndClearCancelReason(e.cfg.SessionID)
	}

	// Determine if empty
	empty := len(blocks) == 0 && receivedTerminal && cancelReason == ""

	return RunResult{
		ReceivedTerminal: receivedTerminal,
		CancelReason:     cancelReason,
		Empty:            empty,
		Blocks:           blocks,
		Metadata:         e.responseMetadata,
		WallMs:           wallMs,
	}
}

// captureExternalSessionID persists the external session ID if not already set.
func (e *SessionExecutor) captureExternalSessionID(externalID string) {
	if externalID == "" {
		return
	}
	existingExtID := GetExternalSessionID(e.cfg.SessionID)
	if existingExtID == "" {
		if err := UpdateExternalSessionID(e.cfg.SessionID, externalID); err != nil {
			slog.Error("failed to save external session ID",
				slog.String("session", e.cfg.SessionID),
				slog.String("external_id", externalID),
				slog.String("err", err.Error()))
		}
	}
}

// trackToolDuration records tool start times and injects the computed wall-clock
// duration into completion events. The duration is cumulative from the first
// tool_use event for a tool ID:
//   - tool_use done=false: marks the start.
//   - tool_use done=true: input streaming is complete and the tool begins
//     executing — an interim (cumulative) duration is injected so backends
//     that never emit tool_result still get a value. The start is kept.
//   - tool_result: the tool actually finished — the final duration is injected
//     and the start is released.
//
// The duration propagates to the WS payload, the accumulated block, and the
// chat_tool_calls upsert. If no start was recorded (e.g. the first event is
// already done), duration stays 0 (unknown).
func (e *SessionExecutor) trackToolDuration(event *ai.StreamEvent) {
	if event.Tool == nil || event.Tool.ID == "" {
		return
	}
	if event.Type == eventTypeToolResult {
		if start, ok := e.toolStarts[event.Tool.ID]; ok {
			event.Tool.DurationMs = int(time.Since(start).Milliseconds())
			delete(e.toolStarts, event.Tool.ID)
		}
		return
	}
	if event.Tool.Done {
		if start, ok := e.toolStarts[event.Tool.ID]; ok {
			event.Tool.DurationMs = int(time.Since(start).Milliseconds())
		}
		return
	}
	if _, ok := e.toolStarts[event.Tool.ID]; !ok {
		e.toolStarts[event.Tool.ID] = time.Now()
	}
}

// upsertToolCallToDB persists tool call data to the chat_tool_calls table.
// Only runs for tool_use and tool_result events when StreamingMessageID is set.
// This legacy per-event path is kept for callers that need immediate
// persistence (drainRemainingEvents). The event-loop path defers upserts to
// the batched flush (flushPendingToolCalls) instead.
func (e *SessionExecutor) upsertToolCallToDB(event ai.StreamEvent) {
	if event.Tool == nil || e.cfg.StreamingMessageID == 0 || e.cfg.SessionID == "" {
		return
	}
	// Find the matching block in accumulated blocks
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.blocks) - 1; i >= 0; i-- {
		if e.blocks[i].Type == eventTypeToolUse && e.blocks[i].ID == event.Tool.ID {
			block := &e.blocks[i]
			inputJSON, _ := json.Marshal(block.Input)
			if err := UpsertToolCall(
				e.cfg.StreamingMessageID, e.cfg.SessionID,
				block.ID, block.Name, inputJSON,
				block.Output, block.Status, block.Summary, block.Done, block.DurationMs,
			); err != nil {
				slog.Warn("upsert tool call failed",
					slog.String("toolID", block.ID),
					slog.String("err", err.Error()))
			}
			return
		}
	}
}

// findToolBlock returns a pointer to the accumulated block for the given tool
// ID, or nil. The caller must hold e.mu. Used by the batched tool-call flush to
// read the latest block state at flush time (pendingToolCalls stores IDs, so
// this re-scan is robust to append reallocations).
func (e *SessionExecutor) findToolBlock(toolID string) *model.ContentBlock {
	for i := len(e.blocks) - 1; i >= 0; i-- {
		if e.blocks[i].Type == eventTypeToolUse && e.blocks[i].ID == toolID {
			return &e.blocks[i]
		}
	}
	return nil
}

// persistContextStateToPending extracts a context_state patch from a stream
// event and queues it for the next batched flush. Mirrors
// PersistContextStateFromEvent but defers the DB write.
func (e *SessionExecutor) persistContextStateToPending(event ai.StreamEvent) {
	patches := buildContextStatePatch(event)
	if len(patches) == 0 {
		return
	}
	e.mu.Lock()
	for k, v := range patches {
		e.pendingContextPatches[k] = v
	}
	e.mu.Unlock()
}

// flushPendingToolCalls upserts every queued tool-call row in one pass and
// clears the queue. Runs inside the flush window (under e.mu).
func (e *SessionExecutor) flushPendingToolCalls() {
	if e.cfg.StreamingMessageID == 0 || e.cfg.SessionID == "" || len(e.pendingToolCalls) == 0 {
		return
	}
	for toolID := range e.pendingToolCalls {
		block := e.findToolBlock(toolID)
		if block == nil {
			continue
		}
		inputJSON, _ := json.Marshal(block.Input)
		if err := UpsertToolCall(
			e.cfg.StreamingMessageID, e.cfg.SessionID,
			block.ID, block.Name, inputJSON,
			block.Output, block.Status, block.Summary, block.Done, block.DurationMs,
		); err != nil {
			slog.Warn("flush tool call failed",
				slog.String("toolID", block.ID),
				slog.String("err", err.Error()))
		}
	}
	e.pendingToolCalls = make(map[string]struct{})
}

// flushPendingContextState applies every queued context_state patch in one
// atomic UPDATE and clears the queue. Runs inside the flush window.
func (e *SessionExecutor) flushPendingContextState() {
	if len(e.pendingContextPatches) == 0 {
		return
	}
	PatchContextStateMerge(e.cfg.SessionID, e.pendingContextPatches)
	e.pendingContextPatches = make(map[string]string)
}

// flushPendingThinking persists the thinking-block text that grew since the
// last flush window into chat_thinking as incremental seq chunks, so a hard
// crash loses at most the thinking that grew since the last flush instead of
// the whole block. The chat_history.content row never carries thinking full
// text — the text lives here in chat_thinking, exactly like tool calls live in
// chat_tool_calls. DONE blocks are referenced from the content row by a slim
// think_id marker (see flushStreamingLocked) for mid-stream refresh recovery.
//
// Incremental semantics: each thinking block tracks (nextSeq, flushedBytes) in
// e.thinkingFlushed. Each flush appends only b.Text[flushedBytes:] as the next
// seq chunk and advances the cursor on success. A long running block (tens of
// KB) is therefore never rewritten in full every 500ms — each flush writes only
// the delta since the previous window. Failure does not advance the cursor, so
// the next flush retries the same seq idempotently (AppendThinkingSegment is
// ON CONFLICT upsert).
//
// ThinkID stability: a thinking block gets a stable ID on first flush and
// reuses it on every subsequent flush, so the same (message_id, think_id)
// always refers to the latest text. Finalize reuses these IDs via
// slimThinkingInContent (blocks that already carry think_id are not
// regenerated), so no orphan rows and no duplicates.
func (e *SessionExecutor) flushPendingThinking() {
	if e.cfg.StreamingMessageID == 0 || e.cfg.SessionID == "" {
		return
	}
	// Once the graceful-shutdown forced flush has run, persistThinkingToDB (in
	// flushStreamingLocked) owns thinking persistence entirely: it deletes all
	// chunks and rewrites the full text as a single seq=0 row on every flush.
	// Incremental appends here would be deleted moments later by that rewrite —
	// skip them so force mode stays full-rewrite and normal mode stays
	// incremental.
	if e.forceIncludeThinking {
		return
	}
	for i := range e.blocks {
		b := &e.blocks[i]
		if b.Type != blockTypeThinking {
			continue
		}
		// Stable ID: generate on first appearance, reuse afterwards.
		if b.ThinkID == "" {
			b.ThinkID = generateThinkingID()
		}
		if b.Text == "" {
			continue
		}
		st := e.thinkingFlushed[b.ThinkID]
		if st == nil {
			st = &thinkingFlushState{}
			e.thinkingFlushed[b.ThinkID] = st
		}
		// Nothing grew since the last flush (or content_reset shrank the block
		// past the cursor — defensive; content_reset clears the map anyway).
		if len(b.Text) <= st.flushedBytes {
			continue
		}
		delta := b.Text[st.flushedBytes:]
		if err := AppendThinkingSegment(e.cfg.StreamingMessageID, e.cfg.SessionID, b.ThinkID, st.nextSeq, delta); err != nil {
			slog.Warn("flush thinking delta failed",
				slog.String("thinkID", b.ThinkID),
				slog.Int("seq", st.nextSeq),
				slog.String("err", err.Error()))
			continue // do not advance — retry same seq next window
		}
		st.flushedBytes = len(b.Text)
		st.nextSeq++
	}
}

// thinkingPersisted reports whether at least one seq chunk for thinkID was
// successfully appended to chat_thinking (flushedBytes > 0). flushPendingThinking
// creates the state entry before the first append attempt, so flushedBytes — not
// entry existence — is the success signal (an AppendThinkingSegment failure
// leaves it 0). Used by flushStreamingLocked to decide whether a done thinking
// block is safe to reference with a slim content marker (no row to lazy-load
// means the marker would 404 on expand).
func (e *SessionExecutor) thinkingPersisted(thinkID string) bool {
	st := e.thinkingFlushed[thinkID]
	return st != nil && st.flushedBytes > 0
}

// flushStreamingMessage persists the current accumulated blocks to the database,
// along with queued tool-call upserts, context-state patches, and the full
// thinking text.
//
// The content row never carries thinking full text — the full thinking text is
// written separately to chat_thinking by flushPendingThinking (stable think_id
// upsert) on the same flush window, so a hard crash loses at most the thinking
// that grew since the last flush. DONE thinking blocks additionally get a slim
// {think_id, done:true} marker in the content row (see flushStreamingLocked) so
// a page refresh mid-stream can recover the completed reasoning via lazy-load.
// Finalization (persistThinkingToDB) produces the final slim markers in the
// completed message's content.
//
// Batched writes (tool-call upserts + context-state patches + thinking text)
// are flushed every interval regardless of content changes so a burst of
// tool/usage events that did not alter the content JSON still reaches the DB
// promptly. The streaming row itself is only rewritten when the marshaled
// content actually changed.
// splitAtSteerBoundary finalizes the assistant reply accumulated so far and
// opens a new streaming message for the content that follows a mid-turn
// injection. It is the executor half of the "split at the insertion point"
// feature; the ACP layer detects the boundary (see mapACPSessionUpdate's
// UserMessageChunk branch) and the backend policy registers the echo it expects.
//
// Why split at all: without it the injected question is persisted with a higher
// DB id than the streaming assistant row, so it renders BELOW a reply that is
// still being generated — the user sees their message with nothing after it.
// Splitting makes the DB id order match the conversational order:
//
//	Q1(1) → assistant·before(2) → Q2 injected(3) → assistant·after(4)
//
// which needs no UI special-casing: the plain id sort is already correct.
//
// Failure is non-fatal by design. A split is a presentation improvement; if the
// "before" row cannot be finalized or the "after" row cannot be created, we log
// and keep accumulating into the ORIGINAL streaming row, degrading to the
// previous single-message behavior rather than losing content.
func (e *SessionExecutor) splitAtSteerBoundary(event ai.StreamEvent) {
	if event.SteerBoundary == nil {
		return
	}
	queueID := event.SteerBoundary.ClientUserMessageID

	// No DB (bare executor in a unit test): nothing to split, but the event must
	// still not reach the generic path (it would be accumulated as a block).
	if db == nil {
		return
	}

	if !e.splitAtSteerBoundaryLocked(queueID) {
		return
	}

	// The user injected this message from THIS session's own UI, so they are
	// provably looking at it — a stronger signal than the cancel path's, which
	// only assumes presence. Finalizing the "before" half just stamped its
	// completed_at, moving that row's timestamp past last_read_at and flipping
	// the session the user is staring at back to unread. Re-anchor last_read_at
	// now that the stamp exists.
	//
	// MAX(CURRENT_TIMESTAMP, newest completed_at) inside UpdateLastRead makes
	// this robust to the same-second race: the anchor can never land before the
	// row it must cover. Called after the lock is released — the split holds
	// e.mu across its DB writes, and this adds another write plus a WS
	// broadcast that have no reason to run under it.
	UpdateLastRead(e.cfg.SessionID)
}

// splitAtSteerBoundaryLocked performs the split and reports whether the
// "before" half was finalized (i.e. whether its completed_at was stamped, and
// therefore whether the caller must re-anchor last_read_at). Caller must NOT
// hold e.mu.
func (e *SessionExecutor) splitAtSteerBoundaryLocked(queueID string) bool {
	// Serialize against the accumulator and the flush ticker. The split replaces
	// e.blocks and the streaming-row identity, so it must not interleave with a
	// concurrent flush.
	e.mu.Lock()
	defer e.mu.Unlock()

	// Flush batched side-writes for the "before" half first, so tool-call rows
	// and thinking text produced before the injection belong to it.
	e.flushPendingToolCalls()
	e.flushPendingContextState()
	e.flushPendingThinking()

	// Finalize the "before" half: write its content and clear streaming=1. The
	// row keeps its id (lower than the injected question's), which is exactly the
	// ordering we want.
	beforeContent := e.buildSplitContentLocked()
	beforeID := e.cfg.StreamingMessageID
	if _, err := FinalizeStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, beforeContent); err != nil {
		slog.Error("session executor: failed to finalize assistant half at steer boundary; "+
			"continuing in the original row (no split)",
			slog.String("session", e.cfg.SessionID),
			slog.Int64("before_msg_id", beforeID),
			slog.String("err", err.Error()))
		// Nothing was finalized, so no completed_at was stamped and the session's
		// unread state is unchanged — the caller must not re-anchor.
		return false
	}

	// Open the "after" half: a fresh streaming assistant row. Its id is higher
	// than the injected question's (the question was materialized into
	// chat_history at injection time), so it lands directly below that question
	// with no anchor.
	afterID, err := CreateStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID)
	if err != nil {
		// The "before" half is already durable; the rest of the turn simply keeps
		// appending to it via UpdateStreamingMessage's "latest streaming row"
		// lookup... except there is now no streaming row, so the remaining content
		// would be lost. Re-open one to stay safe.
		slog.Error("session executor: failed to create assistant half at steer boundary; "+
			"reopening a row so remaining content is not lost",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
		if fallbackID, ferr := CreateStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID); ferr == nil {
			e.resetForSplitLocked(fallbackID)
		}
		// The "before" half IS finalized (its completed_at was stamped) even
		// though the split degraded — the unread flip must still be corrected.
		return true
	}

	slog.Info("session executor: split assistant reply at steer boundary",
		slog.String("session", e.cfg.SessionID),
		slog.Int64("before_msg_id", beforeID),
		slog.Int64("after_msg_id", afterID),
		slog.String("client_user_message_id", queueID))

	// Announce the split so every subscribed client (including one that opened
	// the session mid-turn) creates the second bubble immediately instead of
	// waiting for a reload.
	e.forwardEvent(ai.StreamEvent{
		Type:        "stream_split",
		StreamSplit: &ai.StreamSplitData{MessageID: afterID},
	})

	e.resetForSplitLocked(afterID)
	return true
}

// resetForSplitLocked re-points the executor at a newly created streaming row
// and clears all per-message accumulation, so the "after" half starts empty.
// Caller holds e.mu.
func (e *SessionExecutor) resetForSplitLocked(newMessageID int64) {
	e.cfg.StreamingMessageID = newMessageID
	e.blocks = nil
	e.responseMetadata = nil
	e.lastWrittenContent = ""
	e.lastFlush = time.Time{}
	e.toolStarts = make(map[string]time.Time)
	e.pendingToolCalls = make(map[string]struct{})
	e.pendingContextPatches = make(map[string]string)
	// Thinking is per-message (chat_thinking rows key off message_id); the
	// "before" half's thinking was already flushed above, so the "after" half
	// starts its own cursors.
	e.thinkingFlushed = make(map[string]*thinkingFlushState)
}

// buildSplitContentLocked renders the accumulated blocks as the content JSON for
// the "before" half. Thinking text is split out into chat_thinking exactly like
// a normal finalize, so the completed half is indistinguishable from any other
// finalized assistant message. Caller holds e.mu.
func (e *SessionExecutor) buildSplitContentLocked() string {
	blocks := e.postProcessBlocks(e.blocks)
	// Mirror Finalize: ConvertAskQuestionBlocks creates ask-* tool blocks that
	// the normal upsert path never saw, so persist them against the "before"
	// row. Without this the completed half would render the question as plain
	// text after a reload.
	e.persistAskToolCalls(blocks)
	content, _ := e.buildContentJSON(blocks, RunResult{Blocks: blocks}, e.responseMetadata)
	return persistThinkingToDB(content, e.cfg.StreamingMessageID, e.cfg.SessionID)
}

// flushStreamingMessage writes the accumulated blocks to the database.
func (e *SessionExecutor) flushStreamingMessage() {
	// No DB initialized (e.g. a bare executor in an isolated unit test) — there
	// is nothing to persist to. Guarding here keeps the rate-limited streaming
	// flush safe on every non-terminal event without assuming a DB exists.
	if db == nil {
		return
	}
	e.flushStreamingLocked(false)
}

// flushStreamingLocked writes the accumulated blocks to the database.
// includeThinking controls how thinking blocks are persisted in the content
// row:
//   - false (rate-limited flushes): thinking full text is excluded from content;
//     the text is upserted to chat_thinking by flushPendingThinking. DONE blocks
//     get a slim {think_id, done:true} marker at their natural position so a
//     refresh mid-stream can lazy-load the completed reasoning; in-progress
//     blocks are left out entirely (a done=false marker is the spinner regression).
//   - true (graceful-shutdown forced flush): the full thinking text is embedded
//     in content, then slimThinkingInContent extracts it into chat_thinking —
//     the one-shot durability point where the text may not have been flushed yet.
//
// The content is written as a non-slimmed JSON body; when includeThinking is
// true the thinking blocks are also recorded into chat_thinking keyed by
// message id + think_id (mirroring Finalize's persistThinkingToDB, which is a
// no-op when there is nothing to slim). Full finalization (streaming=0, RAG
// index, summarization) still happens in Finalize.
//
// The graceful-shutdown forced flush (includeThinking=true) always writes the
// streaming row: shutdown is a one-shot durability point and the cost of one
// re-marshal is irrelevant there. Rate-limited flushes skip the row write when
// nothing changed (marshaled content equals the last written value).
func (e *SessionExecutor) flushStreamingLocked(includeThinking bool) {
	// No DB initialized (e.g. a bare executor in an isolated unit test) — there
	// is nothing to persist to. Guarding here keeps the forced flush safe on the
	// graceful-shutdown path without assuming a DB exists.
	if db == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	// Batched side-writes always run: tool-call upserts and context-state
	// patches are independent of the content row. Thinking full text is also
	// flushed here (stably ID'd upserts to chat_thinking), so a hard crash
	// loses only the thinking that grew since the last flush window.
	e.flushPendingToolCalls()
	e.flushPendingContextState()
	e.flushPendingThinking()

	// Once the graceful-shutdown flush runs, keep thinking in every subsequent
	// flush. Otherwise a flush(false) racing the process exit would overwrite
	// the just-persisted thinking with a thinking-less body while chat_thinking
	// already holds records the frontend would never lazy-load (missing
	// think_id), losing the thinking permanently.
	if includeThinking {
		e.forceIncludeThinking = true
	}

	serializedBlocks := make([]model.ContentBlock, 0, len(e.blocks))
	for _, b := range e.blocks {
		if b.Type == blockTypeThinking {
			// The full thinking text NEVER goes into the rate-limited content row
			// (it lives in chat_thinking via flushPendingThinking; embedding even a
			// slim think_id marker for an IN-PROGRESS block would leak an "empty
			// thinking block" into the frontend's live placeholder — the
			// mergeStreamBlocks path (db_load after stream_start) adopts the DB's
			// non-text blocks into the live stream, and a done=false slim block
			// there renders as a perpetual loading spinner until the message
			// finalizes).
			//
			// EXCEPTION: a DONE thinking block (thinking_done received, text fully
			// persisted to chat_thinking) gets a slim {think_id, done:true} marker at
			// its natural position. A done marker renders as a collapsed chip (never
			// a spinner), and it is what lets a page refresh mid-stream recover the
			// already-completed reasoning via the /thinking lazy-load — the streaming
			// row would otherwise carry no trace of the block and the thinking would
			// be lost on reload. Finalize's persistThinkingToDB overwrites these
			// markers with the final slim content (idempotent, same think_ids), so no
			// orphan/duplicate rows are left behind.
			if e.forceIncludeThinking {
				serializedBlocks = append(serializedBlocks, b)
			} else if b.Done && b.ThinkID != "" && e.thinkingPersisted(b.ThinkID) {
				// Only blocks that actually reached chat_thinking get markers — an
				// empty done block has no row to lazy-load and would 404.
				// ParentToolCallID must ride along so a reload keeps a sub-agent's
				// thinking grouped under its parent Agent card.
				serializedBlocks = append(serializedBlocks, model.ContentBlock{
					Type:             blockTypeThinking,
					ThinkID:          b.ThinkID,
					Done:             true,
					ParentToolCallID: b.ParentToolCallID,
				})
			}
			continue
		}
		serializedBlocks = append(serializedBlocks, b)
	}
	contentMap := map[string]any{contentKeyBlocks: serializedBlocks}
	if e.responseMetadata != nil {
		contentMap[contentKeyMetadata] = e.responseMetadata
	}
	blocksJSON, _ := json.Marshal(contentMap)
	content := string(blocksJSON)

	// Rate-limited flush with no content change: skip the full-row UPDATE. The
	// content comparison uses the marshaled JSON so any direct mutation of
	// e.blocks (including from tests) is picked up. The forced shutdown flush
	// always writes the streaming row.
	if !includeThinking && content == e.lastWrittenContent {
		return
	}
	if err := UpdateStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, content); err != nil {
		slog.Error("failed to update streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
		return
	}
	e.lastWrittenContent = content
	if e.forceIncludeThinking {
		// Persist thinking blocks into chat_thinking and slim the persisted
		// content (remove thinking text, keep think_id) — identical to what
		// Finalize does, so the streaming row and chat_thinking stay consistent
		// across a restart. Finalize is idempotent over this.
		if slimContent := persistThinkingToDB(content, e.cfg.StreamingMessageID, e.cfg.SessionID); slimContent != content {
			_ = UpdateStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, slimContent)
		}
		// flushPendingThinking is guarded off while forceIncludeThinking is set
		// (persistThinkingToDB above fully owns thinking persistence in force
		// mode), so no cursor realignment is needed here.
	}
}

// injectSessionMetadata populates ACP mode, thinking effort, transport, and model
// into the response metadata from session-level state.
func (e *SessionExecutor) injectSessionMetadata(meta *ai.Metadata) {
	if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(e.cfg.SessionID); s.Mode != nil || s.Effort != nil {
		if s.Mode != nil && s.Mode.CurrentModeID != "" {
			meta.Mode = s.Mode.CurrentModeID
		}
		if s.Effort != nil && s.Effort.CurrentID != "" {
			meta.ThinkingEffort = s.Effort.CurrentID
		}
	}
	effectiveTransport := transportCLI
	if t := GetSessionTransport(e.cfg.SessionID); t != "" {
		effectiveTransport = t
	} else if agent, ok := model.Agents[e.cfg.AgentID]; ok && agent.Transport != "" {
		effectiveTransport = agent.Transport
	}
	meta.Transport = effectiveTransport

	if sessionModel := GetSessionModel(e.cfg.SessionID); sessionModel != "" {
		meta.Model = sessionModel
	}

	if extID := GetExternalSessionID(e.cfg.SessionID); extID != "" {
		meta.SessionID = extID
	}
}

// buildContentJSON serializes blocks and metadata into the DB content format,
// handling empty-response warnings and cancellation markers.
func (e *SessionExecutor) buildContentJSON(blocks []model.ContentBlock, result RunResult, meta *ai.Metadata) (string, []model.ContentBlock) {
	// Interrupt: the user redirected the turn ("interrupt and send"), they did
	// not abandon it. Persist whatever was produced WITHOUT the cancelled badge —
	// stamping "cancelled" would tell the user their reply was thrown away when
	// in fact it was cut short on purpose and the next message is already
	// running. The queue survives (see drainHandleTerminal).
	if result.CancelReason == cancelReasonInterrupt {
		// An interrupt that produced nothing must still say something: an empty
		// blocks array renders as a blank bubble with no explanation. Report it
		// as an empty result (not a cancellation — the user asked to move on).
		if len(blocks) == 0 {
			blocks = append(blocks, model.ContentBlock{
				Type:   blockTypeWarning,
				Text:   "AI returned no content",
				Reason: ai.ReasonEmpty,
			})
		}
		contentMap := map[string]any{contentKeyBlocks: blocks, contentKeyMetadata: meta}
		blocksJSON, _ := json.Marshal(contentMap)
		return string(blocksJSON), blocks
	}

	// User-initiated cancel: just mark cancelled, never add a warning block.
	// The frontend renders a clean "cancelled" badge — no alarming warning needed.
	if result.CancelReason == cancelReasonUser {
		contentMap := map[string]any{contentKeyBlocks: blocks, contentKeyMetadata: meta, statusCancelled: true}
		blocksJSON, _ := json.Marshal(contentMap)
		return string(blocksJSON), blocks
	}

	// Graceful-shutdown/restart interrupt with partial content: append a restart
	// warning block so the frontend renders the interrupted banner ("继续"
	// button) on reload — mirrors the startup orphan-cleanup behavior.
	if result.CancelReason == cancelReasonRestart {
		blocks = append(blocks, model.ContentBlock{
			Type:   blockTypeWarning,
			Text:   "Server restarted, AI response interrupted",
			Reason: ai.ReasonRestart,
		})
		contentMap := map[string]any{contentKeyBlocks: blocks, contentKeyMetadata: meta, statusCancelled: true}
		blocksJSON, _ := json.Marshal(contentMap)
		return string(blocksJSON), blocks
	}

	if len(blocks) == 0 {
		var errMsg string
		var reason string
		switch {
		case e.ctx.Err() == context.Canceled:
			errMsg, reason = "AI response cancelled", ai.ReasonContextCancel
		case e.ctx.Err() == context.DeadlineExceeded:
			errMsg, reason = "AI response timed out (30 min)", ai.ReasonTimeout
		default:
			errMsg, reason = "AI returned no content", ai.ReasonEmpty
		}
		blocks = append(blocks, model.ContentBlock{Type: blockTypeWarning, Text: errMsg, Reason: reason})
		contentMap := map[string]any{contentKeyBlocks: blocks, contentKeyMetadata: meta}
		if e.ctx.Err() == context.Canceled {
			contentMap[statusCancelled] = true
		}
		blocksJSON, _ := json.Marshal(contentMap)
		return string(blocksJSON), blocks
	}

	contentMap := map[string]any{contentKeyBlocks: blocks, "metadata": meta}
	if e.ctx.Err() == context.Canceled {
		contentMap["cancelled"] = true
	} else if e.ctx.Err() == context.DeadlineExceeded {
		blocks = append(blocks, model.ContentBlock{Type: blockTypeWarning, Text: "AI response timed out (30 min)", Reason: ai.ReasonTimeout})
	}
	contentMap[contentKeyBlocks] = blocks
	blocksJSON, _ := json.Marshal(contentMap)
	return string(blocksJSON), blocks
}

// drainRemainingEvents reads all remaining events from the channel until it is
// closed. It processes tool_use/tool_result events that arrive after the main
// event loop exited (e.g., debouncer flushAll on cancel), persisting them via
// AccumulateBlock + upsertToolCallToDB.
//
// It also processes session_capture and metadata events to persist the external
// session ID, even when the stream was cancelled before the main loop processed
// these events. This prevents resume failures on subsequent prompts.
//
// Draining until close (rather than a one-shot non-blocking scan) guarantees
// the producer's channel sends never block forever on a full buffer, so the
// producer goroutine can always exit and close the channel.
func (e *SessionExecutor) drainRemainingEvents(eventCh <-chan ai.StreamEvent) {
	if eventCh == nil {
		return
	}
	for event := range eventCh {
		switch event.Type {
		case eventTypeToolUse, eventTypeToolResult:
			e.trackToolDuration(&event)
			// e.blocks is only touched by this executor's goroutines; a
			// concurrent FlushStreamingNow read is safe via e.mu.
			e.mu.Lock()
			ai.AccumulateBlock(&e.blocks, event)
			e.mu.Unlock()
			e.upsertToolCallToDB(event)
		case "session_capture":
			if event.Content != "" {
				e.captureExternalSessionID(event.Content)
			}
		case contentKeyMetadata:
			if event.Meta != nil && event.Meta.SessionID != "" {
				e.captureExternalSessionID(event.Meta.SessionID)
			}
		}
	}
}

// Finalize persists the RunResult to the database: builds the content JSON,
// finalizes the streaming message, saves metadata, and drains remaining events.
// Returns the finalized RunResult with DB message ID.
//
// This replaces the old finalizeStreamRun function from handler/chat.go.
// The caller is still responsible for WS terminal events and drain loop logic.
func (e *SessionExecutor) Finalize(result RunResult, eventCh <-chan ai.StreamEvent) RunResult {
	ft := newFinalizeTimer(e.cfg.SessionID)

	// Drain remaining events first (tool calls flushed by debouncer after the
	// main event loop exited on cancel). This updates e.blocks so that
	// buildContentJSON includes the latest tool call data.
	//
	// NOTE: not holding e.mu here — drainRemainingEvents blocks until the
	// producer closes the channel, and it takes e.mu itself around the
	// AccumulateBlock calls. Holding e.mu across the blocking drain would
	// deadlock against a producer goroutine that tries to take e.mu.
	// This phase is the one that can block on a spawned child holding stdout
	// open (see CLIBackend's streamDrainGrace / cmd.WaitDelay), so it is timed
	// separately from the DB work below.
	doneDrain := ft.phase("drain_events")
	e.drainRemainingEvents(eventCh)
	doneDrain()

	// Use e.blocks (may have been updated by drain) instead of result.Blocks
	// snapshot. Snapshot under lock so FlushStreamingNow (shutdown goroutine)
	// can read e.blocks concurrently without a data race. From here on this
	// function owns the local `blocks` slice.
	e.mu.Lock()
	blocks := e.blocks
	e.mu.Unlock()
	responseMetadata := result.Metadata

	// Flush any batched side-writes that have not hit a flush window yet
	// (e.g. a tool event that arrived just before the terminal event). Without
	// this, tool-call rows and context-state patches queued since the last flush
	// would be lost once the streaming row is finalized and the executor exits.
	doneFlush := ft.phase("flush_pending")
	e.flushStreamingMessage()
	doneFlush()

	// Apply the same post-processing as buildResult.
	// buildResult runs postProcessBlocks on a local copy of e.blocks,
	// but Finalize uses e.blocks directly (for drained events) — so the
	// conversion must be applied here too, otherwise DB stores the original
	// unconverted blocks and the frontend renders clawbench-ask-question as plain text
	// instead of an interactive card.
	donePost := ft.phase("post_process")
	blocks = e.postProcessBlocks(blocks)

	// Persist converted AskUserQuestion tool calls to DB.
	// Only done here (in Finalize), not in buildResult, to avoid
	// duplicate records from the two postProcessBlocks calls.
	e.persistAskToolCalls(blocks)

	e.injectSessionMetadata(responseMetadata)
	donePost()

	doneBuild := ft.phase("build_content_json")
	content, blocks := e.buildContentJSON(blocks, result, responseMetadata)
	doneBuild()

	// Split thinking text out of the DB content into chat_thinking (lazy-load).
	// The WS terminal event keeps full blocks (result.Blocks); only the
	// persisted content is slimmed. StreamingMessageID is the streaming row.
	doneThinking := ft.phase("persist_thinking")
	dbContent := persistThinkingToDB(content, e.cfg.StreamingMessageID, e.cfg.SessionID)
	// Finalize is terminal: no rate-limited flush runs after this point. Clear
	// the incremental cursor so a concurrent graceful-shutdown forced flush
	// racing this Finalize cannot append stale chunks for think_ids whose rows
	// persistThinkingToDB just rewrote.
	e.thinkingFlushed = make(map[string]*thinkingFlushState)
	doneThinking()

	// A user-cancelled turn must not stamp completed_at: the user was looking at
	// the session when they cancelled, so the frontend already marked it read
	// before this finalize ran. Stamping the landing time would push the reply
	// past that read and flip the session back to unread. See
	// FinalizeCancelledStreamingMessage.
	var msgID int64
	var err error
	doneFinalize := ft.phase("finalize_row")
	if result.CancelReason == cancelReasonUser {
		msgID, err = FinalizeCancelledStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, dbContent)
	} else {
		msgID, err = FinalizeStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, dbContent)
	}
	doneFinalize()
	if err != nil {
		slog.Error("failed to finalize streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
	}

	// Trigger summarization for all assistant messages in this session that
	// don't yet have a summary. SetSessionRunning(false) uses skipEvent=true
	// (the caller emits its own terminal event), so triggerChatSummarization
	// would never be reached via that path. Call it here instead, right after
	// the message is finalized and streaming=0 is persisted.
	doneSummarize := ft.phase("summarize")
	if msgID > 0 {
		triggerChatSummarization(e.ctx, e.cfg.SessionID)
	}
	doneSummarize()

	// Save metadata to dedicated table for analytical queries
	doneMeta := ft.phase("save_metadata")
	if msgID > 0 && responseMetadata != nil {
		if saveErr := SaveMetadata(msgID, responseMetadata); saveErr != nil {
			slog.Warn("failed to save message metadata", slog.Int64("msg_id", msgID), slog.String("err", saveErr.Error()))
		}
	}
	doneMeta()

	// Update result with finalized blocks and metadata
	result.Blocks = blocks
	result.Metadata = responseMetadata
	result.MsgID = msgID

	// Stream is fully persisted — stop tracking it for graceful shutdown flushes.
	// RunWithChannel may already have unregistered on its own exit; no-op there.
	e.unregisterActiveStream()

	ft.done(result.CancelReason, len(blocks))

	return result
}
