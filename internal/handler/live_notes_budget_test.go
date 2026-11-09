package handler

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A short recording must keep the old behavior, and a long one must buy more
// time in proportion — the whole point of deriving the budget.
func TestLiveNoteDurationBudget_ScalesWithRecordingLength(t *testing.T) {
	cases := []struct {
		name       string
		durationMS int64
		want       time.Duration
	}{
		{"unknown", 0, 30 * time.Minute},
		{"negative", -1, 30 * time.Minute},
		{"ten minutes", 10 * 60 * 1000, 30 * time.Minute},
		{"just under the floor", 59 * 60 * 1000, 30 * time.Minute},
		{"one hour hits the floor exactly", 60 * 60 * 1000, 30 * time.Minute},
		{"two hours", 2 * 60 * 60 * 1000, 60 * time.Minute},
		{"three hours", 3 * 60 * 60 * 1000, 90 * time.Minute},
		{"at the cap", 8 * 60 * 60 * 1000, 4 * time.Hour},
		{"past the cap", 20 * 60 * 60 * 1000, 4 * time.Hour},
		{"overflowing duration", math.MaxInt64, 4 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, liveNoteDurationBudget(tc.durationMS))
		})
	}
}

// The chain deadline has to sit above every step's own deadline. If it did not,
// a step that would have finished would be killed by the chain instead and the
// failure would name the wrong stage.
func TestLiveNoteChainBudget_CoversEveryStep(t *testing.T) {
	for _, durationMS := range []int64{0, 10 * 60 * 1000, 3 * 60 * 60 * 1000, 8 * 60 * 60 * 1000} {
		steps := liveNoteDurationBudget(durationMS) + 2*liveNoteModelTimeout
		assert.Greater(t, liveNoteChainBudget(durationMS), steps, "duration %d", durationMS)
	}
	// A short meeting keeps the historical 60 minutes.
	assert.Equal(t, 60*time.Minute, liveNoteChainBudget(0))
}

func TestArtifactStepTimeout_OnlyRecognitionScales(t *testing.T) {
	long := int64(3 * 60 * 60 * 1000)
	assert.Equal(t, 90*time.Minute, artifactStepTimeout(service.ArtifactTranscript, long))
	assert.Equal(t, liveNoteModelTimeout, artifactStepTimeout(service.ArtifactRevised, long))
	assert.Equal(t, liveNoteModelTimeout, artifactStepTimeout(service.ArtifactMinutes, long))
	// An unknown length is still covered.
	assert.Equal(t, 30*time.Minute, artifactStepTimeout(service.ArtifactTranscript, 0))
}

// The chain sizes its budget from the total audio, not the longest channel:
// every channel is transcribed in full, so two overlapping speakers are twice
// the recognition work.
func TestArtifactChain_AudioBudgetSumsEveryChannel(t *testing.T) {
	token, pid, project := chainFixture(t)
	require.NoError(t, service.RecordLiveNoteAudioChannels(token, []service.LiveNoteAudioChannel{
		{Channel: "mic", File: "mic.webm", BaseAt: 1_700_000_000_000, DurationMS: 3 * 60 * 60 * 1000},
		{Channel: "system", File: "system.webm", BaseAt: 1_700_000_000_000, DurationMS: 40 * 60 * 1000},
	}))

	chain := newArtifactChain(token, pid, project, chainFixtureTitle, false)
	chain.prepare()

	assert.Equal(t, int64(3*60*60*1000+40*60*1000), chain.audioMS)
	assert.Equal(t, 110*time.Minute, artifactStepTimeout(service.ArtifactTranscript, chain.audioMS))
}

// Without merged audio the length is unknown; the budget must fall back to the
// floor rather than to zero (which would expire the step immediately).
func TestArtifactChain_AudioBudgetIsZeroWithoutChannels(t *testing.T) {
	token, pid, project := chainFixture(t)

	chain := newArtifactChain(token, pid, project, chainFixtureTitle, false)
	chain.prepare()

	assert.Zero(t, chain.audioMS)
	assert.Equal(t, 30*time.Minute, artifactStepTimeout(service.ArtifactTranscript, chain.audioMS))
}

// The import's conversion budget is what actually bounds a dropped three-hour
// video, so it must be derived from the probed source length.
func TestRunLiveNoteImport_ConversionBudgetScalesWithSource(t *testing.T) {
	setupLiveNotesDB(t)
	project := t.TempDir()
	pid := cacheProjectID(t, project)
	startedAt := int64(1_700_000_000_000)
	token := "importbudget00000000000000000001"
	paths, err := service.EnsureMeetingDir(token, pid, project, "导入", startedAt)
	require.NoError(t, err)

	// ffprobe answers "three hours" for the duration query; every other probe
	// (the video-stream check behind frame extraction) says nothing, which the
	// caller reads as "no video stream" and skips.
	stubLookPath(t, func(name string) (string, error) { return "/usr/bin/" + name, nil })
	prevRun := runMediaCommand
	runMediaCommand = func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		for _, arg := range args {
			if arg == "format=duration" {
				return []byte("10800.0\n"), nil, nil
			}
		}
		return nil, nil, nil
	}
	t.Cleanup(func() { runMediaCommand = prevRun })

	var budget time.Duration
	stubTranscode(t, func(ctx context.Context, _, dst string) (int64, error) {
		if deadline, ok := ctx.Deadline(); ok {
			budget = time.Until(deadline)
		}
		require.NoError(t, os.WriteFile(dst, testWebM(0, 25_000), 0o644))
		return 3 * 60 * 60 * 1000, nil
	})

	src := t.TempDir() + "/source.mp4"
	require.NoError(t, os.WriteFile(src, []byte("video"), 0o644))
	runLiveNoteImport(token, pid, project, "导入", startedAt, paths, src, "", nil)

	// Half of three hours, with a minute of slack for the probe and scheduling.
	assert.Greater(t, budget, 89*time.Minute)
	assert.Less(t, budget, 91*time.Minute)
}
