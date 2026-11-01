// Package service — AI 听记 (live meeting notes) persistence and summarisation.
//
// A live share is a transient, in-memory room (internal/liveshare): the server
// never sees the media. Transcription therefore happens on the publisher, which
// uploads one short audio segment per channel (system audio / its own
// microphone / each viewer's microphone). Each upload is transcribed by the
// configured STT provider and stored here.
//
// Keeping the channels separate is the whole point: the publisher physically
// holds one track per participant, so the speaker label comes for free — no
// diarisation model is involved. Reading merges the channels back into one
// chronological timeline (see BuildLiveNotesTranscript).
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/summarize"
)

// LiveNotesDDL creates the two tables behind AI 听记.
//
// Exported so handler tests can create the schema in their own databases.
//
// live_notes_segments holds one row per transcribed audio segment. token is the
// share token (the room's only identifier) and project_id scopes the publisher
// endpoints; there is deliberately no foreign key — the room itself lives in
// memory and disappears the moment the publisher stops.
//
// live_notes holds one row per share: the generated minutes plus their status.
// Minutes are produced asynchronously (a long meeting takes tens of seconds to
// summarize), so status is polled rather than returned inline.
const LiveNotesDDL = `
CREATE TABLE IF NOT EXISTS live_notes_segments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	channel TEXT NOT NULL,
	speaker TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_live_note_segments_token ON live_notes_segments(token, started_at);

CREATE TABLE IF NOT EXISTS live_notes (
	token TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'idle',
	minutes TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	file_path TEXT NOT NULL DEFAULT '',
	dir_path TEXT NOT NULL DEFAULT '',
	revised TEXT NOT NULL DEFAULT '',
	revise_status TEXT NOT NULL DEFAULT 'idle',
	revise_error TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL DEFAULT 0,
	ended_at INTEGER NOT NULL DEFAULT 0,
	dir_name TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_live_notes_updated ON live_notes(updated_at);
CREATE INDEX IF NOT EXISTS idx_live_notes_project_started ON live_notes(project_id, started_at);

CREATE TABLE IF NOT EXISTS live_notes_chat (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	client_msg_id TEXT NOT NULL,
	speaker TEXT NOT NULL DEFAULT '',
	viewer_id TEXT NOT NULL DEFAULT '',
	text TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(token, client_msg_id)
);
CREATE INDEX IF NOT EXISTS idx_live_note_chat_token ON live_notes_chat(token, id);
CREATE INDEX IF NOT EXISTS idx_live_note_chat_project ON live_notes_chat(project_id, token);
`

// Live note minutes statuses. Kept as constants so the handler, the SPA and the
// tests cannot drift on spelling.
const (
	LiveNotesStatusIdle       = "idle"
	LiveNotesStatusGenerating = "generating"
	LiveNotesStatusReady      = "ready"
	LiveNotesStatusFailed     = "failed"
)

// ErrLiveNotesEmpty is returned when minutes are requested for a share that has
// no transcribed text.
var ErrLiveNotesEmpty = errors.New("live notes: transcript is empty")

// LiveNoteSegment is one transcribed audio segment from one channel.
type LiveNoteSegment struct {
	ID         int64  `json:"id"`
	Channel    string `json:"channel"` // "system" | "mic" | "viewer:<id>"
	Speaker    string `json:"speaker"` // publisher-localized display label
	StartedAt  int64  `json:"startedAt"`
	DurationMS int64  `json:"durationMs"`
	Text       string `json:"text"`
}

// LiveNote is the per-share minutes row.
type LiveNote struct {
	Token   string `json:"token"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Minutes string `json:"minutes"`
	Error   string `json:"error"`
	// FilePath is the Markdown file the minutes were written to, inside the
	// project. Empty when the project directory was unavailable.
	FilePath string `json:"filePath"`
	// DirName is the meeting folder NAME inside the project's notes root (not a
	// path: the project can be renamed or moved, the folder name cannot). Empty
	// until the first artifact — audio, chat archive or minutes — is written.
	DirName string `json:"dirName"`
	// StartedAt / EndedAt are unix milliseconds, stamped by the SERVER when the
	// publisher attaches and detaches. 0 means "unknown" / "still running".
	StartedAt int64 `json:"startedAt"`
	EndedAt   int64 `json:"endedAt"`
	// Revised is the proofread transcript the minutes were generated from.
	Revised string `json:"revised"`
	// ReviseStatus / ReviseError track the proofreading stage separately from
	// the minutes: the minutes can succeed with a raw transcript when the
	// proofreader is unavailable.
	ReviseStatus string `json:"reviseStatus"`
	ReviseError  string `json:"reviseError"`
}

// AppendLiveNoteSegment stores one transcribed segment. It also makes sure the
// live_notes row exists so that a late upload (arriving after the room was
// torn down) still has an owner to check the project against.
func AppendLiveNoteSegment(ctx context.Context, token string, projectID int64, seg LiveNoteSegment) (int64, error) {
	if token == "" {
		return 0, errors.New("live notes: empty token")
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return 0, fmt.Errorf("live notes: begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if _, execErr := tx.ExecContext(
		ctx,
		`INSERT INTO live_notes (token, project_id, status, started_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(token) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`,
		token, projectID, LiveNotesStatusIdle, time.Now().UnixMilli(),
	); execErr != nil {
		return 0, fmt.Errorf("live notes: ensure row: %w", execErr)
	}
	res, execErr := tx.ExecContext(
		ctx,
		`INSERT INTO live_notes_segments (token, project_id, channel, speaker, started_at, duration_ms, text)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token, projectID, seg.Channel, seg.Speaker, seg.StartedAt, seg.DurationMS, seg.Text,
	)
	if execErr != nil {
		return 0, fmt.Errorf("live notes: insert segment: %w", execErr)
	}
	id, idErr := res.LastInsertId()
	if idErr != nil {
		return 0, fmt.Errorf("live notes: segment id: %w", idErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("live notes: commit: %w", commitErr)
	}
	return id, nil
}

// ListLiveNoteSegments returns a share's segments with id > sinceID, ordered by
// id. Ordering by id (not time) is what makes `since` a correct incremental
// cursor: a segment transcribed late can carry an earlier started_at, so the
// client re-sorts by time after appending.
func ListLiveNoteSegments(token string, sinceID int64) ([]LiveNoteSegment, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, channel, speaker, started_at, duration_ms, text
		   FROM live_notes_segments
		  WHERE token = ? AND id > ?
		  ORDER BY id`,
		token, sinceID,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list segments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]LiveNoteSegment, 0, 32)
	for rows.Next() {
		var s LiveNoteSegment
		if err := rows.Scan(&s.ID, &s.Channel, &s.Speaker, &s.StartedAt, &s.DurationMS, &s.Text); err != nil {
			return nil, fmt.Errorf("live notes: scan segment: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("live notes: iterate segments: %w", err)
	}
	return out, nil
}

// GetLiveNote returns the minutes row for a share.
func GetLiveNote(token string) (LiveNote, bool, error) {
	var n LiveNote
	err := store.ReadDB().QueryRow(
		`SELECT token, title, status, minutes, error, file_path, dir_name, revised,
		        revise_status, revise_error, started_at, ended_at
		   FROM live_notes WHERE token = ?`, token,
	).Scan(&n.Token, &n.Title, &n.Status, &n.Minutes, &n.Error, &n.FilePath,
		&n.DirName, &n.Revised, &n.ReviseStatus, &n.ReviseError, &n.StartedAt, &n.EndedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return LiveNote{}, false, nil
	}
	if err != nil {
		return LiveNote{}, false, fmt.Errorf("live notes: get: %w", err)
	}
	return n, true, nil
}

// LiveNoteProjectID reports which project owns a share's notes. The publisher
// endpoints use it to authorize requests for a room that has already ended.
func LiveNoteProjectID(token string) (int64, bool, error) {
	var id int64
	err := store.ReadDB().QueryRow(`SELECT project_id FROM live_notes WHERE token = ?`, token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("live notes: project lookup: %w", err)
	}
	return id, true, nil
}

// SetLiveNoteMinutes records the minutes status/output for a share.
func SetLiveNoteMinutes(token string, projectID int64, title, status, minutes, errMsg, filePath string) error {
	_, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, title, status, minutes, error, file_path, started_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(token) DO UPDATE SET
		   status = excluded.status,
		   minutes = excluded.minutes,
		   error = excluded.error,
		   file_path = excluded.file_path,
		   title = CASE WHEN excluded.title <> '' THEN excluded.title ELSE live_notes.title END,
		   updated_at = CURRENT_TIMESTAMP`,
		token, projectID, title, status, minutes, errMsg, filePath, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("live notes: set minutes: %w", err)
	}
	return nil
}

// SetLiveNoteRevise records the proofreading stage of a share's transcript.
//
// Kept separate from the minutes status on purpose: the two stages fail
// independently, and a failed proofread must not hide a perfectly good summary
// (which is then generated from the raw transcript).
func SetLiveNoteRevise(token string, projectID int64, status, revised, errMsg string) error {
	_, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, revise_status, revised, revise_error, started_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(token) DO UPDATE SET
		   revise_status = excluded.revise_status,
		   revised = excluded.revised,
		   revise_error = excluded.revise_error,
		   updated_at = CURRENT_TIMESTAMP`,
		token, projectID, status, revised, errMsg, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("live notes: set revise: %w", err)
	}
	return nil
}

// ── Meeting record lifecycle ────────────────────────────────────────────────
//
// A meeting is a row in `live_notes`, created when the publisher attaches and
// closed when it detaches. It exists whether or not anything was recorded: a
// silent meeting with 听记 off still happened, and the user must be able to see
// it (and delete it). Nothing here expires — deleting the record is the only
// way these rows go away, apart from deleting the project.

// StartLiveNote creates the permanent record for a meeting.
//
// ON CONFLICT DO NOTHING, not an update: a publisher that reconnects with the
// same token (a socket blip) must keep the ORIGINAL start time, otherwise the
// record's date would drift every time the network hiccups.
func StartLiveNote(token string, projectID int64, title string, startedAtMS int64) error {
	if token == "" {
		return errors.New("live notes: empty token")
	}
	if startedAtMS <= 0 {
		startedAtMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, title, started_at, status, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(token) DO NOTHING`,
		token, projectID, title, startedAtMS, LiveNotesStatusIdle,
	)
	if err != nil {
		return fmt.Errorf("live notes: start meeting: %w", err)
	}
	return nil
}

// EndLiveNote stamps the end time once. A second call is a no-op so a
// double-close (explicit end + socket teardown) cannot move the finish line.
func EndLiveNote(token string, endedAtMS int64) error {
	if token == "" {
		return nil
	}
	if endedAtMS <= 0 {
		endedAtMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`UPDATE live_notes SET ended_at = ?, updated_at = CURRENT_TIMESTAMP
		  WHERE token = ? AND ended_at = 0`,
		endedAtMS, token,
	)
	if err != nil {
		return fmt.Errorf("live notes: end meeting: %w", err)
	}
	return nil
}

// CloseStaleLiveNotes closes records whose publisher never reported a clean
// end (process crash, killed app). Without this the list would show meetings
// that never finish, and the delete guard would refuse to remove them.
//
// `beforeMS` is the cutoff on updated_at; the reaper passes "now - 12h".
func CloseStaleLiveNotes(beforeMS int64) (int, error) {
	if beforeMS <= 0 {
		return 0, nil
	}
	cutoff := time.UnixMilli(beforeMS).UTC().Format("2006-01-02 15:04:05")
	res, err := store.WriteExec(
		`UPDATE live_notes SET ended_at = started_at, updated_at = CURRENT_TIMESTAMP
		  WHERE ended_at = 0 AND updated_at < ?`, cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("live notes: close stale meetings: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MeetingDirName returns the folder name recorded for a meeting, or "".
func MeetingDirName(token string) (string, error) {
	var name string
	err := store.ReadDB().QueryRow(`SELECT dir_name FROM live_notes WHERE token = ?`, token).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("live notes: dir lookup: %w", err)
	}
	return name, nil
}

// MeetingDirPath joins a folder name onto the project's notes root.
//
// The row stores the NAME, never the absolute path: a project can be renamed or
// moved, and a stale absolute path would silently point at nothing.
func MeetingDirPath(projectPath, dirName string) string {
	if strings.TrimSpace(projectPath) == "" || strings.TrimSpace(dirName) == "" {
		return ""
	}
	return filepath.Join(liveNotesRoot(projectPath), dirName)
}

// EnsureMeetingDir creates the meeting folder on first use and returns its
// paths. Idempotent: later calls reuse the recorded name even if the title
// changed since.
func EnsureMeetingDir(token string, projectID int64, projectPath, title string, startedAtMS int64) (LiveNotesPaths, error) {
	if strings.TrimSpace(projectPath) == "" {
		return LiveNotesPaths{}, errors.New("live notes: no project path")
	}
	if name, err := MeetingDirName(token); err != nil {
		return LiveNotesPaths{}, err
	} else if name != "" {
		return ensureLiveNotesDirs(MeetingDirPath(projectPath, name))
	}

	startedAt := time.UnixMilli(startedAtMS)
	if startedAtMS <= 0 {
		startedAt = time.Now()
	}
	name := liveNotesMeetingDirName(startedAt, title, token)
	paths, err := ensureLiveNotesDirs(MeetingDirPath(projectPath, name))
	if err != nil {
		return LiveNotesPaths{}, err
	}
	// CASE, not excluded: a concurrent caller (the minutes job, a chat archive)
	// may have recorded a folder between the lookup above and this write.
	if _, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, dir_name, started_at, updated_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(token) DO UPDATE SET
		   dir_name = CASE WHEN live_notes.dir_name = '' THEN excluded.dir_name ELSE live_notes.dir_name END,
		   started_at = CASE WHEN live_notes.started_at = 0 THEN excluded.started_at ELSE live_notes.started_at END,
		   updated_at = CURRENT_TIMESTAMP`,
		token, projectID, name, startedAt.UnixMilli(),
	); err != nil {
		return LiveNotesPaths{}, fmt.Errorf("live notes: record dir: %w", err)
	}
	return paths, nil
}

// MeetingRecord is one row of the meeting list.
//
// The record is the database row (permanent); the artifact flags are derived by
// probing the folder, because the folder — not the row — is where those files
// live and a stale flag would send the user looking for a file that is gone.
type MeetingRecord struct {
	Token     string `json:"token"`
	Title     string `json:"title"`
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt"`
	// Dir is the folder NAME; empty when the meeting produced no artifacts.
	Dir string `json:"dir,omitempty"`
	// DirPath is the absolute folder path (publisher only).
	DirPath       string `json:"dirPath,omitempty"`
	HasMinutes    bool   `json:"hasMinutes"`
	HasTranscript bool   `json:"hasTranscript"`
	HasRevised    bool   `json:"hasRevised"`
	AudioFiles    int    `json:"audioFiles"`
	ChatCount     int    `json:"chatCount"`
	// Status is the minutes job state (idle/generating/ready/failed). The list
	// uses it to show "生成中" and to mark a job that died with the process.
	Status string `json:"status"`
	// UpdatedAt is the row's last write, used to detect a stuck "generating".
	UpdatedAt int64 `json:"updatedAt"`
}

// ListMeetingRecords returns a project's meetings, newest first.
func ListMeetingRecords(projectPath string, projectID int64, limit int) ([]MeetingRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := store.ReadDB().Query(
		`SELECT token, title, started_at, ended_at, dir_name, status,
		        CAST(strftime('%s', updated_at) AS INTEGER) * 1000
		   FROM live_notes
		  WHERE project_id = ?
		  ORDER BY started_at DESC, token DESC
		  LIMIT ?`,
		projectID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list meetings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]MeetingRecord, 0, 16)
	for rows.Next() {
		var m MeetingRecord
		var dirName string
		var updatedMS int64
		if scanErr := rows.Scan(&m.Token, &m.Title, &m.StartedAt, &m.EndedAt, &dirName,
			&m.Status, &updatedMS); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan meeting: %w", scanErr)
		}
		m.UpdatedAt = updatedMS
		if dirName != "" {
			m.Dir = dirName
			m.DirPath = MeetingDirPath(projectPath, dirName)
			m.HasMinutes, m.HasTranscript, m.HasRevised, m.AudioFiles = liveNoteArtifactFlags(m.DirPath)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("live notes: iterate meetings: %w", err)
	}

	counts, err := liveNoteChatCounts(projectID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].ChatCount = counts[out[i].Token]
	}
	return out, nil
}

// GetMeetingRecord returns one record by token, scoped to the project.
func GetMeetingRecord(projectPath string, projectID int64, token string) (MeetingRecord, bool, error) {
	var m MeetingRecord
	var dirName string
	var updatedMS int64
	err := store.ReadDB().QueryRow(
		`SELECT token, title, started_at, ended_at, dir_name, status,
		        CAST(strftime('%s', updated_at) AS INTEGER) * 1000
		   FROM live_notes WHERE token = ? AND project_id = ?`, token, projectID,
	).Scan(&m.Token, &m.Title, &m.StartedAt, &m.EndedAt, &dirName, &m.Status, &updatedMS)
	if errors.Is(err, sql.ErrNoRows) {
		return MeetingRecord{}, false, nil
	}
	if err != nil {
		return MeetingRecord{}, false, fmt.Errorf("live notes: get meeting: %w", err)
	}
	m.UpdatedAt = updatedMS
	if dirName != "" {
		m.Dir = dirName
		m.DirPath = MeetingDirPath(projectPath, dirName)
		m.HasMinutes, m.HasTranscript, m.HasRevised, m.AudioFiles = liveNoteArtifactFlags(m.DirPath)
	}
	counts, err := liveNoteChatCounts(projectID)
	if err != nil {
		return MeetingRecord{}, false, err
	}
	m.ChatCount = counts[token]
	return m, true, nil
}

// liveNoteChatCounts returns message counts per token for one project.
func liveNoteChatCounts(projectID int64) (map[string]int, error) {
	rows, err := store.ReadDB().Query(
		`SELECT token, COUNT(*) FROM live_notes_chat WHERE project_id = ? GROUP BY token`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: chat counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]int, 16)
	for rows.Next() {
		var token string
		var n int
		if scanErr := rows.Scan(&token, &n); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan chat count: %w", scanErr)
		}
		out[token] = n
	}
	return out, rows.Err()
}

// liveNoteArtifactFlags probes one meeting folder.
func liveNoteArtifactFlags(dirPath string) (hasMinutes, hasTranscript, hasRevised bool, audio int) {
	if dirPath == "" {
		return false, false, false, 0
	}
	paths := liveNotesPathsFor(dirPath)
	hasMinutes = fileExists(paths.Minutes)
	hasTranscript = fileExists(paths.Transcript) || fileExists(filepath.Join(dirPath, liveNoteTranscriptLegacyMD))
	hasRevised = fileExists(paths.Revised)
	if entries, err := os.ReadDir(paths.Audio); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".webm") {
				audio++
			}
		}
	}
	return hasMinutes, hasTranscript, hasRevised, audio
}

// ErrMeetingNotFound is returned when a record does not exist (or belongs to
// another project).
var ErrMeetingNotFound = errors.New("live notes: meeting not found")

// DeleteMeetingRecord removes one meeting: its folder first, then its rows.
//
// Order matters. If the folder cannot be removed (permissions, a file in use)
// the rows are left untouched and the caller can retry — nothing is lost. If
// the rows fail after the folder is gone, the record remains pointing at a
// missing folder (the detail shows "no artifacts") and deleting again finishes
// the job. Both halves are recoverable; the reverse order would orphan files
// with no record left to find them from.
func DeleteMeetingRecord(projectPath string, projectID int64, token string) error {
	if token == "" {
		return ErrMeetingNotFound
	}
	var dirName string
	err := store.ReadDB().QueryRow(
		`SELECT dir_name FROM live_notes WHERE token = ? AND project_id = ?`, token, projectID,
	).Scan(&dirName)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMeetingNotFound
	}
	if err != nil {
		return fmt.Errorf("live notes: delete lookup: %w", err)
	}

	if dirName != "" {
		root := liveNotesRoot(projectPath)
		full := filepath.Join(root, dirName)
		// Defence in depth: the name is generated by us, but a corrupted row
		// must not be able to point outside the project's notes root.
		if !strings.HasPrefix(filepath.Clean(full)+string(os.PathSeparator),
			filepath.Clean(root)+string(os.PathSeparator)) {
			return fmt.Errorf("live notes: refusing to delete outside notes root")
		}
		if rmErr := os.RemoveAll(full); rmErr != nil {
			return fmt.Errorf("live notes: remove meeting folder: %w", rmErr)
		}
	}

	tx, err := store.WriteBegin()
	if err != nil {
		return fmt.Errorf("live notes: delete begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM live_notes_chat WHERE token = ?`,
		`DELETE FROM live_notes_segments WHERE token = ?`,
		`DELETE FROM live_notes WHERE token = ? AND project_id = ?`,
	} {
		args := []any{token}
		if strings.Contains(stmt, "project_id") {
			args = append(args, projectID)
		}
		if _, execErr := tx.Exec(stmt, args...); execErr != nil {
			return fmt.Errorf("live notes: delete meeting rows: %w", execErr)
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("live notes: delete commit: %w", commitErr)
	}
	return nil
}

// ── Chat ────────────────────────────────────────────────────────────────────

// LiveNoteChatMessage is one persisted chat line.
type LiveNoteChatMessage struct {
	ID       int64  `json:"id"`
	ClientID string `json:"clientId"`
	Speaker  string `json:"speaker"`
	ViewerID string `json:"viewerId,omitempty"`
	Text     string `json:"text"`
	At       int64  `json:"at"`
}

// AppendLiveNoteChat stores one message. Duplicate client ids are ignored
// (returns the existing row's id), so the publisher's retry cannot duplicate a
// line.
func AppendLiveNoteChat(token string, projectID int64, msg LiveNoteChatMessage) (int64, error) {
	if token == "" || strings.TrimSpace(msg.Text) == "" || strings.TrimSpace(msg.ClientID) == "" {
		return 0, errors.New("live notes: invalid chat message")
	}
	res, err := store.WriteExec(
		`INSERT INTO live_notes_chat (token, project_id, client_msg_id, speaker, viewer_id, text)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(token, client_msg_id) DO NOTHING`,
		token, projectID, msg.ClientID, msg.Speaker, msg.ViewerID, msg.Text,
	)
	if err != nil {
		return 0, fmt.Errorf("live notes: append chat: %w", err)
	}
	if id, idErr := res.LastInsertId(); idErr == nil && id > 0 {
		return id, nil
	}
	var existing int64
	if scanErr := store.ReadDB().QueryRow(
		`SELECT id FROM live_notes_chat WHERE token = ? AND client_msg_id = ?`, token, msg.ClientID,
	).Scan(&existing); scanErr != nil {
		return 0, fmt.Errorf("live notes: chat id lookup: %w", scanErr)
	}
	return existing, nil
}

// ListLiveNoteChat returns a meeting's messages in arrival order (id), which is
// the server's own clock order — the sender's `at` may be skewed.
func ListLiveNoteChat(token string, sinceID int64) ([]LiveNoteChatMessage, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, client_msg_id, speaker, viewer_id, text,
		        CAST(strftime('%s', created_at) AS INTEGER) * 1000
		   FROM live_notes_chat
		  WHERE token = ? AND id > ?
		  ORDER BY id`,
		token, sinceID,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list chat: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]LiveNoteChatMessage, 0, 32)
	for rows.Next() {
		var m LiveNoteChatMessage
		if scanErr := rows.Scan(&m.ID, &m.ClientID, &m.Speaker, &m.ViewerID, &m.Text, &m.At); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan chat: %w", scanErr)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CountLiveNoteChat returns how many messages a meeting has.
func CountLiveNoteChat(token string) (int, error) {
	var n int
	if err := store.ReadDB().QueryRow(`SELECT COUNT(*) FROM live_notes_chat WHERE token = ?`, token).Scan(&n); err != nil {
		return 0, fmt.Errorf("live notes: count chat: %w", err)
	}
	return n, nil
}

// WriteLiveNoteChatFile writes the human-readable archive of a meeting's chat.
//
// This is not a fallback for expired rows (there are none any more): it is the
// same bargain the minutes and the transcripts get — a copy on disk that can be
// read, diffed and committed without the app.
func WriteLiveNoteChatFile(paths LiveNotesPaths, msgs []LiveNoteChatMessage) (string, error) {
	if len(msgs) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("# 会议聊天记录\n\n")
	for _, m := range msgs {
		speaker := m.Speaker
		if speaker == "" {
			speaker = "匿名"
		}
		when := ""
		if m.At > 0 {
			when = "[" + time.UnixMilli(m.At).Format("15:04") + "] "
		}
		b.WriteString(when + speaker + "：" + strings.ReplaceAll(m.Text, "\n", " ") + "\n\n")
	}
	path := filepath.Join(paths.Dir, "chat.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("live notes: write chat archive: %w", err)
	}
	return path, nil
}

// ── Timeline assembly ───────────────────────────────────────────────────────

// liveNotesDedupeWindow is how far apart two identical lines may start and
// still be considered the same utterance. Speakers' voices reach the system
// loopback (and the publisher's microphone) with a delay, so a duplicate is
// never sample-aligned.
const liveNotesDedupeWindowMS = 5000

// SortLiveNoteSegments returns the segments in speaking order.
//
// started_at is stamped by a single clock (the publisher's), so comparing it
// across channels is meaningful. id breaks ties deterministically.
func SortLiveNoteSegments(in []LiveNoteSegment) []LiveNoteSegment {
	out := make([]LiveNoteSegment, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s.Text) == "" {
			continue
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartedAt != out[j].StartedAt {
			return out[i].StartedAt < out[j].StartedAt
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// normalizeNoteText folds a transcript line for duplicate detection: case,
// whitespace and punctuation are ignored so that the same sentence captured by
// two microphones compares equal.
//
// Letters and digits of EVERY script are kept, not just Latin and CJK
// ideographs: a Japanese line is mostly kana, a Korean one hangul, a Russian one
// Cyrillic. Filtering those out would fold them all to "" and — because
// DedupeLiveNoteSegments drops empty keys — silently delete every line of a
// non-Chinese/non-English meeting, leaving the transcript (and therefore the
// minutes) empty.
func normalizeNoteText(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DedupeLiveNoteSegments drops a segment when another channel captured the same
// words at nearly the same moment.
//
// This is the speaker's own voice leaking into a second channel: the audience
// playing through the publisher's speakers is re-captured by the system-audio
// loopback, and the publisher's microphone picks up whatever the speakers play.
// Without this, every remote participant appears twice.
//
// The earliest segment wins (it is the one closest to the source); the later
// copy is dropped. Genuine simultaneous speech by two people differs in words,
// so it is unaffected.
func DedupeLiveNoteSegments(sorted []LiveNoteSegment) []LiveNoteSegment {
	out := make([]LiveNoteSegment, 0, len(sorted))
	type kept struct {
		norm string
		at   int64
	}
	recent := make([]kept, 0, 8)
	for _, s := range sorted {
		norm := normalizeNoteText(s.Text)
		if norm == "" {
			continue
		}
		dup := false
		for _, k := range recent {
			if k.norm == norm && absInt64(k.at-s.StartedAt) <= liveNotesDedupeWindowMS {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		recent = append(recent, kept{norm: norm, at: s.StartedAt})
		if len(recent) > 8 {
			recent = recent[1:]
		}
		out = append(out, s)
	}
	return out
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// BuildLiveNotesTranscript renders the merged timeline as the text handed to
// the summariser: one line per utterance, prefixed with the offset from the
// first segment and the speaker label.
func BuildLiveNotesTranscript(segments []LiveNoteSegment) string {
	merged, base := mergedLiveNoteTimeline(segments)
	if len(merged) == 0 {
		return ""
	}
	var b strings.Builder
	for _, s := range merged {
		speaker := strings.TrimSpace(s.Speaker)
		if speaker == "" {
			speaker = s.Channel
		}
		text := strings.Join(strings.Fields(s.Text), " ")
		if s.StartedAt <= 0 || base == 0 {
			// Unknown timing: say so rather than inventing an offset.
			fmt.Fprintf(&b, "[--:--] %s：%s\n", speaker, text)
			continue
		}
		offset := (s.StartedAt - base) / 1000
		if offset < 0 {
			offset = 0
		}
		fmt.Fprintf(&b, "[%02d:%02d] %s：%s\n", offset/60, offset%60, speaker, text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// mergedLiveNoteTimeline sorts and dedupes the segments and returns the epoch-ms
// origin used for offsets.
//
// The origin is the earliest POSITIVE timestamp, never 0. A segment whose
// startedAt is missing parses to 0 and sorts first; using it as the base would
// turn every real offset into "milliseconds since 1970" and put the line that
// actually happened last at the top of the transcript.
func mergedLiveNoteTimeline(segments []LiveNoteSegment) ([]LiveNoteSegment, int64) {
	merged := DedupeLiveNoteSegments(SortLiveNoteSegments(segments))
	base := int64(0)
	for _, s := range merged {
		if s.StartedAt > 0 && (base == 0 || s.StartedAt < base) {
			base = s.StartedAt
		}
	}
	return merged, base
}

// srtFallbackCueMS is the cue length used when a segment carries no duration.
// SRT requires an end after the start, so a missing duration cannot be written
// as-is.
const srtFallbackCueMS = 2000

// BuildLiveNotesSRT renders the merged timeline as SubRip subtitles.
//
// The raw transcript is stored as SRT rather than Markdown because it is a
// subtitle track: Markdown collapses consecutive lines into one paragraph, so
// every utterance ran together when the file was previewed. SRT also keeps the
// timing, which makes the file usable in a player next to the recording.
func BuildLiveNotesSRT(segments []LiveNoteSegment) string {
	merged, base := mergedLiveNoteTimeline(segments)
	if len(merged) == 0 {
		return ""
	}

	var b strings.Builder
	cursor := int64(0) // end of the previous cue
	index := 0
	for _, s := range merged {
		text := strings.Join(strings.Fields(s.Text), " ")
		if text == "" {
			continue
		}
		speaker := strings.TrimSpace(s.Speaker)
		if speaker == "" {
			speaker = s.Channel
		}

		start, end := cursor, cursor+srtFallbackCueMS
		if s.StartedAt > 0 && base > 0 {
			start = s.StartedAt - base
			if start < 0 {
				start = 0
			}
			end = start + s.DurationMS
			if end <= start {
				end = start + srtFallbackCueMS
			}
		}
		cursor = end

		index++
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s：%s\n\n",
			index, srtTimestamp(start), srtTimestamp(end), speaker, text)
	}
	if index == 0 {
		return ""
	}
	return b.String()
}

// srtTimestamp formats milliseconds as SubRip's HH:MM:SS,mmm.
func srtTimestamp(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d,%03d",
		ms/3600000, (ms%3600000)/60000, (ms%60000)/1000, ms%1000)
}

// ── Minutes generation ──────────────────────────────────────────────────────

const liveNotesMinutesPrompt = `你是一名专业的会议记录员。根据下面这场会议的实时转写（每行是「时间 说话人：内容」，不同说话人的语音来自不同信道，已按时间重排），生成结构化的会议纪要。

要求：
1. 输出 Markdown，使用以下章节（没有内容的章节可省略）：
   ## 概要
   ## 议题与讨论
   ## 结论与决定
   ## 待办事项
2. 待办事项用列表表示，能判断负责人时标注说话人，提到时间点时一并保留。
3. 只依据转写内容，不要编造。信息不完整或听不清的地方标注「（待确认）」。
4. 忽略寒暄、口头语和明显的转写错误；不要输出转写原文。
5. 使用与转写内容一致的语言输出（中英混合时以中文为主）。`

const liveNotesChunkPrompt = `你是一名会议记录员。下面是一场长会议转写的其中一段（每行是「时间 说话人：内容」）。请只提取这一段里的关键信息，用简短要点列出：
- 讨论的议题
- 达成的结论或决定
- 待办事项（尽量标注负责人）
不要展开叙述，不要输出转写原文，保留说话人姓名。使用与原文一致的语言。`

// liveNotesChunkRunes is the per-chunk input budget for a long meeting. Kept
// well below the model window so that prompt + output still fit.
const liveNotesChunkRunes = 6000

// liveNotesReviseMaxTokens bounds one proofread chunk. The output is roughly
// the same length as the input, so this is a ceiling against a runaway model
// rather than a real limit.
const liveNotesReviseMaxTokens = 4000

// liveNotesRevisePrompt proofreads one chunk of the transcript.
//
// The instructions are deliberately narrow. A proofreader that is allowed to
// "improve" a sentence rewrites the meeting: minutes then describe what the
// model thinks should have been said. Only substitutions are allowed here —
// every timestamp, speaker and line must survive unchanged so the result is
// still a transcript and not an essay.
const liveNotesRevisePrompt = `你是一名会议转写校对员。输入是语音识别的会议转写，每行格式为「[mm:ss] 说话人：内容」。

你的任务只有一件：修正听写错误。

1. 只改明显的同音/近音错字、专有名词、产品名、技术术语、人名与缩写。
2. 「项目背景」里出现过的写法是唯一正确写法：转写里与之接近的一律改成它（例如把同音词改成项目里的真实名字）。
3. 不要润色、不要改写句式、不要合并或拆分句子、不要补全没听清的内容。
4. 不要删除任何信息（口头语也保留），不要改变时间戳、说话人、行数和顺序。
5. 听不清且无法确定的，保持原样，不要猜测。

直接输出修订后的转写，不要任何解释、不要加代码块。`

const liveNotesReviseQuestion = "请输出这一段修订后的转写。"

// ReviseLiveNotesTranscript proofreads the raw transcript against the project's
// own vocabulary and returns the corrected timeline.
//
// Why this stage exists: ASR has no idea that "claw bench" is ClawBench or that
// "ACP" is not "a c p". The project's own documents (AGENTS.md / README.md) are
// full of the correct spellings, and feeding them to the proofreader fixes
// exactly the errors a human would fix — which is what makes the minutes
// readable.
//
// Chunked for the same reason the minutes are: a one-hour meeting does not fit
// in one request. Chunks are cut on line boundaries so a line is never split.
func ReviseLiveNotesTranscript(ctx context.Context, projectPath, title string, segments []LiveNoteSegment) (string, error) {
	transcript := BuildLiveNotesTranscript(segments)
	if strings.TrimSpace(transcript) == "" {
		return "", ErrLiveNotesEmpty
	}
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}

	background := strings.Join(projectContext(projectPath), "\n\n")
	chunks := splitRunes(transcript, liveNotesChunkRunes)
	out := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		// The transcript always gets its own heading, even with no project
		// documents: without it the model has no delimiter between "context" and
		// "the thing to correct", and a project without an AGENTS.md is the
		// normal case for a fresh directory.
		block := "## 待修订转写\n" + chunk
		if background != "" {
			block = "## 项目背景（专有名词以此为准）\n" + background + "\n\n" + block
		}
		question := liveNotesReviseQuestion
		if len(chunks) > 1 {
			question = fmt.Sprintf("这是第 %d/%d 段。%s", i+1, len(chunks), liveNotesReviseQuestion)
		}
		revised, err := summarize.AskAboutContext(ctx, summarizer, liveNotesRevisePrompt, block, question, liveNotesReviseMaxTokens)
		if err != nil {
			return "", fmt.Errorf("live notes: revise chunk %d: %w", i+1, err)
		}
		// The provider already fails on an empty answer, so a blank chunk cannot
		// reach here; trimming is all that is left to do.
		out = append(out, strings.TrimSpace(revised))
	}
	return strings.Join(out, "\n"), nil
}

// GenerateLiveNotesMinutes turns a transcript into Markdown minutes.
//
// It takes the transcript as text (not segments) because the caller decides
// which variant to summarize — normally the proofread one.
//
// Long meetings are map-reduced: each chunk is condensed to bullet points, then
// the bullet points are merged into the final minutes. Sending the whole
// transcript in one request would either overflow the model window or be
// silently truncated by the provider.
func GenerateLiveNotesMinutes(ctx context.Context, title, transcript string) (string, error) {
	if strings.TrimSpace(transcript) == "" {
		return "", ErrLiveNotesEmpty
	}
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}

	chunks := splitRunes(transcript, liveNotesChunkRunes)
	contextBlock := transcript
	if len(chunks) > 1 {
		partials := make([]string, 0, len(chunks))
		for i, chunk := range chunks {
			out, err := summarize.AskAboutContext(
				ctx, summarizer, liveNotesChunkPrompt,
				fmt.Sprintf("第 %d/%d 段：\n%s", i+1, len(chunks), chunk),
				"请提取这一段的关键信息。", 1200,
			)
			if err != nil {
				return "", fmt.Errorf("live notes: chunk %d: %w", i+1, err)
			}
			partials = append(partials, out)
		}
		contextBlock = strings.Join(partials, "\n\n")
	}

	question := "请根据以上内容生成会议纪要。"
	if title != "" {
		question = "会议标题：" + title + "\n请根据以上内容生成会议纪要。"
	}
	minutes, err := summarize.AskAboutContext(ctx, summarizer, liveNotesMinutesPrompt, contextBlock, question, 3000)
	if err != nil {
		return "", fmt.Errorf("live notes: minutes: %w", err)
	}
	return strings.TrimSpace(minutes), nil
}

// splitRunes splits s into chunks of at most n runes, preferring a line break
// near the boundary so a single utterance is not cut in half.
func splitRunes(s string, n int) []string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return []string{s}
	}
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		if len(runes) <= n {
			out = append(out, string(runes))
			break
		}
		cut := n
		// Walk back to a newline within the last 15% of the window.
		for i := n; i > n-n/6 && i > 0; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		out = append(out, string(runes[:cut]))
		runes = runes[cut:]
	}
	return out
}

// ── Markdown output ─────────────────────────────────────────────────────────

// LiveNotesDir is where a meeting's artifacts are written inside a project.
//
// Dot-prefixed so it does not clutter the tree, and under `.clawbench/` because
// that is already the convention for generated artifacts in this repo.
const LiveNotesDir = ".clawbench/notes"

// liveNotesNameMaxRunes bounds the title part of the folder name. Long titles
// are unwieldy in a file manager and can exceed the platform's path limit once
// the directory is prepended.
const liveNotesNameMaxRunes = 40

// LiveNotesPaths are the files one meeting leaves behind.
//
// Every one of them is permanent: the folder stays until the user deletes the
// meeting. That is why audio is written as it arrives rather than at the end —
// a crash mid-meeting must not cost the recording.
type LiveNotesPaths struct {
	// Dir is the meeting folder.
	Dir string
	// Minutes is `<dir>/minutes.md` — the generated summary.
	Minutes string
	// Transcript is `<dir>/transcript.srt` — the raw ASR output, as SubRip.
	Transcript string
	// Revised is `<dir>/transcript-revised.md` — proofread against the project's
	// own vocabulary. This is what the minutes are generated from.
	Revised string
	// Audio is `<dir>/audio/` — one WebM file per transcribed segment.
	Audio string
}

const (
	// liveNoteTranscriptSRT is the raw transcript artifact.
	liveNoteTranscriptSRT = "transcript.srt"
	// liveNoteTranscriptLegacyMD is the Markdown transcript written before the
	// artifact switched to SRT. Meetings recorded earlier keep it on disk.
	liveNoteTranscriptLegacyMD = "transcript.md"
)

// LiveNotesPathsIn returns the artifact paths for a meeting folder.
func LiveNotesPathsIn(dir string) LiveNotesPaths { return liveNotesPathsFor(dir) }

func liveNotesPathsFor(dir string) LiveNotesPaths {
	return LiveNotesPaths{
		Dir:        dir,
		Minutes:    filepath.Join(dir, "minutes.md"),
		Transcript: filepath.Join(dir, liveNoteTranscriptSRT),
		Revised:    filepath.Join(dir, "transcript-revised.md"),
		Audio:      filepath.Join(dir, "audio"),
	}
}

// liveNotesMeetingDirName builds the folder name from the meeting start.
//
// The token suffix is not decoration: two meetings can start in the same minute
// under the same title, and sharing a folder would interleave their audio and
// overwrite each other's minutes.
func liveNotesMeetingDirName(startedAt time.Time, title, token string) string {
	name := startedAt.Format("2006-01-02-1504")
	if slug := liveNotesFileSlug(title); slug != "" {
		name += "-" + slug
	}
	suffix := token
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	if suffix != "" {
		name += "-" + suffix
	}
	return name
}

func ensureLiveNotesDirs(dir string) (LiveNotesPaths, error) {
	paths := liveNotesPathsFor(dir)
	if err := os.MkdirAll(paths.Audio, 0o755); err != nil {
		return LiveNotesPaths{}, fmt.Errorf("live notes: create notes dir: %w", err)
	}
	// Audio is binary and can reach tens of megabytes per meeting. Keep it out
	// of the repository by default; the Markdown next to it stays committable,
	// which is what the folder is for.
	ignore := filepath.Join(paths.Audio, ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		// Best effort: an ignored audio folder is a convenience, not a
		// requirement, and the caller is in the middle of an upload.
		_ = os.WriteFile(ignore, []byte("*\n"), 0o644)
	}
	return paths, nil
}

// SaveLiveNoteAudio writes one received segment next to the minutes.
//
// Named by the segment's wall-clock start so the files sort in speaking order
// and two channels never collide. The name is also the only link back to the
// transcript line, which is what makes a future "jump to audio" possible.
func SaveLiveNoteAudio(paths LiveNotesPaths, channel string, startedAtMS int64, audio []byte) (string, error) {
	if len(audio) == 0 {
		return "", errors.New("live notes: empty audio")
	}
	if startedAtMS <= 0 {
		startedAtMS = time.Now().UnixMilli()
	}
	if err := os.MkdirAll(paths.Audio, 0o755); err != nil {
		return "", fmt.Errorf("live notes: create audio dir: %w", err)
	}
	name := fmt.Sprintf("%013d-%s.webm", startedAtMS, liveNotesChannelSlug(channel))
	path := filepath.Join(paths.Audio, name)
	if err := os.WriteFile(path, audio, 0o644); err != nil {
		return "", fmt.Errorf("live notes: write audio: %w", err)
	}
	return path, nil
}

// liveNotesChannelSlug turns "viewer:2" into a safe file-name element.
func liveNotesChannelSlug(channel string) string {
	slug := strings.ReplaceAll(channel, ":", "-")
	var b strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "channel"
	}
	return b.String()
}

// WriteLiveNotesMinutesFile saves the generated minutes inside the meeting
// folder and links to the sibling artifacts.
//
// The transcript is NOT appended any more: it has its own file, and a copy here
// would go stale the moment somebody edits it. A link keeps one source of truth.
func WriteLiveNotesMinutesFile(paths LiveNotesPaths, title string, startedAt time.Time, minutes string, audioSegments int) (string, error) {
	if strings.TrimSpace(paths.Dir) == "" {
		return "", errors.New("live notes: no project path")
	}
	if strings.TrimSpace(minutes) == "" {
		return "", ErrLiveNotesEmpty
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return "", fmt.Errorf("live notes: create notes dir: %w", err)
	}

	heading := startedAt.Format("2006-01-02 15:04")
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		fmt.Fprintf(&b, "# %s · %s\n\n", t, heading)
	} else {
		fmt.Fprintf(&b, "# 会议纪要 · %s\n\n", heading)
	}
	b.WriteString(strings.TrimSpace(minutes))
	b.WriteString("\n\n---\n\n## 本次产物\n\n")
	b.WriteString("- 转写：[transcript.srt](transcript.srt)\n")
	b.WriteString("- 修订版转写：[transcript-revised.md](transcript-revised.md)\n")
	if audioSegments > 0 {
		fmt.Fprintf(&b, "- 录音：`audio/`（%d 段）\n", audioSegments)
	}

	if err := os.WriteFile(paths.Minutes, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("live notes: write minutes file: %w", err)
	}
	return paths.Minutes, nil
}

// WriteLiveNotesRawTranscript writes transcript.srt: exactly what the ASR heard.
//
// No Markdown heading: the file is a subtitle track, and a `# 转写 · …` line
// would make every player show it as the first cue.
func WriteLiveNotesRawTranscript(paths LiveNotesPaths, segments []LiveNoteSegment) (string, error) {
	if strings.TrimSpace(paths.Dir) == "" {
		return "", errors.New("live notes: no project path")
	}
	body := BuildLiveNotesSRT(segments)
	if strings.TrimSpace(body) == "" {
		return "", ErrLiveNotesEmpty
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return "", fmt.Errorf("live notes: create notes dir: %w", err)
	}
	if err := os.WriteFile(paths.Transcript, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("live notes: write transcript file: %w", err)
	}
	return paths.Transcript, nil
}

// WriteLiveNotesRevisedTranscript writes transcript-revised.md, the proofread
// variant the minutes are generated from.
//
// The blockquote is not decoration: months later nobody remembers why this file
// differs from transcript.srt, and "the timestamps were not touched" is the one
// thing that makes it comparable.
func WriteLiveNotesRevisedTranscript(paths LiveNotesPaths, title string, startedAt time.Time, body string) (string, error) {
	return writeLiveNotesTranscript(paths, paths.Revised, "修订版转写",
		"已结合项目上下文修正听写错误与专有名词；时间戳、说话人、行序均未改动。",
		title, startedAt, body)
}

// writeLiveNotesTranscript writes one transcript variant into the meeting
// folder. `note` is an optional blockquote placed under the heading.
func writeLiveNotesTranscript(paths LiveNotesPaths, target, heading, note, title string, startedAt time.Time, body string) (string, error) {
	if strings.TrimSpace(paths.Dir) == "" {
		return "", errors.New("live notes: no project path")
	}
	if strings.TrimSpace(body) == "" {
		return "", ErrLiveNotesEmpty
	}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return "", fmt.Errorf("live notes: create notes dir: %w", err)
	}

	stamp := startedAt.Format("2006-01-02 15:04")
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		fmt.Fprintf(&b, "# %s · %s · %s\n\n", heading, t, stamp)
	} else {
		fmt.Fprintf(&b, "# %s · %s\n\n", heading, stamp)
	}
	if note != "" {
		fmt.Fprintf(&b, "> %s\n\n", note)
	}
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")

	if err := os.WriteFile(target, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("live notes: write transcript file: %w", err)
	}
	return target, nil
}

// ── History ─────────────────────────────────────────────────────────────────

// ErrLiveNoteSessionNotFound is returned for a meeting folder that is not
// there (or whose name is not a folder name this package produced).
var ErrLiveNoteSessionNotFound = errors.New("live notes: session not found")

// ErrLiveNoteArtifactKind is returned for an artifact name outside the three
// known kinds.
var ErrLiveNoteArtifactKind = errors.New("live notes: unknown artifact kind")

// ErrLiveNoteArtifactTooLarge bounds one artifact read. Minutes and transcripts
// are text produced by a model; anything past this is a corrupt file, not a
// meeting.
var ErrLiveNoteArtifactTooLarge = errors.New("live notes: artifact too large")

// liveNoteArtifactMaxBytes is the read cap for one artifact.
const liveNoteArtifactMaxBytes = 4 << 20

// liveNoteDirPattern matches the folder names EnsureLiveNotesDir produces:
// `<YYYY-MM-DD-HHMM>[-<slug>]-<token6>`. Anything else under the notes root is
// somebody else's directory and is left alone.
var liveNoteDirPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{4}(?:-.+)?$`)

// ReadLiveNoteArtifact returns one text artifact of a meeting folder.
//
// `dir` is a folder NAME, never a path: it is validated against the pattern the
// writer produces, so a crafted value cannot escape the notes root.
func ReadLiveNoteArtifact(projectPath, dir, kind string) (string, error) {
	body, _, err := ReadLiveNoteArtifactWithName(projectPath, dir, kind)
	return body, err
}

// ReadLiveNoteArtifactWithName is ReadLiveNoteArtifact plus the file name that
// was actually read — the UI opens the artifact in the file viewer and needs
// the resolved name (a transcript may still be the pre-SRT Markdown file).
func ReadLiveNoteArtifactWithName(projectPath, dir, kind string) (string, string, error) {
	if !validLiveNoteDirName(dir) {
		return "", "", ErrLiveNoteSessionNotFound
	}
	var candidates []string
	switch kind {
	case "minutes":
		candidates = []string{"minutes.md"}
	case "transcript":
		// SRT is the current artifact; older meetings only have the Markdown one.
		candidates = []string{liveNoteTranscriptSRT, liveNoteTranscriptLegacyMD}
	case "revised":
		candidates = []string{"transcript-revised.md"}
	case "chat":
		candidates = []string{"chat.md"}
	default:
		return "", "", ErrLiveNoteArtifactKind
	}

	for _, name := range candidates {
		full := filepath.Join(liveNotesRoot(projectPath), dir, name)
		info, err := os.Stat(full)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", "", fmt.Errorf("live notes: stat artifact: %w", err)
		}
		if info.IsDir() {
			continue
		}
		if info.Size() > liveNoteArtifactMaxBytes {
			return "", "", ErrLiveNoteArtifactTooLarge
		}
		body, err := os.ReadFile(full)
		if err != nil {
			return "", "", fmt.Errorf("live notes: read artifact: %w", err)
		}
		return string(body), name, nil
	}
	return "", "", ErrLiveNoteSessionNotFound
}

// liveNotesRoot is the per-project folder holding every meeting.
func liveNotesRoot(projectPath string) string {
	return filepath.Join(projectPath, filepath.FromSlash(LiveNotesDir))
}

// validLiveNoteDirName accepts only a single path element shaped like a folder
// this package created.
func validLiveNoteDirName(dir string) bool {
	if dir == "" || dir != filepath.Base(dir) || dir == "." || dir == ".." {
		return false
	}
	return liveNoteDirPattern.MatchString(dir)
}

// fileExists reports whether path is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// liveNotesFileSlug turns a share title into a safe single path element.
// Characters that are illegal on Windows are dropped rather than replaced, and
// the result is capped so the directory + name stays well inside path limits.
func liveNotesFileSlug(title string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(title) {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			continue
		case '\n', '\r', '\t':
			b.WriteRune(' ')
			continue
		}
		if r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	slug := strings.Join(strings.Fields(b.String()), " ")
	slug = strings.Trim(slug, ". ")
	if utf8.RuneCountInString(slug) > liveNotesNameMaxRunes {
		slug = string([]rune(slug)[:liveNotesNameMaxRunes])
	}
	return slug
}
