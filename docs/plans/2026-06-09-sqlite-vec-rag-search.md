# sqlite-vec RAG Vector Search Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the in-memory `VectorCache` brute-force cosine similarity search with `sqlite-vec`'s native `vec0` virtual table for vector indexing, eliminating the need to load all embeddings into memory and enabling scalable KNN queries.

**Architecture:** Upgrade `modernc.org/sqlite` from v1.34.5 to v1.52.0 (which includes the `vec` sub-package with sqlite-vec compiled via ccgo). Create a `vec0` virtual table (`rag_vec`) alongside the existing `rag_chunks` table. Vector search moves from `VectorCache` (in-memory O(n) scan) to `vec0` KNN (`WHERE embedding MATCH ? AND k = N`). The `VectorCache` struct and its related code are removed. FTS5 search stays as-is. Hybrid search uses RRF over both `vec0` KNN and FTS5 BM25 results.

**Tech Stack:** Go, `modernc.org/sqlite` v1.52.0 (pure Go, includes `vec` sub-package with sqlite-vec v0.1.9), `vec0` virtual table with `distance_metric=cosine`, float32 vectors (4 bytes per dim instead of current float64 8 bytes)

---

## Key Design Decisions

1. **`vec0` table uses `rowid` = `rag_chunks.id`** — direct 1:1 mapping, no separate JOIN needed
2. **Cosine distance metric** — matches current `cosineSimilarity()` behavior
3. **Float32 format** — `vec0` stores `float[1024]` (4096 bytes per embedding vs current 8192 bytes)
4. **Embedding conversion** — `[]float64` → `[]float32` at insert time (loss of precision is negligible for BGE-M3 embeddings)
5. **`rag_chunks.embedding` BLOB column removed** — vector data lives only in `vec0` shadow tables; `has_embedding` flag and `embedding_dim` remain for query planning
6. **No `VectorCache`** — all vector queries go through `vec0` KNN; no in-memory cache, no dirty flag, no reload
7. **Metadata columns on `vec0`** — `project_path`, `backend`, `role` as metadata for pre-filtering in KNN; `session_id` as metadata for include/exclude filtering
8. **Time filters** — applied as post-filter on KNN results via JOIN back to `rag_chunks`
9. **Migration** — schema version bump; existing `rag_chunks.embedding` data migrated to `vec0`; if dimension changes, full reset (same as current behavior)

## Schema Changes

### New `vec0` virtual table:
```sql
CREATE VIRTUAL TABLE rag_vec USING vec0(
    embedding float[1024] distance_metric=cosine,
    project_path TEXT,
    backend TEXT,
    role TEXT,
    session_id TEXT
);
```

The `rag_vec` table uses `rowid` matching `rag_chunks.id` for direct correspondence.

### Removed columns:
- `rag_chunks.embedding` BLOB column — vector data now lives in `rag_vec`'s shadow tables
- `rag_chunks.embedding_dim` column — dimension is stored in `rag_vec_info` shadow table

### Added columns:
- None — metadata already exists in `rag_chunks`

### Kept columns:
- `rag_chunks.has_embedding` — flag for backfill tracking (0 = pending, 1 = embedded)

---

## Task 1: Upgrade modernc.org/sqlite and verify sqlite-vec works

**Files:**
- Modify: `go.mod`
- Create: `internal/rag/vec_test.go` (temp verification test)

**Step 1: Upgrade dependency**

```bash
cd /home/xulongzhe/projects/clawbench
go get modernc.org/sqlite@v1.52.0
go mod tidy
```

**Step 2: Write verification test**

Create `internal/rag/vec_test.go`:

```go
package rag

import (
	"database/sql"
	"encoding/binary"
	"math"
	"testing"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"

	"github.com/stretchr/testify/require"
)

func TestSqliteVec_Available(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	var version string
	err = db.QueryRow("SELECT vec_version()").Scan(&version)
	require.NoError(t, err, "vec_version() should be available")
	t.Logf("sqlite-vec version: %s", version)
}

func TestSqliteVec_CreateVec0Table(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE VIRTUAL TABLE test_vec USING vec0(
		embedding float[4] distance_metric=cosine
	)`)
	require.NoError(t, err, "should be able to create vec0 table")
}

func TestSqliteVec_InsertAndQuery(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE VIRTUAL TABLE test_vec USING vec0(
		embedding float[4] distance_metric=cosine,
		category TEXT
	)`)
	require.NoError(t, err)

	// Insert vectors
	vec1 := serializeFloat32([]float32{1.0, 0.0, 0.0, 0.0})
	vec2 := serializeFloat32([]float32{0.0, 1.0, 0.0, 0.0})
	vec3 := serializeFloat32([]float32{0.9, 0.1, 0.0, 0.0})

	_, err = db.Exec("INSERT INTO test_vec(rowid, embedding, category) VALUES (?, ?, ?)", 1, vec1, "a")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO test_vec(rowid, embedding, category) VALUES (?, ?, ?)", 2, vec2, "b")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO test_vec(rowid, embedding, category) VALUES (?, ?, ?)", 3, vec3, "a")
	require.NoError(t, err)

	// KNN query
	query := serializeFloat32([]float32{1.0, 0.0, 0.0, 0.0})
	rows, err := db.Query(`
		SELECT rowid, distance, category
		FROM test_vec
		WHERE embedding MATCH ? AND k = 3
		ORDER BY distance
	`, query)
	require.NoError(t, err)
	defer rows.Close()

	var results []int64
	for rows.Next() {
		var id int64
		var dist float64
		var cat string
		require.NoError(t, rows.Scan(&id, &dist, &cat))
		results = append(results, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{1, 3, 2}, results, "cosine KNN should return nearest vectors first")
}

// serializeFloat32 converts []float32 to little-endian byte slice for vec0 BLOB storage.
func serializeFloat32(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}
```

**Step 3: Run verification tests**

```bash
go test ./internal/rag/ -run "TestSqliteVec" -v -count=1
```

Expected: All 3 tests PASS

**Step 4: Commit**

```bash
git add go.mod go.sum internal/rag/vec_test.go
git commit -m "chore: upgrade modernc.org/sqlite to v1.52.0 with sqlite-vec support"
```

---

## Task 2: Add vector serialization helpers and remove VectorCache

**Files:**
- Modify: `internal/rag/vector_cache.go` → DELETE entire file
- Modify: `internal/rag/store_sqlite.go` — remove `cache *VectorCache` from Store, add `serializeFloat32`/`float64ToFloat32` helpers

**Step 1: Write failing tests for new helpers**

Add to `internal/rag/vec_test.go`:

```go
func TestSerializeFloat32_Roundtrip(t *testing.T) {
	original := []float32{0.1, -0.2, 0.3, 1.5, -99.9}
	blob := serializeFloat32(original)
	result := deserializeFloat32(blob, len(original))
	for i := range original {
		if math.Abs(float64(original[i]-result[i])) > 1e-6 {
			t.Errorf("index %d: expected %v, got %v", i, original[i], result[i])
		}
	}
}

func TestFloat64ToFloat32(t *testing.T) {
	input := []float64{0.1, -0.2, 0.3, 1.5}
	output := float64ToFloat32(input)
	require.Len(t, output, len(input))
	for i := range input {
		require.InDelta(t, float64(output[i]), input[i], 1e-6)
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/rag/ -run "TestSerializeFloat32_Roundtrip|TestFloat64ToFloat32" -v
```

Expected: FAIL (functions not yet defined)

**Step 3: Add helper functions to `store_sqlite.go`**

Add these functions at the bottom of `store_sqlite.go`:

```go
// serializeFloat32 converts []float32 to a little-endian byte slice for vec0 BLOB storage.
func serializeFloat32(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

// deserializeFloat32 converts a little-endian byte slice back to []float32.
func deserializeFloat32(buf []byte, dim int) []float32 {
	vec := make([]float32, dim)
	for i := 0; i < dim && i*4+4 <= len(buf); i++ {
		vec[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return vec
}

// float64ToFloat32 converts []float64 to []float32 with minimal precision loss.
func float64ToFloat32(vec []float64) []float32 {
	result := make([]float32, len(vec))
	for i, v := range vec {
		result[i] = float32(v)
	}
	return result
}
```

Add imports to `store_sqlite.go`:
```go
"encoding/binary"
"math"
```

**Step 4: Delete `vector_cache.go`**

```bash
rm internal/rag/vector_cache.go
```

**Step 5: Update Store struct to remove cache field**

In `store_sqlite.go`, change `Store` struct:

```go
// Store manages the SQLite connection, FTS5 index, and vec0 vector index.
type Store struct {
	db       *sql.DB
	embDim   int // current embedding dimension (0 until first embedding inserted)
}
```

**Step 6: Update NewSQLiteStore**

Remove `cache: NewVectorCache(0)` from Store initialization. Remove `s.loadCache()` call. Remove `s.loadEmbeddingDimFromDB()`. Replace with:

```go
func NewSQLiteStore(dbPath string) (*Store, error) {
	// ... (same DSN/PRAGMA setup as before)

	s := &Store{db: db}

	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to init sqlite schema: %w", err)
	}

	s.loadEmbeddingDimFromDB()
	return s, nil
}
```

**Step 7: Stub out all methods that reference `cache`**

For now, comment out or stub these methods (we'll rewrite them in later tasks):
- `loadCache()` — delete
- `asyncLoadCache()` — delete
- `loadEmbeddingDimFromDB()` — update to use `s.embDim` instead of `s.cache.SetDim`
- `SearchSimple()` — will rewrite in Task 3
- `ReloadCacheIfNeeded()` — delete
- All `s.cache.*` references — replace with `s.embDim`

This step is about making the code compile. The actual vector search will be rewritten in Task 3.

**Step 8: Run all existing tests**

```bash
go test ./internal/rag/ -v -count=1 2>&1 | head -100
```

Expected: Compilation errors from missing cache references. Fix them one by one. Vector search tests will fail — that's OK, we'll fix them in Task 3.

**Step 9: Commit**

```bash
git add -A internal/rag/
git commit -m "refactor: remove VectorCache, add float32 serialization helpers"
```

---

## Task 3: Create vec0 virtual table in schema and implement vector search

**Files:**
- Modify: `internal/rag/store_sqlite.go` — initSchema with vec0, SearchVector via KNN
- Modify: `internal/rag/store_sqlite_test.go` — update tests
- Modify: `internal/rag/test_helpers_test.go` — update test embedding helpers

**Step 1: Update initSchema to create vec0 table**

In `store_sqlite.go`, update `initSchema()`:

```go
func (s *Store) initSchema() error {
	// Create rag_chunks table (remove embedding and embedding_dim columns)
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS rag_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			chunk_text TEXT NOT NULL,
			chunk_text_segmented TEXT NOT NULL,
			chunk_index INTEGER NOT NULL DEFAULT 0,
			token_count INTEGER NOT NULL,
			has_embedding INTEGER NOT NULL DEFAULT 0,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_rag_chunks_session ON rag_chunks(session_id);
		CREATE INDEX IF NOT EXISTS idx_rag_chunks_project ON rag_chunks(project_path);
		CREATE INDEX IF NOT EXISTS idx_rag_chunks_created ON rag_chunks(created_at);
		CREATE INDEX IF NOT EXISTS idx_rag_chunks_message ON rag_chunks(message_id);
		CREATE INDEX IF NOT EXISTS idx_rag_chunks_has_embedding ON rag_chunks(id) WHERE has_embedding = 1;
	`)
	if err != nil {
		return fmt.Errorf("create rag_chunks table: %w", err)
	}

	// Create FTS5 virtual table
	_, err = s.db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS rag_chunks_fts USING fts5(
			chunk_text_segmented,
			content='rag_chunks',
			content_rowid='id',
			tokenize='unicode61'
		)
	`)
	if err != nil {
		return fmt.Errorf("create rag_chunks_fts: %w", err)
	}

	// Create vec0 virtual table for vector search
	// Uses embedding dim from embDim; if 0, create with default 1024 and recreate later
	dim := s.embDim
	if dim <= 0 {
		dim = 1024
	}
	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS rag_vec USING vec0(
			embedding float[%d] distance_metric=cosine,
			project_path TEXT,
			backend TEXT,
			role TEXT,
			session_id TEXT
		)
	`, dim))
	if err != nil {
		return fmt.Errorf("create rag_vec: %w", err)
	}

	return nil
}
```

**Step 2: Update InsertChunks to insert into vec0**

Replace the embedding BLOB storage with vec0 insert:

```go
func (s *Store) InsertChunks(chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, c := range chunks {
		if c.Embedding != nil {
			if err := validateEmbedding(c.Embedding); err != nil {
				return fmt.Errorf("embedding validation for chunk (message_id=%d): %w", c.MessageID, err)
			}
		}

		result, err := tx.Exec(
			`INSERT INTO rag_chunks (session_id, message_id, chunk_text, chunk_text_segmented,
				chunk_index, token_count, has_embedding,
				project_path, backend, role, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.SessionID, c.MessageID, c.ChunkText, c.ChunkTextSegmented,
			c.ChunkIndex, c.TokenCount, boolToInt(c.HasEmbedding),
			c.ProjectPath, c.Backend, c.Role, c.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert chunk (message_id=%d, chunk_index=%d): %w", c.MessageID, c.ChunkIndex, err)
		}

		chunkID, _ := result.LastInsertId()

		// Sync FTS
		_, err = tx.Exec(`INSERT INTO rag_chunks_fts(rowid, chunk_text_segmented) VALUES (?, ?)`,
			chunkID, c.ChunkTextSegmented)
		if err != nil {
			return fmt.Errorf("insert fts entry for chunk %d: %w", chunkID, err)
		}

		// Insert into vec0 if embedding available
		if c.HasEmbedding && c.Embedding != nil {
			vecBlob := serializeFloat32(float64ToFloat32(c.Embedding))
			_, err = tx.Exec(
				`INSERT INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
				VALUES (?, ?, ?, ?, ?, ?)`,
				chunkID, vecBlob, c.ProjectPath, c.Backend, c.Role, c.SessionID,
			)
			if err != nil {
				return fmt.Errorf("insert vec entry for chunk %d: %w", chunkID, err)
			}

			// Track dimension
			dim := len(c.Embedding)
			if dim != s.embDim && s.embDim == 0 {
				s.embDim = dim
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit insert transaction: %w", err)
	}
	return nil
}
```

**Step 3: Implement SearchVector using vec0 KNN**

Replace `SearchSimple` with `SearchVector`:

```go
// SearchVector performs vector similarity search using sqlite-vec KNN.
func (s *Store) SearchVector(queryEmbedding []float64, limit int, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime string) ([]SearchHit, error) {
	if err := validateEmbedding(queryEmbedding); err != nil {
		return nil, fmt.Errorf("query embedding validation: %w", err)
	}

	vecBlob := serializeFloat32(float64ToFloat32(queryEmbedding))

	// Build KNN query with metadata filters
	query := `
		SELECT v.rowid, v.distance,
		       c.chunk_text, c.session_id, c.message_id, c.role,
		       c.project_path, c.backend, c.created_at
		FROM rag_vec v
		JOIN rag_chunks c ON c.id = v.rowid
		WHERE v.embedding MATCH ? AND v.k = ?`
	args := []any{vecBlob, limit * 2} // over-fetch for post-filtering

	if projectPath != "" {
		query += " AND v.project_path = ?"
		args = append(args, projectPath)
	}
	if backend != "" {
		query += " AND v.backend = ?"
		args = append(args, backend)
	}
	if role != "" {
		query += " AND v.role = ?"
		args = append(args, role)
	}
	if sessionID != "" {
		query += " AND v.session_id = ?"
		args = append(args, sessionID)
	}
	if excludeSessionID != "" {
		query += " AND v.session_id != ?"
		args = append(args, excludeSessionID)
	}
	if fromTime != "" {
		query += " AND c.created_at >= ?"
		args = append(args, fromTime)
	}
	if toTime != "" {
		query += " AND c.created_at <= ?"
		args = append(args, toTime)
	}

	query += " ORDER BY v.distance LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ChunkID, &h.Score, &h.ChunkText, &h.SessionID,
			&h.MessageID, &h.Role, &h.ProjectPath, &h.Backend, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan vector hit: %w", err)
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}
```

**Step 4: Update SearchHybrid to use SearchVector**

```go
func (s *Store) SearchHybrid(queryEmbedding []float64, queryText string, poolSize, limit int, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime string) ([]SearchHit, error) {
	vecHits, vecErr := s.SearchVector(queryEmbedding, poolSize, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime)
	ftsHits, ftsErr := s.SearchFTS(queryText, poolSize, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime)

	// Same RRF fusion logic as before
	if vecErr != nil && ftsErr != nil {
		return nil, fmt.Errorf("both search sources failed: vector=%w, fts=%w", vecErr, ftsErr)
	}
	if vecErr != nil {
		return ftsHits, nil
	}
	if ftsErr != nil {
		return vecHits, nil
	}

	const k = 60
	type rrfEntry struct {
		hit      SearchHit
		rrfScore float64
	}
	scores := make(map[int64]*rrfEntry)
	for rank, h := range vecHits {
		if _, ok := scores[h.ChunkID]; !ok {
			scores[h.ChunkID] = &rrfEntry{hit: h}
		}
		scores[h.ChunkID].rrfScore += 1.0 / float64(k+rank+1)
	}
	for rank, h := range ftsHits {
		if _, ok := scores[h.ChunkID]; !ok {
			scores[h.ChunkID] = &rrfEntry{hit: h}
		}
		scores[h.ChunkID].rrfScore += 1.0 / float64(k+rank+1)
	}

	entries := make([]*rrfEntry, 0, len(scores))
	for _, e := range scores {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].rrfScore > entries[j].rrfScore
	})

	if limit > len(entries) {
		limit = len(entries)
	}
	results := make([]SearchHit, limit)
	for i, e := range entries[:limit] {
		e.hit.Score = e.rrfScore
		results[i] = e.hit
	}
	return results, nil
}
```

**Step 5: Update UpdateEmbedding**

```go
func (s *Store) UpdateEmbedding(chunkID int64, embedding []float64) error {
	if err := validateEmbedding(embedding); err != nil {
		return fmt.Errorf("embedding validation for update: %w", err)
	}

	// Update has_embedding flag in rag_chunks
	_, err := s.db.Exec(`UPDATE rag_chunks SET has_embedding = 1 WHERE id = ?`, chunkID)
	if err != nil {
		return fmt.Errorf("update has_embedding: %w", err)
	}

	// Get metadata for vec0 insert
	var projectPath, backend, role, sessionID string
	err = s.db.QueryRow(
		`SELECT project_path, backend, role, session_id FROM rag_chunks WHERE id = ?`, chunkID,
	).Scan(&projectPath, &backend, &role, &sessionID)
	if err != nil {
		return fmt.Errorf("get chunk metadata: %w", err)
	}

	// Insert into vec0 (upsert: delete old + insert new)
	_, _ = s.db.Exec(`DELETE FROM rag_vec WHERE rowid = ?`, chunkID)
	vecBlob := serializeFloat32(float64ToFloat32(embedding))
	_, err = s.db.Exec(
		`INSERT INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		chunkID, vecBlob, projectPath, backend, role, sessionID,
	)
	if err != nil {
		return fmt.Errorf("insert vec entry: %w", err)
	}

	return nil
}
```

**Step 6: Update DeleteChunksBySessionIDs**

Add vec0 deletion before FTS deletion:

```go
func (s *Store) DeleteChunksBySessionIDs(sessionIDs []string) (int64, error) {
	// ... same placeholder building as before ...

	tx, err := s.db.Begin()
	// ...

	// Delete vec0 entries first
	_, err = tx.Exec("DELETE FROM rag_vec WHERE rowid IN (SELECT id FROM rag_chunks WHERE session_id IN ("+placeholders+"))", args...)
	if err != nil {
		return 0, fmt.Errorf("delete vec entries: %w", err)
	}

	// Delete FTS entries
	// ...

	// Delete main table
	// ...
}
```

**Step 7: Update remaining cache-referencing methods**

- `loadEmbeddingDimFromDB()` — reads from `rag_vec_info` shadow table or uses `vec_length()`
- `CheckDimensionMismatch()` — compare against `rag_vec_info`
- `SetEmbeddingDim()` / `ResetForDimensionMismatch()` — drop/recreate `rag_vec` table
- `ReloadCacheIfNeeded()` — delete (no-op now)
- Remove `loadCache()`, `asyncLoadCache()`

**Step 8: Update test helpers**

In `test_helpers_test.go`, keep `makeTestEmbedding()` returning `[]float64` (same as before — conversion happens at insert time).

**Step 9: Run tests**

```bash
go test ./internal/rag/ -v -count=1 2>&1 | head -200
```

Fix any remaining compilation errors and test failures.

**Step 10: Commit**

```bash
git add -A internal/rag/
git commit -m "feat: implement vec0 vector search replacing VectorCache"
```

---

## Task 4: Update search.go strategy selection

**Files:**
- Modify: `internal/rag/search.go`

**Step 1: Update RAGSearch to remove cache readiness check**

Replace `cacheReady` check with `vecReady` check (verify `rag_vec` table exists and has data):

```go
func RAGSearch(ctx context.Context, store *Store, embedder *EmbeddingClient, params SearchParams, defaultLimit int, searchPoolSize int) (*SearchResult, error) {
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

	embedderHealthy := EmbedderHealthy()
	if !embedderHealthy && embedder != nil {
		reachable, modelAvailable, _ := embedder.IsHealthy(ctx)
		embedderHealthy = reachable && modelAvailable
	}

	vecReady := store.HasVecData()
	ftsAvailable := true

	var hits []SearchHit
	var mode SearchMode
	var err error

	switch {
	case embedderHealthy && vecReady && ftsAvailable:
		if embedder == nil {
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
		mode = SearchModeFTS
		hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)

	default:
		mode = SearchModeFTS
		hits, err = store.SearchFTS(params.Query, limit, params.ProjectPath, params.Backend, params.Role, params.SessionID, params.ExcludeSessionID, params.FromTime, params.ToTime)
	}

	// ... rest same (title enrichment, logging)
}
```

Add `HasVecData()` method to Store:

```go
func (s *Store) HasVecData() bool {
	var count int
	err := s.db.QueryRow("SELECT count FROM rag_vec_info WHERE key = 'rowids'").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}
```

**Step 2: Update search tests**

Update all tests that reference `store.cache.*` to use the new API. Remove tests for `ReloadCacheIfNeeded`, `AsyncLoadCache`, `IsDirty`, `IsReady`.

**Step 3: Run tests**

```bash
go test ./internal/rag/ -v -count=1
```

**Step 4: Commit**

```bash
git add -A internal/rag/
git commit -m "refactor: update search strategy to use vec0 instead of VectorCache"
```

---

## Task 5: Update indexer.go to remove cache references

**Files:**
- Modify: `internal/rag/indexer.go`

**Step 1: Remove cache-related code from indexer**

- Remove `s.cache.MarkDirty()` calls from `InsertChunks` (already done in Task 3)
- Update `checkEmbedderHealth` to use `store.embDim` instead of `store.cache.Dim()`
- Update `SetEmbeddingDim` / `CheckDimensionMismatch` to not reference cache

**Step 2: Run tests**

```bash
go test ./internal/rag/ -v -count=1
```

**Step 3: Commit**

```bash
git add -A internal/rag/
git commit -m "refactor: remove VectorCache references from indexer"
```

---

## Task 6: Add database migration for existing deployments

**Files:**
- Modify: `internal/service/database.go` — add new schema migration

**Step 1: Add migration to create vec0 table and migrate existing embeddings**

Add a new migration entry in the migration list:

```go
{
	Version: <next_version>,
	Name:    "rag_vec0_migration",
	Up: func(db *sql.DB) error {
		// 1. Check if rag_chunks has embedding data
		var hasEmbCol int
		err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('rag_chunks') WHERE name = 'embedding'").Scan(&hasEmbCol)
		if err != nil || hasEmbCol == 0 {
			// No embedding column — already migrated or fresh install
			return nil
		}

		// 2. Get embedding dimension from existing data
		var dim int
		err = db.QueryRow("SELECT COALESCE((SELECT embedding_dim FROM rag_chunks WHERE has_embedding = 1 AND embedding_dim > 0 LIMIT 1), 0)").Scan(&dim)
		if err != nil {
			return err
		}
		if dim == 0 {
			// No embeddings yet — just drop the columns
			// SQLite doesn't support DROP COLUMN easily, but we can recreate the table
			return nil
		}

		// 3. Create vec0 table
		_, err = db.Exec(fmt.Sprintf(`
			CREATE VIRTUAL TABLE IF NOT EXISTS rag_vec USING vec0(
				embedding float[%d] distance_metric=cosine,
				project_path TEXT,
				backend TEXT,
				role TEXT,
				session_id TEXT
			)
		`, dim))
		if err != nil {
			return fmt.Errorf("create rag_vec: %w", err)
		}

		// 4. Migrate existing embeddings from rag_chunks to rag_vec
		rows, err := db.Query(`
			SELECT id, embedding, embedding_dim, project_path, backend, role, session_id
			FROM rag_chunks
			WHERE has_embedding = 1 AND embedding IS NOT NULL AND embedding_dim > 0
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var id int64
			var blob []byte
			var embDim int
			var projectPath, backend, role, sessionID string
			if err := rows.Scan(&id, &blob, &embDim, &projectPath, &backend, &role, &sessionID); err != nil {
				continue
			}
			if len(blob) != embDim*8 {
				continue // skip malformed
			}
			// Convert float64 BLOB to float32 BLOB
			vec64 := deserializeEmbedding(blob, embDim)
			vec32 := float64ToFloat32(vec64)
			vecBlob := serializeFloat32(vec32)

			_, _ = db.Exec(
				`INSERT OR IGNORE INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
				VALUES (?, ?, ?, ?, ?, ?)`,
				id, vecBlob, projectPath, backend, role, sessionID,
			)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Note: We don't drop the embedding/embedding_dim columns from rag_chunks
		// because SQLite ALTER TABLE DROP COLUMN is only supported in 3.35.0+
		// and we want to keep backward compatibility. The columns will just be unused.
		// Fresh installs won't have these columns at all.

		return nil
	},
},
```

**Step 2: Run full test suite**

```bash
go test ./internal/rag/ ./internal/service/ -v -count=1
```

**Step 3: Commit**

```bash
git add -A
git commit -m "feat: add database migration for vec0 vector search"
```

---

## Task 7: Update rag.go init and cleanup

**Files:**
- Modify: `internal/rag/rag.go`

**Step 1: Add vec import**

```go
import (
	_ "modernc.org/sqlite/vec" // register sqlite-vec extension
)
```

**Step 2: Run full test suite**

```bash
go test ./internal/rag/ -v -count=1
```

**Step 3: Commit**

```bash
git add -A internal/rag/
git commit -m "feat: register sqlite-vec extension on RAG init"
```

---

## Task 8: Update and fix all remaining tests

**Files:**
- Modify: `internal/rag/store_sqlite_test.go`
- Modify: `internal/rag/search_sqlite_test.go`
- Modify: `internal/rag/vector_cache_test.go` — DELETE
- Modify: `internal/rag/indexer_test.go`
- Modify: `internal/rag/cleanup_test.go`
- Modify: `internal/rag/vec_test.go` — rename to proper test file

**Step 1: Delete vector_cache_test.go**

```bash
rm internal/rag/vector_cache_test.go
```

**Step 2: Update store_sqlite_test.go**

- Remove `store.cache.*` references
- Remove `ReloadCacheIfNeeded` tests
- Remove `AsyncLoadCache` tests
- Remove `LoadCache` tests
- Update `SearchSimple` tests → rename to `SearchVector` tests
- Update dimension mismatch tests to not reference cache
- Add vec0-specific tests (insert + KNN query)

**Step 3: Update search_sqlite_test.go**

- Remove `store.cache.Clear()` / `store.cache.IsReady()` references
- Update `CacheNotReady` test → test `HasVecData()` instead

**Step 4: Run full test suite**

```bash
go test ./internal/rag/ -v -count=1
```

Expected: ALL PASS

**Step 5: Commit**

```bash
git add -A internal/rag/
git commit -m "test: update all RAG tests for vec0 vector search"
```

---

## Task 9: Run full project build and test suite

**Files:**
- None (verification only)

**Step 1: Build the project**

```bash
./build.sh
```

Expected: Successful build

**Step 2: Run Go tests**

```bash
go test ./... -count=1 2>&1 | tail -30
```

**Step 3: Run frontend tests**

```bash
cd web && npm test
```

**Step 4: Verify coverage gate**

```bash
./scripts/check-go-coverage.sh
```

**Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: address issues from full test suite run"
```

---

## Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| `modernc.org/sqlite` v1.52.0 breaks existing functionality | Pin to v1.52.0, run full test suite before merging |
| `vec0` table creation fails (e.g., SQLite version mismatch) | `initSchema` logs error and continues; search degrades to FTS-only |
| Float64 → float32 precision loss | BGE-M3 embeddings are stored as float32 by the model; float64 was over-engineering |
| Migration fails on existing data | Migration is idempotent; existing BLOB data converted in transaction |
| `vec0` doesn't support `DELETE WHERE` (must delete by rowid) | Use subquery to find rowids from `rag_chunks` before deleting from `rag_vec` |
| `vec0` KNN doesn't support `session_id != ?` filter | Apply as post-filter via JOIN; over-fetch K and filter in Go if needed |
| Dimension change requires `rag_vec` recreation | Drop and recreate `rag_vec` table (same as current `ResetForDimensionMismatch` behavior) |
