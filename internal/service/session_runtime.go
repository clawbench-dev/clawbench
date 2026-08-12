//nolint:goconst // role/status strings are domain constants
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
)

// responsePreviewMaxRunes is an alias for model.ResponsePreviewMaxRunes for local use.
const responsePreviewMaxRunes = model.ResponsePreviewMaxRunes

// EmitSessionEvent broadcasts a session_update event to connected clients.
// toolName and toolInput are optional and only used for "permission_pending" status.
func EmitSessionEvent(sessionID, status string, hasNewMessages bool, toolNameAndInput ...string) {
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
		// Include response preview for DingTalk (Markdown) and Android/browser (plain text)
		responsePreviewRaw = getSessionResponsePreviewRaw(sessionID)
		data.ResponsePreview = truncatePreview(responsePreviewRaw)
		if responsePreviewRaw != "" {
			data.ResponsePreviewPlain = truncatePreview(summarize.StripMarkdown(responsePreviewRaw))
		}
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
	// Write-ahead: persist before broadcast so event log has no gaps
	// Skip when push_mode is "disabled" — no notifications desired
	if model.ConfigInstance.PushMode != "disabled" {
		StoreNotifiableEvent(msg)
	}
	mgr.BroadcastEvent(msg)

	// DingTalk push notification for session events.
	// Pass raw (untruncated) preview — DingTalk package applies its own limit.
	// If push succeeds, remove from pending_events to avoid duplicate
	// Android notification when the app comes back online.
	if dingtalk.IsStarted() && dingtalk.PushSessionEvent(sessionID, status, data.SessionTitle, responsePreviewRaw, data.ProjectPath, data.ToolName, data.ToolInput) {
		_ = DeletePendingEvent(msg.ID)
	} else if feishu.IsStarted() && feishu.PushSessionEvent(sessionID, status, data.SessionTitle, responsePreviewRaw, data.ProjectPath, data.ToolName, data.ToolInput) {
		_ = DeletePendingEvent(msg.ID)
	}
}

// getSessionResponsePreview returns a preview of the AI's final reply text.
// It extracts text from after the last tool_use block in the last assistant
// message, since the final text block(s) contain the AI's actual answer
// rather than intermediate reasoning or tool-call commentary.
func getSessionResponsePreview(sessionID string) string {
	return truncatePreview(getSessionResponsePreviewRaw(sessionID))
}

// getSessionResponsePreviewRaw returns the un-truncated preview text.
// Used when both Markdown and plain-text previews are needed, so that
// StripMarkdown operates on the full text before each variant is truncated.
func getSessionResponsePreviewRaw(sessionID string) string {
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
		var contentMap map[string]any
		if err := json.Unmarshal([]byte(m.content), &contentMap); err != nil {
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
		ClearQueue(sessionID)
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
	// freeing its stdin pipe. Then send ACP Cancel (with 3s timeout) so the
	// agent can stop its turn gracefully on next stdin read.
	sessionCancelReasons.Store(sessionID, "user")
	ClearQueue(sessionID)
	cancel()

	ai.GetACPConnManager().CancelTurn(sessionID)

	EmitSessionEvent(sessionID, "cancelled", false)

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
	ClearQueue(sessionID)
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

// triggerChatSummarization triggers summarization for the last assistant
// message(s) in a session when it completes normally.
// Skipped for cancelled/disconnected sessions (those use skipEvent=true in SetSessionRunning).
// Reading summaries always extract the conclusion (no AI), matching scheduled tasks.
func triggerChatSummarization(sessionID string) {
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
	summarizeTarget("chat_message", lastAssistant.ID, blocks, projectPath, sessionID)

	// 对话推荐: if enabled, generate a next-step recommendation from the
	// assistant's conclusion and emit it to the frontend (auto-fill/建议 chip).
	triggerChatRecommendation(sessionID, projectPath, blocks)
}

// triggerChatRecommendation generates a next-step recommendation (对话推荐) after
// an assistant reply completes, using the shared ai_summary LLM config. Emits a
// chat_recommendation WS event when a recommendation is produced.
func triggerChatRecommendation(sessionID, projectPath string, blocks []model.ContentBlock) {
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
	conversation := recentConversation(sessionID, model.ConfigInstance.Chat.RecommendContextMessages)
	commands := quickCommandList(projectPath)
	projContext := projectContext(projectPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
			Recommendation: recommendation,
		},
	})
	slog.Info("chat recommendation emitted", slog.String("session_id", sessionID))

	// Persist so the recommendation is available even if the client was offline
	// when the session completed (e.g. APP not open at the time).
	SaveChatRecommendation(sessionID, projectPath, recommendation)
}

// SaveChatRecommendation persists a conversation recommendation so it can be
// fetched later (e.g. when a client that was offline opens the session).
func SaveChatRecommendation(sessionID, projectPath, recommendation string) {
	_, err := WriteExec(
		"INSERT INTO chat_recommendations (session_id, project_path, recommendation) VALUES (?, ?, ?)",
		sessionID, projectPath, recommendation,
	)
	if err != nil {
		slog.Debug("failed to persist chat recommendation", slog.String("session_id", sessionID), slog.String("err", err.Error()))
	}
}

// LatestChatRecommendation returns the most recent recommendation for a session.
// Returns empty string if none exists.
func LatestChatRecommendation(sessionID string) string {
	var rec string
	err := dbRead.QueryRow(
		"SELECT recommendation FROM chat_recommendations WHERE session_id = ? ORDER BY id DESC LIMIT 1",
		sessionID,
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
func recentConversation(sessionID string, n int) []string {
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
		if messages[i].Role == "user" {
			text = ExtractPlainText(messages[i].Content)
		} else if messages[i].Role == "assistant" {
			text = assistantConclusion(messages[i].Content)
		} else {
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		texts = append([]string{text}, texts...) // keep chronological order
	}
	return texts
}

// assistantConclusion extracts the conclusion text from an assistant message's
// blocks content (text after the last tool_use), appending any AskUserQuestion
// cards so the recommendation prompt can reference the options.
func assistantConclusion(content string) string {
	if !strings.HasPrefix(content, `{"blocks":`) {
		return content
	}
	var wrapper struct {
		Blocks []model.ContentBlock `json:"blocks"`
	}
	if json.Unmarshal([]byte(content), &wrapper) != nil {
		return content
	}
	conclusion := summarize.ExtractLastAnswerFromBlocks(wrapper.Blocks)
	if q := askQuestionText(wrapper.Blocks); q != "" {
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

// quickCommandList returns the quick-send commands available for a project,
// formatted as "label: command" for the recommendation prompt.
func quickCommandList(projectPath string) []string {
	items, err := GetChatQuickSend(projectPath)
	if err != nil {
		slog.Debug("chat recommendation: failed to load quick commands", slog.String("project", projectPath), slog.String("err", err.Error()))
		return nil
	}
	commands := make([]string, 0, len(items))
	for _, it := range items {
		label := strings.TrimSpace(it.Label)
		cmd := strings.TrimSpace(it.Command)
		if label == "" && cmd == "" {
			continue
		}
		commands = append(commands, label+": "+cmd)
	}
	return commands
}

// projectContextFiles are the project instruction files loaded (in order) as
// stable recommendation context. Their content rarely changes, so it forms a
// good prompt-cacheable prefix.
var projectContextFiles = []string{"AGENTS.md", "CLAUDE.md", "CODEBUDDY.md", "GEMINI.md"}

// projectContextFallbackFile is used only when none of the dedicated instruction
// files above exist, so the recommendation still has minimal project awareness.
const projectContextFallbackFile = "README.md"

// projectContextMaxBytes caps how much of each file is injected, so a very large
// AGENTS.md (or README.md fallback) cannot bloat the cheap recommendation call.
const projectContextMaxBytes = 4096

// projectContext loads the project's instruction files (AGENTS.md, CLAUDE.md,
// CODEBUDDY.md, GEMINI.md) as bounded, deterministic strings for the
// recommendation's stable context. Files that are missing, unreadable, or empty
// are skipped. If none of those exist, README.md is used as a final fallback.
// The byte prefix stays stable across turns, which is what lets prompt caching
// hit.
func projectContext(projectPath string) []string {
	if projectPath == "" {
		return nil
	}
	var out []string
	for _, name := range projectContextFiles {
		if text := readContextFile(projectPath, name); text != "" {
			out = append(out, "--- "+name+" ---\n"+text)
		}
	}
	if len(out) == 0 {
		if text := readContextFile(projectPath, projectContextFallbackFile); text != "" {
			out = append(out, "--- "+projectContextFallbackFile+" ---\n"+text)
		}
	}
	return out
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
// Kept as a thin wrapper over the shared summarizeSimple for the existing simple-mode
// tests; new callers should use summarizeTarget directly.
func summarizeChatSimple(msg *model.ChatMessage, blocks []model.ContentBlock, projectPath, sessionID string) {
	summarizeSimple("chat_message", msg.ID, blocks, projectPath, sessionID)
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
