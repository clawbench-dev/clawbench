package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"clawbench/internal/service"
)

// reasonKey is the JSON detail key identifying the rejected query parameter.
const reasonKey = "reason"

// ServeUsageStats handles GET /api/usage/stats.
// Aggregates token/credit/cost usage for the current project cookie within a
// time range, grouped by the requested dimensions.
//
// Query params:
//
//	start / end   RFC3339 timestamps (UTC). Required.
//	dims          repeated, e.g. dims=model&dims=backend (1..3)
//	metrics       repeated, e.g. metrics=total&metrics=cost (>=1) — used only
//	              as a client hint; the backend always returns every SUM so the
//	              frontend can toggle columns without a refetch.
//	sort          metric id for ordering (default "total")
//	order         "asc" | "desc" (default "desc")
//	trend         "1" → group by day × dims
//	top           trend: number of top dim combos to keep (default 10)
//	limit         max rows (default 50, max 200)
func ServeUsageStats(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()

	startStr := q.Get("start")
	endStr := q.Get("end")
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{reasonKey: "start"})
		return
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{reasonKey: "end"})
		return
	}

	dims := make([]service.UsageDim, 0, 3)
	for _, v := range q["dims"] {
		dims = append(dims, service.UsageDim(v))
	}
	metrics := make([]service.UsageMetric, 0, 7)
	for _, v := range q["metrics"] {
		metrics = append(metrics, service.UsageMetric(v))
	}

	params := service.UsageParams{
		ProjectPath: projectPath,
		Start:       start,
		End:         end,
		Dims:        dims,
		Metrics:     metrics,
		SortBy:      service.UsageMetric(q.Get("sort")),
		SortDesc:    true,
		Trend:       q.Get("trend") == "1",
	}
	if q.Get("order") == "asc" {
		params.SortDesc = false
	}
	if v := q.Get("limit"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil {
			params.Limit = n
		}
	}
	if v := q.Get("top"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil && n > 0 {
			params.TopN = n
		}
	}

	result, err := service.UsageStats(r.Context(), params)
	if err != nil {
		var vErr *service.UsageStatsError
		if errors.As(err, &vErr) {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{reasonKey: vErr.Error()})
			return
		}
		writeLocalizedError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
