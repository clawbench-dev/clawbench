package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// MigrateAskSummaryCardsDone heals already-persisted summary cards whose
// AskUserQuestion entry carries no `done` flag.
//
// extractSummaryCards used to copy the block's Done verbatim. An
// AskUserQuestion is a request for user input, so it never receives a
// tool_result and its block stays Done=false for the whole turn; persisted into
// summary_cards it became `"done"` absent (SummaryTool.Done is omitempty) and
// the frontend's showAskPending() — which cannot tell "false" from "absent" —
// then rendered the card's spinner in the summary view forever. The writer is
// fixed (Done is now forced true), but every summarization path SKIPS a message
// that already has a summary (see backfillMissingSummaries /
// triggerChatSummarization / GenerateMessageSummaryOnDemand), so rows written
// before the fix would never be rewritten on their own.
//
// Only the flag is touched: summary text, inputs, and every other card list are
// re-marshaled from the decoded struct, so the payload round-trips unchanged
// apart from the normalized `done`.
//
// One-time, ledger-gated, keyset-paginated (rows are rewritten and thus leave
// the matching set as they are processed, so OFFSET would drift and skip rows).
func MigrateAskSummaryCardsDone() bool {
	// `"done"` is omitted entirely when false, so the only reliable discriminator
	// is the absence of the key next to an AskUserQuestion tool entry. The LIKE
	// is a cheap pre-filter over the whole table; the JSON walk below is
	// authoritative (a `done` key belonging to a DIFFERENT tool entry, or the
	// literal text appearing in a summary, must not count).
	var needed int
	if err := store.ReadDB().QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM summaries
		WHERE target_type = 'chat_message'
		  AND summary_cards LIKE '%AskUserQuestion%'
		  AND summary_cards LIKE '%"tools"%'
	`).Scan(&needed); err != nil {
		slog.Error("ask summary card migration: count failed", slog.String("err", err.Error()))
		return false
	}
	if needed == 0 {
		return true
	}
	slog.Info("normalizing AskUserQuestion summary cards", slog.Int("candidates", needed))

	const batchSize = 200
	lastID := int64(0)
	migrated := 0
	failed := 0

	for {
		rows, err := store.ReadDB().QueryContext(
			context.Background(), `
			SELECT id, summary_cards FROM summaries
			WHERE target_type = 'chat_message'
			  AND summary_cards LIKE '%AskUserQuestion%'
			  AND summary_cards LIKE '%"tools"%'
			  AND id > ?
			ORDER BY id
			LIMIT ?`,
			lastID, batchSize,
		)
		if err != nil {
			slog.Error("ask summary card migration: query failed", slog.String("err", err.Error()))
			return false
		}

		type cardRow struct {
			ID    int64
			Cards string
		}
		var batch []cardRow
		for rows.Next() {
			var r cardRow
			if err = rows.Scan(&r.ID, &r.Cards); err != nil {
				slog.Error("ask summary card migration: scan failed", slog.String("err", err.Error()))
				continue
			}
			batch = append(batch, r)
		}
		if err = rows.Err(); err != nil {
			slog.Error("ask summary card migration: rows iteration failed", slog.String("err", err.Error()))
			_ = rows.Close() //nolint:sqlclosecheck // batched loop: cannot defer inside for-loop
			return false
		}
		_ = rows.Close()

		if len(batch) == 0 {
			break
		}

		for _, r := range batch {
			if err = normalizeAskCardDone(r.ID, r.Cards); err != nil {
				slog.Error("ask summary card migration: row failed",
					slog.Int64("id", r.ID),
					slog.String("err", err.Error()))
				failed++
			} else {
				migrated++
			}
			// Advance past every visited row so a row that cannot be rewritten
			// (undecodable JSON, no ask entry) is visited exactly once.
			lastID = r.ID
		}

		slog.Info("ask summary card migration progress",
			slog.Int("migrated", migrated),
			slog.Int("failed", failed),
			slog.Int("candidates", needed))

		if len(batch) < batchSize {
			break
		}
	}

	slog.Info("ask summary card migration complete",
		slog.Int("migrated", migrated),
		slog.Int("failed", failed),
		slog.Int("candidates", needed))
	return true
}

// normalizeAskCardDone rewrites one summaries row, forcing Done=true on every
// AskUserQuestion entry in summary_cards.tools. No-op (no write) when the row
// needs no change or cannot be decoded — a malformed blob is left exactly as
// found rather than replaced with a partially reconstructed one.
func normalizeAskCardDone(rowID int64, cardsJSON string) error {
	var cards model.SummaryCards
	if err := json.Unmarshal([]byte(cardsJSON), &cards); err != nil {
		return nil //nolint:nilerr // undecodable blob: leave untouched, not a failure
	}
	changed := false
	for i := range cards.Tools {
		if isSummaryCardTool(cards.Tools[i].Name) && !cards.Tools[i].Done {
			cards.Tools[i].Done = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	raw, err := json.Marshal(cards)
	if err != nil {
		return err
	}
	_, err = store.WriteExec(
		"UPDATE summaries SET summary_cards = ? WHERE id = ?",
		string(raw), rowID,
	)
	return err
}
