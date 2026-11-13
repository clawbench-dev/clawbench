package kb

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"clawbench/internal/model"
	"clawbench/internal/rag"
)

var (
	mu             sync.Mutex
	globalStore    *Store
	globalIndexer  *Indexer
	globalEmbedder *rag.EmbeddingClient
	globalCfg      model.KBConfig
	globalRAG      model.RAGConfig
)

// Available reports whether the knowledge base is initialized and enabled.
func Available() bool {
	return currentStore() != nil
}

func currentStore() *Store {
	mu.Lock()
	defer mu.Unlock()
	return globalStore
}

func currentEmbedder() *rag.EmbeddingClient {
	mu.Lock()
	defer mu.Unlock()
	return globalEmbedder
}

func currentConfig() model.KBConfig {
	mu.Lock()
	defer mu.Unlock()
	return globalCfg
}

// KnowledgeDir is where document copies live: {DataDir}/knowledge/<collection>.
func KnowledgeDir() string {
	return filepath.Join(model.DataDir, "knowledge")
}

// Init opens the KB store and prepares the embedder. It is safe to call when
// the feature is disabled (it becomes a no-op).
func Init(cfg model.KBConfig, ragCfg model.RAGConfig) error {
	if !cfg.Enabled {
		return nil
	}
	// The knowledge base segments Chinese text with the same gse dictionary as
	// chat RAG. In production rag.Init runs first, but KB must not depend on
	// that ordering: a RAG init failure (or a test that never calls it) would
	// silently index whole CJK sentences as single FTS tokens, making every
	// short query miss. InitSegmenter is cheap and idempotent for our purposes.
	if !rag.SegmenterAvailable() {
		if err := rag.InitSegmenter(); err != nil {
			slog.Warn("kb: gse segmenter unavailable, Chinese keyword search will be degraded",
				slog.String("err", err.Error()))
		}
	}
	dbPath := filepath.Join(model.DataDir, "ClawBench.db")
	st, err := NewStore(dbPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(KnowledgeDir(), 0o755); err != nil {
		_ = st.Close()
		return fmt.Errorf("create knowledge dir: %w", err)
	}
	if err := st.ResetStuckDocuments(); err != nil {
		slog.Warn("kb: reset stuck documents failed", slog.String("err", err.Error()))
	}

	var emb *rag.EmbeddingClient
	if ragCfg.VectorEnabled && ragCfg.BaseURL != "" && ragCfg.Model != "" {
		emb = rag.NewEmbeddingClient(ragCfg.BaseURL, ragCfg.Model, ragCfg.APIKey)
	}

	mu.Lock()
	old := globalStore
	globalStore = st
	globalCfg = cfg
	globalRAG = ragCfg
	globalEmbedder = emb
	mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	slog.Info("kb: initialized",
		slog.Bool("vector", emb != nil),
		slog.String("dir", KnowledgeDir()))
	return nil
}

// StartIndexer starts the background worker.
func StartIndexer() {
	mu.Lock()
	if globalStore == nil {
		mu.Unlock()
		return
	}
	idx := NewIndexer(globalStore, globalEmbedder, globalCfg, globalRAG)
	globalIndexer = idx
	mu.Unlock()
	idx.Start()
}

// Reconfigure applies a changed configuration without a restart. The indexer is
// recreated so a changed poll interval / batch size takes effect immediately.
func Reconfigure(cfg model.KBConfig, ragCfg model.RAGConfig) {
	mu.Lock()
	oldIndexer := globalIndexer
	globalIndexer = nil
	oldStore := globalStore
	if !cfg.Enabled {
		globalStore = nil
		globalEmbedder = nil
		globalCfg = cfg
		globalRAG = ragCfg
		mu.Unlock()
		if oldIndexer != nil {
			oldIndexer.Stop()
		}
		if oldStore != nil {
			_ = oldStore.Close()
		}
		slog.Info("kb: disabled by configuration")
		return
	}

	var emb *rag.EmbeddingClient
	if ragCfg.VectorEnabled && ragCfg.BaseURL != "" && ragCfg.Model != "" {
		emb = rag.NewEmbeddingClient(ragCfg.BaseURL, ragCfg.Model, ragCfg.APIKey)
	}
	globalCfg = cfg
	globalRAG = ragCfg
	globalEmbedder = emb
	st := globalStore
	mu.Unlock()

	if oldIndexer != nil {
		oldIndexer.Stop()
	}
	if st == nil {
		if err := Init(cfg, ragCfg); err != nil {
			slog.Warn("kb: re-init failed", slog.String("err", err.Error()))
			return
		}
	}
	StartIndexer()
}

// Shutdown stops the worker and closes the store.
func Shutdown() {
	mu.Lock()
	idx := globalIndexer
	globalIndexer = nil
	st := globalStore
	globalStore = nil
	globalEmbedder = nil
	mu.Unlock()
	if idx != nil {
		idx.Stop()
	}
	if st != nil {
		_ = st.Close()
	}
}

func triggerIndexer() {
	mu.Lock()
	idx := globalIndexer
	mu.Unlock()
	if idx != nil {
		idx.Trigger()
	}
}

func maxFileBytes() int64 {
	cfg := currentConfig()
	mb := cfg.MaxFileMB
	if mb <= 0 {
		mb = 50
	}
	return int64(mb) << 20
}

// MaxFileBytes is the configured per-file cap, used by the upload handler to
// size its request limit.
func MaxFileBytes() int64 {
	return maxFileBytes()
}

// AddDocumentFromPath copies a file into a collection and queues it.
func AddDocumentFromPath(collectionID int64, sourcePath string) (*Document, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	if _, err := st.GetCollection(collectionID); err != nil {
		return nil, err
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, &FileError{Reason: ReasonNotRegular, Err: ErrUnsupportedFile}
	}
	ext := strings.ToLower(filepath.Ext(sourcePath))
	if vErr := ValidateFile(sourcePath, ext, info.Size(), maxFileBytes()); vErr != nil {
		return nil, vErr
	}

	dest, err := newStoredPath(collectionID, filepath.Base(sourcePath), ext)
	if err != nil {
		return nil, err
	}
	hash, size, err := copyFileWithHash(sourcePath, dest)
	if err != nil {
		_ = os.Remove(dest)
		return nil, err
	}
	return finishAdd(st, collectionID, filepath.Base(sourcePath), sourcePath, dest, ext, hash, size)
}

// AddDocumentFromReader copies an uploaded stream into a collection.
func AddDocumentFromReader(collectionID int64, name string, r io.Reader) (*Document, error) { //nolint:gocyclo // upload validation: extension, sniff, limit, copy
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	if _, err := st.GetCollection(collectionID); err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(name))
	if reason, bad := unsupportedExtensions[ext]; bad {
		// A typed error so the handler can localize the REASON (legacy .doc,
		// image, archive, …) instead of falling back to a generic message.
		return nil, &FileError{Reason: reason, Detail: ext, Err: ErrUnsupportedFile}
	}

	head := make([]byte, 8192)
	n, readErr := io.ReadFull(r, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("read upload: %w", readErr)
	}
	head = head[:n]
	if !textExtensions[ext] && !isOfficeExt(ext) && ext != ".pdf" {
		for _, b := range head {
			if b == 0 {
				return nil, &FileError{Reason: ReasonBinary, Err: ErrUnsupportedFile}
			}
		}
	}

	dest, err := newStoredPath(collectionID, name, ext)
	if err != nil {
		return nil, err
	}
	out, err := os.Create(dest)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	h := sha256.New()
	limit := maxFileBytes()
	written, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(io.MultiReader(bytes.NewReader(head), r), limit+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		return nil, fmt.Errorf("save upload: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return nil, fmt.Errorf("save upload: %w", closeErr)
	}
	if written > limit {
		_ = os.Remove(dest)
		return nil, &FileError{Reason: ReasonFileTooLarge, Detail: strconv.FormatInt(limit>>20, 10), Err: ErrFileTooLarge}
	}
	return finishAdd(st, collectionID, name, "", dest, ext, hex.EncodeToString(h.Sum(nil)), written)
}

func finishAdd(st *Store, collectionID int64, title, sourcePath, dest, ext, hash string, size int64) (*Document, error) {
	if existing, err := st.FindDocumentByHash(collectionID, hash); err == nil && existing != nil {
		_ = os.Remove(dest)
		return nil, ErrDuplicateDocument
	}
	id, err := st.AddDocument(Document{
		CollectionID: collectionID,
		Title:        title,
		SourcePath:   sourcePath,
		StoredPath:   dest,
		Ext:          ext,
		SizeBytes:    size,
		ContentHash:  hash,
		Status:       StatusPending,
	})
	if err != nil {
		_ = os.Remove(dest)
		return nil, err
	}
	triggerIndexer()
	return st.GetDocument(id)
}

func newStoredPath(collectionID int64, name, ext string) (string, error) {
	dir := filepath.Join(KnowledgeDir(), strconv.FormatInt(collectionID, 10))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create knowledge dir: %w", err)
	}
	base := sanitizeFileName(name)
	if base == "" {
		base = "document" + ext
	}
	return filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), base)), nil
}

func copyFileWithHash(src, dst string) (string, int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return "", 0, fmt.Errorf("create copy: %w", err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), in)
	closeErr := out.Close()
	if copyErr != nil {
		return "", 0, fmt.Errorf("copy file: %w", copyErr)
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("copy file: %w", closeErr)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0, '\n', '\r', '\t':
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 100 {
		runes := []rune(name)
		name = string(runes[:100])
	}
	return name
}

// DeleteDocument removes the row, its index entries and the stored copy.
func DeleteDocument(id int64) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	stored, err := st.DeleteDocument(id)
	if err != nil {
		return err
	}
	if stored != "" {
		if err := os.Remove(stored); err != nil && !os.IsNotExist(err) {
			slog.Warn("kb: remove stored file failed", slog.String("path", stored), slog.String("err", err.Error()))
		}
	}
	return nil
}

// DeleteCollection removes a collection and its directory.
func DeleteCollection(id int64) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	if err := st.DeleteCollection(id); err != nil {
		return err
	}
	dir := filepath.Join(KnowledgeDir(), strconv.FormatInt(id, 10))
	if err := os.RemoveAll(dir); err != nil {
		slog.Warn("kb: remove collection dir failed", slog.String("dir", dir), slog.String("err", err.Error()))
	}
	return nil
}

// DeleteProjectData removes every collection owned by a project. Best-effort:
// the project rows are already gone, so a failure is logged, not returned.
func DeleteProjectData(projectID int64) {
	st := currentStore()
	if st == nil {
		return
	}
	ids, err := st.DeleteProjectData(projectID)
	if err != nil {
		slog.Warn("kb: delete project data failed", slog.Int64("project", projectID), slog.String("err", err.Error()))
	}
	for _, id := range ids {
		_ = os.RemoveAll(filepath.Join(KnowledgeDir(), strconv.FormatInt(id, 10)))
	}
}

// ReindexDocument clears one document and queues it again.
func ReindexDocument(id int64) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	if err := st.ResetDocumentForReindex(id); err != nil {
		return err
	}
	triggerIndexer()
	return nil
}

// ReindexCollections queues every document in the given collections.
func ReindexCollections(collectionIDs []int64) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	if err := st.ResetDocumentsForReindex(collectionIDs); err != nil {
		return err
	}
	triggerIndexer()
	return nil
}

// ResetVectorIndex drops the semantic index; the indexer re-embeds everything.
func ResetVectorIndex() error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	if err := st.ResetVector(0); err != nil {
		return err
	}
	triggerIndexer()
	return nil
}

// --- Collection / document facades used by the HTTP layer ---

func ListCollections(projectID int64) ([]Collection, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.ListCollections(projectID)
}

func GetCollection(id int64) (*Collection, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.GetCollection(id)
}

func CreateCollection(name, description, scope string, projectID int64) (*Collection, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	id, err := st.CreateCollection(Collection{
		Name:        name,
		Description: description,
		Scope:       scope,
		ProjectID:   projectID,
		AutoInject:  true,
	})
	if err != nil {
		return nil, err
	}
	return st.GetCollection(id)
}

func UpdateCollection(id int64, name, description *string, autoInject *bool) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	return st.UpdateCollection(id, name, description, autoInject)
}

func CollectionAccessible(id, projectID int64) (bool, error) {
	st := currentStore()
	if st == nil {
		return false, ErrUnavailable
	}
	return st.CollectionAccessible(id, projectID)
}

func ListDocuments(collectionID int64) ([]Document, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.ListDocuments(collectionID)
}

// ListCollectionIDs returns every collection visible from projectID.
func ListCollectionIDs(projectID int64) ([]int64, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.ListCollectionIDs(projectID)
}

func GetDocument(id int64) (*Document, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.GetDocument(id)
}

func DocumentChunks(id int64, limit int) ([]Chunk, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.ListChunks(id, limit)
}

// VectorEnabled reports whether an embedding client is configured.
func VectorEnabled() bool {
	return currentEmbedder() != nil
}

// Stats returns the current index health snapshot.
func Stats() (IndexStats, error) {
	st := currentStore()
	if st == nil {
		return IndexStats{}, ErrUnavailable
	}
	return st.Stats()
}
