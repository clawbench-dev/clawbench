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
		if err := kb.UpdateCollection(id, body.Name, body.Description, body.AutoInject); err != nil {
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
			chunks, err := kb.DocumentChunks(id, 20)
			if err != nil {
				writeKBError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"document": doc, "chunks": chunks})
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
		requireMethod(w, r, http.MethodGet, http.MethodPost, http.MethodDelete)
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
	result, err := kb.ImportDirectory(body.CollectionID, absPath)
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
func ServeKBStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	available := kb.Available()
	vectorEnabled := kb.VectorEnabled()
	var stats kb.IndexStats
	if available {
		st, err := kb.Stats()
		if err != nil {
			writeKBError(w, r, err)
			return
		}
		stats = st
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available":      available,
		"vector_enabled": vectorEnabled,
		"stats":          stats,
	})
}
