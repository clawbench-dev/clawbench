// AI 听记 (live meeting notes) HTTP surface.
//
// The publisher records each audio channel separately and uploads short
// segments; the server transcribes them with the configured STT provider and
// stores the text. This file is the transport layer only — persistence,
// timeline merging and summarisation live in internal/service/live_notes.go.
//
// Why the publisher uploads instead of the server capturing the media: a live
// share is P2P, the server never receives a single audio packet. The publisher
// already holds every track (system audio, its own microphone, one per viewer),
// so it is also the only place where the channels are still separate — which is
// what gives each line its speaker.
package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/liveshare"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

const (
	// liveNoteSegmentMaxBytes caps one uploaded segment. A 10-second Opus clip
	// is a few tens of KiB; anything near this limit is a client bug or abuse.
	liveNoteSegmentMaxBytes = 4 << 20
	// liveNoteSegmentMaxDurationMS bounds the duration a client may claim.
	liveNoteSegmentMaxDurationMS = 120_000
	// liveNoteTextMaxRunes bounds one transcribed line. A 10-second utterance is
	// far shorter; the cap only stops a runaway provider from bloating the DB.
	liveNoteTextMaxRunes = 2000
	// liveNoteSpeakerMaxRunes bounds the display label supplied by the client.
	liveNoteSpeakerMaxRunes = 40
	// liveNoteTextKey is the JSON key carrying one transcript line.
	liveNoteTextKey = "text"
	// liveNoteSTTConcurrency is how many segment transcriptions may run at once
	// across all rooms. The STT backend is a shared HTTP service; a long meeting
	// with eight speaking viewers would otherwise burst it.
	liveNoteSTTConcurrency = 4
	// liveNoteMinutesTimeout bounds one minutes generation. The job runs two
	// chunked passes now (proofread, then summarize), so the budget covers both.
	liveNoteMinutesTimeout = 10 * time.Minute
)

// liveNoteSTTSlots is the process-wide transcription semaphore.
var liveNoteSTTSlots = make(chan struct{}, liveNoteSTTConcurrency)

// liveNotesResponse is the publisher/viewer read payload.
type liveNotesResponse struct {
	Title    string                    `json:"title,omitempty"`
	Segments []service.LiveNoteSegment `json:"segments"`
	Minutes  liveNotesMinutesPayload   `json:"minutes"`
}

// liveNotesMinutesPayload carries the generated minutes and their status so the
// SPA can render "generating" without a second endpoint.
type liveNotesMinutesPayload struct {
	Status   string `json:"status"`
	Markdown string `json:"markdown,omitempty"`
	Error    string `json:"error,omitempty"`
	// FilePath is publisher-only: the Markdown copy written inside the project.
	// Never sent to viewers — a server path is not theirs to read.
	FilePath string `json:"filePath,omitempty"`
	// Revise reports the proofreading stage that runs before summarisation, so
	// the dialog can say "正在校对转写" instead of a generic spinner.
	Revise liveNotesRevisePayload `json:"revise"`
	// Artifacts are the files this meeting left in the project (publisher only:
	// they are absolute paths on the publisher's disk).
	Artifacts liveNotesArtifacts `json:"artifacts"`
}

// liveNotesRevisePayload is the state of the context-aware proofreading pass.
type liveNotesRevisePayload struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// liveNotesArtifacts lists the permanent files of one meeting.
//
// Unlike the database rows these are never cleaned up: they are the copy the
// user owns, which is why the SPA shows the paths rather than a download.
type liveNotesArtifacts struct {
	Dir        string `json:"dir,omitempty"`
	Minutes    string `json:"minutes,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Revised    string `json:"revised,omitempty"`
	Audio      string `json:"audio,omitempty"`
	AudioFiles int    `json:"audioFiles,omitempty"`
}

// ServeLiveShareNotesSession starts a microphone-only notes session.
//
// A solo dictation is not a share: no room, no link, no viewer. All it needs
// from the share machinery is a token to key the notes rows on, so this mints
// one and records it in live_notes — every other endpoint (segment upload,
// minutes, artifacts) then works unchanged, because ownership is proven by that
// row rather than by a live room.
func ServeLiveShareNotesSession(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	_, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	var body struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	token, err := liveshare.NewToken()
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}
	title := truncateRunes(strings.TrimSpace(body.Title), 80)
	if err := service.SetLiveNoteMinutes(token, projectID, title, service.LiveNotesStatusIdle, "", "", ""); err != nil {
		slog.Error("live notes: create solo session failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// ServeLiveShareNotes handles GET /api/live-share/notes?token=&since=
// (authenticated, project-scoped).
func ServeLiveShareNotes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if !validShareToken(token) {
		http.NotFound(w, r)
		return
	}
	if !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}

	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	segments, err := service.ListLiveNoteSegments(token, since)
	if err != nil {
		slog.Error("live notes: list segments failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	note, _, err := service.GetLiveNote(token)
	if err != nil {
		slog.Error("live notes: get note failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, liveNotesResponse{
		Title:    note.Title,
		Segments: segments,
		Minutes:  liveNotesPayload(note),
	})
}

// liveNotesPayload turns a stored row into the publisher-facing payload.
//
// Artifacts are resolved here rather than stored as four columns: the folder is
// the single fact, and the file names inside it are a convention.
func liveNotesPayload(note service.LiveNote) liveNotesMinutesPayload {
	return liveNotesMinutesPayload{
		Status:    note.Status,
		Markdown:  note.Minutes,
		Error:     note.Error,
		FilePath:  note.FilePath,
		Revise:    liveNotesRevisePayload{Status: note.ReviseStatus, Error: note.ReviseError},
		Artifacts: liveNoteArtifactsOf(note.DirPath),
	}
}

// liveNoteArtifactsOf lists what is ACTUALLY on disk for a meeting folder.
//
// Each Markdown file is stat'ed rather than assumed: the folder exists from the
// first audio segment, but the transcripts and the minutes only appear once the
// post-meeting job reaches them. Reporting a path to a file that does not exist
// yet is worse than reporting nothing — the user would go looking for it.
func liveNoteArtifactsOf(dir string) liveNotesArtifacts {
	if strings.TrimSpace(dir) == "" {
		return liveNotesArtifacts{}
	}
	paths := service.LiveNotesPathsIn(dir)
	out := liveNotesArtifacts{Dir: paths.Dir, Audio: paths.Audio}
	for _, f := range []struct {
		target string
		into   *string
	}{
		{paths.Minutes, &out.Minutes},
		{paths.Transcript, &out.Transcript},
		{paths.Revised, &out.Revised},
	} {
		if _, err := os.Stat(f.target); err == nil {
			*f.into = f.target
		}
	}
	// Count only real segments: the folder also holds the .gitignore that keeps
	// the binary audio out of the repository.
	if entries, err := os.ReadDir(paths.Audio); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".webm") {
				out.AudioFiles++
			}
		}
	}
	return out
}

// ServeLiveShareNoteSegment handles POST /api/live-share/notes/segment
// (authenticated, project-scoped, multipart).
//
// Fields: token, channel, speaker, startedAt (unix ms), durationMs, file.
// The audio is transcribed synchronously and stored; the response carries the
// recognized text so the publisher can show captions without polling.
func ServeLiveShareNoteSegment(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, liveNoteSegmentMaxBytes)
	// Parse BEFORE reading any field: r.FormValue swallows the reader's error,
	// so an oversized upload would look like "no token" and be answered with
	// 404 — which the publisher reads as "this share is gone" and uses to drop
	// its whole upload queue. 413 says "too big", which is retryable/skippable.
	// The body is already capped by MaxBytesReader above, so this cannot grow
	// without bound; gosec cannot see through the reader.
	if err := r.ParseMultipartForm(liveNoteSegmentMaxBytes); err != nil { //nolint:gosec // bounded by MaxBytesReader
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeLocalizedErrorf(w, r, http.StatusRequestEntityTooLarge, "BadRequest")
			return
		}
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}

	token := r.FormValue("token")
	if !validShareToken(token) {
		http.NotFound(w, r)
		return
	}
	if !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	channel := r.FormValue("channel")
	if !validLiveNoteChannel(channel) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	defer func() { _ = file.Close() }()

	// Bound the audio read: FormFile already enforced the body limit, but a
	// provider call must not be handed an unbounded reader either.
	audio, err := io.ReadAll(io.LimitReader(file, liveNoteSegmentMaxBytes))
	if err != nil || len(audio) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}

	startedAt := parseLiveNoteMillis(r.FormValue("startedAt"))

	persistLiveNoteAudio(token, projectID, projectPath, r.FormValue("title"), channel, startedAt, audio)

	text, err := transcribeLiveNoteSegment(r.Context(), audio)
	if err != nil {
		slog.Warn("live notes: transcribe failed", "channel", channel, "err", err)
		// The share itself is unaffected: a failed segment leaves a gap in the
		// transcript, which is far better than aborting the uploader's queue.
		writeJSON(w, http.StatusBadGateway, map[string]string{strReqError: "transcription failed"})
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		// Silence is normal (a quiet channel between utterances). Storing an
		// empty row would only add noise to the timeline.
		writeJSON(w, http.StatusOK, map[string]any{"id": 0, liveNoteTextKey: ""})
		return
	}

	seg := service.LiveNoteSegment{
		Channel:    channel,
		Speaker:    truncateRunes(strings.TrimSpace(r.FormValue("speaker")), liveNoteSpeakerMaxRunes),
		StartedAt:  startedAt,
		DurationMS: capInt64(parseLiveNoteMillis(r.FormValue("durationMs")), liveNoteSegmentMaxDurationMS),
		Text:       truncateRunes(text, liveNoteTextMaxRunes),
	}
	id, err := service.AppendLiveNoteSegment(r.Context(), token, projectID, seg)
	if err != nil {
		slog.Error("live notes: append segment failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	seg.ID = id
	writeJSON(w, http.StatusOK, map[string]any{
		"id":            id,
		liveNoteTextKey: seg.Text,
		"channel":       seg.Channel,
		"speaker":       seg.Speaker,
		"startedAt":     seg.StartedAt,
		"durationMs":    seg.DurationMS,
	})
}

// ServeLiveShareNotesMinutes handles POST /api/live-share/notes/minutes
// (authenticated, project-scoped). Body: {"token":"...","title":"...","regenerate":false}.
//
// Generation runs in the background: a long meeting takes tens of seconds, and
// the publisher's dialog may be closed while it runs. The caller polls
// ServeLiveShareNotes for the status.
func ServeLiveShareNotesMinutes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	var body struct {
		Token      string `json:"token"`
		Title      string `json:"title"`
		Regenerate bool   `json:"regenerate"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validShareToken(body.Token) || !liveNotesOwnedByProject(body.Token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}

	note, exists, err := service.GetLiveNote(body.Token)
	if err != nil {
		slog.Error("live notes: minutes lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	// Idempotent unless explicitly asked to regenerate: a double-click on
	// "生成纪要" must not queue two summarisation runs over the same transcript.
	if !body.Regenerate && (note.Status == service.LiveNotesStatusGenerating || note.Status == service.LiveNotesStatusReady) {
		writeJSON(w, http.StatusOK, liveNotesPayload(note))
		return
	}

	segments, err := service.ListLiveNoteSegments(body.Token, 0)
	if err != nil {
		slog.Error("live notes: minutes list failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if len(segments) == 0 {
		writeJSON(w, http.StatusOK, liveNotesMinutesPayload{Status: service.LiveNotesStatusIdle})
		return
	}

	title := truncateRunes(strings.TrimSpace(body.Title), 80)
	if err := service.SetLiveNoteMinutes(body.Token, projectID, title, service.LiveNotesStatusGenerating, "", "", ""); err != nil {
		slog.Error("live notes: mark generating failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	go generateLiveNotesArtifacts(body.Token, projectID, projectPath, title, segments)

	writeJSON(w, http.StatusOK, liveNotesMinutesPayload{
		Status:    service.LiveNotesStatusGenerating,
		Revise:    liveNotesRevisePayload{Status: service.LiveNotesStatusGenerating},
		Artifacts: liveNoteArtifactsOf(note.DirPath),
	})
}

// generateLiveNotesArtifacts is the detached post-meeting job.
//
// It runs three stages in order, and each one has its own durable output:
//
//  1. transcript.md          raw ASR, exactly as recognized
//  2. transcript-revised.md  proofread against the project's own vocabulary
//  3. minutes.md             summary generated FROM the proofread transcript
//
// Stage 2 is why the summary reads like a meeting instead of a dictation: ASR
// has no idea that "claw bench" is ClawBench, and the project's AGENTS.md does.
// When proofreading fails the minutes fall back to the raw transcript — a
// summary of an imperfect transcript beats no summary at all.
//
// Everything is written into the meeting folder that already holds the audio,
// so the three artifacts plus the recording stay together. The database rows
// expire after the retention window; these files do not.
func generateLiveNotesArtifacts(token string, projectID int64, projectPath, title string, segments []service.LiveNoteSegment) {
	ctx, cancel := context.WithTimeout(context.Background(), liveNoteMinutesTimeout)
	defer cancel()

	startedAt := liveNotesStartTime(segments)
	paths, dirErr := service.EnsureLiveNotesDir(token, projectID, projectPath, title, startedAt.UnixMilli())
	if dirErr != nil {
		// The transcript and the minutes still go into the database; only the
		// durable copies are lost, which is worth a warning rather than a
		// failed meeting.
		slog.Warn("live notes: meeting folder unavailable", "err", dirErr)
	}
	onDisk := dirErr == nil

	raw := service.BuildLiveNotesTranscript(segments)
	if onDisk {
		writeLiveNotesArtifact("transcript", func() (string, error) {
			return service.WriteLiveNotesRawTranscript(paths, title, startedAt, raw)
		})
	}

	transcript := proofreadLiveNotesTranscript(ctx, token, projectID, projectPath, title, segments, raw, paths, onDisk, startedAt)

	minutes, err := service.GenerateLiveNotesMinutes(ctx, title, transcript)
	if err != nil {
		slog.Warn("live notes: minutes generation failed", "err", err)
		if serr := service.SetLiveNoteMinutes(token, projectID, title, service.LiveNotesStatusFailed, "", err.Error(), ""); serr != nil {
			slog.Error("live notes: persist minutes failure failed", "err", serr)
		}
		return
	}

	// A failed write must not lose the minutes: they are still stored below, and
	// the UI simply shows no file path.
	filePath := ""
	if onDisk {
		filePath = writeLiveNotesMinutes(paths, title, startedAt, minutes)
	}
	if err := service.SetLiveNoteMinutes(token, projectID, title, service.LiveNotesStatusReady, minutes, "", filePath); err != nil {
		slog.Error("live notes: persist minutes failed", "err", err)
	}
}

// proofreadLiveNotesTranscript runs stage 2 and returns the text to summarize.
//
// On failure it returns the raw transcript: a summary of an imperfect
// transcript beats no summary at all, and the failure is recorded where the UI
// can show it.
func proofreadLiveNotesTranscript(
	ctx context.Context, token string, projectID int64, projectPath, title string,
	segments []service.LiveNoteSegment, raw string, paths service.LiveNotesPaths, onDisk bool, startedAt time.Time,
) string {
	if err := service.SetLiveNoteRevise(token, projectID, service.LiveNotesStatusGenerating, "", ""); err != nil {
		slog.Warn("live notes: mark revising failed", "err", err)
	}
	revised, err := service.ReviseLiveNotesTranscript(ctx, projectPath, title, segments)
	if err != nil {
		slog.Warn("live notes: proofread failed", "err", err)
		if serr := service.SetLiveNoteRevise(token, projectID, service.LiveNotesStatusFailed, "", err.Error()); serr != nil {
			slog.Error("live notes: persist proofread failure failed", "err", serr)
		}
		return raw
	}
	if err := service.SetLiveNoteRevise(token, projectID, service.LiveNotesStatusReady, revised, ""); err != nil {
		slog.Error("live notes: persist proofread failed", "err", err)
	}
	if onDisk {
		writeLiveNotesArtifact("revised", func() (string, error) {
			return service.WriteLiveNotesRevisedTranscript(paths, title, startedAt, revised)
		})
	}
	return revised
}

// writeLiveNotesArtifact runs one file write, downgrading any error to a
// warning: a missing durable copy must never abort the pipeline that produces
// the others.
func writeLiveNotesArtifact(kind string, write func() (string, error)) {
	if _, err := write(); err != nil {
		slog.Warn("live notes: write artifact failed", "kind", kind, "err", err)
	}
}

// writeLiveNotesMinutes writes the summary and returns its path ("" on error).
func writeLiveNotesMinutes(paths service.LiveNotesPaths, title string, startedAt time.Time, minutes string) string {
	audioFiles := 0
	if entries, err := os.ReadDir(paths.Audio); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".webm") {
				audioFiles++
			}
		}
	}
	written, err := service.WriteLiveNotesMinutesFile(paths, title, startedAt, minutes, audioFiles)
	if err != nil {
		slog.Warn("live notes: write minutes file failed", "err", err)
		return ""
	}
	return written
}

// liveNotesStartTime is when the meeting started: the earliest segment's
// timestamp. Used for the file name so regenerating overwrites the same file.
func liveNotesStartTime(segments []service.LiveNoteSegment) time.Time {
	var earliest int64
	for _, s := range segments {
		if s.StartedAt <= 0 {
			continue
		}
		if earliest == 0 || s.StartedAt < earliest {
			earliest = s.StartedAt
		}
	}
	if earliest == 0 {
		return time.Now()
	}
	return time.UnixMilli(earliest)
}

// ServeShareLiveNotes handles GET /api/share/{token}/notes (PUBLIC: the share
// token is the sole credential, exactly like the signaling socket).
//
// Notes are only readable while the room is live. After the publisher ends the
// share the token stops resolving, so a leaked link cannot be used to read a
// transcript later.
func ServeShareLiveNotes(w http.ResponseWriter, r *http.Request, token string) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !validShareToken(token) {
		http.NotFound(w, r)
		return
	}
	if liveRegistry.Get(token) == nil {
		http.NotFound(w, r)
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	segments, err := service.ListLiveNoteSegments(token, since)
	if err != nil {
		slog.Error("live notes: public list failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	note, _, err := service.GetLiveNote(token)
	if err != nil {
		slog.Error("live notes: public get failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, liveNotesResponse{
		Segments: segments,
		Minutes: liveNotesMinutesPayload{
			Status:   note.Status,
			Markdown: note.Minutes,
			Error:    note.Error,
			// FilePath and Artifacts deliberately omitted: viewers get the text,
			// not paths into the publisher's project.
			Revise: liveNotesRevisePayload{Status: note.ReviseStatus},
		},
	})
}

// ── helpers ─────────────────────────────────────────────────────────────────

// liveNotesProject resolves the request's project cookie to both its path and
// its registry id.
func liveNotesProject(w http.ResponseWriter, r *http.Request) (string, int64, bool) {
	projectPath, ok := requireProject(w, r)
	if !ok {
		return "", 0, false
	}
	id, err := store.ProjectIDForPath(projectPath)
	if err != nil {
		slog.Error("live notes: project id resolution failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return "", 0, false
	}
	return projectPath, id, true
}

// liveNotesOwnedByProject reports whether this project may write/read a share's
// notes.
//
// Two sources count as ownership, deliberately:
//   - the live room, while it exists (the normal case), and
//   - an existing notes row for the same project (the tail end of a share).
//
// The second is what makes the last upload and the minutes request survive the
// publisher ending the share: the room is deleted the moment the publisher
// stops, but the segments recorded seconds earlier must still land.
func liveNotesOwnedByProject(token, projectPath string, projectID int64) bool {
	if room := liveRegistry.Get(token); room != nil {
		return room.ProjectKey() == projectPath
	}
	pid, exists, err := service.LiveNoteProjectID(token)
	if err != nil {
		slog.Error("live notes: project lookup failed", "err", err)
		return false
	}
	return exists && pid == projectID
}

// validLiveNoteChannel accepts "system", "mic" or "viewer:<id>".
func validLiveNoteChannel(ch string) bool {
	switch ch {
	case "system", "mic":
		return true
	}
	id, ok := strings.CutPrefix(ch, "viewer:")
	if !ok || id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// persistLiveNoteAudio keeps the recording on disk before it is transcribed.
//
// The recording is the only artifact that cannot be regenerated: an STT outage,
// a mis-recognized sentence or a model upgrade all end with "re-transcribe the
// file", which needs the file. A write failure is logged and ignored — losing
// the transcript because the disk was read-only would be the worse trade.
func persistLiveNoteAudio(token string, projectID int64, projectPath, title, channel string, startedAt int64, audio []byte) {
	paths, err := service.EnsureLiveNotesDir(token, projectID, projectPath, title, startedAt)
	if err != nil {
		slog.Warn("live notes: meeting folder unavailable", "err", err)
		return
	}
	if _, err := service.SaveLiveNoteAudio(paths, channel, startedAt, audio); err != nil {
		slog.Warn("live notes: save audio failed", "err", err)
	}
}

// transcribeLiveNoteSegment calls the STT provider under the process-wide
// concurrency cap.
func transcribeLiveNoteSegment(ctx context.Context, audio []byte) (string, error) {
	select {
	case liveNoteSTTSlots <- struct{}{}:
		defer func() { <-liveNoteSTTSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	provider := GetSTTProvider()
	if provider == nil {
		return "", errors.New("stt provider not configured")
	}
	return provider.Transcribe(ctx, bytes.NewReader(audio), model.ConfigInstance.STT.Language)
}

// parseLiveNoteMillis parses a millisecond timestamp, returning 0 for garbage.
func parseLiveNoteMillis(raw string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// capInt64 bounds v from above. There is no lower bound because the only
// caller feeds it parseLiveNoteMillis, which already rejects negative input.
func capInt64(v, hi int64) int64 {
	if v > hi {
		return hi
	}
	return v
}
