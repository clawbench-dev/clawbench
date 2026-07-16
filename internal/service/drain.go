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

	// PersistUser persists the drained user message to DB.
	// Returns the message ID and any error.
	PersistUser func(text string, files []model.FileEntry) (int64, error)

	// ExecuteRunWithMessage runs one AI stream execution for the given queued message.
	// The drain loop calls this after persisting the user message and sending
	// the queue_drain event.
	ExecuteRunWithMessage func(qMsg model.QueuedMessage) DrainResult

	// MarkDoneAndSendFinal sends the terminal event (done/cancelled/error).
	MarkDoneAndSendFinal func(event ai.StreamEvent)
}

// emitDrainEvent emits a stream event to WS clients via StreamHub.
func emitDrainEvent(sessionID string, event ai.StreamEvent) {
	if mgr := ws.GetManager(); mgr != nil {
		if hub := mgr.StreamHub(); hub != nil && hub.HasSubscribers(sessionID) {
			hub.Emit(sessionID, event)
		}
	}
}

// RunDrainLoop runs the complete drain loop after an initial stream execution.
// It checks terminal conditions, dequeues messages, and executes them.
// The loop continues until the queue is empty or a terminal condition is met.
func RunDrainLoop(cfg DrainConfig, result DrainResult) {
	for {
		// Check terminal conditions
		if result.CancelReason == "user" {
			// Collect queue IDs before clearing for queue_cancel event
			queue := GetQueue(cfg.SessionID)
			queueIDs := make([]string, 0, len(queue))
			for _, qm := range queue {
				if qm.QueueID != "" {
					queueIDs = append(queueIDs, qm.QueueID)
				}
			}
			ClearQueue(cfg.SessionID)

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

			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: "cancelled"})
			return
		}
		if result.Err != "" {
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: "error", Error: result.Err})
			return
		}
		if result.Empty {
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: "error", Error: "AI returned no content", Reason: ai.ReasonEmpty})
			return
		}
		if result.CancelReason != "" {
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: "cancelled"})
			return
		}

		// Normal completion — check queue for next message
		qMsg, ok := DequeueMessage(cfg.SessionID)
		if !ok {
			// Wait for enqueue signal instead of blind sleep
			ok = WaitForEnqueue(cfg.SessionID, 100*time.Millisecond)
			if ok {
				qMsg, ok = DequeueMessage(cfg.SessionID)
			}
		}
		if !ok {
			// Queue empty — truly done
			cfg.MarkDoneAndSendFinal(ai.StreamEvent{Type: "done"})
			return
		}

		// Queue has next message — drain it atomically
		slog.Info("draining queued message", slog.String("session", cfg.SessionID), slog.String("queueId", qMsg.QueueID), slog.String("text", qMsg.Text))

		// Persist user message to DB
		msgID, _ := cfg.PersistUser(qMsg.Text, qMsg.Files)

		// Emit queue_drain event to WS clients
		remainingQueue := GetQueue(cfg.SessionID)
		emitDrainEvent(cfg.SessionID, ai.StreamEvent{
			Type: "queue_drain",
			QueueEvent: &ai.QueueEventData{
				SessionID: cfg.SessionID,
				QueueID:   qMsg.QueueID,
				Text:      qMsg.Text,
				MessageID: msgID,
				FilePaths: qMsg.FilePaths,
				Files:     qMsg.Files,
				Queue:     remainingQueue,
			},
		})

		// Execute next stream run with the dequeued message
		result = cfg.ExecuteRunWithMessage(qMsg)
		// Loop continues
	}
}
