package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// detailKey is the JSON detail key identifying the rejected query parameter.
const detailKey = "reason"

// resolveUsageScope decides which projects a stats request may read, writing
// the error response and returning false when the request must not proceed.
//
// It returns the project path to filter on, which is "" for scope=all (the
// caller must then drop the project predicate entirely rather than filter on an
// empty string — see service.UsageStats).
//
// Split out of ServeUsageStats so the gate can be tested on its own and so the
// handler's remaining body stays within the project's complexity budget.
func resolveUsageScope(w http.ResponseWriter, r *http.Request, rawScope string) (service.UsageScope, string, bool) {
	scope := service.UsageScope(rawScope)
	projectPath := middleware.GetProjectFromCookie(r)
	switch scope {
	case "", service.ScopeProject:
		if projectPath == "" {
			slog.Warn("handler: ServeUsageStats — project cookie is empty",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path))
			writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
			return "", "", false
		}
		return service.ScopeProject, projectPath, true
	case service.ScopeAll:
		// Cross-project reads are an AI-only capability. The AI token is
		// loopback + signed, so a remote caller or a browser cannot obtain it;
		// the loopback address alone is not enough (the FRP tunnel also dials
		// in from 127.0.0.1), which is why IsAITokenRequest checks both.
		if !middleware.IsAITokenRequest(r) {
			writeLocalizedErrorf(w, r, http.StatusForbidden, "AccessDenied")
			return "", "", false
		}
		// The cookie is deliberately ignored, not merged: a project path
		// alongside scope=all is contradictory and rejected downstream by
		// validateUsageParams (conflicting_scope). Passing it through is what
		// makes that rejection reachable.
		return service.ScopeAll, projectPath, true
	default:
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: "scope"})
		return "", "", false
	}
}

// ServeUsageStats handles GET /api/usage/stats.
// Aggregates token/credit/cost usage within a time range, grouped by the
// requested dimensions.
//
// Query params:
//
//	start / end   RFC3339 timestamps (UTC). Required.
//	dims          repeated, e.g. dims=model&dims=backend (1..4)
//	metrics       repeated, e.g. metrics=total&metrics=cost (>=1) — used only
//	              as a client hint; the backend always returns every SUM so the
//	              frontend can toggle columns without a refetch.
//	sort          metric id for ordering (default "total")
//	order         "asc" | "desc" (default "desc")
//	trend         "1" → group by day × dims
//	top           trend: number of top dim combos to keep (default 10)
//	limit         max rows (default 50, max 200)
//	scope         "project" (default) | "all" — see below
//
// Project scope: by default the range is restricted to the project named by
// the project cookie, which is required. scope=all aggregates every project on
// the instance and is granted ONLY to a local AI token (the /cb-usage slash
// command); a browser session asking for it is refused, so the stats panel
// keeps its per-project isolation. The cookie must be absent for scope=all —
// sending both is a contradiction and rejected.
func ServeUsageStats(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	q := r.URL.Query()

	// Resolve scope before touching the project cookie: scope=all deliberately
	// has no project, and requireProject would reject the request outright.
	scope, projectPath, ok := resolveUsageScope(w, r, q.Get("scope"))
	if !ok {
		return
	}

	startStr := q.Get("start")
	endStr := q.Get("end")
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: "start"})
		return
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: "end"})
		return
	}

	dims := make([]service.UsageDim, 0, 4)
	for _, v := range q["dims"] {
		dims = append(dims, service.UsageDim(v))
	}
	metrics := make([]service.UsageMetric, 0, 7)
	for _, v := range q["metrics"] {
		metrics = append(metrics, service.UsageMetric(v))
	}

	params := service.UsageParams{
		ProjectPath: projectPath,
		Scope:       scope,
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
			// Report a stable machine-readable code in detail; never surface the
			// server's English Error() text to the UI.
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", map[string]any{detailKey: vErr.Code})
			return
		}
		writeLocalizedError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}
