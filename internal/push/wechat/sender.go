package wechat

import (
	"context"
	"errors"
	"fmt"
)

// ErrNotStarted is returned when a send is attempted while the monitor is not
// running. Callers treat it as "channel unavailable", not as a bug.
var ErrNotStarted = errors.New("wechat: push manager is not running")

// SendText sends one plain-text message to a user.
//
// The context token is passed explicitly rather than going through
// ilink.Client.Push, which reads the SDK's process-local token cache. That cache
// is empty after a restart and only refilled by inbound traffic, so using Push
// would silently disable every outbound notification until each user happened
// to message the bot again. Our token lives in the DB instead.
func (m *Manager) SendText(ctx context.Context, to, text, contextToken string) error {
	if to == "" {
		return fmt.Errorf("wechat: send text: empty user id")
	}
	if contextToken == "" {
		// The platform rejects this, so fail before spending a round trip.
		return fmt.Errorf("wechat: send text: no context token for %s", to)
	}
	if text == "" {
		return fmt.Errorf("wechat: send text: empty body")
	}

	m.startMu.Lock()
	client := m.client
	started := m.started
	m.startMu.Unlock()

	if !started || client == nil {
		return ErrNotStarted
	}

	if _, err := client.SendText(ctx, to, text, contextToken); err != nil {
		return fmt.Errorf("wechat: send text: %w", err)
	}
	return nil
}
