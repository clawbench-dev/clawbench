package service

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skippedCountsFor measures gaps against the session's real ordering, so the
// unit-level contract is worth pinning directly: the payload path only ever
// exercises the happy case, and the defensive branches (a selected message that
// is not in the full list) are otherwise unreachable from a real database.
func TestSkippedCountsFor(t *testing.T) {
	msg := func(id int64) model.ChatMessage { return model.ChatMessage{ID: id} }

	t.Run("empty inputs report nothing", func(t *testing.T) {
		assert.Empty(t, skippedCountsFor(nil, nil))
		assert.Empty(t, skippedCountsFor([]int64{1, 2}, nil))
		assert.Equal(t, []int{0}, skippedCountsFor(nil, []model.ChatMessage{msg(1)}))
	})

	t.Run("first entry never carries a gap", func(t *testing.T) {
		// The selection starts mid-conversation: whatever came before is not
		// "skipped", it is simply not part of the share.
		got := skippedCountsFor([]int64{7, 8, 9}, []model.ChatMessage{msg(9)})
		assert.Equal(t, []int{0}, got)
	})

	t.Run("counts the rows strictly between two selected messages", func(t *testing.T) {
		all := []int64{10, 11, 12, 13, 14, 15}
		got := skippedCountsFor(all, []model.ChatMessage{msg(10), msg(13), msg(15)})
		assert.Equal(t, []int{0, 2, 1}, got)
	})

	t.Run("adjacent messages report zero", func(t *testing.T) {
		all := []int64{10, 11, 12}
		got := skippedCountsFor(all, []model.ChatMessage{msg(10), msg(11), msg(12)})
		assert.Equal(t, []int{0, 0, 0}, got)
	})

	t.Run("a selected id missing from the full list is not a gap", func(t *testing.T) {
		// Only possible if the two queries raced a delete. Inventing a gap here
		// would draw a divider that claims content was omitted when it was not.
		all := []int64{10, 11, 12}
		got := skippedCountsFor(all, []model.ChatMessage{msg(10), msg(99), msg(12)})
		require.Len(t, got, 3)
		assert.Equal(t, 0, got[0])
		assert.Equal(t, 0, got[1], "an unknown message must not claim a gap")
		assert.Equal(t, 1, got[2], "the next known message still measures from the last known position")
	})

	// The unknown id must be SKIPPED, not treated as position 0: falling through
	// would reset the cursor backwards and invent a gap on the following known
	// message (here 11→12 are adjacent, so the correct answer is 0, not 1).
	t.Run("an unknown id does not rewind the cursor", func(t *testing.T) {
		all := []int64{10, 11, 12}
		got := skippedCountsFor(all, []model.ChatMessage{msg(11), msg(99), msg(12)})
		require.Len(t, got, 3)
		assert.Equal(t, []int{0, 0, 0}, got)
	})

	t.Run("a selection out of order cannot produce a negative count", func(t *testing.T) {
		all := []int64{10, 11, 12}
		got := skippedCountsFor(all, []model.ChatMessage{msg(12), msg(10)})
		assert.Equal(t, []int{0, 0}, got)
	})
}
