package rag

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

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
	PreferMode       string `json:"prefer_mode,omitempty"` // "hybrid" (default) or "fts"
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

	// User can force FTS-only mode via PreferMode
	forceFTS := strings.EqualFold(params.PreferMode, string(SearchModeFTS))

	var hits []SearchHit
	var mode SearchMode
	var err error

	switch {
	case forceFTS:
		// User explicitly requested FTS-only
		mode = SearchModeFTS
		hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)

	case embedderHealthy && vecReady:
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

	// Compute match positions for all hits using textMatchPositions.
	// This is done uniformly for all search modes (FTS, vector, hybrid) because:
	// - FTS5 offsets() returns positions in chunk_text_segmented (not chunk_text), making them unusable
	// - Vector search has no native position information
	// - textMatchPositions provides consistent, correct highlighting for all modes
	for i := range hits {
		hits[i].MatchPositions = textMatchPositions(params.Query, hits[i].ChunkText)
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
	if !service.DBReady() {
		return map[string]string{}
	}

	ids := make([]string, 0, len(sessionIDs))
	for id := range sessionIDs {
		ids = append(ids, id)
	}
	titles, err := service.GetSessionTitlesBatchIncludeArchived(ids)
	if err != nil {
		return map[string]string{}
	}
	return titles
}

// maxChunksPerSession caps the number of chunk details returned per session
// to prevent one dominant session from consuming all result space.
const maxChunksPerSession = 5

// SessionSearchResult represents an aggregated search result grouped by session.
type SessionSearchResult struct {
	SessionID    string     `json:"session_id"`
	SessionTitle string     `json:"session_title"`
	Score        float64    `json:"score"`
	Backend      string     `json:"backend"`
	ProjectPath  string     `json:"project_path"`
	Archived     bool       `json:"archived"`
	CreatedAt    time.Time  `json:"created_at"`
	MatchCount   int        `json:"match_count"`
	Chunks       []ChunkHit `json:"chunks"`
}

// ChunkHit represents a single matching chunk within a session search result.
type ChunkHit struct {
	ChunkID        int64        `json:"chunk_id"`
	ChunkText      string       `json:"chunk_text"`
	MatchPositions []MatchRange `json:"match_positions"`
	Score          float64      `json:"score"`
	Role           string       `json:"role"`
	MessageID      int64        `json:"message_id"`
	CreatedAt      time.Time    `json:"created_at"`
}

// SessionSearchResponse is the response for session-aggregated RAG search.
type SessionSearchResponse struct {
	Sessions []SessionSearchResult `json:"sessions"`
	Total    int                   `json:"total"`
	Mode     SearchMode            `json:"mode"`
}

// RAGSessionSearch performs RAG search and aggregates results by session.
// It fetches an expanded pool of chunks, groups by session_id with a per-session
// chunk cap, and returns up to searchLimit sessions sorted by best chunk score.
func RAGSessionSearch(ctx context.Context, store *Store, embedder *EmbeddingClient, params SearchParams, searchLimit int, searchPoolSize int) (*SessionSearchResponse, error) {
	if store == nil {
		return nil, fmt.Errorf("RAG not initialized: store is nil")
	}
	if params.Query == "" {
		return &SessionSearchResponse{}, nil
	}

	// Use searchPoolSize as expanded limit (already configurable, default 20)
	// This ensures enough chunks to aggregate into searchLimit sessions
	expandedLimit := searchPoolSize
	if expandedLimit < searchLimit*3 {
		expandedLimit = searchLimit * 3
	}

	expandedParams := params
	expandedParams.Limit = expandedLimit

	result, err := RAGSearch(ctx, store, embedder, expandedParams, expandedLimit, searchPoolSize)
	if err != nil {
		return nil, err
	}

	// Aggregate by session_id with per-session chunk cap
	sessionMap := make(map[string]*SessionSearchResult)
	var sessionOrder []string

	for _, hit := range result.Results {
		sr, exists := sessionMap[hit.SessionID]
		if !exists {
			sr = &SessionSearchResult{
				SessionID:   hit.SessionID,
				Score:       hit.Score,
				Backend:     hit.Backend,
				ProjectPath: hit.ProjectPath,
				CreatedAt:   hit.CreatedAt,
			}
			sessionMap[hit.SessionID] = sr
			sessionOrder = append(sessionOrder, hit.SessionID)
		}
		// Per-session chunk cap: don't accumulate more than maxChunksPerSession
		if len(sr.Chunks) >= maxChunksPerSession {
			sr.MatchCount++ // Still count it
			continue
		}
		sr.MatchCount++
		sr.Chunks = append(sr.Chunks, ChunkHit{
			ChunkID:        hit.ChunkID,
			ChunkText:      hit.ChunkText,
			MatchPositions: hit.MatchPositions,
			Score:          hit.Score,
			Role:           hit.Role,
			MessageID:      hit.MessageID,
			CreatedAt:      hit.CreatedAt,
		})
		// Keep the best score for the session
		if hit.Score > sr.Score {
			sr.Score = hit.Score
		}
	}

	// Sort sessions by score descending
	sessions := make([]*SessionSearchResult, 0, len(sessionMap))
	for _, id := range sessionOrder {
		sessions = append(sessions, sessionMap[id])
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Score > sessions[j].Score
	})

	// Truncate to searchLimit
	if len(sessions) > searchLimit {
		sessions = sessions[:searchLimit]
	}

	// Enrich with session titles and archived status
	sessionIDs := make(map[string]bool)
	for _, s := range sessions {
		sessionIDs[s.SessionID] = true
	}
	titles := getSessionTitles(sessionIDs)
	archivedMap := getSessionArchivedStatus(sessionIDs)

	// Build response
	out := make([]SessionSearchResult, len(sessions))
	for i, s := range sessions {
		out[i] = *s
		if title, ok := titles[s.SessionID]; ok {
			out[i].SessionTitle = title
		}
		if arch, ok := archivedMap[s.SessionID]; ok {
			out[i].Archived = arch
		}
	}

	slog.Info(
		"rag session search completed",
		slog.String("query", params.Query),
		slog.String("mode", string(result.Mode)),
		slog.Int("sessions", len(out)),
		slog.Int("search_limit", searchLimit),
	)

	return &SessionSearchResponse{
		Sessions: out,
		Total:    len(out),
		Mode:     result.Mode,
	}, nil
}

// getSessionArchivedStatus fetches the archived status for a set of session IDs.
func getSessionArchivedStatus(sessionIDs map[string]bool) map[string]bool {
	archivedMap := make(map[string]bool, len(sessionIDs))
	if !service.DBReady() || len(sessionIDs) == 0 {
		return archivedMap
	}
	ids := make([]string, 0, len(sessionIDs))
	for id := range sessionIDs {
		ids = append(ids, id)
	}
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := service.ReadDB().Query(
		"SELECT id, archived FROM chat_sessions WHERE id IN ("+placeholders+")", args...)
	if err != nil {
		return archivedMap
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var archived int
		if err := rows.Scan(&id, &archived); err == nil {
			archivedMap[id] = archived == 1
		}
	}
	return archivedMap
}
