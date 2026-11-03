package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawbench/internal/store"
)

// LiveNoteShotDirName is the folder inside a meeting directory that holds the
// key frames. Kept next to audio/ and the Markdown artifacts so deleting the
// meeting removes everything with one os.RemoveAll.
const LiveNoteShotDirName = "shots"

// LiveNoteShotMaxPerMeeting bounds one meeting's frames.
//
// A server-side ceiling, not a product rule: the client already stops at 80.
// It exists so a buggy or hostile client cannot fill the disk through an
// endpoint that is otherwise authenticated.
const LiveNoteShotMaxPerMeeting = 100

// liveNoteShotRefMaxRunes bounds the trigger's reference (a file path, a
// viewer id, a segment id). Long enough for a deep project path, short enough
// to keep the row readable — the same budget the file timeline uses.
const liveNoteShotRefMaxRunes = 1024

// Shot reasons. The client sends one of these; the transcript row and the
// detail view localize it. A closed set keeps a typo from becoming an
// unlabelled row nobody can explain later.
const (
	LiveNoteShotReasonManual = "manual"
	LiveNoteShotReasonFile   = "file"
	LiveNoteShotReasonSharer = "sharer"
	LiveNoteShotReasonSwitch = "switch"
	LiveNoteShotReasonSpeech = "speech"
	LiveNoteShotReasonChange = "change"
)

// liveNoteShotReasons is the accepted set. Order is the display priority used
// when two triggers land on the same screen.
var liveNoteShotReasons = map[string]bool{
	LiveNoteShotReasonManual: true,
	LiveNoteShotReasonFile:   true,
	LiveNoteShotReasonSharer: true,
	LiveNoteShotReasonSwitch: true,
	LiveNoteShotReasonSpeech: true,
	LiveNoteShotReasonChange: true,
}

// ValidLiveNoteShotReason reports whether a client-supplied reason is known.
func ValidLiveNoteShotReason(reason string) bool {
	return liveNoteShotReasons[reason]
}

// LiveNoteShot is one stored frame.
//
// OffsetMS is not stored: it is derived at read time from the meeting's
// transcript origin, so a shot recorded before the first utterance still lands
// at the top of the timeline instead of at "milliseconds since 1970".
type LiveNoteShot struct {
	ID       int64  `json:"id"`
	At       int64  `json:"at"`
	OffsetMS int64  `json:"offsetMs"`
	Reason   string `json:"reason"`
	Ref      string `json:"ref,omitempty"`
	// FileName is relative to the meeting directory ("shots/….webp"). The
	// client joins it onto dirPath for the thumbnail endpoint.
	FileName string `json:"fileName"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
}

// SaveLiveNoteShot writes one image into <dir>/shots and returns its name
// relative to the meeting directory.
//
// The name is generated here, never taken from the client: it sorts in capture
// order, says why it was taken, and a content hash keeps two frames in the same
// millisecond from overwriting each other.
func SaveLiveNoteShot(paths LiveNotesPaths, atMS int64, reason, ext string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("live notes: empty shot")
	}
	if atMS <= 0 {
		atMS = time.Now().UnixMilli()
	}
	if !ValidLiveNoteShotReason(reason) {
		reason = LiveNoteShotReasonManual
	}
	if ext == "" {
		ext = "webp"
	}
	dir := paths.Shots
	if dir == "" {
		dir = filepath.Join(paths.Dir, LiveNoteShotDirName)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("live notes: create shots dir: %w", err)
	}
	// Same rationale as the audio folder: binary artifacts stay out of the
	// repository, the Markdown next to them stays committable.
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(ignore, []byte("*\n"), 0o644)
	}
	sum := sha256.Sum256(data)
	name := fmt.Sprintf("%013d-%s-%s.%s", atMS, reason, hex.EncodeToString(sum[:2]), ext)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", fmt.Errorf("live notes: write shot: %w", err)
	}
	return LiveNoteShotDirName + "/" + name, nil
}

// AppendLiveNoteShot stores one row, idempotent on (token, client_shot_id).
//
// The live_notes row is ensured for the same reason the other publishers do it:
// a frame that arrives after the room was torn down still needs an owner to
// check the project against.
func AppendLiveNoteShot(ctx context.Context, token string, projectID int64, shot LiveNoteShot, clientShotID, hash string) (int64, error) {
	if token == "" {
		return 0, errors.New("live notes: empty token")
	}
	clientShotID = strings.TrimSpace(clientShotID)
	if clientShotID == "" {
		return 0, errors.New("live notes: empty client shot id")
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
	if _, execErr := tx.ExecContext(
		ctx,
		`INSERT INTO live_notes_shots
		   (token, project_id, client_shot_id, at_ms, reason, ref, file_name, width, height, bytes, hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(token, client_shot_id) DO NOTHING`,
		token, projectID, clientShotID, shot.At, shot.Reason,
		truncateRunes(strings.TrimSpace(shot.Ref), liveNoteShotRefMaxRunes),
		shot.FileName, shot.Width, shot.Height, shot.Bytes, hash,
	); execErr != nil {
		return 0, fmt.Errorf("live notes: insert shot: %w", execErr)
	}
	// LastInsertId is useless here: a conflict skips the insert and leaves the
	// connection's previous value behind. The lookup is the only correct source.
	var id int64
	if scanErr := tx.QueryRowContext(
		ctx,
		`SELECT id FROM live_notes_shots WHERE token = ? AND client_shot_id = ?`,
		token, clientShotID,
	).Scan(&id); scanErr != nil {
		return 0, fmt.Errorf("live notes: shot id: %w", scanErr)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return 0, fmt.Errorf("live notes: commit shot: %w", commitErr)
	}
	return id, nil
}

// ListLiveNoteShots returns a meeting's frames in capture order.
func ListLiveNoteShots(token string) ([]LiveNoteShot, error) {
	rows, err := store.ReadDB().Query(
		`SELECT id, at_ms, reason, ref, file_name, width, height, bytes
		   FROM live_notes_shots WHERE token = ? ORDER BY at_ms, id`, token,
	)
	if err != nil {
		return nil, fmt.Errorf("live notes: list shots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]LiveNoteShot, 0, 8)
	for rows.Next() {
		var s LiveNoteShot
		if scanErr := rows.Scan(&s.ID, &s.At, &s.Reason, &s.Ref, &s.FileName,
			&s.Width, &s.Height, &s.Bytes); scanErr != nil {
			return nil, fmt.Errorf("live notes: scan shot: %w", scanErr)
		}
		out = append(out, s)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("live notes: iterate shots: %w", rowsErr)
	}
	return out, nil
}

// CountLiveNoteShots reports how many frames a meeting has stored.
func CountLiveNoteShots(token string) (int, error) {
	var n int
	if err := store.ReadDB().QueryRow(
		`SELECT COUNT(*) FROM live_notes_shots WHERE token = ?`, token,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("live notes: count shots: %w", err)
	}
	return n, nil
}

// DeleteLiveNoteShot removes one row and reports its file name so the caller
// can delete the file. An unknown id (or one from another project) is
// ErrMeetingNotFound, the same answer the record endpoints give.
func DeleteLiveNoteShot(token string, projectID, id int64) (string, error) {
	var fileName string
	err := store.ReadDB().QueryRow(
		`SELECT file_name FROM live_notes_shots WHERE id = ? AND token = ? AND project_id = ?`,
		id, token, projectID,
	).Scan(&fileName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrMeetingNotFound
	}
	if err != nil {
		return "", fmt.Errorf("live notes: lookup shot: %w", err)
	}
	if _, execErr := store.WriteExec(
		`DELETE FROM live_notes_shots WHERE id = ? AND token = ? AND project_id = ?`,
		id, token, projectID,
	); execErr != nil {
		return "", fmt.Errorf("live notes: delete shot row: %w", execErr)
	}
	return fileName, nil
}

// DeleteLiveNoteShotFile removes the image behind a stored row.
//
// The name came from our own database, but a corrupted row must not be able to
// point outside the meeting's shots folder — the same defense
// DeleteMeetingRecord applies to the folder name.
func DeleteLiveNoteShotFile(paths LiveNotesPaths, fileName string) error {
	name := filepath.Base(filepath.Clean(fileName))
	if name == "" || name == "." || name == string(os.PathSeparator) {
		return errors.New("live notes: bad shot name")
	}
	dir := paths.Shots
	if dir == "" {
		dir = filepath.Join(paths.Dir, LiveNoteShotDirName)
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("live notes: remove shot: %w", err)
	}
	return nil
}

// liveNoteShotCountIn counts the image files in a meeting's shots folder.
// Derived like the other artifact flags: the folder is the truth, so a shot
// deleted from disk stops being advertised.
func liveNoteShotCountIn(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		n++
	}
	return n
}
