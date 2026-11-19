package kb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
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

	// reconcileWG tracks the background embedding-width reconciliation so
	// Shutdown can wait for it before the store is closed under it.
	reconcileWG sync.WaitGroup
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
	// Reconcile the vector width as soon as a model is configured. Without it
	// the mismatch is only noticed by SaveEmbeddings, i.e. only when there
	// happens to be a pending chunk — a library that was fully embedded with
	// the old model keeps the old width, hybrid search silently degrades to
	// keywords and mode:"vector" fails on every request until someone adds a
	// new file.
	//
	// It runs in the background: Init is called while the server boots, and a
	// probe against a slow (or unreachable) embedding endpoint must not hold up
	// startup. Until it finishes, search reports vector_degraded instead of
	// pretending the semantic index worked.
	if emb != nil {
		reconcileWG.Add(1)
		go reconcileEmbeddingDimension(st, emb)
	}
	return nil
}

// reconcileEmbeddingDimension aligns kb_vec with the configured model. It is
// best-effort and must never fail Init: the knowledge base stays usable with
// keyword search even when the embedding service is down.
//
// It makes exactly one cheap embed call (a single short token), and only when
// there is something to compare: a mismatch is only detectable by asking the
// model for its width, and skipping the probe would leave the stale table in
// place, which is the bug this exists to fix. When the probe itself errors the
// embedder is unusable and every vector search falls back anyway, so nothing is
// dropped on a guess.
func reconcileEmbeddingDimension(st *Store, emb *rag.EmbeddingClient) {
	defer reconcileWG.Done()
	if st == nil || emb == nil {
		return
	}
	oldDim, haveTable := st.vecDimFromSchema()
	if !haveTable && !st.HasEmbeddings() {
		// Nothing was ever embedded: SaveEmbeddings creates the table at the
		// right width on the first batch.
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe, err := emb.Embed(ctx, "dimension probe")
	if err != nil || len(probe) == 0 {
		slog.Warn("kb: embedding dimension probe failed, keeping the existing vector index",
			slog.Bool("table", haveTable), slog.String("err", errText(err)))
		return
	}
	if haveTable && oldDim == len(probe) {
		return
	}
	if err := st.EnsureDimension(len(probe)); err != nil {
		slog.Warn("kb: reconcile embedding dimension failed", slog.String("err", err.Error()))
		return
	}
	slog.Info("kb: vector index rebuilt for the configured embedding model",
		slog.Int("old_dim", oldDim), slog.Int("new_dim", len(probe)))
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
	} else {
		// Same reconciliation Init does: a model switch arrives through this
		// path (settings are patched live), and without it the stale width only
		// shows up when a new chunk happens to be embedded.
		if emb != nil {
			reconcileWG.Add(1)
			go reconcileEmbeddingDimension(st, emb)
		}
	}
	StartIndexer()
}

// Shutdown stops the worker and closes the store.
func Shutdown() {
	reconcileWG.Wait()
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
	if !textExtensions[ext] && !isOfficeExt(ext) && ext != extPDF {
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

// maxURLBytes caps a remote document. Larger responses are refused instead of
// being silently truncated: a half-indexed page is worse than a clear error.
const maxURLBytes = 20 << 20

// maxURLRedirects is how many redirects a fetched page may follow.
const maxURLRedirects = 5

// AddDocumentFromURL fetches an http(s) address and indexes it like an
// uploaded file. Only the response body is kept; the URL is not recorded as a
// source path, so "update from original" does not apply to URL documents.
//
// The fetch is user-initiated, but its result is indexed and then injected into
// AI prompts automatically, so the target is restricted to public addresses
// (see newURLFetchClient).
func AddDocumentFromURL(ctx context.Context, collectionID int64, rawURL string) (*Document, error) {
	return addDocumentFromURL(ctx, collectionID, rawURL, true)
}

// newURLFetchClient builds the HTTP client a page fetch uses. guard=false is
// test-only and drops the address filter entirely (tests run against loopback
// httptest servers); everything else is identical.
//
// With the guard on, the address is judged twice: CheckRedirect re-checks every
// hop (a public URL redirecting to 169.254.169.254 would otherwise index the
// metadata service), and DialContext judges the address actually connected to,
// which is the only place a public hostname resolving to a LAN address can be
// caught.
func newURLFetchClient(guard bool) *http.Client {
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}
	if !guard {
		return client
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxURLRedirects {
			return fmt.Errorf("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("redirect to a non-http URL")
		}
		if isPrivateHost(req.URL.Host) {
			return &FileError{Reason: ReasonPrivateAddress, Detail: req.URL.Hostname(), Err: ErrPrivateAddress}
		}
		return nil
	}
	if transport, isHTTP := client.Transport.(*http.Transport); isHTTP {
		transport.DialContext = dialPublicOnly(10 * time.Second)
	}
	return client
}

// addDocumentFromURL does the fetch. guard=false is test-only: httptest servers
// listen on loopback, so the whole filter (URL, redirect, dial) is skipped —
// deciding it in one place is what keeps a "guard off" test testable instead of
// half-enforced.
func addDocumentFromURL(ctx context.Context, collectionID int64, rawURL string, guard bool) (*Document, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	if _, err := st.GetCollection(collectionID); err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, ErrInvalidURL
	}
	if guard && isPrivateHost(u.Host) {
		return nil, &FileError{Reason: ReasonPrivateAddress, Detail: u.Hostname(), Err: ErrPrivateAddress}
	}

	client := newURLFetchClient(guard)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, ErrInvalidURL
	}
	req.Header.Set("User-Agent", "ClawBench/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/pdf;q=0.9,*/*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetchFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrFetchFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxURLBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFetchFailed, err)
	}
	if len(body) > maxURLBytes {
		return nil, &FileError{Reason: ReasonFileTooLarge, Detail: strconv.Itoa(maxURLBytes >> 20), Err: ErrFileTooLarge}
	}

	return AddDocumentFromReader(collectionID, urlDocumentName(u), bytes.NewReader(body))
}

// urlDocumentName picks a title/extension for a fetched page. Known document
// extensions are kept; everything else is treated as HTML, because a page
// served from "/article" or "/view.php" is still HTML and must go through the
// HTML extractor rather than the plain-text one.
func urlDocumentName(u *url.URL) string {
	name := path.Base(u.Path)
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		name = u.Host
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0, '\n', '\r', '\t':
			return '_'
		}
		return r
	}, name)
	switch strings.ToLower(filepath.Ext(name)) {
	case extPDF, ".docx", ".docm", ".dotx", ".dotm",
		".xlsx", ".xlsm", ".xltx", ".xltm",
		".pptx", ".pptm", ".potx", ".potm", ".ppsx", ".ppsm",
		".md", ".markdown", ".txt", ".csv", ".html", ".htm":
		return name
	default:
		return name + ".html"
	}
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

// MoveDocument moves one document to another collection. The stored copy is
// moved on disk, the row is re-pointed, and the document is requeued for
// indexing (its chunks carried the old collection/project metadata).
func MoveDocument(id, targetCollectionID int64) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	doc, err := st.GetDocument(id)
	if err != nil {
		return err
	}
	if _, collErr := st.GetCollection(targetCollectionID); collErr != nil {
		return collErr
	}
	if doc.CollectionID == targetCollectionID {
		return nil
	}
	// Same content in the target would violate the unique index.
	if doc.ContentHash != "" {
		other, findErr := st.FindDocumentByHash(targetCollectionID, doc.ContentHash)
		if findErr != nil {
			return findErr
		}
		if other != nil {
			return ErrDuplicateDocument
		}
	}

	dest, err := newStoredPath(targetCollectionID, doc.Title, doc.Ext)
	if err != nil {
		return err
	}
	if err := moveStoredFile(doc.StoredPath, dest); err != nil {
		return err
	}
	if err := st.MoveDocumentRecord(id, targetCollectionID, dest); err != nil {
		// Put the file back so the row still points at something real.
		_ = moveStoredFile(dest, doc.StoredPath)
		return err
	}
	triggerIndexer()
	return nil
}

// moveStoredFile renames when possible (same filesystem) and falls back to a
// copy + delete across devices.
func moveStoredFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if _, _, err := copyFileWithHash(src, dst); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("copy stored file: %w", err)
	}
	if err := os.Remove(src); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove old copy: %w", err)
	}
	return nil
}

// RefreshDocumentFromSource re-reads the original file, replaces the stored
// copy when its content changed, and reindexes only in that case.
//
// The copy goes to a temporary sibling and is renamed into place: a failed copy
// must not destroy the copy the current index points at. On Windows a rename
// cannot overwrite, so the fallback removes the target first; the index is
// reset immediately afterwards, so the indexer never sees a half-written file.
func RefreshDocumentFromSource(id int64) (bool, error) {
	st := currentStore()
	if st == nil {
		return false, ErrUnavailable
	}
	doc, err := st.GetDocument(id)
	if err != nil {
		return false, err
	}
	source, err := refreshableSource(doc)
	if err != nil {
		return false, err
	}

	hash, err := hashFile(source)
	if err != nil {
		return false, err
	}
	if hash == doc.ContentHash {
		return false, nil
	}
	// The new content may already exist as another document in this
	// collection; the unique (collection_id, content_hash) index would reject
	// the update, so report the same duplicate error the add path uses.
	if other, findErr := st.FindDocumentByHash(doc.CollectionID, hash); findErr != nil {
		return false, findErr
	} else if other != nil && other.ID != doc.ID {
		return false, ErrDuplicateDocument
	}

	tmp := fmt.Sprintf("%s.%d.refresh", doc.StoredPath, time.Now().UnixNano())
	_, newSize, err := copyFileWithHash(source, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := replaceFile(tmp, doc.StoredPath); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := st.UpdateDocumentSource(id, newSize, hash); err != nil {
		return false, err
	}
	if err := st.ResetDocumentForReindex(id); err != nil {
		return false, err
	}
	triggerIndexer()
	return true, nil
}

// refreshableSource validates that the original file is still there and is
// something the knowledge base can index. Split out to keep the refresh flow
// readable (and below the complexity limit).
func refreshableSource(doc *Document) (string, error) {
	source := strings.TrimSpace(doc.SourcePath)
	if source == "" {
		return "", ErrSourceMissing
	}
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrSourceMissing
		}
		return "", fmt.Errorf("read source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", &FileError{Reason: ReasonNotRegular, Err: ErrUnsupportedFile}
	}
	ext := strings.ToLower(filepath.Ext(source))
	if vErr := ValidateFile(source, ext, info.Size(), maxFileBytes()); vErr != nil {
		return "", vErr
	}
	return source, nil
}

func replaceFile(tmp, dst string) error {
	if err := os.Rename(tmp, dst); err == nil {
		return nil
	}
	// Windows: rename cannot overwrite an existing file.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace stored copy: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("replace stored copy: %w", err)
	}
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ReindexCollections queues every document in the given collections. A nil
// slice means every collection; an empty non-nil slice means none (the store is
// strict about the difference, so callers that resolved "visible here" to an
// empty list cannot clear the whole installation).
func ReindexCollections(collectionIDs []int64) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	if collectionIDs != nil && len(collectionIDs) == 0 {
		return nil
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

func UpdateCollection(id int64, name, description *string, autoInject, pinned *bool) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	return st.UpdateCollection(id, name, description, autoInject, pinned)
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
	docs, err := st.ListDocuments(collectionID)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		decorateSourceState(&docs[i])
	}
	return docs, nil
}

// decorateSourceState fills the two read-time flags the panel uses to say
// "the original changed" or "the original is gone". It stats only when the
// document has a source path; URL uploads and text notes do not.
func decorateSourceState(doc *Document) {
	source := strings.TrimSpace(doc.SourcePath)
	if source == "" {
		return
	}
	src, err := os.Stat(source)
	if err != nil {
		doc.SourceMissing = true
		return
	}
	if !src.Mode().IsRegular() {
		return
	}
	stored, err := os.Stat(doc.StoredPath)
	if err != nil {
		return
	}
	doc.SourceStale = src.ModTime().After(stored.ModTime())
}

// DocumentProgress returns the volatile fields polling needs for one
// collection, without the full document rows.
func ListDocumentProgress(collectionID int64) ([]DocumentProgress, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.DocumentProgress(collectionID)
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
	doc, err := st.GetDocument(id)
	if err != nil {
		return nil, err
	}
	decorateSourceState(doc)
	return doc, nil
}

func DocumentChunks(id int64, limit int) ([]Chunk, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.ListChunks(id, limit)
}

// DocumentChunkWindow returns the chunks around a known chunk id. The second
// result is the target's chunk_index, or -1 when the id is gone.
func DocumentChunkWindow(id, chunkID int64, before, after int) ([]Chunk, int, error) {
	st := currentStore()
	if st == nil {
		return nil, -1, ErrUnavailable
	}
	return st.ChunkWindow(id, chunkID, before, after)
}

// SetDocumentTags replaces a document's comma-separated tag list.
func SetDocumentTags(id int64, tags string) error {
	st := currentStore()
	if st == nil {
		return ErrUnavailable
	}
	return st.SetDocumentTags(id, tags)
}

// RecordDocumentUsage records which documents were cited in one chat turn.
// Best-effort: usage tracking must never fail the turn.
func RecordDocumentUsage(refs []Ref, sessionID string) {
	st := currentStore()
	if st == nil {
		return
	}
	_ = st.RecordDocumentUsage(refs, sessionID)
}

// ListDocumentUsage returns citation counts for one collection.
func ListDocumentUsage(collectionID int64) ([]DocumentUsage, error) {
	st := currentStore()
	if st == nil {
		return nil, ErrUnavailable
	}
	return st.DocumentUsage(collectionID)
}

// VectorEnabled reports whether an embedding client is configured.
func VectorEnabled() bool {
	return currentEmbedder() != nil
}

// Stats returns the current index health snapshot for the whole installation.
func Stats() (IndexStats, error) {
	st := currentStore()
	if st == nil {
		return IndexStats{}, ErrUnavailable
	}
	return st.Stats()
}

// StatsForProject returns the snapshot restricted to the collections a project
// can see (its own plus the global ones). projectID <= 0 means "whole
// installation" and is equivalent to Stats().
func StatsForProject(projectID int64) (IndexStats, error) {
	st := currentStore()
	if st == nil {
		return IndexStats{}, ErrUnavailable
	}
	return st.StatsForProject(projectID)
}
