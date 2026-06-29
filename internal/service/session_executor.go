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
)

// ExecutionMode distinguishes between interactive chat and scheduled task execution.
type ExecutionMode int

// Sentinel errors for RunResult.Err
var (
	errBackendCreate = errors.New("failed to create AI backend")
)

const (
	// ModeInteractive is for normal user-driven chat sessions with SSE streaming.
	ModeInteractive ExecutionMode = iota
	// ModeScheduled is for automated task execution without a user present.
	ModeScheduled

	// contentKeyBlocks is the JSON key for content blocks in serialized messages.
	contentKeyBlocks = "blocks"
	// cancelReasonUser is the cancel reason when the user explicitly cancels.
	cancelReasonUser = "user"
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
	// StreamCh is the SSE channel for forwarding events to the frontend.
	// Non-nil for interactive sessions and scheduled tasks with live preview.
	// Nil when no SSE forwarding is needed.
	StreamCh chan<- ai.StreamEvent
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
//   - Handling post-execution logic (SSE terminal events, drain loop, task status updates)
type SessionExecutor struct {
	cfg RunConfig
	ctx context.Context

	// Internal state accumulated during execution
	blocks           []model.ContentBlock
	responseMetadata *ai.Metadata
	rawOutput        string
	eventCount       int
	receivedTerminal bool
	wallStart        int64 // unix millis at execution start
}

// NewSessionExecutor creates a new executor for the given configuration.
// The caller retains ownership of the context — the executor does NOT derive
// a new context with its own cancel function. This prevents double-cancel
// hierarchies where the cancellation infrastructure can't reach the executor's
// inner context.
func NewSessionExecutor(ctx context.Context, cfg RunConfig) *SessionExecutor {
	return &SessionExecutor{
		cfg: cfg,
		ctx: ctx,
	}
}

// handleNonTerminalEvent processes a single non-terminal stream event.
// Returns true if the event loop should return (SSE send failure).
func (e *SessionExecutor) handleNonTerminalEvent(event ai.StreamEvent) bool {
	// raw_output: accumulate but don't forward or count
	if event.Type == "raw_output" {
		if e.rawOutput != "" {
			e.rawOutput += "\n"
		}
		e.rawOutput += event.RawOutput
		return false
	}

	// session_capture: persist external session ID
	if event.Type == "session_capture" {
		if event.Content != "" {
			e.captureExternalSessionID(event.Content)
		}
		return false
	}

	// SSE forwarding (when stream channel is available)
	if e.cfg.StreamCh != nil {
		if e.forwardSSEEvent(event) {
			return true
		}
	}

	// Accumulate block
	ai.AccumulateBlock(&e.blocks, event)

	// Upsert tool call metadata to DB (best-effort)
	e.upsertToolCallToDB(event)

	// resume_split: finalize current message, start new one
	if event.Type == "resume_split" {
		e.handleResumeSplit()
		return false
	}

	// metadata capture
	if event.Type == "metadata" && event.Meta != nil {
		e.responseMetadata = event.Meta
		if event.Meta.SessionID != "" {
			e.captureExternalSessionID(event.Meta.SessionID)
		}
	}

	// Incremental persistence (every 5 events)
	e.eventCount++
	if e.eventCount%5 == 0 {
		e.flushStreamingMessage()
	}

	return false
}

// forwardSSEEvent forwards an event to the SSE stream channel.
// Returns true if the event loop should return (send failure).
func (e *SessionExecutor) forwardSSEEvent(event ai.StreamEvent) bool {
	forwardEvent := event
	if (event.Type == "tool_use" || event.Type == "tool_result") && event.Tool != nil { //nolint:goconst // event type strings
		meta := ai.ExtractToolCallMeta(event)
		forwardEvent.ToolMeta = &meta
	}
	return !ai.SendStreamEvent(e.ctx, e.cfg.StreamCh, forwardEvent)
}

// RunWithChannel executes the event loop against a pre-built event channel.
// This is the core event processing logic shared by both interactive and scheduled modes.
// The caller is responsible for creating the backend and obtaining the event channel.
func (e *SessionExecutor) RunWithChannel(eventCh <-chan ai.StreamEvent) RunResult {
	e.wallStart = time.Now().UnixMilli()
	wallStart := time.Now()

	flushTicker := time.NewTicker(1 * time.Second)
	defer flushTicker.Stop()

	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				// Channel closed without a terminal event — CLI process crash
				return e.buildResult(false, wallStart)
			}
			if event.Type == "done" || event.Type == "error" {
				e.receivedTerminal = true
				// For "error" events, AccumulateBlock handles them.
				// We process the error event but still finalize.
				if event.Type == "error" {
					ai.AccumulateBlock(&e.blocks, event)
					e.upsertToolCallToDB(event)
				}
				return e.buildResult(true, wallStart)
			}

			if e.handleNonTerminalEvent(event) {
				return e.buildResult(e.receivedTerminal, wallStart)
			}

		case <-e.ctx.Done():
			return e.buildResult(e.receivedTerminal, wallStart)

		case <-flushTicker.C:
			if len(e.blocks) > 0 {
				e.flushStreamingMessage()
			}
		}
	}
}

// buildResult constructs the final RunResult from the executor's accumulated state.
func (e *SessionExecutor) buildResult(receivedTerminal bool, wallStart time.Time) RunResult {
	wallMs := int(time.Since(wallStart).Milliseconds())

	// Apply finalize post-processing on blocks
	blocks := e.blocks

	// Ask-question detection (interactive mode only)
	if e.cfg.Mode == ModeInteractive {
		if ai.StringsContainsAnyBlock(blocks, "<ask-question") {
			blocks = ai.ConvertAskQuestionBlocks(blocks)
		}
	}

	// Common block post-processing (idempotent, cheap)
	blocks = ai.RemoveRejectedToolBlocks(blocks)
	blocks = ai.MergeConsecutiveThinkingBlocks(blocks)

	// Persist interactive tool blocks created by ConvertAskQuestionBlocks
	// to chat_tool_calls table (they were created post-SSE and missed
	// the normal upsertToolCallToDB path during the event loop).
	if e.cfg.StreamingMessageID > 0 && e.cfg.SessionID != "" {
		for i := range blocks {
			b := &blocks[i]
			if b.Type == "tool_use" && strings.HasPrefix(b.ID, "ask-") && b.Name == "AskUserQuestion" {
				inputJSON, _ := json.Marshal(b.Input)
				if err := UpsertToolCall(
					e.cfg.StreamingMessageID, e.cfg.SessionID,
					b.ID, b.Name, inputJSON,
					b.Output, b.Status, b.Summary, b.Done,
				); err != nil {
					slog.Warn("upsert converted AskUserQuestion tool call failed",
						slog.String("toolID", b.ID),
						slog.String("err", err.Error()))
				}
			}
		}
	}

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

// upsertToolCallToDB persists tool call data to the chat_tool_calls table.
// Only runs for tool_use and tool_result events when StreamingMessageID is set.
func (e *SessionExecutor) upsertToolCallToDB(event ai.StreamEvent) {
	if event.Tool == nil || e.cfg.StreamingMessageID == 0 || e.cfg.SessionID == "" {
		return
	}
	// Find the matching block in accumulated blocks
	for i := len(e.blocks) - 1; i >= 0; i-- {
		if e.blocks[i].Type == "tool_use" && e.blocks[i].ID == event.Tool.ID {
			block := &e.blocks[i]
			inputJSON, _ := json.Marshal(block.Input)
			if err := UpsertToolCall(
				e.cfg.StreamingMessageID, e.cfg.SessionID,
				block.ID, block.Name, inputJSON,
				block.Output, block.Status, block.Summary, block.Done,
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
func (e *SessionExecutor) flushStreamingMessage() {
	serializedBlocks := e.blocks
	if serializedBlocks == nil {
		serializedBlocks = []model.ContentBlock{}
	}
	contentMap := map[string]any{contentKeyBlocks: serializedBlocks}
	if e.responseMetadata != nil {
		contentMap["metadata"] = e.responseMetadata
	}
	blocksJSON, _ := json.Marshal(contentMap)
	if err := UpdateStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, string(blocksJSON)); err != nil {
		slog.Error("failed to update streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
	}
}

// handleResumeSplit finalizes the current streaming message and creates a new placeholder.
func (e *SessionExecutor) handleResumeSplit() {
	slog.Info("resume_split received, finalizing current message and starting new one",
		slog.String("session", e.cfg.SessionID))

	// Finalize current streaming message
	serializedBlocks := e.blocks
	if serializedBlocks == nil {
		serializedBlocks = []model.ContentBlock{}
	}
	contentMap := map[string]any{contentKeyBlocks: serializedBlocks}
	if e.responseMetadata != nil {
		contentMap["metadata"] = e.responseMetadata
	}
	blocksJSON, _ := json.Marshal(contentMap)
	if msgID, err := FinalizeStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, string(blocksJSON)); err != nil {
		slog.Error("failed to finalize pre-resume message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
	} else if msgID > 0 && e.responseMetadata != nil {
		_ = SaveMetadata(msgID, e.responseMetadata)
	}

	// Save raw output if captured so far
	if e.rawOutput != "" {
		if msgID := GetStreamingMessageID(e.cfg.SessionID); msgID > 0 {
			if err := SaveRawResponse(e.cfg.SessionID, e.cfg.BackendName, msgID, e.rawOutput); err != nil {
				slog.Error("failed to save raw response",
					slog.String("session", e.cfg.SessionID),
					slog.String("err", err.Error()))
			}
		}
		e.rawOutput = ""
	}

	// Reset state for the resumed stream
	e.blocks = nil
	e.responseMetadata = nil
	e.eventCount = 0
	e.wallStart = time.Now().UnixMilli()

	// Create new streaming assistant placeholder
	emptyContent, _ := json.Marshal(map[string]any{contentKeyBlocks: []any{}})
	if newMsgID, err := AddChatMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, "assistant", string(emptyContent), nil, true, ""); err != nil {
		slog.Error("failed to create resume streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
	} else if newMsgID > 0 {
		e.cfg.StreamingMessageID = newMsgID
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
	effectiveTransport := "cli"
	if t := GetSessionTransport(e.cfg.SessionID); t != "" {
		effectiveTransport = t
	} else if agent, ok := model.Agents[e.cfg.AgentID]; ok && agent.SupportsACP() {
		effectiveTransport = "acp-stdio"
	}
	meta.Transport = effectiveTransport

	if sessionModel := GetSessionModel(e.cfg.SessionID); sessionModel != "" {
		meta.Model = sessionModel
	}
}

// buildContentJSON serializes blocks and metadata into the DB content format,
// handling empty-response warnings and cancellation markers.
func (e *SessionExecutor) buildContentJSON(blocks []model.ContentBlock, result RunResult, meta *ai.Metadata) (string, []model.ContentBlock) {
	if len(blocks) == 0 {
		var errMsg string
		var reason string
		switch {
		case result.CancelReason == cancelReasonUser:
			errMsg, reason = "User cancelled", ai.ReasonUserCancel
		case e.ctx.Err() == context.Canceled:
			errMsg, reason = "AI response cancelled", ai.ReasonContextCancel
		case e.ctx.Err() == context.DeadlineExceeded:
			errMsg, reason = "AI response timed out (30 min)", ai.ReasonTimeout
		default:
			errMsg, reason = "AI returned no content", ai.ReasonEmpty
		}
		blocks = append(blocks, model.ContentBlock{Type: "warning", Text: errMsg, Reason: reason})
		contentMap := map[string]any{contentKeyBlocks: blocks, "metadata": meta}
		if result.CancelReason == cancelReasonUser || e.ctx.Err() == context.Canceled {
			contentMap["cancelled"] = true
		}
		blocksJSON, _ := json.Marshal(contentMap)
		return string(blocksJSON), blocks
	}

	contentMap := map[string]any{contentKeyBlocks: blocks, "metadata": meta}
	if result.CancelReason == cancelReasonUser {
		contentMap["cancelled"] = true
	} else if e.ctx.Err() == context.Canceled {
		contentMap["cancelled"] = true
	} else if e.ctx.Err() == context.DeadlineExceeded {
		blocks = append(blocks, model.ContentBlock{Type: "warning", Text: "AI response timed out (30 min)", Reason: ai.ReasonTimeout})
	}
	contentMap[contentKeyBlocks] = blocks
	blocksJSON, _ := json.Marshal(contentMap)
	return string(blocksJSON), blocks
}

// drainRawOutput reads remaining raw_output events from the channel without blocking.
func drainRawOutput(eventCh <-chan ai.StreamEvent, rawOutput string) string {
	if eventCh == nil {
		return rawOutput
	}
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return rawOutput
			}
			if event.Type == "raw_output" {
				if rawOutput != "" {
					rawOutput += "\n"
				}
				rawOutput += event.RawOutput
			}
		default:
			return rawOutput
		}
	}
}

// Finalize persists the RunResult to the database: builds the content JSON,
// finalizes the streaming message, saves metadata, drains remaining events,
// and saves raw output. Returns the finalized RunResult with DB message ID.
//
// This replaces the old finalizeStreamRun function from handler/chat.go.
// The caller is still responsible for SSE terminal events and drain loop logic.
func (e *SessionExecutor) Finalize(result RunResult, eventCh <-chan ai.StreamEvent) RunResult {
	blocks := result.Blocks
	responseMetadata := result.Metadata

	e.injectSessionMetadata(responseMetadata)

	content, blocks := e.buildContentJSON(blocks, result, responseMetadata)

	msgID, err := FinalizeStreamingMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, content)
	if err != nil {
		slog.Error("failed to finalize streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
	}

	// Save metadata to dedicated table for analytical queries
	if msgID > 0 && responseMetadata != nil {
		if saveErr := SaveMetadata(msgID, responseMetadata); saveErr != nil {
			slog.Warn("failed to save message metadata", slog.Int64("msg_id", msgID), slog.String("err", saveErr.Error()))
		}
	}

	// Drain any remaining events from channel (collect raw_output)
	rawOutput := drainRawOutput(eventCh, result.RawOutput)

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
