package service

import (
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// TestMailSyncWindowAndPrefetchReadTheLiveConfig pins the "read per sync, not at
// startup" rule for the two mail tuning values.
//
// A value captured at startup would make the settings page lie until the next
// restart. The zero cases differ on purpose: a zero window must fall back (a
// misconfiguration would otherwise cache nothing and the list would look empty),
// while a zero prefetch is a real choice for metered connections and must be
// honored.
func TestMailSyncWindowAndPrefetchReadTheLiveConfig(t *testing.T) {
	orig := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = orig })

	model.ConfigInstance.Mail.SyncWindow = 0
	require.Equal(t, mailDefaultSyncWindow, mailSyncWindow())

	model.ConfigInstance.Mail.SyncWindow = 250
	require.Equal(t, 250, mailSyncWindow())

	model.ConfigInstance.Mail.BodyPrefetch = 0
	require.Equal(t, 0, mailBodyPrefetchCount(), "0 means 'do not prefetch', not 'use the default'")

	model.ConfigInstance.Mail.BodyPrefetch = 7
	require.Equal(t, 7, mailBodyPrefetchCount())

	// A negative value cannot mean anything sensible and would make the loop
	// skip everything silently.
	model.ConfigInstance.Mail.BodyPrefetch = -3
	require.Equal(t, 0, mailBodyPrefetchCount())
}

// TestMailSyncWorkerEffectiveInterval: the configured interval wins, the field
// stays the fallback for an install whose config predates the key, and a
// non-positive value can never produce a busy loop (time.After(0) would tick
// continuously).
func TestMailSyncWorkerEffectiveInterval(t *testing.T) {
	orig := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = orig })

	model.ConfigInstance.Mail.SyncIntervalMinutes = 0
	w := &MailSyncWorker{interval: 90 * time.Second}
	require.Equal(t, 90*time.Second, w.effectiveInterval(), "the injected fallback is used when the config is unset")

	model.ConfigInstance.Mail.SyncIntervalMinutes = 5
	require.Equal(t, 5*time.Minute, w.effectiveInterval(), "the live setting wins")

	// Changing the setting takes effect on the next call, without a restart.
	model.ConfigInstance.Mail.SyncIntervalMinutes = 15
	require.Equal(t, 15*time.Minute, w.effectiveInterval())

	model.ConfigInstance.Mail.SyncIntervalMinutes = 0
	w.interval = 0
	require.Equal(t, mailSyncInterval, w.effectiveInterval(), "an unset interval falls back, never zero")

	w.interval = -time.Minute
	require.Equal(t, mailSyncInterval, w.effectiveInterval())
}
