package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"clawbench/internal/model"
)

// Queued-message storage.
//
// A queued user message lives in its own table (queued_messages) until it is
// dequeued or injected. Only then is it materialized into chat_history, so the
// user row gets its DB id immediately before the assistant reply it produces —
// DB id order equals conversational order, and replies need no queue anchor.
//
// Claim operations are the delicate part. ClaimNextAndMaterialize (and its
// ByID / ByQueueID variants) DELETE the queue row AND INSERT the chat_history
// user row in ONE transaction under the global write mutex. That closes the
// window in which a crash between the two writes would either lose the message
// (deleted but not materialized) or duplicate it (materialized but still
// queued). At every instant the message exists in exactly one place.

// QueuedRow is one row of the queued_messages table.
type QueuedRow struct {
	ID          int64
	SessionID   string
	ProjectPath string
	Backend     string
	QueueID     string
	Content     string
	Files       []model.FileEntry
	CreatedAt   time.Time
}

// ToModel converts a QueuedRow to the wire type used by the queue API and by
// callers that build an AI request from a drained message.
func (r QueuedRow) ToModel() model.QueuedMessage {
	paths := make([]string, 0, len(r.Files))
	for _, f := range r.Files {
		if f.IsQuote() {
			continue
		}
		paths = append(paths, f.Path)
	}
	createdAt := ""
	if !r.CreatedAt.IsZero() {
		createdAt = r.CreatedAt.Format(time.RFC3339)
	}
	return model.QueuedMessage{
		ID:        r.ID,
		QueueID:   r.QueueID,
		Text:      r.Content,
		FilePaths: paths,
		Files:     r.Files,
		CreatedAt: createdAt,
	}
}

// NewQueueID mints a queue id for a message whose caller did not supply one.
//
// Exported so the enqueue paths can mint the id BEFORE inserting: the stored id
// is what queue_drain/queue_inject later carry, so a caller that also broadcasts
// queue_added must know it up front. Format mirrors newPushQueueID so ids from
// every path look alike; uniqueness comes from the timestamp plus nanoseconds.
func NewQueueID() string {
	return "q-" + time.Now().Format("20060102150405") + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// newQueueID is the internal alias used as AddQueuedMessage's empty-id fallback.
func newQueueID() string { return NewQueueID() }

// AddQueuedMessage inserts a user message into the session's queue. It does NOT
// touch chat_history: the message is materialized only when it is dequeued (or
// injected mid-turn).
//
// The session title is still updated at enqueue time (the message is what the
// user asked, so it should name the session even while it waits) via
// applyAutoTitle, mirroring the old behavior of titling on the first message.
//
// A caller that passes an empty queueID gets a generated one, but it must NOT
// rely on that: the stored id is what the later queue_drain/queue_inject events
// carry, so a caller that also broadcasts queue_added has to know it up front.
// Call NewQueueID for that (EnqueueAndMaybeStart and the chat handler do) — the
// fallback here only keeps a direct call safe from writing an empty id.
//
// Returns the queued_messages row id.
func AddQueuedMessage(projectPath, backend, sessionID, content string, files []model.FileEntry, queueID, fallbackTitle string) (int64, error) {
	if queueID == "" {
		queueID = newQueueID()
	}

	// Guard: reject messages to archived sessions, mirroring AddChatMessage.
	var isArchived int
	if err := dbRead.QueryRowContext(context.Background(), "SELECT archived FROM chat_sessions WHERE id = ?", sessionID).Scan(&isArchived); err == nil && isArchived == 1 {
		return 0, fmt.Errorf("cannot add message to archived session %s", sessionID)
	}

	var filesJSON string
	if len(files) > 0 {
		data, _ := json.Marshal(files)
		filesJSON = string(data)
	}

	res, err := WriteExec(
		"INSERT INTO queued_messages (session_id, project_path, backend, queue_id, content, files) VALUES (?, ?, ?, ?, ?, ?)",
		sessionID, projectPath, backend, queueID, content, filesJSON,
	)
	if err != nil {
		return 0, err
	}
	rowID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	// Title the session now — the queued message is still the user's request,
	// and a session whose first message is queued would otherwise stay "New
	// Session N" until the drain loop runs. Best-effort: a title failure must
	// not fail the enqueue.
	titled, err := applyAutoTitle(sessionID, content, files, fallbackTitle)
	if err != nil {
		slog.Warn("queue: failed to auto-title session at enqueue",
			slog.String("session", sessionID), slog.String("error", err.Error()))
	}
	// The queued message is not in chat_history yet, so the AI rename must be
	// handed the text explicitly; CollectSessionUserMessages appends it.
	if titled {
		ScheduleAutoRename(sessionID, ExtractPlainText(content))
	}

	return rowID, nil
}

// claimQueuedRowTx selects the next queued row for a session and deletes it,
// inside the caller's transaction. The SELECT and DELETE are guarded by the
// global write mutex (held by WriteBegin), so two concurrent claimers can never
// take the same row.
//
// orderClause / whereClause let the three public variants share this body:
//   - next:   ORDER BY id ASC LIMIT 1
//   - by id:  WHERE id = ?
//   - by qid: WHERE queue_id = ?
//
// Returns ok=false when nothing matched (queue empty, or the row was already
// claimed). A real DB error is returned as err.
func claimQueuedRowTx(tx *sql.Tx, sessionID string, where string, args ...any) (QueuedRow, bool, error) {
	query := "SELECT id, session_id, project_path, backend, queue_id, content, files, created_at FROM queued_messages WHERE session_id = ?"
	qargs := []any{sessionID}
	if where != "" {
		query += " AND " + where
		qargs = append(qargs, args...)
	}
	query += " ORDER BY id ASC LIMIT 1"

	var row QueuedRow
	var filesJSON sql.NullString
	err := tx.QueryRowContext(context.Background(), query, qargs...).Scan(
		&row.ID, &row.SessionID, &row.ProjectPath, &row.Backend, &row.QueueID, &row.Content, &filesJSON, &row.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return QueuedRow{}, false, nil
	}
	if err != nil {
		return QueuedRow{}, false, err
	}
	if filesJSON.Valid && filesJSON.String != "" {
		row.Files = unmarshalFilesJSON(filesJSON.String)
	}

	res, err := tx.ExecContext(context.Background(), "DELETE FROM queued_messages WHERE id = ?", row.ID)
	if err != nil {
		return QueuedRow{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Lost the race despite the mutex (defensive): another claimer removed it.
		return QueuedRow{}, false, nil
	}
	return row, true, nil
}

// materializeQueuedRowTx inserts the claimed queue row into chat_history as a
// normal user message, inside the caller's transaction. The returned id is the
// chat_history message id — allocated NOW, so it sits immediately before the
// assistant reply the run is about to create.
func materializeQueuedRowTx(tx *sql.Tx, row QueuedRow) (int64, error) {
	// fallbackTitle is empty on purpose: the title was already applied at
	// enqueue time (AddQueuedMessage). maybeAutoTitleSessionTx short-circuits
	// once title_source is 'auto', so passing "" here cannot blank the title.
	// The AI rename was likewise already scheduled at enqueue — its `titled`
	// result is discarded here on purpose.
	msgID, _, err := insertChatMessageTx(tx, row.ProjectPath, row.Backend, row.SessionID, "user", row.Content, row.Files, 0, "")
	return msgID, err
}

// ClaimNextAndMaterialize claims the oldest queued message for a session and
// inserts it into chat_history in a single transaction. It returns the claimed
// row and the new chat_history message id.
//
// ok=false means the queue was empty (or the row was already claimed). err is a
// real DB failure and must be treated as retryable by the drain loop, never as
// "queue empty" — that would silently lose messages.
func ClaimNextAndMaterialize(sessionID string) (QueuedRow, int64, bool, error) {
	return claimAndMaterialize(sessionID, "", nil)
}

// ClaimByIDAndMaterialize claims a specific queued_messages row by its table id.
// Used when the caller already knows which row it inserted (idle-path enqueue).
func ClaimByIDAndMaterialize(sessionID string, queueRowID int64) (QueuedRow, int64, bool, error) {
	return claimAndMaterialize(sessionID, "id = ?", []any{queueRowID})
}

// ClaimByQueueIDAndMaterialize claims a queued message by its client-generated
// queue id. Used by mid-turn injection, which is addressed by the id the UI
// holds.
func ClaimByQueueIDAndMaterialize(sessionID, queueID string) (QueuedRow, int64, bool, error) {
	return claimAndMaterialize(sessionID, "queue_id = ?", []any{queueID})
}

func claimAndMaterialize(sessionID, where string, args []any) (QueuedRow, int64, bool, error) {
	tx, err := WriteBegin()
	if err != nil {
		return QueuedRow{}, 0, false, err
	}
	defer writeMu.Unlock()
	defer func() { _ = tx.Rollback() }()

	row, ok, err := claimQueuedRowTx(tx, sessionID, where, args...)
	if err != nil || !ok {
		return QueuedRow{}, 0, false, err
	}

	msgID, err := materializeQueuedRowTx(tx, row)
	if err != nil {
		return QueuedRow{}, 0, false, err
	}

	if err := tx.Commit(); err != nil {
		return QueuedRow{}, 0, false, err
	}
	return row, msgID, true, nil
}

// RequeueMaterialized undoes a claim+materialize: it removes the chat_history
// user row and puts the message back into queued_messages, in one transaction.
//
// Used when a mid-turn insertion is declined AFTER the message was claimed and
// materialized. Without this the user's message would be visible in history but
// never answered (the injection was refused), and the drain loop would never
// pick it up (it only reads queued_messages).
func RequeueMaterialized(row QueuedRow, msgID int64) error {
	tx, err := WriteBegin()
	if err != nil {
		return err
	}
	defer writeMu.Unlock()
	defer func() { _ = tx.Rollback() }()

	// Only remove a row that is still a plain user message and not streaming:
	// if the drain loop somehow already took it, deleting it would destroy real
	// conversation content.
	res, err := tx.ExecContext(context.Background(), "DELETE FROM chat_history WHERE id = ? AND session_id = ? AND role = 'user' AND streaming = 0", msgID, row.SessionID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("requeue: chat_history row %d not found or no longer a plain user message", msgID)
	}

	var filesJSON string
	if len(row.Files) > 0 {
		data, _ := json.Marshal(row.Files)
		filesJSON = string(data)
	}
	if _, err := tx.ExecContext(context.Background(),
		"INSERT INTO queued_messages (session_id, project_path, backend, queue_id, content, files, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		row.SessionID, row.ProjectPath, row.Backend, row.QueueID, row.Content, filesJSON, row.CreatedAt,
	); err != nil {
		return err
	}

	return tx.Commit()
}

// ClearQueuedMessages deletes every queued message of a session. Used by
// session cancel/force-cancel and rewind — cancel semantics are "drop the
// queued messages", so they are truly removed and can never resurface.
//
// NOTE: this function only deletes rows — it does NOT emit queue_cancel.
// Callers are responsible for emitting the WS event so other devices remove
// their pending bubbles.
func ClearQueuedMessages(sessionID string) error {
	_, err := WriteExec("DELETE FROM queued_messages WHERE session_id = ?", sessionID)
	return err
}

// GetQueuedQueueIDs returns the non-empty queue_ids of a session's queued
// messages, oldest first. Used to emit queue_cancel with the exact ids.
func GetQueuedQueueIDs(sessionID string) ([]string, error) {
	rows, err := dbRead.QueryContext(context.Background(),
		"SELECT queue_id FROM queued_messages WHERE session_id = ? AND queue_id != '' ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetQueuedCount returns the number of queued messages for a session.
func GetQueuedCount(sessionID string) int {
	var count int
	_ = dbRead.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM queued_messages WHERE session_id = ?", sessionID).Scan(&count)
	return count
}

// GetQueuedMessages returns the queued messages of a session, oldest first.
func GetQueuedMessages(sessionID string) ([]model.QueuedMessage, error) {
	rows, err := dbRead.QueryContext(context.Background(),
		"SELECT id, session_id, project_path, backend, queue_id, content, files, created_at FROM queued_messages WHERE session_id = ? ORDER BY id ASC",
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []model.QueuedMessage{}
	for rows.Next() {
		var row QueuedRow
		var filesJSON sql.NullString
		if err := rows.Scan(&row.ID, &row.SessionID, &row.ProjectPath, &row.Backend, &row.QueueID, &row.Content, &filesJSON, &row.CreatedAt); err != nil {
			return nil, err
		}
		if filesJSON.Valid && filesJSON.String != "" {
			row.Files = unmarshalFilesJSON(filesJSON.String)
		}
		out = append(out, row.ToModel())
	}
	return out, rows.Err()
}

// CancelQueuedMessage deletes a single queued message by queue_id. The row is
// truly removed so a canceled message can never resurface.
func CancelQueuedMessage(sessionID, queueID string) error {
	_, err := WriteExec("DELETE FROM queued_messages WHERE session_id = ? AND queue_id = ?", sessionID, queueID)
	return err
}

// UpdateQueuedMessageQuoteNote rewrites the files JSON of a still-queued message,
// addressed by (session_id, queue_id). It is the queue-side counterpart of
// UpdateChatQuoteNote: a quote note edited while the message is still waiting
// has no chat_history row to update, but the drain loop re-reads queued_messages
// when it materializes the row, so the edit lands in the prompt.
//
// Returns ErrChatQuoteNotFound when the message is not queued or carries no such
// quote entry.
func UpdateQueuedMessageQuoteNote(sessionID, queueID, quoteID, note string) ([]model.FileEntry, error) {
	if sessionID == "" || queueID == "" || quoteID == "" {
		return nil, ErrChatQuoteNotFound
	}
	tx, err := WriteBegin()
	if err != nil {
		return nil, err
	}
	defer writeMu.Unlock()
	defer func() { _ = tx.Rollback() }()

	var filesJSON sql.NullString
	if qErr := tx.QueryRowContext(context.Background(),
		"SELECT files FROM queued_messages WHERE session_id = ? AND queue_id = ?",
		sessionID, queueID,
	).Scan(&filesJSON); qErr != nil {
		if errors.Is(qErr, sql.ErrNoRows) {
			return nil, ErrChatQuoteNotFound
		}
		return nil, qErr
	}

	entries := unmarshalFilesJSON(filesJSON.String)
	found := false
	for i := range entries {
		if entries[i].IsQuote() && entries[i].ID == quoteID {
			entries[i].Note = note
			found = true
			break
		}
	}
	if !found {
		return nil, ErrChatQuoteNotFound
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(context.Background(), "UPDATE queued_messages SET files = ? WHERE session_id = ? AND queue_id = ?", string(data), sessionID, queueID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return entries, nil
}
