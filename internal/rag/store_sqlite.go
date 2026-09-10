package rag

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawbench/internal/service"

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

// PendingChunk represents a chunk that needs embedding backfill.
type PendingChunk struct {
	ID          int64
	ChunkText   string
	ProjectPath string
	Backend     string
	Role        string
	SessionID   string
}

// WriteLocker abstracts the global write mutex for database writes.
// In production, this is backed by service.WriteLock/WriteUnlock.
// In tests, this is a no-op (the test DB has its own connection).
type WriteLocker interface {
	Lock()
	Unlock()
}

// noOpLocker is a WriteLocker that does nothing (used in tests).
type noOpLocker struct{}

func (noOpLocker) Lock()   {}
func (noOpLocker) Unlock() {}

// serviceWriteLocker delegates to service.WriteLock/WriteUnlock.
type serviceWriteLocker struct{}

func (serviceWriteLocker) Lock()   { service.WriteLock() }
func (serviceWriteLocker) Unlock() { service.WriteUnlock() }

// Store manages the SQLite connection and FTS5 index.
type Store struct {
	db *sql.DB
	// mu guards embDim and vecTableReady. These are written by the indexer
	// goroutine (dimension sync, lazy vec0 creation) and by HTTP handlers
	// (reset/rebuild endpoints) concurrently, so plain fields would race.
	mu            sync.RWMutex
	embDim        int
	vecTableReady bool // cached: true after rag_vec table confirmed to exist
	writeMu       WriteLocker
}

// getEmbDim returns the current embedding dimension under the read lock.
func (s *Store) getEmbDim() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.embDim
}

// setEmbDimInternal updates the embedding dimension under the write lock.
func (s *Store) setEmbDimInternal(dim int) {
	s.mu.Lock()
	s.embDim = dim
	s.mu.Unlock()
}

// isVecTableReady reports the cached "rag_vec exists" flag under the read lock.
func (s *Store) isVecTableReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vecTableReady
}

// markVecTableReady caches that rag_vec exists under the write lock.
func (s *Store) markVecTableReady() {
	s.mu.Lock()
	s.vecTableReady = true
	s.mu.Unlock()
}

// invalidateVecTable clears the cached rag_vec-exists flag after the table is
// dropped (reset paths), forcing the next ensureVecTable to re-check.
func (s *Store) invalidateVecTable() {
	s.mu.Lock()
	s.vecTableReady = false
	s.mu.Unlock()
}

// NewSQLiteStore creates a new SQLite-backed RAG store.
// If dbPath is ":memory:", creates an in-memory database (for testing).
// Uses shared cache mode for in-memory databases to allow cross-goroutine access.
func NewSQLiteStore(dbPath string) (*Store, error) {
	return newSQLiteStoreWithLocker(dbPath, serviceWriteLocker{})
}

// NewSQLiteStoreForTest creates a store without the global write mutex (for unit tests).
func NewSQLiteStoreForTest(dbPath string) (*Store, error) {
	return newSQLiteStoreWithLocker(dbPath, noOpLocker{})
}

func newSQLiteStoreWithLocker(dbPath string, locker WriteLocker) (*Store, error) {
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
		db:      db,
		writeMu: locker,
	}

	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to init sqlite schema: %w", err)
	}

	// Enforce one chunk row per (message_id, chunk_index) so a retried batch
	// cannot duplicate chunks, FTS entries, or vectors. Runs before the
	// dimension load because it may delete rows.
	if err := s.ensureChunkUniqueness(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to enforce chunk uniqueness: %w", err)
	}

	// Load embedding dimension from existing data BEFORE migration
	// (migration needs the dimension to create rag_vec table)
	s.loadEmbeddingDimFromDB()

	// Migrate existing float64 BLOB embeddings from rag_chunks to rag_vec.
	// Must run after loadEmbeddingDimFromDB so rag_vec can be created with the correct dimension.
	if err := s.migrateEmbeddingsToVec(); err != nil {
		slog.Warn("rag: embedding migration to vec0 failed", slog.String("err", err.Error()))
		// Non-fatal: new inserts will populate rag_vec going forward
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

	// Create partial index for embedding queries
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_rag_chunks_has_embedding ON rag_chunks(id) WHERE has_embedding = 1`)
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_rag_chunks_no_embedding ON rag_chunks(id) WHERE has_embedding = 0`)

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

	// Note: rag_vec is created lazily by ensureVecTable() on first vector insert,
	// because the correct dimension is only known after loadEmbeddingDimFromDB().
	// Migration is also deferred to NewSQLiteStore() after dimension is loaded.

	return nil
}

// ensureChunkUniqueness deduplicates rag_chunks on (message_id, chunk_index) and
// adds a unique index enforcing it.
//
// Without this, a batch that partially commits (InsertChunks splits into
// 100-chunk transactions) and then fails will be retried, re-inserting chunks
// that already landed — producing duplicate chunk, FTS, and vector rows for the
// same message and polluting search results. Existing duplicates (from older
// builds) are collapsed to the lowest chunk id, and the FTS entries of the
// discarded rows are removed in step.
func (s *Store) ensureChunkUniqueness() error {
	// Check for duplicates first: the DELETE below is a full scan, so skip it
	// entirely on the common (already unique) path.
	var dupes int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM rag_chunks GROUP BY message_id, chunk_index HAVING COUNT(*) > 1
		)
	`).Scan(&dupes)
	if err != nil {
		return fmt.Errorf("check duplicate chunks: %w", err)
	}

	if dupes > 0 {
		s.writeMu.Lock()
		// Remove the FTS index entries for the rows that are about to be deleted,
		// otherwise the external-content index keeps pointing at gone rowids.
		_, ftsErr := s.db.Exec(`
			DELETE FROM rag_chunks_fts WHERE rowid IN (
				SELECT id FROM rag_chunks WHERE id NOT IN (
					SELECT MIN(id) FROM rag_chunks GROUP BY message_id, chunk_index
				)
			)
		`)
		if ftsErr != nil {
			s.writeMu.Unlock()
			return fmt.Errorf("dedupe fts entries: %w", ftsErr)
		}
		res, delErr := s.db.Exec(`
			DELETE FROM rag_chunks WHERE id NOT IN (
				SELECT MIN(id) FROM rag_chunks GROUP BY message_id, chunk_index
			)
		`)
		if delErr != nil {
			s.writeMu.Unlock()
			return fmt.Errorf("dedupe chunks: %w", delErr)
		}
		removed, _ := res.RowsAffected()
		s.writeMu.Unlock()
		slog.Info("rag: removed duplicate chunks", slog.Int("duplicates", dupes), slog.Int64("rows_removed", removed))
	}

	s.writeMu.Lock()
	_, err = s.db.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_rag_chunks_message_chunk ON rag_chunks(message_id, chunk_index)`,
	)
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("create chunk uniqueness index: %w", err)
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
		s.setEmbDimInternal(dim)
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

	// Check if there are any embeddings to migrate
	var embCount int
	err = s.db.QueryRow("SELECT COUNT(*) FROM rag_chunks WHERE has_embedding = 1 AND embedding IS NOT NULL").Scan(&embCount)
	if err != nil || embCount == 0 {
		return nil
	}

	// Ensure dimension is set before creating vec0
	if s.getEmbDim() <= 0 {
		slog.Info("rag: embedding dimension unknown, skipping vec0 migration until embedder provides it")
		return nil
	}

	// Ensure vec0 table exists
	if vecErr := s.ensureVecTable(); vecErr != nil {
		return fmt.Errorf("ensure vec0 table for migration: %w", vecErr)
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
		s.writeMu.Lock()
		_, err := s.db.Exec(
			`INSERT OR IGNORE INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
			VALUES (?, ?, ?, ?, ?, ?)`,
			id, vecBlob, projectPath, backend, role, sessionID,
		)
		s.writeMu.Unlock()
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

// ensureVecTable creates the rag_vec vec0 virtual table if it doesn't exist.
// The dimension is determined from s.embDim (set by SetEmbeddingDim or loaded from DB).
// Returns an error if dimension is unknown (0) and no existing table is found.
// Must NOT be called while holding writeMu or from within a transaction (opens its own queries).
func (s *Store) ensureVecTable() error {
	if s.isVecTableReady() {
		return nil // already confirmed
	}

	// Check if rag_vec already exists
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rag_vec'").Scan(&count)
	if err != nil {
		return fmt.Errorf("check rag_vec existence: %w", err)
	}
	if count > 0 {
		s.markVecTableReady()
		return nil
	}

	dim := s.getEmbDim()
	if dim <= 0 {
		return fmt.Errorf("cannot create rag_vec: embedding dimension unknown (set via SetEmbeddingDim first)")
	}

	s.writeMu.Lock()
	_, err = s.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS rag_vec USING vec0(
			embedding float[%d] distance_metric=cosine,
			project_path TEXT,
			backend TEXT,
			role TEXT,
			session_id TEXT
		)
	`, dim))
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("create rag_vec: %w", err)
	}
	s.markVecTableReady()
	slog.Info("rag: created rag_vec table", slog.Int("dim", dim))
	return nil
}

// vecTableExists checks if the rag_vec table exists in the database.
func (s *Store) vecTableExists() bool {
	if s.isVecTableReady() {
		return true
	}
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rag_vec'").Scan(&count)
	if err == nil && count > 0 {
		s.markVecTableReady()
		return true
	}
	return false
}

// InsertChunks inserts multiple chunks into SQLite with FTS5 sync.
// Splits large batches into smaller transactions (max chunksPerTx per tx)
// to avoid holding the SQLite write lock too long and blocking other writers.
func (s *Store) InsertChunks(chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	vecReady := s.prepareVecTable(chunks)

	// Resolve the vec0 table width once, before opening any transaction: the
	// per-chunk width check must not query the DB from inside a tx (single
	// connection would deadlock) and the width cannot change mid-insert.
	vecDim := 0
	if vecReady {
		dim, err := s.ragVecDim()
		if err != nil {
			return fmt.Errorf("resolve rag_vec dimension: %w", err)
		}
		vecDim = dim
	}

	// Process in sub-batches to limit write-lock duration
	const chunksPerTx = 100
	for batchStart := 0; batchStart < len(chunks); batchStart += chunksPerTx {
		batchEnd := batchStart + chunksPerTx
		if batchEnd > len(chunks) {
			batchEnd = len(chunks)
		}
		if err := s.insertChunkBatch(chunks[batchStart:batchEnd], vecReady, vecDim); err != nil {
			return err
		}
	}

	return nil
}

// prepareVecTable ensures the vec0 virtual table exists if any chunk has embeddings.
// Returns true if vec0 is ready for inserts.
func (s *Store) prepareVecTable(chunks []Chunk) bool {
	for _, c := range chunks {
		if c.HasEmbedding && c.Embedding != nil {
			if err := s.ensureVecTable(); err != nil {
				slog.Warn("rag: vec0 table not available, storing text-only", slog.String("err", err.Error()))
				return false
			}
			return true
		}
	}
	return false
}

// insertChunkBatch inserts a batch of chunks within a single transaction.
func (s *Store) insertChunkBatch(batch []Chunk, vecReady bool, vecDim int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	for _, c := range batch {
		chunkID, err := s.insertOneChunk(tx, c, vecReady, vecDim)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		_ = chunkID
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit insert transaction: %w", err)
	}
	return nil
}

// deleteChunkByMessageIndex removes any existing chunk row for the given
// (message_id, chunk_index), along with its FTS and vec entries, inside tx.
// It is a no-op when no such row exists, making chunk inserts idempotent across
// retries of a partially-committed batch.
func deleteChunkByMessageIndex(tx *sql.Tx, messageID int64, chunkIndex int) error {
	var oldID int64
	err := tx.QueryRow(
		`SELECT id FROM rag_chunks WHERE message_id = ? AND chunk_index = ?`,
		messageID, chunkIndex,
	).Scan(&oldID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up existing chunk (message_id=%d, chunk_index=%d): %w", messageID, chunkIndex, err)
	}

	if _, err := tx.Exec(`DELETE FROM rag_chunks_fts WHERE rowid = ?`, oldID); err != nil {
		return fmt.Errorf("delete existing fts entry for chunk %d: %w", oldID, err)
	}
	if _, err := tx.Exec(`DELETE FROM rag_vec WHERE rowid = ?`, oldID); err != nil {
		// rag_vec may not exist yet (FTS-only mode); that is not an error.
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete existing vec entry for chunk %d: %w", oldID, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM rag_chunks WHERE id = ?`, oldID); err != nil {
		return fmt.Errorf("delete existing chunk %d: %w", oldID, err)
	}
	return nil
}

// insertOneChunk inserts a single chunk and its FTS + vec entries within a transaction.
// vecDim is the rag_vec table's width (0 when unknown), resolved by the caller
// before the transaction opens. Returns the chunk ID on success.
//
// The chunk is only marked has_embedding = 1 when its vector row is actually
// written to rag_vec. If the embedding is present but vec0 is unavailable (e.g.
// the dimension is not yet known), the embedding BLOB is stored but the flag
// stays 0, so the backfill pass (which selects has_embedding = 0) will embed it
// later. Marking it embedded without a vec row would make it permanently
// invisible to vector search.
func (s *Store) insertOneChunk(tx *sql.Tx, c Chunk, vecReady bool, vecDim int) (int64, error) {
	hasEmbedding := c.HasEmbedding && c.Embedding != nil
	if c.Embedding != nil {
		if err := validateEmbedding(c.Embedding); err != nil {
			return 0, fmt.Errorf("embedding validation for chunk (message_id=%d): %w", c.MessageID, err)
		}
		// Reject vectors whose width does not match the vec0 table. Without this
		// a misconfigured embedder poisons the whole insert transaction and the
		// batch is retried forever. vecDim is resolved by the caller before the
		// transaction opens (querying here would deadlock on single-connection DBs).
		if vecReady && vecDim > 0 && len(c.Embedding) != vecDim {
			return 0, fmt.Errorf(
				"embedding dimension mismatch for chunk (message_id=%d): got %d, rag_vec expects %d",
				c.MessageID, len(c.Embedding), vecDim)
		}
	}

	// Only claim the chunk is embedded if the vec row will actually be written.
	willStoreVector := hasEmbedding && vecReady

	var embBlob []byte
	var embDim int
	if c.Embedding != nil {
		embBlob = serializeEmbedding(c.Embedding)
		embDim = len(c.Embedding)
	}

	// Replace any existing row for this (message_id, chunk_index).
	//
	// InsertChunks commits in 100-chunk sub-batches, so a batch can partially
	// commit and then fail; the messages stay unindexed and are retried, which
	// would otherwise re-insert chunks that already landed. The unique index
	// makes that a hard error, so clear the previous row (plus its FTS and vec
	// entries) first to make inserts idempotent.
	if err := deleteChunkByMessageIndex(tx, c.MessageID, c.ChunkIndex); err != nil {
		return 0, err
	}

	result, err := tx.Exec(
		`INSERT INTO rag_chunks (session_id, message_id, chunk_text, chunk_text_segmented,
			chunk_index, token_count, embedding, has_embedding, embedding_dim,
			project_path, backend, role, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.SessionID, c.MessageID, c.ChunkText, c.ChunkTextSegmented,
		c.ChunkIndex, c.TokenCount, embBlob, boolToInt(willStoreVector), embDim,
		c.ProjectPath, c.Backend, c.Role, c.CreatedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert chunk (message_id=%d, chunk_index=%d): %w", c.MessageID, c.ChunkIndex, err)
	}

	chunkID, _ := result.LastInsertId()

	if _, err = tx.Exec(`INSERT INTO rag_chunks_fts(rowid, chunk_text_segmented) VALUES (?, ?)`,
		chunkID, c.ChunkTextSegmented); err != nil {
		return 0, fmt.Errorf("insert fts entry for chunk %d: %w", chunkID, err)
	}

	if willStoreVector {
		vecBlob := serializeFloat32(float64ToFloat32(c.Embedding))
		if _, err = tx.Exec(
			`INSERT INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
			VALUES (?, ?, ?, ?, ?, ?)`,
			chunkID, vecBlob, c.ProjectPath, c.Backend, c.Role, c.SessionID,
		); err != nil {
			return 0, fmt.Errorf("insert vec entry for chunk %d: %w", chunkID, err)
		}
	}

	return chunkID, nil
}

// vectorFilterSQL holds the optional metadata predicates for a KNN query.
type vectorFilterSQL struct {
	projectPath      string
	backend          string
	role             string
	sessionID        string
	excludeSessionID string
	fromTime         string
	toTime           string
}

// sql renders the filter predicates, prefixed with " AND ", or "" when no
// filter is set.
//
// The time window is deliberately expressed on v.rowid rather than on the
// joined c.created_at: sqlite-vec applies filters on vec0's own columns (and on
// v.rowid) INSIDE the KNN scan, but a predicate on a joined table is applied
// afterwards. Filtering c.created_at post-KNN only inspects the top-k
// candidates, so if the nearest k vectors fall outside the window the query
// returns nothing even when in-range matches exist. A rowid subquery pushes the
// window into the scan so the ranking honors it.
func (f vectorFilterSQL) sql() string {
	var sb strings.Builder
	addEq := func(col, val string) {
		if val != "" {
			sb.WriteString(" AND " + col + " = ?")
		}
	}
	addEq("v.project_path", f.projectPath)
	addEq("v.backend", f.backend)
	addEq("v.role", f.role)
	addEq("v.session_id", f.sessionID)
	if f.excludeSessionID != "" {
		sb.WriteString(" AND v.session_id != ?")
	}

	if f.fromTime != "" || f.toTime != "" {
		var conds []string
		if f.fromTime != "" {
			conds = append(conds, "created_at >= ?")
		}
		if f.toTime != "" {
			conds = append(conds, "created_at <= ?")
		}
		sb.WriteString(" AND v.rowid IN (SELECT id FROM rag_chunks WHERE " + strings.Join(conds, " AND ") + ")")
	}
	return sb.String()
}

// args returns the bind values matching sql(), in the same order.
func (f vectorFilterSQL) args() []any {
	var args []any
	appendIfSet := func(val string) {
		if val != "" {
			args = append(args, val)
		}
	}
	appendIfSet(f.projectPath)
	appendIfSet(f.backend)
	appendIfSet(f.role)
	appendIfSet(f.sessionID)
	appendIfSet(f.excludeSessionID)
	appendIfSet(f.fromTime)
	appendIfSet(f.toTime)
	return args
}

// SearchVector performs vector similarity search using sqlite-vec KNN.
func (s *Store) SearchVector(queryEmbedding []float64, limit int, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime string) ([]SearchHit, error) {
	if err := validateEmbedding(queryEmbedding); err != nil {
		return nil, fmt.Errorf("query embedding validation: %w", err)
	}

	// Check vec0 table exists (may not exist if no vectors have been indexed yet)
	if !s.vecTableExists() {
		return nil, nil
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
	filter := vectorFilterSQL{
		projectPath:      projectPath,
		backend:          backend,
		role:             role,
		sessionID:        sessionID,
		excludeSessionID: excludeSessionID,
		fromTime:         fromTime,
		toTime:           toTime,
	}
	filterArgs := filter.args()
	// Preallocate: vecBlob + k, the metadata filters, and the trailing LIMIT.
	args := make([]any, 0, 2+len(filterArgs)+1)
	args = append(args, vecBlob, limit*2) // over-fetch for post-filtering
	query += filter.sql()
	args = append(args, filterArgs...)

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
	// Check if rag_vec table exists first
	var tableExists int
	err := s.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='rag_vec'").Scan(&tableExists)
	if err != nil || tableExists == 0 {
		return false
	}
	var count int
	err = s.db.QueryRow("SELECT COUNT(*) FROM rag_vec").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// HasFTSData returns true if the FTS5 table contains any indexed chunks.
func (s *Store) HasFTSData() bool {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM rag_chunks_fts").Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// SearchFTS performs BM25 full-text search using SQLite FTS5.
func (s *Store) SearchFTS(queryText string, limit int, projectPath, backend, role, sessionID, excludeSessionID, fromTime, toTime string) ([]SearchHit, error) {
	// Segment the query for Chinese support and split into individual terms.
	tokens := SegmentTokens(queryText)

	// Build an OR query over the individual terms: any term may match, and
	// BM25 ranks chunks matching more terms higher. Order of terms is irrelevant.
	// Each term is wrapped in double quotes so FTS5 special operators (AND, OR,
	// NOT, NEAR, *, "") in user input are treated as plain text (ISS-283); any
	// embedded double-quote characters are escaped by doubling them.
	quotedTerms := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if strings.TrimSpace(tok) == "" {
			continue
		}
		quotedTerms = append(quotedTerms, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
	}
	if len(quotedTerms) == 0 {
		// Empty or whitespace-only query: no terms to match, return no results.
		return nil, nil
	}
	ftsQuery := strings.Join(quotedTerms, " OR ")

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
	total, embedded, err := s.ChunkEmbeddingCounts()
	return total - embedded, err
}

// GetPendingEmbeddings returns chunks that need embedding backfill, including metadata for vec0.
func (s *Store) GetPendingEmbeddings(limit int) ([]PendingChunk, error) {
	rows, err := s.db.Query("SELECT id, chunk_text, project_path, backend, role, session_id FROM rag_chunks WHERE has_embedding = 0 ORDER BY created_at DESC, id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var pending []PendingChunk
	for rows.Next() {
		var p PendingChunk
		if err := rows.Scan(&p.ID, &p.ChunkText, &p.ProjectPath, &p.Backend, &p.Role, &p.SessionID); err != nil {
			return nil, err
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}

// backfillOneChunk writes the vec row and then the embedding columns for a
// single pending chunk. It reports whether the chunk was fully backfilled.
//
// The vector row is written FIRST: has_embedding must only be set once the vec
// row exists, otherwise the chunk is flagged embedded but invisible to vector
// search and will never be retried by the backfill pass.
func backfillOneChunk(emb []float64, p PendingChunk, vecDim int, deleteVecStmt, insertVecStmt, updateStmt *sql.Stmt) bool {
	if err := validateEmbedding(emb); err != nil {
		return false
	}
	// Skip wrong-width vectors rather than letting them fail the vec insert
	// and leave the chunk flagged embedded without a vector row.
	if vecDim > 0 && len(emb) != vecDim {
		slog.Warn("rag: skipping backfill embedding with wrong dimension",
			slog.Int64("chunk_id", p.ID), slog.Int("got", len(emb)), slog.Int("want", vecDim))
		return false
	}

	_, _ = deleteVecStmt.Exec(p.ID)
	vecBlob := serializeFloat32(float64ToFloat32(emb))
	if _, err := insertVecStmt.Exec(p.ID, vecBlob, p.ProjectPath, p.Backend, p.Role, p.SessionID); err != nil {
		slog.Warn("rag: batch insert vec failed", slog.Int64("chunk_id", p.ID), slog.String("err", err.Error()))
		return false
	}

	embBlob := serializeEmbedding(emb)
	if _, err := updateStmt.Exec(embBlob, len(emb), p.ID); err != nil {
		slog.Warn("rag: batch update chunk failed", slog.Int64("chunk_id", p.ID), slog.String("err", err.Error()))
		return false
	}
	return true
}

// BatchUpdateEmbeddings updates embeddings for multiple chunks.
// Also inserts vectors into the vec0 index. Returns the number of chunks updated.
// Caller is responsible for batching at appropriate size (typically embedSubBatchSize).
func (s *Store) BatchUpdateEmbeddings(pendingChunks []PendingChunk, embeddings [][]float64) (int, error) {
	if len(pendingChunks) == 0 {
		return 0, nil
	}

	// Ensure vec0 table exists before starting the transaction.
	// Must NOT be called while holding writeMu or from within a transaction.
	if err := s.ensureVecTable(); err != nil {
		return 0, fmt.Errorf("ensure vec0 table for batch update: %w", err)
	}

	// Resolve the table width before the transaction (see insertOneChunk).
	vecDim, err := s.ragVecDim()
	if err != nil {
		return 0, fmt.Errorf("resolve rag_vec dimension for batch update: %w", err)
	}

	s.writeMu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.writeMu.Unlock()
		return 0, fmt.Errorf("begin batch update transaction: %w", err)
	}

	updateStmt, err := tx.Prepare(`UPDATE rag_chunks SET embedding = ?, has_embedding = 1, embedding_dim = ? WHERE id = ?`)
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("prepare update stmt: %w", err)
	}

	deleteVecStmt, err := tx.Prepare(`DELETE FROM rag_vec WHERE rowid = ?`)
	if err != nil {
		_ = updateStmt.Close()
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("prepare delete vec stmt: %w", err)
	}

	insertVecStmt, err := tx.Prepare(
		`INSERT INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		_ = deleteVecStmt.Close()
		_ = updateStmt.Close()
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("prepare insert vec stmt: %w", err)
	}

	backfilled := 0
	// pendingChunks and embeddings are parallel slices; embeddings may be
	// shorter (or carry nil holes) when embedding failed for some chunks, so
	// bound the walk to the shorter length.
	limit := len(pendingChunks)
	if len(embeddings) < limit {
		limit = len(embeddings)
	}
	for i := range limit {
		emb := embeddings[i]
		if emb == nil {
			continue
		}
		if backfillOneChunk(emb, pendingChunks[i], vecDim, deleteVecStmt, insertVecStmt, updateStmt) {
			backfilled++
		}
	}

	_ = insertVecStmt.Close()
	_ = deleteVecStmt.Close()
	_ = updateStmt.Close()

	if err := tx.Commit(); err != nil {
		s.writeMu.Unlock()
		return backfilled, fmt.Errorf("commit batch update: %w", err)
	}
	s.writeMu.Unlock()

	return backfilled, nil
}

// UpdateEmbedding updates the embedding for a specific chunk (for backfill).
// Also inserts the vector into the vec0 index.
func (s *Store) UpdateEmbedding(chunkID int64, embedding []float64) error {
	// Validate embedding
	if err := validateEmbedding(embedding); err != nil {
		return fmt.Errorf("embedding validation for update: %w", err)
	}

	embBlob := serializeEmbedding(embedding)

	// Ensure vec0 table exists before starting the transaction.
	// Must NOT be called while holding writeMu or from within a transaction.
	if err := s.ensureVecTable(); err != nil {
		slog.Warn("rag: skipping vec0 upsert — table not available", slog.String("err", err.Error()))
		// Fall through — still update rag_chunks without vec0
	}

	s.writeMu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.writeMu.Unlock()
		return fmt.Errorf("begin update embedding transaction: %w", err)
	}

	// Write the vec row first so has_embedding is only set once the chunk is
	// actually searchable by vector. Setting the flag before (or despite) a
	// failed vec insert would hide the chunk from the backfill pass, which only
	// selects has_embedding = 0.
	_, _ = tx.Exec(`DELETE FROM rag_vec WHERE rowid = ?`, chunkID)
	vecBlob := serializeFloat32(float64ToFloat32(embedding))
	_, vecErr := tx.Exec(
		`INSERT INTO rag_vec(rowid, embedding, project_path, backend, role, session_id)
		VALUES (?, ?, ?, ?, ?, ?)`,
		chunkID, vecBlob, "", "", "", "",
	)
	if vecErr != nil {
		// Vec0 unavailable (no dimension yet) — store the embedding BLOB but
		// leave has_embedding = 0 so the backfill pass retries later.
		slog.Warn("rag: vec0 insert failed in UpdateEmbedding, leaving chunk pending",
			slog.String("err", vecErr.Error()))
	} else {
		// Fetch metadata and update vec0 columns
		var projectPath, backend, role, sessionID string
		_ = tx.QueryRow(
			`SELECT project_path, backend, role, session_id FROM rag_chunks WHERE id = ?`,
			chunkID,
		).Scan(&projectPath, &backend, &role, &sessionID)
		_, _ = tx.Exec(
			`UPDATE rag_vec SET project_path = ?, backend = ?, role = ?, session_id = ? WHERE rowid = ?`,
			projectPath, backend, role, sessionID, chunkID,
		)
	}

	// Update rag_chunks; has_embedding reflects whether the vec row landed.
	hasEmb := 0
	if vecErr == nil {
		hasEmb = 1
	}
	_, err = tx.Exec(
		`
		UPDATE rag_chunks
		SET embedding = ?, has_embedding = ?, embedding_dim = ?
		WHERE id = ?`,
		embBlob, hasEmb, len(embedding), chunkID,
	)
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return fmt.Errorf("update embedding: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.writeMu.Unlock()
		return fmt.Errorf("commit update embedding: %w", err)
	}
	s.writeMu.Unlock()

	return nil
}

// ragVecDim returns the embedding dimension of the existing rag_vec table, or 0
// if the table does not exist. The dimension is fixed at CREATE time, so the
// table's own DDL is the only authoritative source once it exists.
func (s *Store) ragVecDim() (int, error) {
	var ddl string
	err := s.db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='rag_vec'",
	).Scan(&ddl)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read rag_vec schema: %w", err)
	}
	// The DDL contains `embedding float[N] distance_metric=cosine`.
	idx := strings.Index(ddl, "float[")
	if idx < 0 {
		return 0, fmt.Errorf("rag_vec schema missing dimension: %s", ddl)
	}
	rest := ddl[idx+len("float["):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return 0, fmt.Errorf("rag_vec schema malformed dimension: %s", ddl)
	}
	dim, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0, fmt.Errorf("parse rag_vec dimension %q: %w", rest[:end], err)
	}
	return dim, nil
}

// CheckDimensionMismatch reports whether the embedding dimension the caller is
// about to use differs from the dimension of the existing rag_vec table.
//
// This must compare against the rag_vec table's actual DDL, NOT against
// s.embDim: s.embDim is loaded from rag_chunks.embedding_dim at startup, so
// comparing the two would always report "match" and a changed embedding model
// (e.g. 1024 → 768) would be silently accepted, making every subsequent vector
// insert fail against the old-width table.
//
// Returns the existing dimension (0 when there is no vec table) and whether the
// caller must reset before inserting vectors of a different width.
func (s *Store) CheckDimensionMismatch(newDim int) (int, bool, error) {
	existing, err := s.ragVecDim()
	if err != nil {
		return 0, false, err
	}
	if existing == 0 || newDim <= 0 {
		return existing, false, nil
	}
	return existing, existing != newDim, nil
}

// SetEmbeddingDim sets the embedding dimension. Returns true if it changed.
func (s *Store) SetEmbeddingDim(dim int) bool {
	if dim == s.getEmbDim() {
		return false
	}
	s.setEmbDimInternal(dim)
	return true
}

// ResetForDimensionMismatch clears all chunks, FTS, and vec0 when dimension changes.
// The vec0 virtual table must be dropped and recreated because its dimension
// is fixed at CREATE time and cannot be altered.
func (s *Store) ResetForDimensionMismatch(newDim int) error {
	s.writeMu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.writeMu.Unlock()
		return fmt.Errorf("begin reset transaction: %w", err)
	}

	// Delete FTS entries
	_, err = tx.Exec("DELETE FROM rag_chunks_fts")
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return fmt.Errorf("delete fts: %w", err)
	}

	// Delete main table
	_, err = tx.Exec("DELETE FROM rag_chunks")
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return fmt.Errorf("delete chunks: %w", err)
	}

	// Drop vec0 table (dimension is fixed at CREATE time)
	_, err = tx.Exec("DROP TABLE IF EXISTS rag_vec")
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return fmt.Errorf("drop rag_vec: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.writeMu.Unlock()
		return fmt.Errorf("commit reset: %w", err)
	}
	s.writeMu.Unlock()

	s.setEmbDimInternal(newDim)
	s.invalidateVecTable() // rag_vec was dropped, need to re-confirm
	// rag_vec will be recreated by ensureVecTable() on next vector insert
	slog.Info("rag: dropped rag_vec, will recreate with new dimension", slog.Int("dim", newDim))
	return nil
}

// RebuildFTS rebuilds the full-text index from the existing chunk text without
// touching chunk rows, the vector index, or message indexed flags.
//
// Because rag_chunks_fts is an external-content FTS5 table (content='rag_chunks'),
// its index can be regenerated directly from rag_chunks via the FTS5 'rebuild'
// command. Dropping and recreating the table first guarantees a clean slate even
// if the index was corrupted or out of sync, and keeps the operation independent
// of the vector layer.
//
// Returns the number of chunks re-indexed.
func (s *Store) RebuildFTS() (int64, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin fts rebuild transaction: %w", err)
	}

	_, err = tx.Exec("DROP TABLE IF EXISTS rag_chunks_fts")
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("drop rag_chunks_fts: %w", err)
	}

	_, err = tx.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS rag_chunks_fts USING fts5(
			chunk_text_segmented,
			content='rag_chunks',
			content_rowid='id',
			tokenize='unicode61'
		)
	`)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("recreate rag_chunks_fts: %w", err)
	}

	if _, err = tx.Exec(`INSERT INTO rag_chunks_fts(rag_chunks_fts) VALUES('rebuild')`); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("rebuild rag_chunks_fts: %w", err)
	}

	var indexed int64
	if err = tx.QueryRow("SELECT COUNT(*) FROM rag_chunks").Scan(&indexed); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("count rebuilt chunks: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit fts rebuild: %w", err)
	}

	slog.Info("rag: FTS index rebuilt from existing chunks", slog.Int64("chunks", indexed))
	return indexed, nil
}

// ResetVectorOnly clears vector embedding data only, keeping FTS and chunk text intact.
// Drops the rag_vec table and resets has_embedding flags so the indexer will re-embed
// all chunks with the current model. Chunk text, FTS index, and indexed message flags
// are preserved — only the vector layer is rebuilt.
func (s *Store) ResetVectorOnly(newDim int) (int64, error) {
	s.writeMu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.writeMu.Unlock()
		return 0, fmt.Errorf("begin vector reset transaction: %w", err)
	}

	// Drop vec0 table (dimension is fixed at CREATE time)
	_, err = tx.Exec("DROP TABLE IF EXISTS rag_vec")
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("drop rag_vec: %w", err)
	}

	// Reset has_embedding flags so indexer will re-embed existing chunks
	result, err := tx.Exec("UPDATE rag_chunks SET has_embedding = 0, embedding = NULL, embedding_dim = 0")
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("reset has_embedding flags: %w", err)
	}
	chunksReset, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("count reset chunks: %w", err)
	}

	if err := tx.Commit(); err != nil {
		s.writeMu.Unlock()
		return 0, fmt.Errorf("commit vector reset: %w", err)
	}
	s.writeMu.Unlock()

	s.setEmbDimInternal(newDim)
	s.invalidateVecTable()
	slog.Info("rag: vector reset complete, will re-embed with new dimension", slog.Int("dim", newDim), slog.Int64("chunks_reset", chunksReset))
	return chunksReset, nil
}

// ChunkCount returns the total number of chunks in the store.
func (s *Store) ChunkCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM rag_chunks").Scan(&count)
	return count, err
}

// IndexDiskUsage returns the logical on-disk footprint (in bytes) of the FTS
// and vector indexes separately, by summing page sizes from SQLite's dbstat
// virtual table.
//
// Both indexes live in the same ClawBench.db file: the FTS5 index is stored in
// its shadow tables (rag_chunks_fts_*), and sqlite-vec stores the vector index
// in rag_vec_* shadow tables plus the sqlite_autoindex_rag_vec_* indexes.
//
// Note: these are logical page sizes, not file sizes. Deleting a table returns
// its pages to SQLite's freelist without shrinking the database file, so the
// reported usage may not drop until a VACUUM runs. dbstat may be unavailable on
// some builds; callers should treat an error as "size unknown".
//
// Performance: dbstat only uses its name index when the table name is filtered
// by a direct `name IN (SELECT ...)` WHERE clause. Expressing the same filter
// as `SUM(CASE WHEN name LIKE 'prefix%' ...)` degrades to a full scan of every
// page in the database — ~57s on a 14 GB store versus ~0.3s with the indexed
// form — so the two prefixes are summed by separate indexed queries.
// TestStore_IndexDiskUsage_UsesIndexedPlan guards against regressing to the
// full-scan form.
func (s *Store) IndexDiskUsage() (ftsBytes int64, vecBytes int64, err error) {
	if err = s.db.QueryRow(ftsDiskUsageQuery).Scan(&ftsBytes); err != nil {
		return 0, 0, fmt.Errorf("fts disk usage: %w", err)
	}
	if err = s.db.QueryRow(vecDiskUsageQuery).Scan(&vecBytes); err != nil {
		return 0, 0, fmt.Errorf("vector disk usage: %w", err)
	}
	return ftsBytes, vecBytes, nil
}

// dbstatDiskUsageQueries. The `name IN (SELECT ...)` form is load-bearing:
// dbstat only uses its name index for a direct IN-subquery filter (query plan
// "SCAN dbstat VIRTUAL TABLE INDEX 2"). Any rewrite that keeps the prefix match
// in a CASE/SUM expression turns this into a full page scan (INDEX 0).
const (
	ftsDiskUsageQuery = `
		SELECT COALESCE(SUM(pgsize), 0)
		FROM dbstat
		WHERE name IN (SELECT name FROM sqlite_master WHERE name LIKE 'rag_chunks_fts%')
	`
	vecDiskUsageQuery = `
		SELECT COALESCE(SUM(pgsize), 0)
		FROM dbstat
		WHERE name IN (SELECT name FROM sqlite_master
			WHERE name LIKE 'rag_vec%' OR name LIKE 'sqlite_autoindex_rag_vec%')
	`
)

// EmbeddedChunkCount returns the number of chunks that have vector embeddings.
func (s *Store) EmbeddedChunkCount() (int, error) {
	_, embedded, err := s.ChunkEmbeddingCounts()
	return embedded, err
}

// ChunkEmbeddingCounts returns (total, embedded) chunk counts in a single query.
func (s *Store) ChunkEmbeddingCounts() (total int, embedded int, err error) {
	err = s.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(CASE WHEN has_embedding = 1 THEN 1 ELSE 0 END), 0) FROM rag_chunks",
	).Scan(&total, &embedded)
	return
}

// EmbeddedMessageCount returns the number of messages whose chunks all have embeddings.
func (s *Store) EmbeddedMessageCount() (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(DISTINCT a.message_id) FROM rag_chunks a
		WHERE NOT EXISTS (
			SELECT 1 FROM rag_chunks b
			WHERE b.message_id = a.message_id AND b.has_embedding = 0
		)
	`).Scan(&count)
	return count, err
}

// GetMessageIndexStatus returns FTS and vector embedding status for a specific message.
// ftsIndexed is true if the message has any chunks in rag_chunks (FTS is always synced on INSERT).
// vecIndexed is true if all chunks for the message have embeddings (has_embedding = 1).
func (s *Store) GetMessageIndexStatus(messageID int64) (ftsIndexed bool, vecIndexed bool, err error) {
	var total int
	var pending int
	err = s.db.QueryRow(
		"SELECT COUNT(*), COALESCE(SUM(CASE WHEN has_embedding = 0 THEN 1 ELSE 0 END), 0) FROM rag_chunks WHERE message_id = ?",
		messageID,
	).Scan(&total, &pending)
	if err != nil {
		return false, false, err
	}
	if total == 0 {
		return false, false, nil
	}
	return true, pending == 0, nil
}

// DeleteChunksBySessionIDs deletes all chunks belonging to the given session IDs.
// FTS entries are deleted in the same transaction for consistency.
func (s *Store) DeleteChunksBySessionIDs(sessionIDs []string) (int64, error) {
	if len(sessionIDs) == 0 {
		return 0, nil
	}

	// Check vec0 table existence before starting transaction (avoids deadlock with in-memory DBs)
	hasVecTable := s.vecTableExists()

	placeholders := strings.Repeat("?,", len(sessionIDs)-1) + "?"
	args := make([]any, len(sessionIDs))
	for i, id := range sessionIDs {
		args[i] = id
	}

	s.writeMu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.writeMu.Unlock()
		return 0, fmt.Errorf("begin delete transaction: %w", err)
	}

	// Delete vec0 entries (table may not exist if dimension is unknown)
	if hasVecTable {
		_, err = tx.Exec("DELETE FROM rag_vec WHERE rowid IN (SELECT id FROM rag_chunks WHERE session_id IN ("+placeholders+"))", args...)
		if err != nil {
			_ = tx.Rollback()
			s.writeMu.Unlock()
			return 0, fmt.Errorf("delete vec entries: %w", err)
		}
	}

	// Delete FTS entries
	_, err = tx.Exec("DELETE FROM rag_chunks_fts WHERE rowid IN (SELECT id FROM rag_chunks WHERE session_id IN ("+placeholders+"))", args...)
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("delete fts entries: %w", err)
	}

	// Delete main table
	result, err := tx.Exec("DELETE FROM rag_chunks WHERE session_id IN ("+placeholders+")", args...)
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("delete chunks: %w", err)
	}
	affected, _ := result.RowsAffected()

	if err := tx.Commit(); err != nil {
		s.writeMu.Unlock()
		return 0, fmt.Errorf("commit delete: %w", err)
	}
	s.writeMu.Unlock()

	return affected, nil
}

// DeleteChunksBySessionAfterMessage deletes all chunks belonging to the given
// session whose message_id is strictly greater than anchorID. Used by the
// rewind/truncate path so orphan chunks never surface as stale search hits
// after the underlying messages (and any messages after them) are deleted in
// place.
//
// The range predicate (session_id = ? AND message_id > ?) is intentionally used
// instead of a pre-captured message-id list: the RAG indexer may insert chunks
// for not-yet-indexed messages concurrently between the chat_history truncation
// and this cleanup, and a range delete covers those too. FTS and vec0 entries
// are deleted in the same transaction for consistency.
func (s *Store) DeleteChunksBySessionAfterMessage(sessionID string, anchorID int64) (int64, error) {
	// Check vec0 table existence before starting transaction (avoids deadlock with in-memory DBs)
	hasVecTable := s.vecTableExists()

	s.writeMu.Lock()
	tx, err := s.db.Begin()
	if err != nil {
		s.writeMu.Unlock()
		return 0, fmt.Errorf("begin delete transaction: %w", err)
	}

	// Delete vec0 entries (table may not exist if dimension is unknown)
	if hasVecTable {
		_, err = tx.Exec("DELETE FROM rag_vec WHERE rowid IN (SELECT id FROM rag_chunks WHERE session_id = ? AND message_id > ?)", sessionID, anchorID)
		if err != nil {
			_ = tx.Rollback()
			s.writeMu.Unlock()
			return 0, fmt.Errorf("delete vec entries: %w", err)
		}
	}

	// Delete FTS entries
	_, err = tx.Exec("DELETE FROM rag_chunks_fts WHERE rowid IN (SELECT id FROM rag_chunks WHERE session_id = ? AND message_id > ?)", sessionID, anchorID)
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("delete fts entries: %w", err)
	}

	// Delete main table
	result, err := tx.Exec("DELETE FROM rag_chunks WHERE session_id = ? AND message_id > ?", sessionID, anchorID)
	if err != nil {
		_ = tx.Rollback()
		s.writeMu.Unlock()
		return 0, fmt.Errorf("delete chunks: %w", err)
	}
	affected, _ := result.RowsAffected()

	if err := tx.Commit(); err != nil {
		s.writeMu.Unlock()
		return 0, fmt.Errorf("commit delete: %w", err)
	}
	s.writeMu.Unlock()

	return affected, nil
}

// FTSIntegrityCheck verifies FTS5 index consistency.
func (s *Store) FTSIntegrityCheck() error {
	s.writeMu.Lock()
	_, err := s.db.Exec("INSERT INTO rag_chunks_fts(rag_chunks_fts) VALUES('integrity-check')")
	s.writeMu.Unlock()
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
