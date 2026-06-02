package rag

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register SQLite driver (pure Go, FTS5 built-in)
)

// Chunk represents a text chunk with its embedding and metadata.
type Chunk struct {
	ID                 int64     `json:"id"`
	SessionID          string    `json:"session_id"`
	MessageID          int64     `json:"message_id"`
	ChunkText          string    `json:"chunk_text"`
	ChunkTextSegmented string    `json:"chunk_text_segmented"`
	ChunkIndex         int       `json:"chunk_index"`
	TokenCount         int       `json:"token_count"`
	Embedding          []float64 `json:"embedding"`
	HasEmbedding       bool      `json:"has_embedding"`
	ProjectPath        string    `json:"project_path"`
	Backend            string    `json:"backend"`
	Role               string    `json:"role"`
	CreatedAt          time.Time `json:"created_at"`
}

// SearchHit represents a search result with similarity score.
type SearchHit struct {
	ChunkID      int64     `json:"chunk_id"`
	ChunkText    string    `json:"chunk_text"`
	Score        float64   `json:"score"`
	SessionID    string    `json:"session_id"`
	SessionTitle string    `json:"session_title"`
	MessageID    int64     `json:"message_id"`
	Role         string    `json:"role"`
	ProjectPath  string    `json:"project_path"`
	Backend      string    `json:"backend"`
	CreatedAt    time.Time `json:"created_at"`
}

// PendingChunk represents a chunk that needs embedding backfill.
type PendingChunk struct {
	ID        int64
	ChunkText string
}

// Store manages the SQLite connection, FTS5 index, and vector cache.
type Store struct {
	db    *sql.DB
	cache *VectorCache
}

// NewSQLiteStore creates a new SQLite-backed RAG store.
// If dbPath is ":memory:", creates an in-memory database (for testing).
// Uses shared cache mode for in-memory databases to allow cross-goroutine access.
func NewSQLiteStore(dbPath string) (*Store, error) {
	dsn := dbPath
	if dbPath == ":memory:" {
		// Shared cache required for in-memory DB to work across goroutines
		dsn = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	// Set pragmas via EXEC (same pattern as service/database.go;
	// modernc.org/sqlite does not recognize mattn-style _busy_timeout/_journal_mode DSN params)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	// Set MaxOpenConns to 1 for in-memory DB (only one connection can see the data)
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	s := &Store{
		db:    db,
		cache: NewVectorCache(0),
	}

	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to init sqlite schema: %w", err)
	}

	// Load embedding dimension from existing data
	s.loadEmbeddingDimFromDB()

	// Load vector cache (synchronously for simplicity; in production this would be async)
	if err := s.loadCache(); err != nil {
		slog.Warn("rag: initial vector cache load failed", slog.String("err", err.Error()))
	}

	return s, nil
}

// initSchema creates the rag_chunks table, FTS5 virtual table, and indexes.
func (s *Store) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS rag_chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			chunk_text TEXT NOT NULL,
			chunk_text_segmented TEXT NOT NULL,
			chunk_index INTEGER NOT NULL DEFAULT 0,
			token_count INTEGER NOT NULL,
			embedding BLOB,
			has_embedding INTEGER NOT NULL DEFAULT 0,
			embedding_dim INTEGER NOT NULL DEFAULT 0,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_rag_chunks_session ON rag_chunks(session_id);
		CREATE INDEX IF NOT EXISTS idx_rag_chunks_project ON rag_chunks(project_path);
		CREATE INDEX IF NOT EXISTS idx_rag_chunks_created ON rag_chunks(created_at);
		CREATE INDEX IF NOT EXISTS idx_rag_chunks_message ON rag_chunks(message_id);
	`)
	if err != nil {
		return fmt.Errorf("create rag_chunks table: %w", err)
	}

	// Create partial index for VectorCache loading
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_rag_chunks_has_embedding ON rag_chunks(id) WHERE has_embedding = 1`)

	// Create FTS5 virtual table with external content mode
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

	return nil
}

// loadEmbeddingDimFromDB reads the embedding dimension from existing data.
func (s *Store) loadEmbeddingDimFromDB() {
	var dim int
	err := s.db.QueryRow(`
		SELECT embedding_dim FROM rag_chunks WHERE has_embedding = 1 AND embedding_dim > 0 LIMIT 1
	`).Scan(&dim)
	if err == nil && dim > 0 {
		s.cache.SetDim(dim)
		slog.Info("rag: loaded embedding dimension from existing data", slog.Int("dim", dim))
	}
}

// asyncLoadCache starts a background goroutine to load all vectors into memory.
func (s *Store) asyncLoadCache() {
	go func() {
		if err := s.loadCache(); err != nil {
			slog.Warn("rag: failed to load vector cache", slog.String("err", err.Error()))
		}
	}()
}

// loadCache reads all embedded vectors from the database into VectorCache.
func (s *Store) loadCache() error {
	rows, err := s.db.Query(`
		SELECT id, session_id, project_path, backend, role, embedding, embedding_dim
		FROM rag_chunks
		WHERE has_embedding = 1 AND embedding IS NOT NULL
	`)
	if err != nil {
		return fmt.Errorf("query vectors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var vectors []CachedVector
	for rows.Next() {
		var cv CachedVector
		var blob []byte
		var dim int
		if err := rows.Scan(&cv.ChunkID, &cv.SessionID, &cv.ProjectPath, &cv.Backend, &cv.Role, &blob, &dim); err != nil {
			return fmt.Errorf("scan vector: %w", err)
		}
		if dim <= 0 || len(blob) != dim*8 {
			continue // skip malformed entries
		}
		cv.Vector = deserializeEmbedding(blob, dim)
		vectors = append(vectors, cv)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(vectors) > 0 {
		s.cache.SetDim(len(vectors[0].Vector))
	}
	s.cache.SetVectors(vectors)
	slog.Info("rag: loaded vector cache", slog.Int("vectors", len(vectors)))
	return nil
}

// InsertChunks inserts multiple chunks into SQLite with FTS5 sync.
// Wraps all inserts in a transaction for atomicity.
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
		// Validate embedding (reject NaN/Inf)
		if c.Embedding != nil {
			if err := validateEmbedding(c.Embedding); err != nil {
				return fmt.Errorf("embedding validation for chunk (message_id=%d): %w", c.MessageID, err)
			}
		}

		// Serialize embedding
		var embBlob []byte
		var embDim int
		if c.Embedding != nil {
			embBlob = serializeEmbedding(c.Embedding)
			embDim = len(c.Embedding)
		}

		result, err := tx.Exec(
			`
			INSERT INTO rag_chunks (session_id, message_id, chunk_text, chunk_text_segmented,
				chunk_index, token_count, embedding, has_embedding, embedding_dim,
				project_path, backend, role, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.SessionID, c.MessageID, c.ChunkText, c.ChunkTextSegmented,
			c.ChunkIndex, c.TokenCount, embBlob, boolToInt(c.HasEmbedding), embDim,
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
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit insert transaction: %w", err)
	}

	s.cache.MarkDirty()
	return nil
}

// SearchSimple performs vector similarity search using in-memory VectorCache.
//
//nolint:gocyclo // time filter branch logic is inherently multi-conditional
func (s *Store) SearchSimple(queryEmbedding []float64, limit int, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime string) ([]SearchHit, error) {
	// Validate query embedding
	if err := validateEmbedding(queryEmbedding); err != nil {
		return nil, fmt.Errorf("query embedding validation: %w", err)
	}

	// Reload cache if dirty (new embeddings were added)
	if s.cache.IsDirty() {
		if err := s.loadCache(); err != nil {
			slog.Warn("rag: failed to reload dirty cache", slog.String("err", err.Error()))
		}
	}

	// Use VectorCache for in-memory search
	cacheHits := s.cache.Search(queryEmbedding, limit*2, projectPath, backend, role, sessionID, excludeSessionID)
	if len(cacheHits) == 0 {
		return nil, nil
	}

	// Apply time filters by fetching from DB
	if fromTime == "" && toTime == "" {
		// No time filters — return cache results directly
		if limit > len(cacheHits) {
			limit = len(cacheHits)
		}
		return cacheHits[:limit], nil
	}

	// Time filters need DB lookup — fetch chunk metadata for candidates
	chunkIDs := make([]int64, len(cacheHits))
	for i, h := range cacheHits {
		chunkIDs[i] = h.ChunkID
	}

	// Build query with time filters
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, 0, len(chunkIDs)+2)
	for i, id := range chunkIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		SELECT id, chunk_text, session_id, message_id, role, project_path, backend, created_at
		FROM rag_chunks
		WHERE id IN (%s)`, strings.Join(placeholders, ","))

	if fromTime != "" {
		query += " AND created_at >= ?"
		args = append(args, fromTime)
	}
	if toTime != "" {
		query += " AND created_at <= ?"
		args = append(args, toTime)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("search time filter query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Build a map of chunk_id -> SearchHit from cache results
	hitMap := make(map[int64]SearchHit, len(cacheHits))
	for _, h := range cacheHits {
		hitMap[h.ChunkID] = h
	}

	var hits []SearchHit
	for rows.Next() {
		var id int64
		var h SearchHit
		if err := rows.Scan(&id, &h.ChunkText, &h.SessionID, &h.MessageID, &h.Role, &h.ProjectPath, &h.Backend, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan time-filtered hit: %w", err)
		}
		if cacheHit, ok := hitMap[id]; ok {
			h.Score = cacheHit.Score
			hits = append(hits, h)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Re-sort by score (time filter may have removed some)
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	if limit > len(hits) {
		limit = len(hits)
	}
	return hits[:limit], nil
}

// SearchFTS performs BM25 full-text search using SQLite FTS5.
func (s *Store) SearchFTS(queryText string, limit int, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime string) ([]SearchHit, error) {
	// Segment the query for Chinese support
	segmentedQuery := SegmentText(queryText)

	// Wrap in FTS5 phrase syntax (double-quoted) to treat the entire segmented
	// string as a literal phrase. This prevents FTS5 special operators (AND, OR,
	// NOT, NEAR, *, "") in user input from causing FTS5 syntax errors (ISS-283).
	// Any embedded double-quote characters are escaped by doubling them.
	escapedQuery := strings.ReplaceAll(segmentedQuery, `"`, `""`)
	ftsQuery := `"` + escapedQuery + `"`

	// Use FTS5 MATCH with BM25 ranking
	query := `
		SELECT rag_chunks.id,
		       rag_chunks.chunk_text,
		       bm25(rag_chunks_fts) AS score,
		       rag_chunks.session_id,
		       rag_chunks.message_id,
		       rag_chunks.role,
		       rag_chunks.project_path,
		       rag_chunks.backend,
		       rag_chunks.created_at
		FROM rag_chunks_fts
		JOIN rag_chunks ON rag_chunks.id = rag_chunks_fts.rowid
		WHERE rag_chunks_fts MATCH ?
	`
	args := []any{ftsQuery}

	if projectPath != "" {
		query += " AND rag_chunks.project_path = ?"
		args = append(args, projectPath)
	}
	if backend != "" {
		query += " AND rag_chunks.backend = ?"
		args = append(args, backend)
	}
	if role != "" {
		query += " AND rag_chunks.role = ?"
		args = append(args, role)
	}
	if sessionID != "" {
		query += " AND rag_chunks.session_id = ?"
		args = append(args, sessionID)
	}
	if excludeSessionID != "" {
		query += " AND rag_chunks.session_id != ?"
		args = append(args, excludeSessionID)
	}
	if fromTime != "" {
		query += " AND rag_chunks.created_at >= ?"
		args = append(args, fromTime)
	}
	if toTime != "" {
		query += " AND rag_chunks.created_at <= ?"
		args = append(args, toTime)
	}

	query += " ORDER BY score LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("fts search query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ChunkID, &h.ChunkText, &h.Score, &h.SessionID, &h.MessageID, &h.Role, &h.ProjectPath, &h.Backend, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan fts hit: %w", err)
		}
		// BM25 returns negative scores for better ranking; negate for consistency
		h.Score = -h.Score
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

// SearchHybrid performs hybrid vector + FTS search using Reciprocal Rank Fusion (RRF).
// poolSize is how many candidates each source returns before fusion.
func (s *Store) SearchHybrid(queryEmbedding []float64, queryText string, poolSize, limit int, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime string) ([]SearchHit, error) {
	// Run both searches
	vecHits, vecErr := s.SearchSimple(queryEmbedding, poolSize, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime)
	ftsHits, ftsErr := s.SearchFTS(queryText, poolSize, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime)

	// If one source fails completely, fall back to the other
	if vecErr != nil && ftsErr != nil {
		return nil, fmt.Errorf("both search sources failed: vector=%w, fts=%w", vecErr, ftsErr)
	}
	if vecErr != nil {
		return ftsHits, nil //nolint:nilerr // intentional: return successful source when other fails
	}
	if ftsErr != nil {
		return vecHits, nil //nolint:nilerr // intentional: return successful source when other fails
	}

	// RRF fusion: score = sum(1 / (k + rank_i)) for each source
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

// PendingEmbeddingCount returns the number of chunks that need embedding backfill.
func (s *Store) PendingEmbeddingCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM rag_chunks WHERE has_embedding = 0").Scan(&count)
	return count, err
}

// GetPendingEmbeddings returns chunk IDs and texts that need embedding backfill.
func (s *Store) GetPendingEmbeddings(limit int) ([]PendingChunk, error) {
	rows, err := s.db.Query("SELECT id, chunk_text FROM rag_chunks WHERE has_embedding = 0 ORDER BY created_at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var pending []PendingChunk
	for rows.Next() {
		var p PendingChunk
		if err := rows.Scan(&p.ID, &p.ChunkText); err != nil {
			return nil, err
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}

// UpdateEmbedding updates the embedding for a specific chunk (for backfill).
// Unlike the DuckDB version, this uses a simple UPDATE (no DELETE+INSERT workaround needed).
func (s *Store) UpdateEmbedding(chunkID int64, embedding []float64) error {
	// Validate embedding
	if err := validateEmbedding(embedding); err != nil {
		return fmt.Errorf("embedding validation for update: %w", err)
	}

	embBlob := serializeEmbedding(embedding)
	_, err := s.db.Exec(
		`
		UPDATE rag_chunks
		SET embedding = ?, has_embedding = 1, embedding_dim = ?
		WHERE id = ?`,
		embBlob, len(embedding), chunkID,
	)
	if err != nil {
		return fmt.Errorf("update embedding: %w", err)
	}

	s.cache.MarkDirty()
	return nil
}

// CheckDimensionMismatch checks if existing embeddings have a different dimension
// than the cache's configured dimension. Returns the existing dimension (0 if no data)
// and whether there is a mismatch.
func (s *Store) CheckDimensionMismatch() (int, bool, error) {
	var dim int
	err := s.db.QueryRow(`
		SELECT COALESCE(
			(SELECT embedding_dim FROM rag_chunks WHERE has_embedding = 1 AND embedding_dim > 0 LIMIT 1),
			0
		)
	`).Scan(&dim)
	if err != nil {
		return 0, false, fmt.Errorf("check dimension: %w", err)
	}
	if dim == 0 {
		return 0, false, nil
	}
	return dim, dim != s.cache.Dim(), nil
}

// SetEmbeddingDim sets the embedding dimension. Returns true if it changed.
func (s *Store) SetEmbeddingDim(dim int) bool {
	if dim == s.cache.Dim() {
		return false
	}
	s.cache.SetDim(dim)
	return true
}

// ResetForDimensionMismatch clears all chunks and FTS when dimension changes.
func (s *Store) ResetForDimensionMismatch(newDim int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin reset transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete FTS entries first
	_, err = tx.Exec("DELETE FROM rag_chunks_fts")
	if err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}

	// Delete main table
	_, err = tx.Exec("DELETE FROM rag_chunks")
	if err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reset: %w", err)
	}

	s.cache.SetDim(newDim)
	s.cache.Clear()
	return nil
}

// ChunkCount returns the total number of chunks in the store.
func (s *Store) ChunkCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM rag_chunks").Scan(&count)
	return count, err
}

// DeleteChunksBySessionIDs deletes all chunks belonging to the given session IDs.
// FTS entries are deleted in the same transaction for consistency.
func (s *Store) DeleteChunksBySessionIDs(sessionIDs []string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Build placeholders for IN clause
	placeholders := ""
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}

	// Delete FTS entries first (uses subquery to find IDs)
	_, err = tx.Exec("DELETE FROM rag_chunks_fts WHERE rowid IN (SELECT id FROM rag_chunks WHERE session_id IN ("+placeholders+"))", args...)
	if err != nil {
		return 0, fmt.Errorf("delete fts entries: %w", err)
	}

	// Delete main table
	result, err := tx.Exec("DELETE FROM rag_chunks WHERE session_id IN ("+placeholders+")", args...)
	if err != nil {
		return 0, fmt.Errorf("delete chunks: %w", err)
	}
	affected, _ := result.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit delete: %w", err)
	}

	s.cache.MarkDirty()
	return affected, nil
}

// FTSIntegrityCheck verifies FTS5 index consistency.
func (s *Store) FTSIntegrityCheck() error {
	_, err := s.db.Exec("INSERT INTO rag_chunks_fts(rag_chunks_fts) VALUES('integrity-check')")
	return err
}

// Close closes the SQLite connection.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// validateEmbedding checks that all values in the embedding are finite.
func validateEmbedding(vec []float64) error {
	for i, v := range vec {
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return fmt.Errorf("embedding contains non-finite value at index %d: %v", i, v)
		}
	}
	return nil
}

// boolToInt converts a bool to SQLite integer (0 or 1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Ensure Store still satisfies the same interface — context-based query methods
// needed by search.go and indexer.go are on the same *Store type.

// ReloadCacheIfNeeded checks if the cache is dirty and reloads incrementally.
func (s *Store) ReloadCacheIfNeeded() error {
	if !s.cache.IsDirty() {
		return nil
	}
	slog.Info("rag: reloading vector cache (dirty)")
	return s.loadCache()
}
