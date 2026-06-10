package rag

import (
	"context"
	"fmt"
	"log/slog"

	"clawbench/internal/service"
)

// SearchMode indicates which search strategy was used.
type SearchMode string

const (
	SearchModeHybrid SearchMode = "hybrid" // Vector + FTS with RRF fusion
	SearchModeVector SearchMode = "vector" // Vector similarity only
	SearchModeFTS    SearchMode = "fts"    // Full-text search only (BM25)
)

// SearchParams holds the parameters for a RAG search request.
type SearchParams struct {
	Query            string `json:"q"`
	Limit            int    `json:"limit"`
	ProjectPath      string `json:"project"`
	Backend          string `json:"backend"`
	Role             string `json:"role"`
	SessionID        string `json:"session_id"`
	ExcludeSessionID string `json:"exclude_session_id"`
	FromTime         string `json:"from"`
	ToTime           string `json:"to"`
}

// SearchResult represents the response from a RAG search.
type SearchResult struct {
	Results []SearchHit `json:"results"`
	Total   int         `json:"total"`
	Mode    SearchMode  `json:"mode"`
}

// RAGSearch performs a search using the best available strategy:
//   - Hybrid (vector + FTS with RRF) when embedding API is available and vec0 has data
//   - FTS-only when embedding API is unavailable or vec0 has no data
//
// FTS5 is always available in SQLite (built-in), unlike DuckDB where it was an extension.
func RAGSearch(ctx context.Context, store *Store, embedder *EmbeddingClient, params SearchParams, defaultLimit int, searchPoolSize int) (*SearchResult, error) { //nolint:gocyclo // multi-mode search with fallback
	if params.Query == "" {
		return &SearchResult{Mode: SearchModeFTS}, nil
	}

	if store == nil {
		return nil, fmt.Errorf("RAG not initialized: store is nil")
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	poolSize := searchPoolSize
	if poolSize <= 0 {
		poolSize = 20
	}

	// Determine embedder health
	embedderHealthy := EmbedderHealthy()
	if !embedderHealthy && embedder != nil {
		reachable, modelAvailable, _ := embedder.IsHealthy(ctx)
		embedderHealthy = reachable && modelAvailable
	}

	// Check vec0 readiness — vector search is available when HasVecData() returns true
	// (i.e., there are chunks with embeddings in the vec0 table)
	vecReady := store.HasVecData()

	// FTS5 is always available with SQLite (no extension loading needed)
	ftsAvailable := true

	var hits []SearchHit
	var mode SearchMode
	var err error

	switch {
	case embedderHealthy && vecReady && ftsAvailable:
		// Hybrid: vector + FTS with RRF fusion
		if embedder == nil {
			// Embedder marked healthy but no client available — fall back to FTS
			mode = SearchModeFTS
			hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)
			break
		}
		mode = SearchModeHybrid
		var queryEmbedding []float64
		queryEmbedding, err = embedder.Embed(ctx, params.Query)
		if err != nil {
			slog.Warn("rag: query embedding failed, falling back to FTS", slog.String("err", err.Error()))
			hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)
			mode = SearchModeFTS
		} else {
			hits, err = store.SearchHybrid(queryEmbedding, params.Query, poolSize, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)
		}

	case embedderHealthy && vecReady && !ftsAvailable:
		// Vector-only (FTS not available — shouldn't happen with SQLite, but defensive)
		if embedder == nil {
			mode = SearchModeFTS
			hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)
			break
		}
		mode = SearchModeVector
		var queryEmbedding []float64
		queryEmbedding, err = embedder.Embed(ctx, params.Query)
		if err != nil {
			return nil, fmt.Errorf("embed query: %w", err)
		}
		hits, err = store.SearchVector(queryEmbedding, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)

	case embedderHealthy && !vecReady:
		// Embedder available but no vectors in vec0 — degrade to FTS-only
		mode = SearchModeFTS
		slog.Warn("rag: vec0 has no data, falling back to FTS-only")
		hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)

	default:
		// FTS-only (embedding API unavailable)
		mode = SearchModeFTS
		hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)
	}

	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Enrich hits with session titles from SQLite
	sessionIDs := make(map[string]bool)
	for _, h := range hits {
		sessionIDs[h.SessionID] = true
	}

	titles := getSessionTitles(sessionIDs)
	for i := range hits {
		if title, ok := titles[hits[i].SessionID]; ok {
			hits[i].SessionTitle = title
		}
	}

	slog.Info(
		"rag search completed",
		slog.String("query", params.Query),
		slog.String("mode", string(mode)),
		slog.Int("results", len(hits)),
		slog.Int("limit", limit),
	)

	return &SearchResult{
		Results: hits,
		Total:   len(hits),
		Mode:    mode,
	}, nil
}

// getSessionTitles fetches session titles for a set of session IDs from SQLite.
// Returns empty map if the service DB is not available (e.g., during tests).
func getSessionTitles(sessionIDs map[string]bool) map[string]string {
	if len(sessionIDs) == 0 {
		return map[string]string{}
	}

	// Check if service DB is available
	if service.DB == nil {
		return map[string]string{}
	}

	ids := make([]string, 0, len(sessionIDs))
	for id := range sessionIDs {
		ids = append(ids, id)
	}
	titles, err := service.GetSessionTitlesBatchIncludeDeleted(ids)
	if err != nil {
		titles = make(map[string]string, len(sessionIDs))
		for id := range sessionIDs {
			title, err := service.GetSessionTitle(id)
			if err == nil && title != "" {
				titles[id] = title
			}
		}
	}
	return titles
}
