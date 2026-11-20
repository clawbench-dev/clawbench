package live

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawbench/internal/httpx"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/sharecore"
)

const (
	// liveNoteRecordingChunkMaxBytes caps one uploaded chunk. The client cuts
	// the recorder every two seconds, so this is ~10× the real size and still
	// small enough that a broken client cannot stream a file through one
	// request.
	liveNoteRecordingChunkMaxBytes = 16 << 20
	// liveNoteRecordingMaxSessions bounds the in-memory table. Sessions are
	// removed when the recorder stops; leftovers only exist when the client
	// crashed, and a handful of those must not accumulate forever.
	liveNoteRecordingMaxSessions = 64
	// liveNoteRecordingSessionTTL drops a session whose client never came back.
	liveNoteRecordingSessionTTL = 6 * time.Hour
)

// liveRecordingSession is one in-flight recording file.
//
// The file itself is the artifact; this struct only remembers where to append
// and which chunk comes next, so a retried upload cannot duplicate bytes in the
// middle of a video (which would corrupt every frame after it).
type liveRecordingSession struct {
	fileName string
	nextSeq  int
	bytes    int64
	started  time.Time
}

var (
	liveRecordingMu       sync.Mutex
	liveRecordingSessions = map[string]*liveRecordingSession{}
)

func liveRecordingKey(token, id string) string { return token + "\x00" + id }

// dropLiveNoteRecordingSessions forgets every recording of one meeting.
//
// Called when the meeting ends: the files are already complete (or were cut
// short by the crash that ended the meeting), and a session left behind would
// only make the next recording with the same id append to the old file.
func dropLiveNoteRecordingSessions(token string) {
	liveRecordingMu.Lock()
	defer liveRecordingMu.Unlock()
	for key := range liveRecordingSessions {
		if strings.HasPrefix(key, token+"\x00") {
			delete(liveRecordingSessions, key)
		}
	}
}

func pruneLiveRecordingSessions(now time.Time) {
	for key, sess := range liveRecordingSessions {
		if now.Sub(sess.started) > liveNoteRecordingSessionTTL {
			delete(liveRecordingSessions, key)
		}
	}
}

// ServeLiveShareNoteRecording appends one chunk of a screen recording.
//
// POST /api/live-share/notes/recording?token=&recordingId=&seq=&at=
// Body: the raw bytes of one MediaRecorder chunk.
//
// Raw bytes rather than multipart: a chunk is already a self-contained slice of
// the container, and multipart would add a boundary scan to every two-second
// upload for no benefit. The name is decided by the server, never the client.
//
// The append is ordered and idempotent per (token, recordingId, seq): a retry
// after a lost response returns the current state instead of writing the chunk
// twice, and a gap is refused because a missing chunk in the middle of a WebM
// file makes every later frame undecodable.
func ServeLiveShareNoteRecording(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	token := query.Get("token")
	if !sharecore.ValidToken(token) || !liveNotesOwnedByProject(token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	id := query.Get("recordingId")
	if !service.ValidLiveNoteRecordingID(id) {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	seq, err := strconv.Atoi(query.Get("seq"))
	if err != nil || seq < 0 {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}

	paths, ok := liveRecordingMeetingDir(w, r, token, projectID, projectPath)
	if !ok {
		return
	}

	chunk, ok := readRecordingChunk(w, r)
	if !ok {
		return
	}
	at := parseLiveNoteMillis(query.Get("at"))
	if at <= 0 {
		at = time.Now().UnixMilli()
	}

	fileName, bytes, nextSeq, status, err := appendLiveRecordingChunk(paths, token, id, seq, at, chunk)
	switch status {
	case http.StatusOK:
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"fileName": fileName,
			"bytes":    bytes,
			"nextSeq":  nextSeq,
		})
	case http.StatusConflict:
		// The client stops on this: its queue is ordered, so a gap means the
		// server lost the session (restart) and the file cannot be completed.
		httpx.WriteJSON(w, status, map[string]string{strReqError: "recording gap"})
	case http.StatusRequestEntityTooLarge:
		httpx.WriteLocalizedErrorf(w, r, http.StatusRequestEntityTooLarge, "BadRequest")
	case http.StatusTooManyRequests:
		httpx.WriteLocalizedErrorf(w, r, http.StatusTooManyRequests, "BadRequest")
	default:
		slog.Error("live notes: append recording failed", "err", err)
		model.WriteError(w, model.Internal(err))
	}
}

// liveRecordingMeetingDir resolves the folder the chunks are appended to,
// answering the request itself when the meeting is gone or its folder cannot be
// created. Split out of the handler because both failures are "log and return
// 500-ish", which is what pushed the handler over the complexity budget.
func liveRecordingMeetingDir(w http.ResponseWriter, r *http.Request, token string, projectID int64, projectPath string) (service.LiveNotesPaths, bool) {
	note, exists, err := service.GetLiveNote(token)
	if err != nil {
		slog.Error("live notes: recording meeting lookup failed", "err", err)
		model.WriteError(w, model.Internal(err))
		return service.LiveNotesPaths{}, false
	}
	if !exists {
		http.NotFound(w, r)
		return service.LiveNotesPaths{}, false
	}
	paths, err := service.EnsureMeetingDir(token, projectID, projectPath, note.Title, note.StartedAt)
	if err != nil {
		slog.Warn("live notes: meeting folder unavailable for recording", "err", err)
		model.WriteError(w, model.Internal(err))
		return service.LiveNotesPaths{}, false
	}
	return paths, true
}

// readRecordingChunk reads the raw body, answering 413 for an oversized one.
func readRecordingChunk(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, liveNoteRecordingChunkMaxBytes)
	chunk, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteLocalizedErrorf(w, r, http.StatusRequestEntityTooLarge, "BadRequest")
			return nil, false
		}
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return nil, false
	}
	if len(chunk) == 0 {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return nil, false
	}
	return chunk, true
}

// appendLiveRecordingChunk writes one ordered chunk and returns the file's
// state plus the HTTP status to answer with.
//
// A 409 means "gap" and carries no error: it is a fact about the client's
// stream, not a server failure, and the caller answers it without the
// internal-error envelope. Every other non-200 status has an error attached.
func appendLiveRecordingChunk(
	paths service.LiveNotesPaths,
	token, id string,
	seq int,
	at int64,
	chunk []byte,
) (fileName string, bytes int64, nextSeq int, status int, err error) {
	liveRecordingMu.Lock()
	defer liveRecordingMu.Unlock()

	pruneLiveRecordingSessions(time.Now())
	key := liveRecordingKey(token, id)
	sess := liveRecordingSessions[key]
	if sess == nil {
		if seq != 0 {
			// A session that starts at seq>0 is missing its first chunk, which
			// carries the container header. The file would be unplayable.
			return "", 0, 0, http.StatusConflict, nil
		}
		if len(liveRecordingSessions) >= liveNoteRecordingMaxSessions {
			return "", 0, 0, http.StatusTooManyRequests, errors.New("too many recording sessions")
		}
		if err := service.EnsureLiveNoteRecordingDir(paths); err != nil {
			return "", 0, 0, http.StatusInternalServerError, err
		}
		name := service.LiveNoteRecordingFileName(at, id)
		// O_EXCL: a leftover file with the same name means this id was already
		// used, and appending to it would mix two recordings.
		path := filepath.Join(paths.Dir, name)
		file, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if createErr != nil {
			return "", 0, 0, http.StatusConflict, createErr
		}
		_ = file.Close()
		sess = &liveRecordingSession{fileName: name, nextSeq: 0, started: time.Now()}
		liveRecordingSessions[key] = sess
	}
	if seq < sess.nextSeq {
		// A retry of a chunk that is already in the file. Reporting success is
		// what lets the client move on instead of retrying forever.
		return sess.fileName, sess.bytes, sess.nextSeq, http.StatusOK, nil
	}
	if seq > sess.nextSeq {
		return "", 0, 0, http.StatusConflict, nil
	}
	if sess.bytes+int64(len(chunk)) > service.LiveNoteRecordingMaxBytes {
		delete(liveRecordingSessions, key)
		return "", 0, 0, http.StatusRequestEntityTooLarge, errors.New("recording too large")
	}
	path := filepath.Join(paths.Dir, sess.fileName)
	file, openErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if openErr != nil {
		return "", 0, 0, http.StatusInternalServerError, openErr
	}
	n, writeErr := file.Write(chunk)
	closeErr := file.Close()
	if writeErr != nil {
		return "", 0, 0, http.StatusInternalServerError, writeErr
	}
	if closeErr != nil {
		return "", 0, 0, http.StatusInternalServerError, closeErr
	}
	sess.nextSeq++
	sess.bytes += int64(n)
	return sess.fileName, sess.bytes, sess.nextSeq, http.StatusOK, nil
}

// ServeLiveShareNoteRecordingFinish closes a recording session.
//
// POST /api/live-share/notes/recording/finish  {"token":"...","recordingId":"..."}
//
// Idempotent: a second call (or one that arrives after the meeting was closed
// and the sessions were dropped) answers with an empty file name instead of an
// error. The file itself is complete as soon as the last chunk was written —
// this call exists so the server can stop remembering the session and so the
// publisher can show where the file landed.
func ServeLiveShareNoteRecordingFinish(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	projectPath, projectID, ok := liveNotesProject(w, r)
	if !ok {
		return
	}
	var body struct {
		Token       string `json:"token"`
		RecordingID string `json:"recordingId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}
	if !sharecore.ValidToken(body.Token) || !liveNotesOwnedByProject(body.Token, projectPath, projectID) {
		http.NotFound(w, r)
		return
	}
	if !service.ValidLiveNoteRecordingID(body.RecordingID) {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "BadRequest")
		return
	}

	liveRecordingMu.Lock()
	key := liveRecordingKey(body.Token, body.RecordingID)
	sess := liveRecordingSessions[key]
	delete(liveRecordingSessions, key)
	liveRecordingMu.Unlock()

	if sess == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"fileName": "", "bytes": 0})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"fileName": sess.fileName, "bytes": sess.bytes})
}
