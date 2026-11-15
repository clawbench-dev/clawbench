//nolint:noctx // the KB store has no per-request context; writes are serialized by the global write lock
package kb

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawbench/internal/rag"
	"clawbench/internal/store"

	_ "modernc.org/sqlite"     // register SQLite driver (pure Go, FTS5 built-in)
	_ "modernc.org/sqlite/vec" // register sqlite-vec for vec0 virtual tables
)

// WriteLocker abstracts the global write mutex for database writes. Production
// uses store.WriteLock/WriteUnlock so KB writes serialize with every other
// writer to the same SQLite file; tests use a no-op.
type WriteLocker interface {
	Lock()
	Unlock()
}

type noOpLocker struct{}

func (noOpLocker) Lock()   {}
func (noOpLocker) Unlock() {}

type serviceWriteLocker struct{}

func (serviceWriteLocker) Lock()   { store.WriteLock() }
func (serviceWriteLocker) Unlock() { store.WriteUnlock() }

// Store owns the KB tables and their FTS/vector indexes.
type Store struct {
	db *sql.DB

	mu            sync.RWMutex
	embDim        int
	vecTableReady bool

	writeMu WriteLocker
}

// NewStore opens the KB tables on the application database.
func NewStore(dbPath string) (*Store, error) {
	return newStore(dbPath, serviceWriteLocker{})
}

// NewStoreForTest opens a store without the global write lock.
func NewStoreForTest(dbPath string) (*Store, error) {
	return newStore(dbPath, noOpLocker{})
}

func newStore(dbPath string, locker WriteLocker) (*Store, error) {
	dsn := dbPath
	if dbPath == ":memory:" {
		dsn = "file::memory:?cache=shared"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy_timeout: %w", err)
	}
	if dbPath == ":memory:" {
		db.SetMaxOpenConns(1)
	}

	s := &Store{db: db, writeMu: locker}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init kb schema: %w", err)
	}
	s.loadEmbeddingDimFromDB()
	return s, nil
}

func (s *Store) initSchema() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS kb_collections (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			scope       TEXT NOT NULL CHECK(scope IN ('global', 'project')),
			project_id  INTEGER NOT NULL DEFAULT 0,
			auto_inject INTEGER NOT NULL DEFAULT 1,
			pinned      INTEGER NOT NULL DEFAULT 0,
			created_at  DATETIME NOT NULL,
			updated_at  DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_kb_collections_project ON kb_collections(project_id);

		CREATE TABLE IF NOT EXISTS kb_documents (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			collection_id INTEGER NOT NULL,
			title         TEXT NOT NULL,
			source_path   TEXT NOT NULL DEFAULT '',
			stored_path   TEXT NOT NULL DEFAULT '',
			ext           TEXT NOT NULL DEFAULT '',
			size_bytes    INTEGER NOT NULL DEFAULT 0,
			content_hash  TEXT NOT NULL DEFAULT '',
			tags          TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL DEFAULT 'pending',
			error         TEXT NOT NULL DEFAULT '',
			warning       TEXT NOT NULL DEFAULT '',
			chunk_count   INTEGER NOT NULL DEFAULT 0,
			fts_ready     INTEGER NOT NULL DEFAULT 0,
			vector_ready  INTEGER NOT NULL DEFAULT 0,
			created_at    DATETIME NOT NULL,
			updated_at    DATETIME NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_kb_documents_collection ON kb_documents(collection_id);
		CREATE INDEX IF NOT EXISTS idx_kb_documents_status ON kb_documents(status);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_documents_hash ON kb_documents(collection_id, content_hash);

		CREATE TABLE IF NOT EXISTS kb_document_usage (
			document_id INTEGER NOT NULL,
			session_id  TEXT NOT NULL,
			used_at     INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_kb_document_usage_doc ON kb_document_usage(document_id);

		CREATE TABLE IF NOT EXISTS kb_chunks (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id          INTEGER NOT NULL,
			collection_id        INTEGER NOT NULL,
			project_id           INTEGER NOT NULL DEFAULT 0,
			chunk_index          INTEGER NOT NULL DEFAULT 0,
			chunk_text           TEXT NOT NULL,
			chunk_text_segmented TEXT NOT NULL,
			location             TEXT NOT NULL DEFAULT '',
			token_count          INTEGER NOT NULL DEFAULT 0,
			embedding            BLOB,
			has_embedding        INTEGER NOT NULL DEFAULT 0,
			embedding_dim        INTEGER NOT NULL DEFAULT 0,
			created_at           DATETIME NOT NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_chunks_doc_index ON kb_chunks(document_id, chunk_index);
		CREATE INDEX IF NOT EXISTS idx_kb_chunks_collection ON kb_chunks(collection_id);
		CREATE INDEX IF NOT EXISTS idx_kb_chunks_project ON kb_chunks(project_id);
		CREATE INDEX IF NOT EXISTS idx_kb_chunks_pending ON kb_chunks(id) WHERE has_embedding = 0;

		CREATE VIRTUAL TABLE IF NOT EXISTS kb_chunks_fts USING fts5(
			chunk_text_segmented,
			content='kb_chunks',
			content_rowid='id',
			tokenize='unicode61'
		);
	`)
	if err != nil {
		return fmt.Errorf("create kb tables: %w", err)
	}

	// The projects registry is owned by the service package, but a store opened
	// on its own (tests, a standalone run) still needs it to resolve paths.
	if _, err := s.db.Exec(store.ProjectsDDL); err != nil {
		return fmt.Errorf("create projects table: %w", err)
	}
	// Existing installs predate the pinned column.
	if err := s.addColumnIfMissing("kb_collections", "pinned", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("kb_documents", "tags", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	return nil
}

func (s *Store) loadEmbeddingDimFromDB() {
	var dim int
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(embedding_dim), 0) FROM kb_chunks WHERE has_embedding = 1`).Scan(&dim)
	if dim > 0 {
		s.mu.Lock()
		s.embDim = dim
		s.mu.Unlock()
	}
}

func (s *Store) setEmbDim(dim int) {
	s.mu.Lock()
	s.embDim = dim
	s.mu.Unlock()
}

func (s *Store) isVecTableReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vecTableReady
}

func (s *Store) markVecTableReady() {
	s.mu.Lock()
	s.vecTableReady = true
	s.mu.Unlock()
}

func (s *Store) invalidateVecTable() {
	s.mu.Lock()
	s.vecTableReady = false
	s.mu.Unlock()
}

var vecDimRe = regexp.MustCompile(`float\[(\d+)\]`)

// vecDimFromSchema returns the width of the existing kb_vec table, if any.
func (s *Store) vecDimFromSchema() (int, bool) {
	var ddl string
	err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='kb_vec'`).Scan(&ddl)
	if err != nil || ddl == "" {
		return 0, false
	}
	m := vecDimRe.FindStringSubmatch(ddl)
	if len(m) != 2 {
		return 0, false
	}
	dim, err := strconv.Atoi(m[1])
	if err != nil || dim <= 0 {
		return 0, false
	}
	return dim, true
}

func (s *Store) vecTableExists() bool {
	if s.isVecTableReady() {
		return true
	}
	if _, ok := s.vecDimFromSchema(); ok {
		s.markVecTableReady()
		return true
	}
	return false
}

// ensureVecTable creates kb_vec with the given width. Must not be called while
// holding writeMu or from inside a transaction.
func (s *Store) ensureVecTable(dim int) error {
	if dim <= 0 {
		return fmt.Errorf("cannot create kb_vec: embedding dimension unknown")
	}
	if s.isVecTableReady() {
		return nil
	}
	if d, ok := s.vecDimFromSchema(); ok {
		s.setEmbDim(d)
		s.markVecTableReady()
		return nil
	}

	s.writeMu.Lock()
	_, err := s.db.Exec(fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS kb_vec USING vec0(
			embedding float[%d] distance_metric=cosine,
			collection_id INTEGER,
			project_id INTEGER
		)
	`, dim))
	s.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("create kb_vec: %w", err)
	}
	s.setEmbDim(dim)
	s.markVecTableReady()
	slog.Info("kb: created kb_vec table", slog.Int("dim", dim))
	return nil
}

// EnsureDimension reconciles the store's vector width with the embedder's.
// A changed width (the user switched embedding models) drops kb_vec and clears
// every chunk's vector; the indexer re-embeds them. Keyword search keeps working
// throughout, so the only cost is a temporary loss of semantic hits.
func (s *Store) EnsureDimension(dim int) error {
	if dim <= 0 {
		return fmt.Errorf("embedding dimension must be positive")
	}
	if d, ok := s.vecDimFromSchema(); ok {
		if d == dim {
			s.setEmbDim(dim)
			s.markVecTableReady()
			return nil
		}
		slog.Warn("kb: embedding dimension changed, rebuilding vector index",
			slog.Int("old", d), slog.Int("new", dim))
		return s.ResetVector(dim)
	}
	s.setEmbDim(dim)
	return s.ensureVecTable(dim)
}

// ResetVector drops kb_vec and marks every chunk unembedded. When dim > 0 the
// table is recreated with that width immediately; otherwise the next embedding
// batch creates it.
func (s *Store) ResetVector(dim int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin reset vector: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE IF EXISTS kb_vec`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("drop kb_vec: %w", err)
	}
	if _, err := tx.Exec(`UPDATE kb_chunks SET has_embedding = 0, embedding = NULL, embedding_dim = 0`); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear chunk embeddings: %w", err)
	}
	if _, err := tx.Exec(`UPDATE kb_documents SET vector_ready = 0, updated_at = ?`, time.Now()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("clear document vector flags: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reset vector: %w", err)
	}

	s.invalidateVecTable()
	s.setEmbDim(0)
	if dim > 0 {
		return s.ensureVecTable(dim)
	}
	return nil
}

// addColumnIfMissing issues ALTER TABLE ADD COLUMN unless the column already
// exists. Safe to run on every startup.
func (s *Store) addColumnIfMissing(table, column, decl string) error {
	var exists int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, column,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if exists > 0 {
		return nil
	}
	if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, decl)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// --- Collections ---

func (s *Store) CreateCollection(c Collection) (int64, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if c.Scope != ScopeGlobal && c.Scope != ScopeProject {
		return 0, ErrInvalidScope
	}
	if c.Scope == ScopeGlobal {
		c.ProjectID = 0
	}
	now := time.Now()
	res, err := s.db.Exec(
		`INSERT INTO kb_collections (name, description, scope, project_id, auto_inject, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.Description, c.Scope, c.ProjectID, boolToInt(c.AutoInject), now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert collection: %w", err)
	}
	return res.LastInsertId()
}

// ListCollections returns global collections plus those of the given project.
// projectID 0 means "global only".
func (s *Store) ListCollections(projectID int64) ([]Collection, error) {
	query := `
		SELECT c.id, c.name, c.description, c.scope, c.project_id,
		       COALESCE(p.path, ''), c.auto_inject, c.pinned, c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM kb_documents d WHERE d.collection_id = c.id),
		       (SELECT COUNT(*) FROM kb_documents d WHERE d.collection_id = c.id AND d.status = 'failed'),
		       (SELECT COUNT(*) FROM kb_documents d WHERE d.collection_id = c.id AND d.status IN ('pending', 'extracting'))
		FROM kb_collections c
		LEFT JOIN projects p ON p.id = c.project_id
		WHERE c.project_id = 0`
	args := []any{}
	if projectID != 0 {
		query += " OR c.project_id = ?"
		args = append(args, projectID)
	}
	query += " ORDER BY CASE WHEN c.project_id = 0 THEN 0 ELSE 1 END, c.pinned DESC, c.created_at ASC, c.id ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Collection
	for rows.Next() {
		var c Collection
		var autoInject int
		var pinned int
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Scope, &c.ProjectID,
			&c.ProjectPath, &autoInject, &pinned, &c.CreatedAt, &c.UpdatedAt,
			&c.DocCount, &c.FailedCount, &c.PendingCount); err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		c.AutoInject = autoInject != 0
		c.Pinned = pinned != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetCollection(id int64) (*Collection, error) {
	var c Collection
	var autoInject int
	var pinned int
	err := s.db.QueryRow(`
		SELECT c.id, c.name, c.description, c.scope, c.project_id,
		       COALESCE(p.path, ''), c.auto_inject, c.pinned, c.created_at, c.updated_at,
		       (SELECT COUNT(*) FROM kb_documents d WHERE d.collection_id = c.id),
		       (SELECT COUNT(*) FROM kb_documents d WHERE d.collection_id = c.id AND d.status = 'failed'),
		       (SELECT COUNT(*) FROM kb_documents d WHERE d.collection_id = c.id AND d.status IN ('pending', 'extracting'))
		FROM kb_collections c
		LEFT JOIN projects p ON p.id = c.project_id
		WHERE c.id = ?`, id,
	).Scan(&c.ID, &c.Name, &c.Description, &c.Scope, &c.ProjectID,
		&c.ProjectPath, &autoInject, &pinned, &c.CreatedAt, &c.UpdatedAt,
		&c.DocCount, &c.FailedCount, &c.PendingCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCollectionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get collection: %w", err)
	}
	c.AutoInject = autoInject != 0
	c.Pinned = pinned != 0
	return &c, nil
}

// UpdateCollection applies only the supplied fields.
func (s *Store) UpdateCollection(id int64, name, description *string, autoInject, pinned *bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	sets := []string{"updated_at = ?"}
	args := []any{time.Now()}
	if name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *name)
	}
	if description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *description)
	}
	if autoInject != nil {
		sets = append(sets, "auto_inject = ?")
		args = append(args, boolToInt(*autoInject))
	}
	if pinned != nil {
		sets = append(sets, "pinned = ?")
		args = append(args, boolToInt(*pinned))
	}
	args = append(args, id)
	res, err := s.db.Exec("UPDATE kb_collections SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return fmt.Errorf("update collection: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCollectionNotFound
	}
	return nil
}

// DeleteCollection removes the collection, its documents, chunks and index
// entries. The caller removes the collection's directory afterwards.
func (s *Store) DeleteCollection(id int64) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin delete collection: %w", err)
	}
	if delErr := deleteChunksByCollection(tx, id); delErr != nil {
		_ = tx.Rollback()
		return delErr
	}
	if _, delErr := tx.Exec(`DELETE FROM kb_documents WHERE collection_id = ?`, id); delErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete documents: %w", delErr)
	}
	res, execErr := tx.Exec(`DELETE FROM kb_collections WHERE id = ?`, id)
	if execErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete collection: %w", execErr)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_ = tx.Rollback()
		return ErrCollectionNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete collection: %w", err)
	}
	return nil
}

// ListCollectionIDs returns every collection visible from projectID.
func (s *Store) ListCollectionIDs(projectID int64) ([]int64, error) {
	return s.listCollectionIDs(projectID, false)
}

// ListInjectCollectionIDs returns the collections that opted into automatic
// injection, restricted to what is visible from projectID.
func (s *Store) ListInjectCollectionIDs(projectID int64) ([]int64, error) {
	return s.listCollectionIDs(projectID, true)
}

func (s *Store) listCollectionIDs(projectID int64, injectOnly bool) ([]int64, error) {
	var sb strings.Builder
	sb.WriteString("SELECT id FROM kb_collections WHERE ")
	if injectOnly {
		sb.WriteString("auto_inject = 1 AND ")
	}
	sb.WriteString("(project_id = 0")
	args := []any{}
	if projectID != 0 {
		sb.WriteString(" OR project_id = ?")
		args = append(args, projectID)
	}
	sb.WriteString(")")

	rows, err := s.db.Query(sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list collection ids: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// FindDocumentByHash returns the document with the same content in a collection,
// or nil when there is none.
func (s *Store) FindDocumentByHash(collectionID int64, hash string) (*Document, error) {
	row := s.db.QueryRow(`
		SELECT id, collection_id, title, source_path, stored_path, ext, size_bytes,
		       content_hash, tags, status, error, warning, chunk_count, fts_ready, vector_ready,
		       created_at, updated_at
		FROM kb_documents WHERE collection_id = ? AND content_hash = ? LIMIT 1`,
		collectionID, hash)
	d, err := scanDocument(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

// CollectionAccessible reports whether the collection may be used from the
// given project (global collections are accessible everywhere).
func (s *Store) CollectionAccessible(id, projectID int64) (bool, error) {
	var cProject int64
	err := s.db.QueryRow(`SELECT project_id FROM kb_collections WHERE id = ?`, id).Scan(&cProject)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrCollectionNotFound
	}
	if err != nil {
		return false, fmt.Errorf("check collection access: %w", err)
	}
	return cProject == 0 || (projectID != 0 && cProject == projectID), nil
}

// DeleteProjectData removes every collection owned by the project and returns
// their ids so the caller can delete the on-disk directories.
func (s *Store) DeleteProjectData(projectID int64) ([]int64, error) {
	if projectID == 0 {
		return nil, nil
	}
	ids, err := s.projectCollectionIDs(projectID)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if err := s.DeleteCollection(id); err != nil {
			return ids, err
		}
	}
	return ids, nil
}

// projectCollectionIDs reads the id list and closes the rows BEFORE returning:
// DeleteCollection opens its own statements, and holding this cursor open would
// deadlock a single-connection store.
func (s *Store) projectCollectionIDs(projectID int64) ([]int64, error) {
	rows, err := s.db.Query(`SELECT id FROM kb_collections WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project collections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// --- Documents ---

func (s *Store) AddDocument(d Document) (int64, error) {
	// Serialize with the indexer's status/chunk writes; without the lock a
	// concurrent index pass makes SQLite return SQLITE_BUSY instead of waiting.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := time.Now()
	if d.Status == "" {
		d.Status = StatusPending
	}
	res, err := s.db.Exec(`
		INSERT INTO kb_documents (collection_id, title, source_path, stored_path, ext,
			size_bytes, content_hash, tags, status, error, warning, chunk_count, fts_ready,
			vector_ready, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', 0, 0, 0, ?, ?)`,
		d.CollectionID, d.Title, d.SourcePath, d.StoredPath, d.Ext,
		d.SizeBytes, d.ContentHash, d.Tags, d.Status, now, now,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return 0, ErrDuplicateDocument
		}
		return 0, fmt.Errorf("insert document: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) ListDocuments(collectionID int64) ([]Document, error) {
	rows, err := s.db.Query(`
		SELECT id, collection_id, title, source_path, stored_path, ext, size_bytes,
		       content_hash, tags, status, error, warning, chunk_count, fts_ready, vector_ready,
		       created_at, updated_at
		FROM kb_documents WHERE collection_id = ? ORDER BY created_at DESC, id DESC`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (s *Store) GetDocument(id int64) (*Document, error) {
	row := s.db.QueryRow(`
		SELECT id, collection_id, title, source_path, stored_path, ext, size_bytes,
		       content_hash, tags, status, error, warning, chunk_count, fts_ready, vector_ready,
		       created_at, updated_at
		FROM kb_documents WHERE id = ?`, id)
	d, err := scanDocument(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDocumentNotFound
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDocument(r rowScanner) (*Document, error) {
	var d Document
	var fts, vec int
	if err := r.Scan(&d.ID, &d.CollectionID, &d.Title, &d.SourcePath, &d.StoredPath,
		&d.Ext, &d.SizeBytes, &d.ContentHash, &d.Tags, &d.Status, &d.Error, &d.Warning, &d.ChunkCount,
		&fts, &vec, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.FTSReady = fts != 0
	d.VectorReady = vec != 0
	return &d, nil
}

// DeleteDocument removes a document and its index entries, returning the stored
// file path so the caller can delete the copy.
func (s *Store) DeleteDocument(id int64) (string, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	var storedPath string
	if err := s.db.QueryRow(`SELECT stored_path FROM kb_documents WHERE id = ?`, id).Scan(&storedPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrDocumentNotFound
		}
		return "", fmt.Errorf("look up document: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", fmt.Errorf("begin delete document: %w", err)
	}
	if err := deleteChunksByDocument(tx, id); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM kb_documents WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return "", fmt.Errorf("delete document: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit delete document: %w", err)
	}
	return storedPath, nil
}

func (s *Store) SetDocumentStatus(id int64, status, errMsg, warning string) error {
	// This is a write like any other; without the process-wide lock it races
	// the import path and SQLite reports SQLITE_BUSY instead of waiting.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.db.Exec(
		`UPDATE kb_documents SET status = ?, error = ?, warning = ?, updated_at = ? WHERE id = ?`,
		status, errMsg, warning, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("set document status: %w", err)
	}
	return nil
}

// UpdateDocumentSource records the new size and content fingerprint after a
// document's stored copy was replaced from its original file.
func (s *Store) UpdateDocumentSource(id, size int64, hash string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err := s.db.Exec(
		`UPDATE kb_documents SET size_bytes = ?, content_hash = ?, updated_at = ? WHERE id = ?`,
		size, hash, time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("update document source: %w", err)
	}
	return nil
}

// MoveDocumentRecord re-points a document at another collection and requeues
// it. The chunks (and their vectors) are dropped in the same transaction: their
// collection/project metadata would otherwise be stale, and re-embedding is
// cheaper than trying to patch a virtual table's metadata.
func (s *Store) MoveDocumentRecord(id, collectionID int64, storedPath string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin document move: %w", err)
	}
	if err := deleteChunksByDocument(tx, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	res, err := tx.Exec(`
		UPDATE kb_documents
		SET collection_id = ?, stored_path = ?, status = 'pending', error = '', warning = '',
		    chunk_count = 0, fts_ready = 0, vector_ready = 0, updated_at = ?
		WHERE id = ?`, collectionID, storedPath, time.Now(), id)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("move document: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return ErrDocumentNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit document move: %w", err)
	}
	return nil
}

// SetDocumentTags replaces a document's comma-separated tag list.
func (s *Store) SetDocumentTags(id int64, tags string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, err := s.db.Exec(
		`UPDATE kb_documents SET tags = ?, updated_at = ? WHERE id = ?`, tags, time.Now(), id)
	if err != nil {
		return fmt.Errorf("set document tags: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

// ResetStuckDocuments requeues documents left in the transient extracting state
// by a crash or shutdown.
func (s *Store) ResetStuckDocuments() error {
	_, err := s.db.Exec(
		`UPDATE kb_documents SET status = 'pending', updated_at = ? WHERE status = 'extracting'`,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("reset stuck documents: %w", err)
	}
	return nil
}

func (s *Store) PendingDocuments(limit int) ([]Document, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := s.db.Query(`
		SELECT id, collection_id, title, source_path, stored_path, ext, size_bytes,
		       content_hash, tags, status, error, warning, chunk_count, fts_ready, vector_ready,
		       created_at, updated_at
		FROM kb_documents WHERE status = 'pending' ORDER BY created_at ASC, id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending documents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ResetDocumentsForReindex clears chunks and queues the documents for a fresh
// extraction. When collectionIDs is empty every collection is reset.
func (s *Store) ResetDocumentsForReindex(collectionIDs []int64) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin reindex reset: %w", err)
	}
	if len(collectionIDs) == 0 {
		if err := deleteAllChunks(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`UPDATE kb_documents SET status = 'pending', error = '', warning = '', chunk_count = 0, fts_ready = 0, vector_ready = 0, updated_at = ?`, time.Now()); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("reset all documents: %w", err)
		}
	} else {
		for _, id := range collectionIDs {
			if err := deleteChunksByCollection(tx, id); err != nil {
				_ = tx.Rollback()
				return err
			}
			if _, err := tx.Exec(`UPDATE kb_documents SET status = 'pending', error = '', warning = '', chunk_count = 0, fts_ready = 0, vector_ready = 0, updated_at = ? WHERE collection_id = ?`, time.Now(), id); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("reset documents of collection %d: %w", id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reindex reset: %w", err)
	}
	return nil
}

// ResetDocumentForReindex clears one document's chunks and queues it again.
func (s *Store) ResetDocumentForReindex(id int64) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin document reindex: %w", err)
	}
	if delErr := deleteChunksByDocument(tx, id); delErr != nil {
		_ = tx.Rollback()
		return delErr
	}
	res, execErr := tx.Exec(`UPDATE kb_documents SET status = 'pending', error = '', warning = '', chunk_count = 0, fts_ready = 0, vector_ready = 0, updated_at = ? WHERE id = ?`, time.Now(), id)
	if execErr != nil {
		_ = tx.Rollback()
		return fmt.Errorf("reset document: %w", execErr)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		_ = tx.Rollback()
		return ErrDocumentNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit document reindex: %w", err)
	}
	return nil
}

// ReplaceChunks swaps a document's chunks atomically. The new chunks start
// without vectors; the indexer backfills them.
func (s *Store) ReplaceChunks(docID int64, chunks []Chunk, warning string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin replace chunks: %w", err)
	}
	if err := deleteChunksByDocument(tx, docID); err != nil {
		_ = tx.Rollback()
		return err
	}
	now := time.Now()
	for i := range chunks {
		c := &chunks[i]
		res, err := tx.Exec(`
			INSERT INTO kb_chunks (document_id, collection_id, project_id, chunk_index,
				chunk_text, chunk_text_segmented, location, token_count,
				has_embedding, embedding_dim, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?)`,
			docID, c.CollectionID, c.ProjectID, c.ChunkIndex,
			c.ChunkText, c.ChunkTextSegmented, c.Location, c.TokenCount, now,
		)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert chunk %d: %w", i, err)
		}
		chunkID, _ := res.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO kb_chunks_fts(rowid, chunk_text_segmented) VALUES (?, ?)`,
			chunkID, c.ChunkTextSegmented); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert fts entry for chunk %d: %w", chunkID, err)
		}
	}
	if _, err := tx.Exec(`
		UPDATE kb_documents
		SET status = 'ready', error = '', warning = ?, chunk_count = ?, fts_ready = 1,
		    vector_ready = 0, updated_at = ?
		WHERE id = ?`, warning, len(chunks), now, docID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update document after chunking: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace chunks: %w", err)
	}
	return nil
}

// ListChunks returns a document's chunks in order, for the detail view.
func (s *Store) ListChunks(documentID int64, limit int) ([]Chunk, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT id, document_id, collection_id, project_id, chunk_index,
		       chunk_text, location, token_count, has_embedding, created_at
		FROM kb_chunks WHERE document_id = ? ORDER BY chunk_index ASC LIMIT ?`, documentID, limit)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var has int
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.CollectionID, &c.ProjectID,
			&c.ChunkIndex, &c.ChunkText, &c.Location, &c.TokenCount, &has, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.HasEmbedding = has != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChunkWindow returns the chunk with the given id plus its neighbors, so a
// search hit that lives past the first page can still be shown in context.
//
// A chunk id that no longer exists (the document was re-indexed between the
// search and the click) returns (nil, -1, nil) instead of an error: the caller
// opens the document at the top, which is still better than a dead end.
func (s *Store) ChunkWindow(documentID, chunkID int64, before, after int) ([]Chunk, int, error) {
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	var target int
	err := s.db.QueryRow(
		`SELECT chunk_index FROM kb_chunks WHERE id = ? AND document_id = ?`,
		chunkID, documentID,
	).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, -1, nil
	}
	if err != nil {
		return nil, -1, fmt.Errorf("locate chunk: %w", err)
	}

	from := target - before
	if from < 0 {
		from = 0
	}
	rows, err := s.db.Query(`
		SELECT id, document_id, collection_id, project_id, chunk_index,
		       chunk_text, location, token_count, has_embedding, created_at
		FROM kb_chunks
		WHERE document_id = ? AND chunk_index BETWEEN ? AND ?
		ORDER BY chunk_index ASC`, documentID, from, target+after)
	if err != nil {
		return nil, -1, fmt.Errorf("list chunk window: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		var has int
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.CollectionID, &c.ProjectID,
			&c.ChunkIndex, &c.ChunkText, &c.Location, &c.TokenCount, &has, &c.CreatedAt); err != nil {
			return nil, -1, err
		}
		c.HasEmbedding = has != 0
		out = append(out, c)
	}
	return out, target, rows.Err()
}

// DocumentProgress returns the volatile fields for every document in a
// collection. The caller merges them into its existing list so only rows that
// actually changed re-render.
func (s *Store) DocumentProgress(collectionID int64) ([]DocumentProgress, error) {
	rows, err := s.db.Query(`
		SELECT id, status, error, warning, chunk_count, fts_ready, vector_ready
		FROM kb_documents WHERE collection_id = ? ORDER BY created_at DESC, id DESC`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list document progress: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DocumentProgress
	for rows.Next() {
		var p DocumentProgress
		var fts, vec int
		if err := rows.Scan(&p.ID, &p.Status, &p.Error, &p.Warning, &p.ChunkCount, &fts, &vec); err != nil {
			return nil, err
		}
		p.FTSReady = fts != 0
		p.VectorReady = vec != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecordDocumentUsage appends one row per cited document (deduped within the
// turn). Best-effort: a failure here must never fail the chat turn.
func (s *Store) RecordDocumentUsage(refs []Ref, sessionID string) error {
	if len(refs) == 0 || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin usage record: %w", err)
	}
	now := time.Now()
	seen := make(map[int64]bool, len(refs))
	for _, r := range refs {
		if r.DocumentID <= 0 || seen[r.DocumentID] {
			continue
		}
		seen[r.DocumentID] = true
		if _, err := tx.Exec(
			`INSERT INTO kb_document_usage (document_id, session_id, used_at) VALUES (?, ?, ?)`,
			r.DocumentID, sessionID, now.UnixMilli(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record usage: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit usage record: %w", err)
	}
	return nil
}

// DocumentUsage returns citation counts for one collection.
func (s *Store) DocumentUsage(collectionID int64) ([]DocumentUsage, error) {
	rows, err := s.db.Query(`
		SELECT u.document_id, COUNT(*), MAX(u.used_at)
		FROM kb_document_usage u
		JOIN kb_documents d ON d.id = u.document_id
		WHERE d.collection_id = ?
		GROUP BY u.document_id`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("list document usage: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []DocumentUsage
	for rows.Next() {
		var u DocumentUsage
		var lastMS int64
		if err := rows.Scan(&u.DocumentID, &u.Count, &lastMS); err != nil {
			return nil, err
		}
		u.LastUsed = time.UnixMilli(lastMS)
		out = append(out, u)
	}
	return out, rows.Err()
}

// --- Embedding backfill ---

// PendingEmbeddingChunks returns ready-document chunks that still need a vector.
func (s *Store) PendingEmbeddingChunks(limit int) ([]Chunk, error) {
	if limit <= 0 {
		limit = 16
	}
	rows, err := s.db.Query(`
		SELECT c.id, c.document_id, c.collection_id, c.project_id, c.chunk_index,
		       c.chunk_text, c.location, c.token_count
		FROM kb_chunks c
		JOIN kb_documents d ON d.id = c.document_id
		WHERE c.has_embedding = 0 AND d.status = 'ready'
		ORDER BY c.document_id ASC, c.chunk_index ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending chunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.DocumentID, &c.CollectionID, &c.ProjectID,
			&c.ChunkIndex, &c.ChunkText, &c.Location, &c.TokenCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveEmbeddings writes vectors into kb_vec and marks the chunks embedded.
func (s *Store) SaveEmbeddings(entries []ChunkEmbedding) error {
	if len(entries) == 0 {
		return nil
	}
	dim := len(entries[0].Vector)
	if dim == 0 {
		return fmt.Errorf("empty embedding")
	}
	if err := s.EnsureDimension(dim); err != nil {
		return err
	}

	// Resolve the table width before opening the transaction: querying inside
	// would deadlock on a single-connection store.
	vecDim, _ := s.vecDimFromSchema()
	if vecDim != 0 && vecDim != dim {
		return fmt.Errorf("embedding dimension mismatch: got %d, kb_vec expects %d", dim, vecDim)
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin save embeddings: %w", err)
	}
	now := time.Now()
	docIDs := map[int64]struct{}{}
	for _, e := range entries {
		if len(e.Vector) != dim {
			_ = tx.Rollback()
			return fmt.Errorf("embedding dimension mismatch inside batch")
		}
		if err := validateEmbedding(e.Vector); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("invalid embedding: %w", err)
		}
		blob := serializeFloat32(float64ToFloat32(e.Vector))
		if _, err := tx.Exec(`
			INSERT INTO kb_vec(rowid, embedding, collection_id, project_id)
			VALUES (?, ?, ?, ?)`, e.ChunkID, blob, e.CollectionID, e.ProjectID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert vec entry for chunk %d: %w", e.ChunkID, err)
		}
		if _, err := tx.Exec(`
			UPDATE kb_chunks SET embedding = ?, has_embedding = 1, embedding_dim = ? WHERE id = ?`,
			serializeEmbedding(e.Vector), dim, e.ChunkID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update chunk %d embedding: %w", e.ChunkID, err)
		}
		docIDs[e.DocumentID] = struct{}{}
	}
	for docID := range docIDs {
		if _, err := tx.Exec(`
			UPDATE kb_documents SET vector_ready = 1, updated_at = ?
			WHERE id = ? AND NOT EXISTS (
				SELECT 1 FROM kb_chunks WHERE document_id = ? AND has_embedding = 0
			)`, now, docID, docID); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update document %d vector flag: %w", docID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save embeddings: %w", err)
	}
	return nil
}

// HasVecData reports whether kb_vec holds any rows.
func (s *Store) HasVecData() bool {
	if !s.vecTableExists() {
		return false
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM kb_vec`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// --- Search ---

// SearchFTS returns keyword hits restricted to collectionIDs.
func (s *Store) SearchFTS(queryText string, limit int, collectionIDs []int64) ([]SearchHit, error) {
	tokens := rag.SegmentTokens(queryText)
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if strings.TrimSpace(t) == "" {
			continue
		}
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	if len(quoted) == 0 || len(collectionIDs) == 0 {
		return nil, nil
	}
	ftsQuery := strings.Join(quoted, " OR ")

	query := `
		SELECT c.id, c.document_id, c.collection_id, c.chunk_text, c.location,
		       bm25(kb_chunks_fts) AS score, d.title, col.name
		FROM kb_chunks_fts
		JOIN kb_chunks c ON c.id = kb_chunks_fts.rowid
		JOIN kb_documents d ON d.id = c.document_id
		JOIN kb_collections col ON col.id = c.collection_id
		WHERE kb_chunks_fts MATCH ?
		  AND c.collection_id IN (` + placeholders(len(collectionIDs)) + `)
		ORDER BY score LIMIT ?`
	args := []any{ftsQuery}
	for _, id := range collectionIDs {
		args = append(args, id)
	}
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("kb fts search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		var score float64
		if err := rows.Scan(&h.ChunkID, &h.DocumentID, &h.CollectionID, &h.ChunkText,
			&h.Location, &score, &h.DocumentTitle, &h.CollectionName); err != nil {
			return nil, fmt.Errorf("scan kb fts hit: %w", err)
		}
		h.FTSScore = -score
		h.Score = h.FTSScore
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// SearchVector returns semantic hits restricted to collectionIDs. Cosine
// distance is converted to similarity (1 - distance).
func (s *Store) SearchVector(queryEmbedding []float64, limit int, collectionIDs []int64) ([]SearchHit, error) {
	if err := validateEmbedding(queryEmbedding); err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}
	if !s.vecTableExists() || len(collectionIDs) == 0 {
		return nil, nil
	}
	blob := serializeFloat32(float64ToFloat32(queryEmbedding))

	query := `
		SELECT v.rowid, v.distance, c.document_id, c.collection_id, c.chunk_text,
		       c.location, d.title, col.name
		FROM kb_vec v
		JOIN kb_chunks c ON c.id = v.rowid
		JOIN kb_documents d ON d.id = c.document_id
		JOIN kb_collections col ON col.id = c.collection_id
		WHERE v.embedding MATCH ? AND v.k = ?
		  AND v.collection_id IN (` + placeholders(len(collectionIDs)) + `)
		ORDER BY v.distance LIMIT ?`
	args := []any{blob, limit}
	for _, id := range collectionIDs {
		args = append(args, id)
	}
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("kb vector search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		var distance float64
		if err := rows.Scan(&h.ChunkID, &distance, &h.DocumentID, &h.CollectionID,
			&h.ChunkText, &h.Location, &h.DocumentTitle, &h.CollectionName); err != nil {
			return nil, fmt.Errorf("scan kb vector hit: %w", err)
		}
		h.VectorScore = 1 - distance
		h.Score = h.VectorScore
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// --- Stats ---

func (s *Store) Stats() (IndexStats, error) {
	var st IndexStats
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM kb_collections`).Scan(&st.Collections); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status = 'ready' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status IN ('pending', 'extracting') THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(size_bytes), 0)
		FROM kb_documents`).Scan(&st.Documents, &st.ReadyDocs, &st.FailedDocs, &st.PendingDocs, &st.StoredBytes); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(has_embedding), 0) FROM kb_chunks`).Scan(&st.Chunks, &st.EmbeddedChunks); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// --- SQL helpers ---

func deleteChunksByDocument(tx *sql.Tx, docID int64) error {
	if _, err := tx.Exec(`
		INSERT INTO kb_chunks_fts(kb_chunks_fts, rowid, chunk_text_segmented)
		SELECT 'delete', id, chunk_text_segmented FROM kb_chunks WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("delete fts entries for document %d: %w", docID, err)
	}
	if _, err := tx.Exec(`DELETE FROM kb_vec WHERE rowid IN (SELECT id FROM kb_chunks WHERE document_id = ?)`, docID); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete vec entries for document %d: %w", docID, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM kb_chunks WHERE document_id = ?`, docID); err != nil {
		return fmt.Errorf("delete chunks for document %d: %w", docID, err)
	}
	return nil
}

func deleteChunksByCollection(tx *sql.Tx, collectionID int64) error {
	if _, err := tx.Exec(`
		INSERT INTO kb_chunks_fts(kb_chunks_fts, rowid, chunk_text_segmented)
		SELECT 'delete', id, chunk_text_segmented FROM kb_chunks WHERE collection_id = ?`, collectionID); err != nil {
		return fmt.Errorf("delete fts entries for collection %d: %w", collectionID, err)
	}
	if _, err := tx.Exec(`DELETE FROM kb_vec WHERE rowid IN (SELECT id FROM kb_chunks WHERE collection_id = ?)`, collectionID); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete vec entries for collection %d: %w", collectionID, err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM kb_chunks WHERE collection_id = ?`, collectionID); err != nil {
		return fmt.Errorf("delete chunks for collection %d: %w", collectionID, err)
	}
	return nil
}

func deleteAllChunks(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		INSERT INTO kb_chunks_fts(kb_chunks_fts, rowid, chunk_text_segmented)
		SELECT 'delete', id, chunk_text_segmented FROM kb_chunks`); err != nil {
		return fmt.Errorf("delete all fts entries: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM kb_vec`); err != nil {
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("delete all vec entries: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM kb_chunks`); err != nil {
		return fmt.Errorf("delete all chunks: %w", err)
	}
	return nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func serializeFloat32(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func serializeEmbedding(v []float64) []byte {
	buf := make([]byte, len(v)*8)
	for i, f := range v {
		binary.LittleEndian.PutUint64(buf[i*8:], math.Float64bits(f))
	}
	return buf
}

func float64ToFloat32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = float32(f)
	}
	return out
}

func validateEmbedding(v []float64) error {
	if len(v) == 0 {
		return fmt.Errorf("embedding is empty")
	}
	for _, f := range v {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("embedding contains NaN or Inf")
		}
	}
	return nil
}
