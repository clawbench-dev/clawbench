package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// ThinkingRecord represents a row in the chat_thinking table.
// Text is the full concatenated thinking text for the (message_id, think_id)
// pair across all seq chunks. Seq is internal chunk ordering and never exposed
// through the API JSON.
type ThinkingRecord struct {
	ID        int64     `json:"id"`
	MessageID int64     `json:"message_id"`
	SessionID string    `json:"session_id"`
	ThinkID   string    `json:"think_id"`
	Seq       int       `json:"-"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// generateThinkingID returns a think_id ("th_" + 32 hex chars).
func generateThinkingID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("th_%d", time.Now().UnixNano())
	}
	return "th_" + hex.EncodeToString(b)
}

// thinkingBatchChunkRows bounds how many rows go into a single INSERT. SQLite
// caps a statement at 32766 bound variables; this INSERT binds 4 per row (seq is
// a literal 0, not a parameter), so the hard ceiling is 8191 rows. 1000 keeps a
// wide margin (4000 params) and bounds the size of the generated SQL string.
const thinkingBatchChunkRows = 1000

// ReplaceThinkingForMessage rewrites every thinking row of a message in ONE
// transaction: it deletes all existing rows, then inserts the given records.
//
// This is the batch equivalent of DeleteThinkingByMessage followed by an
// UpsertThinking per record. That sequence issued N+1 independent transactions
// (one per record, each taking the global write lock and paying its own commit
// fsync) and was NOT atomic — a crash between the delete and the inserts left
// the message with no thinking rows at all while the content row still carried
// slim think_id markers, losing the reasoning permanently. A single transaction
// makes the rewrite all-or-nothing: a mid-way failure rolls back to the rows
// that existed before, which the 500ms streaming flush already persisted.
//
// The delete is deliberately a full per-message delete rather than a diff:
// MergeConsecutiveThinkingBlocks merges adjacent thinking blocks, which retires
// some think_ids entirely — their rows must go, and only a full delete
// guarantees that.
//
// Records with an empty think_id or empty text are skipped (they have nothing to
// store), matching UpsertThinking. An empty record set is a no-op — the caller
// only rewrites when there is something to store, so a message whose thinking
// was entirely dropped keeps whatever the streaming flush already persisted.
func ReplaceThinkingForMessage(messageID int64, sessionID string, records []ThinkingRecord) error {
	if messageID <= 0 || len(records) == 0 {
		return nil
	}
	tx, err := WriteBegin()
	if err != nil {
		return fmt.Errorf("ReplaceThinkingForMessage begin: %w", err)
	}
	defer writeMu.Unlock()
	defer func() { _ = tx.Rollback() }()

	ctx := context.Background()
	if _, err := tx.ExecContext(ctx, "DELETE FROM chat_thinking WHERE message_id = ?", messageID); err != nil {
		return fmt.Errorf("ReplaceThinkingForMessage delete: %w", err)
	}

	// Collapse duplicate think_ids to their last occurrence. The previous
	// per-record shape (delete + insert, once per record) resolved a duplicate
	// by last-wins, so a repeated think_id overwrote its own row instead of
	// failing. A single multi-row INSERT would instead hit
	// UNIQUE(think_id, message_id, seq) and abort the whole rewrite, so the
	// dedupe is what preserves the old semantics under the batch.
	deduped := make([]ThinkingRecord, 0, len(records))
	seenAt := make(map[string]int, len(records))
	for _, rec := range records {
		if rec.ThinkID == "" || rec.Text == "" {
			continue
		}
		if at, ok := seenAt[rec.ThinkID]; ok {
			deduped[at] = rec
			continue
		}
		seenAt[rec.ThinkID] = len(deduped)
		deduped = append(deduped, rec)
	}
	records = deduped

	// Chunked so a large message (thousands of blocks) stays under the SQLite
	// bound-variable limit while remaining inside the single transaction.
	for start := 0; start < len(records); start += thinkingBatchChunkRows {
		end := start + thinkingBatchChunkRows
		if end > len(records) {
			end = len(records)
		}
		chunk := records[start:end]

		query := "INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text) VALUES "
		args := make([]any, 0, len(chunk)*4)
		for i, rec := range chunk {
			if i > 0 {
				query += ","
			}
			query += "(?, ?, ?, 0, ?)"
			args = append(args, messageID, sessionID, rec.ThinkID, rec.Text)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("ReplaceThinkingForMessage insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ReplaceThinkingForMessage commit: %w", err)
	}
	return nil
}

// UpsertThinking stores the complete thinking text for a (think_id, message_id)
// pair as a single seq=0 chunk. It deletes any prior chunks (streaming flushes
// may have appended seq=0..n deltas) before inserting, so the stored text is
// exactly the passed full text — the seq=0 row is the canonical "final" state
// used by Finalize / forced flush / migration. Atomic under a write transaction
// so a concurrent reader never observes an empty window.
func UpsertThinking(messageID int64, sessionID, thinkID, text string) error {
	if thinkID == "" || text == "" {
		return nil
	}
	tx, err := WriteBegin()
	if err != nil {
		return fmt.Errorf("UpsertThinking begin: %w", err)
	}
	defer writeMu.Unlock()
	defer func() { _ = tx.Rollback() }()

	ctx := context.Background()
	if _, err := tx.ExecContext(ctx, "DELETE FROM chat_thinking WHERE think_id = ? AND message_id = ?", thinkID, messageID); err != nil {
		return fmt.Errorf("UpsertThinking delete: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text)
		VALUES (?, ?, ?, 0, ?)
	`, messageID, sessionID, thinkID, text); err != nil {
		return fmt.Errorf("UpsertThinking insert: %w", err)
	}
	return tx.Commit()
}

// AppendThinkingSegment inserts a single incremental delta chunk at the given
// seq for a (think_id, message_id) pair. Used by the streaming flush to persist
// only the text that grew since the last flush window instead of rewriting the
// full accumulated text. Idempotent per seq: a retry after a failure overwrites
// the same chunk with identical text (ON CONFLICT), never duplicating bytes.
func AppendThinkingSegment(messageID int64, sessionID, thinkID string, seq int, delta string) error {
	if thinkID == "" || delta == "" {
		return nil
	}
	_, err := WriteExecContext(context.Background(), `
		INSERT INTO chat_thinking (message_id, session_id, think_id, seq, text)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(think_id, message_id, seq) DO UPDATE SET text = excluded.text
	`, messageID, sessionID, thinkID, seq, delta)
	if err != nil {
		return fmt.Errorf("AppendThinkingSegment: %w", err)
	}
	return nil
}

// DeleteThinkingByMessage removes thinking records for a message.
// Called before insert in the Finalize write path for idempotency.
func DeleteThinkingByMessage(messageID int64) error {
	_, err := WriteExecContext(context.Background(), "DELETE FROM chat_thinking WHERE message_id = ?", messageID)
	if err != nil {
		return fmt.Errorf("DeleteThinkingByMessage: %w", err)
	}
	return nil
}

// GetThinking retrieves the full (concatenated) thinking text for a think_id +
// message_id pair. Returns nil if not found. All seq chunks are concatenated in
// order, so the returned text is identical whether the record was written as a
// single full upsert or as incremental streaming segments.
func GetThinking(thinkID string, messageID int64) (*ThinkingRecord, error) {
	return scanThinkingChunks(thinkID, messageID)
}

// scanThinkingChunks fetches all seq rows for a (thinkID, messageID) pair,
// concatenates their text in seq order, and returns a single record. Returns
// (nil, nil) when no rows exist.
func scanThinkingChunks(thinkID string, messageID int64) (*ThinkingRecord, error) {
	rows, err := dbRead.QueryContext(context.Background(), `
		SELECT id, message_id, session_id, think_id, seq, text, created_at
		FROM chat_thinking WHERE think_id = ? AND message_id = ?
		ORDER BY seq ASC
	`, thinkID, messageID)
	if err != nil {
		return nil, fmt.Errorf("scanThinkingChunks query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var rec *ThinkingRecord
	for rows.Next() {
		var r ThinkingRecord
		if err := rows.Scan(&r.ID, &r.MessageID, &r.SessionID, &r.ThinkID, &r.Seq, &r.Text, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanThinkingChunks scan: %w", err)
		}
		if rec == nil {
			rec = &ThinkingRecord{
				ID:        r.ID, // id of the lowest-seq chunk (stable anchor)
				MessageID: r.MessageID,
				SessionID: r.SessionID,
				ThinkID:   r.ThinkID,
				Seq:       r.Seq,
				CreatedAt: r.CreatedAt,
			}
		}
		rec.Text += r.Text
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanThinkingChunks iterate: %w", err)
	}
	return rec, nil
}

// GetThinkingBySession retrieves a thinking record by think_id and session_id.
// Fallback for ACP multi-assistant-message sessions where the frontend may not
// know the exact message_id (mirrors GetToolCallBySession).
func GetThinkingBySession(thinkID, sessionID string) (*ThinkingRecord, error) {
	var messageID int64
	err := dbRead.QueryRowContext(context.Background(), `
		SELECT message_id FROM chat_thinking WHERE think_id = ? AND session_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, thinkID, sessionID).Scan(&messageID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetThinkingBySession locate: %w", err)
	}
	return scanThinkingChunks(thinkID, messageID)
}

// GetThinkingBySessionAll retrieves all thinking records for a session.
// Each distinct (think_id, message_id) pair is returned once with its chunks
// concatenated in seq order. Used by BuildForkContext to batch-fetch thinking
// text without N+1 queries.
func GetThinkingBySessionAll(sessionID string) ([]ThinkingRecord, error) {
	rows, err := dbRead.QueryContext(context.Background(), `
		SELECT message_id, think_id, seq, text
		FROM chat_thinking WHERE session_id = ?
		ORDER BY message_id ASC, think_id ASC, seq ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("GetThinkingBySessionAll: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Group chunks by (message_id, think_id) preserving first-seen order.
	type groupKey struct {
		messageID int64
		thinkID   string
	}
	var order []groupKey
	grouped := make(map[groupKey]*ThinkingRecord)
	for rows.Next() {
		var msgID int64
		var thinkID, text string
		var seq int
		if err := rows.Scan(&msgID, &thinkID, &seq, &text); err != nil {
			return nil, fmt.Errorf("GetThinkingBySessionAll scan: %w", err)
		}
		key := groupKey{msgID, thinkID}
		rec, ok := grouped[key]
		if !ok {
			rec = &ThinkingRecord{MessageID: msgID, ThinkID: thinkID, SessionID: sessionID, Seq: seq}
			grouped[key] = rec
			order = append(order, key)
		}
		rec.Text += text
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetThinkingBySessionAll iterate: %w", err)
	}

	records := make([]ThinkingRecord, 0, len(order))
	for _, k := range order {
		records = append(records, *grouped[k])
	}
	return records, nil
}

// slimThinkingInContent parses content JSON, extracts thinking block text into
// ThinkingRecord entries, and rewrites the content with slim thinking blocks
// ({type:"thinking", think_id, done} — text removed).
//
// think_id handling:
//   - Block without think_id: a fresh ID is generated and the text extracted.
//   - Block with think_id (already slimmed, or pre-assigned by the periodic
//     flush in SessionExecutor): the ID is reused and any text still present is
//     extracted so the final text (after MergeConsecutiveThinkingBlocks
//     concatenation) overwrites the periodic-flush rows.
//
// Thinking blocks without text are slimmed too (think_id assigned, no record);
// if nothing changed, returns content unchanged with empty records.
func slimThinkingInContent(content string) (string, []ThinkingRecord, error) {
	var wrapper map[string]any
	if err := json.Unmarshal([]byte(content), &wrapper); err != nil {
		return content, nil, fmt.Errorf("slimThinkingInContent: unmarshal: %w", err)
	}
	blocksRaw, ok := wrapper[contentKeyBlocks].([]any)
	if !ok {
		return content, nil, nil
	}
	var records []ThinkingRecord
	changed := false
	for i := range blocksRaw {
		block, ok := blocksRaw[i].(map[string]any)
		if !ok || block[contentKeyType] != blockTypeThinking {
			continue
		}
		text, _ := block["text"].(string)
		thinkID, hasID := block["think_id"].(string)
		if !hasID || thinkID == "" {
			thinkID = generateThinkingID()
			block["think_id"] = thinkID
			changed = true
		}
		delete(block, "text")
		// Extract text whenever present — even for a pre-existing think_id — so
		// the final (possibly merged) text overwrites periodic-flush rows.
		if text != "" {
			records = append(records, ThinkingRecord{ThinkID: thinkID, Text: text})
			changed = true
		}
		// A slim marker must never claim to be in-progress. The frontend renders
		// `{think_id, done:false}` with no text as a perpetual spinner ("输出中")
		// that never emits anything — the text was just moved to chat_thinking,
		// so nothing will ever arrive to finish it. Every caller of this function
		// is a terminal path (Finalize, orphan cleanup, the forced shutdown
		// flush, and the steer split's "before" half), where no thinking block
		// can still be running, so the flag is both safe and truthful here.
		// Leaving it false is what produced ~181k stale `done:false` markers in
		// production, each of which rendered as a headless spinner once the
		// frontend adopted the DB row on a session switch.
		if done, _ := block["done"].(bool); !done {
			block["done"] = true
			changed = true
		}
	}
	if !changed {
		return content, nil, nil
	}
	slim, err := json.Marshal(wrapper)
	if err != nil {
		return content, nil, fmt.Errorf("slimThinkingInContent: marshal: %w", err)
	}
	return string(slim), records, nil
}

// persistThinkingToDB slims thinking text out of the DB content into chat_thinking.
// Returns the content to persist (slimmed if thinking records were extracted).
// The WS terminal event keeps full blocks; only the persisted content is slimmed.
func persistThinkingToDB(content string, streamingMsgID int64, sessionID string) string {
	if streamingMsgID <= 0 || sessionID == "" {
		return content
	}
	slimContent, records, err := slimThinkingInContent(content)
	if err != nil {
		slog.Warn("slim thinking failed; persisting full content", slog.Int64("msgID", streamingMsgID), slog.String("err", err.Error()))
		return content
	}
	if slimContent == content {
		return content
	}
	if len(records) > 0 {
		// One transaction for the whole rewrite (delete + all inserts). The
		// previous delete-then-upsert-per-record shape issued N+1 transactions
		// and was not atomic: a crash after the delete but before the inserts
		// wiped the message's reasoning while the content row kept its slim
		// think_id markers, so the frontend lazy-loaded nothing. A single
		// transaction either replaces every row or leaves the streamed rows
		// intact.
		if err := ReplaceThinkingForMessage(streamingMsgID, sessionID, records); err != nil {
			slog.Warn("replace thinking for message failed",
				slog.Int64("msgID", streamingMsgID),
				slog.Int("records", len(records)),
				slog.String("err", err.Error()))
			// Keep the unslimmed content: the rows were not rewritten, so the
			// thinking text must stay reachable from the content row rather
			// than being dropped on the floor.
			return content
		}
	}
	return slimContent
}
