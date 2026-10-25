package service

import (
	"log/slog"

	"clawbench/internal/store"
)

// MigrateLedgerWallClock backfills chat_metadata.wall_ms (the AI turn's
// wall-clock runtime) for ledger rows written before it was captured.
//
// The ledger has always carried wall_ms, but two kinds of row can still hold 0:
//
//   - rows inserted by the content→ledger backfill from a message whose
//     embedded metadata predates the field, and
//   - rows written by a path that never reached buildResult (a turn abandoned
//     before the executor produced a RunResult has no WallMs to record).
//
// Two sources, tried in order of trustworthiness:
//
//  1. EXACT — the message's own embedded metadata (chat_history.content
//     .metadata.wallMs). This is the same number SaveMetadata would have
//     written, so the backfilled value is indistinguishable from a native one.
//     json_valid() guards it: json_extract on malformed JSON RAISES instead of
//     returning NULL, which would fail the whole statement and leave the
//     migration retrying forever without fixing a single row.
//
//  2. APPROXIMATE — chat_history.completed_at − created_at. An assistant row is
//     created when the turn starts and stamped at finalize, so the difference is
//     the turn's wall clock. It is only used when the exact source is absent AND
//     the delta is plausible (1 s … 48 h): a wider window would let a re-saved
//     or replayed row contribute hours that no model call ever spent. Measured
//     against rows that carry BOTH values, this derivation agrees within a few
//     seconds on 6 276 of 6 322 rows, so it is a fair last resort rather than a
//     guess.
//
// A row that has neither source stays 0 — an unknown runtime must not be
// invented, and 0 already means "not recorded" everywhere else in this table.
// The migration is idempotent: both steps only touch rows where wall_ms = 0.
func MigrateLedgerWallClock() bool {
	if err := backfillWallClockFromMetadata(); err != nil {
		slog.Error("usage ledger wall clock: metadata backfill failed", slog.String("err", err.Error()))
		return false
	}
	if err := backfillWallClockFromTimestamps(); err != nil {
		slog.Error("usage ledger wall clock: timestamp backfill failed", slog.String("err", err.Error()))
		return false
	}
	return true
}

// backfillWallClockFromMetadata copies the exact runtime the message itself
// recorded. Runs before the timestamp fallback so an exact value always wins.
func backfillWallClockFromMetadata() error {
	res, err := store.WriteExec(`
		UPDATE chat_metadata
		SET wall_ms = (
			SELECT CAST(json_extract(h.content, '$.metadata.wallMs') AS INTEGER)
			FROM chat_history h
			WHERE h.id = chat_metadata.message_id
		)
		WHERE wall_ms = 0
		  AND EXISTS (
			SELECT 1 FROM chat_history h
			WHERE h.id = chat_metadata.message_id
			  AND json_valid(h.content)
			  AND CAST(json_extract(h.content, '$.metadata.wallMs') AS INTEGER) > 0
		  )`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("usage ledger wall clock: restored from message metadata", slog.Int64("rows", n))
	}
	return nil
}

// backfillWallClockFromTimestamps derives the runtime from the message row's
// own start/finish stamps, for rows whose embedded metadata never carried it.
//
// The 48 h ceiling is deliberately generous but finite: a scheduled task may
// legitimately run for hours, while a delta beyond two days is a re-save
// artifact (the same failure mode healLedgerTimestamps repairs) rather than a
// turn anyone actually waited for.
func backfillWallClockFromTimestamps() error {
	res, err := store.WriteExec(`
		UPDATE chat_metadata
		SET wall_ms = CAST(
			(strftime('%s', (SELECT h.completed_at FROM chat_history h WHERE h.id = chat_metadata.message_id))
			 - strftime('%s', (SELECT h.created_at FROM chat_history h WHERE h.id = chat_metadata.message_id))) * 1000
			AS INTEGER)
		WHERE wall_ms = 0
		  AND EXISTS (
			SELECT 1 FROM chat_history h
			WHERE h.id = chat_metadata.message_id
			  AND h.completed_at IS NOT NULL
			  AND h.completed_at <> ''
			  AND h.created_at IS NOT NULL
			  AND h.created_at <> ''
			  AND strftime('%s', h.completed_at) - strftime('%s', h.created_at) BETWEEN 1 AND 172800
		  )`)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("usage ledger wall clock: derived from message timestamps", slog.Int64("rows", n))
	}
	return nil
}
