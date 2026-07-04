//nolint:goconst // role/status strings are domain constants
package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"
)

// Active session tracking - keyed by sessionID
var (
	activeSessions = make(map[string]bool)
	activeMu       sync.Mutex
)

// Session stream channel management for SSE streaming.
// Each session has one channel (producer → single SSE consumer).
// When multiple clients connect to the same session's SSE stream, only the first
// gets the live channel; subsequent clients receive an SSE error event and fall
// back to HTTP polling (which reads from DB and is multi-reader safe).
var sessionStreams sync.Map // map[string]chan ai.StreamEvent

// sessionSSEClaim tracks which sessions have an active SSE connection.
// Prevents multiple goroutines from competing on the same channel (Go channels
// deliver each message to exactly one reader, causing split content).
var sessionSSEClaim sync.Map // map[string]bool

// Session cancel functions for aborting AI responses
var (
	sessionCancels       sync.Map // map[string]context.CancelFunc
	sessionCancelReasons sync.Map // map[string]string — "user", "disconnect"
)

// responsePreviewMaxRunes is an alias for model.ResponsePreviewMaxRunes for local use.
const responsePreviewMaxRunes = model.ResponsePreviewMaxRunes

// EmitSessionEvent broadcasts a session_update event to connected clients.
// toolName is optional and only used for "permission_pending" status.
func EmitSessionEvent(sessionID, status string, hasNewMessages bool, toolName ...string) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}

	data := &ws.SessionUpdateData{
		SessionID:      sessionID,
		Status:         status,
		HasNewMessages: hasNewMessages,
	}

	// On completion, include session title for push notification
	if status == "completed" || status == "cancelled" {
		if title, err := GetSessionTitle(sessionID); err == nil && title != "" {
			data.SessionTitle = title
		}
		// Also include response preview for other consumers
		if status == "completed" {
			data.ResponsePreview = getSessionResponsePreview(sessionID)
		}
	}

	// Include toolName for permission_pending events
	if status == "permission_pending" && len(toolName) > 0 {
		data.ToolName = toolName[0]
	}

	data.ProjectPath = GetSessionProjectPath(sessionID)

	// Generate one event ID, use for both store and broadcast (write-ahead)
	msg := ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "session_update",
		Data:  data,
	}
	// Write-ahead: persist before broadcast so event log has no gaps
	StoreNotifiableEvent(msg)
	mgr.BroadcastEvent(msg)
}

// getSessionResponsePreview returns a preview of the AI's final reply text.
// It extracts text from after the last tool_use block in the last assistant
// message, since the final text block(s) contain the AI's actual answer
// rather than intermediate reasoning or tool-call commentary.
func getSessionResponsePreview(sessionID string) string {
	messages, err := GetMessagesBySessionID(sessionID)
	if err != nil {
		slog.Debug("session_event: failed to get messages for preview", "session_id", sessionID, "error", err)
		return ""
	}
	// Walk backwards to find the last assistant message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "assistant" {
			continue
		}
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(messages[i].Content), &content); err != nil {
			continue
		}
		if preview := extractPreviewFromBlocks(content.Blocks); preview != "" {
			return preview
		}
	}
	return ""
}

// extractPreviewFromBlocks returns a preview string from content blocks.
// It first tries text after the last tool_use block, then falls back to the
// longest text block (handles AskUserQuestion-style terminal tool_use).
func extractPreviewFromBlocks(blocks []model.ContentBlock) string {
	// Find the last tool_use block index to skip intermediate text
	lastToolIdx := -1
	for j, b := range blocks {
		if b.Type == "tool_use" {
			lastToolIdx = j
		}
	}
	// Extract text from blocks after the last tool_use
	for j := lastToolIdx + 1; j < len(blocks); j++ {
		b := blocks[j]
		if b.Type == "text" && b.Text != "" {
			return truncatePreview(b.Text)
		}
	}
	// Fallback: longest text block
	var bestText string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" && utf8.RuneCountInString(b.Text) > utf8.RuneCountInString(bestText) {
			bestText = b.Text
		}
	}
	return truncatePreview(bestText)
}

// truncatePreview truncates text to responsePreviewMaxRunes with ellipsis if needed.
func truncatePreview(text string) string {
	if text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) > responsePreviewMaxRunes {
		return string([]rune(text)[:responsePreviewMaxRunes]) + "…"
	}
	return text
}

// IsSessionRunning checks if a session is currently running.
func IsSessionRunning(sessionID string) bool {
	activeMu.Lock()
	defer activeMu.Unlock()
	return activeSessions[sessionID]
}

// GetRunningSessionIDs returns all currently running session IDs in a single call.
// This avoids N separate mutex acquisitions when checking running state for multiple sessions.
func GetRunningSessionIDs() []string {
	activeMu.Lock()
	defer activeMu.Unlock()
	ids := make([]string, 0, len(activeSessions))
	for id := range activeSessions {
		ids = append(ids, id)
	}
	return ids
}

// SetSessionRunning sets the running state for a session.
// If skipEvent is true, the session_update event is suppressed (used by CancelSession
// which emits its own "cancelled" event and should not also emit "completed").
func SetSessionRunning(sessionID string, running bool, skipEvent ...bool) {
	activeMu.Lock()
	if running {
		activeSessions[sessionID] = true
	} else {
		delete(activeSessions, sessionID)
	}
	activeMu.Unlock()

	if !running {
		// Safety net: finalize any orphaned streaming=1 messages for this session.
		// This handles the case where FinalizeStreamingMessage failed due to SQLITE_BUSY
		// during the stream, leaving the message stuck at streaming=1 forever.
		go finalizeOrphanedStreamingMessages(sessionID)
	}

	// Emit event unless caller explicitly skips (e.g. CancelSession sends its own event)
	if len(skipEvent) == 0 || !skipEvent[0] {
		if !running {
			EmitSessionEvent(sessionID, "completed", true)

			// Trigger async summarization for chat messages on normal completion
			// (cancel/disconnect uses skipEvent=true, so this only runs on "completed")
			triggerChatSummarization(sessionID)
		} else {
			EmitSessionEvent(sessionID, "running", false)
		}
	}
}

// finalizeOrphanedStreamingMessages checks for and finalizes any streaming=1
// assistant messages left behind for a session (e.g. due to SQLITE_BUSY failures).
func finalizeOrphanedStreamingMessages(sessionID string) {
	if db == nil {
		return
	}
	// Find streaming=1 messages for this session
	rows, err := dbRead.Query( //nolint:noctx // background goroutine, no request context available
		"SELECT id, content FROM chat_history WHERE session_id = ? AND role = 'assistant' AND streaming = 1",
		sessionID,
	)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	type orphanMsg struct {
		id      int64
		content string
	}
	var orphans []orphanMsg
	for rows.Next() {
		var m orphanMsg
		if err := rows.Scan(&m.id, &m.content); err != nil {
			continue
		}
		orphans = append(orphans, m)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("failed to iterate orphaned streaming messages", "session_id", sessionID, "error", err)
	}

	for _, m := range orphans {
		var contentMap map[string]any
		if err := json.Unmarshal([]byte(m.content), &contentMap); err != nil {
			contentMap = map[string]any{
				"blocks":    []any{map[string]any{"type": "text", "text": m.content}},
				"cancelled": true,
			}
		} else {
			if _, ok := contentMap["cancelled"]; !ok {
				contentMap["cancelled"] = true
				blocks, _ := contentMap["blocks"].([]any)
				blocks = append(blocks, map[string]any{
					"type":   "warning",
					"text":   "Finalization failed, AI response may be incomplete",
					"reason": "finalize_busy",
				})
				contentMap["blocks"] = blocks
			}
		}
		updatedContent, _ := json.Marshal(contentMap)
		if _, err := WriteExec("UPDATE chat_history SET content = ?, streaming = 0 WHERE id = ?", string(updatedContent), m.id); err != nil {
			slog.Error("failed to finalize orphaned streaming message on session stop",
				slog.Int64("id", m.id),
				slog.String("session", sessionID),
				slog.String("err", err.Error()))
		} else {
			slog.Info("finalized orphaned streaming message on session stop",
				slog.Int64("id", m.id),
				slog.String("session", sessionID))
		}
	}
}

// TrySetSessionRunning atomically checks and sets running state.
// Returns true if session was successfully marked as running (was not running before).
// Returns false if session was already running.
// Emits a "running" session_update event on success.
func TrySetSessionRunning(sessionID string) bool {
	activeMu.Lock()

	if activeSessions[sessionID] {
		activeMu.Unlock()
		return false
	}
	activeSessions[sessionID] = true
	activeMu.Unlock()

	// Emit event so frontends know the session started running
	EmitSessionEvent(sessionID, "running", false)

	return true
}

// RegisterSessionCancel stores the cancel function for a session
func RegisterSessionCancel(sessionID string, cancel context.CancelFunc) {
	sessionCancels.Store(sessionID, cancel)
}

// UnregisterSessionCancel removes the cancel function for a session
func UnregisterSessionCancel(sessionID string) {
	sessionCancels.Delete(sessionID)
}

// SetCancelReason records the cancellation reason for a session without cancelling it.
// Used by the SSE handler when a client disconnects — the AI session continues running
// but the reason is stored for the session finalizer to read later.
func SetCancelReason(sessionID string, reason string) {
	sessionCancelReasons.Store(sessionID, reason)
}

// GetAndClearCancelReason returns the reason for the most recent cancellation of a session.
// Returns "user" for user-initiated cancel, "disconnect" for SSE client disconnect.
// Returns "" if no reason was recorded (e.g. timeout or no cancel).
func GetAndClearCancelReason(sessionID string) string {
	val, ok := sessionCancelReasons.LoadAndDelete(sessionID)
	if !ok {
		return ""
	}
	// Safe type assertion to prevent panic if value is not a string (ISS-126)
	reason, ok := val.(string)
	if !ok {
		return ""
	}
	return reason
}

// CancelSession cancels an ongoing AI stream for a session.
// Returns true if session was found and cancelled, or if session is already not running (idempotent).
func CancelSession(sessionID string) bool {
	// Load and delete the cancel function
	val, ok := sessionCancels.LoadAndDelete(sessionID)
	if !ok {
		// If session is not in running state, consider it already cancelled (idempotent)
		if !IsSessionRunning(sessionID) {
			return true
		}
		// Session is marked as running but has no cancel function — this is a stuck state.
		// Can happen if the goroutine hasn't registered its cancel yet (race window),
		// or if the cancel was already consumed by a previous cancel call.
		// Force-clear the running state to unstick the session.
		slog.Warn("CancelSession: session running but no cancel func, force-clearing",
			slog.String("session_id", sessionID))
		ClearQueue(sessionID)
		SetSessionRunning(sessionID, false, true)
		return true
	}
	cancel, ok := val.(context.CancelFunc)
	if !ok {
		return false
	}

	// Cancel the Go context first so the agent process starts shutting down,
	// freeing its stdin pipe. Then send ACP Cancel (with 3s timeout) so the
	// agent can stop its turn gracefully on next stdin read.
	sessionCancelReasons.Store(sessionID, "user")
	ClearQueue(sessionID)
	cancel()

	ai.GetACPConnManager().CancelTurn(sessionID)

	EmitSessionEvent(sessionID, "cancelled", false)

	// Send cancelled event to SSE stream after cancelling context (non-blocking)
	if streamVal, ok := sessionStreams.Load(sessionID); ok {
		if ch, ok := streamVal.(chan ai.StreamEvent); ok {
			select {
			case ch <- ai.StreamEvent{Type: "cancelled"}:
			default:
				// Channel full — SSE handler will detect session not running via checkSSE loop
			}
		}
	}

	// Mark session as not running (skip completed event — we already sent "cancelled")
	SetSessionRunning(sessionID, false, true)

	return true
}

// ForceCancelSession cancels the AI context for a session without sending SSE events.
// Used when the SSE client has disconnected and we want to stop the AI goroutine
// to prevent zombie processes.
func ForceCancelSession(sessionID string) {
	val, ok := sessionCancels.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	sessionCancelReasons.Store(sessionID, "disconnect")
	ClearQueue(sessionID)
	if cancel, ok := val.(context.CancelFunc); ok {
		cancel()
	}
	// ISS-120: Clear activeSessions to prevent zombie entries that block new messages.
	// Skip the "completed" event (true) — ForceCancelSession is for disconnected clients
	// that won't see it anyway, and we don't want to emit a stale event on reconnection.
	SetSessionRunning(sessionID, false, true)
}

// sessionStreamBufferSize is the buffer capacity for the per-session event channel.
// Controls backpressure: when the channel is full, SendSessionEvent drops events.
const sessionStreamBufferSize = 256

// RegisterSessionStream creates and registers a stream channel for a session
func RegisterSessionStream(sessionID string) chan ai.StreamEvent {
	ch := make(chan ai.StreamEvent, sessionStreamBufferSize)
	sessionStreams.Store(sessionID, ch)
	return ch
}

// TryClaimSSEStream atomically claims the SSE stream for a session.
// Returns true if the claim was acquired (no other SSE handler is reading).
// Returns false if another SSE handler is already consuming the stream.
// The claim is released via ReleaseSSEStream when the handler exits.
func TryClaimSSEStream(sessionID string) bool {
	_, loaded := sessionSSEClaim.LoadOrStore(sessionID, true)
	return !loaded
}

// ReleaseSSEStream releases the SSE stream claim for a session.
// Called by the SSE handler on all exit paths (done, cancelled, error, disconnect).
func ReleaseSSEStream(sessionID string) {
	sessionSSEClaim.Delete(sessionID)
}

// GetSessionStream returns the stream channel for a session
func GetSessionStream(sessionID string) (<-chan ai.StreamEvent, bool) {
	val, ok := sessionStreams.Load(sessionID)
	if !ok {
		return nil, false
	}
	ch, ok := val.(chan ai.StreamEvent)
	if !ok {
		return nil, false
	}
	return ch, true
}

// UnregisterSessionStream removes and closes the stream channel for a session
func UnregisterSessionStream(sessionID string) {
	if val, ok := sessionStreams.LoadAndDelete(sessionID); ok {
		if ch, ok := val.(chan ai.StreamEvent); ok {
			close(ch)
		}
	}
	// Also release any lingering SSE claim
	sessionSSEClaim.Delete(sessionID)
}

// SendSessionEvent sends an event to the session stream channel (non-blocking).
// Returns true if the event was sent successfully.
func SendSessionEvent(sessionID string, event ai.StreamEvent) bool {
	if streamVal, ok := sessionStreams.Load(sessionID); ok {
		if ch, ok := streamVal.(chan ai.StreamEvent); ok {
			select {
			case ch <- event:
				return true
			default:
				slog.Warn(
					"session stream channel full, dropping event",
					slog.String("session_id", sessionID),
					slog.String("event_type", event.Type),
				)
			}
		}
	}
	return false
}

// chatSummaryEnabled controls whether chat message auto-summarization is active.
// Set during server startup based on config. Uses atomic.Bool for safe concurrent
// access from HTTP handlers (write) and session completion goroutines (read).
var chatSummaryEnabled atomic.Bool

// chatSummaryMode controls how chat messages are summarized.
// "simple" = extract last text after tool_use (no AI call)
// "ai" = use AI summarizer via AsyncSummarize
// "" = disabled (no summarization)
var chatSummaryMode atomic.Value // stores string

func init() {
	chatSummaryEnabled.Store(true) // default enabled
}

// SetChatSummaryEnabled configures whether chat messages are auto-summarized on completion.
func SetChatSummaryEnabled(enabled bool) {
	chatSummaryEnabled.Store(enabled)
}

// SetChatSummaryMode sets the chat summarization mode.
func SetChatSummaryMode(mode string) {
	chatSummaryMode.Store(mode)
}

// GetChatSummaryMode returns the current chat summarization mode.
func GetChatSummaryMode() string {
	v := chatSummaryMode.Load()
	if v == nil {
		return ""
	}
	return v.(string) //nolint:errcheck // type assertion is safe: only string values are stored via chatSummaryMode.Store
}

// triggerChatSummarization triggers async summarization for the last assistant
// message(s) in a session when it completes normally.
// Skipped for cancelled/disconnected sessions (those use skipEvent=true in SetSessionRunning).
func triggerChatSummarization(sessionID string) {
	mode := GetChatSummaryMode()
	if mode == "" || !chatSummaryEnabled.Load() {
		return
	}

	lastAssistant, blocks := getLastAssistantBlocks(sessionID)
	if lastAssistant == nil || len(blocks) == 0 {
		return
	}

	// Check if already summarized
	_, found := GetSummary("chat_message", lastAssistant.ID)
	if found {
		return
	}

	projectPath := GetSessionProjectPath(sessionID)

	if mode == "simple" {
		summarizeChatSimple(lastAssistant, blocks, projectPath, sessionID)
		return
	}

	// AI mode: use existing AsyncSummarize path
	if taskSummarizerInstance == nil {
		return
	}
	AsyncSummarize("chat_message", lastAssistant.ID, blocks, projectPath, sessionID)
}

// getLastAssistantBlocks returns the last assistant message and its parsed content blocks.
func getLastAssistantBlocks(sessionID string) (*model.ChatMessage, []model.ContentBlock) {
	messages, err := GetMessagesBySessionID(sessionID)
	if err != nil || len(messages) == 0 {
		return nil, nil
	}

	var lastAssistant *model.ChatMessage
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistant = &messages[i]
			break
		}
	}
	if lastAssistant == nil {
		return nil, nil
	}

	var content struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(lastAssistant.Content), &content); err != nil {
		return lastAssistant, nil
	}
	return lastAssistant, content.Blocks
}

// summarizeChatSimple extracts the last answer text and saves it as a summary.
func summarizeChatSimple(msg *model.ChatMessage, blocks []model.ContentBlock, projectPath, sessionID string) {
	text := summarize.ExtractLastAnswerFromBlocks(blocks)
	if text == "" {
		return
	}
	if err := SaveSummary("chat_message", msg.ID, text); err != nil {
		slog.Warn(
			"failed to save simple summary",
			slog.String("target_type", "chat_message"),
			slog.Int64("target_id", msg.ID),
			slog.String("err", err.Error()),
		)
		return
	}
	mgr := ws.GetManager()
	if mgr != nil {
		mgr.BroadcastEvent(ws.ServerMessage{
			Type:  ws.MessageTypeEvent,
			ID:    ws.GenerateEventID(),
			Event: "summary_update",
			Data: ws.SummaryUpdateData{
				TargetType:  "chat_message",
				TargetID:    msg.ID,
				Summary:     text,
				ProjectPath: projectPath,
				SessionID:   sessionID,
			},
		})
	}
}
