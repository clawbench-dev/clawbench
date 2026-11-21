// Package kb implements the knowledge base: multiple collections, each scoped
// either globally or to one project, with files copied into the data directory
// and indexed for both keyword (FTS5) and semantic (sqlite-vec) search.
//
// It deliberately keeps its own tables. The chat history index (internal/rag)
// has chat-specific columns and invariants (message_id, session_id, per-message
// chunk uniqueness); folding documents into it would make every chat search
// query carry a "not a knowledge base row" predicate and every chat indexer
// pass skip document rows. The two indexes share only the chunker, the Chinese
// segmenter and the embedding client.
package kb

import (
	"errors"
	"time"
)

// Collection scopes.
const (
	ScopeGlobal  = "global"
	ScopeProject = "project"
)

// Document statuses.
const (
	// StatusPending means the document is queued for extraction.
	StatusPending = "pending"
	// StatusExtracting means the indexer is reading the file right now.
	StatusExtracting = "extracting"
	// StatusReady means the keyword index is complete. Semantic indexing may
	// still be in flight; VectorReady reports that separately.
	StatusReady = "ready"
	// StatusFailed means extraction or chunking failed; Error says why.
	StatusFailed = "failed"
)

// Sentinel errors mapped to HTTP statuses by the handler.
var (
	ErrCollectionNotFound = errors.New("knowledge base collection not found")
	ErrDocumentNotFound   = errors.New("knowledge base document not found")
	ErrDuplicateDocument  = errors.New("the same file is already in this collection")
	ErrUnsupportedFile    = errors.New("this file type cannot be indexed")
	ErrFileTooLarge       = errors.New("file exceeds the knowledge base size limit")
	ErrNoText             = errors.New("no extractable text in this file")
	ErrNotDirectory       = errors.New("the path is not a folder")
	ErrInvalidScope       = errors.New("invalid knowledge base scope")
	ErrAccessDenied       = errors.New("knowledge base is not accessible from this project")
	ErrUnavailable        = errors.New("knowledge base is not enabled")
	// ErrSourceMissing means the original file behind a document is gone, so
	// "update from the original" has nothing to read. The stored copy is still
	// intact and searchable; only the refresh path fails.
	ErrSourceMissing = errors.New("the original file is gone")
	// ErrInvalidURL means the address is not a usable http(s) URL.
	ErrInvalidURL = errors.New("not a valid http(s) URL")
	// ErrFetchFailed means the URL could not be fetched (network, non-2xx,
	// oversized body). The remote server's own message is wrapped inside.
	ErrFetchFailed = errors.New("could not fetch the URL")
	// ErrDocumentChanged means the document moved to another collection, was
	// re-queued, or was deleted while it was being extracted, so the freshly
	// produced chunks describe a row that no longer exists in this shape.
	//
	// It is NOT a document failure: the indexer drops the result and leaves the
	// row pending so the next pass re-reads whatever the document is now.
	// Writing the stale chunk set would put searchable text into the old
	// collection (and leave orphans behind a deleted document).
	ErrDocumentChanged = errors.New("the document changed while it was being indexed")
)

// Collection is one knowledge base. A global collection has ProjectID 0 and is
// visible from every project; a project collection is visible only from the
// project it was created in.
type Collection struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Scope       string `json:"scope"`
	ProjectID   int64  `json:"-"`
	ProjectPath string `json:"project_path,omitempty"`
	AutoInject  bool   `json:"auto_inject"`
	DocCount    int    `json:"doc_count"`
	// FailedCount / PendingCount let the collection rail show which library
	// needs attention without opening every one of them. PendingCount covers
	// both queued and extracting documents (the two states the user reads as
	// "still working").
	FailedCount  int       `json:"failed_count"`
	PendingCount int       `json:"pending_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Document is one file added to a collection. StoredPath is the copy inside
// {DataDir}/knowledge/<collection id>/; SourcePath is the original location and
// is display-only (the original may be moved or deleted).
type Document struct {
	ID           int64  `json:"id"`
	CollectionID int64  `json:"collection_id"`
	Title        string `json:"title"`
	SourcePath   string `json:"source_path,omitempty"`
	StoredPath   string `json:"-"`
	Ext          string `json:"ext,omitempty"`
	SizeBytes    int64  `json:"size_bytes"`
	ContentHash  string `json:"-"`
	// Tags is a free-form, comma-separated label list for organization.
	Tags        string `json:"tags"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	Warning     string `json:"warning,omitempty"`
	ChunkCount  int    `json:"chunk_count"`
	FTSReady    bool   `json:"fts_ready"`
	VectorReady bool   `json:"vector_ready"`
	// SourceMissing / SourceStale are derived at read time (never stored):
	// they answer "can I still refresh this document, and is it already out of
	// date?" without a background scanner.
	SourceMissing bool      `json:"source_missing,omitempty"`
	SourceStale   bool      `json:"source_stale,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// DocumentUsage is how often a document was cited in chat.
type DocumentUsage struct {
	DocumentID int64     `json:"document_id"`
	Count      int       `json:"count"`
	LastUsed   time.Time `json:"last_used"`
}

// DocumentProgress is the small, volatile slice of a document that polling
// needs. Shipping the full document list every couple of seconds is wasteful,
// and replacing the list would re-render every row instead of just the ones
// whose status changed.
type DocumentProgress struct {
	ID          int64  `json:"id"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	Warning     string `json:"warning,omitempty"`
	ChunkCount  int    `json:"chunk_count"`
	FTSReady    bool   `json:"fts_ready"`
	VectorReady bool   `json:"vector_ready"`
}

// Chunk is one searchable segment of a document. Location is a human-readable
// position ("第 12 页", "工作表 报价", "第 30–45 行") and travels with the hit so
// the user can find the passage in the original file.
type Chunk struct {
	ID           int64  `json:"id"`
	DocumentID   int64  `json:"document_id"`
	CollectionID int64  `json:"collection_id"`
	ProjectID    int64  `json:"project_id"`
	ChunkIndex   int    `json:"chunk_index"`
	ChunkText    string `json:"chunk_text"`
	// Segmented text and the embedding vector are index internals. They used
	// to be serialized by the document-detail endpoint (no json tags), which
	// shipped thousands of floats to the browser for every chunk.
	ChunkTextSegmented string    `json:"-"`
	Location           string    `json:"location,omitempty"`
	TokenCount         int       `json:"token_count"`
	Embedding          []float64 `json:"-"`
	HasEmbedding       bool      `json:"has_embedding"`
	EmbeddingDim       int       `json:"embedding_dim"`
	CreatedAt          time.Time `json:"created_at"`
}

// ChunkEmbedding pairs a chunk id with its computed vector.
type ChunkEmbedding struct {
	ChunkID      int64
	DocumentID   int64
	CollectionID int64
	ProjectID    int64
	Vector       []float64
}

// MatchRange is a rune-offset span inside ChunkText, used for highlighting.
type MatchRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// SearchHit is one result. Score is the fused (RRF) score; VectorScore and
// FTSScore are kept so callers can apply a similarity floor to vector-only
// hits without penalizing keyword matches.
type SearchHit struct {
	ChunkID        int64        `json:"chunk_id"`
	DocumentID     int64        `json:"document_id"`
	DocumentTitle  string       `json:"document_title"`
	CollectionID   int64        `json:"collection_id"`
	CollectionName string       `json:"collection_name"`
	Location       string       `json:"location,omitempty"`
	ChunkText      string       `json:"chunk_text"`
	Score          float64      `json:"score"`
	VectorScore    float64      `json:"vector_score,omitempty"`
	FTSScore       float64      `json:"fts_score,omitempty"`
	MatchPositions []MatchRange `json:"match_positions,omitempty"`
}

// SearchResult is the response of a knowledge base search.
type SearchResult struct {
	Results []SearchHit `json:"results"`
	Total   int         `json:"total"`
	Mode    string      `json:"mode"` // "hybrid" | "vector" | "fts"
	// VectorDegraded is set when the request asked for semantic search but only
	// keyword results are present: no embedding model, a dead endpoint, or a
	// vector table whose width no longer matches the model. Mode is "fts" in
	// that case; the flag exists so the panel can say WHY instead of pretending
	// the query simply had no semantic matches.
	VectorDegraded bool `json:"vector_degraded,omitempty"`
	// VectorError is the user-facing reason (never raw SQL).
	VectorError string `json:"vector_error,omitempty"`
}

// Stats is the index health snapshot shown in the panel.
type IndexStats struct {
	Collections    int   `json:"collections"`
	Documents      int   `json:"documents"`
	ReadyDocs      int   `json:"ready_docs"`
	FailedDocs     int   `json:"failed_docs"`
	PendingDocs    int   `json:"pending_docs"`
	Chunks         int   `json:"chunks"`
	EmbeddedChunks int   `json:"embedded_chunks"`
	StoredBytes    int64 `json:"stored_bytes"`
}

// Ref is one injected snippet, kept for logging and the UI chip.
type Ref struct {
	// ChunkID is what lets the chat citation open the exact passage. Messages
	// written before it existed simply have no chunk id and open at the top.
	ChunkID        int64  `json:"chunk_id"`
	DocumentID     int64  `json:"document_id"`
	DocumentTitle  string `json:"document_title"`
	CollectionID   int64  `json:"collection_id"`
	CollectionName string `json:"collection_name"`
	Location       string `json:"location,omitempty"`
	Text           string `json:"text"`
}
