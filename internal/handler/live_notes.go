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
	"strconv"
	"strings"
	"time"

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
	// liveNoteMinutesTimeout bounds one minutes generation (chunked map-reduce
	// over a possibly hour-long transcript).
	liveNoteMinutesTimeout = 5 * time.Minute
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
		Minutes: liveNotesMinutesPayload{
			Status:   note.Status,
			Markdown: note.Minutes,
			Error:    note.Error,
			FilePath: note.FilePath,
		},
	})
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
		StartedAt:  parseLiveNoteMillis(r.FormValue("startedAt")),
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
		writeJSON(w, http.StatusOK, liveNotesMinutesPayload{
			Status:   note.Status,
			Markdown: note.Minutes,
			Error:    note.Error,
			FilePath: note.FilePath,
		})
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
	go generateLiveNotesMinutes(body.Token, projectID, projectPath, title, segments)

	writeJSON(w, http.StatusOK, liveNotesMinutesPayload{Status: service.LiveNotesStatusGenerating})
}

// generateLiveNotesMinutes is the detached minutes job. It uses a background
// context because the HTTP request that started it has already returned.
//
// The minutes are both stored (with the 7-day retention) and written into the
// project as Markdown. The file is the durable copy: it survives cleanup, can be
// edited, and the user can commit it.
func generateLiveNotesMinutes(token string, projectID int64, projectPath, title string, segments []service.LiveNoteSegment) {
	ctx, cancel := context.WithTimeout(context.Background(), liveNoteMinutesTimeout)
	defer cancel()

	minutes, err := service.GenerateLiveNotesMinutes(ctx, title, segments)
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
	written, werr := service.WriteLiveNotesFile(projectPath, title, minutes, segments, liveNotesStartTime(segments))
	if werr != nil {
		slog.Warn("live notes: write minutes file failed", "err", werr)
	} else {
		filePath = written
	}

	if err := service.SetLiveNoteMinutes(token, projectID, title, service.LiveNotesStatusReady, minutes, "", filePath); err != nil {
		slog.Error("live notes: persist minutes failed", "err", err)
	}
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
			// FilePath deliberately omitted: viewers get the text, not a path
			// into the publisher's project.
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
