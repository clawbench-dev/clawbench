package rag

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"     // register SQLite driver (pure Go, FTS5 built-in)
	_ "modernc.org/sqlite/vec" // register sqlite-vec extension for vec0 virtual tables
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

// Store manages the SQLite connection and FTS5 index.
type Store struct {
	db     *sql.DB
	embDim int
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
		db: db,
	}

	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to init sqlite schema: %w", err)
	}

	// Load embedding dimension from existing data
	s.loadEmbeddingDimFromDB()

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

	// Create partial index for embedding queries
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

	// Create vec0 virtual table for vector similarity search
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

	// Migrate existing float64 BLOB embeddings from rag_chunks to rag_vec
	if err := s.migrateEmbeddingsToVec(); err != nil {
		slog.Warn("rag: embedding migration to vec0 failed", slog.String("err", err.Error()))
		// Non-fatal: new inserts will populate rag_vec going forward
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
		s.embDim = dim
		slog.Info("rag: loaded embedding dimension from existing data", slog.Int("dim", dim))
	}
}

// migrateEmbeddingsToVec migrates existing float64 BLOB embeddings from rag_chunks
// into the rag_vec vec0 virtual table. This handles upgrades from the pre-vec0 schema
// where embeddings were stored as float64 BLOBs in the rag_chunks.embedding column.
// The migration is idempotent: rows already present in rag_vec are skipped.
func (s *Store) migrateEmbeddingsToVec() error {
	// Check if rag_chunks has embedding column (it should, but be defensive)
	var hasEmbCol int
	err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('rag_chunks') WHERE name = 'embedding'").Scan(&hasEmbCol)
	if err != nil || hasEmbCol == 0 {
		return nil // no embedding column — nothing to migrate
	}

	rows, err := s.db.Query(`
		SELECT id, embedding, project_path, backend, role, session_id
		FROM rag_chunks
		WHERE has_embedding = 1 AND embedding IS NOT NULL
		AND id NOT IN (SELECT rowid FROM rag_vec)
	`)
	if err != nil {
		return fmt.Errorf("query embeddings for migration: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var migrated int
	for rows.Next() {
		var id int64
		var blob []byte
		var projectPath, backend, role, sessionID string
		if err := rows.Scan(&id, &blob, &projectPath, &backend, &role, &sessionID); err != nil {
			continue
		}
		// Convert float64 BLOB → float32 BLOB for vec0
		dim := len(blob) / 8
		if dim == 0 {
			continue
		}
		vec64 := deserializeEmbedding(blob, dim)
		vec32 := float64ToFloat32(vec64)
		vecBlob := serializeFloat32(vec32)
		_, err := s.db.Exec(
			`INSERT OR IGNORE INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, vecBlob, projectPath, backend, role, sessionID,
		)
		if err != nil {
			slog.Warn("rag: failed to migrate embedding to vec0",
				slog.Int64("chunk_id", id), slog.String("err", err.Error()))
			continue
		}
		migrated++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate embeddings for migration: %w", err)
	}
	if migrated > 0 {
		slog.Info("rag: migrated embeddings to vec0", slog.Int("count", migrated))
	}
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
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit insert transaction: %w", err)
	}

	return nil
}

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

// HasVecData returns true if the vec0 table contains any vectors.
func (s *Store) HasVecData() bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM rag_chunks WHERE has_embedding = 1").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
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
	vecHits, vecErr := s.SearchVector(queryEmbedding, poolSize, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime)
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
// Also inserts the vector into the vec0 index.
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

	// Fetch chunk metadata for vec0 insert
	var projectPath, backend, role, sessionID string
	err = s.db.QueryRow(
		`SELECT project_path, backend, role, session_id FROM rag_chunks WHERE id = ?`,
		chunkID,
	).Scan(&projectPath, &backend, &role, &sessionID)
	if err != nil {
		return fmt.Errorf("fetch chunk metadata for vec insert: %w", err)
	}

	// Upsert into vec0 (delete old + insert new)
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

// CheckDimensionMismatch checks if existing embeddings have a different dimension
// than the store's configured dimension. Returns the existing dimension (0 if no data)
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
	return dim, dim != s.embDim, nil
}

// SetEmbeddingDim sets the embedding dimension. Returns true if it changed.
func (s *Store) SetEmbeddingDim(dim int) bool {
	if dim == s.embDim {
		return false
	}
	s.embDim = dim
	return true
}

// ResetForDimensionMismatch clears all chunks, FTS, and vec0 when dimension changes.
func (s *Store) ResetForDimensionMismatch(newDim int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin reset transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Delete vec0 entries first
	_, err = tx.Exec("DELETE FROM rag_vec")
	if err != nil {
		return fmt.Errorf("delete vec entries: %w", err)
	}

	// Delete FTS entries
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

	s.embDim = newDim
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

	// Delete vec0 entries first
	_, err = tx.Exec("DELETE FROM rag_vec WHERE rowid IN (SELECT id FROM rag_chunks WHERE session_id IN ("+placeholders+"))", args...)
	if err != nil {
		return 0, fmt.Errorf("delete vec entries: %w", err)
	}

	// Delete FTS entries (uses subquery to find IDs)
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

// serializeEmbedding converts a []float64 to a byte slice for BLOB storage.
// Each float64 is stored as 8 bytes using math.Float64bits.
func serializeEmbedding(vec []float64) []byte {
	buf := make([]byte, len(vec)*8)
	for i, v := range vec {
		bits := math.Float64bits(v)
		buf[i*8+0] = byte(bits >> 56)
		buf[i*8+1] = byte(bits >> 48) //nolint:gosec // G115: intentional bit extraction
		buf[i*8+2] = byte(bits >> 40) //nolint:gosec // G115: intentional bit extraction
		buf[i*8+3] = byte(bits >> 32) //nolint:gosec // G115: intentional bit extraction
		buf[i*8+4] = byte(bits >> 24) //nolint:gosec // G115: intentional bit extraction
		buf[i*8+5] = byte(bits >> 16) //nolint:gosec // G115: intentional bit extraction
		buf[i*8+6] = byte(bits >> 8)  //nolint:gosec // G115: intentional bit extraction
		buf[i*8+7] = byte(bits)       //nolint:gosec // G115: intentional bit extraction
	}
	return buf
}

// deserializeEmbedding converts a BLOB byte slice back to []float64.
// dim specifies the expected number of float64 values.
func deserializeEmbedding(buf []byte, dim int) []float64 {
	vec := make([]float64, 0, dim)
	for i := 0; i+8 <= len(buf) && len(vec) < dim; i += 8 {
		bits := uint64(buf[i+0])<<56 |
			uint64(buf[i+1])<<48 |
			uint64(buf[i+2])<<40 |
			uint64(buf[i+3])<<32 |
			uint64(buf[i+4])<<24 |
			uint64(buf[i+5])<<16 |
			uint64(buf[i+6])<<8 |
			uint64(buf[i+7])
		vec = append(vec, math.Float64frombits(bits))
	}
	return vec
}
