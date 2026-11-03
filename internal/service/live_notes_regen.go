package service

import "fmt"

// Regeneration modes. `asr` re-runs speech recognition on the recording;
// `render` rebuilds the VTT from the segments that already exist; `model` is
// the proofread/summarize path.
const (
	RegenModeASR    = "asr"
	RegenModeRender = "render"
	RegenModeModel  = "model"
)

// Why a step is in the plan. The SPA turns these into one short sentence, so
// the user can see that "regenerate minutes" will really run three stages.
const (
	RegenReasonMissing    = "missing"
	RegenReasonStale      = "stale"
	RegenReasonPending    = "pending"
	RegenReasonTarget     = "target"
	RegenReasonDownstream = "downstream"
)

// Plan warnings. Strings, not booleans: they travel to the SPA verbatim and are
// localized there.
const (
	RegenWarnOverwriteTranscript = "overwrite_transcript"
	RegenWarnAudioUnavailable    = "audio_unavailable"
	RegenWarnAudioIncomplete     = "audio_incomplete"
	RegenWarnMixedAudioOnly      = "mixed_audio_only"
	RegenWarnAudioBaseEstimated  = "audio_base_estimated"
)

// RegenStep is one stage of an accepted plan.
type RegenStep struct {
	Kind   ArtifactKind `json:"kind"`
	Mode   string       `json:"mode"`
	Reason string       `json:"reason"`
}

// RegenBlock explains why a request cannot run at all.
type RegenBlock struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RegenPlan is what the confirmation sheet renders and what the executor runs.
type RegenPlan struct {
	Target   ArtifactKind `json:"target"`
	Steps    []RegenStep  `json:"steps"`
	Warnings []string     `json:"warnings"`
	Blocked  *RegenBlock  `json:"blocked"`
}

// RegenState is the pure input of PlanRegeneration.
//
// The handler computes the booleans from the database and the meeting folder;
// keeping them here (instead of passing the record) makes every branch testable
// without a database or a filesystem.
type RegenState struct {
	// Sources.
	HasSegments bool // live_notes_segments has at least one row
	HasAudio    bool // any recording (per-channel or the mix)
	// AudioIncomplete: a channel with segments has no usable audio file, so a
	// full re-recognition would silently drop that speaker.
	AudioIncomplete bool
	// MixedOnly: only audio/meeting.webm exists (a solo meeting's channel was
	// folded into the mix), so speakers cannot be separated.
	MixedOnly bool
	// AudioBaseKnown: live_notes_audio has the absolute origin of every file
	// used for recognition. False on old meetings; timestamps may shift.
	AudioBaseKnown bool

	TranscriptHasContent bool
	TranscriptPendingASR bool // ASR finished, VTT write did not
	RevisedHasContent    bool
	RevisedStale         bool
	MinutesHasContent    bool
	MinutesStale         bool
}

// PlanRegeneration decides what has to run to regenerate `target`.
//
// The rules, in order:
//
//  1. every upstream level that is missing or stale is added first;
//  2. the target is always added;
//  3. with includeDownstream, the levels below the target are added too;
//  4. the transcript step is `asr` only when the user targeted it (or when a
//     render has no segments but audio exists); cascades default to `render`;
//  5. if a step has no source at all, the plan is blocked instead of failing
//     after the user waits for it.
func PlanRegeneration(target ArtifactKind, includeDownstream bool, st RegenState) RegenPlan {
	if !target.Valid() {
		return RegenPlan{
			Target:  target,
			Blocked: &RegenBlock{Code: ArtifactErrNoSource, Message: "unknown artifact kind"},
		}
	}
	p := &regenPlanner{
		plan: RegenPlan{Target: target},
		st:   st,
		// A full re-recognition is only possible when every channel that has
		// transcript text also has audio. Otherwise the plan falls back to a
		// render and says so — silently dropping a speaker would be worse.
		canASR: st.HasAudio && !st.AudioIncomplete,
	}
	if !p.planTarget(target) {
		return p.plan
	}
	if includeDownstream {
		p.addDownstream(target)
	}
	return p.plan
}

// regenPlanner accumulates one plan. Keeping the state and the partial plan
// together is what lets the rules below be small functions instead of one
// deeply nested switch.
type regenPlanner struct {
	plan   RegenPlan
	st     RegenState
	canASR bool
}

// planTarget applies the target-specific rules. A false return means the plan
// is blocked and must be returned as-is.
func (p *regenPlanner) planTarget(target ArtifactKind) bool {
	switch target {
	case ArtifactTranscript:
		return p.addTranscript(RegenReasonTarget, true)
	case ArtifactRevised:
		if !p.ensureTranscript() {
			return false
		}
		p.addModelStep(ArtifactRevised, RegenReasonTarget)
	case ArtifactMinutes:
		if !p.ensureRevised() {
			return false
		}
		p.addModelStep(ArtifactMinutes, RegenReasonTarget)
	}
	return true
}

// ensureTranscript is the shared upstream rule for the model steps: a transcript
// is needed when it has no content or when a previous recognition never got as
// far as writing the VTT.
func (p *regenPlanner) ensureTranscript() bool {
	if !p.st.TranscriptHasContent || p.st.TranscriptPendingASR {
		if !p.addTranscript(RegenReasonMissing, false) {
			return false
		}
	}
	if !p.st.HasSegments && !p.contains(ArtifactTranscript) {
		// The transcript file exists but its segments are gone; the only way to
		// rebuild the proofread text (which reads the database, not the VTT) is
		// to recognize the recording again.
		return p.addTranscript(RegenReasonMissing, true)
	}
	return true
}

// ensureRevised makes sure a usable proofread text is available to summarize.
func (p *regenPlanner) ensureRevised() bool {
	if p.st.RevisedHasContent && !p.st.RevisedStale {
		return true
	}
	if !p.ensureTranscript() {
		return false
	}
	reason := RegenReasonStale
	if !p.st.RevisedHasContent {
		reason = RegenReasonMissing
	}
	p.addModelStep(ArtifactRevised, reason)
	return true
}

// addTranscript adds the transcript step (once) and its warnings. A false
// return means the plan is blocked.
func (p *regenPlanner) addTranscript(reason string, wantASR bool) bool {
	if p.contains(ArtifactTranscript) {
		return true
	}
	mode, reason, ok := p.transcriptMode(reason, wantASR)
	if !ok {
		return false
	}
	p.addTranscriptWarnings(mode, wantASR)
	p.plan.Steps = append(p.plan.Steps, RegenStep{Kind: ArtifactTranscript, Mode: mode, Reason: reason})
	return true
}

// transcriptMode picks between recognizing the recording again and rebuilding
// the VTT from the existing segments, or blocks the plan when neither is
// possible.
func (p *regenPlanner) transcriptMode(reason string, wantASR bool) (mode, why string, ok bool) {
	mode = RegenModeRender
	if wantASR && p.canASR {
		mode = RegenModeASR
	}
	if p.st.TranscriptPendingASR {
		// Recognition already finished; only the VTT write is missing.
		mode = RegenModeRender
		reason = RegenReasonPending
	}
	if mode == RegenModeRender && !p.st.HasSegments {
		if !p.canASR {
			p.plan.Blocked = &RegenBlock{
				Code:    ArtifactErrNoSource,
				Message: "no segments and no usable recording",
			}
			return "", "", false
		}
		mode = RegenModeASR
	}
	return mode, reason, true
}

func (p *regenPlanner) addTranscriptWarnings(mode string, wantASR bool) {
	if wantASR && !p.canASR {
		// The user asked for recognition but it is not fully possible; the sheet
		// must explain why the step became a rebuild.
		if p.st.AudioIncomplete {
			p.plan.addWarning(RegenWarnAudioIncomplete)
		} else {
			p.plan.addWarning(RegenWarnAudioUnavailable)
		}
	}
	if mode == RegenModeASR {
		if p.st.MixedOnly {
			p.plan.addWarning(RegenWarnMixedAudioOnly)
		}
		if !p.st.AudioBaseKnown {
			p.plan.addWarning(RegenWarnAudioBaseEstimated)
		}
	}
	if p.st.TranscriptHasContent {
		p.plan.addWarning(RegenWarnOverwriteTranscript)
	}
}

func (p *regenPlanner) addModelStep(kind ArtifactKind, reason string) {
	if p.contains(kind) {
		return
	}
	p.plan.Steps = append(p.plan.Steps, RegenStep{Kind: kind, Mode: RegenModeModel, Reason: reason})
}

func (p *regenPlanner) addDownstream(target ArtifactKind) {
	for _, kind := range downstreamOf(target) {
		p.addModelStep(kind, RegenReasonDownstream)
	}
}

func (p *regenPlanner) contains(kind ArtifactKind) bool {
	return containsKind(p.plan.Steps, kind)
}

// downstreamOf returns the artifacts that depend on target, in dependency
// order.
func downstreamOf(target ArtifactKind) []ArtifactKind {
	switch target {
	case ArtifactTranscript:
		return []ArtifactKind{ArtifactRevised, ArtifactMinutes}
	case ArtifactRevised:
		return []ArtifactKind{ArtifactMinutes}
	default:
		return nil
	}
}

func containsKind(steps []RegenStep, kind ArtifactKind) bool {
	for _, step := range steps {
		if step.Kind == kind {
			return true
		}
	}
	return false
}

func (p *RegenPlan) addWarning(w string) {
	for _, existing := range p.Warnings {
		if existing == w {
			return
		}
	}
	p.Warnings = append(p.Warnings, w)
}

// PlanForInitialJob is the plan the post-meeting job always runs: render the
// transcript from the segments the client already uploaded, proofread it, then
// summarize. It is deliberately not derived from the missing/stale rules — a
// fresh meeting has all three artifacts missing, and the initial job must still
// produce them in one pass.
func PlanForInitialJob() []RegenStep {
	return []RegenStep{
		{Kind: ArtifactTranscript, Mode: RegenModeRender, Reason: RegenReasonTarget},
		{Kind: ArtifactRevised, Mode: RegenModeModel, Reason: RegenReasonDownstream},
		{Kind: ArtifactMinutes, Mode: RegenModeModel, Reason: RegenReasonTarget},
	}
}

// PlanForInitialJobWithoutProofread is PlanForInitialJob with the proofread
// step removed (meeting.proofread = false). The minutes step already falls back
// to the raw transcript when no revised draft exists, so the chain still
// produces a complete document with one fewer model pass.
func PlanForInitialJobWithoutProofread() []RegenStep {
	return []RegenStep{
		{Kind: ArtifactTranscript, Mode: RegenModeRender, Reason: RegenReasonTarget},
		{Kind: ArtifactMinutes, Mode: RegenModeModel, Reason: RegenReasonTarget},
	}
}

// FormatPlanKinds is a small helper for logs and error messages.
func FormatPlanKinds(steps []RegenStep) string {
	out := ""
	for i, step := range steps {
		if i > 0 {
			out += " → "
		}
		out += fmt.Sprintf("%s(%s)", step.Kind, step.Mode)
	}
	return out
}
