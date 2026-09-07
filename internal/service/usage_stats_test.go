package service_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

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
	if _, err := db.Exec(
		`INSERT INTO chat_metadata (message_id, model, input_tokens, output_tokens, total_tokens,
			cache_hit_tokens, cache_miss_tokens, credit, cost_usd, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msgID, s.model, s.input, s.output, s.total, s.cacheHit, s.cacheMiss, s.credit, s.costUSD, s.createdAt,
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
		mut  func(*service.UsageParams)
	}{
		{"missing dims", func(p *service.UsageParams) { p.Dims = nil }},
		{"invalid dim", func(p *service.UsageParams) { p.Dims = []service.UsageDim{"nope"} }},
		{"duplicate dims", func(p *service.UsageParams) { p.Dims = []service.UsageDim{service.DimModel, service.DimModel} }},
		{"missing metrics", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = nil
		}},
		{"invalid sort", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = []service.UsageMetric{service.MetricTotal}
			p.SortBy = "bogus"
		}},
		{"end before start", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = []service.UsageMetric{service.MetricTotal}
			p.End = p.Start
		}},
		{"span over 370 days", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel}
			p.Metrics = []service.UsageMetric{service.MetricTotal}
			p.End = p.Start.AddDate(1, 1, 0) // ~396 days
		}},
		{"more than three dims", func(p *service.UsageParams) {
			p.Dims = []service.UsageDim{service.DimModel, service.DimBackend, service.DimAgent, service.DimModel}
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
