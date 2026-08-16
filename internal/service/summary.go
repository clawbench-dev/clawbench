package service

import (
	"log/slog"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"
)

// summarizeTarget is the single shared entry point for generating a reading
// summary for a chat message or task execution. Both interactive chat
// (triggerChatSummarization) and scheduled tasks (executeTask) route through
// this function so they follow the exact same strategy: always extract the
// last answer text and save it directly (no AI call, no threshold). Cards
// (AskUserQuestion, permission approval tools) are extracted unchanged.
func summarizeTarget(targetType string, targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) { //nolint:unparam // targetType always "chat_message"; kept generic to mirror the previous AsyncSummarize signature
	// Async caller (triggerChatSummarization): discard the save error and rely on
	// summarizeSimple's internal logging, matching the pre-refactor behavior.
	_ = summarizeSimple(targetType, targetID, blocks, projectPath, sessionID)
}

// summarizeSimple extracts the last answer text and saves it as a summary
// without any AI call or length threshold. Shared by interactive chat and
// scheduled tasks via summarizeTarget. Returns an error when the summary could
// not be saved so on-demand callers can surface a real failure to the user;
// async callers (triggerChatSummarization) discard it and only log.
func summarizeSimple(targetType string, targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) error {
	text := summarize.ExtractLastAnswerFromBlocks(blocks)
	if text == "" {
		return nil
	}
	cards := extractSummaryCards(blocks)
	if err := SaveSummaryWithCards(targetType, targetID, text, cards); err != nil {
		slog.Warn(
			"failed to save simple summary",
			slog.String("target_type", targetType),
			slog.Int64("target_id", targetID),
			slog.String("err", err.Error()),
		)
		return err
	}
	broadcastSummaryUpdate(targetType, targetID, text, cards, projectPath, sessionID)
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
	if err := summarizeSimple("chat_message", messageID, blocks, msg.ProjectPath, msg.SessionID); err != nil {
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
