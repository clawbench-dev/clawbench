package wechat

import (
	"context"
	"log/slog"

	ilink "github.com/openilink/openilink-sdk-go"
)

// runMonitor drives the long-poll loop until the context is cancelled or the
// session expires.
//
// The SDK's Monitor sleeps for a full hour after a -14 (session expired) before
// retrying, which would leave a silently dead bot for an hour. So the
// OnSessionExpired callback cancels this manager's own context: sleepCtx returns
// immediately, the loop exits, the expired state becomes visible in Status, and
// IsStarted goes false so Push* stops attempting sends with a dead token.
func (m *Manager) runMonitor(ctx context.Context, client *ilink.Client, gen uint64) {
	// The cursor is per bot: see syncBufPath.
	m.startMu.Lock()
	botID := m.cfg.BotID
	m.startMu.Unlock()

	err := client.Monitor(ctx,
		func(msg ilink.WeixinMessage) { m.onInbound(ctx, msg) },
		&ilink.MonitorOptions{
			InitialBuf: loadSyncBuf(botID),
			OnBufUpdate: func(buf string) {
				if saveErr := saveSyncBuf(botID, buf); saveErr != nil {
					slog.Warn("wechat: persist sync buf failed", "error", saveErr)
				}
			},
			OnError: func(e error) {
				slog.Warn("wechat: poll error", "error", e)
				m.setLastError(e)
			},
			OnSessionExpired: func() {
				slog.Warn("wechat: session expired (-14), stopping monitor for re-scan")
				m.markExpired(gen)
				m.cancelCurrent(gen)
			},
		})

	// A cancelled context is the normal shutdown path (Stop or -14), not a
	// failure worth recording.
	if ctx.Err() == nil && err != nil {
		slog.Warn("wechat: monitor exited", "error", err)
		m.setLastError(err)
	}

	m.retire(gen)
}
