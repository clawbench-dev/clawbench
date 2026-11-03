package handler

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeLiveShareNotesRegeneratePlan handles
// GET /api/live-share/notes/regenerate/plan?token=&target=&downstream=
// (authenticated, project-scoped).
//
// The confirmation sheet asks this before it shows anything, so the steps the
// user reads are exactly the steps the server will run. Recomputing the plan on
// the POST (rather than trusting the client's copy) is deliberate: the record
// can change between opening the sheet and confirming.
func ServeLiveShareNotesRegeneratePlan(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	token := r.URL.Query().Get("token")
	target := service.ArtifactKind(r.URL.Query().Get("target"))
	projectPath, projectID, ok := regenPreamble(w, r, token, target)
	if !ok {
		return
	}
	downstream := parseQueryBool(r.URL.Query().Get("downstream"))

	state, exists, err := regenStateFor(projectPath, projectID, token)
	if err != nil {
		slog.Error("live notes: regeneration plan state failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, service.PlanRegeneration(target, downstream, state))
}

// regenPreamble validates what every regeneration request needs: project scope,
// token ownership and a known target. It writes the failure response itself.
func regenPreamble(w http.ResponseWriter, r *http.Request, token string, target service.ArtifactKind) (projectPath string, projectID int64, ok bool) {
	projectPath, projectID, ok = liveNotesProject(w, r)
	if !ok {
		return "", 0, false
	}
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return "", 0, false
	}
	if !target.Valid() {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return "", 0, false
	}
	return projectPath, projectID, true
}

// ServeLiveShareNotesRegenerate handles
// POST /api/live-share/notes/regenerate (authenticated, project-scoped).
//
// Body: {"token":"...","target":"transcript|revised|minutes","includeDownstream":false}.
// Idempotent: if a job is already running for the meeting, the current record is
// returned instead of starting a second one.
func ServeLiveShareNotesRegenerate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Token             string               `json:"token"`
		Target            service.ArtifactKind `json:"target"`
		IncludeDownstream bool                 `json:"includeDownstream"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	projectPath, projectID, ok := regenPreamble(w, r, body.Token, body.Target)
	if !ok {
		return
	}

	record, exists, err := service.GetMeetingRecord(projectPath, projectID, body.Token)
	if err != nil {
		slog.Error("live notes: regenerate record failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	if record.EndedAt == 0 {
		// The recording is still being written; regenerating it now would
		// produce a transcript that is missing the end of the meeting.
		writeLocalizedErrorf(w, r, http.StatusConflict, "LiveNotesMeetingLive")
		return
	}

	state, exists, err := regenStateFor(projectPath, projectID, body.Token)
	if err != nil {
		slog.Error("live notes: regenerate state failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	plan := service.PlanRegeneration(body.Target, body.IncludeDownstream, state)
	if plan.Blocked != nil {
		writeJSON(w, http.StatusConflict, map[string]any{
			strReqError: plan.Blocked.Message,
			"code":      plan.Blocked.Code,
			"plan":      plan,
		})
		return
	}

	claimed, claimErr := service.ClaimArtifactJob(body.Token, artifactPlanKinds(plan.Steps), 0)
	if claimErr != nil {
		slog.Error("live notes: claim regeneration failed", "err", claimErr)
		model.WriteError(w, model.Internal(claimErr))
		return
	}
	if claimed {
		slog.Info("live notes: regeneration accepted",
			"token", body.Token, "plan", service.FormatPlanKinds(plan.Steps))
		go runArtifactChain(body.Token, projectID, projectPath, record.Title, plan.Steps, true)
	}

	writeRegeneratedRecord(w, r, projectPath, projectID, body.Token)
}

// writeRegeneratedRecord answers with the record as it looks right after the
// claim, so the caller can render "generating" without a second request.
func writeRegeneratedRecord(w http.ResponseWriter, r *http.Request, projectPath string, projectID int64, token string) {
	updated, exists, err := service.GetMeetingRecord(projectPath, projectID, token)
	if err != nil {
		slog.Error("live notes: regenerate reload failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": updated})
}

// ServeLiveShareNotesRegenerateCancel handles
// POST /api/live-share/notes/regenerate/cancel (authenticated, project-scoped).
//
// Only the context is cancelled; the chain goroutine is the single writer of
// status, so there is no race between "cancel" and "the step just finished".
func ServeLiveShareNotesRegenerateCancel(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validShareToken(body.Token) || !liveNotesOwnedByProject(body.Token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	if !cancelLiveNoteJob(body.Token) {
		writeLocalizedErrorf(w, r, http.StatusConflict, "LiveNotesGenerating")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// regenStateFor assembles the pure PlanRegeneration input from the record, the
// stored text and the artifact states.
func regenStateFor(projectPath string, projectID int64, token string) (service.RegenState, bool, error) {
	record, exists, err := service.GetMeetingRecord(projectPath, projectID, token)
	if err != nil || !exists {
		return service.RegenState{}, exists, err
	}
	note, ok, err := service.GetLiveNote(token)
	if err != nil {
		return service.RegenState{}, false, err
	}
	if !ok {
		return service.RegenState{}, false, nil
	}
	states, err := service.ArtifactStatesForToken(token)
	if err != nil {
		return service.RegenState{}, false, err
	}
	state := service.RegenState{
		HasSegments:          record.HasSegments,
		HasAudio:             record.HasAudio,
		TranscriptHasContent: record.HasTranscript,
		TranscriptPendingASR: states[service.ArtifactTranscript].PendingASRAt > 0,
		RevisedHasContent:    strings.TrimSpace(note.Revised) != "",
		RevisedStale:         record.Artifacts[service.ArtifactRevised].Stale,
		MinutesHasContent:    strings.TrimSpace(note.Minutes) != "",
		MinutesStale:         record.Artifacts[service.ArtifactMinutes].Stale,
	}
	segments, err := service.ListLiveNoteSegments(token, 0)
	if err != nil {
		return service.RegenState{}, false, err
	}
	incomplete, mixedOnly, baseKnown, err := regenAudioState(token, record.DirPath, record.HasAudio, segments)
	if err != nil {
		return service.RegenState{}, false, err
	}
	state.AudioIncomplete = incomplete
	state.MixedOnly = mixedOnly
	state.AudioBaseKnown = baseKnown
	return state, true, nil
}

// regenAudioState answers the three audio questions PlanRegeneration needs:
// is a channel missing, is only the mix left, and do we know the files' origins.
//
// The directory is the source of truth for "is there a file"; the metadata only
// supplies origins. That is what lets an old meeting (merged before the metadata
// existed) still be re-recognized, with an explicit estimate warning.
func regenAudioState(token, dirPath string, hasAudio bool, segments []service.LiveNoteSegment) (incomplete, mixedOnly, baseKnown bool, err error) {
	if !hasAudio {
		return false, false, true, nil
	}
	if dirPath == "" {
		return true, false, false, nil
	}
	channels, err := service.ListLiveNoteAudioChannels(token)
	if err != nil {
		return false, false, false, err
	}
	segmentChannels := liveNoteSegmentChannels(segments)
	files := scanRegenAudio(dirPath, segmentChannels)
	return files.missingChannel(segmentChannels), files.mixedOnly, regenAudioBaseKnown(channels, files.merged), nil
}

// regenAudioFiles is what the meeting's audio folder currently holds.
type regenAudioFiles struct {
	have      map[string]bool // channel → a recording exists
	merged    bool            // at least one merged (non-segment) file
	mixedOnly bool            // the mix is the only recording
}

func scanRegenAudio(dirPath string, segmentChannels []string) regenAudioFiles {
	out := regenAudioFiles{have: make(map[string]bool, 4)}
	entries, err := os.ReadDir(filepath.Join(dirPath, "audio"))
	if err != nil {
		return out
	}
	nonMix := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".webm") {
			continue
		}
		if !service.LiveNoteAudioFileIsSegment(entry.Name()) {
			out.merged = true
		}
		slug, ok := service.LiveNoteAudioFileChannelSlug(entry.Name())
		if !ok {
			continue
		}
		channel := service.LiveNoteAudioChannelForSlug(slug, segmentChannels)
		if channel == "" {
			channel = slug
		}
		out.have[channel] = true
		if channel != service.LiveNoteMixChannel {
			nonMix = true
		}
	}
	out.mixedOnly = !nonMix && out.have[service.LiveNoteMixChannel]
	return out
}

// missingChannel reports whether a channel that produced transcript text has no
// recording. Only a per-channel setup can have a hole: the mix covers every
// speaker, so a channel folded into it (a solo meeting) is not missing.
func (f regenAudioFiles) missingChannel(segmentChannels []string) bool {
	nonMix := false
	for channel := range f.have {
		if channel != service.LiveNoteMixChannel {
			nonMix = true
			break
		}
	}
	if !nonMix {
		return false
	}
	for _, channel := range segmentChannels {
		if channel != "system" && !f.have[channel] {
			return true
		}
	}
	return false
}

// regenAudioBaseKnown reports whether every file's absolute origin is recorded.
func regenAudioBaseKnown(channels []service.LiveNoteAudioChannel, mergedFile bool) bool {
	if len(channels) == 0 {
		// No metadata: merged files have lost their origin; unmerged segment
		// files still carry it in their names and the next merge will record it.
		return !mergedFile
	}
	for _, channel := range channels {
		if channel.BaseAt <= 0 {
			return false
		}
	}
	return true
}

// parseQueryBool accepts the forms a browser or curl is likely to send.
func parseQueryBool(raw string) bool {
	if raw == "" {
		return false
	}
	if raw == "1" || raw == "on" {
		return true
	}
	value, err := strconv.ParseBool(raw)
	return err == nil && value
}
