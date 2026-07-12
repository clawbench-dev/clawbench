//nolint:noctx // background goroutine, context from Start()
package dingtalk

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

const (
	// outboxPollInterval is how often the consumer checks for pending messages.
	outboxPollInterval = 3 * time.Second
	// outboxBatchSize is the max messages fetched per poll cycle.
	outboxBatchSize = 20
)

// consumeOutbox is the background goroutine that sends pending outbox messages.
func (m *Manager) consumeOutbox(ctx context.Context) {
	defer func() {
		select {
		case m.done <- struct{}{}:
		default:
		}
	}()

	ticker := time.NewTicker(outboxPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.processOutboxBatch(ctx)
		}
	}
}

// processOutboxBatch sends one batch of pending outbox messages.
func (m *Manager) processOutboxBatch(ctx context.Context) {
	if db == nil {
		return
	}
	msgs, err := db.GetPendingMessages(outboxBatchSize)
	if err != nil {
		slog.Warn("dingtalk: outbox query failed", "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	for _, msg := range msgs {
		// Parse msg_param JSON to extract title and markdown text
		var msgParam struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal([]byte(msg.MsgParam), &msgParam); err != nil {
			slog.Warn("dingtalk: outbox parse failed", "error", err, "id", msg.ID)
			_ = db.MarkMessageFailed(msg.ID, msg.MaxRetries)
			continue
		}

		// Send the message
		sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := m.SendMarkdownMessage(sendCtx, msg.UserID, msgParam.Title, msgParam.Text)
		cancel()

		if err != nil {
			slog.Warn("dingtalk: send failed", "error", err, "id", msg.ID, "user_id", msg.UserID, "retry", msg.RetryCount)
			_ = db.MarkMessageFailed(msg.ID, msg.MaxRetries)
		} else {
			_ = db.MarkMessageSent(msg.ID)
		}
	}
}
