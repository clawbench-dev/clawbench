package service_test

import (
	"database/sql"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// insertLedgerSession registers a chat_sessions row with the given type/agent.
func insertLedgerSession(t *testing.T, db *sql.DB, id, sessionType, agentID string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, backend, title, agent_id, session_type)
		 VALUES (?, 1, 'codebuddy', 't', ?, ?)`,
		id, agentID, sessionType,
	)
	require.NoError(t, err)
}

// insertLedgerMessage inserts an assistant chat_history row and returns its id.
// agentID is chat_history.agent_id (the SPEAKER, i.e. a group member row id).
func insertLedgerMessage(t *testing.T, db *sql.DB, sessionID, agentID, createdAt, completedAt string) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO chat_history (project_id, backend, session_id, role, content, streaming, agent_id, created_at, completed_at)
		 VALUES (1, 'codebuddy', ?, 'assistant', '{}', 0, ?, ?, ?)`,
		sessionID, agentID, createdAt, completedAt,
	)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

// readLedgerCost returns (cost_usd, cost_session_total, cost_cumulative).
func readLedgerCost(t *testing.T, db *sql.DB, msgID int64) (float64, float64, int) {
	t.Helper()
	var cost, total float64
	var cumulative int
	require.NoError(t, db.QueryRow(
		"SELECT cost_usd, cost_session_total, cost_cumulative FROM chat_metadata WHERE message_id = ?", msgID,
	).Scan(&cost, &total, &cumulative))
	return cost, total, cumulative
}

// In a group chat every member has its own ACP connection and therefore its own
// cost counter, while all their messages share one timeline session. Diffing by
// timeline would compare one member's running total against another's, which
// produced nonsense deltas (and re-billed whole sessions whenever the counters
// happened to decrease).
func TestSaveMetadata_CumulativeCostPartitionedByAgentConnection(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "group-1", "group", "host-agent")
	insertLedgerSession(t, db, "member-a", "group_member", "agent-a")
	insertLedgerSession(t, db, "member-b", "group_member", "agent-b")

	// Member A's connection reports 0.50.
	a1 := insertLedgerMessage(t, db, "group-1", "member-a", "2026-01-10 10:00:00", "2026-01-10 10:01:00")
	require.NoError(t, service.SaveMetadata(a1, &ai.Metadata{
		SessionID: "acp-conn-a", CostUSD: 0.50, CostCumulative: true, CostSessionTotal: 0.50,
	}))
	// Member B's connection reports 0.80 — a DIFFERENT counter on the same
	// timeline. It must bill its whole value, not 0.30.
	b1 := insertLedgerMessage(t, db, "group-1", "member-b", "2026-01-10 10:02:00", "2026-01-10 10:03:00")
	require.NoError(t, service.SaveMetadata(b1, &ai.Metadata{
		SessionID: "acp-conn-b", CostUSD: 0.80, CostCumulative: true, CostSessionTotal: 0.80,
	}))
	// Member A's next turn rises to 0.70 → 0.20, unaffected by B's higher total.
	a2 := insertLedgerMessage(t, db, "group-1", "member-a", "2026-01-10 10:04:00", "2026-01-10 10:05:00")
	require.NoError(t, service.SaveMetadata(a2, &ai.Metadata{
		SessionID: "acp-conn-a", CostUSD: 0.70, CostCumulative: true, CostSessionTotal: 0.70,
	}))

	costA1, _, _ := readLedgerCost(t, db, a1)
	costB1, _, _ := readLedgerCost(t, db, b1)
	costA2, _, _ := readLedgerCost(t, db, a2)
	assert.InDelta(t, 0.50, costA1, 1e-9)
	assert.InDelta(t, 0.80, costB1, 1e-9, "another member's counter is not this member's baseline")
	assert.InDelta(t, 0.20, costA2, 1e-9)

	var sum float64
	require.NoError(t, db.QueryRow("SELECT SUM(cost_usd) FROM chat_metadata WHERE clawbench_session_id = 'group-1'").Scan(&sum))
	assert.InDelta(t, 1.50, sum, 1e-9, "the group total is the sum of both members' final counters")
}

// ACP interleaves cost-less turns (a naked usage_update leaves cost 0). Diffing
// against one of those would bill the whole running total again, so the baseline
// must be the last turn that actually RECORDED a cumulative value.
func TestSaveMetadata_CumulativeCostSkipsCostlessTurns(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-gap", "chat", "claude")

	first := insertLedgerMessage(t, db, "sess-gap", "", "2026-01-10 10:00:00", "2026-01-10 10:01:00")
	require.NoError(t, service.SaveMetadata(first, &ai.Metadata{CostUSD: 0.40, CostCumulative: true, CostSessionTotal: 0.40}))

	// A turn that reported no cost at all (cost stays 0, no cumulative flag).
	gap := insertLedgerMessage(t, db, "sess-gap", "", "2026-01-10 10:03:00", "2026-01-10 10:04:00")
	require.NoError(t, service.SaveMetadata(gap, &ai.Metadata{InputTokens: 10}))
	cost, _, cumulative := readLedgerCost(t, db, gap)
	assert.InDelta(t, 0, cost, 1e-9)
	assert.Equal(t, 0, cumulative)

	// The next informative turn's total already includes the skipped turn's
	// consumption, so diffing against 0.40 captures it exactly once.
	next := insertLedgerMessage(t, db, "sess-gap", "", "2026-01-10 10:06:00", "2026-01-10 10:07:00")
	require.NoError(t, service.SaveMetadata(next, &ai.Metadata{CostUSD: 0.90, CostCumulative: true, CostSessionTotal: 0.90}))
	cost, _, _ = readLedgerCost(t, db, next)
	assert.InDelta(t, 0.50, cost, 1e-9, "the diff spans the skipped turn, counted once")

	var sum float64
	require.NoError(t, db.QueryRow("SELECT SUM(cost_usd) FROM chat_metadata WHERE clawbench_session_id = 'sess-gap'").Scan(&sum))
	assert.InDelta(t, 0.90, sum, 1e-9)
}

// normalizeLedgerCost runs TWICE per turn: once in the executor (so the
// embedded content JSON carries this turn's cost) and once in SaveMetadata. The
// second call must produce the same delta — the baseline query excludes the
// message's own row, so a pre-write and a post-write call agree.
func TestSaveMetadata_CostNormalizationIsIdempotent(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-idem", "chat", "claude")

	first := insertLedgerMessage(t, db, "sess-idem", "", "2026-01-10 10:00:00", "2026-01-10 10:01:00")
	require.NoError(t, service.SaveMetadata(first, &ai.Metadata{CostUSD: 0.40, CostCumulative: true, CostSessionTotal: 0.40}))

	second := insertLedgerMessage(t, db, "sess-idem", "", "2026-01-10 10:05:00", "2026-01-10 10:06:00")
	meta := &ai.Metadata{CostUSD: 0.90, CostCumulative: true, CostSessionTotal: 0.90}
	require.NoError(t, service.SaveMetadata(second, meta))
	firstWrite, _, _ := readLedgerCost(t, db, second)

	// A second save (the executor's pre-write path runs before this) must not
	// move the value: the row is excluded from its own baseline.
	require.NoError(t, service.SaveMetadata(second, meta))
	secondWrite, total, _ := readLedgerCost(t, db, second)
	assert.InDelta(t, firstWrite, secondWrite, 1e-9, "re-saving must not re-diff the same row")
	assert.InDelta(t, 0.90, total, 1e-9)
}

// ACP reports usage_update.cost as the SESSION CUMULATIVE total. Storing it
// verbatim made SUM(cost_usd) count the running total once per message — the
// measured 2.8× inflation on Claude. The ledger must store per-turn deltas while
// keeping the raw total for the next turn's diff.
func TestSaveMetadata_CumulativeCostStoredAsDelta(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-cost", "chat", "claude")

	first := insertLedgerMessage(t, db, "sess-cost", "", "2026-01-10 10:00:00", "2026-01-10 10:01:00")
	require.NoError(t, service.SaveMetadata(first, &ai.Metadata{
		Transport: "acp-stdio", CostUSD: 0.40, CostCumulative: true, CostSessionTotal: 0.40,
	}))
	cost, total, cumulative := readLedgerCost(t, db, first)
	assert.InDelta(t, 0.40, cost, 1e-9, "first turn bills its whole cumulative value")
	assert.InDelta(t, 0.40, total, 1e-9)
	assert.Equal(t, 1, cumulative)

	second := insertLedgerMessage(t, db, "sess-cost", "", "2026-01-10 10:05:00", "2026-01-10 10:06:00")
	require.NoError(t, service.SaveMetadata(second, &ai.Metadata{
		Transport: "acp-stdio", CostUSD: 0.90, CostCumulative: true, CostSessionTotal: 0.90,
	}))
	cost, total, cumulative = readLedgerCost(t, db, second)
	assert.InDelta(t, 0.50, cost, 1e-9, "second turn bills only the increment")
	assert.InDelta(t, 0.90, total, 1e-9)
	assert.Equal(t, 1, cumulative)

	var sum float64
	require.NoError(t, db.QueryRow("SELECT SUM(cost_usd) FROM chat_metadata WHERE clawbench_session_id = 'sess-cost'").Scan(&sum))
	assert.InDelta(t, 0.90, sum, 1e-9, "SUM over the ledger equals the session's final cumulative cost")
}

// An agent can restart its cost counter mid-session (a model switch does). The
// baseline must follow the new counter, otherwise every later turn diffs against
// a value that can never be reached again and re-counts its whole amount.
func TestSaveMetadata_CumulativeCostCounterReset(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-reset", "chat", "claude")

	first := insertLedgerMessage(t, db, "sess-reset", "", "2026-01-10 10:00:00", "2026-01-10 10:01:00")
	require.NoError(t, service.SaveMetadata(first, &ai.Metadata{CostUSD: 0.60, CostCumulative: true, CostSessionTotal: 0.60}))

	// New epoch: the reported total drops below the previous one.
	second := insertLedgerMessage(t, db, "sess-reset", "", "2026-01-10 10:05:00", "2026-01-10 10:06:00")
	require.NoError(t, service.SaveMetadata(second, &ai.Metadata{CostUSD: 0.20, CostCumulative: true, CostSessionTotal: 0.20}))
	cost, _, _ := readLedgerCost(t, db, second)
	assert.InDelta(t, 0.20, cost, 1e-9, "a reset bills the whole reported amount")

	third := insertLedgerMessage(t, db, "sess-reset", "", "2026-01-10 10:10:00", "2026-01-10 10:11:00")
	require.NoError(t, service.SaveMetadata(third, &ai.Metadata{CostUSD: 0.35, CostCumulative: true, CostSessionTotal: 0.35}))
	cost, _, _ = readLedgerCost(t, db, third)
	assert.InDelta(t, 0.15, cost, 1e-9, "the post-reset baseline is the reset value, not the old maximum")
}

// A per-turn cost (CLI backends) must be stored verbatim.
func TestSaveMetadata_PerTurnCostUntouched(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-cli", "chat", "pi")

	first := insertLedgerMessage(t, db, "sess-cli", "", "2026-01-10 10:00:00", "2026-01-10 10:01:00")
	require.NoError(t, service.SaveMetadata(first, &ai.Metadata{CostUSD: 0.002}))
	second := insertLedgerMessage(t, db, "sess-cli", "", "2026-01-10 10:05:00", "2026-01-10 10:06:00")
	require.NoError(t, service.SaveMetadata(second, &ai.Metadata{CostUSD: 0.003}))

	cost, _, cumulative := readLedgerCost(t, db, second)
	assert.InDelta(t, 0.003, cost, 1e-9)
	assert.Equal(t, 0, cumulative, "per-turn costs are not flagged cumulative")
}

// Parsers that never reported an upstream total left total_tokens = 0, which the
// stats panel's default (total) metric rendered as "no usage at all".
func TestSaveMetadata_TotalTokensInvariant(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-total", "chat", "opencode")
	msgID := insertLedgerMessage(t, db, "sess-total", "", "2026-01-10 10:00:00", "2026-01-10 10:01:00")

	require.NoError(t, service.SaveMetadata(msgID, &ai.Metadata{
		InputTokens: 300, OutputTokens: 40, TotalTokens: 0,
	}))
	var total int
	require.NoError(t, db.QueryRow("SELECT total_tokens FROM chat_metadata WHERE message_id = ?", msgID).Scan(&total))
	assert.Equal(t, 340, total)

	// A total below input+output (an older snapshot) is raised, not trusted.
	msgID2 := insertLedgerMessage(t, db, "sess-total", "", "2026-01-10 10:05:00", "2026-01-10 10:06:00")
	require.NoError(t, service.SaveMetadata(msgID2, &ai.Metadata{
		InputTokens: 500, OutputTokens: 60, TotalTokens: 400,
	}))
	require.NoError(t, db.QueryRow("SELECT total_tokens FROM chat_metadata WHERE message_id = ?", msgID2).Scan(&total))
	assert.Equal(t, 560, total)
}

// Claude/Codex often leave the request model empty but do report which model
// actually responded. Without the fallback those rows collapse into "(empty)".
func TestSaveMetadata_ModelFallsBackToResponseModel(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-model", "chat", "claude")
	msgID := insertLedgerMessage(t, db, "sess-model", "", "2026-01-10 10:00:00", "2026-01-10 10:01:00")

	require.NoError(t, service.SaveMetadata(msgID, &ai.Metadata{
		Model: "", ResponseModelID: "claude-sonnet-4-6", InputTokens: 10,
	}))
	var model string
	require.NoError(t, db.QueryRow("SELECT model FROM chat_metadata WHERE message_id = ?", msgID).Scan(&model))
	assert.Equal(t, "claude-sonnet-4-6", model)

	// An explicit request model always wins.
	msgID2 := insertLedgerMessage(t, db, "sess-model", "", "2026-01-10 10:05:00", "2026-01-10 10:06:00")
	require.NoError(t, service.SaveMetadata(msgID2, &ai.Metadata{
		Model: "claude-opus-4-6", ResponseModelID: "claude-sonnet-4-6", InputTokens: 10,
	}))
	require.NoError(t, db.QueryRow("SELECT model FROM chat_metadata WHERE message_id = ?", msgID2).Scan(&model))
	assert.Equal(t, "claude-opus-4-6", model)
}

// A group-chat member turn writes the GROUP session id to chat_history but the
// member's session ROW id to chat_history.agent_id. Reading the session's agent
// credited every member's consumption to the host (25% of the tokens in a
// production DB).
func TestSaveMetadata_GroupSpeakerAttribution(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "group-1", "group", "host-agent")
	insertLedgerSession(t, db, "member-row-1", "group_member", "member-agent")

	msgID := insertLedgerMessage(t, db, "group-1", "member-row-1", "2026-01-10 10:00:00", "2026-01-10 10:01:00")
	require.NoError(t, service.SaveMetadata(msgID, &ai.Metadata{InputTokens: 100}))

	var agentID, sessionID string
	require.NoError(t, db.QueryRow(
		"SELECT agent_id, clawbench_session_id FROM chat_metadata WHERE message_id = ?", msgID,
	).Scan(&agentID, &sessionID))
	assert.Equal(t, "member-agent", agentID, "the SPEAKER's agent owns the usage, not the host")
	assert.Equal(t, "group-1", sessionID, "the ledger still records the timeline session")

	// A single-agent turn (empty speaker) keeps the session's agent.
	solo := insertLedgerMessage(t, db, "group-1", "", "2026-01-10 10:05:00", "2026-01-10 10:06:00")
	require.NoError(t, service.SaveMetadata(solo, &ai.Metadata{InputTokens: 10}))
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_metadata WHERE message_id = ?", solo).Scan(&agentID))
	assert.Equal(t, "host-agent", agentID)

	// A deleted member row must not leave a dangling row id in the ledger.
	orphan := insertLedgerMessage(t, db, "group-1", "deleted-member", "2026-01-10 10:10:00", "2026-01-10 10:11:00")
	require.NoError(t, service.SaveMetadata(orphan, &ai.Metadata{InputTokens: 10}))
	require.NoError(t, db.QueryRow("SELECT agent_id FROM chat_metadata WHERE message_id = ?", orphan).Scan(&agentID))
	assert.Equal(t, "host-agent", agentID)
}

// ledgerCreatedAt reads the row's created_at as a time. The column is declared
// DATETIME, so the driver hands it back as a parsed time rather than the raw
// text the writer stored.
func ledgerCreatedAt(t *testing.T, db *sql.DB, msgID int64) time.Time {
	t.Helper()
	var raw any
	require.NoError(t, db.QueryRow("SELECT created_at FROM chat_metadata WHERE message_id = ?", msgID).Scan(&raw))
	switch v := raw.(type) {
	case time.Time:
		return v
	case string:
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
			if ts, err := time.Parse(layout, v); err == nil {
				return ts
			}
		}
		t.Fatalf("unparseable created_at %q", v)
	}
	t.Fatalf("unexpected created_at type %T", raw)
	return time.Time{}
}

// INSERT OR REPLACE used to re-stamp created_at on every re-save, so the
// content→ledger backfill moved month-old messages into today's stats window.
func TestSaveMetadata_CreatedAtFollowsMessageCompletion(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-time", "chat", "codebuddy")

	msgID := insertLedgerMessage(t, db, "sess-time", "", "2026-01-10 09:00:00", "2026-01-10 09:05:00")
	require.NoError(t, service.SaveMetadata(msgID, &ai.Metadata{InputTokens: 10}))

	want := time.Date(2026, 1, 10, 9, 5, 0, 0, time.UTC)
	assert.True(t, ledgerCreatedAt(t, db, msgID).Equal(want),
		"the ledger is stamped with the message's own completion time, got %v", ledgerCreatedAt(t, db, msgID))

	// A re-save must not move it.
	time.Sleep(1100 * time.Millisecond)
	require.NoError(t, service.SaveMetadata(msgID, &ai.Metadata{InputTokens: 20}))
	assert.True(t, ledgerCreatedAt(t, db, msgID).Equal(want), "a re-save must not re-stamp created_at")
}

// A cancelled turn has no completed_at; the ledger must still get a timestamp
// rather than an empty string (which would sort before every window).
func TestSaveMetadata_CreatedAtFallsBackToNowWhenUncompleted(t *testing.T) {
	db := setupDB(t)
	insertLedgerSession(t, db, "sess-cancel", "chat", "codebuddy")
	msgID := insertLedgerMessage(t, db, "sess-cancel", "", "2026-01-10 09:00:00", "")

	require.NoError(t, service.SaveMetadata(msgID, &ai.Metadata{InputTokens: 10}))
	var createdAt string
	require.NoError(t, db.QueryRow("SELECT created_at FROM chat_metadata WHERE message_id = ?", msgID).Scan(&createdAt))
	assert.NotEmpty(t, createdAt)
}
