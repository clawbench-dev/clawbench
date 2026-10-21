package ai

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// chunkIdentity
// ---------------------------------------------------------------------------

func TestChunkIdentity_RequiresMessageIDAndTimestamp(t *testing.T) {
	sid := "sid-1"
	base := map[string]any{
		metaKeyCodeBuddyMessageID: "m1",
		metaKeyCodeBuddyTimestamp: "1792590363737",
	}

	t.Run("both present", func(t *testing.T) {
		assert.NotEmpty(t, chunkIdentity(sid, base, "agent_message_chunk", "hi"))
	})

	// Upstream's own rule: no key without BOTH halves. A text-only key would
	// also match a model legitimately repeating a token, so it must not exist.
	t.Run("missing timestamp yields no key", func(t *testing.T) {
		assert.Empty(t, chunkIdentity(sid, map[string]any{
			metaKeyCodeBuddyMessageID: "m1",
		}, "agent_message_chunk", "hi"))
	})

	t.Run("missing messageId yields no key", func(t *testing.T) {
		assert.Empty(t, chunkIdentity(sid, map[string]any{
			metaKeyCodeBuddyTimestamp: "1",
		}, "agent_message_chunk", "hi"))
	})

	t.Run("empty meta yields no key", func(t *testing.T) {
		assert.Empty(t, chunkIdentity(sid, nil, "agent_message_chunk", "hi"))
	})
}

// Every component must participate, otherwise two genuinely different chunks
// could collide and the second would be dropped as a "duplicate".
func TestChunkIdentity_DistinguishesEveryComponent(t *testing.T) {
	base := map[string]any{
		metaKeyCodeBuddyMessageID:   "m1",
		metaKeyCodeBuddyTimestamp:   "t1",
		metaKeyCodeBuddyMemberEvent: "alpha",
	}
	want := chunkIdentity("sid", base, "agent_message_chunk", "hi")
	require.NotEmpty(t, want)

	otherSession := chunkIdentity("sid-2", base, "agent_message_chunk", "hi")
	otherVariant := chunkIdentity("sid", base, "agent_thought_chunk", "hi")
	otherText := chunkIdentity("sid", base, "agent_message_chunk", "ho")
	otherMember := chunkIdentity("sid", map[string]any{
		metaKeyCodeBuddyMessageID:   "m1",
		metaKeyCodeBuddyTimestamp:   "t1",
		metaKeyCodeBuddyMemberEvent: "beta",
	}, "agent_message_chunk", "hi")
	otherMsg := chunkIdentity("sid", map[string]any{
		metaKeyCodeBuddyMessageID:   "m2",
		metaKeyCodeBuddyTimestamp:   "t1",
		metaKeyCodeBuddyMemberEvent: "alpha",
	}, "agent_message_chunk", "hi")
	otherTS := chunkIdentity("sid", map[string]any{
		metaKeyCodeBuddyMessageID:   "m1",
		metaKeyCodeBuddyTimestamp:   "t2",
		metaKeyCodeBuddyMemberEvent: "alpha",
	}, "agent_message_chunk", "hi")

	for name, got := range map[string]string{
		"session": otherSession,
		"variant": otherVariant,
		"text":    otherText,
		"member":  otherMember,
		"msgId":   otherMsg,
		"time":    otherTS,
	} {
		assert.NotEqual(t, want, got, "%s must be part of the identity", name)
	}
}

// ---------------------------------------------------------------------------
// isDuplicateChunk
// ---------------------------------------------------------------------------

func TestIsDuplicateChunk_DropsExactRepeatOnly(t *testing.T) {
	conn := &ACPConn{}
	id := "sid\x00agent_message_chunk\x00m1\x00t1\x00alpha\x00hi"

	assert.False(t, conn.isDuplicateChunk(id), "first delivery must pass")
	assert.True(t, conn.isDuplicateChunk(id), "second identical delivery must be dropped")
	assert.True(t, conn.isDuplicateChunk(id), "third identical delivery must be dropped too")

	// A different chunk after a duplicate is still accepted — the guard must not
	// latch into "drop everything".
	assert.False(t, conn.isDuplicateChunk("sid\x00agent_message_chunk\x00m1\x00t1\x00alpha\x00ho"))
}

func TestIsDuplicateChunk_EmptyIdentityNeverDrops(t *testing.T) {
	conn := &ACPConn{}
	// Frames without the identity pair cannot be de-duplicated; dropping them
	// would lose content.
	assert.False(t, conn.isDuplicateChunk(""))
	assert.False(t, conn.isDuplicateChunk(""))
}

func TestIsDuplicateChunk_NilConnNeverDrops(t *testing.T) {
	var conn *ACPConn
	assert.False(t, conn.isDuplicateChunk("anything"))
}

func TestResetChunkDedup_AllowsTheSameChunkNextTurn(t *testing.T) {
	conn := &ACPConn{}
	id := "sid\x00agent_message_chunk\x00m1\x00t1\x00alpha\x00hi"
	require.False(t, conn.isDuplicateChunk(id))
	require.True(t, conn.isDuplicateChunk(id))

	conn.resetChunkDedup()
	assert.False(t, conn.isDuplicateChunk(id), "a new turn must accept the same identity again")
}

// A very long turn must not grow the set without bound. Clearing can only let an
// OLD duplicate through — never drop a fresh chunk.
func TestIsDuplicateChunk_BoundedSetStillAcceptsNewChunks(t *testing.T) {
	conn := &ACPConn{}
	for i := range maxDupChunkKeys + 10 {
		id := "sid\x00agent_message_chunk\x00m" + strconv.Itoa(i) + "\x00t\x00alpha\x00hi"
		assert.False(t, conn.isDuplicateChunk(id), "unique chunk %d must be accepted", i)
	}
	// Still functional after the reset that the cap triggers.
	assert.False(t, conn.isDuplicateChunk("sid\x00agent_message_chunk\x00fresh\x00t\x00alpha\x00hi"))
	assert.True(t, conn.isDuplicateChunk("sid\x00agent_message_chunk\x00fresh\x00t\x00alpha\x00hi"))
}

// ---------------------------------------------------------------------------
// backend scoping
// ---------------------------------------------------------------------------

func TestChunkDedupEnabled_OnlyCodeBuddy(t *testing.T) {
	assert.True(t, chunkDedupEnabled("codebuddy"))
	for _, backend := range []string{"claude", "codex", "kimi", "opencode", "hermes", "cursor", ""} {
		assert.False(t, chunkDedupEnabled(backend), "%s must not be de-duplicated", backend)
	}
}
