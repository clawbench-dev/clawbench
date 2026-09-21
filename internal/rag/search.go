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
	SearchModeRecent SearchMode = "recent" // Browse all sessions newest-first (empty query)
	// SearchModeTitle means only the title channel ran: RAG is not configured,
	// so no content search happened. It is deliberately distinct from "recent"
	// (browse) — the client treats "recent" as the paginated browse list, and
	// these are search results.
	SearchModeTitle SearchMode = "title"
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
	Archived         string `json:"archived,omitempty"`    // "all" (default) | "active" | "archived"
	SortOrder        string `json:"sort,omitempty"`        // "relevance" (default) | "newest" | "oldest"
	// SessionType filters by session kind: "all" (default) | "chat" | "task".
	// "task" matches sessions whose stored session_type is 'scheduled'. Applied
	// post-aggregation, because rag_chunks carries no session_type column.
	SessionType string `json:"session_type,omitempty"`
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
	// SessionType is the raw stored value ("chat" | "scheduled") so the client
	// can badge task executions apart from interactive conversations.
	SessionType string `json:"session_type"`
	// TitleMatch reports that the session's title matched the query, so the
	// client can badge it and highlight the title. A session may match on both
	// title and content.
	TitleMatch bool `json:"title_match"`
	// TitleMatchPositions are rune offsets into SessionTitle, for highlighting.
	TitleMatchPositions []MatchRange `json:"title_match_positions,omitempty"`
	// TitleOnly reports that this session matched on its title alone and
	// therefore carries no chunks: the detail view has nothing to show, so the
	// client fetches the first message instead (as it does in browse mode).
	TitleOnly bool `json:"title_only"`
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
	// HasMore is set in browse ("recent") mode to signal another page exists.
	HasMore bool `json:"has_more,omitempty"`
}

// RecentSessions lists a page of the project's sessions for the "browse all"
// state of session search (no query entered). archiveFilter narrows to
// active/archived sessions (or all), typeFilter selects conversations or task
// executions (browse mode never mixes the two — see GetRecentSessions),
// fromTime/toTime bound the session creation time, and sortOrder selects
// newest/oldest time ordering. It returns up to limit sessions with title,
// backend, project, archived flag, session type and creation time, plus whether
// a further page exists. Pass the last row's created_at (RFC3339) and id as
// cursor to fetch the next page. No message content is attached: the browse
// list stays cheap, and the detail view lazily fetches the first message on
// demand.
func RecentSessions(ctx context.Context, projectPath string, limit int, archiveFilter, typeFilter, sortOrder, fromTime, toTime, cursor, cursorID string) (*SessionSearchResponse, error) {
	sessions, hasMore, err := service.GetRecentSessions(projectPath, limit, archiveFilter, typeFilter, sortOrder, fromTime, toTime, cursor, cursorID)
	if err != nil {
		return nil, err
	}

	out := make([]SessionSearchResult, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, SessionSearchResult{
			SessionID:    s.ID,
			SessionTitle: s.Title,
			Backend:      s.Backend,
			ProjectPath:  s.ProjectPath,
			Archived:     s.Archived,
			CreatedAt:    s.CreatedAt,
			SessionType:  s.SessionType,
			// No search happened in browse mode — no match count.
			MatchCount: 0,
			Chunks:     []ChunkHit{},
		})
	}

	return &SessionSearchResponse{
		Sessions: out,
		Total:    len(out),
		Mode:     SearchModeRecent,
		HasMore:  hasMore,
	}, nil
}

// RAGSessionSearch performs RAG search and aggregates results by session,
// merged with sessions matched by title.
//
// Two independent channels feed the result: message content (the RAG index) and
// session title (a plain SQL LIKE on chat_sessions). They exist side by side
// because a session the user renamed, or one whose auto-title was truncated,
// cannot be found by the words in its name through the content index. Title
// matches rank ahead of content matches — a user typing a remembered name wants
// that session first — and a session found by both keeps its chunks while also
// carrying the title-match badge.
//
// A nil store (RAG not configured) is not an error here: the title channel is
// pure SQL and still answers, so those installs get name search rather than a
// 503. The response mode then reports "title" to say no content search ran.
func RAGSessionSearch(ctx context.Context, store *Store, embedder *EmbeddingClient, params SearchParams, searchLimit int, searchPoolSize int) (*SessionSearchResponse, error) {
	if params.Query == "" {
		return &SessionSearchResponse{}, nil
	}

	// Title channel. Its filters mirror the content channel's, so the two sets
	// of results are drawn from the same population — including the
	// single-session and excluded-session scopes.
	titleMatches, err := service.SearchSessionsByTitle(
		params.ProjectPath, titleSearchTerms(params.Query), searchLimit,
		params.Archived, params.SessionType, params.FromTime, params.ToTime,
		params.SessionID, params.ExcludeSessionID,
	)
	if err != nil {
		// A failing title channel must not sink a working content search.
		slog.Warn("rag: title search failed, continuing with content only", slog.String("err", err.Error()))
		titleMatches = nil
	}

	// Content channel. Skipped entirely when RAG is unavailable — the title
	// channel above is the whole answer in that case.
	contentSessions, contentMode, err := contentSessionMatches(ctx, store, embedder, params, searchLimit, searchPoolSize)
	if err != nil {
		return nil, err
	}

	sessions := mergeTitleAndContentMatches(titleMatches, contentSessions, params.Query)

	// Sort sessions. Default keeps the title-first, then relevance ordering
	// established by the merge; time orders re-sort the whole result set by the
	// session's creation time, tie-broken by session id.
	sortSessionResults(sessions, params.SortOrder)

	// Truncate to searchLimit
	if len(sessions) > searchLimit {
		sessions = sessions[:searchLimit]
	}

	// Build response. Titles come from the DB, so a title-matched session keeps
	// the exact stored string its offsets were computed against.
	out := make([]SessionSearchResult, len(sessions))
	for i, s := range sessions {
		out[i] = *s
	}

	slog.Info(
		"rag session search completed",
		slog.String("query", params.Query),
		slog.String("mode", string(contentMode)),
		slog.Int("sessions", len(out)),
		slog.Int("title_matches", len(titleMatches)),
		slog.Int("search_limit", searchLimit),
	)

	return &SessionSearchResponse{
		Sessions: out,
		Total:    len(out),
		Mode:     contentMode,
	}, nil
}

// contentSessionMatches runs the content channel and returns the aggregated
// sessions plus the mode to report. A nil store means RAG is not configured:
// there is no content to search, so it returns no sessions and SearchModeTitle
// to say the title channel is the whole answer.
//
// The archive and session-type filters run here, after aggregation, because
// rag_chunks carries neither column — the predicates can only be applied once
// each session's DB metadata has been loaded.
func contentSessionMatches(ctx context.Context, store *Store, embedder *EmbeddingClient, params SearchParams, searchLimit, searchPoolSize int) ([]*SessionSearchResult, SearchMode, error) {
	if store == nil {
		return nil, SearchModeTitle, nil
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
		return nil, "", err
	}

	sessions := aggregateSessionHits(result.Results)
	enrichSessionMeta(sessions)

	// Filter by archive status now that each session's archived flag is known.
	archiveFilter := service.NormalizeSessionArchiveFilter(params.Archived)
	if archiveFilter != service.SessionArchiveFilterAll {
		wantArchived := archiveFilter == service.SessionArchiveFilterArchived
		filtered := sessions[:0]
		for _, s := range sessions {
			if s.Archived == wantArchived {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	// Filter by session type. A session whose row is missing (or whose stored
	// type is empty) counts as 'chat' — the schema default — mirroring how a
	// missing row is treated as active above.
	if dbType := service.SessionTypeDBValue(params.SessionType); dbType != "" {
		filtered := sessions[:0]
		for _, s := range sessions {
			st := s.SessionType
			if st == "" {
				st = "chat"
			}
			if st == dbType {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	return sessions, result.Mode, nil
}

// mergeTitleAndContentMatches folds the two channels into one ordered list:
// title matches first (newest-first, as the title query returned them), then
// content matches in relevance order. A session present in both is emitted once
// in the title block, keeping its chunks so the detail view still shows the
// message hits, and is marked so the client can badge both kinds of match.
//
// Content matches carry no title text — only chunks — so their titles are
// filled in from the DB here. That is also where the title-match offsets are
// computed: they are rune offsets into the stored title, the same coordinate
// space the chunk highlighting uses.
func mergeTitleAndContentMatches(titleMatches []service.RecentSession, contentSessions []*SessionSearchResult, query string) []*SessionSearchResult {
	merged := make([]*SessionSearchResult, 0, len(titleMatches)+len(contentSessions))
	seen := make(map[string]int, len(titleMatches))

	for _, t := range titleMatches {
		s := &SessionSearchResult{
			SessionID:    t.ID,
			SessionTitle: t.Title,
			Backend:      t.Backend,
			ProjectPath:  t.ProjectPath,
			Archived:     t.Archived,
			CreatedAt:    t.CreatedAt,
			SessionType:  t.SessionType,
			TitleMatch:   true,
			TitleOnly:    true,
			// No content search ran for this session, so there is no match count.
			MatchCount: 0,
			Chunks:     []ChunkHit{},
		}
		s.TitleMatchPositions = textMatchPositions(query, t.Title)
		seen[t.ID] = len(merged)
		merged = append(merged, s)
	}

	for _, c := range contentSessions {
		if idx, ok := seen[c.SessionID]; ok {
			// Found by both channels: keep the chunks from the content pass and
			// record that the title matched too.
			existing := merged[idx]
			existing.Chunks = c.Chunks
			existing.MatchCount = c.MatchCount
			existing.Score = c.Score
			existing.TitleOnly = false
			continue
		}
		seen[c.SessionID] = len(merged)
		merged = append(merged, c)
	}

	// Content-only results arrive without a title (the content query carries
	// none), so fill them in — the client renders and highlights titles.
	if missing := titlesMissingFrom(merged); len(missing) > 0 {
		titles := getSessionTitles(missing)
		for _, s := range merged {
			if s.SessionTitle == "" {
				if title, ok := titles[s.SessionID]; ok {
					s.SessionTitle = title
				}
			}
		}
	}

	return merged
}

// titlesMissingFrom collects the IDs of results that still have no title text,
// so they can be fetched in one batch.
func titlesMissingFrom(sessions []*SessionSearchResult) map[string]bool {
	missing := make(map[string]bool)
	for _, s := range sessions {
		if s.SessionTitle == "" {
			missing[s.SessionID] = true
		}
	}
	return missing
}

// titleSearchTerms splits a title query into the terms that must all appear in
// a session title. Segmentation output includes single-character tokens
// (whitespace, punctuation, a lone CJK character); tokens shorter than two
// runes are dropped because matching them would return nearly every session.
// When that leaves nothing — a one-character query, or punctuation only — the
// trimmed query is returned as a single literal term, so the search still
// honors what the user typed rather than silently matching everything.
func titleSearchTerms(query string) []string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}
	terms := make([]string, 0, 4)
	for _, tok := range SegmentTokens(trimmed) {
		tok = strings.TrimSpace(tok)
		if len([]rune(tok)) >= 2 {
			terms = append(terms, tok)
		}
	}
	if len(terms) == 0 {
		return []string{trimmed}
	}
	return terms
}

// aggregateSessionHits groups raw chunk hits by session_id in first-seen order,
// applying a per-session chunk cap and tracking each session's best score and
// match count.
func aggregateSessionHits(hits []SearchHit) []*SessionSearchResult {
	sessionMap := make(map[string]*SessionSearchResult)
	var sessionOrder []string

	for _, hit := range hits {
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

	// Materialize the aggregated sessions in first-seen order.
	sessions := make([]*SessionSearchResult, 0, len(sessionMap))
	for _, id := range sessionOrder {
		sessions = append(sessions, sessionMap[id])
	}
	return sessions
}

// enrichSessionMeta overwrites each session's archived flag, creation time and
// session type with the authoritative DB values. In search mode CreatedAt
// initially comes from the matched chunk; the session-level timestamp is
// authoritative for display and for time sorting.
func enrichSessionMeta(sessions []*SessionSearchResult) {
	metaMap := getSessionMetaBatch(sessionIDSet(sessions))
	for _, s := range sessions {
		if m, ok := metaMap[s.SessionID]; ok {
			s.Archived = m.Archived
			if !m.CreatedAt.IsZero() {
				s.CreatedAt = m.CreatedAt
			}
			s.SessionType = m.SessionType
		}
	}
}

// sessionIDSet collects the distinct session IDs of the given results.
func sessionIDSet(sessions []*SessionSearchResult) map[string]bool {
	ids := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		ids[s.SessionID] = true
	}
	return ids
}

// sortSessionResults sorts sessions in place. The default order puts title
// matches first — a user who typed a remembered name wants that session at the
// top — then orders each block by search-engine relevance (best chunk score
// desc). Time orders re-sort the whole set by session creation time, tie-broken
// by session id; a title match carries no score, so relevance is the only order
// in which it can outrank a content hit.
//
// Equal scores fall back to newest-first, then session id. The newest-first step
// is what preserves the title query's own ordering (all title matches score 0);
// the id step keeps the result deterministic, since sort.Slice is not stable and
// sessions can share both a score and a timestamp.
func sortSessionResults(sessions []*SessionSearchResult, sortOrder string) {
	switch service.NormalizeSessionSortOrder(sortOrder) {
	case service.SessionSortNewest:
		sort.Slice(sessions, func(i, j int) bool {
			if !sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
				return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
			}
			return sessions[i].SessionID < sessions[j].SessionID
		})
	case service.SessionSortOldest:
		sort.Slice(sessions, func(i, j int) bool {
			if !sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
				return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
			}
			return sessions[i].SessionID < sessions[j].SessionID
		})
	default:
		sort.Slice(sessions, func(i, j int) bool {
			if sessions[i].TitleMatch != sessions[j].TitleMatch {
				return sessions[i].TitleMatch
			}
			if sessions[i].Score != sessions[j].Score {
				return sessions[i].Score > sessions[j].Score
			}
			// Within a block of equal scores, newest first. This is what keeps
			// title matches in the newest-first order the title query returned:
			// they all score 0, so without it they would fall through to the id
			// tie-break and come out in UUID order.
			if !sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
				return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
			}
			return sessions[i].SessionID < sessions[j].SessionID
		})
	}
}

// sessionMeta holds the DB-sourced session attributes needed to enrich and
// filter session search results.
type sessionMeta struct {
	Archived    bool
	CreatedAt   time.Time
	SessionType string
}

// getSessionMetaBatch fetches archived status, creation time and session type
// for a set of session IDs in a single query. Missing entries fall back to the
// zero value.
func getSessionMetaBatch(sessionIDs map[string]bool) map[string]sessionMeta {
	meta := make(map[string]sessionMeta, len(sessionIDs))
	if !service.DBReady() || len(sessionIDs) == 0 {
		return meta
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
		"SELECT id, archived, created_at, session_type FROM chat_sessions WHERE id IN ("+placeholders+")", args...,
	)
	if err != nil {
		return meta
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		var archived int
		var createdAt time.Time
		var sessionType string
		if err := rows.Scan(&id, &archived, &createdAt, &sessionType); err == nil {
			meta[id] = sessionMeta{Archived: archived == 1, CreatedAt: createdAt, SessionType: sessionType}
		}
	}
	return meta
}
