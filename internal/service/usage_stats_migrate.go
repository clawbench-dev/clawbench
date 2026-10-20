package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"clawbench/internal/store"
)

// healUsageLedger repairs chat_metadata rows written before the ledger became
// SUM-able. Each step fixes one measured defect; all of them are idempotent so
// the migration can be retried after a partial failure.
//
// The defects, all verified against a production database:
//
//  1. COST — ACP's usage_update.cost is the SESSION CUMULATIVE cost, but it was
//     stored verbatim per message. SUM(cost_usd) therefore counted the running
//     total once per message (Claude: $110.15 reported vs $39.97 actual).
//     Rows are re-based onto per-turn deltas and the raw total is preserved in
//     cost_session_total so future turns can keep diffing.
//  2. LEGACY INPUT — the pre-ACP CodeBuddy CLI parser stored the
//     SESSION-CUMULATIVE prompt size in input_tokens (one measured session
//     climbs 73k → 31.0M across ten turns in a 200k window), inflating the
//     total by 74%. Deltas are reconstructed.
//  3. TOTAL — parsers that dropped the upstream total left total_tokens = 0
//     (OpenCode/Pi/Hermes/CommandCode), and an older CodeBuddy build wrote
//     total < input+output. Under the panel's default metric those rows were
//     filtered out of the table while still counting toward the totals.
//  4. MODEL — rows with no model name but a known response_model_id collapsed
//     into "(empty)" (27% of Claude's tokens).
//  5. CACHE SPLIT — Claude/Codex report cache counters OUTSIDE inputTokens, so
//     their cache_hit/miss stayed 0 and the panel's hit rate described
//     CodeBuddy alone (98.3% on a mixed DB). input_tokens is normalized to the
//     CodeBuddy shape (full prompt) and the split is derived, only when the
//     row's own counters satisfy total = input+output+cacheRead+cacheWrite.
//  6. AGENT — group-chat member turns were attributed to the session's agent
//     (the HOST) instead of the speaker. chat_history.agent_id holds the
//     member row id; re-resolve it to the real agent.
//  7. CREATED_AT — SaveMetadata used INSERT OR REPLACE without created_at, so
//     a re-save (notably the content→ledger backfill) re-stamped old messages
//     with the migration time. Restore the message's own completion time.
//
// NOTE: there is deliberately NO "too many tokens for the context window"
// step. A single agentic turn issues many model calls (one per tool round) and
// the reported counters sum over them — a measured 6.5M-token turn in a 200k
// window was ~30 calls of ~217k each. Such a guard destroys real usage.
func healUsageLedger() bool {
	steps := []struct {
		name string
		fn   func() error
	}{
		// The token steps are ordered: healLedgerLegacyCumulativeInput keys off
		// total_tokens = 0, which healLedgerTokens erases by raising the total
		// to input+output.
		{"cost", healLedgerCost},
		{"legacy input", healLedgerLegacyCumulativeInput},
		{"tokens", healLedgerTokens},
		{"cache split", healLedgerCacheSplit},
		{"agent", healLedgerAgent},
		{"timestamps", healLedgerTimestamps},
	}
	for _, s := range steps {
		if err := s.fn(); err != nil {
			slog.Error("usage ledger migration failed",
				slog.String("step", s.name), slog.String("err", err.Error()))
			return false
		}
	}
	return true
}

// cumulativeWalk reconstructs per-turn values from a session's running counter.
//
// The first row of a session bills its whole value; a rising value bills the
// increment; a drop means the counter restarted (new epoch — a model switch does
// this, measured as claude-sonnet-4-6 → MiniMax dropping 0.47 → 0.21) so the
// whole reported value belongs to that turn. The baseline then follows the new
// counter, because keeping the old maximum would make every later turn diff
// against a value that can never be reached again and re-count its whole amount.
func cumulativeWalk(values []float64) []float64 {
	out := make([]float64, len(values))
	prev := 0.0
	for i, v := range values {
		delta := v - prev
		if delta < 0 {
			delta = v
		}
		out[i] = delta
		prev = v
	}
	return out
}

// groupBySession splits a session-ordered slice into runs of equal session id
// and returns the reconstructed values in the same order.
func groupBySession(sessions []string, values []float64) []float64 {
	out := make([]float64, len(values))
	start := 0
	// Iterate over the shorter of the two: a value with no session cannot be
	// attributed, and a session with no value has nothing to reconstruct. The
	// index is bounded explicitly so the slice access below is provably safe.
	for i := 1; i <= len(sessions); i++ {
		// Bounds-check before indexing rather than relying on the loop
		// condition: gosec cannot see that i < len(sessions) whenever the
		// right-hand operand is evaluated, and an explicit guard documents it.
		sameRun := false
		if i < len(sessions) {
			sameRun = sessions[i] == sessions[start]
		}
		if sameRun {
			continue
		}
		end := min(i, len(values))
		if start < end {
			copy(out[start:end], cumulativeWalk(values[start:end]))
		}
		start = end
	}
	return out
}

// healLedgerCost converts every ACP-sourced cumulative cost into a per-turn
// delta.
//
// The one-time migration is scoped to the backends whose stored values were
// verified to be cumulative (Claude and OpenCode: monotonically rising within a
// session while per-turn tokens fell). New rows are tagged at ingestion via
// ai.Metadata.CostCumulative, so this predicate never has to grow.
//
// CodeBuddy is absent by construction: its usage_update.cost carries credit,
// which costFieldCarriesCredit discards, so it never had a money value here.
// CLI backends that report a genuine per-turn cost (pi) are left untouched.
//
// The same delta is written into the message's embedded content JSON: the
// detail modal reads that copy, and leaving the running total there would make
// the per-message view claim one reply cost the whole session.
func healLedgerCost() error {
	rows, err := store.ReadDB().Query(`
		SELECT message_id, clawbench_session_id, COALESCE(session_id, ''), cost_usd
		FROM chat_metadata
		WHERE backend IN ('claude', 'opencode')
		  AND cost_usd > 0
		  AND cost_cumulative = 0
		ORDER BY COALESCE(NULLIF(session_id, ''), clawbench_session_id), message_id`)
	if err != nil {
		return fmt.Errorf("query cumulative cost rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	var partitions []string
	var totals []float64
	for rows.Next() {
		var id int64
		var session, extSession string
		var cost float64
		if scanErr := rows.Scan(&id, &session, &extSession, &cost); scanErr != nil {
			return fmt.Errorf("scan cumulative cost row: %w", scanErr)
		}
		// Partition by the agent CONNECTION (the external ACP session id), the
		// same key the ingestion path uses. In a group chat every member has its
		// own counter while all their messages share one timeline, so
		// partitioning by timeline would diff one member's total against
		// another's.
		partition := extSession
		if partition == "" {
			partition = session
		}
		ids = append(ids, id)
		partitions = append(partitions, partition)
		totals = append(totals, cost)
	}
	if iterErr := rows.Err(); iterErr != nil {
		return fmt.Errorf("iterate cumulative cost rows: %w", iterErr)
	}
	if len(ids) == 0 {
		return nil
	}

	deltas := groupBySession(partitions, totals)
	return applyLedgerUpdates(len(ids), func(tx *sql.Tx, i int) error {
		if _, execErr := tx.ExecContext(context.Background(),
			"UPDATE chat_metadata SET cost_usd = ?, cost_session_total = ?, cost_cumulative = 1 WHERE message_id = ?",
			deltas[i], totals[i], ids[i],
		); execErr != nil {
			return fmt.Errorf("rebase cost for message %d: %w", ids[i], execErr)
		}
		if contentErr := rewriteContentCost(tx, ids[i], deltas[i]); contentErr != nil {
			return contentErr
		}
		return nil
	})
}

// rewriteContentCost replaces metadata.costUsd in a message's stored content
// JSON with the given per-turn value. Content that does not parse, or that has
// no embedded metadata, is left untouched — the ledger remains authoritative
// for the statistics either way.
func rewriteContentCost(tx *sql.Tx, messageID int64, cost float64) error {
	var content string
	scanErr := tx.QueryRowContext(context.Background(),
		"SELECT content FROM chat_history WHERE id = ?", messageID).Scan(&content)
	if errors.Is(scanErr, sql.ErrNoRows) {
		return nil
	}
	if scanErr != nil {
		return fmt.Errorf("read content for message %d: %w", messageID, scanErr)
	}
	encoded, changed := contentWithTurnCost(content, cost)
	if !changed {
		return nil
	}
	if _, execErr := tx.ExecContext(context.Background(),
		"UPDATE chat_history SET content = ? WHERE id = ?", encoded, messageID,
	); execErr != nil {
		return fmt.Errorf("rewrite content cost for message %d: %w", messageID, execErr)
	}
	return nil
}

// contentWithTurnCost replaces metadata.costUsd in a stored content JSON
// document and strips the ledger-only cost fields. The bool reports whether the
// document was rewritten; unparseable content, or content without embedded
// metadata, is passed through untouched (the ledger stays authoritative for the
// statistics either way).
func contentWithTurnCost(content string, cost float64) (string, bool) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return content, false
	}
	meta, ok := doc["metadata"].(map[string]any)
	if !ok {
		return content, false
	}
	meta["costUsd"] = cost
	// The raw session total is ledger bookkeeping, not something the message
	// view should ever render.
	delete(meta, "costCumulative")
	delete(meta, "costSessionTotal")
	encoded, err := json.Marshal(doc)
	if err != nil {
		return content, false
	}
	return string(encoded), true
}

// healLedgerLegacyCumulativeInput reconstructs per-turn input tokens for rows
// written by the pre-ACP CodeBuddy CLI parser.
//
// Those rows (identifiable by total_tokens = 0 — that column did not exist yet)
// stored the SESSION-CUMULATIVE prompt size: one measured session climbs
// 73k → 11.5M → 31.0M across ten turns while its window is 200k. Summing them
// inflates the total by 74% (2.21B stored vs 1.27B actual).
//
// MUST run before healLedgerTokens, which raises total_tokens to input+output
// and would erase the total_tokens = 0 discriminator.
func healLedgerLegacyCumulativeInput() error {
	rows, err := store.ReadDB().Query(`
		SELECT message_id, clawbench_session_id, input_tokens
		FROM chat_metadata
		WHERE backend = 'codebuddy'
		  AND total_tokens = 0
		  AND input_tokens > 0
		ORDER BY clawbench_session_id, message_id`)
	if err != nil {
		return fmt.Errorf("query legacy cumulative input rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	var sessions []string
	var inputs []float64
	for rows.Next() {
		var id, input int64
		var session string
		if scanErr := rows.Scan(&id, &session, &input); scanErr != nil {
			return fmt.Errorf("scan legacy input row: %w", scanErr)
		}
		ids = append(ids, id)
		sessions = append(sessions, session)
		inputs = append(inputs, float64(input))
	}
	if iterErr := rows.Err(); iterErr != nil {
		return fmt.Errorf("iterate legacy input rows: %w", iterErr)
	}
	if len(ids) == 0 {
		return nil
	}

	deltas := groupBySession(sessions, inputs)
	return applyLedgerUpdates(len(ids), func(tx *sql.Tx, i int) error {
		if _, execErr := tx.ExecContext(context.Background(),
			"UPDATE chat_metadata SET input_tokens = ? WHERE message_id = ?", int64(deltas[i]), ids[i],
		); execErr != nil {
			return fmt.Errorf("normalize legacy input for message %d: %w", ids[i], execErr)
		}
		return nil
	})
}

// applyLedgerUpdates runs apply for every index inside one write transaction.
func applyLedgerUpdates(n int, apply func(tx *sql.Tx, i int) error) error {
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()
	for i := range n {
		if applyErr := apply(tx, i); applyErr != nil {
			return applyErr
		}
	}
	return tx.Commit()
}

// healLedgerTokens enforces the total ≥ input+output invariant and fills in the
// model name from the response model when the request model was never reported.
func healLedgerTokens() error {
	res, err := store.WriteExec(`
		UPDATE chat_metadata
		SET total_tokens = input_tokens + output_tokens
		WHERE total_tokens < input_tokens + output_tokens`)
	if err != nil {
		return fmt.Errorf("backfill total_tokens: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("usage ledger migration: backfilled total_tokens", slog.Int64("rows", n))
	}
	res, err = store.WriteExec(`
		UPDATE chat_metadata
		SET model = response_model_id
		WHERE (model IS NULL OR model = '') AND response_model_id <> ''`)
	if err != nil {
		return fmt.Errorf("backfill model: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("usage ledger migration: backfilled model from response_model_id", slog.Int64("rows", n))
	}
	return nil
}

// healLedgerCacheSplit normalizes the Claude/Codex token shape to the one the
// stats panel assumes (input includes cached tokens, cache_hit/miss split out).
//
// The predicate is the row's own internal consistency:
//
//	total_tokens = input_tokens + output_tokens + cached_read + cached_write
//
// which is true only for the OLD (un-normalized) shape — after the rewrite
// total = input+output, so the predicate stops matching and the migration is
// idempotent. A row that never reported an aggregate total (total = 0) or that
// uses a different convention is left untouched rather than guessed at.
func healLedgerCacheSplit() error {
	res, err := store.WriteExec(`
		UPDATE chat_metadata
		SET cache_hit_tokens = COALESCE(cached_read_tokens, 0),
		    cache_miss_tokens = input_tokens,
		    input_tokens = input_tokens + COALESCE(cached_read_tokens, 0) + COALESCE(cached_write_tokens, 0)
		WHERE backend IN ('claude', 'codex')
		  AND COALESCE(cache_hit_tokens, 0) = 0
		  AND COALESCE(cache_miss_tokens, 0) = 0
		  AND COALESCE(cached_read_tokens, 0) + COALESCE(cached_write_tokens, 0) > 0
		  AND total_tokens = input_tokens + output_tokens
		                    + COALESCE(cached_read_tokens, 0) + COALESCE(cached_write_tokens, 0)`)
	if err != nil {
		return fmt.Errorf("normalize claude/codex cache split: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("usage ledger migration: normalized cache split", slog.Int64("rows", n))
	}
	return nil
}

// healLedgerAgent re-resolves the ledger's agent attribution for rows whose
// message carries a speaker id (group-chat member turns).
//
// chat_history.agent_id is a member session ROW id for those turns; resolving
// it to the real agent id is what stops the host from absorbing every member's
// consumption. Rows whose speaker no longer resolves (member left, session
// deleted) keep their current value.
func healLedgerAgent() error {
	res, err := store.WriteExec(`
		UPDATE chat_metadata
		SET agent_id = (
			SELECT ms.agent_id
			FROM chat_history h
			JOIN chat_sessions ms ON ms.id = h.agent_id
			WHERE h.id = chat_metadata.message_id
		)
		WHERE EXISTS (
			SELECT 1
			FROM chat_history h
			JOIN chat_sessions ms ON ms.id = h.agent_id
			WHERE h.id = chat_metadata.message_id
			  AND ms.agent_id <> ''
			  AND ms.agent_id <> chat_metadata.agent_id
		)`)
	if err != nil {
		return fmt.Errorf("re-resolve speaker attribution: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		slog.Info("usage ledger migration: re-resolved agent attribution", slog.Int64("rows", n))
	}
	return nil
}

// healLedgerTimestamps restores created_at for rows stamped by a late re-save
// (the content→ledger backfill rewrote old messages with the migration time).
//
// Two cases, both keyed on how far the ledger stamp sits AFTER the message:
//
//   - completed messages: any drift beyond an hour. A normal finalize writes the
//     ledger seconds after completed_at, so an hour already excludes every
//     legitimate case.
//   - cancelled turns (completed_at IS NULL): the ledger stamp is the finalize
//     time while the message row is created at turn START, so a long turn
//     legitimately drifts by hours. Only a drift beyond a DAY is treated as an
//     artifact — no interactive turn runs that long.
func healLedgerTimestamps() error {
	res, err := store.WriteExec(`
		UPDATE chat_metadata
		SET created_at = (
			SELECT h.completed_at FROM chat_history h WHERE h.id = chat_metadata.message_id
		)
		WHERE EXISTS (
			SELECT 1 FROM chat_history h
			WHERE h.id = chat_metadata.message_id
			  AND h.completed_at IS NOT NULL
			  AND h.completed_at <> ''
			  AND strftime('%s', chat_metadata.created_at) - strftime('%s', h.completed_at) > 3600
		)`)
	if err != nil {
		return fmt.Errorf("restore ledger timestamps: %w", err)
	}
	n, _ := res.RowsAffected()

	// Cancelled turns: no completion time to restore, so fall back to the
	// message's own creation time — but only when the ledger stamp is clearly
	// a backfill artifact (more than a day after the turn started).
	res, err = store.WriteExec(`
		UPDATE chat_metadata
		SET created_at = (
			SELECT h.created_at FROM chat_history h WHERE h.id = chat_metadata.message_id
		)
		WHERE EXISTS (
			SELECT 1 FROM chat_history h
			WHERE h.id = chat_metadata.message_id
			  AND (h.completed_at IS NULL OR h.completed_at = '')
			  AND strftime('%s', chat_metadata.created_at) - strftime('%s', h.created_at) > 86400
		)`)
	if err != nil {
		return fmt.Errorf("restore ledger timestamps for cancelled turns: %w", err)
	}
	if cancelled, _ := res.RowsAffected(); cancelled > 0 {
		n += cancelled
	}
	if n > 0 {
		slog.Info("usage ledger migration: restored created_at", slog.Int64("rows", n))
	}
	return nil
}

// MigrateUsageLedger is the exported entry point used by InitDB's runOnce.
//
// It refuses to run against a schema that is missing the columns it needs: a
// silently-skipped UPDATE would leave the statistics wrong forever, which is
// exactly the failure mode this migration exists to remove. A brand-new
// install (no rows) is recorded as applied without scanning.
func MigrateUsageLedger() bool {
	if err := ensureLedgerColumns(); err != nil {
		slog.Error("usage ledger migration: schema precondition failed", slog.String("err", err.Error()))
		return false
	}
	hasRows, err := ledgerRowExists()
	if err != nil {
		slog.Error("usage ledger migration: row probe failed", slog.String("err", err.Error()))
		return false
	}
	if !hasRows {
		return true
	}
	return healUsageLedger()
}

// ensureLedgerColumns fails loudly when a column the healing migration depends
// on is missing. A silently-skipped migration would leave the stats wrong
// forever, which is exactly the failure mode this change set exists to remove.
func ensureLedgerColumns() error {
	for _, col := range []string{"cost_session_total", "cost_cumulative", "total_tokens", "cache_hit_tokens"} {
		var n int
		if err := store.ReadDB().QueryRow(
			"SELECT COUNT(*) FROM pragma_table_info('chat_metadata') WHERE name = ?", col,
		).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("chat_metadata.%s is missing; column migration did not run", col)
		}
	}
	return nil
}

// ledgerRowExists reports whether the ledger has any row at all. A brand-new
// install has nothing to heal, so the migration records itself as applied
// without scanning.
func ledgerRowExists() (bool, error) {
	var n int
	err := store.ReadDB().QueryRow("SELECT COUNT(*) FROM chat_metadata").Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return n > 0, nil
}
