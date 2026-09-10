package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// UsageDim is an aggregation dimension for usage statistics.
// Whitelisted identifiers — never interpolate user strings into SQL.
type UsageDim string

const (
	DimModel   UsageDim = "model"
	DimBackend UsageDim = "backend"
	DimAgent   UsageDim = "agent"
)

// UsageMetric is a numeric metric summed over the selected time range.
type UsageMetric string

const (
	MetricInput    UsageMetric = "input"
	MetricOutput   UsageMetric = "output"
	MetricTotal    UsageMetric = "total"
	MetricCacheHit UsageMetric = "cacheHit"
	MetricCredit   UsageMetric = "credit"
	MetricCost     UsageMetric = "cost"
)

// UsageParams configures a single usage statistics query.
type UsageParams struct {
	ProjectPath string
	// Start/End bound the usage window on chat_metadata.created_at.
	// NOTE: chat_metadata.created_at is NOT the user message time — SaveMetadata
	// (service/chat.go) upserts via INSERT OR REPLACE without writing created_at,
	// so SQLite re-stamps it CURRENT_TIMESTAMP on every metadata save. The value
	// is therefore the *final metadata write* (≈ assistant message completion),
	// which is the intended window/bucket timebase for usage stats.
	Start, End time.Time // range filter on chat_metadata.created_at (UTC)
	Dims       []UsageDim
	Metrics    []UsageMetric
	SortBy     UsageMetric
	SortDesc   bool
	Limit      int  // max rows returned; 0 → default 50, capped at 200
	Trend      bool // group by day × dims instead of dims only
	TopN       int  // trend: keep only top-N dim combos by total tokens; 0 → default 10
}

// UsageRow aggregates one dimension combination (or one day × combination for
// trend queries). CacheHit/CacheMiss sums are returned so the client can
// compose the cache breakdown (hit rate is shown from range totals in the
// overview, not as an additive metric column).
type UsageRow struct {
	Day        string            `json:"day,omitempty"` // "2006-01-02", trend only
	Key        map[string]string `json:"key"`           // dim → group label, only requested dims
	Input      int64             `json:"input"`
	Output     int64             `json:"output"`
	Total      int64             `json:"total"`
	CacheHit   int64             `json:"cacheHit"`
	CacheMiss  int64             `json:"cacheMiss"`
	Credit     float64           `json:"credit"`
	CostUSD    float64           `json:"costUsd"`
	MessageCnt int64             `json:"messageCnt"`
}

// UsageTotals is the single-row aggregate over the whole filtered range.
type UsageTotals struct {
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	Total      int64   `json:"total"`
	CacheHit   int64   `json:"cacheHit"`
	CacheMiss  int64   `json:"cacheMiss"`
	Credit     float64 `json:"credit"`
	CostUSD    float64 `json:"costUsd"`
	MessageCnt int64   `json:"messageCnt"`
}

// UsageStatsResult is the full response of a UsageStats call.
type UsageStatsResult struct {
	Totals *UsageTotals `json:"totals"`
	Rows   []*UsageRow  `json:"rows"`
	Trend  []*UsageRow  `json:"trend,omitempty"`
}

// UsageStatsError identifies validation failures so the handler can map them
// to HTTP 400 instead of 500. Code is a stable machine-readable identifier the
// client can act on; Error() carries a human-readable detail for server logs
// only (never surfaced to the UI — the handler reports Code).
type UsageStatsError struct {
	Code string
	msg  string
}

func (e *UsageStatsError) Error() string { return e.msg }

func usageErr(code, format string, args ...any) error {
	return &UsageStatsError{Code: code, msg: fmt.Sprintf(format, args...)}
}

const emptyGroupLabel = "(empty)"

// dimExpr maps a whitelisted dim to its SELECT/GROUP BY expression.
func dimExpr(d UsageDim) (string, bool) {
	switch d {
	case DimModel:
		return "COALESCE(NULLIF(m.model,''),'" + emptyGroupLabel + "')", true
	case DimBackend:
		return "COALESCE(NULLIF(m.backend,''),'" + emptyGroupLabel + "')", true
	case DimAgent:
		// Agent display name from agents.name, falling back to the stored
		// agent_id (the agents row may have been deleted), then to a stable
		// "(empty)" bucket so NULLs never merge into one unknown group. The
		// name is resolved live via LEFT JOIN agents; the ledger itself stores
		// only agent_id so it survives session deletion.
		return "COALESCE(NULLIF(a.name,''), NULLIF(m.agent_id,''), '" + emptyGroupLabel + "')", true
	}
	return "", false
}

// sortExpr maps a whitelisted metric to an ORDER BY expression.
func sortExpr(m UsageMetric) (string, bool) {
	switch m {
	case MetricInput:
		return "SUM(m.input_tokens)", true
	case MetricOutput:
		return "SUM(m.output_tokens)", true
	case MetricTotal:
		return "SUM(m.total_tokens)", true
	case MetricCacheHit:
		return "SUM(m.cache_hit_tokens)", true
	case MetricCredit:
		return "SUM(m.credit)", true
	case MetricCost:
		return "SUM(m.cost_usd)", true
	}
	return "", false
}

const (
	defaultUsageLimit = 50
	maxUsageLimit     = 200
	defaultTrendTopN  = 10
	maxRangeDays      = 370
)

func validateUsageParams(p *UsageParams) error {
	if p == nil {
		return usageErr("missing_params", "usage params required")
	}
	if p.ProjectPath == "" {
		return usageErr("missing_project", "project path required")
	}
	if !p.End.After(p.Start) {
		return usageErr("invalid_range", "end must be after start")
	}
	if p.End.Sub(p.Start) > maxRangeDays*24*time.Hour {
		return usageErr("range_too_long", "range exceeds %d days", maxRangeDays)
	}
	if len(p.Dims) == 0 {
		return usageErr("missing_dims", "at least one dimension required")
	}
	if len(p.Dims) > 3 {
		return usageErr("too_many_dims", "at most three dimensions")
	}
	seen := map[UsageDim]bool{}
	for _, d := range p.Dims {
		if _, ok := dimExpr(d); !ok {
			return usageErr("invalid_dim", "invalid dimension %q", d)
		}
		if seen[d] {
			return usageErr("duplicate_dim", "duplicate dimension %q", d)
		}
		seen[d] = true
	}
	if len(p.Metrics) == 0 {
		return usageErr("missing_metrics", "at least one metric required")
	}
	for _, m := range p.Metrics {
		if _, ok := sortExpr(m); !ok {
			return usageErr("invalid_metric", "invalid metric %q", m)
		}
	}
	if p.SortBy == "" {
		p.SortBy = MetricTotal
	}
	if _, ok := sortExpr(p.SortBy); !ok {
		return usageErr("invalid_sort", "invalid sort metric %q", p.SortBy)
	}
	return nil
}

// UsageStats aggregates token/cost/credit usage from the chat_metadata ledger.
// The ledger is standalone (no FK to chat_history) and carries denormalized
// project_path/backend/agent_id, so this query works even after the session and
// its messages have been deleted. agents is LEFT JOINed only for the display
// name. The query never touches real user strings: dims/metrics map onto fixed
// whitelisted column expressions.
func UsageStats(ctx context.Context, p UsageParams) (*UsageStatsResult, error) {
	if err := validateUsageParams(&p); err != nil {
		return nil, err
	}

	dimExprs := make([]string, 0, len(p.Dims))
	for _, d := range p.Dims {
		e, _ := dimExpr(d)
		dimExprs = append(dimExprs, e)
	}

	sums := "COALESCE(SUM(m.input_tokens),0), COALESCE(SUM(m.output_tokens),0), COALESCE(SUM(m.total_tokens),0), " +
		"COALESCE(SUM(m.cache_hit_tokens),0), COALESCE(SUM(m.cache_miss_tokens),0), " +
		"COALESCE(SUM(m.credit),0), COALESCE(SUM(m.cost_usd),0), COUNT(*)"
	// chat_metadata is a standalone ledger with denormalized project_path/
	// backend/agent_id, so no join back to chat_history/chat_sessions is needed
	// (those rows may have been deleted). agents is joined only to resolve the
	// agent display name.
	from := "FROM chat_metadata m " +
		"LEFT JOIN agents a ON a.id = m.agent_id"

	limit := p.Limit
	if limit <= 0 {
		limit = defaultUsageLimit
	}
	if limit > maxUsageLimit {
		limit = maxUsageLimit
	}

	order := "DESC"
	if !p.SortDesc {
		order = "ASC"
	}
	sortBy, _ := sortExpr(p.SortBy)

	// Where clause. project_path filters the ledger's denormalized project;
	// created_at bounds the usage window. UTC-formatted params match SQLite
	// stored text.
	startStr := p.Start.UTC().Format("2006-01-02 15:04:05")
	endStr := p.End.UTC().Format("2006-01-02 15:04:05")
	where := "WHERE m.project_path = ? AND m.created_at >= ? AND m.created_at < ?"
	args := []any{p.ProjectPath, startStr, endStr}

	res := &UsageStatsResult{}

	// Totals: same filter, no grouping — one aggregate row over the range.
	totalsRow, err := scanUsageTotalsRow(dbRead.QueryRowContext(ctx, "SELECT "+sums+" "+from+" "+where, args...))
	if err != nil {
		return nil, fmt.Errorf("usage totals: %w", err)
	}
	res.Totals = totalsRow

	if !p.Trend {
		rows, qErr := queryUsageRows(ctx, p, dimExprs, sums, from, where, args, limit, sortBy, order)
		if qErr != nil {
			return nil, qErr
		}
		res.Rows = rows
		return res, nil
	}

	// Trend: group by day × dims. SQLite date() buckets UTC text created_at.
	topN := p.TopN
	if topN <= 0 {
		topN = defaultTrendTopN
	}
	trendRows, trendErr := queryTrendRows(ctx, p, dimExprs, sums, from, where, args)
	if trendErr != nil {
		return nil, trendErr
	}
	res.Rows = []*UsageRow{}
	res.Trend = trimTrendToTopN(trendRows, topN)
	return res, nil
}

// queryUsageRows executes the grouped SELECT for non-trend queries.
func queryUsageRows(ctx context.Context, p UsageParams, dimExprs []string, sums, from, where string, args []any, limit int, sortBy, order string) ([]*UsageRow, error) {
	dimCols := strings.Join(dimExprs, ", ")
	query := "SELECT " + dimCols + ", " + sums + " " + from + " " + where +
		" GROUP BY " + dimCols + " ORDER BY " + sortBy + " " + order + ", " + dimCols +
		" LIMIT " + fmt.Sprintf("%d", limit)

	rows, err := dbRead.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("usage stats query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []*UsageRow{}
	for rows.Next() {
		row, scanErr := scanUsageRow(rows, p.Dims, false)
		if scanErr != nil {
			return nil, fmt.Errorf("scan usage row: %w", scanErr)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage stats rows: %w", err)
	}
	return out, nil
}

// queryTrendRows executes the day × dims grouped SELECT for trend queries.
func queryTrendRows(ctx context.Context, p UsageParams, dimExprs []string, sums, from, where string, args []any) ([]*UsageRow, error) {
	dayDims := make([]string, 0, len(dimExprs)+1)
	dayDims = append(dayDims, "date(m.created_at)")
	dayDims = append(dayDims, dimExprs...)
	query := "SELECT " + strings.Join(dayDims, ", ") + ", " + sums + " " + from + " " + where +
		" GROUP BY " + strings.Join(dayDims, ", ") +
		" ORDER BY " + dayDims[0] + " ASC"

	rows, err := dbRead.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("usage trend query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []*UsageRow{}
	for rows.Next() {
		row, scanErr := scanUsageRow(rows, p.Dims, true)
		if scanErr != nil {
			return nil, fmt.Errorf("scan usage trend: %w", scanErr)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage trend rows: %w", err)
	}
	return out, nil
}

// trimTrendToTopN keeps only the top-N dimension combinations (ranked by total
// tokens summed across the range) so the trend chart does not explode with rows.
func trimTrendToTopN(trend []*UsageRow, topN int) []*UsageRow {
	dayTotal := map[string]int64{} // dim-combo key (serialized) → total tokens
	for _, row := range trend {
		dayTotal[serializeDimKey(row.Key)] += row.Total
	}
	type comboAgg struct {
		key string
		tot int64
	}
	rank := make([]comboAgg, 0, len(dayTotal))
	for k, tot := range dayTotal {
		rank = append(rank, comboAgg{key: k, tot: tot})
	}
	sort.Slice(rank, func(i, j int) bool { return rank[i].tot > rank[j].tot })

	keep := map[string]bool{}
	for i := 0; i < len(rank) && i < topN; i++ {
		keep[rank[i].key] = true
	}
	out := []*UsageRow{}
	for _, row := range trend {
		if keep[serializeDimKey(row.Key)] {
			out = append(out, row)
		}
	}
	return out
}

func serializeDimKey(key map[string]string) string {
	// map key iteration order is random — serialize deterministically.
	parts := make([]string, 0, len(key))
	for _, d := range []UsageDim{DimModel, DimBackend, DimAgent} {
		if v, ok := key[string(d)]; ok {
			parts = append(parts, string(d)+":"+v)
		}
	}
	return strings.Join(parts, "\x00")
}
