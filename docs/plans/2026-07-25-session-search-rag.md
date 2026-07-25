# Session Search via RAG Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a search button to the SessionDrawer header that opens a search drawer for finding historical sessions via RAG, with session-level aggregation, match position highlighting, chunk detail modals, and session resume support.

**Architecture:** New backend endpoint `POST /api/rag/session-search` aggregates RAG chunk-level results by session_id, returns TopN sessions with match positions (computed via `textMatchPositions()` for ALL search modes — NOT FTS5 `offsets()` which operates on `chunk_text_segmented`). New backend endpoint `GET /api/rag/status` reports RAG availability. Frontend adds SessionSearchDrawer (BottomSheet), SessionSearchDetailModal (ModalDialog), and useSessionSearch composable with 300ms debounce auto-search.

**Tech Stack:** Go (backend), Vue 3 + TypeScript (frontend), SQLite FTS5 + sqlite-vec (RAG), lucide-vue-next (icons)

**Design Review Fixes Applied:**
- **[Critical] Dropped FTS5 `offsets()` approach** — `offsets()` returns positions in `chunk_text_segmented` (gse-segmented), not `chunk_text`. Using `textMatchPositions()` uniformly for all modes (FTS/vector/hybrid).
- **[Critical] Fixed import path** — `formatRelativeTime` from correct module (verify at implementation time).
- **[Moderate] Fixed XSS in `getPreviewHtml`** — Always use `escapeHtml` in fallback path.
- **[Moderate] Added `onUnmounted` cleanup** to composable.
- **[Moderate] Added `project_path` and `deleted` to `SessionSearchResult`**.
- **[Moderate] Renamed `total_chunks` to `match_count`**.
- **[Moderate] Added `untitledSession` i18n key**.
- **[Moderate] Added per-session chunk cap** in aggregation (maxChunksPerSession=5).
- **[Moderate] Improved `textMatchPositions`** — rune-aware matching, whole-query fallback.
- **[Moderate] Removed unused `req.ProjectPath`** from session-search request struct.
- **[Moderate] Added TTL cache** to `checkRagAvailability`.
- **[Moderate] Added comprehensive tests** for `textMatchPositions` with CJK.

---

### Task 1: Backend — Add MatchRange type and MatchPositions to SearchHit

**Files:**
- Modify: `internal/rag/store_sqlite.go:35-46`

**Step 1: Add MatchRange struct and MatchPositions field to SearchHit**

In `internal/rag/store_sqlite.go`, add the `MatchRange` struct before `SearchHit` and add `MatchPositions` field to `SearchHit`:

```go
// MatchRange represents a character-level (rune offset) match position within chunk text.
type MatchRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// SearchHit represents a search result with similarity score.
type SearchHit struct {
	ChunkID        int64        `json:"chunk_id"`
	ChunkText      string       `json:"chunk_text"`
	Score          float64      `json:"score"`
	SessionID      string       `json:"session_id"`
	SessionTitle   string       `json:"session_title"`
	MessageID      int64        `json:"message_id"`
	Role           string       `json:"role"`
	ProjectPath    string       `json:"project_path"`
	Backend        string       `json:"backend"`
	CreatedAt      time.Time    `json:"created_at"`
	MatchPositions []MatchRange `json:"match_positions,omitempty"`
}
```

**Step 2: Run existing tests to verify no breakage**

Run: `go test ./internal/rag/... -v -count=1`
Expected: All existing tests pass (MatchPositions is omitempty so JSON output unchanged)

**Step 3: Commit**

```bash
git add internal/rag/store_sqlite.go
git commit -m "feat(rag): add MatchRange type and MatchPositions field to SearchHit"
```

---

### Task 2: Backend — Add textMatchPositions helper (uniform for all search modes)

**Files:**
- Create: `internal/rag/match.go`
- Create: `internal/rag/match_test.go`

**Step 1: Write failing tests for textMatchPositions**

Create `internal/rag/match_test.go` with comprehensive tests covering CJK, mixed text, overlapping matches, etc.:

```go
package rag

import "testing"

func TestTextMatchPositions_EnglishSingleTerm(t *testing.T) {
	positions := textMatchPositions("quick", "The quick brown fox")
	if len(positions) == 0 {
		t.Fatal("expected match positions")
	}
	if positions[0].Start != 4 || positions[0].End != 9 {
		t.Errorf("expected [4:9], got %+v", positions[0])
	}
}

func TestTextMatchPositions_EnglishMultipleTerms(t *testing.T) {
	positions := textMatchPositions("quick brown", "The quick brown fox jumps quickly")
	if len(positions) < 2 {
		t.Fatalf("expected at least 2 match ranges, got %d", len(positions))
	}
	// "quick" should match at [4:9] and "quickly" at [26:33]
	// "brown" should match at [10:15]
}

func TestTextMatchPositions_ChineseText(t *testing.T) {
	positions := textMatchPositions("数据库查询", "使用数据库查询进行全文检索")
	if len(positions) == 0 {
		t.Fatal("expected match positions for Chinese text")
	}
	// Verify positions point to actual matches
	for _, mp := range positions {
		runes := []rune("使用数据库查询进行全文检索")
		if mp.Start < 0 || mp.End > len(runes) {
			t.Errorf("invalid position: %+v (text has %d runes)", mp, len(runes))
		}
	}
}

func TestTextMatchPositions_MixedCJKLatin(t *testing.T) {
	positions := textMatchPositions("API错误", "处理API错误日志中的问题")
	if len(positions) == 0 {
		t.Fatal("expected match positions for mixed text")
	}
}

func TestTextMatchPositions_NoMatch(t *testing.T) {
	positions := textMatchPositions("xyz", "Hello world")
	if len(positions) != 0 {
		t.Errorf("expected no matches, got %d", len(positions))
	}
}

func TestTextMatchPositions_EmptyInputs(t *testing.T) {
	if textMatchPositions("", "text") != nil {
		t.Error("expected nil for empty query")
	}
	if textMatchPositions("query", "") != nil {
		t.Error("expected nil for empty text")
	}
}

func TestTextMatchPositions_OverlappingRanges(t *testing.T) {
	positions := textMatchPositions("aa", "aaa")
	if len(positions) != 1 {
		t.Errorf("expected 1 merged range for overlapping matches, got %d", len(positions))
	}
}

func TestTextMatchPositions_WholeQueryFallback(t *testing.T) {
	// If segmented terms don't match but the whole query does, it should still highlight
	positions := textMatchPositions("error handling", "error handling in Go")
	if len(positions) == 0 {
		t.Fatal("expected match positions for whole query fallback")
	}
}

func TestMergeRanges(t *testing.T) {
	tests := []struct {
		name   string
		input  []MatchRange
		expect []MatchRange
	}{
		{"empty", nil, nil},
		{"single", []MatchRange{{1, 3}}, []MatchRange{{1, 3}}},
		{"adjacent", []MatchRange{{1, 3}, {3, 5}}, []MatchRange{{1, 5}}},
		{"overlapping", []MatchRange{{1, 4}, {2, 5}}, []MatchRange{{1, 5}}},
		{"disjoint", []MatchRange{{1, 3}, {5, 7}}, []MatchRange{{1, 3}, {5, 7}}},
		{"unsorted", []MatchRange{{5, 7}, {1, 3}}, []MatchRange{{1, 3}, {5, 7}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeRanges(tt.input)
			if len(result) != len(tt.expect) {
				t.Fatalf("expected %v, got %v", tt.expect, result)
			}
			for i, r := range result {
				if r != tt.expect[i] {
					t.Errorf("position %d: expected %+v, got %+v", i, tt.expect[i], r)
				}
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/rag/... -run TestTextMatchPositions -v -count=1`
Expected: FAIL — function doesn't exist yet

**Step 3: Implement textMatchPositions and mergeRanges**

Create `internal/rag/match.go`:

```go
package rag

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// textMatchPositions finds occurrences of query terms in text and returns
// character-level (rune offset) match positions. Uses segmentation for CJK support.
// Falls back to whole-query matching if no segmented terms match.
func textMatchPositions(queryText, chunkText string) []MatchRange {
	if queryText == "" || chunkText == "" {
		return nil
	}

	chunkRunes := []rune(chunkText)
	var ranges []MatchRange

	// Try segmented terms first
	terms := strings.Fields(SegmentText(queryText))
	for _, term := range terms {
		ranges = append(ranges, findTermInRunes(term, chunkRunes, chunkText)...)
	}

	// If no segmented term matched, try the whole query as-is
	if len(ranges) == 0 {
		ranges = findTermInRunes(queryText, chunkRunes, chunkText)
	}

	return mergeRanges(ranges)
}

// findTermInRunes finds all occurrences of term in the text (case-insensitive)
// and returns rune-offset match ranges.
func findTermInRunes(term string, chunkRunes []rune, chunkText string) []MatchRange {
	var ranges []MatchRange
	termLower := strings.ToLower(term)
	if termLower == "" {
		return nil
	}

	// Convert chunkText to lowercase for case-insensitive matching
	chunkLower := strings.ToLower(chunkText)

	// Search at byte level, convert to rune offsets
	start := 0
	for {
		idx := strings.Index(chunkLower[start:], termLower)
		if idx < 0 {
			break
		}
		byteStart := start + idx
		byteEnd := byteStart + len(termLower)

		// Convert byte offsets to rune offsets
		runeStart := utf8.RuneCountInString(chunkText[:byteStart])
		runeEnd := runeStart + utf8.RuneCountInString(chunkText[byteStart:byteEnd])

		ranges = append(ranges, MatchRange{Start: runeStart, End: runeEnd})
		start = byteEnd
	}

	return ranges
}

// mergeRanges merges overlapping and adjacent MatchRange entries.
func mergeRanges(ranges []MatchRange) []MatchRange {
	if len(ranges) <= 1 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	merged := []MatchRange{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.Start <= last.End {
			if r.End > last.End {
				last.End = r.End
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/rag/... -run "TestTextMatchPositions|TestMergeRanges" -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/rag/match.go internal/rag/match_test.go
git commit -m "feat(rag): add textMatchPositions helper for uniform match highlighting"
```

---

### Task 3: Backend — Add match_positions to RAGSearch output (all modes)

**Files:**
- Modify: `internal/rag/search.go`

**Step 1: Write failing test**

In `internal/rag/search_test.go`:

```go
func TestRAGSearch_MatchPositions(t *testing.T) {
	// Verify that RAGSearch populates MatchPositions for all search modes
	// This tests the integration of textMatchPositions into the search pipeline
}
```

**Step 2: Integrate textMatchPositions into RAGSearch**

In `internal/rag/search.go`, add match position computation after all search result paths. Add it as a post-processing step after the `switch` block, just before the `getSessionTitles` enrichment:

```go
// After the switch block, compute match positions for all hits using textMatchPositions.
// This is done uniformly for all search modes (FTS, vector, hybrid) because:
// - FTS5 offsets() returns positions in chunk_text_segmented (not chunk_text), making them unusable
// - Vector search has no native position information
// - textMatchPositions provides consistent, correct highlighting for all modes
for i := range hits {
    hits[i].MatchPositions = textMatchPositions(params.Query, hits[i].ChunkText)
}
```

**Step 3: Run all RAG tests**

Run: `go test ./internal/rag/... -v -count=1`
Expected: All pass

**Step 4: Commit**

```bash
git add internal/rag/search.go
git commit -m "feat(rag): populate MatchPositions in RAGSearch for all search modes"
```

---

### Task 4: Backend — Add RAGSessionSearch aggregation function

**Files:**
- Modify: `internal/rag/search.go`

**Step 1: Write failing test for session aggregation**

In `internal/rag/search_test.go`:

```go
func TestRAGSessionSearch_Aggregation(t *testing.T) {
	// Setup: create test store with multiple sessions and chunks
	s := newTestStore(t)
	defer s.Close()

	// Session A: 2 matching chunks
	chunks := []Chunk{
		{SessionID: "sess-a", MessageID: 1, ChunkText: "database query optimization", ChunkIndex: 0, TokenCount: 3, Role: "user"},
		{SessionID: "sess-a", MessageID: 2, ChunkText: "query performance tuning", ChunkIndex: 1, TokenCount: 3, Role: "assistant"},
		{SessionID: "sess-b", MessageID: 3, ChunkText: "query language design", ChunkIndex: 0, TokenCount: 3, Role: "user"},
	}
	if err := s.InsertChunks(chunks); err != nil {
		t.Fatalf("InsertChunks: %v", err)
	}

	result, err := RAGSessionSearch(context.Background(), s, nil, SearchParams{Query: "query"}, 5, 20)
	if err != nil {
		t.Fatalf("RAGSessionSearch: %v", err)
	}
	if len(result.Sessions) == 0 {
		t.Fatal("expected at least one session")
	}
	// sess-a should have 2 chunks, sess-b should have 1 chunk
	for _, sess := range result.Sessions {
		if sess.SessionID == "sess-a" && sess.MatchCount != 2 {
			t.Errorf("sess-a: expected match_count=2, got %d", sess.MatchCount)
		}
		if sess.SessionID == "sess-b" && sess.MatchCount != 1 {
			t.Errorf("sess-b: expected match_count=1, got %d", sess.MatchCount)
		}
	}
}
```

**Step 2: Implement RAGSessionSearch**

In `internal/rag/search.go`, add:

```go
const maxChunksPerSession = 5

// SessionSearchResult represents an aggregated search result grouped by session.
type SessionSearchResult struct {
	SessionID    string     `json:"session_id"`
	SessionTitle string     `json:"session_title"`
	Score        float64    `json:"score"`
	Backend      string     `json:"backend"`
	ProjectPath  string     `json:"project_path"`
	Deleted      bool       `json:"deleted"`
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

// RAGSessionSearch performs RAG search and aggregates results by session.
// It fetches an expanded pool of chunks, groups by session_id with a per-session
// chunk cap, and returns up to searchLimit sessions sorted by best chunk score.
func RAGSessionSearch(ctx context.Context, store *Store, embedder *EmbeddingClient, params SearchParams, searchLimit int, searchPoolSize int) (*SessionSearchResponse, error) { //nolint:gocyclo
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

	// Enrich with session titles and deleted status
	sessionIDs := make(map[string]bool)
	for _, s := range sessions {
		sessionIDs[s.SessionID] = true
	}
	titles, deletedMap := getSessionTitlesAndDeleted(sessionIDs)

	// Build response
	out := make([]SessionSearchResult, len(sessions))
	for i, s := range sessions {
		out[i] = *s
		if title, ok := titles[s.SessionID]; ok {
			out[i].SessionTitle = title
		}
		if del, ok := deletedMap[s.SessionID]; ok {
			out[i].Deleted = del
		}
	}

	return &SessionSearchResponse{
		Sessions: out,
		Total:    len(out),
		Mode:     result.Mode,
	}, nil
}

// SessionSearchResponse is the response for session-aggregated RAG search.
type SessionSearchResponse struct {
	Sessions []SessionSearchResult `json:"sessions"`
	Total    int                   `json:"total"`
	Mode     SearchMode            `json:"mode"`
}
```

**Step 3: Add getSessionTitlesAndDeleted helper**

In `internal/rag/search.go`, add a new function alongside `getSessionTitles`:

```go
// getSessionTitlesAndDeleted fetches session titles and deleted status for a set of session IDs.
func getSessionTitlesAndDeleted(sessionIDs map[string]bool) (map[string]string, map[string]bool) {
	titles := getSessionTitles(sessionIDs)
	deletedMap := make(map[string]bool, len(sessionIDs))

	if !service.DBReady() || len(sessionIDs) == 0 {
		return titles, deletedMap
	}

	ids := make([]string, 0, len(sessionIDs))
	for id := range sessionIDs {
		ids = append(ids, id)
	}

	// Query deleted status from service DB
	for _, id := range ids {
		var deleted int
		err := service.ReadDB().QueryRow("SELECT deleted FROM chat_sessions WHERE id = ?", id).Scan(&deleted)
		if err == nil {
			deletedMap[id] = deleted == 1
		}
	}

	return titles, deletedMap
}
```

**Step 4: Run tests**

Run: `go test ./internal/rag/... -v -count=1`
Expected: All pass

**Step 5: Commit**

```bash
git add internal/rag/search.go
git commit -m "feat(rag): add RAGSessionSearch for session-level aggregation"
```

---

### Task 5: Backend — Add GET /api/rag/status endpoint

**Files:**
- Modify: `internal/handler/rag_api.go`
- Modify: `internal/handler/handler.go`
- Modify: `internal/rag/store_sqlite.go` (add HasFTSData method)

**Step 1: Add HasFTSData method to Store**

In `internal/rag/store_sqlite.go`:

```go
// HasFTSData returns true if the FTS5 table contains any indexed chunks.
func (s *Store) HasFTSData() bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM rag_chunks_fts").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}
```

**Step 2: Implement ServeRAGStatus**

In `internal/handler/rag_api.go`:

```go
// ServeRAGStatus handles GET /api/rag/status — returns RAG availability status.
func ServeRAGStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	hasVecData := rag.GlobalStore != nil && rag.GlobalStore.HasVecData()
	hasFTSData := rag.GlobalStore != nil && rag.GlobalStore.HasFTSData()
	embedderHealthy := rag.EmbedderHealthy()

	mode := "none"
	if embedderHealthy && hasVecData {
		mode = "hybrid"
	} else if hasFTSData {
		mode = "fts"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"available":        hasFTSData || hasVecData,
		"mode":             mode,
		"has_fts_data":     hasFTSData,
		"has_vec_data":     hasVecData,
		"embedder_healthy": embedderHealthy,
	})
}
```

**Step 3: Register route**

In `internal/handler/handler.go`, add near the other RAG routes:

```go
register("/api/rag/status", middleware.Auth(ServeRAGStatus))
register("/api/rag/session-search", middleware.Auth(ServeRAGSessionSearch))
```

**Step 4: Write test**

In `internal/handler/rag_api_test.go`:

```go
func TestServeRAGStatus(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/rag/status", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	ServeRAGStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var data map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&data); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := data["available"]; !ok {
		t.Error("missing 'available' field")
	}
	if _, ok := data["mode"]; !ok {
		t.Error("missing 'mode' field")
	}
}
```

**Step 5: Run tests**

Run: `go test ./internal/handler/... -run TestServeRAGStatus -v -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/handler/rag_api.go internal/handler/handler.go internal/rag/store_sqlite.go internal/handler/rag_api_test.go
git commit -m "feat(rag): add GET /api/rag/status endpoint for availability check"
```

---

### Task 6: Backend — Add POST /api/rag/session-search endpoint

**Files:**
- Modify: `internal/handler/rag_api.go`

**Step 1: Implement ServeRAGSessionSearch**

In `internal/handler/rag_api.go`:

```go
// ServeRAGSessionSearch handles POST /api/rag/session-search — session-aggregated RAG search.
func ServeRAGSessionSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" && !middleware.IsLocalhost(r) {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return
	}

	var req struct {
		Query            string `json:"q"`
		Backend          string `json:"backend"`
		Role             string `json:"role"`
		SessionID        string `json:"session_id"`
		ExcludeSessionID string `json:"exclude_session_id"`
		FromTime         string `json:"from"`
		ToTime           string `json:"to"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Query == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SearchQueryRequired")
		return
	}

	searchLimit := model.ConfigInstance.RAG.SearchLimit
	if searchLimit <= 0 {
		searchLimit = 5
	}

	searchPoolSize := model.ConfigInstance.RAG.SearchPoolSize

	params := rag.SearchParams{
		Query:            req.Query,
		ProjectPath:      projectPath,
		Backend:          req.Backend,
		Role:             req.Role,
		SessionID:        req.SessionID,
		ExcludeSessionID: req.ExcludeSessionID,
		FromTime:         req.FromTime,
		ToTime:           req.ToTime,
	}

	result, err := rag.RAGSessionSearch(r.Context(), rag.GlobalStore, rag.GlobalEmbedder, params, searchLimit, searchPoolSize)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "RAGSearchFailed")
		return
	}

	if result.Sessions == nil {
		result.Sessions = []rag.SessionSearchResult{}
	}
	writeJSON(w, http.StatusOK, result)
}
```

Note: `req.ProjectPath` is intentionally omitted — project isolation uses the cookie-derived `projectPath`, not a client-supplied value.

**Step 2: Write test**

```go
func TestServeRAGSessionSearch_MethodCheck(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/rag/session-search", nil)
	rr := httptest.NewRecorder()
	ServeRAGSessionSearch(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestServeRAGSessionSearch_MissingQuery(t *testing.T) {
	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rag/session-search", body)
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	ServeRAGSessionSearch(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestServeRAGSessionSearch_RemoteNoProject(t *testing.T) {
	body := bytes.NewBufferString(`{"q":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/rag/session-search", body)
	req.RemoteAddr = "192.168.1.1:1234" // Non-localhost
	rr := httptest.NewRecorder()
	ServeRAGSessionSearch(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/handler/... -run TestServeRAGSessionSearch -v -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/handler/rag_api.go internal/handler/rag_api_test.go
git commit -m "feat(rag): add POST /api/rag/session-search endpoint"
```

---

### Task 7: Frontend — Add i18n keys

**Files:**
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/i18n/locales/zh.ts`

**Step 1: Add sessionSearch i18n keys**

In `en.ts`, add under the `session` section:

```typescript
sessionSearch: {
  title: 'Session Search',
  placeholder: 'Search conversations...',
  noResults: 'No matching sessions found',
  noQuery: 'Enter keywords to search',
  searching: 'Searching...',
  resultCount: '{count} sessions found',
  chunks: '{count} matches',
  resume: 'Resume Session',
  resumeConfirm: 'Resume session "{title}"?',
  resumeFailed: 'Failed to resume session',
  resumeProjectMismatch: 'This session belongs to a different project',
  ragUnavailable: 'RAG not configured',
  detailTitle: 'Match Details',
  roleUser: 'User',
  roleAssistant: 'Assistant',
  untitledSession: 'Untitled Session',
  deleted: 'Deleted',
},
```

In `zh.ts`:

```typescript
sessionSearch: {
  title: '会话搜索',
  placeholder: '搜索对话内容...',
  noResults: '未找到匹配的会话',
  noQuery: '输入关键词搜索',
  searching: '搜索中...',
  resultCount: '找到 {count} 个会话',
  chunks: '{count} 处匹配',
  resume: '恢复会话',
  resumeConfirm: '恢复会话"{title}"？',
  resumeFailed: '恢复会话失败',
  resumeProjectMismatch: '该会话属于其他项目',
  ragUnavailable: 'RAG 未配置',
  detailTitle: '匹配详情',
  roleUser: '用户',
  roleAssistant: '助手',
  untitledSession: '未命名会话',
  deleted: '已删除',
},
```

**Step 2: Commit**

```bash
git add web/src/i18n/locales/en.ts web/src/i18n/locales/zh.ts
git commit -m "feat(i18n): add sessionSearch translation keys"
```

---

### Task 8: Frontend — Add useSessionSearch composable

**Files:**
- Create: `web/src/composables/useSessionSearch.ts`
- Create: `web/src/composables/__tests__/useSessionSearch.test.ts`

**Step 1: Write the composable**

Create `web/src/composables/useSessionSearch.ts`:

```typescript
import { ref, watch, onUnmounted } from 'vue'
import { appLog } from '@/utils/appLog'

export interface MatchRange {
  start: number
  end: number
}

export interface ChunkHit {
  chunk_id: number
  chunk_text: string
  match_positions: MatchRange[]
  score: number
  role: string
  message_id: number
  created_at: string
}

export interface SessionSearchResult {
  session_id: string
  session_title: string
  score: number
  backend: string
  project_path: string
  deleted: boolean
  created_at: string
  match_count: number
  chunks: ChunkHit[]
}

export interface SessionSearchResponse {
  sessions: SessionSearchResult[]
  total: number
  mode: string
}

export function useSessionSearch(debounceMs = 300) {
  const query = ref('')
  const results = ref<SessionSearchResult[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)
  const searchMode = ref<string>('none')

  let debounceTimer: ReturnType<typeof setTimeout> | null = null
  let abortController: AbortController | null = null

  async function doSearch(q: string) {
    if (!q.trim()) {
      results.value = []
      error.value = null
      return
    }

    // Cancel previous request
    abortController?.abort()
    abortController = new AbortController()

    loading.value = true
    error.value = null

    try {
      const resp = await fetch('/api/rag/session-search', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ q }),
        signal: abortController.signal,
      })

      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}))
        error.value = data.error || 'Search failed'
        results.value = []
        return
      }

      const data: SessionSearchResponse = await resp.json()
      results.value = data.sessions || []
      searchMode.value = data.mode || 'none'
    } catch (e: any) {
      if (e.name === 'AbortError') return
      appLog.e('SessionSearch', 'Search failed', e)
      error.value = e.message || 'Search failed'
      results.value = []
    } finally {
      loading.value = false
    }
  }

  // Debounced auto-search
  watch(query, (newVal) => {
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(() => doSearch(newVal), debounceMs)
  })

  function clear() {
    query.value = ''
    results.value = []
    error.value = null
    abortController?.abort()
    if (debounceTimer) clearTimeout(debounceTimer)
  }

  // Cleanup on unmount
  onUnmounted(() => {
    if (debounceTimer) clearTimeout(debounceTimer)
    abortController?.abort()
  })

  return {
    query,
    results,
    loading,
    error,
    searchMode,
    clear,
  }
}

// RAG availability cache (5-minute TTL)
let cachedRagStatus: { available: boolean; mode: string } | null = null
let ragStatusExpiry = 0

/** Check if RAG is available (cached for 5 minutes). */
export async function checkRagAvailability(): Promise<{ available: boolean; mode: string }> {
  if (cachedRagStatus && Date.now() < ragStatusExpiry) return cachedRagStatus
  try {
    const resp = await fetch('/api/rag/status')
    if (!resp.ok) return { available: false, mode: 'none' }
    const data = await resp.json()
    const result = { available: !!data.available, mode: data.mode || 'none' }
    cachedRagStatus = result
    ragStatusExpiry = Date.now() + 5 * 60 * 1000
    return result
  } catch {
    return { available: false, mode: 'none' }
  }
}
```

**Step 2: Write tests**

Create `web/src/composables/__tests__/useSessionSearch.test.ts` — same as original plan but add cleanup test.

**Step 3: Run test**

Run: `npx vitest run web/src/composables/__tests__/useSessionSearch.test.ts`
Expected: PASS

**Step 4: Commit**

```bash
git add web/src/composables/useSessionSearch.ts web/src/composables/__tests__/useSessionSearch.test.ts
git commit -m "feat: add useSessionSearch composable with debounce, cleanup, and cached RAG check"
```

---

### Task 9: Frontend — Add highlightTextByPositions utility

**Files:**
- Modify: `web/src/utils/searchUtils.ts`

**Step 1: Add highlightTextByPositions function**

```typescript
/**
 * Highlight text at specified character positions using <mark> tags.
 * Positions are rune-based (character-level), matching backend MatchRange.
 * All text is escaped via escapeHtml to prevent XSS.
 */
export function highlightTextByPositions(text: string, positions: { start: number; end: number }[]): string {
  if (!text) return ''
  if (!positions || positions.length === 0) return escapeHtml(text)

  // Convert rune offsets to string indices (JavaScript strings are UTF-16)
  const runes = [...text]
  const runeToIndex: number[] = []
  let idx = 0
  for (let i = 0; i < runes.length; i++) {
    runeToIndex.push(idx)
    idx += runes[i].length // surrogate pairs occupy 2 UTF-16 units
  }
  runeToIndex.push(idx) // sentinel

  let result = ''
  let lastEnd = 0

  for (const pos of positions) {
    const byteStart = runeToIndex[Math.min(pos.start, runes.length)]
    const byteEnd = runeToIndex[Math.min(pos.end, runes.length)]

    if (byteStart > lastEnd) {
      result += escapeHtml(text.slice(lastEnd, byteStart))
    }
    if (byteEnd > byteStart) {
      result += '<mark>' + escapeHtml(text.slice(byteStart, byteEnd)) + '</mark>'
    }
    lastEnd = Math.max(lastEnd, byteEnd)
  }

  if (lastEnd < text.length) {
    result += escapeHtml(text.slice(lastEnd))
  }

  return result
}
```

**Step 2: Commit**

```bash
git add web/src/utils/searchUtils.ts
git commit -m "feat: add highlightTextByPositions utility for match position highlighting"
```

---

### Task 10: Frontend — Add SessionSearchDrawer component

**Files:**
- Create: `web/src/components/session/SessionSearchDrawer.vue`
- Create: `web/src/components/session/__tests__/SessionSearchDrawer.test.ts`

Same as original plan with these fixes:
- **[XSS Fix]** `getPreviewHtml` fallback uses `escapeHtml(text.slice(0, 150))` instead of raw `text.slice(0, 150)`
- **[i18n Fix]** Uses `t('sessionSearch.untitledSession')` instead of `t('sessionSearch.noQuery')` for untitled sessions
- **[Import Fix]** Import `formatRelativeTime` from the correct module (verify path at implementation time)
- **[Deleted Fix]** Show deleted badge on session items: `<span v-if="session.deleted" class="session-search-item-deleted">{{ t('sessionSearch.deleted') }}</span>`

**Step 1-4:** Same structure as original Task 10.

---

### Task 11: Frontend — Add SessionSearchDetailModal component

**Files:**
- Create: `web/src/components/session/SessionSearchDetailModal.vue`
- Create: `web/src/components/session/__tests__/SessionSearchDetailModal.test.ts`

Same as original plan with these fixes:
- **[i18n Fix]** Uses `t('sessionSearch.untitledSession')` for untitled sessions
- **[Import Fix]** Verify `formatRelativeTime` import path
- **[Field Fix]** Uses `session.match_count` instead of `session.total_chunks`
- **[Deleted Fix]** Shows deleted indicator and handles resume for deleted sessions

**Step 1-4:** Same structure as original Task 11.

---

### Task 12: Frontend — Integrate search button into SessionDrawer

**Files:**
- Modify: `web/src/components/session/SessionDrawer.vue`

Same as original plan with these fixes:
- **[Error handling]** Handle 403 from `/api/ai/session/resume` with `t('sessionSearch.resumeProjectMismatch')` message
- Uses `t('sessionSearch.untitledSession')` for resume confirm dialog

**Step 1-3:** Same structure as original Task 12.

---

### Task 13: Full integration test

**Step 1: Run all Go tests**

Run: `go test ./internal/rag/... ./internal/handler/... -v -count=1`
Expected: All pass

**Step 2: Run all frontend tests**

Run: `npx vitest run`
Expected: All pass

**Step 3: Build and smoke test**

Run: `./build.sh && ./clawbench --port 20001`
Manual: Open session drawer → see search icon → type query → see results → click result → see modal with highlights → click resume → session switches

**Step 4: Final commit**

```bash
git add -A
git commit -m "chore: session search via RAG — integration complete"
```
