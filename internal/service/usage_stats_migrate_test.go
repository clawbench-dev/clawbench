package service

import (
	"database/sql"
	"strconv"
	"testing"
	"time"

	"clawbench/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupLedgerHealDB builds a minimal schema with the CURRENT chat_metadata
// columns (including the two cost bookkeeping ones) so the healing migration can
// be exercised directly.
func setupLedgerHealDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
		CREATE TABLE chat_sessions (
			id TEXT PRIMARY KEY,
			agent_id TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			context_state TEXT DEFAULT ''
		);
		CREATE TABLE chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL DEFAULT 1,
			role TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '{}',
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			agent_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		);
		CREATE TABLE chat_metadata (
			message_id INTEGER PRIMARY KEY,
			transport TEXT DEFAULT '',
			model TEXT DEFAULT '',
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0,
			cached_read_tokens INTEGER DEFAULT 0,
			cached_write_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			cache_hit_tokens INTEGER DEFAULT 0,
			cache_miss_tokens INTEGER DEFAULT 0,
			response_model_id TEXT DEFAULT '',
			backend TEXT DEFAULT '',
			agent_id TEXT DEFAULT '',
			clawbench_session_id TEXT DEFAULT '',
			session_id TEXT DEFAULT '',
			cost_session_total REAL DEFAULT 0,
			cost_cumulative INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`)
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	return db
}

func insertHealRow(t *testing.T, db *sql.DB, msgID int64, sessionID, backend, transport string, cost float64, createdAt string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO chat_metadata (message_id, backend, transport, cost_usd, clawbench_session_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		msgID, backend, transport, cost, sessionID, createdAt,
	)
	require.NoError(t, err)
	// The message row carries the same cost in its embedded metadata; the
	// migration must rewrite both so the detail modal agrees with the stats.
	_, err = db.Exec(
		`INSERT INTO chat_history (id, role, session_id, content) VALUES (?, 'assistant', ?, ?)`,
		msgID, sessionID, `{"blocks":[],"metadata":{"costUsd":`+formatFloat(cost)+`}}`,
	)
	require.NoError(t, err)
}

// formatFloat renders a float without exponent notation, for building the
// embedded content JSON in fixtures.
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ACP's cumulative cost was stored per message, so SUM(cost_usd) counted the
// running total once per message. The migration must re-base every row onto the
// per-turn delta while preserving the raw total.
func TestHealLedgerCost_RebasesCumulativeRows(t *testing.T) {
	db := setupLedgerHealDB(t)
	// Monotonically rising: 0.20 → 0.50 → 0.90 (the observed Claude shape).
	insertHealRow(t, db, 1, "s1", "claude", "acp-stdio", 0.20, "2026-01-10 10:00:00")
	insertHealRow(t, db, 2, "s1", "claude", "acp-stdio", 0.50, "2026-01-10 10:05:00")
	insertHealRow(t, db, 3, "s1", "claude", "acp-stdio", 0.90, "2026-01-10 10:10:00")
	// A separate session starts its own counter.
	insertHealRow(t, db, 4, "s2", "claude", "acp-stdio", 0.30, "2026-01-10 11:00:00")
	// Per-turn cost (pi) must be left alone.
	insertHealRow(t, db, 5, "s3", "pi", "cli", 0.004, "2026-01-10 12:00:00")

	require.NoError(t, healLedgerCost())

	got := func(id int64) (float64, float64, int) {
		var cost, total float64
		var cumulative int
		require.NoError(t, db.QueryRow(
			"SELECT cost_usd, cost_session_total, cost_cumulative FROM chat_metadata WHERE message_id = ?", id,
		).Scan(&cost, &total, &cumulative))
		return cost, total, cumulative
	}
	cost, total, cumulative := got(1)
	assert.InDelta(t, 0.20, cost, 1e-9)
	assert.InDelta(t, 0.20, total, 1e-9)
	assert.Equal(t, 1, cumulative)

	cost, total, cumulative = got(2)
	assert.InDelta(t, 0.30, cost, 1e-9, "second turn bills only the increment")
	assert.InDelta(t, 0.50, total, 1e-9)
	assert.Equal(t, 1, cumulative)

	cost, _, _ = got(3)
	assert.InDelta(t, 0.40, cost, 1e-9)

	cost, _, _ = got(4)
	assert.InDelta(t, 0.30, cost, 1e-9, "a new session starts from zero")

	cost, _, cumulative = got(5)
	assert.InDelta(t, 0.004, cost, 1e-9, "per-turn costs are untouched")
	assert.Equal(t, 0, cumulative)

	var sum float64
	require.NoError(t, db.QueryRow("SELECT SUM(cost_usd) FROM chat_metadata WHERE clawbench_session_id = 's1'").Scan(&sum))
	assert.InDelta(t, 0.90, sum, 1e-9, "SUM now equals the session's final cumulative cost")

	// The embedded content JSON must carry the same per-turn value: the detail
	// modal reads that copy, and a stale running total there would claim one
	// reply cost the whole session.
	var content string
	require.NoError(t, db.QueryRow("SELECT content FROM chat_history WHERE id = 3").Scan(&content))
	assert.Contains(t, content, `"costUsd":0.4`, "content cost is rewritten to the delta")
	assert.NotContains(t, content, "costSessionTotal", "ledger bookkeeping must not leak into the message view")
}

// A counter reset (model switch) must re-baseline: otherwise every later turn
// diffs against a value that can never be reached again and re-counts its whole
// amount.
func TestHealLedgerCost_RebaselinesAfterCounterReset(t *testing.T) {
	db := setupLedgerHealDB(t)
	insertHealRow(t, db, 1, "s1", "claude", "acp-stdio", 0.60, "2026-01-10 10:00:00")
	insertHealRow(t, db, 2, "s1", "claude", "acp-stdio", 0.20, "2026-01-10 10:05:00") // reset
	insertHealRow(t, db, 3, "s1", "claude", "acp-stdio", 0.35, "2026-01-10 10:10:00")

	require.NoError(t, healLedgerCost())

	var sum float64
	require.NoError(t, db.QueryRow("SELECT SUM(cost_usd) FROM chat_metadata").Scan(&sum))
	// 0.60 + 0.20 (whole reset value) + 0.15 (post-reset increment)
	assert.InDelta(t, 0.95, sum, 1e-9)
}

// A partially-upgraded schema must fail loudly instead of silently matching
// nothing (which would leave the statistics wrong forever).
func TestMigrateUsageLedger_FailsWhenColumnMissing(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec("CREATE TABLE chat_metadata (message_id INTEGER PRIMARY KEY, cost_usd REAL DEFAULT 0)")
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})

	assert.False(t, MigrateUsageLedger(), "missing bookkeeping columns must abort the migration")
}

// An empty ledger is recorded as applied without scanning.
func TestMigrateUsageLedger_EmptyLedgerIsNoOp(t *testing.T) {
	setupLedgerHealDB(t)
	assert.True(t, MigrateUsageLedger())
}

// The healing migration as a whole must be idempotent: running it twice leaves
// the same values (a retry after a partial failure is the normal path).
func TestMigrateUsageLedger_Idempotent(t *testing.T) {
	db := setupLedgerHealDB(t)
	insertHealRow(t, db, 1, "s1", "claude", "acp-stdio", 0.20, "2026-01-10 10:00:00")
	insertHealRow(t, db, 2, "s1", "claude", "acp-stdio", 0.50, "2026-01-10 10:05:00")

	require.True(t, MigrateUsageLedger())
	var first float64
	require.NoError(t, db.QueryRow("SELECT SUM(cost_usd) FROM chat_metadata").Scan(&first))
	assert.InDelta(t, 0.50, first, 1e-9)

	require.True(t, MigrateUsageLedger())
	var second float64
	require.NoError(t, db.QueryRow("SELECT SUM(cost_usd) FROM chat_metadata").Scan(&second))
	assert.InDelta(t, first, second, 1e-9, "a second run must not re-diff the already-delta'd rows")
}

// The pre-ACP CodeBuddy CLI parser stored the SESSION-CUMULATIVE prompt size in
// input_tokens: one measured session climbs 73k → 11.5M → 31.0M across ten
// turns while its window is 200k. Summing them inflated the total by 74%.
func TestHealLedgerLegacyCumulativeInput_NormalizesToDeltas(t *testing.T) {
	db := setupLedgerHealDB(t)
	rows := []struct {
		id     int64
		input  int64
		output int64
	}{
		{1, 73525, 538},
		{2, 11521209, 32067},
		{3, 14304925, 34239},
		{4, 10682530, 27461}, // counter reset
		{5, 16495148, 38262},
	}
	for _, r := range rows {
		_, err := db.Exec(`INSERT INTO chat_metadata
			(message_id, backend, clawbench_session_id, input_tokens, output_tokens, total_tokens)
			VALUES (?, 'codebuddy', 's1', ?, ?, 0)`, r.id, r.input, r.output)
		require.NoError(t, err)
	}

	require.NoError(t, healLedgerLegacyCumulativeInput())

	got := func(id int64) int64 {
		var v int64
		require.NoError(t, db.QueryRow("SELECT input_tokens FROM chat_metadata WHERE message_id = ?", id).Scan(&v))
		return v
	}
	assert.Equal(t, int64(73525), got(1), "the first turn bills its whole value")
	assert.Equal(t, int64(11521209-73525), got(2))
	assert.Equal(t, int64(14304925-11521209), got(3))
	assert.Equal(t, int64(10682530), got(4), "a counter reset bills the whole value")
	assert.Equal(t, int64(16495148-10682530), got(5))

	var sum int64
	require.NoError(t, db.QueryRow("SELECT SUM(input_tokens) FROM chat_metadata").Scan(&sum))
	// Two counter epochs (a reset at row 4), so the sum is the sum of each
	// epoch's final value — not the sum of five cumulative snapshots.
	assert.Equal(t, int64(14304925+16495148), sum)
}

// Rows with no total (parsers that dropped the upstream value) and an empty
// model must be repaired.
func TestHealLedgerTokens_FillsTotalAndModel(t *testing.T) {
	db := setupLedgerHealDB(t)
	_, err := db.Exec(`INSERT INTO chat_metadata (message_id, input_tokens, output_tokens, total_tokens, model, response_model_id)
		VALUES (1, 300, 40, 0, '', 'claude-sonnet-4-6')`)
	require.NoError(t, err)
	// A total below input+output is raised, not trusted.
	_, err = db.Exec(`INSERT INTO chat_metadata (message_id, input_tokens, output_tokens, total_tokens, model, response_model_id)
		VALUES (2, 500, 60, 400, 'explicit', 'claude-sonnet-4-6')`)
	require.NoError(t, err)

	require.NoError(t, healLedgerTokens())

	var total int
	var model string
	require.NoError(t, db.QueryRow("SELECT total_tokens, model FROM chat_metadata WHERE message_id = 1").Scan(&total, &model))
	assert.Equal(t, 340, total)
	assert.Equal(t, "claude-sonnet-4-6", model)

	require.NoError(t, db.QueryRow("SELECT total_tokens, model FROM chat_metadata WHERE message_id = 2").Scan(&total, &model))
	assert.Equal(t, 560, total)
	assert.Equal(t, "explicit", model, "an explicit request model always wins")
}

// Claude/Codex report cache counters outside inputTokens, so their cache split
// stayed 0 and the panel's hit rate described CodeBuddy alone.
func TestHealLedgerCacheSplit_NormalizesClaudeShape(t *testing.T) {
	db := setupLedgerHealDB(t)
	// total = input + output + cacheRead + cacheWrite (the old, un-normalized
	// shape) → 100 + 10 + 900 + 0 = 1010.
	_, err := db.Exec(`INSERT INTO chat_metadata
		(message_id, backend, input_tokens, output_tokens, cached_read_tokens, cached_write_tokens, total_tokens)
		VALUES (1, 'claude', 100, 10, 900, 0, 1010)`)
	require.NoError(t, err)
	// Codex with a cached write → also normalized.
	_, err = db.Exec(`INSERT INTO chat_metadata
		(message_id, backend, input_tokens, output_tokens, cached_read_tokens, cached_write_tokens, total_tokens)
		VALUES (2, 'codex', 200, 20, 50, 30, 300)`)
	require.NoError(t, err)
	// A row with no aggregate total is left alone (nothing to verify against).
	_, err = db.Exec(`INSERT INTO chat_metadata
		(message_id, backend, input_tokens, output_tokens, cached_read_tokens, total_tokens)
		VALUES (3, 'claude', 10, 2, 100, 0)`)
	require.NoError(t, err)

	require.NoError(t, healLedgerCacheSplit())

	var input, hit, miss, total int
	require.NoError(t, db.QueryRow(
		"SELECT input_tokens, cache_hit_tokens, cache_miss_tokens, total_tokens FROM chat_metadata WHERE message_id = 1",
	).Scan(&input, &hit, &miss, &total))
	assert.Equal(t, 1000, input, "input is normalized to the full prompt")
	assert.Equal(t, 900, hit)
	assert.Equal(t, 100, miss)
	assert.Equal(t, 1010, total, "total is unchanged")

	require.NoError(t, db.QueryRow(
		"SELECT input_tokens, cache_hit_tokens, cache_miss_tokens FROM chat_metadata WHERE message_id = 2",
	).Scan(&input, &hit, &miss))
	assert.Equal(t, 280, input)
	assert.Equal(t, 50, hit)
	assert.Equal(t, 200, miss)

	require.NoError(t, db.QueryRow(
		"SELECT input_tokens, cache_hit_tokens FROM chat_metadata WHERE message_id = 3",
	).Scan(&input, &hit))
	assert.Equal(t, 10, input, "an unverifiable row keeps its raw value")
	assert.Equal(t, 0, hit)
}

// Group-chat member turns were attributed to the host (the session's agent).
func TestHealLedgerAgent_ResolvesSpeaker(t *testing.T) {
	db := setupLedgerHealDB(t)
	_, err := db.Exec(`INSERT INTO chat_sessions (id, agent_id, session_type) VALUES
		('group-1', 'host-agent', 'group'),
		('member-row-1', 'member-agent', 'group_member')`)
	require.NoError(t, err)
	// Message 1: group timeline, speaker = member row.
	_, err = db.Exec(`INSERT INTO chat_history (id, role, session_id, agent_id) VALUES
		(1, 'assistant', 'group-1', 'member-row-1'),
		(2, 'assistant', 'group-1', '')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO chat_metadata (message_id, agent_id, clawbench_session_id) VALUES
		(1, 'host-agent', 'group-1'),
		(2, 'host-agent', 'group-1')`)
	require.NoError(t, err)

	require.NoError(t, healLedgerAgent())

	var agent string
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_metadata WHERE message_id = 1").Scan(&agent))
	assert.Equal(t, "member-agent", agent, "the speaker owns the usage")
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_metadata WHERE message_id = 2").Scan(&agent))
	assert.Equal(t, "host-agent", agent, "a single-agent turn keeps the session agent")
}

// A late re-save (notably the content→ledger backfill) used to stamp old
// messages with the migration time, moving them into the current stats window.
func TestHealLedgerTimestamps_RestoresCompletionTime(t *testing.T) {
	db := setupLedgerHealDB(t)
	_, err := db.Exec(`INSERT INTO chat_history (id, role, session_id, created_at, completed_at) VALUES
		(1, 'assistant', 's1', '2026-01-10 09:00:00', '2026-01-10 09:05:00'),
		(2, 'assistant', 's1', '2026-01-10 09:00:00', '2026-01-10 09:05:00'),
		(3, 'assistant', 's1', '2026-01-10 09:00:00', NULL),
		(4, 'assistant', 's1', '2026-01-10 09:00:00', NULL),
		(5, 'assistant', 's1', '2026-01-10 09:00:00', NULL)`)
	require.NoError(t, err)
	// Row 1 was re-stamped a month late; row 2 is a normal finalize (seconds
	// after completion). Row 3 is a cancelled turn whose ledger stamp is a few
	// hours after the turn STARTED (legitimate: the turn ran that long); row 4
	// is the same but re-stamped a month late; row 5 is a normal cancelled turn.
	_, err = db.Exec(`INSERT INTO chat_metadata (message_id, created_at) VALUES
		(1, '2026-02-10 09:05:00'),
		(2, '2026-01-10 09:05:02'),
		(3, '2026-01-10 13:00:00'),
		(4, '2026-02-10 09:05:00'),
		(5, '2026-01-10 09:30:00')`)
	require.NoError(t, err)

	require.NoError(t, healLedgerTimestamps())

	// created_at is a DATETIME column, so the driver returns a parsed time.
	got := func(id int64) time.Time {
		var raw any
		require.NoError(t, db.QueryRow("SELECT created_at FROM chat_metadata WHERE message_id = ?", id).Scan(&raw))
		if ts, ok := raw.(time.Time); ok {
			return ts
		}
		text, _ := raw.(string)
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
			if ts, err := time.Parse(layout, text); err == nil {
				return ts
			}
		}
		t.Fatalf("unparseable created_at %v", raw)
		return time.Time{}
	}
	assert.True(t, got(1).Equal(time.Date(2026, 1, 10, 9, 5, 0, 0, time.UTC)),
		"the late re-stamp is restored, got %v", got(1))
	assert.True(t, got(2).Equal(time.Date(2026, 1, 10, 9, 5, 2, 0, time.UTC)),
		"a normal finalize is not rewritten, got %v", got(2))
	assert.True(t, got(3).Equal(time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC)),
		"a long cancelled turn keeps its finalize stamp, got %v", got(3))
	assert.True(t, got(4).Equal(time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)),
		"a cancelled turn re-stamped a month late falls back to the turn start, got %v", got(4))
	assert.True(t, got(5).Equal(time.Date(2026, 1, 10, 9, 30, 0, 0, time.UTC)),
		"a normal cancelled turn is untouched, got %v", got(5))
}
