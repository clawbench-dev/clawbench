package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestShouldNewSessionFallback verifies the rule that a failed session recovery
// only falls back to a brand-new session when the session has NO conversation
// history at all (no user or assistant messages). If the session has any messages,
// rebuilding would cause amnesia, so we must NOT fall back — the error is
// surfaced so the user can retry, preserving the original session mapping.
func TestShouldNewSessionFallback(t *testing.T) {
	t.Run("no conversation yet falls back", func(t *testing.T) {
		assert.True(t, shouldNewSessionFallback(false), "session with no messages may fall back to a new session")
	})

	t.Run("existing conversation does not fall back", func(t *testing.T) {
		assert.False(t, shouldNewSessionFallback(true), "session with conversation history must not be rebuilt (amnesia)")
	})
}
