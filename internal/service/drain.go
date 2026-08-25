package service

import (
	"log/slog"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/ws"
)

// DrainResult is a generic result type for the drain loop.
// Both handler/chat.go streamRunResult and service/session_command.go streamRunResultShared
// map to this type.
type DrainResult struct {
	CancelReason string
	Err          string
	Empty        bool
}

// DrainConfig holds the parameters for the drain loop.
type DrainConfig struct {
	SessionID   string
	ProjectPath string
	BackendName string

	// ExecuteRunWithMessage runs one AI stream execution for the given queued message.
	// The drain loop calls this after dequeuing the message from DB and sending
	// the queue_drain event. The message is a ChatMessage with queued=0 (already
	// claimed) and QueueID preserved.
	ExecuteRunWithMessage func(msg model.ChatMessage) DrainResult

	// MarkDoneAndSendFinal sends the terminal event (done/cancelled/error).
	MarkDoneAndSendFinal func(event ai.StreamEvent)
}

// emitDrainEvent emits a stream event to WS clients via StreamHub.
func emitDrainEvent(sessionID string, event ai.StreamEvent) {
	ws.EmitToSession(sessionID, event)
}

// RunDrainLoop runs the complete drain loop after an initial stream execution.
// It checks terminal conditions, dequeues messages from chat_history (queued=1),
// and executes them. The loop continues until the queue is empty or a terminal
// condition is met.
func RunDrainLoop(cfg DrainConfig, result DrainResult) {
	for {
		// Check terminal conditions
		if result.CancelReason == cancelReasonUser {
			// Collect queue IDs before clearing for queue_cancel event
			queueIDs, _ := GetQueuedQueueIDs(cfg.SessionID)
			_ = ClearQueuedMessages(cfg.SessionID)

			// Emit queue_cancel so frontend can immediately remove pending messages
			if len(queueIDs) > 0 {
				emitDrainEvent(cfg.SessionID, ai.StreamEvent{
					Type: "queue_cancel",
					QueueEvent: &ai.QueueEventData{
						SessionID: cfg.SessionID,
						QueueIDs:  queueIDs,
					},
				})
			}

			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: statusCancelled})
			return
		}
		if result.Err != "" {
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: eventTypeError, Error: result.Err})
			return
		}
		if result.Empty {
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: eventTypeError, Error: "AI returned no content", Reason: ai.ReasonEmpty})
			return
		}
		if result.CancelReason != "" {
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: statusCancelled})
			return
		}

		// Normal completion — check DB queue for next message
		msg, ok, err := DequeueQueuedMessage(cfg.SessionID)
		if err != nil {
			// Real DB error — don't exit as if empty (would silently lose the
			// message). Brief retry window, then check queue again.
			slog.Error("drain: dequeue failed", slog.String("session", cfg.SessionID), slog.String("error", err.Error()))
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if !ok {
			// Wait for enqueue signal instead of blind sleep
			ok = WaitForEnqueue(cfg.SessionID, 100*time.Millisecond)
			if ok {
				msg, ok, err = DequeueQueuedMessage(cfg.SessionID)
				if err != nil {
					slog.Error("drain: dequeue failed", slog.String("session", cfg.SessionID), slog.String("error", err.Error()))
					time.Sleep(100 * time.Millisecond)
					continue
				}
			}
		}
		if !ok {
			// Queue empty — truly done
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: eventTypeDone})
			return
		}

		// Queue has next message — drain it (row already persisted with queued=0)
		slog.Info("drain: draining queued message",
			slog.String("session", cfg.SessionID),
			slog.String("queueId", msg.QueueID),
			slog.Int64("msgId", msg.ID),
			slog.String("text", msg.Content))

		// Emit queue_drain event to WS clients
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{
			Type: "queue_drain",
			QueueEvent: &ai.QueueEventData{
				SessionID: cfg.SessionID,
				QueueID:   msg.QueueID,
				Text:      msg.Content,
				MessageID: msg.ID,
				FilePaths: filePathsFromFiles(msg.Files),
				Files:     msg.Files,
			},
		})
		slog.Info("drain: emitted queue_drain",
			slog.String("session", cfg.SessionID),
			slog.String("queueId", msg.QueueID),
			slog.Int64("msgId", msg.ID))

		// Execute next stream run with the dequeued message
		result = cfg.ExecuteRunWithMessage(msg)
		// Loop continues
	}
}

// filePathsFromFiles extracts the file paths from FileEntry list for the
// queue_drain event payload (the frontend drains by queueId; filePaths keep
// the legacy field populated).
func filePathsFromFiles(files []model.FileEntry) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}
