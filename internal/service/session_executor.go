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
	errStreamStart   = errors.New("failed to start AI stream")
)

const (
	// ModeInteractive is for normal user-driven chat sessions with SSE streaming.
	ModeInteractive ExecutionMode = iota
	// ModeScheduled is for automated task execution without a user present.
	ModeScheduled
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
			if event.Type == "done" || event.Type == "error" {
				e.receivedTerminal = true
				// For "error" events, AccumulateBlock handles them.
				// We process the error event but still finalize.
				if event.Type == "error" {
					ai.AccumulateBlock(&e.blocks, event)
				}
				return e.buildResult(true, wallStart)
			}

			// raw_output: accumulate but don't forward or count
			if event.Type == "raw_output" {
				if e.rawOutput != "" {
					e.rawOutput += "\n"
				}
				e.rawOutput += event.RawOutput
				continue
			}

			// session_capture: persist external session ID
			if event.Type == "session_capture" {
				if event.Content != "" {
					e.captureExternalSessionID(event.Content)
				}
				continue
			}

			// SSE forwarding (interactive mode only)
			if e.cfg.Mode == ModeInteractive && e.cfg.StreamCh != nil {
				if !ai.SendStreamEvent(e.ctx, e.cfg.StreamCh, event) {
					// Context cancelled or stream channel closed
					return e.buildResult(e.receivedTerminal, wallStart)
				}
			}

			// Accumulate block
			ai.AccumulateBlock(&e.blocks, event)

			// resume_split: finalize current message, start new one
			if event.Type == "resume_split" {
				e.handleResumeSplit()
				continue
			}

			// metadata capture
			if event.Type == "metadata" && event.Meta != nil {
				e.responseMetadata = event.Meta
				// Capture external session ID from metadata
				if event.Meta.SessionID != "" {
					e.captureExternalSessionID(event.Meta.SessionID)
				}
			}

			// Incremental persistence (every 5 events)
			e.eventCount++
			if e.eventCount%5 == 0 {
				e.flushStreamingMessage()
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

// flushStreamingMessage persists the current accumulated blocks to the database.
func (e *SessionExecutor) flushStreamingMessage() {
	serializedBlocks := e.blocks
	if serializedBlocks == nil {
		serializedBlocks = []model.ContentBlock{}
	}
	contentMap := map[string]any{"blocks": serializedBlocks}
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
	contentMap := map[string]any{"blocks": serializedBlocks}
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
	emptyContent, _ := json.Marshal(map[string]any{"blocks": []any{}})
	if _, err := AddChatMessage(e.cfg.ProjectPath, e.cfg.BackendName, e.cfg.SessionID, "assistant", string(emptyContent), nil, true, ""); err != nil {
		slog.Error("failed to create resume streaming message",
			slog.String("session", e.cfg.SessionID),
			slog.String("err", err.Error()))
	}
}
