//nolint:goconst // role/status strings are domain constants
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/push/dingtalk"
	"clawbench/internal/push/feishu"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"
)

// Active session tracking - keyed by sessionID
var (
	activeSessions = make(map[string]bool)
	activeMu       sync.Mutex
)

// Session cancel functions for aborting AI responses
var (
	sessionCancels       sync.Map // map[string]context.CancelFunc
	sessionCancelReasons sync.Map // map[string]string — "user", "disconnect"
	// terminalPushDone tracks whether a terminal push notification has already been
	// claimed for a session. Guarded via LoadOrStore so only one of the done/cancel
	// race paths can send a push; cleared when a new run starts (TrySetSessionRunning).
	terminalPushDone sync.Map // map[string]struct{}
)

// responsePreviewMaxRunes is an alias for model.ResponsePreviewMaxRunes for local use.
const responsePreviewMaxRunes = model.ResponsePreviewMaxRunes

// previewAssistantContentLimit caps how many most-recent assistant messages are
// loaded when extracting a response preview. A preview only needs the latest few
// replies; 20 is far beyond any realistic "last text block" lookback while
// bounding memory on very long sessions with large tool outputs.
const previewAssistantContentLimit = 20

// EmitSessionEvent broadcasts a session_update event to connected clients.
// toolName and toolInput are optional and only used for "permission_pending" status.
func EmitSessionEvent(sessionID, status string, hasNewMessages bool, toolNameAndInput ...string) {
	emitSessionEvent(sessionID, status, hasNewMessages, true, true, toolNameAndInput...)
}

// EmitSessionEventWSOnly broadcasts a session_update event to connected clients
// WITHOUT producing a push notification or storing a pending push event. Used by
// the terminal-completion path: normal completion already sends its own push via
// EmitSessionPushNotification, but still needs the global WS broadcast so every
// client (including ones that missed the stream-level "done" event) can clear the
// session's running flag.
func EmitSessionEventWSOnly(sessionID, status string, hasNewMessages bool) {
	emitSessionEvent(sessionID, status, hasNewMessages, false, true)
}

// emitSessionEvent is EmitSessionEvent with explicit push/broadcast control.
// Callers that manage their own terminal-push guard (e.g. CancelSession) pass
// pushEnabled based on whether they won the guard, so a duplicate "cancelled" push
// is avoided. wsBroadcastEnabled additionally gates the live WS broadcast (the
// replay/pending-event buffer path is unaffected — it is only used when pushEnabled
// is true). CancelSession turns the broadcast OFF when it loses the terminal guard
// (the goroutine already broadcast "completed"), so clients never see a stale
// "cancelled" overwriting a terminal "completed".
func emitSessionEvent(sessionID, status string, hasNewMessages bool, pushEnabled bool, wsBroadcastEnabled bool, toolNameAndInput ...string) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}

	data := &ws.SessionUpdateData{
		SessionID:      sessionID,
		Status:         status,
		HasNewMessages: hasNewMessages,
	}

	// Include session title and project path for push notifications
	if title, err := GetSessionTitle(sessionID); err == nil && title != "" {
		data.SessionTitle = title
	}

	var responsePreviewRaw string
	if status == "completed" {
		// Include response preview for the completion popover (full, untruncated —
		// the frontend scrolls it). The plain-text variant stays truncated for
		// push notifications.
		responsePreviewRaw = getSessionResponsePreviewRaw(sessionID)
		data.ResponsePreview = responsePreviewRaw
		if responsePreviewRaw != "" {
			data.ResponsePreviewPlain = truncatePreview(summarize.StripMarkdown(responsePreviewRaw))
		}
		// Include the last user message so clients can show it alongside the reply
		data.LastUserMessage = GetLastUserMessagePlain(context.Background(), sessionID)
		// Include the agent so clients can render the backend icon
		data.AgentID = GetSessionAgentID(sessionID)
	}

	// Include toolName and toolInput for permission_pending events
	if status == "permission_pending" && len(toolNameAndInput) > 0 {
		data.ToolName = toolNameAndInput[0]
		if len(toolNameAndInput) > 1 {
			data.ToolInput = toolNameAndInput[1]
		}
	}

	data.ProjectPath = GetSessionProjectPath(sessionID)

	// Generate one event ID, use for both store and broadcast (write-ahead)
	msg := ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "session_update",
		Data:  data,
	}
	// Write-ahead: persist before broadcast so event log has no gaps.
	// Skip when push_mode is "disabled" — no notifications desired.
	// When pushEnabled is false (a terminal state already reported elsewhere, e.g.
	// the goroutine pushed "completed" before CancelSession ran), neither the
	// notifiable pending event nor a push is produced — only the live WS broadcast.
	if model.ConfigInstance.PushMode != "disabled" && pushEnabled {
		StoreNotifiableEvent(msg)
	}
	if wsBroadcastEnabled {
		mgr.BroadcastEvent(msg)
	}

	if !pushEnabled {
		return
	}

	pushSessionEvent(sessionID, status, msg, data, responsePreviewRaw)
}

// pushSessionEvent sends the DingTalk/Feishu push for a session event. Pass the
// raw (untruncated) preview — DingTalk/Feishu packages apply their own limit.
// If the push succeeds, the pending event is removed to avoid a duplicate
// Android notification when the app comes back online.
func pushSessionEvent(sessionID, status string, msg ws.ServerMessage, data *ws.SessionUpdateData, responsePreviewRaw string) {
	if dingtalk.IsStarted() && dingtalk.PushSessionEvent(sessionID, status, data.SessionTitle, responsePreviewRaw, data.ProjectPath, data.ToolName, data.ToolInput) {
		_ = DeletePendingEvent(msg.ID)
	} else if feishu.IsStarted() && feishu.PushSessionEvent(sessionID, status, data.SessionTitle, responsePreviewRaw, data.ProjectPath, data.ToolName, data.ToolInput) {
		_ = DeletePendingEvent(msg.ID)
	}
}

// EmitSessionPushNotification sends DingTalk/Feishu push for a session terminal event.
// Extracted from EmitSessionEvent so markDoneAndSendFinal can also trigger push
// without going through the full EmitSessionEvent path (which broadcasts WS events).
// Also stores a pending_event for Android offline replay (like EmitSessionEvent does),
// and deletes it if push succeeds.
// Returns true when this call won the terminal guard and actually issued a push
// (the first terminal state for the session), false when a terminal state was
// already claimed — callers that must also broadcast a terminal session_update
// (e.g. "completed") can use this to suppress a contradictory broadcast.
func EmitSessionPushNotification(sessionID, status string) bool {
	// Only the first terminal state to arrive sends a push. Guards against the
	// race where CancelSession already pushed "cancelled" but the goroutine then
	// completes and would otherwise push "completed".
	if !markTerminalPushDone(sessionID) {
		return false
	}
	title, err := GetSessionTitle(sessionID)
	if err != nil {
		title = ""
	}
	projectPath := GetSessionProjectPath(sessionID)
	var responsePreviewRaw string
	if status == statusCompleted {
		responsePreviewRaw = getSessionResponsePreviewRaw(sessionID)
	}

	// Store pending event for Android offline replay (mirrors EmitSessionEvent's
	// write-ahead logic). Skip when push_mode is "disabled".
	if model.ConfigInstance.PushMode != "disabled" {
		data := &ws.SessionUpdateData{
			SessionID:       sessionID,
			Status:          status,
			HasNewMessages:  true,
			SessionTitle:    title,
			ProjectPath:     projectPath,
			ResponsePreview: truncatePreview(responsePreviewRaw),
		}
		if responsePreviewRaw != "" {
			data.ResponsePreviewPlain = truncatePreview(summarize.StripMarkdown(responsePreviewRaw))
		}
		msg := ws.ServerMessage{
			Type:  ws.MessageTypeEvent,
			ID:    ws.GenerateEventID(),
			Event: "session_update",
			Data:  data,
		}
		StoreNotifiableEvent(msg)

		if dingtalk.IsStarted() && dingtalk.PushSessionEvent(sessionID, status, title, responsePreviewRaw, projectPath, "", "") {
			_ = DeletePendingEvent(msg.ID)
		} else if feishu.IsStarted() && feishu.PushSessionEvent(sessionID, status, title, responsePreviewRaw, projectPath, "", "") {
			_ = DeletePendingEvent(msg.ID)
		}
	}
	return true
}

// markTerminalPushDone atomically claims the single terminal push slot for a session.
// Returns true if this call is the first to claim it (caller should send the push),
// false if a push was already claimed. Prevents the done/cancel race from sending two
// contradictory terminal notifications.
func markTerminalPushDone(sessionID string) bool {
	_, loaded := terminalPushDone.LoadOrStore(sessionID, struct{}{})
	return !loaded
}

// getSessionResponsePreview returns a preview of the AI's final reply text.
// It extracts text from after the last tool_use block in the last assistant
// message, since the final text block(s) contain the AI's actual answer
// rather than intermediate reasoning or tool-call commentary.
func getSessionResponsePreview(sessionID string) string {
	return truncatePreview(getSessionResponsePreviewRaw(sessionID))
}

// getSessionResponsePreviewRaw returns the un-truncated preview text.// Used when both Markdown and plain-text previews are needed, so that
// StripMarkdown operates on the full text before each variant is truncated.
func getSessionResponsePreviewRaw(sessionID string) string {
	// Read raw assistant contents directly from chat_history. GetMessagesBySessionID
	// cannot be used here: enrichMessagesWithSummaries strips heavy content from
	// summarized non-streaming assistant messages (summarizeContentForView replaces
	// blocks with an empty array), which would make every push preview empty once
	// a reading summary exists for the last message.
	// Contents are newest-first; walk forward to find the most recent message
	// that yields a preview.
	contents, err := GetAssistantRawContents(sessionID)
	if err != nil {
		slog.Debug("session_event: failed to get messages for preview", "session_id", sessionID, "error", err)
		return ""
	}
	for _, raw := range contents {
		var content struct {
			Blocks []model.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal([]byte(raw), &content); err != nil {
			continue
		}
		if preview := extractPreviewFromBlocksRaw(content.Blocks); preview != "" {
			return preview
		}
	}
	return ""
}

// extractPreviewFromBlocks returns a preview string from content blocks.
// Delegates to summarize.ExtractLastAnswerFromBlocks for the extraction logic,
// then truncates for display in push notifications and WS events.
func extractPreviewFromBlocks(blocks []model.ContentBlock) string {
	return truncatePreview(extractPreviewFromBlocksRaw(blocks))
}

// extractPreviewFromBlocksRaw returns the un-truncated preview from content blocks.
func extractPreviewFromBlocksRaw(blocks []model.ContentBlock) string {
	return summarize.ExtractLastAnswerFromBlocks(blocks)
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

// GetLastUserMessagePlain returns the plain-text content of the most recent
// non-streaming, non-queued user message in a session. Used to include a
// "last user message" line in completion popovers/notifications alongside the
// AI's response preview. Returns "" when no such message exists.
func GetLastUserMessagePlain(ctx context.Context, sessionID string) string {
	if dbRead == nil || sessionID == "" {
		return ""
	}
	var content string
	err := dbRead.QueryRowContext(ctx,
		"SELECT content FROM chat_history WHERE session_id = ? AND role = 'user' AND streaming = 0 AND queued = 0 ORDER BY id DESC LIMIT 1",
		sessionID,
	).Scan(&content)
	if err != nil {
		// sql.ErrNoRows → no user message yet; other errors → treat as unavailable
		return ""
	}
	plain := ExtractPlainText(content)
	if plain == "" {
		return ""
	}
	return truncatePreview(plain)
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

	// Note: orphan finalization is NOT triggered here automatically.
	// It must be called explicitly from paths where FinalizeStreamingMessage
	// is known to have failed or will not be called. See FinalizeOrphanedMessages.

	// Emit event unless caller explicitly skips (e.g. CancelSession sends its own event)
	if len(skipEvent) == 0 || !skipEvent[0] {
		if !running {
			EmitSessionEvent(sessionID, "completed", true)
		} else {
			EmitSessionEvent(sessionID, "running", false)
		}
	}
}

// finalizeOrphanedStreamingMessages checks for and finalizes any streaming=1
// assistant messages left behind for a session (e.g. due to SQLITE_BUSY failures).
// cancelReason is captured at the time SetSessionRunning(false) is called to avoid
// a race with GetAndClearCancelReason in buildResult clearing the value first.
func finalizeOrphanedStreamingMessages(sessionID string, cancelReason string) {
	if db == nil || dbRead == nil {
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
		// ISS-252: persist any still-unslimmed thinking text into chat_thinking
		// BEFORE marking the message cancelled. The normal finalize path calls
		// persistThinkingToDB (slims thinking out of content into chat_thinking
		// and leaves a think_id marker the frontend lazy-loads). An orphaned
		// streaming row skipped Finalize entirely, so without this the thinking
		// text stays buried in content (or is lost if it was never flushed) and
		// the frontend has no think_id marker to load it from. persistThinkingToDB
		// is idempotent: already-slimmed blocks (think_id present, no text) and
		// content without thinking blocks are returned unchanged.
		slimContent := persistThinkingToDB(m.content, m.id, sessionID)

		var contentMap map[string]any
		if err := json.Unmarshal([]byte(slimContent), &contentMap); err != nil {
			contentMap = map[string]any{
				"blocks":    []any{map[string]any{"type": "text", "text": m.content}},
				"cancelled": true,
			}
		} else {
			if _, ok := contentMap["cancelled"]; !ok {
				contentMap["cancelled"] = true
				// For user-initiated cancel, just mark cancelled without a warning block.
				// The frontend renders a clean "cancelled" badge — no alarming warning needed.
				if cancelReason != "user" {
					blocks, _ := contentMap["blocks"].([]any)
					blocks = append(blocks, map[string]any{
						"type":   "warning",
						"text":   "Finalization failed, AI response may be incomplete",
						"reason": "finalize_busy",
					})
					contentMap["blocks"] = blocks
				}
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

// FinalizeOrphanedMessages finalizes any streaming=1 assistant messages left behind
// for a session. This should only be called from code paths where
// FinalizeStreamingMessage is known to have failed (e.g. SQLITE_BUSY) or will
// not be called (e.g. scheduler cancel/crash, ForceCancelSession).
// cancelReason controls the warning block: "user" suppresses it (clean cancel),
// "" or "disconnect" adds a warning.
func FinalizeOrphanedMessages(sessionID string, cancelReason string) {
	finalizeOrphanedStreamingMessages(sessionID, cancelReason)
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

	// Reset the terminal-push guard so a new run of the same session can push again.
	terminalPushDone.Delete(sessionID)

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

// CancelAllSessions cancels every registered session context without clearing
// the running state or finalizing anything. Called by the graceful-shutdown
// path so every active executor's event loop exits on ctx.Done() and runs
// Finalize (streaming=0 + final content) before the DB closes. This is what
// unblocks CLI-backend streams whose process is still alive — unlike ACP
// prompts there is no per-connection promptCancel to invoke.
//
// Entries are removed after cancelling (mirroring CancelSession's
// LoadAndDelete) so no stale cancel func outlives the shutdown.
func CancelAllSessions() {
	sessionCancels.Range(func(key, value any) bool {
		cancel, ok := value.(context.CancelFunc)
		if !ok {
			sessionCancels.Delete(key)
			return true
		}
		// Record the restart reason BEFORE cancelling so each executor's
		// buildResult (interactive mode reads GetAndClearCancelReason) persists
		// the interrupted message with a restart warning block — otherwise the
		// graceful-shutdown Finalize would only mark it cancelled:true and the
		// frontend's "服务重启，AI 响应中断" banner would never appear.
		if sid, ok := key.(string); ok {
			sessionCancelReasons.Store(sid, cancelReasonRestart)
		}
		cancel()
		sessionCancels.Delete(key)
		if sid, ok := key.(string); ok {
			slog.Debug("shutdown: cancelled session", slog.String("session_id", sid))
		}
		return true
	})
}

// SetCancelReason records the cancellation reason for a session without cancelling it.
// Used by the WS handler when a client disconnects — the AI session continues running
// but the reason is stored for the session finalizer to read later.
func SetCancelReason(sessionID string, reason string) {
	sessionCancelReasons.Store(sessionID, reason)
}

// GetAndClearCancelReason returns the reason for the most recent cancellation of a session.
// Returns "user" for user-initiated cancel, "disconnect" for WS client disconnect.
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

// GetCancelReason returns the cancellation reason without clearing it.
// Used by SetSessionRunning to capture the reason before launching the
// finalizeOrphanedStreamingMessages goroutine, avoiding a race with
// GetAndClearCancelReason in buildResult.
func GetCancelReason(sessionID string) string {
	val, ok := sessionCancelReasons.Load(sessionID)
	if !ok {
		return ""
	}
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
		// The goroutine is dead, so its RunDrainLoop cancel branch will never
		// emit queue_cancel. Collect + clear + emit here instead.
		queueIDs, _ := GetQueuedQueueIDs(sessionID)
		_ = ClearQueuedMessages(sessionID)
		if len(queueIDs) > 0 {
			ws.EmitToSession(sessionID, ai.StreamEvent{
				Type: "queue_cancel",
				QueueEvent: &ai.QueueEventData{
					SessionID: sessionID,
					QueueIDs:  queueIDs,
				},
			})
		}
		SetSessionRunning(sessionID, false, true)
		// Stuck session: nothing will finalize its streaming messages.
		FinalizeOrphanedMessages(sessionID, "user")
		return true
	}
	cancel, ok := val.(context.CancelFunc)
	if !ok {
		return false
	}

	// Cancel the Go context first so the agent process starts shutting down,
	// freeing its stdin pipe. For ACP sessions the context cancellation makes
	// the SDK's Prompt() return ctx.Err() and automatically send exactly one
	// `session/cancel` notification to the agent (see acp-go-sdk
	// ClientSideConnection.Prompt). Do NOT also call
	// ACPConnManager.CancelTurn here: doing so would send a SECOND
	// session/cancel in quick succession. CodeBuddy treats back-to-back
	// cancels as two separate user cancels and its run auto-restart can leave
	// the session in a stale "pendingCancellations" state that poisons the
	// next turn's permission gate (msg 43596: next turn spuriously resolved
	// as cancelled / outcome=CANCELLED).
	sessionCancelReasons.Store(sessionID, "user")
	// NOTE: do NOT clear queued messages here — the goroutine's RunDrainLoop
	// cancel branch collects the queueIDs, clears them and emits queue_cancel
	// itself. Clearing first would lose the queue_cancel event.
	cancel()

	// Claim the terminal push slot BEFORE emitting. If a concurrent terminal path
	// (the goroutine's done) already claimed it, we lose the guard — suppress the
	// "cancelled" push (which would otherwise contradict the "completed" push the
	// goroutine already sent). Losing the guard also means a terminal "completed"
	// session_update was already broadcast, so the WS "cancelled" broadcast is
	// suppressed too: broadcasting it would overwrite the client's terminal
	// "completed" state with a contradictory "cancelled" (ISS-247).
	wonPush := markTerminalPushDone(sessionID)
	emitSessionEvent(sessionID, "cancelled", false, wonPush, wonPush)

	// Mark session as not running (skip completed event — we already sent "cancelled")
	SetSessionRunning(sessionID, false, true)

	return true
}

// ForceCancelSession cancels the AI context for a session without sending WS events.
// Used when the WS client has disconnected and we want to stop the AI goroutine
// to prevent zombie processes.
func ForceCancelSession(sessionID string) {
	val, ok := sessionCancels.LoadAndDelete(sessionID)
	if !ok {
		return
	}
	sessionCancelReasons.Store(sessionID, "disconnect")
	if err := ClearQueuedMessages(sessionID); err != nil {
		slog.Warn("forceCancel: failed to clear queued messages",
			slog.String("session", sessionID), slog.String("error", err.Error()))
	}
	if cancel, ok := val.(context.CancelFunc); ok {
		cancel()
	}
	// ISS-120: Clear activeSessions to prevent zombie entries that block new messages.
	// Skip the "completed" event (true) — ForceCancelSession is for disconnected clients
	// that won't see it anyway, and we don't want to emit a stale event on reconnection.
	SetSessionRunning(sessionID, false, true)

	// ForceCancel: the AI goroutine may still be running and may or may not
	// complete FinalizeStreamingMessage. Launch orphan cleanup with a delay
	// to give the goroutine a chance to finalize normally. If Finalize
	// succeeds, streaming=0 and the orphan check is a no-op.
	go func() {
		time.Sleep(2 * time.Second)
		FinalizeOrphanedMessages(sessionID, "disconnect")
	}()
}

// triggerChatSummarization triggers summarization for every assistant message
// in a session that does not yet have a summary.
// Skipped for cancelled/disconnected sessions (those use skipEvent=true in SetSessionRunning).
// Reading summaries always extract the conclusion (no AI), matching scheduled tasks.
//
// Summarizing all (not just the last) assistant messages is important because
// queued/drained messages share the same session goroutine: when a long reply
// completes and a queued message is drained immediately, only the final reply
// used to get a summary and every intermediate reply was skipped. Summarizing
// each missing message closes that gap.
func triggerChatSummarization(ctx context.Context, sessionID string) {
	if dbRead == nil {
		return
	}
	projectPath := GetSessionProjectPath(sessionID)
	messages, err := GetMessagesBySessionID(sessionID)
	if err != nil || len(messages) == 0 {
		return
	}

	lastAssistant := (*model.ChatMessage)(nil)
	for i := range messages {
		if messages[i].Role != "assistant" {
			continue
		}
		lastAssistant = &messages[i]
		blocks, err := parseMessageBlocks(messages[i].Content)
		if err != nil || len(blocks) == 0 {
			continue
		}
		if _, found := GetSummary("chat_message", messages[i].ID); found {
			continue
		}
		summarizeMessageOnce(messages[i].ID, blocks, projectPath, sessionID)
	}

	// 推荐回复: only for the last assistant message. If enabled, generate a
	// next-step recommendation from its conclusion and emit it to the frontend
	// (auto-fill/建议 chip).
	if lastAssistant == nil {
		return
	}
	// Re-read the last assistant message's raw content from DB. GetMessagesBySessionID
	// goes through enrichMessagesWithSummaries, which strips the blocks of any
	// assistant message that already has a reading summary (summarizeContentForView
	// replaces them with an empty {"blocks":[]}). Without this, the recommendation
	// silently stops firing once a reply's summary exists — every subsequent
	// lastAssistant.Content would parse to zero blocks and the len(blocks) > 0
	// guard below would skip the recommendation every time.
	if blocks, err := rawAssistantBlocks(ctx, lastAssistant.ID, lastAssistant.Content); err == nil && len(blocks) > 0 {
		// Run the recommendation in a background goroutine: RecommendNextStep makes
		// a blocking LLM call (up to the 60s internal timeout) that would otherwise
		// stall Finalize() and delay the terminal 'done' WS event by seconds. The
		// frontend keys the message meta bar AND the completion sound to that 'done'
		// event, so an inline call here makes the whole reply feel laggy after the
		// content stops streaming. It also leaves the session reporting running=true
		// and the DB message streaming=1 for the duration, which surfaces a phantom
		// "loading" placeholder when the user switches to the session meanwhile. The
		// recommendation chip is a nice-to-have and can arrive whenever the LLM
		// responds. blocks is a fresh slice parsed above — safe to hand to the goroutine.
		//
		// The session's ctx is cancelled as soon as the handler goroutine returns
		// (defer cancel() in the AI goroutine). Without stripping the cancellation
		// signal, the recommendation's LLM call — which legitimately outlives the
		// reply stream — would be aborted almost immediately, so recommendations
		// would silently never appear. The detached ctx still carries no deadline
		// of its own; triggerChatRecommendation applies its own 60s timeout.
		go triggerChatRecommendation(context.WithoutCancel(ctx), sessionID, projectPath, lastAssistant.ID, blocks)
	}
}

// rawAssistantBlocks returns the parsed ContentBlock array of an assistant
// message, preferring the raw (unmodified) content from DB over the possibly
// summary-stripped view. GetMessagesBySessionID goes through
// enrichMessagesWithSummaries, which strips the blocks of any assistant message
// that already has a reading summary (summarizeContentForView replaces them
// with an empty {"blocks":[]}). The recommendation feature needs the real
// blocks to extract the assistant's latest conclusion, so when the provided
// view content parses to zero blocks it falls back to the DB content. Returns
// an empty slice when the message is missing, still streaming, or unparsable.
func rawAssistantBlocks(ctx context.Context, messageID int64, viewContent string) ([]model.ContentBlock, error) {
	if blocks, err := parseMessageBlocks(viewContent); err == nil && len(blocks) > 0 {
		return blocks, nil
	}
	if dbRead == nil {
		return nil, nil
	}
	// Filter streaming = 0 in SQL (matching backfillMissingSummaries) so a
	// half-persisted placeholder row can never be read as the final answer.
	var content string
	if err := dbRead.QueryRowContext(ctx,
		"SELECT content FROM chat_history WHERE id = ? AND streaming = 0",
		messageID,
	).Scan(&content); err != nil {
		return nil, err
	}
	return parseMessageBlocks(content)
}

// parseMessageBlocks unmarshals message content into its ContentBlock array.
func parseMessageBlocks(content string) ([]model.ContentBlock, error) {
	var parsed struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, err
	}
	return parsed.Blocks, nil
}

// triggerChatRecommendation generates a next-step recommendation (推荐回复) after
// an assistant reply completes, using the shared ai_summary LLM config. Emits a
// chat_recommendation WS event when a recommendation is produced.
//
// ctx comes from the caller: triggerChatSummarization hands over a context
// derived with context.WithoutCancel, so the session's cancel() does not abort
// this goroutine's LLM call (which legitimately outlives the reply stream).
// The call is bounded by the 60s timeout established inside this function; a
// recover() wraps the body so a panic here can never crash the process — it
// runs detached from the main session goroutine.
func triggerChatRecommendation(ctx context.Context, sessionID, projectPath string, messageID int64, blocks []model.ContentBlock) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("chat recommendation goroutine panicked",
				slog.String("session_id", sessionID),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()
	if ctx.Err() != nil {
		return
	}
	if !model.ConfigInstance.Chat.RecommendEnabled {
		return
	}
	if model.ConfigInstance.AISummary.API.BaseURL == "" {
		return
	}
	conclusion := summarize.ExtractLastAnswerFromBlocks(blocks)
	if strings.TrimSpace(conclusion) == "" {
		return
	}

	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return
	}

	// Gather the most recent conversation turns (user messages in full,
	// assistant messages as their conclusion) so the recommendation can account
	// for the user's recent intent.
	conversation := recentConversation(ctx, sessionID, model.ConfigInstance.Chat.RecommendContextMessages)
	commands := quickCommandList(projectPath)
	projContext := projectContext(projectPath)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	recommendation, err := summarize.RecommendNextStep(ctx, summarizer, conversation, commands, projContext, conclusion, "zh")
	if err != nil {
		slog.Debug("chat recommendation failed", slog.String("session_id", sessionID), slog.String("err", err.Error()))
		return
	}
	recommendation = strings.TrimSpace(recommendation)
	if recommendation == "" {
		return
	}

	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "chat_recommendation",
		Data: ws.ChatRecommendationData{
			SessionID:      sessionID,
			ProjectPath:    projectPath,
			MessageID:      messageID,
			Recommendation: recommendation,
		},
	})
	slog.Info("chat recommendation emitted", slog.String("session_id", sessionID))

	// Persist so the recommendation is available even if the client was offline
	// when the session completed (e.g. APP not open at the time).
	SaveChatRecommendation(sessionID, projectPath, messageID, recommendation)
}

// SaveChatRecommendation persists a conversation recommendation so it can be
// fetched later (e.g. when a client that was offline opens the session).
func SaveChatRecommendation(sessionID, projectPath string, messageID int64, recommendation string) {
	_, err := WriteExec(
		"INSERT INTO chat_recommendations (session_id, project_path, message_id, recommendation) VALUES (?, ?, ?, ?)",
		sessionID, projectPath, messageID, recommendation,
	)
	if err != nil {
		slog.Debug("failed to persist chat recommendation", slog.String("session_id", sessionID), slog.String("err", err.Error()))
	}
}

// LatestChatRecommendation returns the most recent recommendation for a session
// that was generated for the given assistant message. If no recommendation
// belongs to that exact message (e.g. it was generated for an earlier reply, or
// has not been produced yet), it returns "" so the client never surfaces a
// stale recommendation from a previous reply. Returns empty string if none.
func LatestChatRecommendation(ctx context.Context, sessionID string, messageID int64) string {
	var rec string
	err := dbRead.QueryRowContext(
		ctx,
		"SELECT recommendation FROM chat_recommendations WHERE session_id = ? AND message_id = ? ORDER BY id DESC LIMIT 1",
		sessionID, messageID,
	).Scan(&rec)
	if err != nil {
		return ""
	}
	return rec
}

// recentConversation returns the text of the most recent n messages in a
// session. User messages are included in full; assistant messages are reduced
// to their conclusion (text after the last tool_use). Limited to n messages
// (0 or negative = no context).
func recentConversation(ctx context.Context, sessionID string, n int) []string {
	if n <= 0 {
		return nil
	}
	messages, err := GetMessagesBySessionID(sessionID)
	if err != nil {
		slog.Debug("chat recommendation: failed to load messages for context", slog.String("session_id", sessionID), slog.String("err", err.Error()))
		return nil
	}
	var texts []string
	for i := len(messages) - 1; i >= 0 && len(texts) < n; i-- {
		var text string
		switch messages[i].Role {
		case "user":
			text = ExtractPlainText(messages[i].Content)
		case "assistant":
			// GetMessagesBySessionID strips the blocks of any assistant message
			// that already has a reading summary (summarizeContentForView yields
			// an empty {"blocks":[]}). Re-read the raw content so the conclusion
			// the recommendation LLM sees is the real answer, not nothing.
			blocks, _ := rawAssistantBlocks(ctx, messages[i].ID, messages[i].Content)
			text = assistantConclusionFromBlocks(blocks)
			// Legacy assistant content that is not blocks JSON (bare content
			// array, ACP notification wrapper, plain text) parses to zero blocks;
			// fall back to plain-text extraction so such messages still contribute
			// context instead of being silently skipped.
			if text == "" {
				text = ExtractPlainText(messages[i].Content)
			}
		default:
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		texts = append([]string{text}, texts...) // keep chronological order
	}
	return texts
}

// assistantConclusionFromBlocks extracts the conclusion text from an already
// parsed ContentBlock array: the last answer (text after the last tool_use),
// plus any AskUserQuestion cards so the recommendation prompt can reference
// the options. Returns an empty string when there are no blocks.
func assistantConclusionFromBlocks(blocks []model.ContentBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	conclusion := summarize.ExtractLastAnswerFromBlocks(blocks)
	if q := askQuestionText(blocks); q != "" {
		conclusion += q
	}
	return conclusion
}

// askQuestionText renders any AskUserQuestion cards in the assistant blocks as
// plain text, reusing extractSummaryCards to parse them. Covers both the
// <ask-question> tag form (cards.AskQuestions) and the converted AskUserQuestion
// tool_use form (cards.Tools[].Input["questions"]).
func askQuestionText(blocks []model.ContentBlock) string {
	cards := extractSummaryCards(blocks)
	var qs []model.AskQuestionCard
	qs = append(qs, cards.AskQuestions...)
	for _, tool := range cards.Tools {
		if !strings.EqualFold(tool.Name, "AskUserQuestion") {
			continue
		}
		raw, ok := tool.Input["questions"]
		if !ok {
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var parsed []model.AskQuestionCard
		if json.Unmarshal(data, &parsed) != nil {
			continue
		}
		qs = append(qs, parsed...)
	}
	if len(qs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n[AI asks the user to choose]\n")
	for _, q := range qs {
		if q.Question != "" {
			b.WriteString("Question: " + q.Question + "\n")
		}
		for _, o := range q.Options {
			if o.Label == "" {
				continue
			}
			if o.Description != "" {
				b.WriteString("- " + o.Label + " (" + o.Description + ")\n")
			} else {
				b.WriteString("- " + o.Label + "\n")
			}
		}
	}
	return b.String()
}

// quickCommandList returns the quick-send command bodies available for a
// project, so the recommendation can suggest the actual command to run.
func quickCommandList(projectPath string) []string {
	items, err := GetChatQuickSend(projectPath)
	if err != nil {
		slog.Debug("chat recommendation: failed to load quick commands", slog.String("project", projectPath), slog.String("err", err.Error()))
		return nil
	}
	return quickCommandDetails(items)
}

// quickCommandDetails extracts just the command body from each item, omitting
// the label, so the recommendation recommends the command itself rather than
// its title.
func quickCommandDetails(items []ChatQuickSendItem) []string {
	commands := make([]string, 0, len(items))
	for _, it := range items {
		if cmd := strings.TrimSpace(it.Command); cmd != "" {
			commands = append(commands, cmd)
		}
	}
	return commands
}

// projectContextFiles are the project context files loaded (in order) as
// stable recommendation context. Their content rarely changes, so it forms a
// good prompt-cacheable prefix.
var projectContextFiles = []string{"AGENTS.md", "CLAUDE.md", "CODEBUDDY.md", "GEMINI.md", "README.md"}

// projectContextMaxBytes caps how much of the chosen file is injected, so a very
// large AGENTS.md (or README.md) cannot bloat the cheap recommendation call.
const projectContextMaxBytes = 4096

// projectContext loads the project context files (AGENTS.md, CLAUDE.md,
// CODEBUDDY.md, GEMINI.md, README.md) as a bounded, deterministic string for the
// recommendation's stable context. Only the FIRST non-empty file in the chain is
// used — files are never stacked. Missing, unreadable, or empty files are
// skipped. The byte prefix stays stable across turns, which is what lets prompt
// caching hit.
func projectContext(projectPath string) []string {
	if projectPath == "" {
		return nil
	}
	for _, name := range projectContextFiles {
		if text := readContextFile(projectPath, name); text != "" {
			return []string{"--- " + name + " ---\n" + text}
		}
	}
	return nil
}

// readContextFile reads a single project file, capped at projectContextMaxBytes,
// returning "" if the file is missing, unreadable, or empty/whitespace-only.
func readContextFile(projectPath, name string) string {
	data, err := os.ReadFile(filepath.Join(projectPath, name))
	if err != nil {
		return ""
	}
	text := string(data)
	if len(text) > projectContextMaxBytes {
		text = text[:projectContextMaxBytes]
	}
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

// RespondPermission delivers a user's approval/rejection response to a pending
// ACP permission request. Extracted from the HTTP handler so it can be called
// from both the WS handler and the HTTP endpoint.
func RespondPermission(sessionID, toolCallID, optionID string, cancelled bool) error {
	// Resolve ClawBench session ID → ACP session ID via agent ID
	agentID := GetSessionAgentID(sessionID)
	if agentID == "" {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Look up the ACP connection for this ClawBench session
	mgr := ai.GetACPConnManager()
	conn := mgr.GetConn(sessionID)
	if conn == nil {
		return fmt.Errorf("session not running: %s", sessionID)
	}

	client := conn.GetClient()
	if client == nil {
		return fmt.Errorf("session not running: %s", sessionID)
	}

	// We need the ACP session ID to construct the permission key.
	acpSessionID := conn.AcpSID()
	if acpSessionID == "" {
		return fmt.Errorf("ACP session not found: %s", sessionID)
	}

	// The frontend sends the permissionBlockID (prefixed with "perm_") as toolCallId.
	// Strip the prefix to recover the original ACP tool call ID used in PermissionKey.
	if len(toolCallID) > 5 && toolCallID[:5] == "perm_" {
		toolCallID = toolCallID[5:]
	}

	key := ai.PermissionKey(acpSessionID, toolCallID)

	ok := client.RespondPermission(key, optionID, cancelled)
	if !ok {
		slog.Warn(
			"permission respond: no pending permission found",
			"session_id", sessionID,
			"tool_call_id", toolCallID,
		)
		return fmt.Errorf("no pending permission found for session %s, tool %s", sessionID, toolCallID)
	}

	slog.Info(
		"permission respond: user responded to permission request",
		"session_id", sessionID,
		"tool_call_id", toolCallID,
		"option_id", optionID,
		"cancelled", cancelled,
	)

	return nil
}
