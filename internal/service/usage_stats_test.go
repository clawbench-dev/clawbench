package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ensureAgentsTable creates the agents table in the in-memory test DB (the
// shared service schema const does not include it).
func ensureAgentsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			backend TEXT NOT NULL,
			models TEXT NOT NULL DEFAULT '[]',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	assert.NoError(t, err)
}

// usageSeed describes one chat_history + chat_metadata + (optional) session/agent row.
type usageSeed struct {
	project   string
	sessionID string // must equal chat_sessions.id
	agentID   string
	agentName string // "" → no agents row (falls back to agent_id)
	backend   string
	model     string
	createdAt string // "2006-01-02 15:04:05" UTC text
	input     int64
	output    int64
	total     int64
	cacheHit  int64
	cacheMiss int64
	credit    float64
	costUSD   float64
}

func insertUsageSeed(t *testing.T, db *sql.DB, s usageSeed) {
	t.Helper()

	// Agents table: insert the agent once per unique (id).
	if s.agentName != "" {
		var name string
		err := db.QueryRow("SELECT name FROM agents WHERE id = ?", s.agentID).Scan(&name)
		if err == sql.ErrNoRows {
			if _, err := db.Exec("INSERT INTO agents (id, name, backend) VALUES (?, ?, ?)", s.agentID, s.agentName, s.backend); err != nil {
				t.Fatalf("insert agent: %v", err)
			}
		}
	}

	// Chat session: insert only if not present.
	var sid string
	err := db.QueryRow("SELECT id FROM chat_sessions WHERE id = ?", s.sessionID).Scan(&sid)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.Exec(
			"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES (?, ?, ?, 't', ?)",
			s.sessionID, s.project, s.backend, s.agentID,
		); err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}

	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES (?, ?, ?, 'assistant', '{}', 0, ?)",
		s.project, s.backend, s.sessionID, s.createdAt,
	)
	if err != nil {
		t.Fatalf("insert history: %v", err)
	}
	msgID, _ := res.LastInsertId()
	// The ledger carries denormalized attribution (project_path/backend/
	// agent_id/clawbench_session_id) so stats work after the session is deleted.
	if _, err := db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, output_tokens, total_tokens,
			cache_hit_tokens, cache_miss_tokens, credit, cost_usd, created_at,
			project_path, backend, agent_id, clawbench_session_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msgID, s.model, s.input, s.output, s.total, s.cacheHit, s.cacheMiss, s.credit, s.costUSD, s.createdAt,
		s.project, s.backend, s.agentID, s.sessionID,
	); err != nil {
		t.Fatalf("insert metadata: %v", err)
	}
}

func usageParams(p service.UsageParams) service.UsageParams {
	if p.Start.IsZero() {
		p.Start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if p.End.IsZero() {
		p.End = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	}
	if p.SortBy == "" {
		p.SortBy = service.MetricTotal
	}
	return p
}

func TestUsageStatsTotalsAndProjectIsolation(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// Project A: two rows in window, one row outside the window.
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-a1", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 10:00:00",
		input: 100, output: 50, total: 150, cacheHit: 40, cacheMiss: 10, credit: 1.5, costUSD: 0.02,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-a2", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "deepseek", createdAt: "2026-01-11 10:00:00",
		input: 200, output: 100, total: 300, cacheHit: 100, cacheMiss: 50, credit: 3.0, costUSD: 0.05,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-a3", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2025-12-01 10:00:00", // outside window
		input: 999, output: 999, total: 1998,
	})
	// Project B: must be excluded by project filter.
	insertUsageSeed(t, db, usageSeed{
		project: "/b", sessionID: "s-b1", agentID: "other", agentName: "Other",
		backend: "claude", model: "opus", createdAt: "2026-01-10 10:00:00",
		input: 5000, output: 5000, total: 10000,
	})

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/a",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)

	require.NotNil(t, res.Totals)
	assert.Equal(t, int64(300), res.Totals.Input, "project B must be excluded")
	assert.Equal(t, int64(150), res.Totals.Output)
	assert.Equal(t, int64(450), res.Totals.Total)
	assert.Equal(t, int64(140), res.Totals.CacheHit)
	assert.Equal(t, int64(60), res.Totals.CacheMiss)
	assert.InDelta(t, 4.5, res.Totals.Credit, 0.0001)
	assert.InDelta(t, 0.07, res.Totals.CostUSD, 0.0001)
	assert.Equal(t, int64(2), res.Totals.MessageCnt)

	// Grouped by model: glm (150 total) + deepseek (300 total), sorted by total desc.
	require.Len(t, res.Rows, 2)
	byModel := map[string]*service.UsageRow{}
	for _, r := range res.Rows {
		byModel[r.Key["model"]] = r
	}
	assert.Equal(t, int64(150), byModel["glm"].Total)
	assert.Equal(t, int64(300), byModel["deepseek"].Total)
}

func TestUsageStatsCartesianDimsAndAgentJoin(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// Two backends × two models under the same agent.
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s1", agentID: "cb", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 10:00:00", total: 10,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s2", agentID: "cb", agentName: "CodeBuddy",
		backend: "codebuddy", model: "deepseek", createdAt: "2026-01-11 10:00:00", total: 20,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s3", agentID: "cc", agentName: "ClaudeCode",
		backend: "claude", model: "opus", createdAt: "2026-01-12 10:00:00", total: 30,
	})

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimBackend, service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	require.Len(t, res.Rows, 3)
	seen := map[string]bool{}
	for _, r := range res.Rows {
		seen[r.Key["backend"]+"/"+r.Key["model"]] = true
	}
	assert.True(t, seen["codebuddy/glm"])
	assert.True(t, seen["codebuddy/deepseek"])
	assert.True(t, seen["claude/opus"])

	// Agent dim resolves agents.name.
	res2, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimAgent},
		Metrics:     []service.UsageMetric{service.MetricTotal},
		SortBy:      service.MetricTotal,
	}))
	require.NoError(t, err)
	require.Len(t, res2.Rows, 2)
	byAgent := map[string]*service.UsageRow{}
	for _, r := range res2.Rows {
		byAgent[r.Key["agent"]] = r
	}
	assert.Equal(t, int64(30), byAgent["ClaudeCode"].Total)
	assert.Equal(t, int64(30), byAgent["CodeBuddy"].Total, "CodeBuddy rows sum 10+20")
}

func TestUsageStatsAgentFallbackToID(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// No agents row for this session's agent_id → falls back to agent_id label.
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s1", agentID: "orphan-agent",
		backend: "claude", model: "opus", createdAt: "2026-01-10 10:00:00", total: 7,
	})

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimAgent},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	assert.Equal(t, "orphan-agent", res.Rows[0].Key["agent"])
}

func TestUsageStatsEmptyLabelAndZeroUsageRowsIncluded(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// model is empty → (empty) bucket. Two rows, one with all-zero usage.
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s1", agentID: "a", agentName: "A",
		backend: "claude", model: "", createdAt: "2026-01-10 10:00:00", total: 5,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s2", agentID: "a", agentName: "A",
		backend: "claude", model: "", createdAt: "2026-01-11 10:00:00", // zero usage
	})

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	assert.Equal(t, "(empty)", res.Rows[0].Key["model"])
	assert.Equal(t, int64(5), res.Rows[0].Total)
	assert.Equal(t, int64(2), res.Rows[0].MessageCnt, "zero-usage rows still counted")
}

func TestUsageStatsTrendByDay(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	// Two models across three days.
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s1", agentID: "a", agentName: "A",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 08:00:00", total: 10,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s2", agentID: "a", agentName: "A",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 20:00:00", total: 5,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s3", agentID: "a", agentName: "A",
		backend: "codebuddy", model: "deepseek", createdAt: "2026-01-11 09:00:00", total: 30,
	})

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
		Trend:       true,
	}))
	require.NoError(t, err)
	require.Len(t, res.Trend, 2, "two same-day+model rows merge into one bucket")

	// Sum per model across days matches the non-trend grouping.
	byDayModel := map[string]int64{}
	for _, r := range res.Trend {
		byDayModel[r.Day+"/"+r.Key["model"]] += r.Total
	}
	assert.Equal(t, int64(15), byDayModel["2026-01-10/glm"], "two rows same day model merge")
	assert.Equal(t, int64(30), byDayModel["2026-01-11/deepseek"])
}

func TestUsageStatsEmptyResult(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/nothing-here",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	require.NotNil(t, res.Totals)
	assert.Equal(t, int64(0), res.Totals.Total)
	assert.Empty(t, res.Rows)
}

func TestUsageStatsValidation(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	base := service.UsageParams{
		ProjectPath: "/p",
		Start:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		SortBy:      service.MetricTotal,
	}

	cases := []struct {
		name string
		want string // expected stable error code
		mut  func(*service.UsageParams)
	}{
		{"missing dims", "missing_dims", func(p *service.UsageParams) { p.Dims = nil }},
		{"invalid dim", "invalid_dim", func(p *service.UsageParams) { p.Dims = []service.UsageDim{"nope"} }},
		{"duplicate dims", "duplicate_dim", func(p *service.UsageParams) { p.Dims = []service.UsageDim{service.DimModel, service.DimModel} }},
		{"missing metrics", "missing_metrics", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = nil
		}},
		{"invalid sort", "invalid_sort", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = []service.UsageMetric{service.MetricTotal}
			p.SortBy = "bogus"
		}},
		{"end before start", "invalid_range", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = []service.UsageMetric{service.MetricTotal}
			p.End = p.Start
		}},
		{"span over 370 days", "range_too_long", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = []service.UsageMetric{service.MetricTotal}
			p.End = p.Start.AddDate(1, 1, 0) // ~396 days
		}},
		{"more than four dims", "too_many_dims", func(p *service.UsageParams) {
			// Five entries including a duplicate: the cap must trip on count
			// before the duplicate check, so this pins maxUsageDims (4) itself
			// rather than accidentally passing via duplicate_dim.
			p.Dims = []service.UsageDim{
				service.DimProject, service.DimModel, service.DimBackend, service.DimAgent, service.DimModel,
			}
			p.Metrics = []service.UsageMetric{service.MetricTotal}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.mut(&p)
			_, err := service.UsageStats(context.Background(), p)
			require.Error(t, err)
			var vErr *service.UsageStatsError
			require.ErrorAs(t, err, &vErr, "expected validation error, got %T", err)
			assert.Equal(t, tc.want, vErr.Code,
				"the rejection reason must be the specific rule, not a coincidental one")
		})
	}
}

func TestUsageStatsSortByCostAsc(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s1", agentID: "a", agentName: "A",
		backend: "claude", model: "opus", createdAt: "2026-01-10 10:00:00",
		input: 100, output: 50, total: 150, credit: 1.0, costUSD: 0.50,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s2", agentID: "a", agentName: "A",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 11:00:00",
		input: 200, output: 20, total: 220, credit: 2.0, costUSD: 0.10,
	})

	// Ascending cost → cheapest (codebuddy/glm) first.
	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimBackend},
		Metrics:     []service.UsageMetric{service.MetricCost, service.MetricInput, service.MetricCredit},
		SortBy:      service.MetricCost,
		SortDesc:    false,
	}))
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, "codebuddy", res.Rows[0].Key["backend"])
	assert.InDelta(t, 0.10, res.Rows[0].CostUSD, 0.0001)
	assert.Equal(t, int64(200), res.Rows[0].Input)
	assert.InDelta(t, 2.0, res.Rows[0].Credit, 0.0001)
	assert.Equal(t, "claude", res.Rows[1].Key["backend"])
}

// TestUsageStatsSurvivesSessionHardDelete is the core regression for the
// standalone usage ledger: hard-deleting a session (and therefore all of its
// chat_history rows) must NOT remove the token/cost records for work that was
// actually performed.
func TestUsageStatsSurvivesSessionHardDelete(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s-del", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 10:00:00",
		input: 100, output: 50, total: 150, cacheHit: 40, cacheMiss: 10, credit: 1.5, costUSD: 0.02,
	})

	before, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	require.Equal(t, int64(150), before.Totals.Total)

	// Delete the session outright — chat_history rows cascade away.
	require.NoError(t, service.HardDeleteSession("s-del"))

	var historyCount, ledgerCount int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE session_id = ?", "s-del").Scan(&historyCount))
	assert.Equal(t, 0, historyCount, "messages must be gone")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_metadata WHERE clawbench_session_id = ?", "s-del").Scan(&ledgerCount))
	assert.Equal(t, 1, ledgerCount, "usage ledger must survive session deletion")

	// Statistics must still report the consumed usage after deletion.
	after, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	require.NotNil(t, after.Totals)
	assert.Equal(t, int64(150), after.Totals.Total, "usage must not be lost after session delete")
	assert.Equal(t, int64(1), after.Totals.MessageCnt)
	require.Len(t, after.Rows, 1)
	assert.Equal(t, "glm", after.Rows[0].Key["model"])
}

// TestUsageStatsSurvivesPurgeArchivedData covers the retention auto-purge path:
// archived sessions hard-deleted by the cleanup worker must also leave the
// usage ledger intact.
func TestUsageStatsSurvivesPurgeArchivedData(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "/p", sessionID: "s-arch", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 10:00:00",
		input: 100, output: 50, total: 150,
	})
	require.NoError(t, service.ArchiveSession("/p", "codebuddy", "s-arch"))

	sessionsPurged, _, err := service.PurgeArchivedData([]string{"s-arch"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), sessionsPurged)

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(150), res.Totals.Total, "purge must not remove usage ledger rows")
}

// TestUsageStatsResendAfterRewindNotDoubleCounted verifies the no-overlap claim:
// a rewind drops the removed turns but keeps their ledger rows, and a later
// re-send creates a fresh AUTOINCREMENT message id → a distinct ledger row. The
// preserved old row must not be merged or duplicated.
func TestUsageStatsResendAfterRewindNotDoubleCounted(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	sessID := "s-rewind"
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES (?, '/p', 'claude', 't', 'a')",
		sessID,
	)
	require.NoError(t, err)

	// Turn 1: assistant reply consumed 100 tokens.
	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES ('/p', 'claude', ?, 'assistant', '{}', 0, '2026-01-10 10:00:00')",
		sessID,
	)
	require.NoError(t, err)
	asst1ID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, total_tokens, created_at, project_path, backend, agent_id, clawbench_session_id)
		 VALUES (?, 'opus', 100, 100, '2026-01-10 10:00:00', '/p', 'claude', 'a', ?)`,
		asst1ID, sessID,
	)
	require.NoError(t, err)

	// Turn 2: another assistant reply, then rewind back to turn 1.
	res, err = db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES ('/p', 'claude', ?, 'assistant', '{}', 0, '2026-01-10 10:05:00')",
		sessID,
	)
	require.NoError(t, err)
	asst2ID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, total_tokens, created_at, project_path, backend, agent_id, clawbench_session_id)
		 VALUES (?, 'opus', 50, 50, '2026-01-10 10:05:00', '/p', 'claude', 'a', ?)`,
		asst2ID, sessID,
	)
	require.NoError(t, err)

	_, err = service.TruncateSessionAfterMessage(sessID, asst1ID)
	require.NoError(t, err)

	// The rewound turn's ledger row survives; its message row does not.
	var n int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_metadata WHERE message_id = ?", asst2ID).Scan(&n))
	assert.Equal(t, 1, n, "rewound usage must be retained")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM chat_history WHERE id = ?", asst2ID).Scan(&n))
	assert.Equal(t, 0, n)

	// Re-send: a new AUTOINCREMENT id, so a new distinct ledger row.
	res, err = db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming, created_at) VALUES ('/p', 'claude', ?, 'assistant', '{}', 0, '2026-01-10 10:10:00')",
		sessID,
	)
	require.NoError(t, err)
	asst3ID, _ := res.LastInsertId()
	require.NotEqual(t, asst2ID, asst3ID, "AUTOINCREMENT must not reuse ids")
	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, total_tokens, created_at, project_path, backend, agent_id, clawbench_session_id)
		 VALUES (?, 'opus', 100, 100, '2026-01-10 10:10:00', '/p', 'claude', 'a', ?)`,
		asst3ID, sessID,
	)
	require.NoError(t, err)

	// Total = retained rewound turn (50) + original turn 1 (100) + re-send (100).
	// No double-count: the rewound row and the re-sent row are separate records.
	got, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/p",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(250), got.Totals.Total)
	assert.Equal(t, int64(3), got.Totals.MessageCnt)
}

// TestUsageStatsForkDoesNotDoubleCountOnBackfill guards the fork invariant
// against the startup metadata backfill. ForkSession copies assistant content
// verbatim (including embedded metadata) but deliberately does not copy the
// ledger. Without excluding copy-origin sessions from MigrateMetadataFromContent,
// the next startup would re-create ledger rows for the forked messages and
// double-count usage the source session already contributed.
func TestUsageStatsForkDoesNotDoubleCountOnBackfill(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	sid := "fork-src"
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES (?, '/p', 'claude', 'T', 'a')", sid)
	require.NoError(t, err)
	content := `{"blocks":[{"type":"text","text":"hi"}],"metadata":{"model":"opus","inputTokens":100,"totalTokens":100}}`
	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES ('/p','claude',?,'assistant',?,0)",
		sid, content,
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, total_tokens, project_path, backend, agent_id, clawbench_session_id)
		 VALUES (?, 'opus', 100, 100, '/p', 'claude', 'a', ?)`, msgID, sid,
	)
	require.NoError(t, err)

	_, err = service.ForkSession(sid, "/p", "Fork", 0, "")
	require.NoError(t, err)

	// Startup backfill must not resurrect usage for the forked copy.
	service.MigrateMetadataFromContent()

	got, err := service.UsageStats(context.Background(), service.UsageParams{
		ProjectPath: "/p",
		Start:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		End:         time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(100), got.Totals.Total, "fork must not double-count usage on backfill")
	assert.Equal(t, int64(1), got.Totals.MessageCnt)
}

// TestSaveMetadataAttributionPopulated verifies SaveMetadata denormalizes the
// attribution columns from chat_history/chat_sessions at write time, so the
// ledger is self-contained.
func TestSaveMetadataAttributionPopulated(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	sid := "attr-sess"
	_, err := db.Exec("INSERT INTO chat_sessions (id, project_path, backend, title, agent_id) VALUES (?, '/proj', 'codebuddy', 'T', 'codebuddy')", sid)
	require.NoError(t, err)
	res, err := db.Exec(
		"INSERT INTO chat_history (project_path, backend, session_id, role, content, streaming) VALUES ('/proj','codebuddy',?,'assistant','{}',0)",
		sid,
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()

	require.NoError(t, service.SaveMetadata(msgID, &ai.Metadata{Model: "glm", TotalTokens: 5}))

	var project, backend, agentID, clawSID string
	require.NoError(t, db.QueryRow(
		"SELECT project_path, backend, agent_id, clawbench_session_id FROM chat_metadata WHERE message_id = ?", msgID,
	).Scan(&project, &backend, &agentID, &clawSID))
	assert.Equal(t, "/proj", project)
	assert.Equal(t, "codebuddy", backend)
	assert.Equal(t, "codebuddy", agentID)
	assert.Equal(t, sid, clawSID)
}

// --- cross-project scope ---

// seedTwoProjects inserts one row in /a and one in /b, both inside the default
// window, so a scope=all query must see 2 rows while scope=project sees 1.
func seedTwoProjects(t *testing.T, db *sql.DB) {
	t.Helper()
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-a1", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 10:00:00",
		input: 100, output: 50, total: 150,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/b", sessionID: "s-b1", agentID: "other", agentName: "Other",
		backend: "claude", model: "opus", createdAt: "2026-01-11 10:00:00",
		input: 200, output: 100, total: 300,
	})
}

// TestUsageStatsScopeAllSpansProjects is the core cross-project test: the same
// window and dims must return only /a in the default scope and both projects in
// ScopeAll. Asserting both directions in one test is what makes it meaningful —
// a ScopeAll that silently degraded to single-project would still "work" if
// only the per-project case were checked.
func TestUsageStatsScopeAllSpansProjects(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)
	seedTwoProjects(t, db)

	single, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		ProjectPath: "/a",
		Dims:        []service.UsageDim{service.DimModel},
		Metrics:     []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(150), single.Totals.Total, "default scope stays single-project")
	require.Len(t, single.Rows, 1)

	all, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		Scope:   service.ScopeAll,
		Dims:    []service.UsageDim{service.DimModel},
		Metrics: []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(450), all.Totals.Total, "scope=all must span every project")
	assert.Equal(t, int64(2), all.Totals.MessageCnt)
	require.Len(t, all.Rows, 2)
}

// TestUsageStatsScopeAllIncludesEmptyProjectPath guards the reason the project
// predicate is dropped rather than passed as an empty string: legacy ledger
// rows can carry project_path=”. A naive implementation that filters on the
// empty string instead of removing the predicate would return only those legacy
// rows and silently drop every real project — so the fixture deliberately
// contains one of each and the assertion requires both.
func TestUsageStatsScopeAllIncludesEmptyProjectPath(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "", sessionID: "s-legacy", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 10:00:00",
		input: 70, output: 30, total: 100,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/real", sessionID: "s-real", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-11 10:00:00",
		input: 20, output: 5, total: 25,
	})

	all, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		Scope:   service.ScopeAll,
		Dims:    []service.UsageDim{service.DimModel},
		Metrics: []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(125), all.Totals.Total,
		"scope=all must include both the legacy empty-project row and real projects")
	assert.Equal(t, int64(2), all.Totals.MessageCnt)
}

// TestUsageStatsProjectDimGroupsByProject asserts the new dim splits the
// instance-wide total per project, which is the whole point of cross-project
// reporting ("which project consumes the most").
func TestUsageStatsProjectDimGroupsByProject(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)
	seedTwoProjects(t, db)

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		Scope:    service.ScopeAll,
		Dims:     []service.UsageDim{service.DimProject},
		Metrics:  []service.UsageMetric{service.MetricTotal},
		SortDesc: true,
	}))
	require.NoError(t, err)

	require.Len(t, res.Rows, 2)
	byProject := map[string]*service.UsageRow{}
	for _, r := range res.Rows {
		byProject[r.Key["project"]] = r
	}
	require.Contains(t, byProject, "/a")
	require.Contains(t, byProject, "/b")
	assert.Equal(t, int64(150), byProject["/a"].Total)
	assert.Equal(t, int64(300), byProject["/b"].Total)
	// Sorted by total desc, so the heavier project leads.
	assert.Equal(t, "/b", res.Rows[0].Key["project"])
}

// TestUsageStatsProjectDimEmptyBucket covers the "(empty)" bucket: a project
// group must be labeled, never rendered as an empty string, or the client
// would show a nameless row.
func TestUsageStatsProjectDimEmptyBucket(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	insertUsageSeed(t, db, usageSeed{
		project: "", sessionID: "s-legacy", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-10 10:00:00",
		input: 10, output: 10, total: 20,
	})

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		Scope:   service.ScopeAll,
		Dims:    []service.UsageDim{service.DimProject},
		Metrics: []service.UsageMetric{service.MetricTotal},
	}))
	require.NoError(t, err)
	require.Len(t, res.Rows, 1)
	assert.Equal(t, "(empty)", res.Rows[0].Key["project"])
}

// TestUsageStatsScopeAllTrendKeepsProjectDim covers the trend path's separate
// ranking step. trimTrendToTopN groups day rows into dim combos via
// serializeDimKey and keeps only the top N; if that key function omits the
// project dim, every project collapses into one combo and topN stops trimming
// anything. Three projects with topN=1 is the smallest case that can tell the
// two behaviors apart — with two rows and the default topN=10 both survive
// either way.
func TestUsageStatsScopeAllTrendKeepsProjectDim(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)
	seedTwoProjects(t, db)
	insertUsageSeed(t, db, usageSeed{
		project: "/c", sessionID: "s-c1", agentID: "codebuddy", agentName: "CodeBuddy",
		backend: "codebuddy", model: "glm", createdAt: "2026-01-12 10:00:00",
		input: 10, output: 10, total: 20,
	})

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		Scope:   service.ScopeAll,
		Dims:    []service.UsageDim{service.DimProject},
		Metrics: []service.UsageMetric{service.MetricTotal},
		Trend:   true,
		TopN:    1,
	}))
	require.NoError(t, err)

	require.Len(t, res.Trend, 1, "topN=1 must keep exactly the heaviest project")
	assert.Equal(t, "/b", res.Trend[0].Key["project"])
	assert.Equal(t, int64(300), res.Trend[0].Total)
	assert.NotEmpty(t, res.Trend[0].Day)
}

// TestUsageStatsAllFourDimsAccepted pins the dim cap to the number of real dims
// now that project joined the list. The old cap of 3 would reject the natural
// "project × model × backend × agent" breakdown.
func TestUsageStatsAllFourDimsAccepted(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)
	seedTwoProjects(t, db)

	res, err := service.UsageStats(context.Background(), usageParams(service.UsageParams{
		Scope:    service.ScopeAll,
		Dims:     []service.UsageDim{service.DimProject, service.DimModel, service.DimBackend, service.DimAgent},
		Metrics:  []service.UsageMetric{service.MetricTotal},
		SortDesc: true,
	}))
	require.NoError(t, err)
	require.Len(t, res.Rows, 2)
	assert.Equal(t, "/b", res.Rows[0].Key["project"])
	assert.Equal(t, "opus", res.Rows[0].Key["model"])
	assert.Equal(t, "claude", res.Rows[0].Key["backend"])
	assert.Equal(t, "Other", res.Rows[0].Key["agent"])
}

// TestUsageStatsScopeValidation covers the new scope rules. Each case asserts
// the stable error code so a caller can distinguish "you forgot the project"
// from "you sent both project and scope=all".
func TestUsageStatsScopeValidation(t *testing.T) {
	db := setupDB(t)
	ensureAgentsTable(t, db)

	base := func() service.UsageParams {
		return usageParams(service.UsageParams{
			ProjectPath: "/p",
			Dims:        []service.UsageDim{service.DimModel},
			Metrics:     []service.UsageMetric{service.MetricTotal},
		})
	}

	t.Run("unknown scope rejected", func(t *testing.T) {
		p := base()
		p.Scope = "everything"
		_, err := service.UsageStats(context.Background(), p)
		var vErr *service.UsageStatsError
		require.ErrorAs(t, err, &vErr)
		assert.Equal(t, "invalid_scope", vErr.Code)
	})

	t.Run("scope=all with a project path is contradictory", func(t *testing.T) {
		p := base()
		p.Scope = service.ScopeAll
		p.ProjectPath = "/p"
		_, err := service.UsageStats(context.Background(), p)
		var vErr *service.UsageStatsError
		require.ErrorAs(t, err, &vErr)
		assert.Equal(t, "conflicting_scope", vErr.Code)
	})

	t.Run("empty project still rejected without scope=all", func(t *testing.T) {
		p := base()
		p.ProjectPath = ""
		_, err := service.UsageStats(context.Background(), p)
		var vErr *service.UsageStatsError
		require.ErrorAs(t, err, &vErr)
		assert.Equal(t, "missing_project", vErr.Code)
	})

	t.Run("scope=all with no project path is accepted", func(t *testing.T) {
		p := base()
		p.ProjectPath = ""
		p.Scope = service.ScopeAll
		_, err := service.UsageStats(context.Background(), p)
		require.NoError(t, err)
	})

	t.Run("explicit scope=project behaves like the default", func(t *testing.T) {
		p := base()
		p.Scope = service.ScopeProject
		_, err := service.UsageStats(context.Background(), p)
		require.NoError(t, err)
	})
}
