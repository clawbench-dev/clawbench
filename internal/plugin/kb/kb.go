package kb

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"clawbench/internal/httpx"
	"clawbench/internal/kb"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/store"
)

const (
	// 与 handler/upload.go 的同名常量一致（API JSON key）。
	fieldDeleted = "deleted"
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
	// Typed file rejections and typed sentinels go through their own helpers;
	// the switch below only keeps the ones needing bespoke bodies (499, the
	// private-address detail blob, the reason-coded extraction errors).
	if errors.Is(err, kb.ErrPrivateAddress) {
		var fileErr *kb.FileError
		data := map[string]any{}
		if errors.As(err, &fileErr) {
			data[fieldDetail] = fileErr.Detail
		}
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "KBPrivateAddress", data)
		return
	}
	if errors.Is(err, kb.ErrFileTooLarge) || errors.Is(err, kb.ErrUnsupportedFile) || errors.Is(err, kb.ErrNoText) {
		writeKBFileError(w, r, err)
		return
	}
	if status, key := kbErrorStatus(err); status != 0 {
		httpx.WriteLocalizedErrorf(w, r, status, key)
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// The client went away while a long operation was running; there is no
		// one left to read a body. 499 is the de-facto "client closed request".
		w.WriteHeader(499)
		return
	}
	httpx.WriteLocalizedError(w, r, model.Internal(err))
}

// kbFileErrorStatus is the HTTP status for a typed extraction error: too large
// gets 413, everything else is a 400.
func kbFileErrorStatus(err error) int {
	if errors.Is(err, kb.ErrFileTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

// writeKBFileError renders a typed kb.FileError, passing its reason to the
// locale table so the user sees WHY a file was rejected rather than "failed".
func writeKBFileError(w http.ResponseWriter, r *http.Request, err error) {
	var fileErr *kb.FileError
	if !errors.As(err, &fileErr) {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "KBUnsupportedFile")
		return
	}
	data := map[string]any{fieldDetail: fileErr.Detail}
	if fileErr.Reason == kb.ReasonFileTooLarge {
		data["MaxMB"] = fileErr.Detail
		data["LimitMB"] = strconv.FormatInt(kb.MaxFileBytes()>>20, 10)
	}
	httpx.WriteLocalizedErrorf(w, r, kbFileErrorStatus(err), kbReasonKey(fileErr.Reason), data)
}

// kbErrorStatus maps the remaining kb sentinels to (status, message key).
// Returns 0 when err matches none of them.
func kbErrorStatus(err error) (status int, msgKey string) {
	switch {
	case errors.Is(err, kb.ErrCollectionNotFound), errors.Is(err, kb.ErrDocumentNotFound):
		return http.StatusNotFound, "KBNotFound"
	case errors.Is(err, kb.ErrDuplicateDocument):
		return http.StatusConflict, "KBDuplicate"
	case errors.Is(err, kb.ErrAccessDenied):
		return http.StatusForbidden, "AccessDenied"
	case errors.Is(err, kb.ErrInvalidScope):
		return http.StatusBadRequest, "InvalidRequestBody"
	case errors.Is(err, kb.ErrNotDirectory):
		return http.StatusBadRequest, "KBNotDirectory"
	case errors.Is(err, kb.ErrSourceMissing):
		return http.StatusConflict, "KBSourceMissing"
	case errors.Is(err, kb.ErrInvalidURL):
		return http.StatusBadRequest, "KBInvalidURL"
	case errors.Is(err, kb.ErrFetchFailed):
		return http.StatusBadGateway, "KBFetchFailed"
	case errors.Is(err, kb.ErrUnavailable):
		return http.StatusServiceUnavailable, "KBUnavailable"
	}
	return 0, ""
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
	case kb.ReasonWPSDoc:
		return "KBReasonWPSDoc"
	case kb.ReasonWPSSheet:
		return "KBReasonWPSSheet"
	case kb.ReasonWPSSlides:
		return "KBReasonWPSSlides"
	case kb.ReasonPages:
		return "KBReasonPages"
	case kb.ReasonNumbers:
		return "KBReasonNumbers"
	case kb.ReasonKeynote:
		return "KBReasonKeynote"
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
	case kb.ReasonPrivateAddress:
		return "KBReasonPrivateAddress"
	default:
		return "KBUnsupportedFile"
	}
}

func kbQueryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
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
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"collections": cols})

	case http.MethodPost:
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Scope       string `json:"scope"`
		}
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" || len([]rune(body.Name)) > 100 {
			httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "KBNameRequired")
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
			projectPath, ok := httpx.RequireProject(w, r)
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
		httpx.WriteJSON(w, http.StatusCreated, col)

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
		}
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Name != nil {
			name := strings.TrimSpace(*body.Name)
			if name == "" || len([]rune(name)) > 100 {
				httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "KBNameRequired")
				return
			}
			body.Name = &name
		}
		if err := kb.UpdateCollection(id, body.Name, body.Description, body.AutoInject); err != nil {
			writeKBError(w, r, err)
			return
		}
		col, err := kb.GetCollection(id)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, col)

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
		httpx.WriteJSON(w, http.StatusOK, map[string]any{fieldDeleted: true})

	default:
		httpx.RequireMethod(w, r, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete)
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
					httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
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
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"document":       doc,
				"chunks":         chunks,
				"focus_chunk_id": focusChunkID,
			})
			return
		}
		raw := r.URL.Query().Get("collection_id")
		collectionID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || collectionID <= 0 {
			httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
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
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"documents": docs})

	case http.MethodPost:
		var body struct {
			CollectionID int64  `json:"collection_id"`
			Path         string `json:"path"`
		}
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.CollectionID <= 0 || strings.TrimSpace(body.Path) == "" {
			httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if !kbRequireCollectionAccess(w, r, body.CollectionID) {
			return
		}
		absPath, ok := httpx.ResolveAbsPath(w, r, strings.TrimSpace(body.Path))
		if !ok {
			return
		}
		doc, err := kb.AddDocumentFromPath(body.CollectionID, absPath)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, doc)

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
		if !httpx.DecodeJSON(w, r, &body) {
			return
		}
		if body.Tags == nil {
			httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if tagsErr := kb.SetDocumentTags(id, strings.TrimSpace(*body.Tags)); tagsErr != nil {
			writeKBError(w, r, tagsErr)
			return
		}
		updated, err := kb.GetDocument(id)
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, updated)

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
		httpx.WriteJSON(w, http.StatusOK, map[string]any{fieldDeleted: true})

	default:
		httpx.RequireMethod(w, r, http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete)
	}
}

// ServeKBDocumentUpload handles POST /api/kb/documents/upload (multipart).
func ServeKBDocumentUpload(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	limit := kb.MaxFileBytes()
	r.Body = http.MaxBytesReader(w, r.Body, limit+(1<<20))
	if err := r.ParseMultipartForm(32 << 20); err != nil { //nolint:gosec // body is bounded by MaxBytesReader above
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "FileTooLargeOrInvalid")
		return
	}

	rawID := r.URL.Query().Get("collection_id")
	if rawID == "" {
		rawID = r.FormValue("collection_id")
	}
	collectionID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || collectionID <= 0 {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, collectionID) {
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "NoFileProvided")
		return
	}
	defer func() { _ = file.Close() }()

	doc, err := kb.AddDocumentFromReader(collectionID, header.Filename, file)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, doc)
}

// ServeKBDocumentMove handles POST /api/kb/documents/move.
//
// Moves one document to another collection. Both ends must be visible from the
// current project.
func ServeKBDocumentMove(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		ID           int64 `json:"id"`
		CollectionID int64 `json:"collection_id"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if body.ID <= 0 || body.CollectionID <= 0 {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"moved": true})
}

// ServeKBDocumentURL handles POST /api/kb/documents/url.
//
// Fetches an http(s) page and indexes it like an uploaded file.
func ServeKBDocumentURL(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		CollectionID int64  `json:"collection_id"`
		URL          string `json:"url"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if body.CollectionID <= 0 || strings.TrimSpace(body.URL) == "" {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, body.CollectionID) {
		return
	}
	doc, err := kb.AddDocumentFromURL(r.Context(), body.CollectionID, body.URL)
	if err != nil {
		writeKBError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, doc)
}

// ServeKBDocumentImport handles POST /api/kb/documents/import.
//
// Adds every supported file under a folder in one request. The frontend could
// list the folder and POST each file, but that turns a large pick into hundreds
// of round trips and lets the UI's own walk drift from the server's rules.
func ServeKBDocumentImport(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		CollectionID int64  `json:"collection_id"`
		Path         string `json:"path"`
		DryRun       bool   `json:"dry_run"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if body.CollectionID <= 0 || strings.TrimSpace(body.Path) == "" {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}
	if !kbRequireCollectionAccess(w, r, body.CollectionID) {
		return
	}
	absPath, ok := httpx.ResolveAbsPath(w, r, strings.TrimSpace(body.Path))
	if !ok {
		return
	}
	var result *kb.DirImportResult
	var err error
	if body.DryRun {
		result, err = kb.PreviewDirectory(r.Context(), body.CollectionID, absPath)
	} else {
		result, err = kb.ImportDirectory(r.Context(), body.CollectionID, absPath)
	}
	if err != nil {
		// A client that hung up (or hit its own timeout) leaves nothing to
		// answer; the import has already stopped copying.
		if r.Context().Err() != nil {
			return
		}
		writeKBError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

// ServeKBDocumentReindex handles POST /api/kb/documents/reindex?id=.
func ServeKBDocumentReindex(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
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
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{fieldQueued: true})
}

// ServeKBDocumentProgress handles GET /api/kb/documents/progress?collection_id=.
//
// Polling endpoint: only the fields that change while indexing, so the panel
// can merge them into its list instead of replacing it.
func ServeKBDocumentProgress(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodGet) {
		return
	}
	raw := r.URL.Query().Get("collection_id")
	collectionID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || collectionID <= 0 {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"documents": docs})
}

// ServeKBDocumentUsage handles GET /api/kb/documents/usage?collection_id=.
func ServeKBDocumentUsage(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodGet) {
		return
	}
	raw := r.URL.Query().Get("collection_id")
	collectionID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || collectionID <= 0 {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"usage": usage})
}

// ServeKBDocumentRefresh handles POST /api/kb/documents/refresh?id=.
//
// Re-copies the original file over the stored copy and reindexes only when the
// content changed. `changed:false` means the original is byte-identical.
func ServeKBDocumentRefresh(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"changed": changed})
}

// ServeKBSearch handles POST /api/kb/search.
func ServeKBSearch(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Query         string  `json:"q"`
		CollectionIDs []int64 `json:"collection_ids"`
		Limit         int     `json:"limit"`
		Mode          string  `json:"mode"`
	}
	if !httpx.DecodeJSON(w, r, &body) {
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
	httpx.WriteJSON(w, http.StatusOK, res)
}

// ServeKBReindex handles POST /api/kb/reindex. Body:
//
//	{"collection_id": 3}       re-extract one collection
//	{"kind": "vector"}         drop and rebuild the semantic index only
//	{}                         re-extract every collection visible here
func ServeKBReindex(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		CollectionID int64  `json:"collection_id"`
		Kind         string `json:"kind"`
	}
	if r.ContentLength > 0 && !httpx.DecodeJSON(w, r, &body) {
		return
	}
	if body.Kind == "vector" {
		if err := kb.ResetVectorIndex(); err != nil {
			writeKBError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{fieldQueued: true})
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
		// Nothing visible here means nothing to rebuild. Passing the empty
		// slice through is still safe (the store reads it as "no collections"),
		// but returning early keeps the response honest instead of accepting a
		// "rebuild started" that will not touch anything.
		if len(ids) == 0 {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				fieldQueued: false,
				fieldDetail: "no_collections",
			})
			return
		}
	}
	if err := kb.ReindexCollections(ids); err != nil {
		writeKBError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{fieldQueued: true})
}

// ServeKBStatus handles GET /api/kb/status.
//
// `scope=project` restricts the counters to what the current project can see
// (its own collections plus the global ones), which is exactly what the
// knowledge-base panel lists. Every other caller gets installation-wide totals:
// the settings page is global, and the completion watcher runs outside any
// project context.
func ServeKBStatus(w http.ResponseWriter, r *http.Request) {
	if !httpx.RequireMethod(w, r, http.MethodGet) {
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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"available":      available,
		"vector_enabled": vectorEnabled,
		"stats":          stats,
	})
}
