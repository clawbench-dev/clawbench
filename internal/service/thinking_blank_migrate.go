package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"clawbench/internal/store"
)

// blankThinkingSQL reports whether the SQL expression `expr` holds only
// whitespace (or is empty). SQLite has no regexp, so the trim set is spelled
// out; it must stay in sync with strings.TrimSpace in blankThinkingText.
func blankThinkingSQL(expr string) string {
	return "trim(coalesce(" + expr + ", ''), ' ' || char(9) || char(10) || char(13)) = ''"
}

func blankThinkingText(s string) bool {
	return strings.TrimSpace(s) == ""
}

// MigrateBlankThinkingBlocks removes thinking blocks that hold nothing but
// whitespace, together with the chat_thinking rows behind them.
//
// Why they exist: models emit lone space tokens between reasoning runs
// (measured in opencode's own store: 9944 single-space reasoning fragments out
// of 757k). The accumulator only skipped a COMPLETELY empty delta, so a space
// arriving right after a tool call opened a brand-new thinking block, and the
// next tool call closed it immediately. Each one surfaced as an expandable
// 「深度思考」chip with nothing inside — 545 rows across 58 messages in one real
// database, up to 16 in a single reply.
//
// The writer is fixed (AccumulateBlock and appendThinkingDelta now refuse to
// OPEN a block from whitespace), but existing rows are never rewritten on their
// own: a finalized message is not re-saved. Two shapes must be handled:
//
//   - slim markers `{type:"thinking", think_id}` whose chat_thinking rows are
//     all whitespace (the current shape);
//   - blocks that still carry inline whitespace text (the pre-split shape, from
//     before thinking moved into its own table).
//
// The verdict is "no loadable text", not "text looks empty": a slim marker whose
// row holds real reasoning must stay even though the marker itself carries no
// text. The frontend resolves a marker through (message_id, think_id) and then
// falls back to think_id + session_id (ACP multi-message sessions), so both
// scopes are checked before a block is declared dead.
//
// A message that would be left with no blocks at all is skipped — an empty
// bubble is worse than the empty chip it replaces.
//
// One-time, ledger-gated, keyset-paginated by message id.
func MigrateBlankThinkingBlocks() bool {
	// Cheap pre-filter; the JSON walk below is authoritative. Both branches are
	// supersets:
	//   - a message owning at least one whitespace-only chat_thinking row, or
	//   - a message whose content contains a whitespace-leading inline text
	//     (the pre-split shape). A real thought starting with a space is
	//     filtered out by the walk.
	//
	// Deliberately NOT gated on `content LIKE '%think_id%'`: a whitespace row
	// with no marker left in the content (an orphan) still has to be deleted,
	// and such a message mentions think_id nowhere.
	const candidateWhere = `
		h.role = 'assistant'
		  AND h.streaming = 0
		  AND (
		    EXISTS (SELECT 1 FROM chat_thinking t
		            WHERE t.message_id = h.id
		              AND ` + "trim(t.text, ' ' || char(9) || char(10) || char(13))" + ` = '')
		    OR h.content LIKE '%"text":" "%'
		    OR h.content LIKE '%"text":"\t"%'
		    OR h.content LIKE '%"text":"\n"%'
		    OR h.content LIKE '%"text":"\r"%'
		  )`

	var needed int
	if err := store.ReadDB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM chat_history h WHERE "+candidateWhere,
	).Scan(&needed); err != nil {
		slog.Error("blank thinking migration: count failed", slog.String("err", err.Error()))
		return false
	}
	if needed == 0 {
		return true
	}
	slog.Info("removing blank thinking blocks", slog.Int("candidates", needed))

	const batchSize = 100
	lastID := int64(0)
	cleaned := 0
	failed := 0

	for {
		rows, err := store.ReadDB().QueryContext(context.Background(), `
			SELECT h.id, h.session_id, h.content FROM chat_history h
			WHERE `+candidateWhere+`
			  AND h.id > ?
			ORDER BY h.id
			LIMIT ?`, lastID, batchSize)
		if err != nil {
			slog.Error("blank thinking migration: query failed", slog.String("err", err.Error()))
			return false
		}
		type msgRow struct {
			ID        int64
			SessionID string
			Content   string
		}
		var batch []msgRow
		for rows.Next() {
			var r msgRow
			if err := rows.Scan(&r.ID, &r.SessionID, &r.Content); err != nil {
				slog.Error("blank thinking migration: scan failed", slog.String("err", err.Error()))
				continue
			}
			batch = append(batch, r)
		}
		if err := rows.Err(); err != nil {
			slog.Error("blank thinking migration: rows iteration failed", slog.String("err", err.Error()))
			_ = rows.Close() //nolint:sqlclosecheck // batched loop: cannot defer inside for-loop
			return false
		}
		_ = rows.Close()

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			if err := cleanBlankThinkingForRow(r.ID, r.SessionID, r.Content); err != nil {
				slog.Error("blank thinking migration: row failed",
					slog.Int64("id", r.ID),
					slog.String("err", err.Error()))
				failed++
			} else {
				cleaned++
			}
			lastID = r.ID
		}

		slog.Info("blank thinking migration progress",
			slog.Int("cleaned", cleaned),
			slog.Int("failed", failed),
			slog.Int("total", needed))

		if len(batch) < batchSize {
			break
		}
	}

	slog.Info("blank thinking migration complete",
		slog.Int("cleaned", cleaned),
		slog.Int("failed", failed),
		slog.Int("candidates", needed))
	return true
}

// cleanBlankThinkingForRow removes one message's blank thinking blocks.
//
// The chat_thinking rows are deleted and the content rewritten in ONE
// transaction: a crash between the two must not leave a marker pointing at a
// deleted row (the frontend would then show a lazy-load error where it used to
// show an empty chip).
func cleanBlankThinkingForRow(msgID int64, sessionID, content string) error {
	var wrapper map[string]any
	// UseNumber keeps numeric fields byte-identical on the way back out: a plain
	// float64 round-trip would silently reformat them (and lose precision above
	// 2^53) in a row that this migration only means to prune.
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&wrapper); err != nil {
		// Not a blocks payload — the LIKE pre-filter can match the literal
		// "think_id" in ordinary prose. Nothing to do, and reporting an error
		// would keep the migration from ever being marked applied.
		return nil //nolint:nilerr // an unparseable row is out of scope, not a failure
	}
	blocksRaw, ok := wrapper[contentKeyBlocks].([]any)
	if !ok || len(blocksRaw) == 0 {
		return nil
	}

	// think_ids of THIS message whose every stored chunk is whitespace.
	blankIDs, err := blankThinkingIDs(msgID)
	if err != nil {
		return err
	}

	kept, err := pruneBlankThinkingBlocks(blocksRaw, blankIDs, func(thinkID string) (bool, error) {
		return thinkingRowExists(sessionID, thinkID)
	})
	if err != nil {
		return err
	}

	blocksChanged := len(kept) != len(blocksRaw)
	if !blocksChanged && len(blankIDs) == 0 {
		return nil
	}
	// Dropping everything would leave an empty bubble.
	if blocksChanged && len(kept) == 0 {
		return nil
	}

	updated := content
	if blocksChanged {
		wrapper[contentKeyBlocks] = kept
		marshaled, marshalErr := json.Marshal(wrapper)
		if marshalErr != nil {
			return marshalErr
		}
		updated = string(marshaled)
	}

	return writeBlankThinkingCleanup(msgID, updated, blankIDs, blocksChanged)
}

// pruneBlankThinkingBlocks returns the blocks that must be kept.
//
// Pure apart from the injected rowExists lookup, so the keep/drop verdicts can
// be tested without a database.
func pruneBlankThinkingBlocks(
	blocksRaw []any,
	blankIDs map[string]struct{},
	rowExists func(thinkID string) (bool, error),
) ([]any, error) {
	kept := make([]any, 0, len(blocksRaw))
	for _, raw := range blocksRaw {
		drop, err := isBlankThinkingBlock(raw, blankIDs, rowExists)
		if err != nil {
			return nil, err
		}
		if !drop {
			kept = append(kept, raw)
		}
	}
	return kept, nil
}

// isBlankThinkingBlock decides whether one block holds nothing loadable.
func isBlankThinkingBlock(
	raw any,
	blankIDs map[string]struct{},
	rowExists func(thinkID string) (bool, error),
) (bool, error) {
	block, ok := raw.(map[string]any)
	if !ok || block[contentKeyType] != blockTypeThinking {
		return false, nil
	}
	thinkID, _ := block["think_id"].(string)
	text, hasText := block["text"].(string)
	// A block still showing real text is never touched, whatever the rows say
	// (a replayed stream can carry the whole thought inline).
	if hasText && !blankThinkingText(text) {
		return false, nil
	}
	if thinkID == "" {
		// No identity and no real text: nothing can ever be loaded for it.
		return true, nil
	}
	if _, isBlank := blankIDs[thinkID]; isBlank {
		// Rows exist and every one of them is whitespace — this is the shape
		// the writer bug produced.
		return true, nil
	}
	if hasText {
		// Pre-split shape: inline whitespace with no row behind it. Drop only
		// when the row really is missing; if a row exists the marker is
		// loadable and the inline whitespace is just a stale prefix.
		exists, err := rowExists(thinkID)
		if err != nil {
			return false, err
		}
		if !exists {
			return true, nil
		}
	}
	// A marker with NO text and no row is a different defect (a lost
	// chat_thinking row). Its reasoning may be real, so this migration — whose
	// subject is blank blocks — leaves it alone.
	return false, nil
}

// writeBlankThinkingCleanup deletes the blank rows and, when the block list
// changed, rewrites the message content — all in one transaction so a crash
// cannot leave a marker pointing at a deleted row.
func writeBlankThinkingCleanup(msgID int64, updated string, blankIDs map[string]struct{}, blocksChanged bool) error {
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	ctx := context.Background()
	for thinkID := range blankIDs {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM chat_thinking WHERE message_id = ? AND think_id = ?", msgID, thinkID,
		); err != nil {
			return err
		}
	}
	// Orphan rows (a whitespace row with no marker left in the content) can be
	// deleted without touching the message; rewriting identical content would
	// only cost a pointless full-row write.
	if blocksChanged {
		if _, err := tx.ExecContext(ctx,
			"UPDATE chat_history SET content = ? WHERE id = ?", updated, msgID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// blankThinkingIDs returns the think_ids of a message whose every stored chunk
// is whitespace.
func blankThinkingIDs(msgID int64) (map[string]struct{}, error) {
	// HAVING on the aggregate, not a per-row filter: a think_id is blank only
	// when ALL of its seq chunks are blank. A block whose first chunk is a space
	// and whose later chunks carry real reasoning must survive.
	rows, err := store.ReadDB().QueryContext(context.Background(), `
		SELECT think_id
		FROM chat_thinking
		WHERE message_id = ?
		GROUP BY think_id
		HAVING SUM(CASE WHEN `+blankThinkingSQL("text")+` THEN 1 ELSE 0 END) = COUNT(*)`,
		msgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	ids := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}

// thinkingRowExists reports whether any chunk for thinkID exists in the session.
func thinkingRowExists(sessionID, thinkID string) (bool, error) {
	var n int
	err := store.ReadDB().QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM chat_thinking WHERE session_id = ? AND think_id = ?`,
		sessionID, thinkID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
