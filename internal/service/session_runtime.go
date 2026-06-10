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
func EmitSessionEvent(sessionID, status string, hasNewMessages bool) {
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

	data.ProjectPath = GetSessionProjectPath(sessionID)

	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  "event",
		ID:    ws.GenerateEventID(),
		Event: "session_update",
		Data:  data,
	})
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
		// Find the last tool_use block index to skip intermediate text
		lastToolIdx := -1
		for j, b := range content.Blocks {
			if b.Type == "tool_use" {
				lastToolIdx = j
			}
		}
		// Extract text from blocks after the last tool_use
		for j := lastToolIdx + 1; j < len(content.Blocks); j++ {
			b := content.Blocks[j]
			if b.Type == "text" && b.Text != "" {
				if utf8.RuneCountInString(b.Text) > responsePreviewMaxRunes {
					return string([]rune(b.Text)[:responsePreviewMaxRunes]) + "…"
				}
				return b.Text
			}
		}
	}
	return ""
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

func init() {
	chatSummaryEnabled.Store(true) // default enabled
}

// SetChatSummaryEnabled configures whether chat messages are auto-summarized on completion.
func SetChatSummaryEnabled(enabled bool) {
	chatSummaryEnabled.Store(enabled)
}

// triggerChatSummarization triggers async summarization for the last assistant
// message(s) in a session when it completes normally.
// Skipped for cancelled/disconnected sessions (those use skipEvent=true in SetSessionRunning).
func triggerChatSummarization(sessionID string) {
	if !chatSummaryEnabled.Load() || taskSummarizerInstance == nil {
		return
	}

	// Get the last assistant message for this session
	messages, err := GetMessagesBySessionID(sessionID)
	if err != nil || len(messages) == 0 {
		return
	}

	// Find the last assistant message
	var lastAssistant *model.ChatMessage
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			lastAssistant = &messages[i]
			break
		}
	}
	if lastAssistant == nil {
		return
	}

	// Parse blocks from the assistant content
	var content struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(lastAssistant.Content), &content); err != nil {
		return
	}
	if len(content.Blocks) == 0 {
		return
	}

	// Check if already summarized
	_, found := GetSummary("chat_message", lastAssistant.ID)
	if found {
		return
	}

	// Get project path for WS event
	projectPath := GetSessionProjectPath(sessionID)

	AsyncSummarize("chat_message", lastAssistant.ID, content.Blocks, projectPath, sessionID)
}
