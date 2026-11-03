package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func fullRegenState() RegenState {
	return RegenState{
		HasSegments:          true,
		HasAudio:             true,
		AudioBaseKnown:       true,
		TranscriptHasContent: true,
		RevisedHasContent:    true,
		MinutesHasContent:    true,
	}
}

func TestPlanRegeneration(t *testing.T) {
	cases := []struct {
		name       string
		target     ArtifactKind
		downstream bool
		mutate     func(*RegenState)
		want       []RegenStep
		warnings   []string
		blocked    string
	}{
		{
			name:   "minutes when everything is fresh",
			target: ArtifactMinutes,
			want:   []RegenStep{{ArtifactMinutes, RegenModeModel, RegenReasonTarget}},
		},
		{
			name:   "minutes with a missing proofread",
			target: ArtifactMinutes,
			mutate: func(s *RegenState) { s.RevisedHasContent = false },
			want: []RegenStep{
				{ArtifactRevised, RegenModeModel, RegenReasonMissing},
				{ArtifactMinutes, RegenModeModel, RegenReasonTarget},
			},
		},
		{
			name:   "minutes with a stale proofread",
			target: ArtifactMinutes,
			mutate: func(s *RegenState) { s.RevisedStale = true },
			want: []RegenStep{
				{ArtifactRevised, RegenModeModel, RegenReasonStale},
				{ArtifactMinutes, RegenModeModel, RegenReasonTarget},
			},
		},
		{
			name:   "minutes with a missing transcript",
			target: ArtifactMinutes,
			mutate: func(s *RegenState) {
				s.TranscriptHasContent = false
				s.RevisedHasContent = false
			},
			want: []RegenStep{
				{ArtifactTranscript, RegenModeRender, RegenReasonMissing},
				{ArtifactRevised, RegenModeModel, RegenReasonMissing},
				{ArtifactMinutes, RegenModeModel, RegenReasonTarget},
			},
		},
		{
			name:   "minutes with no segments recovers through recognition",
			target: ArtifactMinutes,
			mutate: func(s *RegenState) {
				s.HasSegments = false
				s.TranscriptHasContent = false
				s.RevisedHasContent = false
			},
			want: []RegenStep{
				{ArtifactTranscript, RegenModeASR, RegenReasonMissing},
				{ArtifactRevised, RegenModeModel, RegenReasonMissing},
				{ArtifactMinutes, RegenModeModel, RegenReasonTarget},
			},
		},
		{
			name:   "minutes need no transcript when the proofread is usable",
			target: ArtifactMinutes,
			mutate: func(s *RegenState) {
				s.HasSegments = false
				s.TranscriptHasContent = false
			},
			want: []RegenStep{{ArtifactMinutes, RegenModeModel, RegenReasonTarget}},
		},
		{
			name:   "revised with a missing transcript",
			target: ArtifactRevised,
			mutate: func(s *RegenState) { s.TranscriptHasContent = false },
			want: []RegenStep{
				{ArtifactTranscript, RegenModeRender, RegenReasonMissing},
				{ArtifactRevised, RegenModeModel, RegenReasonTarget},
			},
		},
		{
			name:   "revised recovers segments through recognition",
			target: ArtifactRevised,
			mutate: func(s *RegenState) { s.HasSegments = false },
			want: []RegenStep{
				{ArtifactTranscript, RegenModeASR, RegenReasonMissing},
				{ArtifactRevised, RegenModeModel, RegenReasonTarget},
			},
			warnings: []string{RegenWarnOverwriteTranscript},
		},
		{
			name:     "transcript re-recognizes the recording",
			target:   ArtifactTranscript,
			want:     []RegenStep{{ArtifactTranscript, RegenModeASR, RegenReasonTarget}},
			warnings: []string{RegenWarnOverwriteTranscript},
		},
		{
			name:     "transcript falls back to a rebuild without audio",
			target:   ArtifactTranscript,
			mutate:   func(s *RegenState) { s.HasAudio = false },
			want:     []RegenStep{{ArtifactTranscript, RegenModeRender, RegenReasonTarget}},
			warnings: []string{RegenWarnAudioUnavailable, RegenWarnOverwriteTranscript},
		},
		{
			name:   "transcript recognizes when only the recording is left",
			target: ArtifactTranscript,
			mutate: func(s *RegenState) {
				s.HasSegments = false
				s.TranscriptHasContent = false
			},
			want: []RegenStep{{ArtifactTranscript, RegenModeASR, RegenReasonTarget}},
		},
		{
			name:   "transcript is blocked without any source",
			target: ArtifactTranscript,
			mutate: func(s *RegenState) {
				s.HasSegments = false
				s.HasAudio = false
				s.TranscriptHasContent = false
			},
			blocked: ArtifactErrNoSource,
		},
		{
			name:   "pending recognition only needs the render",
			target: ArtifactTranscript,
			mutate: func(s *RegenState) { s.TranscriptPendingASR = true },
			want:   []RegenStep{{ArtifactTranscript, RegenModeRender, RegenReasonPending}},
			warnings: []string{
				RegenWarnOverwriteTranscript,
			},
		},
		{
			name:       "transcript with downstream",
			target:     ArtifactTranscript,
			downstream: true,
			want: []RegenStep{
				{ArtifactTranscript, RegenModeASR, RegenReasonTarget},
				{ArtifactRevised, RegenModeModel, RegenReasonDownstream},
				{ArtifactMinutes, RegenModeModel, RegenReasonDownstream},
			},
			warnings: []string{RegenWarnOverwriteTranscript},
		},
		{
			name:       "revised with downstream",
			target:     ArtifactRevised,
			downstream: true,
			want: []RegenStep{
				{ArtifactRevised, RegenModeModel, RegenReasonTarget},
				{ArtifactMinutes, RegenModeModel, RegenReasonDownstream},
			},
		},
		{
			name:   "incomplete audio downgrades recognition to a rebuild",
			target: ArtifactTranscript,
			mutate: func(s *RegenState) { s.AudioIncomplete = true },
			want:   []RegenStep{{ArtifactTranscript, RegenModeRender, RegenReasonTarget}},
			warnings: []string{
				RegenWarnAudioIncomplete,
				RegenWarnOverwriteTranscript,
			},
		},
		{
			name:   "mixed-only audio is flagged",
			target: ArtifactTranscript,
			mutate: func(s *RegenState) { s.MixedOnly = true },
			want:   []RegenStep{{ArtifactTranscript, RegenModeASR, RegenReasonTarget}},
			warnings: []string{
				RegenWarnMixedAudioOnly,
				RegenWarnOverwriteTranscript,
			},
		},
		{
			name:   "unknown audio origin is flagged",
			target: ArtifactTranscript,
			mutate: func(s *RegenState) { s.AudioBaseKnown = false },
			want:   []RegenStep{{ArtifactTranscript, RegenModeASR, RegenReasonTarget}},
			warnings: []string{
				RegenWarnAudioBaseEstimated,
				RegenWarnOverwriteTranscript,
			},
		},
		{
			name:    "unknown target is blocked",
			target:  ArtifactKind("nope"),
			want:    nil,
			blocked: ArtifactErrNoSource,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := fullRegenState()
			if tc.mutate != nil {
				tc.mutate(&st)
			}
			plan := PlanRegeneration(tc.target, tc.downstream, st)
			assert.Equal(t, tc.want, plan.Steps)
			assert.Equal(t, tc.warnings, plan.Warnings)
			if tc.blocked == "" {
				assert.Nil(t, plan.Blocked)
			} else if assert.NotNil(t, plan.Blocked) {
				assert.Equal(t, tc.blocked, plan.Blocked.Code)
			}
		})
	}
}

func TestPlanForInitialJob(t *testing.T) {
	steps := PlanForInitialJob()
	assert.Equal(t, []RegenStep{
		{ArtifactTranscript, RegenModeRender, RegenReasonTarget},
		{ArtifactRevised, RegenModeModel, RegenReasonDownstream},
		{ArtifactMinutes, RegenModeModel, RegenReasonTarget},
	}, steps)
}
