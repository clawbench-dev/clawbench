package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

	// JSON content map keys
	contentKeyBlocks = "blocks"
	// Cancel reason values
	cancelReasonUser = "user"
)

// RunConfig configures a single SessionExecutor execution.
type RunConfig struct {
	Mode ExecutionMode

	// --- Common fields ---
	ProjectPath string
	BackendName string
	SessionID   string
	AgentID     string
	ChatRequest ai.ChatRequest
	FileDir     string

	// --- ModeInteractive only ---
	// StreamCh is the SSE channel for forwarding events to the frontend.
	// Nil for scheduled mode.
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
			if done, result := e.processEvent(event, wallStart); done {
				return result
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

// processEvent handles a single stream event. Returns (true, RunResult) if the
// event loop should terminate (terminal event or SSE send failure), or (false, zero)
// to continue processing.
func (e *SessionExecutor) processEvent(event ai.StreamEvent, wallStart time.Time) (bool, RunResult) {
	// Terminal events: done or error
	if event.Type == "done" || event.Type == "error" {
		e.receivedTerminal = true
		if event.Type == "error" {
			ai.AccumulateBlock(&e.blocks, event)
		}
		return true, e.buildResult(true, wallStart)
	}

	// Non-forwardable events: raw_output and session_capture
	if done := e.handleNonForwardableEvent(event); done {
		return false, RunResult{}
	}

	// SSE forwarding (interactive mode only)
	if e.cfg.Mode == ModeInteractive && e.cfg.StreamCh != nil {
		if !ai.SendStreamEvent(e.ctx, e.cfg.StreamCh, event) {
			return true, e.buildResult(e.receivedTerminal, wallStart)
		}
	}

	// Accumulate block
	ai.AccumulateBlock(&e.blocks, event)

	// resume_split: finalize current message, start new one
	if event.Type == "resume_split" {
		e.handleResumeSplit()
		return false, RunResult{}
	}

	// metadata capture
	e.captureMetadata(event)

	// Incremental persistence (every 5 events)
	e.eventCount++
	if e.eventCount%5 == 0 {
		e.flushStreamingMessage()
	}

	return false, RunResult{}
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
	if existingExtID == "" || existingExtID == e.cfg.SessionID {
		if err := UpdateExternalSessionID(e.cfg.SessionID, externalID); err != nil {
			slog.Error("failed to save external session ID",
				slog.String("session", e.cfg.SessionID),
				slog.String("external_id", externalID),
				slog.String("err", err.Error()))
		}
	}
}

// handleNonForwardableEvent processes events that should not be forwarded to SSE
// (raw_output and session_capture). Returns true if the event was handled.
func (e *SessionExecutor) handleNonForwardableEvent(event ai.StreamEvent) bool {
	if event.Type == "raw_output" {
		if e.rawOutput != "" {
			e.rawOutput += "\n"
		}
		e.rawOutput += event.RawOutput
		return true
	}
	if event.Type == "session_capture" {
		if event.Content != "" {
			e.captureExternalSessionID(event.Content)
		}
		return true
	}
	return false
}

// captureMetadata stores metadata from a metadata event and captures the external session ID.
func (e *SessionExecutor) captureMetadata(event ai.StreamEvent) {
	if event.Type != "metadata" || event.Meta == nil {
		return
	}
	e.responseMetadata = event.Meta
	if event.Meta.SessionID != "" {
		e.captureExternalSessionID(event.Meta.SessionID)
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
	if _, err := AddChatMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, "assistant", string(emptyContent), nil, true, ""); err != nil {
		slog.Error("failed to create resume streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
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

	e.injectResponseMetadata(responseMetadata)

	blocks, responseMetadata, content := e.finalizeContent(blocks, responseMetadata, result.CancelReason)

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
	rawOutput := e.drainRawOutput(eventCh, result.RawOutput)

	// Save raw AI backend output for debugging/analysis
	if rawOutput != "" {
		if streamingMsgID := GetStreamingMessageID(e.cfg.SessionID); streamingMsgID > 0 {
			if err := SaveRawResponse(e.cfg.SessionID, e.cfg.BackendName, streamingMsgID, rawOutput); err != nil {
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

// injectResponseMetadata injects ACP mode, thinking effort, transport, and model into the response metadata.
func (e *SessionExecutor) injectResponseMetadata(md *ai.Metadata) {
	if s := ai.GetACPConnManager().GetCachedStateByClawbenchSID(e.cfg.SessionID); s.Mode != nil || s.Effort != nil {
		if s.Mode != nil && s.Mode.CurrentModeID != "" {
			md.Mode = s.Mode.CurrentModeID
		}
		if s.Effort != nil && s.Effort.CurrentID != "" {
			md.ThinkingEffort = s.Effort.CurrentID
		}
	}
	effectiveTransport := "cli"
	if t := GetSessionTransport(e.cfg.SessionID); t != "" {
		effectiveTransport = t
	} else if agent, ok := model.Agents[e.cfg.AgentID]; ok && agent.SupportsACP() {
		effectiveTransport = "acp-stdio"
	}
	md.Transport = effectiveTransport

	if sessionModel := GetSessionModel(e.cfg.SessionID); sessionModel != "" {
		md.Model = sessionModel
	}
}

// finalizeContent builds the content JSON for DB storage, handling empty and non-empty block cases.
// Returns the (possibly modified) blocks, metadata, and serialized content string.
func (e *SessionExecutor) finalizeContent(blocks []model.ContentBlock, responseMetadata *ai.Metadata, cancelReason string) ([]model.ContentBlock, *ai.Metadata, string) {
	var content string
	if len(blocks) == 0 {
		blocks, content = e.buildEmptyContentJSON(blocks, responseMetadata, cancelReason)
	} else {
		content = e.buildNonEmptyContentJSON(blocks, responseMetadata, cancelReason)
	}
	return blocks, responseMetadata, content
}

// buildEmptyContentJSON builds the content JSON for an empty response (warning block added).
func (e *SessionExecutor) buildEmptyContentJSON(blocks []model.ContentBlock, responseMetadata *ai.Metadata, cancelReason string) ([]model.ContentBlock, string) {
	var errMsg string
	var reason string
	switch {
	case cancelReason == cancelReasonUser:
		errMsg, reason = "User cancelled", ai.ReasonUserCancel
	case e.ctx.Err() == context.Canceled:
		errMsg, reason = "AI response cancelled", ai.ReasonContextCancel
	case e.ctx.Err() == context.DeadlineExceeded:
		errMsg, reason = "AI response timed out (30 min)", ai.ReasonTimeout
	default:
		errMsg, reason = "AI returned no content", ai.ReasonEmpty
	}
	blocks = append(blocks, model.ContentBlock{Type: "warning", Text: errMsg, Reason: reason})
	contentMap := map[string]any{contentKeyBlocks: blocks, "metadata": responseMetadata}
	if cancelReason == cancelReasonUser || e.ctx.Err() == context.Canceled {
		contentMap["cancelled"] = true
	}
	blocksJSON, _ := json.Marshal(contentMap)
	return blocks, string(blocksJSON)
}

// buildNonEmptyContentJSON builds the content JSON for a non-empty response.
func (e *SessionExecutor) buildNonEmptyContentJSON(blocks []model.ContentBlock, responseMetadata *ai.Metadata, cancelReason string) string {
	contentMap := map[string]any{contentKeyBlocks: blocks, "metadata": responseMetadata}
	if cancelReason == cancelReasonUser {
		contentMap["cancelled"] = true
	} else if e.ctx.Err() == context.Canceled {
		contentMap["cancelled"] = true
	} else if e.ctx.Err() == context.DeadlineExceeded {
		blocks = append(blocks, model.ContentBlock{Type: "warning", Text: "AI response timed out (30 min)", Reason: ai.ReasonTimeout})
	}
	contentMap[contentKeyBlocks] = blocks
	blocksJSON, _ := json.Marshal(contentMap)
	return string(blocksJSON)
}

// drainRawOutput drains remaining events from the channel, collecting raw_output events.
func (e *SessionExecutor) drainRawOutput(eventCh <-chan ai.StreamEvent, rawOutput string) string {
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
