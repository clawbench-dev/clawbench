package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The share switch is the owner's opt-in for "a stranger may read this meeting
// after it ends". A fresh record must start closed, and the public resolver
// must honor the flag in both directions.
func TestMeetingShareFlag_DefaultsToOffAndRoundTrips(t *testing.T) {
	setupTestDBForLiveNotes(t)
	require.NoError(t, StartLiveNote("tok-share", 7, "周会", 1_700_000_000_000))

	enabled, err := MeetingShareEnabled("tok-share")
	require.NoError(t, err)
	assert.False(t, enabled, "a new meeting record must not be public")

	_, ok, err := PublicMeetingByTokenShared("tok-share")
	require.NoError(t, err)
	assert.False(t, ok, "an unpublished record must not resolve for a stranger")

	require.NoError(t, SetMeetingShareEnabled(7, "tok-share", true))
	enabled, err = MeetingShareEnabled("tok-share")
	require.NoError(t, err)
	assert.True(t, enabled)

	_, ok, err = PublicMeetingByTokenShared("tok-share")
	require.NoError(t, err)
	assert.True(t, ok, "a published record must resolve")

	require.NoError(t, SetMeetingShareEnabled(7, "tok-share", false))
	_, ok, err = PublicMeetingByTokenShared("tok-share")
	require.NoError(t, err)
	assert.False(t, ok, "turning the switch off must close the record again")
}

// Unknown and blank tokens answer "not shared" rather than an error: the public
// endpoints must degrade to a 404, never a 500.
func TestMeetingShareEnabled_UnknownAndBlankTokens(t *testing.T) {
	setupTestDBForLiveNotes(t)
	for _, token := range []string{"", "   ", "nope"} {
		enabled, err := MeetingShareEnabled(token)
		assert.NoError(t, err, "token %q", token)
		assert.False(t, enabled, "token %q", token)
	}
}

func TestSetMeetingShareEnabled_UnknownOrForeignRecord(t *testing.T) {
	setupTestDBForLiveNotes(t)
	require.NoError(t, StartLiveNote("tok-own", 7, "周会", 1))

	assert.ErrorIs(t, SetMeetingShareEnabled(7, "", true), ErrMeetingNotFound)
	assert.ErrorIs(t, SetMeetingShareEnabled(7, "nope", true), ErrMeetingNotFound)
	assert.ErrorIs(t, SetMeetingShareEnabled(8, "tok-own", true), ErrMeetingNotFound,
		"another project must not be able to publish this record")

	// The failed attempts must not have flipped anything.
	enabled, err := MeetingShareEnabled("tok-own")
	require.NoError(t, err)
	assert.False(t, enabled)
}

// A hidden record is reported exactly like a missing one — the owner deleted
// it, so there is nothing to publish and nothing to reveal.
func TestMeetingShare_HiddenRecordIsInvisible(t *testing.T) {
	db := setupTestDBForLiveNotes(t)
	require.NoError(t, StartLiveNote("tok-hidden", 7, "周会", 1))
	_, err := db.Exec(`UPDATE live_notes SET hidden = 1 WHERE token = ?`, "tok-hidden")
	require.NoError(t, err)

	assert.ErrorIs(t, SetMeetingShareEnabled(7, "tok-hidden", true), ErrMeetingNotFound)

	_, ok, err := PublicMeetingByToken("tok-hidden")
	require.NoError(t, err)
	assert.False(t, ok, "a hidden record must not resolve")

	_, ok, err = PublicMeetingByTokenShared("tok-hidden")
	require.NoError(t, err)
	assert.False(t, ok)
}

// PublicMeetingByTokenShared must pass a missing record straight through: the
// caller's 404 is decided by "not found", not by the share switch.
func TestPublicMeetingByTokenShared_UnknownToken(t *testing.T) {
	setupTestDBForLiveNotes(t)
	_, ok, err := PublicMeetingByTokenShared("nope")
	require.NoError(t, err)
	assert.False(t, ok)
}
