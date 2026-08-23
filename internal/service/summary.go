package service

import (
	"log/slog"
	"sync"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"
)

// summaryInFlight tracks chat-message IDs currently being summarized by the
// bulk background paths (triggerChatSummarization, backfillMissingSummaries).
// GetChatHistoryPaged spawns a backfill goroutine on every read, so without this
// dedup concurrent reads would generate duplicate summary goroutines — and
// duplicate DB writes + WS broadcasts — for the same message.
var summaryInFlight sync.Map // map[int64]struct{} keyed by chat_history message ID

// summarizeMessageOnce runs summarizeMessage under a per-message in-flight guard.
// If another goroutine is already summarizing the same message, it returns false
// (skipped) and the in-flight call will persist the summary. Returns true when
// this call performed the summarization.
func summarizeMessageOnce(targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) bool {
	if _, loaded := summaryInFlight.LoadOrStore(targetID, struct{}{}); loaded {
		return false
	}
	defer summaryInFlight.Delete(targetID)
	_ = summarizeMessage(targetID, blocks, projectPath, sessionID)
	return true
}

// summarizeMessage extracts the last answer text and saves it as a reading
// summary for a chat message, without any AI call or length threshold.
// Cards (AskUserQuestion, permission approval tools) are extracted unchanged.
// Both interactive chat (triggerChatSummarization, backfillMissingSummaries)
// and scheduled tasks (executeTask) route through this function.
// Returns an error when the summary could not be saved; async callers discard
// it and rely on the internal logging.
func summarizeMessage(targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) error {
	text := summarize.ExtractLastAnswerFromBlocks(blocks)
	if text == "" {
		return nil
	}
	cards := extractSummaryCards(blocks)
	if err := SaveSummaryWithCards("chat_message", targetID, text, cards); err != nil {
		slog.Warn(
			"failed to save summary",
			slog.Int64("target_id", targetID),
			slog.String("err", err.Error()),
		)
		return err
	}
	broadcastSummaryUpdate("chat_message", targetID, text, cards, projectPath, sessionID)
	return nil
}

// GenerateMessageSummaryOnDemand generates a reading summary for a single chat
// message on demand, e.g. when the user clicks the summary button on a
// historical message that has no summary yet. If a summary already exists it is
// returned unchanged. Only non-streaming assistant messages can be summarized
// (matching triggerChatSummarization); anything else returns ok=false. Returns
// the summary text (empty when no answer could be extracted, e.g. the message
// has no text/tool blocks), its cards, whether a summary is available, and any
// load/save error.
func GenerateMessageSummaryOnDemand(messageID int64) (summary string, cards *model.SummaryCards, ok bool, err error) {
	msg, err := GetMessageByID(messageID)
	if err != nil {
		return "", nil, false, err
	}
	if msg.Role != roleAssistant || msg.Streaming {
		return "", nil, false, nil
	}
	if existing, found := GetSummary("chat_message", messageID); found {
		_, existingCards, _ := GetSummaryWithCards("chat_message", messageID)
		return existing, existingCards, true, nil
	}
	// Non-JSON content (e.g. a plain-text assistant message) means there are no
	// blocks to summarize — treated as "no summary available", not an error.
	blocks, _ := parseMessageBlocks(msg.Content)
	if len(blocks) == 0 {
		return "", nil, false, nil
	}
	// Save errors must propagate so the caller can surface a real failure
	// instead of silently reporting "no summary" to the user.
	if err := summarizeMessage(messageID, blocks, msg.ProjectPath, msg.SessionID); err != nil {
		return "", nil, false, err
	}
	if existing, found := GetSummary("chat_message", messageID); found {
		_, existingCards, _ := GetSummaryWithCards("chat_message", messageID)
		return existing, existingCards, true, nil
	}
	return "", nil, false, nil
}

// broadcastSummaryUpdate emits a summary_update WebSocket event for a target.
func broadcastSummaryUpdate(targetType string, targetID int64, summary string, cards *model.SummaryCards, projectPath, sessionID string) {
	mgr := ws.GetManager()
	if mgr == nil {
		return
	}
	mgr.BroadcastEvent(ws.ServerMessage{
		Type:  ws.MessageTypeEvent,
		ID:    ws.GenerateEventID(),
		Event: "summary_update",
		Data: ws.SummaryUpdateData{
			TargetType:   targetType,
			TargetID:     targetID,
			Summary:      summary,
			SummaryCards: cards,
			ProjectPath:  projectPath,
			SessionID:    sessionID,
		},
	})
}
