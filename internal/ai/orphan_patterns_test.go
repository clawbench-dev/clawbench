package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The registry is what replaces the old hard-coded three-row table. These tests
// pin the two behaviors that make it safe to be a registry at all: matching is
// driven by what backends registered, and registration cannot double up.

// TestOrphanCmdlinePattern_MatchesOnlyWhenBothHalvesPresent is the core
// contract: the binary alone must not match (a shell running `claude --version`
// is not our child) and the argument alone must not either (dozens of tools take
// `--acp`).
func TestOrphanCmdlinePattern_MatchesOnlyWhenBothHalvesPresent(t *testing.T) {
	resetOrphanPatterns(t)
	RegisterOrphanCmdline("probe-agent", "--acp")

	assert.True(t, hasOrphanCmdlinePattern([]byte("probe-agent\x00--acp\x00")),
		"both halves present is a match")
	assert.False(t, hasOrphanCmdlinePattern([]byte("probe-agent\x00--version\x00")),
		"the binary alone must not match")
	assert.False(t, hasOrphanCmdlinePattern([]byte("other-agent\x00--acp\x00")),
		"the argument alone must not match")
}

// An empty half would match every command line (strings.Contains(s, "") is
// true), turning orphan cleanup into "kill every process". Registration must
// ignore such a pattern rather than silently arming it.
func TestRegisterOrphanCmdline_IgnoresEmptyHalves(t *testing.T) {
	resetOrphanPatterns(t)
	RegisterOrphanCmdline("", "--acp")
	RegisterOrphanCmdline("probe-agent", "")

	assert.Empty(t, orphanCmdlinePatterns(), "a half-empty pattern must not be registered")
	assert.False(t, hasOrphanCmdlinePattern([]byte("anything\x00at\x00all\x00")),
		"an empty half must never make every process match")
}

// Re-registering the same shape (two builds importing the same plugin) must not
// append a duplicate: the list is walked on every startup scan.
func TestRegisterOrphanCmdline_IsIdempotent(t *testing.T) {
	resetOrphanPatterns(t)
	RegisterOrphanCmdline("probe-agent", "--acp")
	RegisterOrphanCmdline("probe-agent", "--acp")

	assert.Len(t, orphanCmdlinePatterns(), 1)
}

// The registry is a snapshot, not a live slice: a caller iterating while
// another registers must not observe a torn read.
func TestOrphanCmdlinePatterns_ReturnsACopy(t *testing.T) {
	resetOrphanPatterns(t)
	RegisterOrphanCmdline("probe-agent", "--acp")

	first := orphanCmdlinePatterns()
	RegisterOrphanCmdline("second-agent", "app-server")

	assert.Len(t, first, 1, "an earlier snapshot must not change underneath its holder")
	assert.Len(t, orphanCmdlinePatterns(), 2)
}

// resetOrphanPatterns clears the package registry and restores the caller's
// state afterwards, so these tests neither leak into each other nor erase the
// real registrations made by the backend plugins at init.
func resetOrphanPatterns(t *testing.T) {
	t.Helper()
	orphanPatternsMu.Lock()
	saved := orphanPatterns
	orphanPatterns = nil
	orphanPatternsMu.Unlock()
	t.Cleanup(func() {
		orphanPatternsMu.Lock()
		orphanPatterns = saved
		orphanPatternsMu.Unlock()
	})
}
