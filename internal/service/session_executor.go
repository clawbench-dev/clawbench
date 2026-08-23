package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// ExecutionMode distinguishes between interactive chat and scheduled task execution.
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
	// blockTypeWarning is the content block type for warning messages.
	blockTypeWarning = "warning"
	// eventTypeContentReset clears accumulated blocks from a failed Prompt before retry.
	eventTypeContentReset = "content_reset"

	// transportACPStdio is the ACP stdio transport type.
	transportACPStdio = "acp-stdio"
	// transportCLI is the CLI transport type.
	transportCLI = "cli"
	// eventTypeError is the stream event type for errors.
	eventTypeError = "error"
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
	flushInterval = 500 * time.Millisecond
)

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
	// RawOutput is the collected raw AI backend output for debugging.
	RawOutput string

	// WallMs is the wall-clock duration of the execution in milliseconds.
	WallMs int
	// FirstContentMs is the time to first content event for performance diagnosis.
	FirstContentMs int
	// MsgID is the database message ID after finalization (0 if not yet finalized).
	MsgID int64
}

// SessionExecutor handles the full lifecycle of a single AI session execution.
// It unifies the event loop logic for both interactive chat and scheduled tasks,
// with mode-specific behavior controlled by RunConfig.
//
// The caller is responsible for:
//   - Creating and managing the context (including cancel functions)
//   - Setting session running state (TrySetSessionRunning / SetSessionRunning)
//   - Handling post-execution logic (WS terminal events, drain loop, task status updates)
type SessionExecutor struct {
	cfg RunConfig
	ctx context.Context

	// Internal state accumulated during execution
	blocks           []model.ContentBlock
	responseMetadata *ai.Metadata
	rawOutput        string
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
}

// NewSessionExecutor creates a new executor for the given configuration.
// The caller retains ownership of the context — the executor does NOT derive
// a new context with its own cancel function. This prevents double-cancel
// hierarchies where the cancellation infrastructure can't reach the executor's
// inner context.
func NewSessionExecutor(ctx context.Context, cfg RunConfig) *SessionExecutor {
	return &SessionExecutor{
		cfg:        cfg,
		ctx:        ctx,
		toolStarts: make(map[string]time.Time),
	}
}

// handleNonTerminalEvent processes a single non-terminal stream event.
func (e *SessionExecutor) handleNonTerminalEvent(event ai.StreamEvent) {
	// content_reset: clear accumulated blocks from a failed Prompt before retry.
	// Sent by ACPBackend.ExecuteStream when the first Prompt fails due to peer
	// disconnect and the retry Prompt will re-emit the full response. Without
	// this, AccumulateBlock would append the retry's content onto the stale
	// partial content from the first attempt, producing duplicated text.
	if event.Type == eventTypeContentReset {
		slog.Warn("session executor: content_reset, clearing accumulated blocks",
			slog.String("session", e.cfg.SessionID),
			slog.Int("blocks_before", len(e.blocks)))
		e.blocks = nil
		e.rawOutput = ""
		e.responseMetadata = nil
		e.lastFlush = time.Time{}
		e.toolStarts = make(map[string]time.Time)
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
		}
		// Forward to WS clients so the frontend clears its rendered partial content.
		e.forwardEvent(event)
		return
	}

	// raw_output: accumulate but don't forward or count
	if event.Type == "raw_output" {
		if e.rawOutput != "" {
			e.rawOutput += "\n"
		}
		e.rawOutput += event.RawOutput
		return
	}

	// session_capture: persist external session ID
	if event.Type == "session_capture" {
		if event.Content != "" {
			e.captureExternalSessionID(event.Content)
		}
		return
	}

	// Inject per-tool duration into completion events before forwarding,
	// so WS clients and AccumulateBlock both see it.
	if event.Type == eventTypeToolUse || event.Type == eventTypeToolResult {
		e.trackToolDuration(&event)
	}

	// Forward event to WS clients via StreamHub
	e.forwardEvent(event)

	// Accumulate block
	ai.AccumulateBlock(&e.blocks, event)

	// Upsert tool call metadata to DB (best-effort)
	e.upsertToolCallToDB(event)

	// metadata capture
	if event.Type == contentKeyMetadata && event.Meta != nil {
		e.responseMetadata = event.Meta
		if event.Meta.SessionID != "" {
			e.captureExternalSessionID(event.Meta.SessionID)
		}
	}

	// Incremental persistence (rate-limited). Persisting every N events is too
	// aggressive for ACP backends that emit bursts of incremental deltas — the
	// full-block JSON marshal + SQLite write stalls the consumer and the stream
	// channel fills, dropping events. Persist at most once per flushInterval.
	if time.Since(e.lastFlush) >= flushInterval {
		e.flushStreamingMessage()
		e.lastFlush = time.Now()
	}
}

// forwardEvent forwards an event to WS clients via StreamHub
// and persists context state (mode, thinking effort, usage) to DB.
func (e *SessionExecutor) forwardEvent(event ai.StreamEvent) {
	forwardEvent := event
	if (event.Type == eventTypeToolUse || event.Type == eventTypeToolResult) && event.Tool != nil {
		meta := ai.ExtractToolCallMeta(event)
		forwardEvent.ToolMeta = &meta
	}

	ws.EmitToSession(e.cfg.SessionID, forwardEvent)

	// Persist context state to DB so it survives server restarts.
	// Called for all event types; PersistContextStateFromEvent only acts on
	// mode_update/usage_update/thinking_effort_update and ignores others.
	PersistContextStateFromEvent(e.cfg.SessionID, event)
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

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed without a terminal event — CLI process crash
				return e.buildResult(false, wallStart)
			}
			if event.Type == "done" || event.Type == eventTypeError {
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
			}
		}
	}
}

// postProcessBlocks applies finalize post-processing on blocks:
// ask-question conversion, rejected-tool removal, thinking-block merging.
// Shared by buildResult and Finalize to prevent divergence.
// NOTE: persistAskToolCalls must be called separately after Finalize
// uses postProcessBlocks, to avoid double-persisting from buildResult.
func (e *SessionExecutor) postProcessBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	// Ask-question detection (interactive mode only)
	if e.cfg.Mode == ModeInteractive {
		if ai.StringsContainsAnyBlock(blocks, "<ask-question") {
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
	wallMs := int(time.Since(wallStart).Milliseconds())

	// Apply finalize post-processing on blocks
	blocks := e.postProcessBlocks(e.blocks)

	// Inject WallMs into metadata
	if e.responseMetadata == nil {
		e.responseMetadata = &ai.Metadata{}
	}
	e.responseMetadata.WallMs = wallMs

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
		RawOutput:        e.rawOutput,
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
func (e *SessionExecutor) upsertToolCallToDB(event ai.StreamEvent) {
	if event.Tool == nil || e.cfg.StreamingMessageID == 0 || e.cfg.SessionID == "" {
		return
	}
	// Find the matching block in accumulated blocks
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

// flushStreamingMessage persists the current accumulated blocks to the database.
// Thinking blocks are excluded: they are process data rendered live over WS and
// only persisted once at finalization via persistThinkingToDB. Excluding them
// keeps the per-flush JSON small even when the agent streams tens of KB of
// thinking, which is the dominant cost that previously stalled the consumer.
func (e *SessionExecutor) flushStreamingMessage() {
	serializedBlocks := make([]model.ContentBlock, 0, len(e.blocks))
	for _, b := range e.blocks {
		if b.Type == "thinking" {
			continue
		}
		serializedBlocks = append(serializedBlocks, b)
	}
	contentMap := map[string]any{contentKeyBlocks: serializedBlocks}
	if e.responseMetadata != nil {
		contentMap[contentKeyMetadata] = e.responseMetadata
	}
	blocksJSON, _ := json.Marshal(contentMap)
	if err := UpdateStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, string(blocksJSON)); err != nil {
		slog.Error("failed to update streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
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
}

// buildContentJSON serializes blocks and metadata into the DB content format,
// handling empty-response warnings and cancellation markers.
func (e *SessionExecutor) buildContentJSON(blocks []model.ContentBlock, result RunResult, meta *ai.Metadata) (string, []model.ContentBlock) {
	// User-initiated cancel: just mark cancelled, never add a warning block.
	// The frontend renders a clean "cancelled" badge — no alarming warning needed.
	if result.CancelReason == cancelReasonUser {
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
// closed. In addition to raw_output (for debugging), it also processes
// tool_use/tool_result events that arrive after the main event loop exited
// (e.g., debouncer flushAll on cancel), persisting them via AccumulateBlock +
// upsertToolCallToDB.
//
// It also processes session_capture and metadata events to persist the external
// session ID, even when the stream was cancelled before the main loop processed
// these events. This prevents resume failures on subsequent prompts.
//
// Draining until close (rather than a one-shot non-blocking scan) guarantees
// the producer's channel sends never block forever on a full buffer, so the
// producer goroutine can always exit and close the channel.
func (e *SessionExecutor) drainRemainingEvents(eventCh <-chan ai.StreamEvent, rawOutput string) string {
	if eventCh == nil {
		return rawOutput
	}
	for event := range eventCh {
		switch event.Type {
		case "raw_output":
			if rawOutput != "" {
				rawOutput += "\n"
			}
			rawOutput += event.RawOutput
		case eventTypeToolUse, eventTypeToolResult:
			e.trackToolDuration(&event)
			ai.AccumulateBlock(&e.blocks, event)
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
	return rawOutput
}

// Finalize persists the RunResult to the database: builds the content JSON,
// finalizes the streaming message, saves metadata, drains remaining events,
// and saves raw output. Returns the finalized RunResult with DB message ID.
//
// This replaces the old finalizeStreamRun function from handler/chat.go.
// The caller is still responsible for WS terminal events and drain loop logic.
func (e *SessionExecutor) Finalize(result RunResult, eventCh <-chan ai.StreamEvent) RunResult {
	// Drain remaining events first (raw_output + tool calls flushed by debouncer
	// after the main event loop exited on cancel). This updates e.blocks so that
	// buildContentJSON includes the latest tool call data.
	rawOutput := e.drainRemainingEvents(eventCh, result.RawOutput)

	// Use e.blocks (may have been updated by drain) instead of result.Blocks snapshot
	blocks := e.blocks
	responseMetadata := result.Metadata

	// Apply the same post-processing as buildResult.
	// buildResult runs postProcessBlocks on a local copy of e.blocks,
	// but Finalize uses e.blocks directly (for drained events) — so the
	// conversion must be applied here too, otherwise DB stores the original
	// unconverted blocks and the frontend renders ask-question as plain text
	// instead of an interactive card.
	blocks = e.postProcessBlocks(blocks)

	// Persist converted AskUserQuestion tool calls to DB.
	// Only done here (in Finalize), not in buildResult, to avoid
	// duplicate records from the two postProcessBlocks calls.
	e.persistAskToolCalls(blocks)

	e.injectSessionMetadata(responseMetadata)

	content, blocks := e.buildContentJSON(blocks, result, responseMetadata)

	// Split thinking text out of the DB content into chat_thinking (lazy-load).
	// The WS terminal event keeps full blocks (result.Blocks); only the
	// persisted content is slimmed. StreamingMessageID is the streaming row.
	dbContent := persistThinkingToDB(content, e.cfg.StreamingMessageID, e.cfg.SessionID)

	msgID, err := FinalizeStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, dbContent)
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
	if msgID > 0 {
		triggerChatSummarization(e.cfg.SessionID)
	}

	// Save metadata to dedicated table for analytical queries
	if msgID > 0 && responseMetadata != nil {
		if saveErr := SaveMetadata(msgID, responseMetadata); saveErr != nil {
			slog.Warn("failed to save message metadata", slog.Int64("msg_id", msgID), slog.String("err", saveErr.Error()))
		}
	}

	// Save raw AI backend output for debugging/analysis
	if rawOutput != "" {
		if streamMsgID := GetStreamingMessageID(e.cfg.SessionID); streamMsgID > 0 {
			if err := SaveRawResponse(e.cfg.SessionID, e.cfg.BackendName, streamMsgID, rawOutput); err != nil {
				slog.Error("failed to save raw response",
					slog.String("session", e.cfg.SessionID),
					slog.String("err", err.Error()))
			}
		}
	}

	// Update result with finalized blocks and metadata
	result.Blocks = blocks
	result.Metadata = responseMetadata
	result.RawOutput = rawOutput
	result.MsgID = msgID

	return result
}
