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
			if err := SaveSummary(targetType, targetID, ""); err != nil {
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
			slog.Warn(
				"summarization failed",
				slog.String("target_type", targetType),
				slog.Int64("target_id", targetID),
				slog.String("err", err.Error()),
			)
			return // summary stays non-existent, frontend shows original
		}

		if err := SaveSummary(targetType, targetID, summary); err != nil {
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
		mgr := ws.GetManager()
		if mgr != nil {
			mgr.BroadcastEvent(ws.ServerMessage{
				Type:  ws.MessageTypeEvent,
				ID:    ws.GenerateEventID(),
				Event: "summary_update",
				Data: ws.SummaryUpdateData{
					TargetType:  targetType,
					TargetID:    targetID,
					Summary:     summary,
					ProjectPath: projectPath,
					SessionID:   sessionID,
				},
			})
		}
	}()
}
