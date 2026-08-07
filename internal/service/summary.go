package service

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/summarize"
	"clawbench/internal/ws"
)

// taskSummarizerInstance is the shared TaskSummarizer instance used by
// both chat message summarization and task execution summarization.
// Set via SetTaskSummarizerInstance() during server startup.
var taskSummarizerInstance *summarize.TaskSummarizer

// SetTaskSummarizerInstance sets the global TaskSummarizer instance
// used for async summarization of both chat messages and task executions.
func SetTaskSummarizerInstance(s *summarize.TaskSummarizer) {
	taskSummarizerInstance = s
}

// summarizeTarget is the single shared entry point for generating a reading
// summary for a chat message or task execution. Both interactive chat
// (triggerChatSummarization) and scheduled tasks (executeTask) route through
// this function so they follow the exact same strategy:
//
//   - "" / "disabled" → no summarization
//   - "simple" → extract the last answer text and save it directly (no AI, no threshold)
//   - "ai" → use the configured AI summarizer via AsyncSummarize, which falls
//     back to the extracted text on AI failure. If no AI summarizer is
//     configured, no summary is produced.
func summarizeTarget(targetType string, targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) { //nolint:unparam // targetType always "chat_message"; kept generic to mirror AsyncSummarize's signature
	if !chatSummaryEnabled.Load() {
		return
	}
	switch GetChatSummaryMode() {
	case "", "disabled":
		return
	case "simple":
		summarizeSimple(targetType, targetID, blocks, projectPath, sessionID)
	default: // "ai"
		if taskSummarizerInstance == nil {
			return
		}
		AsyncSummarize(targetType, targetID, blocks, projectPath, sessionID)
	}
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

// AsyncSummarize generates a reading summary asynchronously for a target
// (chat message or task execution). It runs in a goroutine with an
// independent context and 5-minute timeout.
//
// On completion, the summary is persisted via SaveSummary() and a
// summary_update WebSocket event is broadcast.
func AsyncSummarize(targetType string, targetID int64, blocks []model.ContentBlock, projectPath, sessionID string) {
	if taskSummarizerInstance == nil {
		return
	}

	go func() {
		sumCtx, sumCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer sumCancel()

		// Extract only the last substantive answer (the text after the
		// last tool_use, or the longest text block). This avoids
		// summarizing intermediate reasoning like "Let me check..."
		// when the real answer is just before a terminal tool_use.
		text := summarize.ExtractLastAnswerFromBlocks(blocks)
		if utf8.RuneCountInString(text) < summarize.ShortTextThreshold {
			// Text too short, mark as empty (frontend shows original)
			cards := extractSummaryCards(blocks)
			if err := SaveSummaryWithCards(targetType, targetID, "", cards); err != nil {
				slog.Warn(
					"failed to save summary (short text)",
					slog.String("target_type", targetType),
					slog.Int64("target_id", targetID),
					slog.String("err", err.Error()),
				)
			}
			return
		}

		summary, err := taskSummarizerInstance.Summarize(sumCtx, text, "")
		if err != nil {
			// Fallback: use the extracted text directly (same as simple mode).
			// text already contains ExtractLastAnswerFromBlocks result, which
			// is the last substantive answer — no truncation needed.
			slog.Warn(
				"summarization failed, using extracted text as summary",
				slog.String("target_type", targetType),
				slog.Int64("target_id", targetID),
				slog.String("err", err.Error()),
			)
			summary = text
		}

		cards := extractSummaryCards(blocks)
		if err := SaveSummaryWithCards(targetType, targetID, summary, cards); err != nil {
			slog.Warn(
				"failed to save summary",
				slog.String("target_type", targetType),
				slog.Int64("target_id", targetID),
				slog.String("err", err.Error()),
			)
		}

		slog.Info(
			"summarization completed",
			slog.String("target_type", targetType),
			slog.Int64("target_id", targetID),
			slog.Int("summary_len", utf8.RuneCountInString(summary)),
		)

		// Broadcast summary_update via WebSocket
		broadcastSummaryUpdate(targetType, targetID, summary, cards, projectPath, sessionID)
	}()
}
