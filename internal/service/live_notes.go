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
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/store"
	"clawbench/internal/summarize"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
)

// LiveNotesDDL creates the tables behind AI 听记: segments, the meeting row,
// chat, the file timeline, the screen-share log and the key frames.
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
//
// The tables and the indexes are two constants because they are applied at
// different points of the upgrade: idx_live_notes_project_started references
// started_at, which only the ALTER TABLE migration adds on a database created
// before the meeting-record model. CREATE TABLE IF NOT EXISTS is a no-op there,
// so creating the index in the same batch fails with "no such column:
// started_at" and takes the whole startup down with it. LiveNotesDDL (tables +
// indexes, for fresh databases and tests) is just the two concatenated.
const LiveNotesTablesDDL = `
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
	kind TEXT NOT NULL DEFAULT '',
	-- hidden keeps a pre-record meeting (started_at = 0, from before the
	-- meeting-record model) out of the list without deleting its transcript.
	-- Production databases get this column from the ALTER probe in InitDB; it is
	-- declared here too so the DDL constant used by tests is self-contained.
	hidden INTEGER NOT NULL DEFAULT 0,
	-- share_enabled is the owner's opt-in for "this meeting's link keeps working
	-- after the meeting ends". DEFAULT 0 on purpose: the share token is minted
	-- automatically when the meeting starts (viewers need it to join), so
	-- without an explicit opt-in every finished meeting would be publicly
	-- readable forever just because it happened to be held.
	--
	-- It does NOT gate the live room: joining an in-progress meeting is the
	-- reason the link exists. Only the archived record endpoints check it.
	share_enabled INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

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

CREATE TABLE IF NOT EXISTS live_notes_files (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	owner TEXT NOT NULL DEFAULT 'host',
	owner_name TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL,
	opened_at INTEGER NOT NULL,
	closed_at INTEGER NOT NULL DEFAULT 0,
	source TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(token, owner, path, opened_at)
);

CREATE TABLE IF NOT EXISTS live_notes_screen (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	sharer TEXT NOT NULL,
	sharer_name TEXT NOT NULL DEFAULT '',
	started_at INTEGER NOT NULL,
	ended_at INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- One row per regenerable artifact (transcript / revised / minutes). Two axes
-- live here on purpose: content_at says when the content last CHANGED (drives
-- staleness), while job_status/job_at describe the last attempt (drives
-- "generating" and the 10-minute interruption rule). A single status cannot
-- express "regenerating while the old content is still on screen".
CREATE TABLE IF NOT EXISTS live_notes_artifacts (
	token      TEXT NOT NULL,
	kind       TEXT NOT NULL,
	job_status TEXT NOT NULL DEFAULT 'idle',
	error_code TEXT NOT NULL DEFAULT '',
	error      TEXT NOT NULL DEFAULT '',
	detail     TEXT NOT NULL DEFAULT '',
	content_at INTEGER NOT NULL DEFAULT 0,
	job_at     INTEGER NOT NULL DEFAULT 0,
	started_at INTEGER NOT NULL DEFAULT 0,
	-- transcript only: ASR finished but the VTT has not been written yet. A
	-- retry renders instead of paying for the whole recognition again.
	pending_asr_at INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (token, kind)
);

-- The absolute time each merged channel file starts at. Leading silent chunks
-- never become live_notes_segments rows, so MIN(started_at) cannot reconstruct
-- this origin; re-recognition needs it to place the new segments on the
-- timeline. Filled by the merge, not by the recording path.
CREATE TABLE IF NOT EXISTS live_notes_audio (
	token       TEXT NOT NULL,
	channel     TEXT NOT NULL,
	file        TEXT NOT NULL,
	base_at     INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (token, channel)
);

-- live_notes_shots holds the key frames captured during a meeting. The file
-- itself lives in <meeting dir>/shots; this row is the timeline entry, so the
-- record can place a frame between two transcript lines.
CREATE TABLE IF NOT EXISTS live_notes_shots (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0,
	client_shot_id TEXT NOT NULL,
	at_ms INTEGER NOT NULL,
	reason TEXT NOT NULL,
	ref TEXT NOT NULL DEFAULT '',
	file_name TEXT NOT NULL,
	width INTEGER NOT NULL DEFAULT 0,
	height INTEGER NOT NULL DEFAULT 0,
	bytes INTEGER NOT NULL DEFAULT 0,
	hash TEXT NOT NULL DEFAULT '',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(token, client_shot_id)
);
`

// LiveNotesIndexesDDL creates the indexes. Applied by InitDB *after* the
// column migration, never together with the tables — see the comment on
// LiveNotesTablesDDL.
const LiveNotesIndexesDDL = `
CREATE INDEX IF NOT EXISTS idx_live_note_segments_token ON live_notes_segments(token, started_at);
CREATE INDEX IF NOT EXISTS idx_live_notes_updated ON live_notes(updated_at);
CREATE INDEX IF NOT EXISTS idx_live_notes_project_started ON live_notes(project_id, started_at);
CREATE INDEX IF NOT EXISTS idx_live_note_chat_token ON live_notes_chat(token, id);
CREATE INDEX IF NOT EXISTS idx_live_note_chat_project ON live_notes_chat(project_id, token);
CREATE INDEX IF NOT EXISTS idx_live_note_files_token ON live_notes_files(token, opened_at);
CREATE INDEX IF NOT EXISTS idx_live_note_screen_token ON live_notes_screen(token, started_at);
CREATE INDEX IF NOT EXISTS idx_live_note_artifacts_token ON live_notes_artifacts(token);
CREATE INDEX IF NOT EXISTS idx_live_note_audio_token ON live_notes_audio(token);
CREATE INDEX IF NOT EXISTS idx_live_note_shots_token ON live_notes_shots(token, at_ms);
`

// LiveNotesDDL is the full schema for a fresh database (tables then indexes).
// Tests use it as their fixture; InitDB applies the two halves separately.
const LiveNotesDDL = LiveNotesTablesDDL + LiveNotesIndexesDDL

// Live note minutes statuses. Kept as constants so the handler, the SPA and the
// tests cannot drift on spelling.
const (
	LiveNotesStatusIdle       = "idle"
	LiveNotesStatusGenerating = "generating"
	LiveNotesStatusReady      = "ready"
	LiveNotesStatusFailed     = "failed"
)

// liveNotesHostID is the participant id the publisher uses for itself. The room
// (internal/liveshare) and the clients agree on it, so file owners, screen
// sharers and transcript labels all compare against the same value.
const liveNotesHostID = "host"

// ErrLiveNotesEmpty is returned when minutes are requested for a share that has
// no transcribed text.
var ErrLiveNotesEmpty = errors.New("live notes: transcript is empty")

// ErrLiveNotesBudget is returned when the minutes hit the configured output
// limit. It is deliberately distinct from a model failure: the transcript is
// fine, and the fix is to raise 会议设置 → 纪要输出上限 (or shorten the meeting),
// not to change the model.
var ErrLiveNotesBudget = errors.New("live notes: minutes exceeded the output limit")

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

// ensureLiveNoteRow makes sure the share has an owner row, so an event that
// arrives after the room was torn down still has a project to be checked
// against. Every publisher write starts with it.
func ensureLiveNoteRow(ctx context.Context, tx *sql.Tx, token string, projectID int64) error {
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO live_notes (token, project_id, status, started_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(token) DO UPDATE SET updated_at = CURRENT_TIMESTAMP`,
		token, projectID, LiveNotesStatusIdle, time.Now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("live notes: ensure row: %w", err)
	}
	return nil
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

	if ensureErr := ensureLiveNoteRow(ctx, tx, token, projectID); ensureErr != nil {
		return 0, ensureErr
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

// LiveNoteFile is one file a participant opened during a meeting.
//
// The row carries an interval rather than a timestamp: the transcript has to say
// "10:03–10:11 opened X", and a bare "opened at" cannot answer when the reader
// stopped looking. ClosedAt == 0 means the file was still open when the meeting
// ended; CloseOpenLiveNoteFiles stamps those.
type LiveNoteFile struct {
	ID        int64  `json:"id"`
	Owner     string `json:"owner"`
	OwnerName string `json:"ownerName"`
	// Path is the full path as the opener saw it. Empty in viewer responses —
	// the publisher's directory layout is not theirs to read.
	Path string `json:"path"`
	// Name is the file's base name and is always present. Viewers get only
	// this, which is enough to follow the meeting without exposing the tree.
	Name     string `json:"name"`
	OpenedAt int64  `json:"openedAt"`
	ClosedAt int64  `json:"closedAt"`
	Source   string `json:"source"`
}

// RecordLiveNoteFiles stores a batch of open/close events.
//
// Upsert on (token, owner, path, opened_at) so a publisher retrying after a
// flaky response is idempotent. closed_at only ever moves forward: a retried
// batch that still says "open" must not reopen an interval the server already
// closed at meeting end.
//
// The live_notes row is ensured for the same reason AppendLiveNoteSegment does
// it: an event that arrives after the room was torn down still needs an owner
// to check the project against.
func RecordLiveNoteFiles(ctx context.Context, token string, projectID int64, files []LiveNoteFile) error {
	if token == "" {
		return errors.New("live notes: empty token")
	}
	if len(files) == 0 {
		return nil
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return fmt.Errorf("live notes: begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if ensureErr := ensureLiveNoteRow(ctx, tx, token, projectID); ensureErr != nil {
		return ensureErr
	}
	for _, f := range files {
		path := strings.TrimSpace(f.Path)
		if path == "" || f.OpenedAt <= 0 {
			continue
		}
		owner := strings.TrimSpace(f.Owner)
		if owner == "" {
			owner = liveNotesHostID
		}
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO live_notes_files (token, project_id, owner, owner_name, path, opened_at, closed_at, source)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(token, owner, path, opened_at) DO UPDATE SET
				owner_name = excluded.owner_name,
				closed_at = MAX(live_notes_files.closed_at, excluded.closed_at),
				source = excluded.source`,
			token, projectID, owner,
			truncateRunes(strings.TrimSpace(f.OwnerName), liveNotesNameMaxRunes),
			path, f.OpenedAt, f.ClosedAt, truncateRunes(strings.TrimSpace(f.Source), 32),
		); execErr != nil {
			return fmt.Errorf("live notes: insert file: %w", execErr)
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("live notes: commit: %w", commitErr)
	}
	return nil
}

// ListLiveNoteFiles returns a share's file events with id > sinceID, ordered by
// id.
//
// Ordering by id (not opened_at) keeps `since` a correct incremental cursor: an
// event uploaded late can carry an earlier opened_at, so callers sort by time
// after appending, exactly like ListLiveNoteSegments.
func ListLiveNoteFiles(token string, sinceID int64) ([]LiveNoteFile, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, owner, owner_name, path, opened_at, closed_at, source
		   FROM live_notes_files
		  WHERE token = ? AND id > ?
		  ORDER BY id`,
		token, sinceID,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]LiveNoteFile, 0, 16)
	for rows.Next() {
		var f LiveNoteFile
		if err := rows.Scan(&f.ID, &f.Owner, &f.OwnerName, &f.Path, &f.OpenedAt, &f.ClosedAt, &f.Source); err != nil {
			return nil, fmt.Errorf("live notes: scan file: %w", err)
		}
		f.Name = liveNoteFileBase(f.Path)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("live notes: iterate files: %w", err)
	}
	return out, nil
}

// CloseOpenLiveNoteFiles stamps every still-open interval with the meeting's
// end. Idempotent: a second call finds nothing left to close.
func CloseOpenLiveNoteFiles(token string, atMS int64) error {
	if token == "" {
		return nil
	}
	if atMS <= 0 {
		atMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`UPDATE live_notes_files SET closed_at = ? WHERE token = ? AND closed_at = 0`,
		atMS, token,
	)
	if err != nil {
		return fmt.Errorf("live notes: close open files: %w", err)
	}
	return nil
}

// liveNoteFileBase returns the last path element, accepting both separators.
//
// filepath.Base is OS-specific, and the path comes from a client that may run on
// a different platform (a Windows publisher talking to a Linux server).
func liveNoteFileBase(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// liveNoteFileLabel is what a reader is shown for a file event: the full path
// when the caller has it (the publisher), the base name otherwise (viewer
// responses carry no path). Empty means the event cannot be shown at all.
func liveNoteFileLabel(f LiveNoteFile) string {
	if label := strings.TrimSpace(f.Path); label != "" {
		return label
	}
	return f.Name
}

// LiveNoteScreenShare is one interval of "this participant is sharing a screen".
//
// Sharer is "host" or "viewer:<id>" — the same identities the room uses, so a
// line in the transcript can be matched to the person the meeting UI showed.
type LiveNoteScreenShare struct {
	ID         int64  `json:"id"`
	Sharer     string `json:"sharer"`
	SharerName string `json:"sharerName"`
	StartedAt  int64  `json:"startedAt"`
	EndedAt    int64  `json:"endedAt"`
}

// SetLiveNoteScreenSharer records a CHANGE of who is sharing the screen.
//
// The client reports transitions, not state: sharer "" means nobody is sharing.
// Closing the previous interval and opening the next in one transaction is what
// keeps the timeline from ever showing two sharers at once — an overlapping
// "who was sharing" list is worse than none.
//
// Idempotent for a repeated transition: reporting the same sharer twice only
// refreshes the display name.
func SetLiveNoteScreenSharer(ctx context.Context, token string, projectID int64, sharer, name string, atMS int64) error {
	if token == "" {
		return errors.New("live notes: empty token")
	}
	if atMS <= 0 {
		atMS = time.Now().UnixMilli()
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return fmt.Errorf("live notes: begin: %w", err)
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	if ensureErr := ensureLiveNoteRow(ctx, tx, token, projectID); ensureErr != nil {
		return ensureErr
	}

	// The previous interval has to be dealt with before the new one is opened,
	// so the timeline never shows two sharers at once.
	refreshed, retireErr := retireOpenScreenShare(ctx, tx, token, sharer, name, atMS)
	if retireErr != nil {
		return retireErr
	}
	if refreshed {
		// Same sharer reported again (a reconnect, a duplicate watch): the label
		// is already refreshed, never split the interval.
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("live notes: commit: %w", commitErr)
		}
		return nil
	}

	if sharer != "" {
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO live_notes_screen (token, project_id, sharer, sharer_name, started_at)
			 VALUES (?, ?, ?, ?, ?)`,
			token, projectID, sharer,
			truncateRunes(strings.TrimSpace(name), liveNotesNameMaxRunes), atMS,
		); execErr != nil {
			return fmt.Errorf("live notes: insert screen share: %w", execErr)
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("live notes: commit: %w", commitErr)
	}
	return nil
}

// retireOpenScreenShare closes the interval that is still open, or — when the
// same sharer is reported again — refreshes its label without splitting the
// interval. refreshed reports that second case, where the caller has nothing
// left to insert.
func retireOpenScreenShare(ctx context.Context, tx *sql.Tx, token, sharer, name string, atMS int64) (refreshed bool, err error) {
	var (
		openID      int64
		openSharer  string
		openStarted int64
	)
	scanErr := tx.QueryRowContext(
		ctx,
		`SELECT id, sharer, started_at FROM live_notes_screen
		  WHERE token = ? AND ended_at = 0
		  ORDER BY id DESC LIMIT 1`,
		token,
	).Scan(&openID, &openSharer, &openStarted)
	switch {
	case scanErr == nil && openSharer == sharer:
		if _, execErr := tx.ExecContext(
			ctx, `UPDATE live_notes_screen SET sharer_name = ? WHERE id = ?`,
			truncateRunes(strings.TrimSpace(name), liveNotesNameMaxRunes), openID,
		); execErr != nil {
			return false, fmt.Errorf("live notes: update sharer name: %w", execErr)
		}
		return true, nil
	case scanErr == nil:
		end := atMS
		if end < openStarted {
			end = openStarted
		}
		if _, execErr := tx.ExecContext(
			ctx, `UPDATE live_notes_screen SET ended_at = ? WHERE id = ?`, end, openID,
		); execErr != nil {
			return false, fmt.Errorf("live notes: close screen share: %w", execErr)
		}
	case !errors.Is(scanErr, sql.ErrNoRows):
		return false, fmt.Errorf("live notes: read open screen share: %w", scanErr)
	}
	return false, nil
}

// ListLiveNoteScreenShares returns a share's screen-sharing intervals in
// chronological order.
func ListLiveNoteScreenShares(token string) ([]LiveNoteScreenShare, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, sharer, sharer_name, started_at, ended_at
		   FROM live_notes_screen
		  WHERE token = ?
		  ORDER BY started_at, id`,
		token,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list screen shares: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]LiveNoteScreenShare, 0, 8)
	for rows.Next() {
		var s LiveNoteScreenShare
		if err := rows.Scan(&s.ID, &s.Sharer, &s.SharerName, &s.StartedAt, &s.EndedAt); err != nil {
			return nil, fmt.Errorf("live notes: scan screen share: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("live notes: iterate screen shares: %w", err)
	}
	return out, nil
}

// CloseOpenLiveNoteScreenShare stamps the interval that was still running when
// the meeting ended. Idempotent.
func CloseOpenLiveNoteScreenShare(token string, atMS int64) error {
	if token == "" {
		return nil
	}
	if atMS <= 0 {
		atMS = time.Now().UnixMilli()
	}
	_, err := store.WriteExec(
		`UPDATE live_notes_screen SET ended_at = ? WHERE token = ? AND ended_at = 0`,
		atMS, token,
	)
	if err != nil {
		return fmt.Errorf("live notes: close open screen share: %w", err)
	}
	return nil
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

// PublicMeetingByToken resolves a share token to its meeting record WITHOUT a
// project scope.
//
// This is what makes a meeting link outlive the room: the room is in-memory and
// disappears the moment the publisher stops, while the record is permanent. The
// public share endpoints use this after the room lookup fails, so the same URL
// keeps answering with the meeting's minutes, transcript and chat until the
// record is deleted.
//
// Hidden rows (pre-record meetings the upgrade could not place on a timeline)
// are reported as missing, exactly like the list and the detail view: the link
// must not expose something the owner cannot even see.
func PublicMeetingByToken(token string) (LiveNote, bool, error) {
	if strings.TrimSpace(token) == "" {
		return LiveNote{}, false, nil
	}
	var n LiveNote
	var hidden int64
	err := store.ReadDB().QueryRow(
		`SELECT token, title, status, minutes, error, file_path, dir_name, revised,
		        revise_status, revise_error, started_at, ended_at, hidden
		   FROM live_notes WHERE token = ?`, token,
	).Scan(&n.Token, &n.Title, &n.Status, &n.Minutes, &n.Error, &n.FilePath,
		&n.DirName, &n.Revised, &n.ReviseStatus, &n.ReviseError, &n.StartedAt,
		&n.EndedAt, &hidden)
	if errors.Is(err, sql.ErrNoRows) {
		return LiveNote{}, false, nil
	}
	if err != nil {
		return LiveNote{}, false, fmt.Errorf("live notes: public get: %w", err)
	}
	if hidden != 0 {
		return LiveNote{}, false, nil
	}
	return n, true, nil
}

// PublicMeetingByTokenShared resolves a token to its meeting record only when
// the owner has opted in to publishing it.
//
// This is the gate for every ARCHIVED-record endpoint a stranger can reach. The
// live room is exempt (it is addressed by the same token and must stay joinable
// while the meeting runs) — callers check the registry first and only fall
// through to this function once the room is gone.
func PublicMeetingByTokenShared(token string) (LiveNote, bool, error) {
	n, ok, err := PublicMeetingByToken(token)
	if err != nil || !ok {
		return n, ok, err
	}
	enabled, err := MeetingShareEnabled(token)
	if err != nil {
		return LiveNote{}, false, err
	}
	if !enabled {
		return LiveNote{}, false, nil
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
//
// The legacy status columns are a projection for the live panel (`/notes`); the
// record list, detail and regeneration guard read live_notes_artifacts. Both
// are written here so the two surfaces cannot disagree about what is running.
//
// Content is only replaced on `ready`: starting or failing a job must not wipe
// the last good minutes, which is what made a failed regeneration lose the
// only copy the user had.
func SetLiveNoteMinutes(token string, projectID int64, title, status, minutes, errMsg, filePath string) error {
	var existing string
	err := store.ReadDB().QueryRow(`SELECT minutes FROM live_notes WHERE token = ?`, token).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		// Let the write below report the failure: it carries the operation's
		// established error prefix, and a store that cannot be read cannot be
		// written either.
		existing = ""
	}
	changed := status == LiveNotesStatusReady && existing != minutes

	nowMS := time.Now().UnixMilli()
	if _, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, title, status, minutes, error, file_path, started_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(token) DO UPDATE SET
		   status = excluded.status,
		   error = excluded.error,
		   minutes = CASE WHEN excluded.status = ? THEN excluded.minutes ELSE live_notes.minutes END,
		   file_path = CASE WHEN excluded.status = ? THEN excluded.file_path ELSE live_notes.file_path END,
		   title = CASE WHEN excluded.title <> '' THEN excluded.title ELSE live_notes.title END,
		   updated_at = CURRENT_TIMESTAMP`,
		token, projectID, title, status, minutes, errMsg, filePath, nowMS,
		LiveNotesStatusReady, LiveNotesStatusReady,
	); err != nil {
		return fmt.Errorf("live notes: set minutes: %w", err)
	}
	return syncArtifactFromLegacy(token, ArtifactMinutes, status, errMsg, changed, nowMS)
}

// SetLiveNoteRevise records the proofreading stage of a share's transcript.
//
// Kept separate from the minutes status on purpose: the two stages fail
// independently, and a failed proofread must not hide a perfectly good summary
// (which is then generated from the raw transcript).
func SetLiveNoteRevise(token string, projectID int64, status, revised, errMsg string) error {
	var existing string
	err := store.ReadDB().QueryRow(`SELECT revised FROM live_notes WHERE token = ?`, token).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		existing = ""
	}
	changed := status == LiveNotesStatusReady && existing != revised

	nowMS := time.Now().UnixMilli()
	if _, err := store.WriteExec(
		`INSERT INTO live_notes (token, project_id, revise_status, revised, revise_error, started_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(token) DO UPDATE SET
		   revise_status = excluded.revise_status,
		   revise_error = excluded.revise_error,
		   revised = CASE WHEN excluded.revise_status = ? THEN excluded.revised ELSE live_notes.revised END,
		   updated_at = CURRENT_TIMESTAMP`,
		token, projectID, status, revised, errMsg, nowMS, LiveNotesStatusReady,
	); err != nil {
		return fmt.Errorf("live notes: set revise: %w", err)
	}
	return syncArtifactFromLegacy(token, ArtifactRevised, status, errMsg, changed, nowMS)
}

// syncArtifactFromLegacy mirrors one legacy status write onto the artifact
// table. The artifact row is the authority for the record/detail; the legacy
// columns exist only so the live panel and the pre-artifact tests keep working.
func syncArtifactFromLegacy(token string, kind ArtifactKind, status, errMsg string, changed bool, nowMS int64) error {
	switch status {
	case LiveNotesStatusGenerating:
		return SetArtifactRunning(token, kind, "", nowMS)
	case LiveNotesStatusReady:
		contentAt := int64(0)
		if changed {
			contentAt = nowMS
		}
		return SetArtifactSucceeded(token, kind, contentAt, nowMS)
	case LiveNotesStatusFailed:
		code := ArtifactErrModel
		if strings.Contains(strings.ToLower(errMsg), "stt") || strings.Contains(strings.ToLower(errMsg), "transcri") {
			code = ArtifactErrSTT
		}
		return SetArtifactFailed(token, kind, code, errMsg, nowMS)
	default:
		return nil
	}
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

// placeholderTitleLayout is how an unnamed meeting is dated. It matches the
// list's own timestamp column, so a record the user never named still reads as
// "when this happened" rather than as a name.
const placeholderTitleLayout = "01-02 15:04"

// PlaceholderMeetingTitle is the name a meeting gets when the user gave none.
//
// It used to be the project's own name, which made every meeting in a project
// carry the identical title ("clawbench") and told the reader nothing. The
// summary model replaces this placeholder once the minutes exist
// (GenerateLiveNotesTitle), so it only has to be readable until then.
func PlaceholderMeetingTitle(at time.Time) string {
	return at.Format(placeholderTitleLayout)
}

// IsPlaceholderMeetingTitle reports whether title is still the untouched
// fallback — i.e. nobody has named this meeting yet, so a generated title may
// replace it.
//
// Format-based rather than compared against started_at on purpose: the title is
// stamped when the ROOM is created and started_at when the publisher ATTACHES,
// and those two can straddle a minute boundary. The only false positive is a
// user who types something shaped exactly like "11-01 17:35", which is not
// worth a schema column to rule out.
func IsPlaceholderMeetingTitle(title string) bool {
	title = strings.TrimSpace(title)
	if len(title) != len(placeholderTitleLayout) {
		return false
	}
	_, err := time.Parse(placeholderTitleLayout, title)
	return err == nil
}

// SetMeetingTitle renames one meeting record.
//
// Only the record's title changes. The FOLDER keeps the name it was created
// with (`liveNotesMeetingDirName`): every artifact path, the artifacts
// endpoint and anything the reader opened already point at it, and moving it
// under a running job would break all of them at once. A rename changes how
// the meeting is listed, quoted and titled — not where its leftovers live.
//
// A hidden row (a pre-record meeting the upgrade could not place) is reported
// as not found, exactly like a missing one: it is invisible everywhere else.
func SetMeetingTitle(projectID int64, token, title string) error {
	if token == "" {
		return ErrMeetingNotFound
	}
	res, err := store.WriteExec(
		`UPDATE live_notes SET title = ?, updated_at = CURRENT_TIMESTAMP
		  WHERE token = ? AND project_id = ? AND hidden = 0`,
		title, token, projectID,
	)
	if err != nil {
		return fmt.Errorf("live notes: rename meeting: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("live notes: rename meeting: %w", err)
	}
	if n == 0 {
		return ErrMeetingNotFound
	}
	return nil
}

// SetMeetingShareEnabled turns the meeting record's public link on or off.
//
// This is the owner's opt-in for "keep this meeting readable after it ends".
// It is deliberately NOT the switch that lets people join a running meeting —
// the live room is addressed by the same token and must stay reachable while
// the meeting is in progress (that is what the link is handed out for).
//
// A hidden row is reported as not found, exactly like a missing one.
func SetMeetingShareEnabled(projectID int64, token string, enabled bool) error {
	if token == "" {
		return ErrMeetingNotFound
	}
	flag := 0
	if enabled {
		flag = 1
	}
	res, err := store.WriteExec(
		`UPDATE live_notes SET share_enabled = ?, updated_at = CURRENT_TIMESTAMP
		  WHERE token = ? AND project_id = ? AND hidden = 0`,
		flag, token, projectID,
	)
	if err != nil {
		return fmt.Errorf("live notes: set share flag: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("live notes: set share flag: %w", err)
	}
	if n == 0 {
		return ErrMeetingNotFound
	}
	return nil
}

// MeetingShareEnabled reports whether a meeting record's link is public.
//
// An unknown token answers false rather than an error: the callers are public
// endpoints that must degrade to "not shared" (a 404) instead of a 500.
func MeetingShareEnabled(token string) (bool, error) {
	if strings.TrimSpace(token) == "" {
		return false, nil
	}
	var flag int
	err := store.ReadDB().QueryRow(
		`SELECT share_enabled FROM live_notes WHERE token = ?`, token,
	).Scan(&flag)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("live notes: share flag: %w", err)
	}
	return flag != 0, nil
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

// CloseOrphanedLiveNotes closes every record still marked as running and
// reports how many it closed.
//
// Called ONCE at process start, before any room can exist. Live rooms are
// in-memory only, so after a restart no open record can belong to a running
// meeting: the process that owned it is gone (crash, SIGKILL, OOM kill, a
// reboot), and its socket teardown — the only thing that stamps the finish
// line — never ran. Without this the list keeps showing 进行中 (and ticking a
// clock) until the hourly sweep's 12h staleness cutoff.
//
// The finish line is the row's LAST WRITE rather than "now": a server that was
// down overnight must not stretch a 20-minute meeting across the downtime.
// Clamped to [started_at, nowMS] so the duration can neither go negative nor
// point into the future.
func CloseOrphanedLiveNotes(nowMS int64) (int, error) {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	// Before stamping finish lines: that UPDATE bumps updated_at, which is the
	// only evidence ResetStaleLiveNoteGeneration has for "this job is dead".
	if _, err := ResetStaleLiveNoteGeneration(nowMS); err != nil {
		return 0, err
	}
	// Same sweep for the artifact table (the authority for the record/detail).
	// The legacy columns above only feed the live panel.
	if _, err := ResetStaleArtifactJobs(nowMS); err != nil {
		return 0, err
	}
	res, err := store.WriteExec(
		`UPDATE live_notes
		    SET ended_at = MIN(?, MAX(started_at,
		          COALESCE(CAST(strftime('%s', updated_at) AS INTEGER) * 1000, 0))),
		        updated_at = CURRENT_TIMESTAMP
		  WHERE ended_at = 0`,
		nowMS,
	)
	if err != nil {
		return 0, fmt.Errorf("live notes: close orphaned meetings: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CloseStaleLiveNotes closes records whose publisher never reported a clean
// end (process crash, killed app). Without this the list would show meetings
// that never finish, and the delete guard would refuse to remove them.
//
// This is the BACKSTOP: a restart closes the records it orphaned immediately
// (CloseOrphanedLiveNotes), so what is left here are rows whose writer vanished
// while the process stayed up — a late upload recreating a deleted meeting, for
// one. `beforeMS` is the cutoff on updated_at; the reaper passes "now - 12h".
func CloseStaleLiveNotes(beforeMS int64) (int, error) {
	if beforeMS <= 0 {
		return 0, nil
	}
	// Same ordering reason as CloseOrphanedLiveNotes: the close below refreshes
	// updated_at, which would hide a long-dead generating row from the reset.
	if _, err := ResetStaleLiveNoteGeneration(time.Now().UnixMilli()); err != nil {
		return 0, err
	}
	if _, err := ResetStaleArtifactJobs(time.Now().UnixMilli()); err != nil {
		return 0, err
	}
	cutoff := time.UnixMilli(beforeMS).UTC().Format(liveNoteSQLTimeLayout)
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

// liveNoteSQLTimeLayout is how SQLite's CURRENT_TIMESTAMP writes updated_at.
// Comparing against a formatted string is a lexicographic comparison of
// identical-shape timestamps, which is why the cutoff is formatted, not
// compared as a number.
const liveNoteSQLTimeLayout = "2006-01-02 15:04:05"

// liveNotesGeneratingStaleMS is how long a `generating` row may go without any
// write before its job is presumed dead.
//
// Ten minutes is the same threshold the SPA uses (isStaleGenerating in
// useMeetingHistory.ts): the list labels such a meeting 「已中断」, so the delete
// guard must agree or the user sees an interrupted meeting the server refuses
// to delete.
const liveNotesGeneratingStaleMS = 10 * 60 * 1000

// ResetStaleLiveNoteGeneration flips abandoned `generating` rows to `failed`
// and reports how many it touched.
//
// A generating row is written by a detached goroutine with a ten-minute budget.
// If the process dies inside that window (restart, SIGKILL, OOM) nothing ever
// writes the terminal status again, so the row stays `generating` forever: the
// list shows 「生成中断」 and the delete guard used to refuse with 409, making the
// meeting — and its folder, transcript and chat — undeletable. There is no
// in-memory job registry to consult (the job died with the process), so age is
// the only signal available.
//
// updated_at is deliberately NOT refreshed: it is the evidence for this
// decision, and the SPA keeps using it to date the interruption.
func ResetStaleLiveNoteGeneration(nowMS int64) (int, error) {
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	cutoff := time.UnixMilli(nowMS - liveNotesGeneratingStaleMS).UTC().Format(liveNoteSQLTimeLayout)
	res, err := store.WriteExec(
		`UPDATE live_notes
		    SET status = CASE WHEN status = ? THEN ? ELSE status END,
		        error = CASE WHEN status = ? THEN ? ELSE error END,
		        revise_status = CASE WHEN revise_status = ? THEN ? ELSE revise_status END,
		        revise_error = CASE WHEN revise_status = ? THEN ? ELSE revise_error END
		  WHERE updated_at < ? AND (status = ? OR revise_status = ?)`,
		LiveNotesStatusGenerating, LiveNotesStatusFailed,
		LiveNotesStatusGenerating, "interrupted: no progress for 10 minutes",
		LiveNotesStatusGenerating, LiveNotesStatusFailed,
		LiveNotesStatusGenerating, "interrupted: no progress for 10 minutes",
		cutoff,
		LiveNotesStatusGenerating, LiveNotesStatusGenerating,
	)
	if err != nil {
		return 0, fmt.Errorf("live notes: reset stale generation: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// LiveNoteGenerationRunning reports whether a minutes or proofread job on this
// meeting is still plausibly running.
//
// The same age threshold as ResetStaleLiveNoteGeneration, applied as a read:
// the delete guard must not depend on the hourly sweep having run since the
// crash, or a meeting stays undeletable for up to an hour after a restart.
func LiveNoteGenerationRunning(token string, nowMS int64) (bool, error) {
	if token == "" {
		return false, nil
	}
	if nowMS <= 0 {
		nowMS = time.Now().UnixMilli()
	}
	cutoff := time.UnixMilli(nowMS - liveNotesGeneratingStaleMS).UTC().Format(liveNoteSQLTimeLayout)
	var n int
	err := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM live_notes
		  WHERE token = ? AND (status = ? OR revise_status = ?) AND updated_at >= ?`,
		token, LiveNotesStatusGenerating, LiveNotesStatusGenerating, cutoff,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("live notes: generation lookup: %w", err)
	}
	return n > 0, nil
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
	// HasMeetingAudio reports the merged whole-meeting file. False for a
	// recording that is still split into segments (meetings recorded before the
	// merge existed), which the SPA shows as "N clips" instead.
	HasMeetingAudio bool `json:"hasMeetingAudio"`
	AudioFiles      int  `json:"audioFiles"`
	ChatCount       int  `json:"chatCount"`
	// ShotCount is how many key frames were kept. Derived from the folder like
	// the other artifact flags, so a frame deleted from disk stops counting.
	ShotCount int `json:"shotCount"`
	// HasSegments / HasAudio answer "can anything be regenerated": a meeting
	// with neither has no source for any of the three artifacts.
	HasSegments bool `json:"hasSegments"`
	HasAudio    bool `json:"hasAudio"`
	// Artifacts carries the per-artifact job state; Generation is the derived
	// "which step is running" summary (nil when nothing is running).
	//
	// There is deliberately no single `status` field: it cannot express
	// "regenerating while the old minutes are still readable". The old minutes
	// job state survives only as the /notes live-panel projection.
	Artifacts  map[ArtifactKind]MeetingArtifactView `json:"artifacts"`
	Generation *MeetingGeneration                   `json:"generation"`
	// SpeakerCount is how many distinct people the transcript heard. Derived
	// from the segments like the artifact flags, so it disappears with them.
	SpeakerCount int `json:"speakerCount"`
	// UpdatedAt is the row's last write, used by the record's own lifecycle
	// (the artifact heartbeat is separate).
	UpdatedAt int64 `json:"updatedAt"`
	// ShareEnabled is the owner's opt-in for "this meeting's link keeps working
	// after it ends". Off by default: the token is minted automatically so
	// viewers can join, which is not the same as publishing the record.
	ShareEnabled bool `json:"shareEnabled"`
}

// ListMeetingRecords returns a project's meetings, newest first.
//
// hidden rows are skipped: they are pre-record meetings the upgrade could not
// place on a timeline (started_at = 0). They are kept in the table — and their
// segments with them — so nothing is lost, but the list is the user's view and
// must not show them.
func ListMeetingRecords(projectPath string, projectID int64, limit int) ([]MeetingRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := store.ReadDB().Query(
		`SELECT token, title, started_at, ended_at, dir_name,
		        CAST(strftime('%s', updated_at) AS INTEGER) * 1000,
		        share_enabled
		   FROM live_notes
		  WHERE project_id = ? AND hidden = 0
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
		var shareEnabled int
		if scanErr := rows.Scan(&m.Token, &m.Title, &m.StartedAt, &m.EndedAt, &dirName,
			&updatedMS, &shareEnabled); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan meeting: %w", scanErr)
		}
		m.UpdatedAt = updatedMS
		m.ShareEnabled = shareEnabled != 0
		if dirName != "" {
			m.Dir = dirName
			m.DirPath = MeetingDirPath(projectPath, dirName)
			flags := liveNoteArtifactFlags(m.DirPath)
			applyLiveNoteArtifactFlags(&m, flags)
			m.HasAudio = flags.hasMeetingAudio || flags.audioFiles > 0
		}
		out = append(out, m)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("live notes: iterate meetings: %w", rowsErr)
	}

	counts, err := liveNoteChatCounts(projectID)
	if err != nil {
		return nil, err
	}
	speakers, err := liveNoteSpeakerCounts(projectID)
	if err != nil {
		return nil, err
	}
	segments, err := liveNoteSegmentCounts(projectID)
	if err != nil {
		return nil, err
	}
	tokens := make([]string, len(out))
	for i := range out {
		tokens[i] = out[i].Token
	}
	states, err := ArtifactStatesForTokens(tokens)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	for i := range out {
		m := &out[i]
		m.ChatCount = counts[m.Token]
		m.SpeakerCount = speakers[m.Token]
		m.HasSegments = segments[m.Token] > 0
		flags := liveNoteArtifactState{
			hasMinutes:      m.HasMinutes,
			hasTranscript:   m.HasTranscript,
			hasRevised:      m.HasRevised,
			hasMeetingAudio: m.HasMeetingAudio,
			audioFiles:      m.AudioFiles,
		}
		m.Artifacts, m.Generation = BuildMeetingArtifactViews(flags, states[m.Token], now)
	}
	return out, nil
}

// GetMeetingRecord returns one record by token, scoped to the project.
//
// A hidden row is reported as not found, exactly like a missing one: the detail
// view must agree with the list it was opened from.
func GetMeetingRecord(projectPath string, projectID int64, token string) (MeetingRecord, bool, error) {
	var m MeetingRecord
	var dirName string
	var updatedMS int64
	var shareEnabled int
	err := store.ReadDB().QueryRow(
		`SELECT token, title, started_at, ended_at, dir_name,
		        CAST(strftime('%s', updated_at) AS INTEGER) * 1000,
		        share_enabled
		   FROM live_notes WHERE token = ? AND project_id = ? AND hidden = 0`, token, projectID,
	).Scan(&m.Token, &m.Title, &m.StartedAt, &m.EndedAt, &dirName, &updatedMS, &shareEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return MeetingRecord{}, false, nil
	}
	if err != nil {
		return MeetingRecord{}, false, fmt.Errorf("live notes: get meeting: %w", err)
	}
	m.UpdatedAt = updatedMS
	m.ShareEnabled = shareEnabled != 0
	var flags liveNoteArtifactState
	if dirName != "" {
		m.Dir = dirName
		m.DirPath = MeetingDirPath(projectPath, dirName)
		flags = liveNoteArtifactFlags(m.DirPath)
		applyLiveNoteArtifactFlags(&m, flags)
		m.HasAudio = flags.hasMeetingAudio || flags.audioFiles > 0
	}
	counts, err := liveNoteChatCounts(projectID)
	if err != nil {
		return MeetingRecord{}, false, err
	}
	m.ChatCount = counts[token]
	speakers, err := liveNoteSpeakerCounts(projectID)
	if err != nil {
		return MeetingRecord{}, false, err
	}
	m.SpeakerCount = speakers[token]
	var segmentCount int
	if scanErr := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM live_notes_segments WHERE token = ? AND project_id = ?`,
		token, projectID,
	).Scan(&segmentCount); scanErr != nil {
		return MeetingRecord{}, false, fmt.Errorf("live notes: segment count: %w", scanErr)
	}
	m.HasSegments = segmentCount > 0
	states, err := ArtifactStatesForToken(token)
	if err != nil {
		return MeetingRecord{}, false, err
	}
	m.Artifacts, m.Generation = BuildMeetingArtifactViews(flags, states, time.Now().UnixMilli())
	return m, true, nil
}

// liveNoteSegmentCounts returns how many transcript segments each meeting has,
// per token. Used to answer "can this meeting be regenerated at all".
func liveNoteSegmentCounts(projectID int64) (map[string]int, error) {
	rows, err := store.ReadDB().Query(
		`SELECT token, COUNT(*) FROM live_notes_segments
		  WHERE project_id = ? GROUP BY token`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: segment counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]int, 16)
	for rows.Next() {
		var token string
		var n int
		if scanErr := rows.Scan(&token, &n); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan segment count: %w", scanErr)
		}
		out[token] = n
	}
	return out, rows.Err()
}

// liveNoteSpeakerCounts returns how many distinct speakers each transcript
// heard, per token.
//
// Empty labels are skipped: a segment uploaded before the client knew the
// speaker would otherwise count as a person.
func liveNoteSpeakerCounts(projectID int64) (map[string]int, error) {
	rows, err := store.ReadDB().Query(
		`SELECT token, COUNT(DISTINCT speaker) FROM live_notes_segments
		  WHERE project_id = ? AND speaker <> '' GROUP BY token`, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: speaker counts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]int, 16)
	for rows.Next() {
		var token string
		var n int
		if scanErr := rows.Scan(&token, &n); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan speaker count: %w", scanErr)
		}
		out[token] = n
	}
	return out, rows.Err()
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
func liveNoteArtifactFlags(dirPath string) liveNoteArtifactState {
	var st liveNoteArtifactState
	if dirPath == "" {
		return st
	}
	paths := liveNotesPathsFor(dirPath)
	st.hasMinutes = fileExists(paths.Minutes)
	st.hasTranscript = fileExists(paths.Transcript)
	st.hasRevised = fileExists(paths.Revised)
	st.hasMeetingAudio = fileExists(filepath.Join(paths.Audio, LiveNoteMeetingAudioFile))
	if entries, err := os.ReadDir(paths.Audio); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".webm") {
				continue
			}
			// The merged whole-meeting file is not a channel recording; counting
			// it would make "2 段录音" mean one microphone and one mix.
			if e.Name() == LiveNoteMeetingAudioFile {
				continue
			}
			st.audioFiles++
		}
	}
	st.shots = liveNoteShotCountIn(paths.Shots)
	return st
}

// applyLiveNoteArtifactFlags copies a folder probe onto a record.
func applyLiveNoteArtifactFlags(m *MeetingRecord, st liveNoteArtifactState) {
	m.HasMinutes = st.hasMinutes
	m.HasTranscript = st.hasTranscript
	m.HasRevised = st.hasRevised
	m.HasMeetingAudio = st.hasMeetingAudio
	m.AudioFiles = st.audioFiles
	m.ShotCount = st.shots
}

// liveNoteArtifactState is what one meeting folder actually holds. A struct
// rather than a return list because the set keeps growing and a positional call
// site would silently mix up two bools.
type liveNoteArtifactState struct {
	hasMinutes      bool
	hasTranscript   bool
	hasRevised      bool
	hasMeetingAudio bool
	audioFiles      int
	shots           int
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
		// Defense in depth: the name is generated by us, but a corrupted row
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
	// The exported signature carries no ctx (callers delete synchronously from
	// an HTTP handler), so the statements run on the background context: the
	// transaction is five short DELETEs.
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM live_notes_chat WHERE token = ?`,
		`DELETE FROM live_notes_segments WHERE token = ?`,
		`DELETE FROM live_notes_files WHERE token = ?`,
		`DELETE FROM live_notes_screen WHERE token = ?`,
		`DELETE FROM live_notes_artifacts WHERE token = ?`,
		`DELETE FROM live_notes_audio WHERE token = ?`,
		`DELETE FROM live_notes_shots WHERE token = ?`,
		`DELETE FROM live_notes WHERE token = ? AND project_id = ?`,
	} {
		args := []any{token}
		if strings.Contains(stmt, "project_id") {
			args = append(args, projectID)
		}
		if _, execErr := tx.ExecContext(ctx, stmt, args...); execErr != nil {
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
//
// The id always comes from a lookup, never from LastInsertId: SQLite leaves
// last_insert_rowid() untouched when ON CONFLICT DO NOTHING skips the insert, so
// the result would be the id of whatever THIS CONNECTION inserted last — a
// different message. (Modernc exposes exactly sqlite3_last_insert_rowid.)
func AppendLiveNoteChat(token string, projectID int64, msg LiveNoteChatMessage) (int64, error) {
	if token == "" || strings.TrimSpace(msg.Text) == "" || strings.TrimSpace(msg.ClientID) == "" {
		return 0, errors.New("live notes: invalid chat message")
	}
	if _, err := store.WriteExec(
		`INSERT INTO live_notes_chat (token, project_id, client_msg_id, speaker, viewer_id, text)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(token, client_msg_id) DO NOTHING`,
		token, projectID, msg.ClientID, msg.Speaker, msg.ViewerID, msg.Text,
	); err != nil {
		return 0, fmt.Errorf("live notes: append chat: %w", err)
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
	sep := liveNoteT("MeetingTranscriptSeparator")
	var b strings.Builder
	b.WriteString("# " + liveNoteT("MeetingChatTitle") + "\n\n")
	for _, m := range msgs {
		speaker := liveNoteOneLine(m.Speaker)
		if speaker == "" {
			speaker = liveNoteT("MeetingChatAnonymous")
		}
		when := ""
		if m.At > 0 {
			when = "[" + time.UnixMilli(m.At).Format("15:04") + "] "
		}
		b.WriteString(when + speaker + sep + liveNoteOneLine(m.Text) + "\n\n")
	}
	path := filepath.Join(paths.Dir, "chat.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("live notes: write chat archive: %w", err)
	}
	return path, nil
}

// liveNoteOneLine flattens a client-supplied label or message onto one line.
//
// The speaker is a nickname a participant typed; a newline (or any other control
// character) in it would start a new Markdown paragraph — or a whole new chat
// line — and let the sender forge what looks like somebody else's message. The
// handler sanitizes new names on the way in, but rows written before it did are
// still in the table, so the archive cleans up on the way out too. Same rule as
// handler.sanitizeLiveViewerName: control characters become spaces, runs of
// whitespace collapse to one.
func liveNoteOneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
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

// liveNoteSystemChannel is the system-audio loopback: the one channel that
// records sound the publisher did not make locally (everything playing through
// its speakers).
const liveNoteSystemChannel = "system"

// liveNoteChannelsEcho reports whether two DIFFERENT channels can hold the same
// utterance — i.e. one is the loopback and the other is a real capture.
//
// The channel pair is what makes dedupe safe. Two segments on the SAME channel
// are two utterances: somebody saying 「好」 twice three seconds apart, or two
// viewers both answering 「对」, are ordinary meeting speech, not an echo. Only
// the loopback can duplicate another channel's words.
func liveNoteChannelsEcho(a, b string) bool {
	if a == b {
		return false
	}
	return a == liveNoteSystemChannel || b == liveNoteSystemChannel
}

// DedupeLiveNoteSegments drops a segment when another CHANNEL captured the same
// words at nearly the same moment.
//
// This is the speaker's own voice leaking into a second channel: the audience
// playing through the publisher's speakers is re-captured by the system-audio
// loopback, and the publisher's microphone picks up whatever the speakers play.
// Without this, every remote participant appears twice.
//
// The earliest segment wins (it is the one closest to the source); the later
// copy is dropped. Genuine simultaneous speech by two people differs in words,
// so it is unaffected — and identical words on one channel (or on two channels
// that are not the loopback) are kept, because that is two real utterances.
func DedupeLiveNoteSegments(sorted []LiveNoteSegment) []LiveNoteSegment {
	out := make([]LiveNoteSegment, 0, len(sorted))
	type kept struct {
		norm    string
		at      int64
		channel string
	}
	recent := make([]kept, 0, 8)
	for _, s := range sorted {
		norm := normalizeNoteText(s.Text)
		if norm == "" {
			continue
		}
		dup := false
		for _, k := range recent {
			if k.norm == norm && absInt64(k.at-s.StartedAt) <= liveNotesDedupeWindowMS &&
				liveNoteChannelsEcho(k.channel, s.Channel) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		recent = append(recent, kept{norm: norm, at: s.StartedAt, channel: s.Channel})
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
	sep := liveNoteT("MeetingTranscriptSeparator")
	for _, s := range merged {
		speaker := strings.TrimSpace(s.Speaker)
		if speaker == "" {
			speaker = s.Channel
		}
		text := strings.Join(strings.Fields(s.Text), " ")
		if s.StartedAt <= 0 || base == 0 {
			// Unknown timing: say so rather than inventing an offset.
			fmt.Fprintf(&b, "[--:--] %s%s%s\n", speaker, sep, text)
			continue
		}
		offset := (s.StartedAt - base) / 1000
		if offset < 0 {
			offset = 0
		}
		fmt.Fprintf(&b, "[%02d:%02d] %s%s%s\n", offset/60, offset%60, speaker, sep, text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// LiveNotesTimelineBase is the epoch-ms origin the transcript offsets are
// relative to.
//
// Anything merged into a transcript (file events, for instance) has to use this
// same origin: a second timeline computed from a different base would put every
// line at the wrong minute and look plausible while doing it.
func LiveNotesTimelineBase(segments []LiveNoteSegment) int64 {
	_, base := mergedLiveNoteTimeline(segments)
	return base
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

// liveNoteFallbackCueMS is the cue length used when a segment carries no
// duration. A subtitle cue needs an end after its start, so a missing duration
// cannot be written as-is.
const liveNoteFallbackCueMS = 2000

// BuildLiveNotesVTT renders the merged timeline as WebVTT.
//
// The raw transcript is a subtitle track rather than Markdown because Markdown
// collapses consecutive lines into one paragraph — every utterance ran together
// when the file was previewed — and because the timing is what makes the file
// usable next to the recording.
//
// WebVTT rather than SubRip for two reasons. The cue identifier carries the
// CHANNEL (`mic` / `viewer:<id>`), which is the only stable speaker identity
// here: display names get renamed, reused and localized, so a file that keeps
// nothing but "观众 1" cannot be attributed afterwards. The voice span
// (`<v 名字>`) carries the name a player shows next to the line. Browsers also
// only support VTT in <track>, so SRT could not be played without a JS parser.
//
// File events ride the same track. They are not speech, but they share the one
// thing a transcript is for — what happened when — and a separate file would
// leave the reader to correlate two timelines by eye. Their cue id is `file`, so
// a renderer can draw them differently instead of inventing a speaker avatar.
func BuildLiveNotesVTT(segments []LiveNoteSegment, files []LiveNoteFile, screens []LiveNoteScreenShare) string {
	merged, base := mergedLiveNoteTimeline(segments)
	if len(merged) == 0 && len(files) == 0 && len(screens) == 0 {
		return ""
	}

	cues := liveNoteSpeechCues(merged, base)
	if base <= 0 {
		// Without a timeline origin an absolute event time would be rendered as
		// "1970" relative to nothing. Speech still chains forward (see
		// liveNoteSpeechCues); file events are dropped rather than placed at an
		// invented moment.
		files = nil
	}
	cues = append(cues, liveNoteFileCues(files, base)...)
	cues = append(cues, liveNoteScreenCues(screens, base)...)
	if len(cues) == 0 {
		return ""
	}
	// Stable so equal timestamps keep speech before the file it triggered.
	sort.SliceStable(cues, func(i, j int) bool { return cues[i].start < cues[j].start })

	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, c := range cues {
		if c.id != "" {
			b.WriteString(c.id)
			b.WriteString("\n")
		}
		if c.voice != "" {
			fmt.Fprintf(&b, "%s --> %s\n<v %s>%s\n\n",
				vttTimestamp(c.start), vttTimestamp(c.end), c.voice, vttEscape(c.text))
			continue
		}
		fmt.Fprintf(&b, "%s --> %s\n%s\n\n",
			vttTimestamp(c.start), vttTimestamp(c.end), vttEscape(c.text))
	}
	return b.String()
}

// liveNoteSpeechCues turns the merged utterances into cues.
//
// A segment without timing chains forward from the end of the previous cue: a
// subtitle needs a start, and inventing an absolute one is worse than letting
// it follow the line before.
func liveNoteSpeechCues(merged []LiveNoteSegment, base int64) []vttCue {
	cues := make([]vttCue, 0, len(merged))
	cursor := int64(0) // end of the previous speech cue
	for _, s := range merged {
		text := strings.Join(strings.Fields(s.Text), " ")
		if text == "" {
			continue
		}
		speaker := strings.TrimSpace(s.Speaker)
		if speaker == "" {
			speaker = s.Channel
		}

		start, end := cursor, cursor+liveNoteFallbackCueMS
		if s.StartedAt > 0 && base > 0 {
			start = liveNoteOffset(s.StartedAt, base)
			end = start + s.DurationMS
			if end <= start {
				end = start + liveNoteFallbackCueMS
			}
		}
		cursor = end

		cues = append(cues, vttCue{
			start: start,
			end:   end,
			id:    vttCueID(s.Channel),
			voice: vttVoiceName(speaker),
			text:  text,
		})
	}
	return cues
}

// vttCue is one cue before rendering, so speech and file events can be merged
// by start time instead of written as two blocks the reader has to interleave.
type vttCue struct {
	start int64
	end   int64
	id    string
	voice string
	text  string
}

// LiveNoteFileCueID marks a cue that describes a file event rather than speech.
// The SPA keys on it to draw a file row instead of a speaker avatar.
const LiveNoteFileCueID = "file"

// LiveNoteScreenCueID marks a cue that describes a change of screen sharer.
const LiveNoteScreenCueID = "screen"

// liveNoteLoc is the localizer for meeting artifacts.
//
// These are written without an HTTP request (the meeting folder is produced by
// a background job), so the configured UI language is the only signal. An empty
// value happens in unit tests and on a zero config; it must mean the product
// default rather than "English by accident".
func liveNoteLoc() *goi18n.Localizer {
	lang := model.Language
	if lang == "" {
		lang = model.DefaultLanguage
	}
	return i18n.LocalizerForLocale(lang)
}

// liveNoteT localizes one meeting-artifact string.
func liveNoteT(key string, data ...map[string]interface{}) string {
	return i18n.T(liveNoteLoc(), key, data...)
}

// Template placeholders of the meeting-artifact strings. They must match the
// `{{.X}}` names in the i18n catalogs, and are shared by the WebVTT cues and
// the proofread-transcript lines so the two renderings cannot drift.
const (
	i18nPathKey = "Path"
	i18nStayKey = "Stay"
)

// liveNoteOffset is an event instant relative to the timeline origin, clamped
// at zero: an event stamped before the first utterance would otherwise render
// as a negative timestamp.
func liveNoteOffset(at, base int64) int64 {
	if at <= base {
		return 0
	}
	return at - base
}

// liveNoteFileCues turns intervals into open/close cues.
//
// Two short cues rather than one long one: a cue spanning the whole interval
// would sit on screen for the entire duration (unusable as a subtitle), while
// the pair reads exactly like the event log it is.
func liveNoteFileCues(files []LiveNoteFile, base int64) []vttCue {
	if len(files) == 0 || base <= 0 {
		return nil
	}
	voice := liveNoteT("MeetingFilesSpeaker")
	out := make([]vttCue, 0, len(files)*2)
	for _, f := range files {
		label := liveNoteFileLabel(f)
		if f.OpenedAt <= 0 || label == "" {
			continue
		}
		open := liveNoteOffset(f.OpenedAt, base)
		out = append(out, vttCue{
			start: open,
			end:   open + liveNoteFallbackCueMS,
			id:    LiveNoteFileCueID,
			voice: voice,
			text:  liveNoteT("MeetingFilesOpened", map[string]interface{}{i18nPathKey: label}),
		})
		if f.ClosedAt <= f.OpenedAt {
			continue
		}
		closeAt := liveNoteOffset(f.ClosedAt, base)
		out = append(out, vttCue{
			start: closeAt,
			end:   closeAt + liveNoteFallbackCueMS,
			id:    LiveNoteFileCueID,
			voice: voice,
			text: liveNoteT("MeetingFilesClosed", map[string]interface{}{
				i18nPathKey: label,
				i18nStayKey: liveNoteDurationSuffix(f.ClosedAt - f.OpenedAt),
			}),
		})
	}
	return out
}

// liveNoteScreenCues turns screen-sharing intervals into open/close cues.
//
// "Who is sharing" is a fact about the meeting, not about the audio: when the
// host hands the screen to a participant, everything said afterwards refers to
// a different picture. Recording it makes the transcript readable months later.
func liveNoteScreenCues(screens []LiveNoteScreenShare, base int64) []vttCue {
	if len(screens) == 0 || base <= 0 {
		return nil
	}
	voice := liveNoteT("MeetingScreenVoice")
	out := make([]vttCue, 0, len(screens)*2)
	for _, s := range screens {
		if s.StartedAt <= 0 {
			continue
		}
		name := liveNoteScreenName(s)
		start := liveNoteOffset(s.StartedAt, base)
		out = append(out, vttCue{
			start: start,
			end:   start + liveNoteFallbackCueMS,
			id:    LiveNoteScreenCueID,
			voice: voice,
			text:  liveNoteT("MeetingScreenStarted", map[string]interface{}{i18nNameKey: name}),
		})
		if s.EndedAt <= s.StartedAt {
			continue
		}
		end := liveNoteOffset(s.EndedAt, base)
		out = append(out, vttCue{
			start: end,
			end:   end + liveNoteFallbackCueMS,
			id:    LiveNoteScreenCueID,
			voice: voice,
			text: liveNoteT("MeetingScreenStopped", map[string]interface{}{
				i18nNameKey: name,
				i18nStayKey: liveNoteDurationSuffix(s.EndedAt - s.StartedAt),
			}),
		})
	}
	return out
}

// liveNoteScreenName is the label a reader sees: the participant's nickname, the
// host's role label, or the raw id when the client never supplied a name.
func liveNoteScreenName(s LiveNoteScreenShare) string {
	if name := strings.TrimSpace(s.SharerName); name != "" {
		return name
	}
	if s.Sharer == liveNotesHostID {
		return liveNoteT("MeetingScreenHost")
	}
	return s.Sharer
}

// liveNoteDurationSuffix renders "（停留 8 分 25 秒）", or "" when the interval is
// too short to say anything useful.
func liveNoteDurationSuffix(ms int64) string {
	value := liveNoteDurationValue(ms)
	if value == "" {
		return ""
	}
	return liveNoteT("MeetingFilesStayWrap", map[string]interface{}{"Text": value})
}

// liveNoteDurationValue renders just the duration ("8 分 25 秒"), for tables
// whose column already says what the number means.
func liveNoteDurationValue(ms int64) string {
	if ms < 1000 {
		return ""
	}
	sec := (ms + 500) / 1000
	switch {
	case sec < 60:
		return liveNoteT("MeetingFilesStaySeconds", map[string]interface{}{"N": sec})
	case sec < 3600:
		if sec%60 == 0 {
			return liveNoteT("MeetingFilesStayMinutes", map[string]interface{}{"N": sec / 60})
		}
		return liveNoteT("MeetingFilesStayMinSec", map[string]interface{}{"Min": sec / 60, "Sec": sec % 60})
	default:
		return liveNoteT("MeetingFilesStayHours", map[string]interface{}{"Hours": sec / 3600, "Min": (sec % 3600) / 60})
	}
}

// liveNoteTranscriptLine matches the `[mm:ss]` prefix every proofread line
// carries. Minutes may exceed 59 (a three-hour meeting), so the left side is
// not bounded to two digits.
var liveNoteTranscriptLine = regexp.MustCompile(`^\[(\d+):(\d{2})\]`)

// liveNoteEvent is one non-speech line waiting to be merged into the proofread
// transcript: its offset from the timeline origin plus the rendered line.
type liveNoteEvent struct {
	at   int64
	line string
}

// MergeLiveNoteFileEvents inserts file open/close lines into a proofread
// transcript, in timeline order.
//
// The events cannot ride through the proofreading model call: it is told to
// rewrite speech, and a line that is not speech gets rewritten into one. They
// are therefore merged afterwards, keyed on the `[mm:ss]` prefix the revise
// prompt forbids the model to touch.
//
// When no line parses at all (the model changed the format anyway), the events
// are appended as a trailing section rather than dropped — a transcript that
// silently loses them is worse than one with a duplicate-looking list at the
// end.
func MergeLiveNoteFileEvents(revised string, files []LiveNoteFile, screens []LiveNoteScreenShare, base int64) string {
	if (len(files) == 0 && len(screens) == 0) || base <= 0 || strings.TrimSpace(revised) == "" {
		return revised
	}
	events := liveNoteFileEvents(files, base)
	events = append(events, liveNoteScreenEvents(screens, base)...)
	if len(events) == 0 {
		return revised
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at < events[j].at })
	return insertLiveNoteEvents(revised, events)
}

// liveNoteFileEvents renders file open/close events as transcript lines.
func liveNoteFileEvents(files []LiveNoteFile, base int64) []liveNoteEvent {
	out := make([]liveNoteEvent, 0, len(files)*2)
	for _, f := range files {
		label := liveNoteFileLabel(f)
		if f.OpenedAt <= 0 || label == "" {
			continue
		}
		at := liveNoteOffset(f.OpenedAt, base)
		out = append(out, liveNoteEvent{at: at, line: liveNoteFileLine(at,
			liveNoteT("MeetingFilesOpened", map[string]interface{}{i18nPathKey: label}))})
		if f.ClosedAt > f.OpenedAt {
			closed := liveNoteOffset(f.ClosedAt, base)
			out = append(out, liveNoteEvent{at: closed, line: liveNoteFileLine(closed,
				liveNoteT("MeetingFilesClosed", map[string]interface{}{
					i18nPathKey: label,
					i18nStayKey: liveNoteDurationSuffix(f.ClosedAt - f.OpenedAt),
				}))})
		}
	}
	return out
}

// liveNoteScreenEvents renders screen-sharing open/close events as transcript
// lines.
func liveNoteScreenEvents(screens []LiveNoteScreenShare, base int64) []liveNoteEvent {
	out := make([]liveNoteEvent, 0, len(screens)*2)
	for _, s := range screens {
		if s.StartedAt <= 0 {
			continue
		}
		name := liveNoteScreenName(s)
		at := liveNoteOffset(s.StartedAt, base)
		out = append(out, liveNoteEvent{at: at, line: liveNoteScreenLine(at,
			liveNoteT("MeetingScreenStarted", map[string]interface{}{i18nNameKey: name}))})
		if s.EndedAt > s.StartedAt {
			end := liveNoteOffset(s.EndedAt, base)
			out = append(out, liveNoteEvent{at: end, line: liveNoteScreenLine(end,
				liveNoteT("MeetingScreenStopped", map[string]interface{}{
					i18nNameKey: name,
					i18nStayKey: liveNoteDurationSuffix(s.EndedAt - s.StartedAt),
				}))})
		}
	}
	return out
}

// insertLiveNoteEvents splices the events into the proofread transcript, using
// each line's `[mm:ss]` prefix as the insertion point.
func insertLiveNoteEvents(revised string, events []liveNoteEvent) string {
	lines := strings.Split(revised, "\n")
	out := make([]string, 0, len(lines)+len(events))
	next := 0
	parsed := 0
	for _, line := range lines {
		if m := liveNoteTranscriptLine.FindStringSubmatch(line); m != nil {
			parsed++
			offset := liveNoteCueOffset(m)
			for next < len(events) && events[next].at <= offset {
				out = append(out, events[next].line)
				next++
			}
		}
		out = append(out, line)
	}
	if parsed == 0 {
		return appendLiveNoteEventSection(revised, events)
	}
	for next < len(events) {
		out = append(out, events[next].line)
		next++
	}
	return strings.Join(out, "\n")
}

// appendLiveNoteEventSection is the fallback for a transcript whose lines lost
// the `[mm:ss]` prefix: the events still have to be visible somewhere.
func appendLiveNoteEventSection(revised string, events []liveNoteEvent) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(revised, "\n"))
	b.WriteString("\n\n## " + liveNoteT("MeetingTranscriptFilesSection") + "\n\n")
	for _, e := range events {
		b.WriteString("- ")
		b.WriteString(e.line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// liveNoteCueOffset reads the `[mm:ss]` prefix captured by
// liveNoteTranscriptLine as an offset in milliseconds.
func liveNoteCueOffset(match []string) int64 {
	minutes, _ := strconv.Atoi(match[1])
	seconds, _ := strconv.Atoi(match[2])
	return int64(minutes)*60_000 + int64(seconds)*1000
}

// liveNoteFileLine renders one event as a transcript line, matching the shape
// the proofread pass produced: `[mm:ss] 说话人：内容` (the separator follows the
// UI language, like every other transcript line).
func liveNoteFileLine(offsetMS int64, text string) string {
	if offsetMS < 0 {
		offsetMS = 0
	}
	return fmt.Sprintf("[%02d:%02d] %s%s%s", offsetMS/60_000, (offsetMS/1000)%60,
		liveNoteT("MeetingFilesSpeaker"), liveNoteT("MeetingTranscriptSeparator"), text)
}

// liveNoteScreenLine renders a screen-sharing event in the same shape.
func liveNoteScreenLine(offsetMS int64, text string) string {
	if offsetMS < 0 {
		offsetMS = 0
	}
	return fmt.Sprintf("[%02d:%02d] %s%s%s", offsetMS/60_000, (offsetMS/1000)%60,
		liveNoteT("MeetingScreenVoice"), liveNoteT("MeetingTranscriptSeparator"), text)
}

// vttCueID sanitizes a channel for use as a cue identifier. A cue id may not
// contain "-->" or a line break; channels are validated on upload, so this only
// guards a hand-edited database. "" means "write this cue without an id".
func vttCueID(channel string) string {
	id := strings.TrimSpace(channel)
	if id == "" || strings.Contains(id, "-->") || strings.ContainsAny(id, "\r\n") {
		return ""
	}
	return id
}

// vttVoiceName sanitizes a speaker label for a voice span. The label comes from
// the client (a viewer types it), and an unescaped ">" would end the span early
// — turning the rest of the line into markup.
func vttVoiceName(speaker string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', '\r', '\n':
			return -1
		}
		return r
	}, speaker)
}

// vttEscape escapes the three characters WebVTT treats as markup. Without it a
// line containing "&" or "<" is parsed as an entity or a tag and the text
// silently loses characters.
func vttEscape(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}

// vttTimestamp formats milliseconds as WebVTT's HH:MM:SS.mmm.
func vttTimestamp(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d.%03d",
		ms/3600000, (ms%3600000)/60000, (ms%60000)/1000, ms%1000)
}

// ── Minutes generation ──────────────────────────────────────────────────────

// LiveNotesSpeaker is who the transcript's first person refers to.
//
// New transcripts label the publisher with their own 称谓, so nothing has to be
// resolved: the line already reads `[00:08] 泷泽：…`. Older ones — recorded
// while the label was the localized pronoun 「我」 / 「Me」 — carry no name at
// all, and a line like "我负责发版" is unreadable to anyone who was not in the
// meeting. Minutes are generated from those transcripts too (regeneration is a
// supported action), so the mapping stays: it simply turns itself off when the
// label already is the name.
type LiveNotesSpeaker struct {
	// Label is the speaker label the transcript uses for the user's own
	// channel. Empty when the user never spoke — there is nothing to resolve.
	Label string
	// Name is the user's 称谓 (user.nickname): the name the room shows and the
	// name both model stages should use for them.
	Name string
}

// resolved reports whether a clarification is needed at all: a label, a real
// name, and a label that is not already that name.
//
// The default nickname is skipped for the same reason BuildUserIdentitySection
// skips it: "User" is a placeholder, and telling the model that "我" is called
// "User" is worse than leaving the pronoun alone.
func (s LiveNotesSpeaker) resolved() (label, name string, ok bool) {
	label = strings.TrimSpace(s.Label)
	name = strings.TrimSpace(s.Name)
	if label == "" || name == "" || name == model.DefaultUserNickname || label == name {
		return "", "", false
	}
	return label, name, true
}

// note renders the clarification appended to a summarisation prompt, or ""
// when there is nothing worth saying.
func (s LiveNotesSpeaker) note() string {
	label, name, ok := s.resolved()
	if !ok {
		return ""
	}
	return fmt.Sprintf(
		"补充说明：转写里说话人标注为「%s」的那一路，是正在使用本系统的用户本人（本次会议的发起人），称谓是「%s」。"+
			"纪要里指代这个人时写「%s」，不要照抄转写里的说话人标注。",
		label, name, name,
	)
}

// withPrompt appends the clarification to a system prompt. A no-op when there
// is nothing to inject, so callers can wire it unconditionally.
func (s LiveNotesSpeaker) withPrompt(prompt string) string {
	if note := s.note(); note != "" {
		return prompt + "\n\n" + note
	}
	return prompt
}

// reviseNote renders the initiator clarification for the PROOFREADER.
//
// Same two facts as note(), a different consequence. The minutes may refer to
// the initiator by name; the proofreader may only substitute spellings, so what
// it needs is exactly that — the correct spelling of a participant whose label
// is a pronoun and therefore cannot be checked against anything else in the
// transcript. That is the legacy shape only (see LiveNotesSpeaker); with the
// 称谓 as the label, rule 3 already covers it and nothing is injected.
//
// The label is optional. When the initiator never spoke there is no 「我」 line
// to bind the name to, but a viewer naming them in the body is still the case
// this exists for, so the name alone is worth injecting.
func (s LiveNotesSpeaker) reviseNote() string {
	name := strings.TrimSpace(s.Name)
	if name == "" || name == model.DefaultUserNickname {
		return ""
	}
	label := strings.TrimSpace(s.Label)
	if label == "" {
		return fmt.Sprintf(
			"补充说明：本次会议的发起人，称谓是「%s」。正文里明显指这个人的近音写法改成「%s」；"+
				"拿不准是不是指这个人时保持原样。", name, name,
		)
	}
	if label == name {
		// The label already is the name: rule 3 has everything it needs.
		return ""
	}
	return fmt.Sprintf(
		"补充说明（优先于第 4 条）：转写里说话人标注为「%s」的那一路，是本次会议的发起人本人，称谓是「%s」。"+
			"正文里明显指这个人的近音写法改成「%s」；「%s」这个标注本身不要动，也不要把别的说话人当成这个人。",
		label, name, name, label,
	)
}

// LiveNotesSelfLabel returns the speaker label the transcript uses for the
// user's own voice.
//
// The `mic` channel IS the publisher by construction — the client records its
// own microphone there (web/src/composables/useLiveNotes.ts) — but the label
// itself comes from the client, so it has to be read from the data rather than
// assumed. Empty when the user never spoke.
func LiveNotesSelfLabel(segments []LiveNoteSegment) string {
	for _, s := range segments {
		if s.Channel != "mic" {
			continue
		}
		// Same fallback BuildLiveNotesTranscript uses, so the label quoted in
		// the prompt is the one the model actually sees.
		if label := strings.TrimSpace(s.Speaker); label != "" {
			return label
		}
		return s.Channel
	}
	return ""
}

// liveNotesMinutesChunkRunes is the per-chunk input budget for the minutes
// map-reduce. Much larger than the proofreading chunk because the output is a
// short extraction, not a rewrite: a long-context model can read a whole
// meeting, and fewer chunks means fewer cross-chunk decisions lost in the
// reduction. Sized so prompt + extraction still fit a 32k window (the smallest
// summary model worth configuring).
const liveNotesMinutesChunkRunes = 24000

const liveNotesMinutesPrompt = `你是一名专业的会议记录员。根据下面这场会议的实时转写（每行是「时间 说话人：内容」，已按时间重排；同一路声音里也可能有多人交替发言，一律以每行的说话人标签为准），生成结构化的会议纪要。

要求：
1. 输出 Markdown，使用以下章节（没有内容的章节可省略）：
   ## 概要
   ## 议题与讨论
   ## 结论与决定
   ## 待办事项
   ## 遗留事项
2. 待办事项用列表表示，能判断负责人时标注说话人，提到时间点时一并保留。
3. 只依据转写内容，不要编造。信息不完整或听不清的地方标注「（待确认）」。
4. 忽略寒暄、口头语和明显的转写错误；不要输出转写原文。
5. 使用与转写内容一致的语言输出（中英混合时以中文为主）。
6. 转写之外的材料各有用途：「会议信息」说明时间、时长与在场的人，可据此写「本次会议约 47 分钟」这类表述；
   「会议聊天记录」与语音讨论同属会议内容；「打开的文件」「屏幕共享」「共享画面」只说明当时在看什么，
   不要仅凭它们推断结论、决定或待办。
7. 「上一次会议的待办事项」是背景，不是本次会议的内容：只有本次讨论明确提到某一项时，才写进「遗留事项」
   并说明进展；本次会议与它们无关时省略该章节，也不要把它们当成这次会议的待办。`

// liveNotesChunkPrompt extracts one chunk of a long meeting.
//
// The `[mm:ss] 说话人：` prefix is mandatory in the output: the final pass never
// sees the transcript, so a bullet list without an anchor loses exactly what the
// action items need — who said it and when. Asking for the prefix keeps the two
// facts alive across the reduction.
const liveNotesChunkPrompt = `你是一名会议记录员。下面是一场长会议转写的其中一段（每行是「时间 说话人：内容」）。请只提取这一段里的关键信息。

输出要求：每条要点单独一行，必须以原行的时间与说话人为前缀，格式为「[mm:ss] 说话人：要点」。
- 讨论的议题
- 达成的结论或决定
- 待办事项（尽量标注负责人；负责人就是说话人时也写明）
不要展开叙述，不要输出转写原文的正文，使用与原文一致的语言。`

// LiveNoteFilesPromptSection renders the meeting's file timeline for the
// summariser, or "" when nothing was opened.
//
// It is a separate block rather than extra transcript lines on purpose: the
// transcript is map-reduced chunk by chunk, so a section appended to its tail
// would be summarized away in exactly the long meetings where it matters most.
// The final pass receives it after the reduction.
//
// The wording is defensive. A model handed a list of files will happily write
// "the team decided to refactor main.go" from the mere fact that main.go was
// open; the block says explicitly that these are things people LOOKED AT.
func LiveNoteFilesPromptSection(files []LiveNoteFile, startedAt time.Time, projectPath string) string {
	return LiveNoteFilesPromptSectionWithExcerpts(files, startedAt, projectPath, true)
}

// LiveNoteFilesPromptSectionWithExcerpts is LiveNoteFilesPromptSection with the
// file CONTENT excerpt switched off. The timeline stays either way: the setting
// is about sending project file contents to the model, not about hiding which
// documents were on screen.
func LiveNoteFilesPromptSectionWithExcerpts(files []LiveNoteFile, startedAt time.Time, projectPath string, includeExcerpts bool) string {
	if len(files) == 0 || startedAt.IsZero() {
		return ""
	}
	ordered := make([]LiveNoteFile, len(files))
	copy(ordered, files)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OpenedAt < ordered[j].OpenedAt })

	var rows []string
	for _, f := range ordered {
		label := liveNoteFileLabel(f)
		if f.OpenedAt <= 0 || label == "" {
			continue
		}
		span := liveNoteClock(f.OpenedAt)
		if f.ClosedAt > f.OpenedAt {
			span += "–" + liveNoteClock(f.ClosedAt)
		} else {
			span += " 起（至会议结束）"
		}
		row := fmt.Sprintf("- %s %s", span, label)
		if owner := strings.TrimSpace(f.OwnerName); owner != "" && f.Owner != liveNotesHostID {
			row += "（" + owner + "）"
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 会议期间打开的文件\n")
	b.WriteString("以下是参会人在会议期间查看过的文件（按打开时间排序）。它们可以帮助理解讨论的对象，")
	b.WriteString("但**不是发言内容**：不要仅凭某个文件被打开就推断结论、决定或待办。\n\n")
	for _, row := range rows {
		b.WriteString(row)
		b.WriteString("\n")
	}
	// The timeline alone only says which documents were on screen. The excerpt
	// is what lets the summary use their vocabulary and subject — which is the
	// whole reason the meeting recorded the files.
	if includeExcerpts {
		if excerpts := LiveNoteFileExcerpts(projectPath, files); excerpts != "" {
			b.WriteString("\n")
			b.WriteString(excerpts)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// liveNoteClock renders an epoch-ms instant as wall-clock HH:MM.
func liveNoteClock(ms int64) string {
	return time.UnixMilli(ms).Format("15:04")
}

// LiveNoteScreenPromptSection renders the screen-sharing timeline for the
// summariser, or "" when nobody shared.
//
// It matters for attribution: "the screen showed the deploy log" is only
// meaningful once the reader knows whose screen it was, and a meeting where the
// host handed the screen to a participant has two different pictures.
func LiveNoteScreenPromptSection(screens []LiveNoteScreenShare, startedAt time.Time) string {
	if len(screens) == 0 || startedAt.IsZero() {
		return ""
	}
	var rows []string
	for _, s := range screens {
		if s.StartedAt <= 0 {
			continue
		}
		span := liveNoteClock(s.StartedAt)
		if s.EndedAt > s.StartedAt {
			span += "–" + liveNoteClock(s.EndedAt)
		} else {
			span += " " + liveNoteT("MeetingFilesSince")
		}
		rows = append(rows, fmt.Sprintf("- %s %s 共享屏幕", span, liveNoteScreenName(s)))
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 屏幕共享\n")
	b.WriteString("以下是会议期间共享屏幕的人（按时间排序）。")
	b.WriteString("说话人提到的「这个」「这里」指的是当时共享的那个画面。\n\n")
	for _, row := range rows {
		b.WriteString(row)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// LiveNoteFileExcerpts returns a prompt block with the contents of the files the
// meeting opened, or "" when nothing readable was found.
//
// Only the first few files are read, and each is truncated: the point is to give
// the summariser the vocabulary and subject of the documents, not to paste them.
// Reading is bounded by a per-file cap AND a total budget, because the files
// come from the client and a single 5 MB log would otherwise consume the whole
// prompt.
func LiveNoteFileExcerpts(projectPath string, files []LiveNoteFile) string {
	if len(files) == 0 {
		return ""
	}
	ordered := make([]LiveNoteFile, len(files))
	copy(ordered, files)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OpenedAt < ordered[j].OpenedAt })

	var b strings.Builder
	budget := liveNoteFileExcerptBudget
	count := 0
	for _, f := range ordered {
		if count >= liveNoteFileExcerptMaxFiles || budget <= 0 {
			break
		}
		label := strings.TrimSpace(f.Path)
		if label == "" {
			label = f.Name
		}
		source := resolveLiveNoteFilePath(projectPath, f.Path)
		if source == "" || label == "" {
			continue
		}
		info, err := os.Stat(source)
		if err != nil || !info.Mode().IsRegular() || info.Size() > LiveNoteFileCopyMaxBytes {
			continue
		}
		data, err := os.ReadFile(source)
		if err != nil {
			continue
		}
		text := liveNoteExcerptText(data, liveNoteFileExcerptMaxRunes)
		if text == "" {
			continue
		}
		if len([]rune(text)) > budget {
			text = string([]rune(text)[:budget])
		}
		budget -= len([]rune(text))
		count++
		fmt.Fprintf(&b, "\n### %s\n```\n%s\n```\n", label, text)
	}
	if count == 0 {
		return ""
	}
	return "### 文件内容摘录（已截断）\n" + b.String()
}

// liveNoteExcerptText returns a text excerpt, or "" for binary content.
//
// A NUL byte in the first kilobyte is the cheap, reliable "this is not text"
// test: images and archives always have one, and no source file does.
func liveNoteExcerptText(data []byte, maxRunes int) string {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	if strings.IndexByte(string(head), 0) >= 0 {
		return ""
	}
	if !utf8.Valid(data) {
		// Invalid UTF-8 is either binary or a legacy encoding we cannot render
		// faithfully; both are worse than no excerpt.
		return ""
	}
	runes := []rune(string(data))
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return strings.TrimSpace(string(runes))
}

// LiveNotesMinutesOptions tunes one minutes pass.
//
// It intentionally does not carry the model or endpoint: those come from the
// shared AISummaryConfig so every feature rotates its key in one place.
type LiveNotesMinutesOptions struct {
	// Language is "zh" / "en" to force the output language, or "" to follow
	// the transcript. It applies to the FINAL pass only; chunk extraction
	// stays in the transcript's language so the intermediate notes do not
	// drift from the source.
	Language string
	// MaxTokens caps the final minutes. <= 0 uses the default. The cap is a
	// limit, not a target: the model may stop earlier.
	MaxTokens int
}

// liveNotesLanguageDirective is appended AFTER the base prompt (and after the
// speaker clarification) so it overrides rule 5's "use the transcript's
// language" without rewriting the prompt for every locale.
func liveNotesLanguageDirective(lang string) string {
	switch lang {
	case "zh":
		return "输出语言：简体中文。即使转写主要是英文，也用简体中文写纪要。"
	case "en":
		return "Output language: English. Write the minutes in English even if the transcript is mostly Chinese."
	default:
		return ""
	}
}

// GenerateLiveNotesMinutes turns a transcript into Markdown minutes.
//
// It takes the transcript as text (not segments) because the caller decides
// which variant to summarize — normally the proofread one. `self` is the one
// thing about the transcript that is NOT in its text: who the speaker labeled
// "我" actually is.
//
// `contextSection` is everything else the meeting knows about itself — when it
// ran, who was in it, what was on screen, what was typed in chat, what the last
// meeting left open (see LiveNoteMinutesContext). It is appended AFTER the
// transcript (and after the reduction, when there is one) on purpose: a block
// placed in the tail of a chunk would be summarized away exactly in the long
// meetings where it matters most.
//
// Long meetings are map-reduced: each chunk is condensed to bullet points, then
// the bullet points are merged into the final minutes. Sending the whole
// transcript in one request would either overflow the model window or be
// silently truncated by the provider.
func GenerateLiveNotesMinutes(
	ctx context.Context, title, transcript, contextSection string, self LiveNotesSpeaker,
	progress ...func(done, total int, phase string),
) (string, error) {
	return GenerateLiveNotesMinutesWithOptions(
		ctx, title, transcript, contextSection, self, LiveNotesMinutesOptions{}, progress...,
	)
}

// GenerateLiveNotesMinutesWithOptions is GenerateLiveNotesMinutes plus the two
// user-facing knobs: output language and output cap.
func GenerateLiveNotesMinutesWithOptions(
	ctx context.Context, title, transcript, contextSection string, self LiveNotesSpeaker,
	opts LiveNotesMinutesOptions,
	progress ...func(done, total int, phase string),
) (string, error) {
	if strings.TrimSpace(transcript) == "" {
		return "", ErrLiveNotesEmpty
	}
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}
	// The model the budget is derived from — the same one the summarizer will
	// actually call (NewAISummarizer builds it from this config).
	summaryModelID := model.ConfigInstance.AISummary.Model
	report := func(done, total int, phase string) {
		if len(progress) > 0 && progress[0] != nil {
			progress[0](done, total, phase)
		}
	}

	chunks := splitRunes(transcript, liveNotesMinutesChunkRunes)
	contextBlock := transcript
	if len(chunks) > 1 {
		// One extraction per chunk, each given the model's own budget rather
		// than a fixed number: a cap that also has to cover the model's
		// reasoning starves long chunks and fails the whole run with
		// "output truncated". The prompt — not the cap — keeps the extraction
		// short (roughly 5:1 against the chunk).
		chunkBudget := summarize.OutputBudgetForModel(summaryModelID, 0)
		partials := make([]string, 0, len(chunks))
		for i, chunk := range chunks {
			out, err := summarize.AskAboutContext(
				ctx, summarizer, self.withPrompt(liveNotesChunkPrompt),
				fmt.Sprintf("第 %d/%d 段：\n%s", i+1, len(chunks), chunk),
				"请提取这一段的关键信息。", chunkBudget,
			)
			if err != nil {
				return "", fmt.Errorf("live notes: chunk %d: %w", i+1, err)
			}
			partials = append(partials, out)
			report(i+1, len(chunks), "chunk")
		}
		contextBlock = strings.Join(partials, "\n\n")
	}
	// After the reduction, never inside it: a file list in the tail of one chunk
	// would be summarized away exactly when the meeting is long enough to be
	// chunked at all.
	if strings.TrimSpace(contextSection) != "" {
		contextBlock += "\n\n" + contextSection
	}

	report(0, 1, "final")
	question := liveNotesMinutesQuestion(title)
	// opts.MaxTokens <= 0 means "auto": follow the model's published output
	// limit. A manual value still cannot exceed it — the provider rejects that
	// outright rather than returning a longer document.
	maxTokens := summarize.OutputBudgetForModel(summaryModelID, opts.MaxTokens)
	systemPrompt := self.withPrompt(liveNotesMinutesPrompt)
	if directive := liveNotesLanguageDirective(opts.Language); directive != "" {
		systemPrompt += "\n\n" + directive
	}
	// Lossless rather than AskAboutContext: a user-configurable cap that
	// silently returns half a document is worse than an explicit failure. The
	// transcript and audio are intact, so raising the limit and regenerating
	// is a complete recovery.
	minutes, err := summarize.AskAboutContextLossless(
		ctx, summarizer, systemPrompt, contextBlock, question, maxTokens,
	)
	if err != nil {
		if errors.Is(err, summarize.ErrOutputTruncated) {
			return "", fmt.Errorf("%w (max_tokens=%d): %w", ErrLiveNotesBudget, maxTokens, err)
		}
		return "", fmt.Errorf("live notes: minutes: %w", err)
	}
	return strings.TrimSpace(minutes), nil
}

// liveNotesMinutesQuestion frames the summary request.
//
// A placeholder title ("11-01 17:35") is not a name: handing it to the
// summarizer as "会议标题：" only invites a meeting about timestamps.
func liveNotesMinutesQuestion(title string) string {
	if title == "" || IsPlaceholderMeetingTitle(title) {
		return "请根据以上内容生成会议纪要。"
	}
	return "会议标题：" + title + "\n请根据以上内容生成会议纪要。"
}

// GenerateLiveNotesTitle names a meeting from its minutes.
//
// This is the second half of "the record is not called clawbench": the meeting
// is created before anyone knows what it was about, so the title starts as a
// date placeholder and is replaced here, once there is something to name it
// after. The minutes (not the transcript) are the input — they already hold the
// summary, and they are a fraction of the size.
func GenerateLiveNotesTitle(ctx context.Context, minutes string) (string, error) {
	summarizer := summarize.NewAISummarizer(model.ConfigInstance.AISummary)
	if summarizer == nil {
		return "", ErrSummaryModelNotConfigured
	}
	title, err := summarize.GenerateMeetingTitle(ctx, summarizer, minutes)
	if err != nil {
		return "", fmt.Errorf("live notes: title: %w", err)
	}
	return title, nil
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
	// Transcript is `<dir>/transcript.vtt` — the raw ASR output, as WebVTT.
	Transcript string
	// Revised is `<dir>/transcript-revised.md` — proofread against the project's
	// own vocabulary. This is what the minutes are generated from.
	Revised string
	// Audio is `<dir>/audio/` — one WebM file per transcribed segment.
	Audio string
	// Shots is `<dir>/shots/` — the key frames captured during the meeting.
	// Created on the first frame, not up front: an empty folder would show up
	// in the resource list as a directory with nothing in it.
	Shots string
}

const (
	// liveNoteTranscriptVTT is the raw transcript artifact.
	liveNoteTranscriptVTT = "transcript.vtt"
)

// LiveNotesPathsIn returns the artifact paths for a meeting folder.
func LiveNotesPathsIn(dir string) LiveNotesPaths { return liveNotesPathsFor(dir) }

func liveNotesPathsFor(dir string) LiveNotesPaths {
	return LiveNotesPaths{
		Dir:        dir,
		Minutes:    filepath.Join(dir, "minutes.md"),
		Transcript: filepath.Join(dir, liveNoteTranscriptVTT),
		Revised:    filepath.Join(dir, "transcript-revised.md"),
		Audio:      filepath.Join(dir, "audio"),
		Shots:      filepath.Join(dir, LiveNoteShotDirName),
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
func WriteLiveNotesMinutesFile(paths LiveNotesPaths, title string, startedAt time.Time, minutes string, audioFiles int, hasMeetingAudio bool) (string, error) {
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
	sep := liveNoteT("MeetingTranscriptSeparator")
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		fmt.Fprintf(&b, "# %s · %s\n\n", t, heading)
	} else {
		fmt.Fprintf(&b, "# %s\n\n",
			liveNoteT("MeetingMinutesTitle", map[string]interface{}{"Time": heading}))
	}
	b.WriteString(strings.TrimSpace(minutes))
	b.WriteString("\n\n---\n\n## " + liveNoteT("MeetingMinutesArtifacts") + "\n\n")
	fmt.Fprintf(&b, "- %s%s[transcript.vtt](transcript.vtt)\n", liveNoteT("MeetingMinutesTranscript"), sep)
	fmt.Fprintf(&b, "- %s%s[transcript-revised.md](transcript-revised.md)\n",
		liveNoteT("MeetingTranscriptRevised"), sep)
	if hasMeetingAudio {
		// The merged track is what somebody actually listens to; the
		// per-channel files are the source it was built from.
		fmt.Fprintf(&b, "- %s%s[%s](audio/%s)\n", liveNoteT("MeetingMinutesRecording"), sep,
			LiveNoteMeetingAudioFile, LiveNoteMeetingAudioFile)
	}
	if audioFiles > 0 {
		fmt.Fprintf(&b, "- %s\n",
			liveNoteT("MeetingMinutesChannelRecordings", map[string]interface{}{"N": audioFiles}))
	}

	if err := os.WriteFile(paths.Minutes, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("live notes: write minutes file: %w", err)
	}
	return paths.Minutes, nil
}

// WriteLiveNotesRawTranscript writes transcript.vtt: exactly what the ASR heard.
//
// No Markdown heading: the file is a subtitle track, and a `# 转写 · …` line
// would make every player show it as the first cue.
func WriteLiveNotesRawTranscript(paths LiveNotesPaths, segments []LiveNoteSegment, files []LiveNoteFile, screens []LiveNoteScreenShare) (string, error) {
	if strings.TrimSpace(paths.Dir) == "" {
		return "", errors.New("live notes: no project path")
	}
	body := BuildLiveNotesVTT(segments, files, screens)
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
// differs from transcript.vtt, and "the timestamps were not touched" is the one
// thing that makes it comparable.
func WriteLiveNotesRevisedTranscript(paths LiveNotesPaths, title string, startedAt time.Time, body string) (string, error) {
	return writeLiveNotesTranscript(paths, paths.Revised, liveNoteT("MeetingTranscriptRevised"),
		liveNoteT("MeetingTranscriptRevisedNote"),
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
// was read — the UI opens the artifact in the file viewer and needs it.
//
// One file per kind, no fallback list: the format is whatever this build
// writes, and a folder holding a transcript this build cannot read is reported
// as missing rather than silently served in a stale format.
func ReadLiveNoteArtifactWithName(projectPath, dir, kind string) (string, string, error) {
	if !validLiveNoteDirName(dir) {
		return "", "", ErrLiveNoteSessionNotFound
	}
	var name string
	switch kind {
	case "minutes":
		name = "minutes.md"
	case "transcript":
		name = liveNoteTranscriptVTT
	case "revised":
		name = "transcript-revised.md"
	case "chat":
		name = "chat.md"
	default:
		return "", "", ErrLiveNoteArtifactKind
	}

	full := filepath.Join(liveNotesRoot(projectPath), dir, name)
	info, err := os.Stat(full)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", ErrLiveNoteSessionNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("live notes: stat artifact: %w", err)
	}
	if info.IsDir() {
		return "", "", ErrLiveNoteSessionNotFound
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
