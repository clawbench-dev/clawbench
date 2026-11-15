package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"clawbench/internal/kb"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

const (
	fieldDetail = "detail"
	fieldQueued = "queued"
)

// kbProjectID resolves the current project cookie to its registry id without
// creating a row (listing collections must not register a project as a side
// effect). An unknown project behaves like "no project": global collections
// still work.
func kbProjectID(r *http.Request) int64 {
	path := middleware.GetProjectFromCookie(r)
	if path == "" {
		return 0
	}
	id, ok, err := store.ProjectIDByPath(path)
	if err != nil {
		slog.Warn("kb: resolve project id failed", slog.String("path", path), slog.String("err", err.Error()))
		return 0
	}
	if !ok {
		return 0
	}
	return id
}

func writeKBError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, kb.ErrCollectionNotFound), errors.Is(err, kb.ErrDocumentNotFound):
		writeLocalizedErrorf(w, r, http.StatusNotFound, "KBNotFound")
	case errors.Is(err, kb.ErrDuplicateDocument):
		writeLocalizedErrorf(w, r, http.StatusConflict, "KBDuplicate")
	case errors.Is(err, kb.ErrFileTooLarge), errors.Is(err, kb.ErrUnsupportedFile), errors.Is(err, kb.ErrNoText):
		var fileErr *kb.FileError
		if errors.As(err, &fileErr) {
			status := http.StatusBadRequest
			if errors.Is(err, kb.ErrFileTooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			data := map[string]any{fieldDetail: fileErr.Detail}
			if fileErr.Reason == kb.ReasonFileTooLarge {
				data["MaxMB"] = fileErr.Detail
				data["LimitMB"] = strconv.FormatInt(kb.MaxFileBytes()>>20, 10)
			}
			writeLocalizedErrorf(w, r, status, kbReasonKey(fileErr.Reason), data)
			return
		}
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "KBUnsupportedFile")
	case errors.Is(err, kb.ErrAccessDenied):
		writeLocalizedErrorf(w, r, http.StatusForbidden, "AccessDenied")
	case errors.Is(err, kb.ErrInvalidScope):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
	case errors.Is(err, kb.ErrNotDirectory):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "KBNotDirectory")
	case errors.Is(err, kb.ErrSourceMissing):
		writeLocalizedErrorf(w, r, http.StatusConflict, "KBSourceMissing")
	case errors.Is(err, kb.ErrInvalidURL):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "KBInvalidURL")
	case errors.Is(err, kb.ErrFetchFailed):
		writeLocalizedErrorf(w, r, http.StatusBadGateway, "KBFetchFailed")
	case errors.Is(err, kb.ErrUnavailable):
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "KBUnavailable")
	default:
		writeLocalizedError(w, r, model.Internal(err))
	}
}

// kbReasonKey maps a stable extraction reason to its i18n key. The codes live
// in the kb package (which must not depend on i18n); the wording lives in the
// locale tables.
func kbReasonKey(reason string) string { //nolint:gocyclo // flat reason-code lookup table
	switch reason {
	case kb.ReasonLegacyDoc:
		return "KBReasonLegacyDoc"
	case kb.ReasonLegacyXls:
		return "KBReasonLegacyXls"
	case kb.ReasonLegacyPpt:
		return "KBReasonLegacyPpt"
	case kb.ReasonImage:
		return "KBReasonImage"
	case kb.ReasonSVG:
		return "KBReasonSVG"
	case kb.ReasonAudioVideo:
		return "KBReasonAudioVideo"
	case kb.ReasonArchive:
		return "KBReasonArchive"
	case kb.ReasonExecutable:
		return "KBReasonExecutable"
	case kb.ReasonBinary:
		return "KBReasonBinary"
	case kb.ReasonFont:
		return "KBReasonFont"
	case kb.ReasonPDFEncrypted:
		return "KBReasonPDFEncrypted"
	case kb.ReasonPDFNoText:
		return "KBReasonPDFNoText"
	case kb.ReasonNoText:
		return "KBReasonNoText"
	case kb.ReasonFileTooLarge:
		return "KBReasonFileTooLarge"
	case kb.ReasonNotRegular:
		return "KBReasonNotRegular"
	case kb.ReasonUnknownFormat:
		return "KBReasonUnknownFormat"
	default:
		return "KBUnsupportedFile"
	}
}

func kbQueryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return 0, false
	}
	return id, true
}

// kbRequireCollectionAccess checks that the collection is visible from the
// current project.
func kbRequireCollectionAccess(w http.ResponseWriter, r *http.Request, collectionID int64) bool {
	pid := kbProjectID(r)
	ok, err := kb.CollectionAccessible(collectionID, pid)
	if err != nil {
		writeKBError(w, r, err)
		return false
	}
	if !ok {
		writeKBError(w, r, kb.ErrAccessDenied)
		return false
	}
	return true
}

// ServeKBCollections handles /api/kb/collections (GET list, POST create,
// PATCH ?id= update, DELETE ?id= remove).
func ServeKBCollections(w http.ResponseWriter, r *http.Request) { //nolint:gocognit,gocyclo // one resource, four methods
	switch r.Method {
	case http.MethodGet:
		pid := kbProjectID(r)
		cols, err := kb.ListCollections(pid)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		if cols == nil {
			cols = []kb.Collection{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"collections": cols})

	case http.MethodPost:
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Scope       string `json:"scope"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" || len([]rune(body.Name)) > 100 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "KBNameRequired")
			return
		}
		scope := strings.TrimSpace(body.Scope)
		if scope == "" {
			scope = kb.ScopeProject
		}
		var projectID int64
		switch scope {
		case kb.ScopeGlobal:
			projectID = 0
		case kb.ScopeProject:
			projectPath, ok := requireProject(w, r)
			if !ok {
				return
			}
			id, err := store.ProjectIDForPath(projectPath)
			if err != nil {
				writeKBError(w, r, err)
				return
			}
			projectID = id
		default:
			writeKBError(w, r, kb.ErrInvalidScope)
			return
		}
		col, err := kb.CreateCollection(body.Name, strings.TrimSpace(body.Description), scope, projectID)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, col)

	case http.MethodPatch:
		id, ok := kbQueryID(w, r)
		if !ok {
			return
		}
		if !kbRequireCollectionAccess(w, r, id) {
			return
		}
		var body struct {
			Name        *string `json:"name"`
			Description *string `json:"description"`
			AutoInject  *bool   `json:"auto_inject"`
			Pinned      *bool   `json:"pinned"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Name != nil {
			name := strings.TrimSpace(*body.Name)
			if name == "" || len([]rune(name)) > 100 {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, "KBNameRequired")
				return
			}
			body.Name = &name
		}
		if err := kb.UpdateCollection(id, body.Name, body.Description, body.AutoInject, body.Pinned); err != nil {
			writeKBError(w, r, err)
			return
		}
		col, err := kb.GetCollection(id)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, col)

	case http.MethodDelete:
		id, ok := kbQueryID(w, r)
		if !ok {
			return
		}
		if !kbRequireCollectionAccess(w, r, id) {
			return
		}
		if err := kb.DeleteCollection(id); err != nil {
			writeKBError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{fieldDeleted: true})

	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete)
	}
}

// ServeKBDocuments handles /api/kb/documents:
//   - GET ?id=       document detail plus its first chunks
//   - GET ?collection_id=  document list
//   - POST {collection_id, path}  add a file already on the server
//   - DELETE ?id=    remove a document and its stored copy
func ServeKBDocuments(w http.ResponseWriter, r *http.Request) { //nolint:gocognit,gocyclo // one resource, three methods + detail/list split
	switch r.Method {
	case http.MethodGet:
		if raw := r.URL.Query().Get("id"); raw != "" {
			id, ok := kbQueryID(w, r)
			if !ok {
				return
			}
			doc, err := kb.GetDocument(id)
			if err != nil {
				writeKBError(w, r, err)
				return
			}
			if !kbRequireCollectionAccess(w, r, doc.CollectionID) {
				return
			}
			// A search hit may live far past the first page, so ?chunk_id=
			// returns a window around it instead of the document's head. An id
			// that no longer exists (re-indexed since the search) falls back to
			// the first page rather than failing the whole request.
			var chunks []kb.Chunk
			focusChunkID := int64(0)
			if rawChunk := r.URL.Query().Get("chunk_id"); rawChunk != "" {
				chunkID, parseErr := strconv.ParseInt(rawChunk, 10, 64)
				if parseErr != nil || chunkID <= 0 {
					writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
					return
				}
				var target int
				chunks, target, err = kb.DocumentChunkWindow(id, chunkID, 5, 5)
				if err != nil {
					writeKBError(w, r, err)
					return
				}
				if target >= 0 {
					focusChunkID = chunkID
				} else {
					chunks = nil
				}
			}
			if chunks == nil {
				chunks, err = kb.DocumentChunks(id, 20)
				if err != nil {
					writeKBError(w, r, err)
					return
				}
			}
			if chunks == nil {
				chunks = []kb.Chunk{}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"document":       doc,
				"chunks":         chunks,
				"focus_chunk_id": focusChunkID,
			})
			return
		}
		raw := r.URL.Query().Get("collection_id")
		collectionID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || collectionID <= 0 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if !kbRequireCollectionAccess(w, r, collectionID) {
			return
		}
		docs, err := kb.ListDocuments(collectionID)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		if docs == nil {
			docs = []kb.Document{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"documents": docs})

	case http.MethodPost:
		var body struct {
			CollectionID int64  `json:"collection_id"`
			Path         string `json:"path"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.CollectionID <= 0 || strings.TrimSpace(body.Path) == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if !kbRequireCollectionAccess(w, r, body.CollectionID) {
			return
		}
		absPath, ok := resolveAbsPath(w, r, strings.TrimSpace(body.Path))
		if !ok {
			return
		}
		doc, err := kb.AddDocumentFromPath(body.CollectionID, absPath)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, doc)

	case http.MethodPatch:
		id, ok := kbQueryID(w, r)
		if !ok {
			return
		}
		doc, err := kb.GetDocument(id)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		if !kbRequireCollectionAccess(w, r, doc.CollectionID) {
			return
		}
		var body struct {
			Tags *string `json:"tags"`
		}
		if !decodeJSON(w, r, &body) {
			return
		}
		if body.Tags == nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if err := kb.SetDocumentTags(id, strings.TrimSpace(*body.Tags)); err != nil {
			writeKBError(w, r, err)
			return
		}
		updated, err := kb.GetDocument(id)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		id, ok := kbQueryID(w, r)
		if !ok {
			return
		}
		doc, err := kb.GetDocument(id)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		if !kbRequireCollectionAccess(w, r, doc.CollectionID) {
			return
		}
		if err := kb.DeleteDocument(id); err != nil {
			writeKBError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{fieldDeleted: true})

	default:
		requireMethod(w, r, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete)
	}
}

// ServeKBDocumentUpload handles POST /api/kb/documents/upload (multipart).
func ServeKBDocumentUpload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	limit := kb.MaxFileBytes()
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil { //nolint:gosec // body is bounded by MaxBytesReader above
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeOrInvalid")
		return
	}

	rawID := r.URL.Query().Get("collection_id")
	if rawID == "" {
		rawID = r.FormValue("collection_id")
	}
	collectionID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || collectionID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, collectionID) {
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "NoFileProvided")
		return
	}
	defer func() { _ = file.Close() }()

	doc, err := kb.AddDocumentFromReader(collectionID, header.Filename, file)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// ServeKBDocumentMove handles POST /api/kb/documents/move.
//
// Moves one document to another collection. Both ends must be visible from the
// current project.
func ServeKBDocumentMove(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		ID           int64 `json:"id"`
		CollectionID int64 `json:"collection_id"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ID <= 0 || body.CollectionID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	doc, err := kb.GetDocument(body.ID)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	if !kbRequireCollectionAccess(w, r, doc.CollectionID) {
		return
	}
	if !kbRequireCollectionAccess(w, r, body.CollectionID) {
		return
	}
	if err := kb.MoveDocument(body.ID, body.CollectionID); err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"moved": true})
}

// ServeKBDocumentURL handles POST /api/kb/documents/url.
//
// Fetches an http(s) page and indexes it like an uploaded file.
func ServeKBDocumentURL(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		CollectionID int64  `json:"collection_id"`
		URL          string `json:"url"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.CollectionID <= 0 || strings.TrimSpace(body.URL) == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, body.CollectionID) {
		return
	}
	doc, err := kb.AddDocumentFromURL(body.CollectionID, body.URL)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// ServeKBDocumentImport handles POST /api/kb/documents/import.
//
// Adds every supported file under a folder in one request. The frontend could
// list the folder and POST each file, but that turns a large pick into hundreds
// of round trips and lets the UI's own walk drift from the server's rules.
func ServeKBDocumentImport(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		CollectionID int64  `json:"collection_id"`
		Path         string `json:"path"`
		DryRun       bool   `json:"dry_run"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.CollectionID <= 0 || strings.TrimSpace(body.Path) == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, body.CollectionID) {
		return
	}
	absPath, ok := resolveAbsPath(w, r, strings.TrimSpace(body.Path))
	if !ok {
		return
	}
	var result *kb.DirImportResult
	var err error
	if body.DryRun {
		result, err = kb.PreviewDirectory(body.CollectionID, absPath)
	} else {
		result, err = kb.ImportDirectory(body.CollectionID, absPath)
	}
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ServeKBDocumentReindex handles POST /api/kb/documents/reindex?id=.
func ServeKBDocumentReindex(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	id, ok := kbQueryID(w, r)
	if !ok {
		return
	}
	doc, err := kb.GetDocument(id)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	if !kbRequireCollectionAccess(w, r, doc.CollectionID) {
		return
	}
	if err := kb.ReindexDocument(id); err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{fieldQueued: true})
}

// ServeKBDocumentProgress handles GET /api/kb/documents/progress?collection_id=.
//
// Polling endpoint: only the fields that change while indexing, so the panel
// can merge them into its list instead of replacing it.
func ServeKBDocumentProgress(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	raw := r.URL.Query().Get("collection_id")
	collectionID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || collectionID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, collectionID) {
		return
	}
	docs, err := kb.ListDocumentProgress(collectionID)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	if docs == nil {
		docs = []kb.DocumentProgress{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": docs})
}

// ServeKBDocumentUsage handles GET /api/kb/documents/usage?collection_id=.
func ServeKBDocumentUsage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	raw := r.URL.Query().Get("collection_id")
	collectionID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || collectionID <= 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, collectionID) {
		return
	}
	usage, err := kb.ListDocumentUsage(collectionID)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	if usage == nil {
		usage = []kb.DocumentUsage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"usage": usage})
}

// ServeKBDocumentRefresh handles POST /api/kb/documents/refresh?id=.
//
// Re-copies the original file over the stored copy and reindexes only when the
// content changed. `changed:false` means the original is byte-identical.
func ServeKBDocumentRefresh(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	id, ok := kbQueryID(w, r)
	if !ok {
		return
	}
	doc, err := kb.GetDocument(id)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	if !kbRequireCollectionAccess(w, r, doc.CollectionID) {
		return
	}
	changed, err := kb.RefreshDocumentFromSource(id)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": changed})
}

// ServeKBSearch handles POST /api/kb/search.
func ServeKBSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Query         string  `json:"q"`
		CollectionIDs []int64 `json:"collection_ids"`
		Limit         int     `json:"limit"`
		Mode          string  `json:"mode"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	pid := kbProjectID(r)
	res, err := kb.Search(r.Context(), kb.SearchParams{
		Query:         body.Query,
		CollectionIDs: body.CollectionIDs,
		ProjectID:     pid,
		Limit:         body.Limit,
		Mode:          body.Mode,
	})
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ServeKBReindex handles POST /api/kb/reindex. Body:
//
//	{"collection_id": 3}       re-extract one collection
//	{"kind": "vector"}         drop and rebuild the semantic index only
//	{}                         re-extract every collection visible here
func ServeKBReindex(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		CollectionID int64  `json:"collection_id"`
		Kind         string `json:"kind"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &body) {
		return
	}
	if body.Kind == "vector" {
		if err := kb.ResetVectorIndex(); err != nil {
			writeKBError(w, r, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{fieldQueued: true})
		return
	}

	var ids []int64
	if body.CollectionID > 0 {
		if !kbRequireCollectionAccess(w, r, body.CollectionID) {
			return
		}
		ids = []int64{body.CollectionID}
	} else {
		pid := kbProjectID(r)
		var err error
		ids, err = kb.ListCollectionIDs(pid)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
	}
	if err := kb.ReindexCollections(ids); err != nil {
		writeKBError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{fieldQueued: true})
}

// ServeKBStatus handles GET /api/kb/status.
//
// `scope=project` restricts the counters to what the current project can see
// (its own collections plus the global ones), which is exactly what the
// knowledge-base panel lists. Every other caller gets installation-wide totals:
// the settings page is global, and the completion watcher runs outside any
// project context.
func ServeKBStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	available := kb.Available()
	vectorEnabled := kb.VectorEnabled()
	var stats kb.IndexStats
	if available {
		var err error
		if r.URL.Query().Get("scope") == "project" {
			stats, err = kb.StatsForProject(kbProjectID(r))
		} else {
			stats, err = kb.Stats()
		}
		if err != nil {
			writeKBError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":      available,
		"vector_enabled": vectorEnabled,
		"stats":          stats,
	})
}
