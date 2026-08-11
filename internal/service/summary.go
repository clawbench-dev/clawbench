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
	summarizeSimple(targetType, targetID, blocks, projectPath, sessionID)
}

// summarizeSimple extracts the last answer text and saves it as a summary
// without any AI call or length threshold. Shared by interactive chat and
// scheduled tasks via summarizeTarget.
func summarizeSimple(targetType string, targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) {
	text := summarize.ExtractLastAnswerFromBlocks(blocks)
	if text == "" {
		return
	}
	cards := extractSummaryCards(blocks)
	if err := SaveSummaryWithCards(targetType, targetID, text, cards); err != nil {
		slog.Warn(
			"failed to save simple summary",
			slog.String("target_type", targetType),
			slog.Int64("target_id", targetID),
			slog.String("err", err.Error()),
		)
		return
	}
	broadcastSummaryUpdate(targetType, targetID, text, cards, projectPath, sessionID)
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
