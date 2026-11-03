package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimArtifactJob_SingleJobPerMeeting(t *testing.T) {
	setupTestDBForLiveNotes(t)
	now := int64(1_700_000_000_000)

	ok, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactTranscript, ArtifactRevised, ArtifactMinutes}, now)
	require.NoError(t, err)
	require.True(t, ok)

	states, err := ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobRunning, states[ArtifactTranscript].JobStatus)
	assert.Equal(t, ArtifactJobQueued, states[ArtifactRevised].JobStatus)
	assert.Equal(t, ArtifactJobQueued, states[ArtifactMinutes].JobStatus)

	// A second device clicking while the first job is fresh must not start a
	// second job.
	ok, err = ClaimArtifactJob("tok", []ArtifactKind{ArtifactMinutes}, now+1_000)
	require.NoError(t, err)
	assert.False(t, ok)

	// A heartbeat older than the interruption window is a dead job: the claim
	// must be allowed again (the process that owned it is gone).
	ok, err = ClaimArtifactJob("tok", []ArtifactKind{ArtifactMinutes}, now+liveNotesGeneratingStaleMS+1)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestClaimArtifactJob_DeduplicatesPlan(t *testing.T) {
	setupTestDBForLiveNotes(t)
	ok, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactMinutes, ArtifactMinutes}, 0)
	require.NoError(t, err)
	require.True(t, ok)
	states, err := ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobRunning, states[ArtifactMinutes].JobStatus)
}

func TestSetArtifactSucceeded_AdvancesContentOnlyWhenChanged(t *testing.T) {
	setupTestDBForLiveNotes(t)
	now := int64(1_700_000_000_000)
	_, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactMinutes}, now)
	require.NoError(t, err)

	require.NoError(t, SetArtifactSucceeded("tok", ArtifactMinutes, 0, now+1_000))
	states, err := ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobIdle, states[ArtifactMinutes].JobStatus)
	assert.Zero(t, states[ArtifactMinutes].ContentAt, "identical content must not advance content_at")

	require.NoError(t, SetArtifactSucceeded("tok", ArtifactMinutes, now+2_000, now+2_000))
	states, err = ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, now+2_000, states[ArtifactMinutes].ContentAt)

	// A second identical success keeps the original timestamp.
	require.NoError(t, SetArtifactSucceeded("tok", ArtifactMinutes, 0, now+3_000))
	states, err = ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, now+2_000, states[ArtifactMinutes].ContentAt)
}

func TestSetArtifactFailed_KeepsContent(t *testing.T) {
	setupTestDBForLiveNotes(t)
	now := int64(1_700_000_000_000)
	_, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactMinutes}, now)
	require.NoError(t, err)
	require.NoError(t, SetArtifactSucceeded("tok", ArtifactMinutes, now, now))
	require.NoError(t, SetArtifactFailed("tok", ArtifactMinutes, ArtifactErrModel, "boom", now+1_000))

	states, err := ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobFailed, states[ArtifactMinutes].JobStatus)
	assert.Equal(t, ArtifactErrModel, states[ArtifactMinutes].ErrorCode)
	assert.Equal(t, now, states[ArtifactMinutes].ContentAt, "a failed attempt must not erase the old content time")
}

func TestHeartbeatArtifact_DoesNotResurrectSettledArtifact(t *testing.T) {
	setupTestDBForLiveNotes(t)
	now := int64(1_700_000_000_000)
	_, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactTranscript}, now)
	require.NoError(t, err)

	require.NoError(t, HeartbeatArtifact("tok", ArtifactTranscript, "12/90", now+1_000))
	states, err := ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, "12/90", states[ArtifactTranscript].Detail)
	assert.Equal(t, now+1_000, states[ArtifactTranscript].JobAt)

	require.NoError(t, SetArtifactSucceeded("tok", ArtifactTranscript, 0, now+2_000))
	require.NoError(t, HeartbeatArtifact("tok", ArtifactTranscript, "late", now+3_000))
	states, err = ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobIdle, states[ArtifactTranscript].JobStatus)
	assert.Empty(t, states[ArtifactTranscript].Detail, "a late heartbeat must not write progress onto a settled row")
}

func TestResetQueuedArtifacts_OnlyTouchesQueued(t *testing.T) {
	setupTestDBForLiveNotes(t)
	now := int64(1_700_000_000_000)
	_, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactTranscript, ArtifactRevised, ArtifactMinutes}, now)
	require.NoError(t, err)

	require.NoError(t, ResetQueuedArtifacts("tok", now+1_000))
	states, err := ArtifactStatesForToken("tok")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobRunning, states[ArtifactTranscript].JobStatus, "the running step is not queued")
	assert.Equal(t, ArtifactJobIdle, states[ArtifactRevised].JobStatus)
	assert.Equal(t, ArtifactJobIdle, states[ArtifactMinutes].JobStatus)
}

func TestResetStaleArtifactJobs_FlipsOnlyAbandonedRows(t *testing.T) {
	setupTestDBForLiveNotes(t)
	now := int64(1_700_000_000_000)
	_, err := ClaimArtifactJob("stale", []ArtifactKind{ArtifactMinutes}, now)
	require.NoError(t, err)
	_, err = ClaimArtifactJob("fresh", []ArtifactKind{ArtifactMinutes}, now)
	require.NoError(t, err)

	n, err := ResetStaleArtifactJobs(now + liveNotesGeneratingStaleMS + 1)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	stale, err := ArtifactStatesForToken("stale")
	require.NoError(t, err)
	assert.Equal(t, ArtifactJobFailed, stale[ArtifactMinutes].JobStatus)
	assert.Equal(t, ArtifactErrInterrupted, stale[ArtifactMinutes].ErrorCode)

	// A fresh heartbeat keeps its row running.
	_, err = ClaimArtifactJob("fresh2", []ArtifactKind{ArtifactMinutes}, now)
	require.NoError(t, err)
	n, err = ResetStaleArtifactJobs(now + 1_000)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestArtifactJobActive_IgnoresStaleHeartbeat(t *testing.T) {
	setupTestDBForLiveNotes(t)
	now := int64(1_700_000_000_000)
	_, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactMinutes}, now)
	require.NoError(t, err)

	active, err := ArtifactJobActive("tok", now+1_000)
	require.NoError(t, err)
	assert.True(t, active)

	active, err = ArtifactJobActive("tok", now+liveNotesGeneratingStaleMS+1)
	require.NoError(t, err)
	assert.False(t, active)
}

func TestNormalizeArtifactState(t *testing.T) {
	now := int64(1_700_000_000_000)
	running := ArtifactState{JobStatus: ArtifactJobRunning, JobAt: now - liveNotesGeneratingStaleMS - 1}
	got := NormalizeArtifactState(running, now)
	assert.Equal(t, ArtifactJobFailed, got.JobStatus)
	assert.Equal(t, ArtifactErrInterrupted, got.ErrorCode)

	fresh := ArtifactState{JobStatus: ArtifactJobRunning, JobAt: now}
	assert.Equal(t, ArtifactJobRunning, NormalizeArtifactState(fresh, now).JobStatus)

	idle := ArtifactState{JobStatus: ArtifactJobIdle, JobAt: now - liveNotesGeneratingStaleMS - 1}
	assert.Equal(t, ArtifactJobIdle, NormalizeArtifactState(idle, now).JobStatus)
}

func TestBuildMeetingArtifactViews_StaleAndGeneration(t *testing.T) {
	now := int64(1_700_000_000_000)
	flags := liveNoteArtifactState{hasTranscript: true, hasRevised: true, hasMinutes: true}
	states := map[ArtifactKind]ArtifactState{
		ArtifactTranscript: {Kind: ArtifactTranscript, JobStatus: ArtifactJobIdle, ContentAt: 200},
		ArtifactRevised:    {Kind: ArtifactRevised, JobStatus: ArtifactJobRunning, ContentAt: 100, JobAt: now, StartedAt: now, Detail: "3/8"},
		ArtifactMinutes:    {Kind: ArtifactMinutes, JobStatus: ArtifactJobQueued, ContentAt: 50, JobAt: now, StartedAt: now},
	}
	views, gen := BuildMeetingArtifactViews(flags, states, now)

	assert.True(t, views[ArtifactTranscript].HasContent)
	assert.False(t, views[ArtifactTranscript].Stale)
	assert.True(t, views[ArtifactRevised].Stale, "the transcript is newer than the revised draft")
	assert.True(t, views[ArtifactMinutes].Stale, "the revised draft is newer than the minutes")
	require.NotNil(t, gen)
	assert.Equal(t, ArtifactRevised, gen.Step)
	assert.Equal(t, []ArtifactKind{ArtifactMinutes}, gen.Remaining)
	assert.Equal(t, "3/8", gen.Detail)
	assert.Equal(t, now, gen.StartedAt)
}

func TestBuildMeetingArtifactViews_NoJob(t *testing.T) {
	views, gen := BuildMeetingArtifactViews(liveNoteArtifactState{}, defaultArtifactStates("tok"), 0)
	assert.Nil(t, gen)
	assert.Equal(t, ArtifactJobIdle, views[ArtifactMinutes].JobStatus)
	assert.False(t, views[ArtifactMinutes].HasContent)
}

func TestRecordLiveNoteAudioChannels(t *testing.T) {
	setupTestDBForLiveNotes(t)
	require.NoError(t, RecordLiveNoteAudioChannels("tok", []LiveNoteAudioChannel{
		{Channel: "mic", File: "mic.webm", BaseAt: 1_000, DurationMS: 60_000},
		{Channel: LiveNoteMixChannel, File: LiveNoteMeetingAudioFile, BaseAt: 900, DurationMS: 61_000},
		{Channel: "", File: "ignored.webm", BaseAt: 1},
	}))

	channels, err := ListLiveNoteAudioChannels("tok")
	require.NoError(t, err)
	require.Len(t, channels, 2)
	assert.Equal(t, "mic", channels[0].Channel, "the mix sorts last")
	assert.Equal(t, int64(1_000), channels[0].BaseAt)
	assert.Equal(t, LiveNoteMixChannel, channels[1].Channel)

	// A later merge may learn a better duration; the origin is updated with it.
	require.NoError(t, RecordLiveNoteAudioChannels("tok", []LiveNoteAudioChannel{
		{Channel: "mic", File: "mic.webm", BaseAt: 1_100, DurationMS: 61_000},
	}))
	channels, err = ListLiveNoteAudioChannels("tok")
	require.NoError(t, err)
	assert.Equal(t, int64(1_100), channels[0].BaseAt)
	assert.Equal(t, int64(61_000), channels[0].DurationMS)
}

func TestDeleteMeetingRecord_ClearsArtifactTables(t *testing.T) {
	setupTestDBForLiveNotes(t)
	require.NoError(t, StartLiveNote("tok", 7, "会议", 1_700_000_000_000))
	_, err := ClaimArtifactJob("tok", []ArtifactKind{ArtifactMinutes}, time.Now().UnixMilli())
	require.NoError(t, err)
	require.NoError(t, RecordLiveNoteAudioChannels("tok", []LiveNoteAudioChannel{
		{Channel: "mic", File: "mic.webm", BaseAt: 1_000},
	}))

	require.NoError(t, DeleteMeetingRecord(t.TempDir(), 7, "tok"))

	states, err := ArtifactStatesForToken("tok")
	require.NoError(t, err)
	for _, kind := range ArtifactKinds {
		assert.Equal(t, ArtifactJobIdle, states[kind].JobStatus)
		assert.Zero(t, states[kind].ContentAt)
	}
	channels, err := ListLiveNoteAudioChannels("tok")
	require.NoError(t, err)
	assert.Empty(t, channels)
}
