package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestShouldNewSessionFallback verifies the rule that a failed session recovery
// only falls back to a brand-new session when NO conversation has happened yet.
// If the session already has assistant history, rebuilding would lose it, so we
// must NOT fall back — the user retries to preserve the original session mapping.
func TestShouldNewSessionFallback(t *testing.T) {
	t.Run("no conversation yet falls back", func(t *testing.T) {
		assert.True(t, shouldNewSessionFallback(0), "brand-new session (0 assistant messages) may fall back to a new session")
	})

	t.Run("existing conversation does not fall back", func(t *testing.T) {
		assert.False(t, shouldNewSessionFallback(1), "session with assistant history must not be rebuilt (history loss)")
		assert.False(t, shouldNewSessionFallback(5), "session with multiple turns must not be rebuilt (history loss)")
	})
}
