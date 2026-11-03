// AI 听记 (live meeting notes) HTTP surface.
//
// The publisher records each audio channel separately and uploads short
// segments; the server transcribes them with the configured STT provider and
// stores the text. This file is the transport layer only — persistence,
// timeline merging and summarization live in internal/service/live_notes.go.
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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	// liveNoteChatMaxRunes bounds one chat line, mirroring CHAT_MAX_CHARS in
	// web/src/share/liveControl.ts.
	liveNoteChatMaxRunes = 2000
	// liveNoteClientIDMaxRunes bounds the client message id used for idempotency.
	liveNoteClientIDMaxRunes = 64
	// liveNoteFilesMaxBatch caps one file-event batch. A meeting opens tens of
	// files, not thousands; the cap is what stops a looping client from turning
	// one request into an unbounded transaction.
	liveNoteFilesMaxBatch = 200
	// liveNotePathMaxRunes bounds one reported path. Long enough for a deep
	// project path plus a Windows drive prefix, short enough to keep the row and
	// the transcript line readable.
	liveNotePathMaxRunes = 1024
	// liveNoteFilesMaxBodyBytes bounds one batch body. 200 paths of 1024 runes
	// plus JSON overhead is well under this; the cap exists so a malformed or
	// hostile client cannot make the server parse an unbounded array before the
	// per-batch check runs.
	liveNoteFilesMaxBodyBytes = 1 << 20
	// liveNoteSharerMaxRunes bounds the sharer identity ("host" / "viewer:v12").
	liveNoteSharerMaxRunes = 64
	// liveNoteShotMaxBytes caps one uploaded frame. A 1920-wide WebP of a
	// desktop is well under 1 MiB; the cap only stops a client from streaming a
	// video through an endpoint that expects a still.
	liveNoteShotMaxBytes = 2 << 20
	// liveNoteShotMaxDimension bounds the claimed pixel size. It is metadata
	// for the UI (aspect ratio while the image loads), not a decode budget.
	liveNoteShotMaxDimension = 8192
	// liveNoteTextKey is the JSON key carrying one transcript line.
	liveNoteTextKey = "text"
	// liveNoteSTTConcurrency is how many segment transcriptions may run at once
	// across all rooms. The STT backend is a shared HTTP service; a long meeting
	// with eight speaking viewers would otherwise burst it.
	liveNoteSTTConcurrency = 4
	// Per-step budgets. Recognition is the long one (a one-hour meeting is
	// hundreds of 20-second chunks); proofread and summarize are model passes
	// over the same text. The chain budget is deliberately larger than the sum
	// of two steps because a regeneration can run all three.
	liveNoteASRTimeout   = 30 * time.Minute
	liveNoteModelTimeout = 10 * time.Minute
	liveNoteChainTimeout = 60 * time.Minute
)

// liveNoteSTTSlots is the process-wide transcription semaphore.
var liveNoteSTTSlots = make(chan struct{}, liveNoteSTTConcurrency)

// liveNoteJobs is the cancellation registry: token → the cancel func of the
// chain that owns it. Only the goroutine writes status; cancelling just asks
// the context to stop, so the two paths cannot race.
var liveNoteJobs sync.Map

func registerLiveNoteJob(token string, cancel context.CancelFunc) {
	liveNoteJobs.Store(token, cancel)
}

func unregisterLiveNoteJob(token string) {
	liveNoteJobs.Delete(token)
}

func cancelLiveNoteJob(token string) bool {
	value, ok := liveNoteJobs.Load(token)
	if !ok {
		return false
	}
	cancel, ok := value.(context.CancelFunc)
	if !ok {
		return false
	}
	cancel()
	return true
}

// liveNotesResponse is the publisher/viewer read payload.
type liveNotesResponse struct {
	Title string `json:"title,omitempty"`
	// StartedAt / EndedAt are unix ms, server clock. A viewer reads them from
	// the record once the meeting is over (the record view prints the time and
	// the duration); during a live meeting they are simply the current values.
	StartedAt int64                     `json:"startedAt,omitempty"`
	EndedAt   int64                     `json:"endedAt,omitempty"`
	Ended     bool                      `json:"ended"`
	Segments  []service.LiveNoteSegment `json:"segments"`
	// Files are the open/close events so far. A viewer gets the same rows with
	// Path blanked — it may know which file, not where the publisher keeps it.
	Files []service.LiveNoteFile `json:"files,omitempty"`
	// Screens are the screen-sharing intervals (who shared, when). No redaction
	// needed: a nickname is already visible to every viewer in the roster.
	Screens []service.LiveNoteScreenShare `json:"screens,omitempty"`
	// Chat is the archived conversation, oldest first. Filled only for the
	// record view (`?full=1`): the live poll runs every few seconds and must not
	// re-transfer a growing message list on every tick.
	Chat []liveNotePublicChat `json:"chat,omitempty"`
	// Shots are the key frames kept for this meeting. Publisher-only: the
	// public endpoint leaves this empty (a viewer watches the live picture, and
	// the frames are a persistent artifact of the host's project).
	Shots   []service.LiveNoteShot  `json:"shots,omitempty"`
	Minutes liveNotesMinutesPayload `json:"minutes"`
}

// liveNotePublicChat is one chat line as a viewer may see it: the text, who
// said it and when. The client message id and the viewer id are internal
// identifiers and are not part of the record.
type liveNotePublicChat struct {
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
	At      int64  `json:"at"`
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
	// Revise reports the proofreading stage that runs before summarization, so
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
	// Text is the proofread transcript. Viewer-only (`?full=1`): the publisher
	// reads the same content from `transcript-revised.md`, which is a path on
	// its own disk.
	Text string `json:"text,omitempty"`
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
	// AudioMeeting is the merged whole-meeting file. Empty until the merge has
	// run (or for a recording that predates it).
	AudioMeeting string `json:"audioMeeting,omitempty"`
	AudioFiles   int    `json:"audioFiles,omitempty"`
}

// ServeLiveNotesSessions handles GET /api/live-share/notes/sessions
// (authenticated, project-scoped): the project's meeting records.
//
// The record is the database row, not the folder. A meeting exists from the
// moment the publisher attaches — including one that recorded nothing, or whose
// AI 听记 was off — and the folder is only created once there is something to
// put in it. Listing folders instead would silently drop exactly those
// meetings, and would lose the record entirely once the folder is deleted.
func ServeLiveNotesSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		serveLiveNotesList(w, r)
	case http.MethodDelete:
		serveLiveNotesDelete(w, r)
	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

func serveLiveNotesList(w http.ResponseWriter, r *http.Request) {
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	if token := r.URL.Query().Get("token"); token != "" {
		if !validShareToken(token) {
			http.NotFound(w, r)
			return
		}
		record, exists, err := service.GetMeetingRecord(projectPath, projectID, token)
		if err != nil {
			slog.Error("live notes: get session failed", "err", err)
			model.WriteError(w, model.Internal(err))
			return
		}
		if !exists {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": record})
		return
	}
	sessions, err := service.ListMeetingRecords(projectPath, projectID, 200)
	if err != nil {
		slog.Error("live notes: list sessions failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{jsonSessions: sessions})
}

// ServeLiveNotesArtifact handles
// GET /api/live-share/notes/sessions/artifact?token=&kind= (authenticated,
// project-scoped).
//
// The caller addresses the meeting by TOKEN, never by a path: the folder name
// comes from our own row. That removes the whole class of "validate the client
// supplied directory name" problems — there is no client supplied path.
// `kind` is one of minutes / transcript / revised / chat (default minutes).
func ServeLiveNotesArtifact(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	dirName, err := service.MeetingDirName(token)
	if err != nil {
		slog.Error("live notes: artifact dir lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if dirName == "" {
		// The meeting produced no artifacts (notes off, silence, chat-only
		// before the archive was written).
		http.NotFound(w, r)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "minutes"
	}
	content, file, err := service.ReadLiveNoteArtifactWithName(projectPath, dirName, kind)
	switch {
	case errors.Is(err, service.ErrLiveNoteSessionNotFound):
		http.NotFound(w, r)
		return
	case errors.Is(err, service.ErrLiveNoteArtifactKind), errors.Is(err, service.ErrLiveNoteArtifactTooLarge):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	case err != nil:
		slog.Error("live notes: read artifact failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": kind, strContent: content, jsonFile: file})
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
	files, err := service.ListLiveNoteFiles(token, 0)
	if err != nil {
		slog.Error("live notes: list files failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	screens, err := service.ListLiveNoteScreenShares(token)
	if err != nil {
		slog.Error("live notes: list screen shares failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	note, _, err := service.GetLiveNote(token)
	if err != nil {
		slog.Error("live notes: get note failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	shots, err := service.ListLiveNoteShots(token)
	if err != nil {
		slog.Error("live notes: list shots failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, liveNotesResponse{
		Title:    note.Title,
		Segments: segments,
		Files:    files,
		Screens:  screens,
		Shots:    shots,
		Minutes:  liveNotesPayload(projectPath, note),
	})
}

// serveLiveNotesDelete handles DELETE /api/live-share/notes/sessions?token=
// (authenticated, project-scoped).
//
// Two refusals, both 409 rather than 400: the request is well-formed, the
// meeting simply cannot be deleted right now.
//   - the room is still publishing (the user should end the meeting first);
//   - the minutes/proofread job is running — its detached goroutine would
//     recreate the folder and re-upsert the row seconds after the delete.
//
// "Running" is judged by the job's last write, not by the bare status: a crash
// during generation leaves status='generating' behind with no job to finish it,
// and refusing forever would make that meeting (and its folder) undeletable.
// The age threshold is the SPA's own isStaleGenerating, so the list and this
// guard agree on which meetings are interrupted rather than in progress.
func serveLiveNotesDelete(w http.ResponseWriter, r *http.Request) {
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	if liveRegistry.Get(token) != nil {
		writeLocalizedErrorf(w, r, http.StatusConflict, "LiveNotesMeetingLive")
		return
	}
	generating, err := service.ArtifactJobActive(token, time.Now().UnixMilli())
	if err != nil {
		slog.Error("live notes: delete lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if generating {
		writeLocalizedErrorf(w, r, http.StatusConflict, "LiveNotesGenerating")
		return
	}
	if err := service.DeleteMeetingRecord(projectPath, projectID, token); err != nil {
		if errors.Is(err, service.ErrMeetingNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("live notes: delete failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeLiveShareNoteShot is the one path with two verbs: POST uploads a frame,
// DELETE removes one. They share the path because they address the same
// resource; the router keys on the path alone, so the switch lives here.
func ServeLiveShareNoteShot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		serveLiveShareNoteShotUpload(w, r)
	case http.MethodDelete:
		serveLiveNoteShotDelete(w, r)
	default:
		requireMethod(w, r, http.MethodPost)
	}
}

// serveLiveShareNoteShotUpload handles POST /api/live-share/notes/shot
// (authenticated, project-scoped, multipart): the publisher uploads one key
// frame it captured locally.
//
// Fields: token, clientShotId, at (unix ms), reason, ref, width, height, file.
// The file is sniffed, never trusted by its declared type: the client asks the
// canvas for WebP but some engines hand back PNG, and a mislabelled upload must
// not become a file the thumbnailer cannot open.
func serveLiveShareNoteShotUpload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	if !parseShotUpload(w, r) {
		return
	}
	token := r.FormValue("token")
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	reason, clientShotID, ok := shotFields(w, r)
	if !ok {
		return
	}
	img, ext, ok := readShotImage(w, r)
	if !ok {
		return
	}
	if !shotQuotaAvailable(w, r, token) {
		return
	}
	note, ok := shotMeeting(w, r, token)
	if !ok {
		return
	}
	at := shotUploadAt(r, note.StartedAt)
	paths, err := service.EnsureMeetingDir(token, projectID, projectPath, note.Title, note.StartedAt)
	if err != nil {
		slog.Warn("live notes: meeting folder unavailable for shot", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	fileName, err := service.SaveLiveNoteShot(paths, at, reason, ext, img)
	if err != nil {
		slog.Error("live notes: save shot failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	sum := sha256.Sum256(img)
	shot := service.LiveNoteShot{
		At:       at,
		Reason:   reason,
		Ref:      truncateRunes(strings.TrimSpace(r.FormValue("ref")), liveNotePathMaxRunes),
		FileName: fileName,
		Width:    clampInt(parseLiveNoteInt(r.FormValue("width")), 0, liveNoteShotMaxDimension),
		Height:   clampInt(parseLiveNoteInt(r.FormValue("height")), 0, liveNoteShotMaxDimension),
		Bytes:    int64(len(img)),
	}
	id, err := service.AppendLiveNoteShot(r.Context(), token, projectID, shot, clientShotID, hex.EncodeToString(sum[:8]))
	if err != nil {
		// The row is the record; a file with no row would never be listed or
		// deleted, so undo the write instead of leaving an orphan.
		_ = service.DeleteLiveNoteShotFile(paths, fileName)
		slog.Error("live notes: append shot failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	shot.ID = id
	writeJSON(w, http.StatusOK, shot)
}

// parseShotUpload reads the multipart body, answering 413 for an oversized one.
//
// Parsed before any field is read: r.FormValue swallows the reader error, and
// an oversized upload would look like "no token" — which the publisher reads as
// "this share is gone" and uses to drop its whole queue.
func parseShotUpload(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, liveNoteShotMaxBytes)
	if err := r.ParseMultipartForm(liveNoteShotMaxBytes); err != nil { //nolint:gosec // bounded by MaxBytesReader
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeLocalizedErrorf(w, r, http.StatusRequestEntityTooLarge, "BadRequest")
			return false
		}
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return false
	}
	return true
}

// shotFields validates the two fields that make the row addressable.
func shotFields(w http.ResponseWriter, r *http.Request) (reason, clientShotID string, ok bool) {
	reason = strings.TrimSpace(r.FormValue("reason"))
	clientShotID = truncateRunes(strings.TrimSpace(r.FormValue("clientShotId")), liveNoteClientIDMaxRunes)
	if !service.ValidLiveNoteShotReason(reason) || clientShotID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return "", "", false
	}
	return reason, clientShotID, true
}

// readShotImage reads and sniffs the uploaded frame.
//
// The container is decided by the bytes, never by the client's label: the
// canvas may hand back PNG where WebP was requested, and a text file with a
// .webp name would only break the thumbnailer later, where nobody can connect
// it back to this request.
func readShotImage(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	file, _, err := r.FormFile("file")
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return nil, "", false
	}
	defer func() { _ = file.Close() }()
	img, err := io.ReadAll(io.LimitReader(file, liveNoteShotMaxBytes))
	if err != nil || len(img) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return nil, "", false
	}
	ext, ok := liveNoteShotExt(img)
	if !ok {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return nil, "", false
	}
	return img, ext, true
}

// shotQuotaAvailable enforces the per-meeting ceiling.
func shotQuotaAvailable(w http.ResponseWriter, r *http.Request, token string) bool {
	n, err := service.CountLiveNoteShots(token)
	if err != nil {
		slog.Error("live notes: count shots failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return false
	}
	if n >= service.LiveNoteShotMaxPerMeeting {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return false
	}
	return true
}

// shotMeeting loads the row the frame belongs to (title and start time are
// needed to create the folder).
func shotMeeting(w http.ResponseWriter, r *http.Request, token string) (service.LiveNote, bool) {
	note, exists, err := service.GetLiveNote(token)
	if err != nil {
		slog.Error("live notes: shot meeting lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return service.LiveNote{}, false
	}
	if !exists {
		http.NotFound(w, r)
		return service.LiveNote{}, false
	}
	return note, true
}

// shotUploadAt clamps the client's timestamp into the meeting window.
//
// Clock skew is normal across machines; a frame that claims to be from before
// the meeting started (or from the future) is clamped rather than rejected,
// because the picture itself is still real.
func shotUploadAt(r *http.Request, startedAt int64) int64 {
	at := parseLiveNoteMillis(r.FormValue("at"))
	if at <= 0 {
		at = time.Now().UnixMilli()
	}
	if startedAt > 0 && at < startedAt {
		at = startedAt
	}
	if now := time.Now().UnixMilli(); at > now {
		at = now
	}
	return at
}

// ServeLiveNotesShots handles GET /api/live-share/notes/shots?token=
// (authenticated, project-scoped): the meeting's frames with their offset on
// the transcript timeline.
//
// The offset is computed here, from the same origin the transcript uses, so the
// detail view can merge frames into the cue list without a second notion of
// "when did this meeting start".
func ServeLiveNotesShots(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	shots, err := service.ListLiveNoteShots(token)
	if err != nil {
		slog.Error("live notes: list shots failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	base := int64(0)
	if segments, segErr := service.ListLiveNoteSegments(token, 0); segErr == nil {
		base = service.LiveNotesTimelineBase(segments)
	}
	if base <= 0 {
		// No transcript at all (听记 was off): the meeting start is the only
		// origin that keeps offsets small and meaningful.
		if note, exists, noteErr := service.GetLiveNote(token); noteErr == nil && exists {
			base = note.StartedAt
		}
	}
	for i := range shots {
		offset := shots[i].At - base
		if base <= 0 || offset < 0 {
			offset = 0
		}
		shots[i].OffsetMS = offset
	}
	writeJSON(w, http.StatusOK, map[string]any{"shots": shots})
}

// serveLiveNoteShotDelete handles DELETE /api/live-share/notes/shot?token=&id=
// (authenticated, project-scoped): remove one frame, row first then file.
func serveLiveNoteShotDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodDelete) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	fileName, err := service.DeleteLiveNoteShot(token, projectID, id)
	if errors.Is(err, service.ErrMeetingNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("live notes: delete shot failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if dirName, nameErr := service.MeetingDirName(token); nameErr == nil && dirName != "" {
		paths := service.LiveNotesPathsIn(service.MeetingDirPath(projectPath, dirName))
		if rmErr := service.DeleteLiveNoteShotFile(paths, fileName); rmErr != nil {
			// The row is gone; a leftover file is invisible to every surface and
			// disappears with the meeting folder. Log it, do not fail the call.
			slog.Warn("live notes: remove shot file failed", "err", rmErr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// liveNoteShotExt sniffs the image container from its first bytes.
//
// The extension decides the stored file name, and the thumbnailer dispatches on
// it, so it has to describe the real content rather than the client's label.
func liveNoteShotExt(b []byte) (string, bool) {
	switch {
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "webp", true
	case len(b) >= 8 && bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "png", true
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg", true
	}
	return "", false
}

// parseLiveNoteInt parses a non-negative int, returning 0 for garbage.
func parseLiveNoteInt(raw string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// ServeLiveNotesChat handles GET /api/live-share/notes/sessions/chat?token=&since=
// (authenticated, project-scoped): the meeting's chat, oldest first.
func ServeLiveNotesChat(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	token := r.URL.Query().Get("token")
	if !validShareToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	messages, err := service.ListLiveNoteChat(token, since)
	if err != nil {
		slog.Error("live notes: list chat failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	count, err := service.CountLiveNoteChat(token)
	if err != nil {
		slog.Error("live notes: count chat failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{strMessages: messages, "count": count})
}

// ServeLiveShareNoteChat handles POST /api/live-share/notes/chat
// (authenticated, project-scoped): the publisher persists one chat line.
//
// Body: {"token","id","speaker","viewerId","text"}. `id` is the chat frame's own
// id and is the idempotency key — the publisher retries on failure, and a retry
// whose response was lost must not duplicate the line.
func ServeLiveShareNoteChat(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, liveNoteFilesMaxBodyBytes)
	var body struct {
		Token    string `json:"token"`
		ID       string `json:"id"`
		Speaker  string `json:"speaker"`
		ViewerID string `json:"viewerId"`
		Text     string `json:"text"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validShareToken(body.Token) || !liveNotesOwnedByProject(body.Token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	id, err := service.AppendLiveNoteChat(body.Token, projectID, service.LiveNoteChatMessage{
		ClientID: truncateRunes(strings.TrimSpace(body.ID), liveNoteClientIDMaxRunes),
		Speaker:  truncateRunes(strings.TrimSpace(body.Speaker), liveNoteSpeakerMaxRunes),
		ViewerID: truncateRunes(strings.TrimSpace(body.ViewerID), 32),
		Text:     truncateRunes(text, liveNoteChatMaxRunes),
	})
	if err != nil {
		slog.Error("live notes: append chat failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// ServeLiveShareNoteFiles handles POST /api/live-share/notes/files
// (authenticated, project-scoped): the publisher reports which files were
// opened, and when.
//
// Body: {"token","files":[{"owner","ownerName","path","openedAt","closedAt","source"}]}.
// Batched rather than one request per open: a busy meeting switches files a few
// times a minute, and a request per switch would put the whole event stream on
// the same connection the audio uploads use.
//
// The interval is the payload, not two events: the transcript has to say
// "10:03–10:11 opened X", and a close that never arrives (crash, killed app) is
// stamped by CloseOpenLiveNoteFiles when the meeting ends.
func ServeLiveShareNoteFiles(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	var body struct {
		Token string `json:"token"`
		Files []struct {
			Owner     string `json:"owner"`
			OwnerName string `json:"ownerName"`
			Path      string `json:"path"`
			OpenedAt  int64  `json:"openedAt"`
			ClosedAt  int64  `json:"closedAt"`
			Source    string `json:"source"`
		} `json:"files"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validShareToken(body.Token) || !liveNotesOwnedByProject(body.Token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	if len(body.Files) > liveNoteFilesMaxBatch {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	files := make([]service.LiveNoteFile, 0, len(body.Files))
	for _, f := range body.Files {
		path := truncateRunes(strings.TrimSpace(f.Path), liveNotePathMaxRunes)
		if path == "" || f.OpenedAt <= 0 {
			// One malformed entry must not discard the batch: the client retries
			// the whole thing, and dropping the rest would lose real events.
			continue
		}
		closedAt := f.ClosedAt
		if closedAt > 0 && closedAt < f.OpenedAt {
			closedAt = f.OpenedAt
		}
		files = append(files, service.LiveNoteFile{
			Owner:     strings.TrimSpace(f.Owner),
			OwnerName: strings.TrimSpace(f.OwnerName),
			Path:      path,
			OpenedAt:  f.OpenedAt,
			ClosedAt:  closedAt,
			Source:    strings.TrimSpace(f.Source),
		})
	}
	if err := service.RecordLiveNoteFiles(r.Context(), body.Token, projectID, files); err != nil {
		slog.Error("live notes: record files failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ServeLiveShareNoteScreen handles POST /api/live-share/notes/screen
// (authenticated, project-scoped): the publisher reports a CHANGE of who is
// sharing the screen.
//
// Body: {"token","sharer","sharerName","at"}. `sharer` is "host", "viewer:<id>",
// or "" for "nobody is sharing". Transitions rather than state: the server
// closes the previous interval and opens the next one in one transaction, so
// the timeline can never show two sharers at once.
//
// The publisher reports it because the publisher is the only party that sees
// every transition: the host's own picture, an audience member claiming the
// screen, a revocation, and a sharer who disconnects.
func ServeLiveShareNoteScreen(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	var body struct {
		Token      string `json:"token"`
		Sharer     string `json:"sharer"`
		SharerName string `json:"sharerName"`
		At         int64  `json:"at"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !validShareToken(body.Token) || !liveNotesOwnedByProject(body.Token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	sharer := truncateRunes(strings.TrimSpace(body.Sharer), liveNoteSharerMaxRunes)
	if sharer != "" && sharer != "host" && !strings.HasPrefix(sharer, "viewer:") {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	if err := service.SetLiveNoteScreenSharer(r.Context(), body.Token, projectID, sharer,
		truncateRunes(strings.TrimSpace(body.SharerName), liveNoteSpeakerMaxRunes), body.At); err != nil {
		slog.Error("live notes: record screen sharer failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// liveNotesPayload turns a stored row into the publisher-facing payload.
//
// Artifacts are resolved here rather than stored as four columns: the folder is
// the single fact, and the file names inside it are a convention.
func liveNotesPayload(projectPath string, note service.LiveNote) liveNotesMinutesPayload {
	payload := liveNotesMinutesPayload{
		Status:    note.Status,
		Markdown:  note.Minutes,
		Error:     note.Error,
		FilePath:  note.FilePath,
		Revise:    liveNotesRevisePayload{Status: note.ReviseStatus, Error: note.ReviseError},
		Artifacts: liveNoteArtifactsOf(service.MeetingDirPath(projectPath, note.DirName)),
	}
	// The artifact table is the authority. The legacy columns above are the
	// fallback for a store error, not a second opinion in the normal path: a
	// chain that starts with the transcript would otherwise leave the dialog
	// saying "idle" for the first two stages.
	states, err := service.ArtifactStatesForToken(note.Token)
	if err != nil {
		return payload
	}
	minutes := service.NormalizeArtifactState(states[service.ArtifactMinutes], 0)
	payload.Status = artifactPhase(minutes, note.Minutes)
	payload.Error = minutes.Error
	revised := service.NormalizeArtifactState(states[service.ArtifactRevised], 0)
	payload.Revise = liveNotesRevisePayload{
		Status: artifactPhase(revised, note.Revised),
		Error:  revised.Error,
	}
	return payload
}

// artifactPhase maps the two-axis artifact state onto the single phase the
// live panel already understands.
func artifactPhase(st service.ArtifactState, content string) string {
	switch st.JobStatus {
	case service.ArtifactJobQueued, service.ArtifactJobRunning:
		return service.LiveNotesStatusGenerating
	case service.ArtifactJobFailed:
		return service.LiveNotesStatusFailed
	}
	if st.ContentAt > 0 || strings.TrimSpace(content) != "" {
		return service.LiveNotesStatusReady
	}
	return service.LiveNotesStatusIdle
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
	// Count only the channel recordings: the folder also holds the .gitignore
	// that keeps the binary audio out of the repository, and the merged
	// whole-meeting file is not a channel.
	if entries, err := os.ReadDir(paths.Audio); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".webm") || e.Name() == service.LiveNoteMeetingAudioFile {
				continue
			}
			out.AudioFiles++
		}
	}
	if meeting := filepath.Join(paths.Audio, service.LiveNoteMeetingAudioFile); fileExists(meeting) {
		out.AudioMeeting = meeting
	}
	return out
}

// fileExists reports whether path is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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

	// Archive-only: the whole-meeting mix track exists to be listened to, not
	// transcribed. It has no single speaker, so a transcript of it would be a
	// duplicate of the per-channel lines with the labels lost.
	if r.FormValue("archive") != "" {
		writeJSON(w, http.StatusOK, map[string]any{"id": 0, liveNoteTextKey: ""})
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

// ServeLiveNotesAudioMerge handles POST /api/live-share/notes/audio/merge
// (authenticated, project-scoped). Body: {"token":"..."}.
//
// Folds the meeting's audio segments into one file per channel plus the
// whole-meeting mix, and removes the segments. The publisher calls this once
// its upload queue has drained — that is the only moment at which "no more
// audio is coming" is actually true.
func ServeLiveNotesAudioMerge(w http.ResponseWriter, r *http.Request) {
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
	dirName, err := service.MeetingDirName(body.Token)
	if err != nil {
		slog.Error("live notes: merge dir lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if dirName == "" {
		// Nothing was recorded (notes off, or every segment was silence).
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "artifacts": liveNotesArtifacts{}})
		return
	}
	dir := service.MeetingDirPath(projectPath, dirName)
	merged, err := service.MergeLiveNoteAudio(service.LiveNotesPathsIn(dir))
	if err != nil {
		slog.Warn("live notes: merge audio failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	slog.Info("live notes: merged meeting audio",
		"segments", merged.Segments, "channels", len(merged.Channels), "meeting", merged.Meeting != "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "artifacts": liveNoteArtifactsOf(dir)})
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
	// "生成纪要" must not queue two runs over the same transcript.
	ready, readyErr := liveNotesMinutesReady(body.Token, note)
	if readyErr != nil {
		slog.Error("live notes: generation state lookup failed", "err", readyErr)
		model.WriteError(w, model.Internal(readyErr))
		return
	}
	if !body.Regenerate && ready {
		writeJSON(w, http.StatusOK, liveNotesPayload(projectPath, note))
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
	plan := service.PlanForInitialJob()
	claimed, claimErr := service.ClaimArtifactJob(body.Token, artifactPlanKinds(plan), 0)
	if claimErr != nil {
		slog.Error("live notes: claim minutes job failed", "err", claimErr)
		model.WriteError(w, model.Internal(claimErr))
		return
	}
	if claimed {
		go runArtifactChain(body.Token, projectID, projectPath, title, plan, false)
	}

	// The payload reads the artifact table, so it already says "generating"
	// even though the minutes step is still queued behind transcript/proofread.
	writeJSON(w, http.StatusOK, liveNotesPayload(projectPath, note))
}

// liveNotesMinutesReady reports whether the post-meeting job already produced
// the minutes or is already running them.
//
// The legacy projection is the cheap "already ready" check; the artifact table
// is the authority for "is something running" (the claim below).
func liveNotesMinutesReady(token string, note service.LiveNote) (bool, error) {
	running, err := service.LiveNoteGenerationRunning(token, 0)
	if err != nil {
		return false, err
	}
	return running || note.Status == service.LiveNotesStatusReady, nil
}

// artifactPlanKinds lists the artifacts a plan touches, in dependency order.
func artifactPlanKinds(plan []service.RegenStep) []service.ArtifactKind {
	kinds := make([]service.ArtifactKind, 0, len(plan))
	for _, step := range plan {
		kinds = append(kinds, step.Kind)
	}
	return kinds
}

// liveNotesStoredTitle returns the title actually stored for the meeting, or
// fallback when the row is gone or still has no title.
func liveNotesStoredTitle(token, fallback string) string {
	row, ok, err := service.GetLiveNote(token)
	if err != nil || !ok || strings.TrimSpace(row.Title) == "" {
		return fallback
	}
	return row.Title
}

// liveNotesEndTime returns the stored end time, or 0 when the meeting is still
// running (or the row is gone). The minutes prompt uses it to say how long the
// meeting took; a running meeting has no duration to report.
func liveNotesEndTime(token string) int64 {
	row, ok, err := service.GetLiveNote(token)
	if err != nil || !ok {
		return 0
	}
	return row.EndedAt
}

// ── Regeneration chain ─────────────────────────────────────────────────────

// artifactChain is the shared state of one chain run: the meeting folder, the
// file/screen timeline, the segments and the text produced by earlier steps.
//
// It exists so each step is a small method with the same inputs, and so a chain
// that runs transcript → proofread → minutes reads the folder once.
type artifactChain struct {
	token       string
	projectID   int64
	projectPath string
	title       string
	strict      bool

	paths    service.LiveNotesPaths
	onDisk   bool
	files    []service.LiveNoteFile
	screens  []service.LiveNoteScreenShare
	segments []service.LiveNoteSegment
	self     service.LiveNotesSpeaker

	// Context the transcript cannot carry. Loaded once in prepare() and consumed
	// by the minutes step; the proofreader gets its own vocabulary from the
	// files and segments instead.
	chat    []service.LiveNoteChatMessage
	shots   []service.LiveNoteShot
	prev    service.LiveNote
	hasPrev bool
	raw     string
	revised string
}

func newArtifactChain(token string, projectID int64, projectPath, title string, strict bool) *artifactChain {
	return &artifactChain{
		token: token, projectID: projectID, projectPath: projectPath,
		title: title, strict: strict,
	}
}

// prepare resolves everything the steps share. A failure to create the folder is
// not fatal: the transcript/minutes can still be stored in the database, and the
// durable copies are the only thing lost.
func (c *artifactChain) prepare() {
	c.refreshSegments()
	startedAt := liveNotesStartTime(c.segments)
	c.title = liveNotesStoredTitle(c.token, c.title)
	paths, err := service.EnsureMeetingDir(c.token, c.projectID, c.projectPath, c.title, startedAt.UnixMilli())
	if err != nil {
		slog.Warn("live notes: meeting folder unavailable", "err", err)
	}
	c.paths = paths
	c.onDisk = err == nil
	if c.onDisk {
		// Fold the segments into one recording per channel before anything else
		// reads the folder. This is also the retry path for a publisher whose
		// merge request never arrived.
		merged, mergeErr := service.MergeLiveNoteAudio(paths)
		if mergeErr != nil {
			slog.Warn("live notes: merge audio failed", "err", mergeErr)
		} else if merged.Segments > 0 {
			slog.Info("live notes: merged meeting audio", "segments", merged.Segments)
		}
		recordMergedAudioChannels(c.token, merged)
	}
	files, filesErr := service.ListLiveNoteFiles(c.token, 0)
	if filesErr != nil {
		// The transcript and the minutes are worth producing without the file
		// timeline; losing it is a gap, not a reason to fail the meeting.
		slog.Warn("live notes: list files failed", "err", filesErr)
	}
	c.files = files
	screens, screensErr := service.ListLiveNoteScreenShares(c.token)
	if screensErr != nil {
		slog.Warn("live notes: list screen shares failed", "err", screensErr)
	}
	c.screens = screens
	// Chat, key frames and the previous meeting are the same kind of input: they
	// make the minutes better and none of them is worth failing the meeting over.
	if chat, chatErr := service.ListLiveNoteChat(c.token, 0); chatErr != nil {
		slog.Warn("live notes: list chat failed", "err", chatErr)
	} else {
		c.chat = chat
	}
	if frames, shotErr := service.ListLiveNoteShots(c.token); shotErr != nil {
		slog.Warn("live notes: list shots failed", "err", shotErr)
	} else {
		c.shots = frames
	}
	if prev, ok, prevErr := service.PreviousLiveNoteMinutes(c.projectID, startedAt.UnixMilli(), c.token); prevErr != nil {
		slog.Warn("live notes: previous meeting lookup failed", "err", prevErr)
	} else if ok {
		c.prev, c.hasPrev = prev, true
	}
}

// refreshSegments re-reads the transcript rows. The ASR step replaces them, so
// every step after it must see the new rows rather than the pre-chain snapshot.
func (c *artifactChain) refreshSegments() {
	segments, err := service.ListLiveNoteSegments(c.token, 0)
	if err != nil {
		slog.Warn("live notes: list segments failed", "err", err)
		segments = nil
	}
	c.segments = segments
	c.self = service.LiveNotesSpeaker{
		Label: service.LiveNotesSelfLabel(segments),
		Name:  model.UserNickname,
	}
}

// run executes the plan. Status writes are the only authority: the chain is the
// single writer, so cancellation (which only asks the context to stop) cannot
// race it.
func (c *artifactChain) run(ctx context.Context, steps []service.RegenStep) {
	for _, step := range steps {
		if ctx.Err() != nil {
			c.abandon()
			return
		}
		c.markRunning(step)
		stepCtx, cancel := context.WithTimeout(ctx, artifactStepTimeout(step.Kind))
		err := c.runStep(stepCtx, step)
		cancel()
		if err == nil {
			continue
		}
		if ctx.Err() == context.Canceled {
			c.abandon()
			return
		}
		c.markFailed(step, err)
		if resetErr := service.ResetQueuedArtifacts(c.token, 0); resetErr != nil {
			slog.Warn("live notes: reset queued steps failed", "err", resetErr)
		}
		// Manual regeneration is strict: the user asked for this level, and
		// quietly summarizing the raw transcript instead would hide the failure.
		// The initial post-meeting job keeps the old fallback (a summary of an
		// imperfect transcript beats no summary at all).
		if c.strict || step.Kind != service.ArtifactRevised {
			c.emit(step.Kind, false)
			return
		}
		slog.Warn("live notes: proofread failed; minutes use the raw transcript", "err", err)
		c.revised = c.raw
	}
	c.emit(steps[len(steps)-1].Kind, true)
}

func (c *artifactChain) runStep(ctx context.Context, step service.RegenStep) error {
	switch step.Kind {
	case service.ArtifactTranscript:
		return c.buildTranscript(ctx, step.Mode)
	case service.ArtifactRevised:
		return c.buildRevised(ctx)
	case service.ArtifactMinutes:
		return c.buildMinutes(ctx)
	default:
		return fmt.Errorf("live notes: unknown artifact kind %q", step.Kind)
	}
}

func (c *artifactChain) markRunning(step service.RegenStep) {
	var err error
	switch step.Kind {
	case service.ArtifactTranscript:
		err = service.SetArtifactRunning(c.token, step.Kind, "", 0)
	case service.ArtifactRevised:
		err = service.SetLiveNoteRevise(c.token, c.projectID, service.LiveNotesStatusGenerating, "", "")
	case service.ArtifactMinutes:
		err = service.SetLiveNoteMinutes(c.token, c.projectID, c.title, service.LiveNotesStatusGenerating, "", "", "")
	}
	if err != nil {
		slog.Warn("live notes: mark step running failed", "kind", step.Kind, "err", err)
	}
}

func (c *artifactChain) markFailed(step service.RegenStep, cause error) {
	code := artifactErrorCode(step.Kind, cause)
	msg := cause.Error()
	var err error
	switch step.Kind {
	case service.ArtifactTranscript:
		err = service.SetArtifactFailed(c.token, step.Kind, code, msg, 0)
	case service.ArtifactRevised:
		err = service.SetLiveNoteRevise(c.token, c.projectID, service.LiveNotesStatusFailed, "", msg)
	case service.ArtifactMinutes:
		err = service.SetLiveNoteMinutes(c.token, c.projectID, c.title, service.LiveNotesStatusFailed, "", msg, "")
	}
	if err != nil {
		slog.Error("live notes: persist step failure failed", "kind", step.Kind, "err", err)
	}
}

// abandon settles a cancelled chain. Cancellation is not a failure: the content
// the artifact already had stays, and the user is not shown an error they
// caused.
func (c *artifactChain) abandon() {
	if err := service.ResetActiveArtifacts(c.token, 0); err != nil {
		slog.Warn("live notes: reset cancelled artifacts failed", "err", err)
	}
}

func (c *artifactChain) emit(kind service.ArtifactKind, ok bool) {
	eventType := ""
	switch kind {
	case service.ArtifactTranscript:
		if ok {
			eventType = service.MeetingEventTranscriptReady
		} else {
			eventType = service.MeetingEventTranscriptFailed
		}
	case service.ArtifactRevised:
		if ok {
			eventType = service.MeetingEventRevisedReady
		} else {
			eventType = service.MeetingEventRevisedFailed
		}
	case service.ArtifactMinutes:
		if ok {
			eventType = service.MeetingEventMinutesReady
		} else {
			eventType = service.MeetingEventMinutesFailed
		}
	}
	if eventType == "" {
		return
	}
	service.EmitMeetingEvent(c.token, c.projectPath, eventType, c.title, "", "")
}

// buildTranscript renders the VTT from the segments (mode=render) or re-runs
// recognition on the recording (mode=asr, implemented in live_notes_reasr.go).
func (c *artifactChain) buildTranscript(ctx context.Context, mode string) error {
	if mode == service.RegenModeASR {
		return c.buildTranscriptASR(ctx)
	}
	c.refreshSegments()
	raw := service.BuildLiveNotesTranscript(c.segments)
	if strings.TrimSpace(raw) == "" {
		return service.ErrLiveNotesEmpty
	}
	c.raw = raw
	changed := true
	if c.onDisk {
		body := service.BuildLiveNotesVTT(c.segments, c.files, c.screens)
		if old, readErr := os.ReadFile(c.paths.Transcript); readErr == nil && string(old) == body {
			changed = false
		}
		if _, err := service.WriteLiveNotesRawTranscript(c.paths, c.segments, c.files, c.screens); err != nil {
			// The transcript is file-only, so this artifact really is missing.
			// It must not take the minutes down with it: the initial job keeps
			// going on the text it already has, exactly like the proofread
			// failure path. A manual transcript regeneration is strict and
			// reports the failure.
			slog.Warn("live notes: write transcript failed", "err", err)
			if markErr := service.SetArtifactFailed(c.token, service.ArtifactTranscript, service.ArtifactErrIO, err.Error(), 0); markErr != nil {
				slog.Error("live notes: persist transcript failure failed", "err", markErr)
			}
			if c.strict {
				return fmt.Errorf("write transcript: %w", err)
			}
			return nil
		}
	}
	contentAt := int64(0)
	if changed && c.onDisk {
		contentAt = time.Now().UnixMilli()
	}
	return service.SetArtifactSucceeded(c.token, service.ArtifactTranscript, contentAt, 0)
}

// buildRevised runs the proofreading pass and stores both the database copy and
// the Markdown file. The file write is best-effort: the content is already in
// the database, so a read-only project folder must not fail the meeting.
func (c *artifactChain) buildRevised(ctx context.Context) error {
	if strings.TrimSpace(c.raw) == "" {
		c.refreshSegments()
		c.raw = service.BuildLiveNotesTranscript(c.segments)
	}
	progress := func(done, total int) {
		if err := service.HeartbeatArtifact(c.token, service.ArtifactRevised, fmt.Sprintf("%d/%d", done, total), 0); err != nil {
			slog.Warn("live notes: revise heartbeat failed", "err", err)
		}
	}
	revised, err := service.ReviseLiveNotesTranscript(ctx, service.LiveNotesReviseInput{
		ProjectPath: c.projectPath,
		Title:       c.title,
		Files:       c.files,
		Segments:    c.segments,
		Self:        c.self,
	}, progress)
	if err != nil {
		return err
	}
	c.revised = revised
	if err := service.SetLiveNoteRevise(c.token, c.projectID, service.LiveNotesStatusReady, revised, ""); err != nil {
		slog.Error("live notes: persist proofread failed", "err", err)
	}
	if c.onDisk {
		// Merged only into the FILE, never into the DB copy or the minutes
		// input: the proofread text is speech, and the summarizer gets the file
		// timeline as its own explicitly-framed block.
		body := service.MergeLiveNoteFileEvents(revised, c.files, c.screens, service.LiveNotesTimelineBase(c.segments))
		writeLiveNotesArtifact("revised", func() (string, error) {
			return service.WriteLiveNotesRevisedTranscript(c.paths, c.title, liveNotesStartTime(c.segments), body)
		})
	}
	return nil
}

// buildMinutes summarizes the proofread text. The database copy is the input
// (the chain reads database text, not the exported file); when this chain did
// not produce a proofread, the stored one is used, and a raw transcript is the
// last resort.
func (c *artifactChain) buildMinutes(ctx context.Context) error {
	text := c.revised
	if strings.TrimSpace(text) == "" {
		if note, ok, err := service.GetLiveNote(c.token); err == nil && ok {
			text = note.Revised
		}
	}
	if strings.TrimSpace(text) == "" {
		if strings.TrimSpace(c.raw) == "" {
			c.refreshSegments()
			c.raw = service.BuildLiveNotesTranscript(c.segments)
		}
		text = c.raw
	}
	if strings.TrimSpace(text) == "" {
		return service.ErrLiveNotesEmpty
	}

	startedAt := liveNotesStartTime(c.segments)
	// Everything the meeting knows about itself, in one block: framing, chat,
	// what was on screen, and what the previous meeting left open. Assembled by
	// the service so the order is a single decision rather than a call-site
	// convention.
	contextSection := service.LiveNoteMinutesContext{
		Info: service.LiveNoteMeetingInfo{
			ProjectName: filepath.Base(c.projectPath),
			ProjectPath: c.projectPath,
			StartedAt:   startedAt.UnixMilli(),
			EndedAt:     liveNotesEndTime(c.token),
			Segments:    c.segments,
			Screens:     c.screens,
			Self:        c.self,
		},
		Chat:    c.chat,
		Files:   c.files,
		Screens: c.screens,
		Shots:   c.shots,
		Prev:    c.prev,
		HasPrev: c.hasPrev,
	}.PromptSection()
	progress := func(done, total int, phase string) {
		detail := fmt.Sprintf("%d/%d", done, total)
		if phase == "final" {
			detail = service.LiveNoteSummarizing()
		}
		if err := service.HeartbeatArtifact(c.token, service.ArtifactMinutes, detail, 0); err != nil {
			slog.Warn("live notes: minutes heartbeat failed", "err", err)
		}
	}
	minutes, err := service.GenerateLiveNotesMinutes(ctx, c.title, text, contextSection, c.self, progress)
	if err != nil {
		return err
	}

	// Now that the meeting has been summarized there is something to name it
	// after. Only a placeholder is replaced — a title the user typed is theirs.
	// Done before the file is written so the minutes.md heading carries the
	// real name instead of "11-01 17:35".
	title := nameMeetingFromMinutes(ctx, c.title, minutes, service.GenerateLiveNotesTitle)

	// A failed write must not lose the minutes: they are still stored below, and
	// the UI simply shows no file path.
	filePath := ""
	if c.onDisk {
		filePath = writeLiveNotesMinutes(c.paths, title, startedAt, minutes)
	}
	if err := service.SetLiveNoteMinutes(c.token, c.projectID, title, service.LiveNotesStatusReady, minutes, "", filePath); err != nil {
		slog.Error("live notes: persist minutes failed", "err", err)
	}
	c.title = title
	return nil
}

// nameMeetingFromMinutes replaces a placeholder title with one the summary
// model derived from the minutes, and returns the title to use from here on.
//
// `generate` is a parameter rather than a direct call so the decision — is this
// still a placeholder, and what happens when the model fails — is testable
// without a model.
func nameMeetingFromMinutes(ctx context.Context, title, minutes string, generate func(context.Context, string) (string, error)) string {
	if !service.IsPlaceholderMeetingTitle(title) {
		return title
	}
	named, err := generate(ctx, minutes)
	if err != nil {
		slog.Warn("live notes: title generation failed", "err", err)
		return title
	}
	if named = strings.TrimSpace(named); named == "" {
		return title
	}
	return named
}

// runArtifactChain is the detached job behind both the initial post-meeting
// generation and every manual regeneration.
func runArtifactChain(token string, projectID int64, projectPath, title string, steps []service.RegenStep, strict bool) {
	if len(steps) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveNoteChainTimeout)
	registerLiveNoteJob(token, cancel)
	defer func() {
		unregisterLiveNoteJob(token)
		cancel()
	}()

	chain := newArtifactChain(token, projectID, projectPath, title, strict)
	chain.prepare()
	chain.run(ctx, steps)
}

// artifactStepTimeout is the budget for one step. Recognition is the long one;
// the model passes share a smaller budget.
func artifactStepTimeout(kind service.ArtifactKind) time.Duration {
	if kind == service.ArtifactTranscript {
		return liveNoteASRTimeout
	}
	return liveNoteModelTimeout
}

// artifactErrorCode maps a step failure onto the code the SPA localizes.
func artifactErrorCode(kind service.ArtifactKind, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return service.ArtifactErrTimeout
	case errors.Is(err, service.ErrLiveNotesEmpty):
		return service.ArtifactErrNoSource
	case errors.Is(err, service.ErrSummaryModelNotConfigured):
		return service.ArtifactErrModel
	case errors.Is(err, service.ErrLiveNoteSTT):
		return service.ArtifactErrSTT
	case errors.Is(err, service.ErrLiveNoteAudioMissing):
		return service.ArtifactErrAudio
	}
	if kind == service.ArtifactTranscript {
		return service.ArtifactErrIO
	}
	return service.ArtifactErrModel
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
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".webm") || e.Name() == service.LiveNoteMeetingAudioFile {
				continue
			}
			audioFiles++
		}
	}
	hasMeeting := fileExists(filepath.Join(paths.Audio, service.LiveNoteMeetingAudioFile))
	written, err := service.WriteLiveNotesMinutesFile(paths, title, startedAt, minutes, audioFiles, hasMeeting)
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
// The token stays readable after the meeting ends: the room is only the live
// transport, the meeting RECORD is permanent. Once the publisher stops, the
// same URL answers with the archived transcript, minutes, proofread text and
// chat until the owner deletes the record — which is what makes a link handed
// out in advance still worth opening the next day.
//
// `?full=1` is the record view: it adds the proofread text and the archived
// chat. The live poll deliberately omits them (it runs every few seconds and
// must not re-transfer a growing message list on each tick).
func ServeShareLiveNotes(w http.ResponseWriter, r *http.Request, token string) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if !validShareToken(token) {
		http.NotFound(w, r)
		return
	}
	// A live room is the fast path, but it is NOT the only valid state: after
	// the room is gone the record still owns the token.
	live := liveRegistry.Get(token) != nil
	note, exists, err := service.PublicMeetingByToken(token)
	if err != nil {
		slog.Error("live notes: public get failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	if !live && !exists {
		http.NotFound(w, r)
		return
	}
	full := r.URL.Query().Get("full") != ""
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	segments, err := service.ListLiveNoteSegments(token, since)
	if err != nil {
		slog.Error("live notes: public list failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	files, err := service.ListLiveNoteFiles(token, 0)
	if err != nil {
		slog.Error("live notes: public files failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	screens, err := service.ListLiveNoteScreenShares(token)
	if err != nil {
		slog.Error("live notes: public screen shares failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return
	}
	var chat []liveNotePublicChat
	if full {
		messages, chatErr := service.ListLiveNoteChat(token, 0)
		if chatErr != nil {
			// Chat is not an official artifact of the meeting: losing it must
			// not cost the reader the minutes and the transcript.
			slog.Warn("live notes: public chat failed", "err", chatErr)
		}
		for _, m := range messages {
			chat = append(chat, liveNotePublicChat{Speaker: m.Speaker, Text: m.Text, At: m.At})
		}
	}
	revise := liveNotesRevisePayload{Status: note.ReviseStatus, Error: note.ReviseError}
	if full {
		revise.Text = note.Revised
	}
	// Frames are part of the record a member opens after the meeting: the
	// transcript reads the same for everyone, pictures included. Only the full
	// (record) view carries them — the live poll runs every few seconds and a
	// viewer already watches the picture itself.
	var shots []service.LiveNoteShot
	if full {
		listed, shotErr := service.ListLiveNoteShots(token)
		if shotErr != nil {
			// Like chat: losing the frames must not cost the reader the minutes
			// and the transcript.
			slog.Warn("live notes: public shots failed", "err", shotErr)
		}
		shots = redactLiveNoteShots(listed)
	}
	writeJSON(w, http.StatusOK, liveNotesResponse{
		Title:     note.Title,
		StartedAt: note.StartedAt,
		EndedAt:   note.EndedAt,
		Ended:     !live,
		Segments:  segments,
		Files:     redactLiveNoteFilePaths(files),
		Screens:   screens,
		Shots:     shots,
		Chat:      chat,
		Minutes: liveNotesMinutesPayload{
			Status:   note.Status,
			Markdown: note.Minutes,
			Error:    note.Error,
			// FilePath and Artifacts deliberately omitted: viewers get the text,
			// not paths into the publisher's project.
			Revise: revise,
		},
	})
}

// ── helpers ─────────────────────────────────────────────────────────────────

// redactLiveNoteFilePaths drops the full path, leaving the base name.
//
// A viewer may know WHICH file was opened — that is what makes the transcript
// followable — but not where the publisher keeps it. The name is what the
// transcript line renders, so nothing visible is lost.
func redactLiveNoteFilePaths(files []service.LiveNoteFile) []service.LiveNoteFile {
	if len(files) == 0 {
		return nil
	}
	out := make([]service.LiveNoteFile, len(files))
	copy(out, files)
	for i := range out {
		out[i].Path = ""
	}
	return out
}

// redactLiveNoteShots makes a stored frame safe to hand a viewer.
//
// The id is the address (the image endpoint resolves the file server-side), so
// the stored relative name is dropped. The ref keeps only its base name — the
// same rule the file timeline follows: a viewer may know WHICH file was opened,
// but not where the publisher keeps it. Both separators are handled because the
// publisher may be on Windows.
func redactLiveNoteShots(shots []service.LiveNoteShot) []service.LiveNoteShot {
	if len(shots) == 0 {
		return nil
	}
	out := make([]service.LiveNoteShot, len(shots))
	copy(out, shots)
	for i := range out {
		out[i].FileName = ""
		if ref := out[i].Ref; ref != "" {
			if idx := strings.LastIndexAny(ref, `\/`); idx >= 0 {
				out[i].Ref = ref[idx+1:]
			}
		}
	}
	return out
}

// ServeShareLiveNoteShotImage handles GET /api/share/{token}/notes/shot?id=&w=
// (public, token-scoped): one stored key frame, resized like a file thumbnail.
//
// The token is the credential, exactly as for the notes payload itself, and the
// file is located from the database row — the client never supplies a path.
func ServeShareLiveNoteShotImage(w http.ResponseWriter, r *http.Request, token string) {
	if !requireMethod(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id <= 0 || !validShareToken(token) {
		http.NotFound(w, r)
		return
	}
	absPath, found, lookupErr := service.LiveNoteShotFile(token, id)
	if lookupErr != nil {
		slog.Error("live notes: shot file lookup failed", "err", lookupErr)
		model.WriteError(w, model.Internal(lookupErr))
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	info, statErr := os.Stat(absPath)
	if statErr != nil || info.IsDir() || info.Size() > thumbMaxFileSize || !isThumbDecodable(absPath) {
		http.NotFound(w, r)
		return
	}
	serveThumbOfFile(w, r, absPath, info)
}

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

// validLiveNoteChannel accepts "system", "mic", the synthetic "mix" archive
// track, or "viewer:<id>".
func validLiveNoteChannel(ch string) bool {
	switch ch {
	case "system", "mic", service.LiveNoteMixChannel:
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
	paths, err := service.EnsureMeetingDir(token, projectID, projectPath, title, startedAt)
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
