package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
)

// uploadLedgerRelPath is the per-project record of files that were uploaded
// OUTSIDE .clawbench/uploads/ (i.e. through the file manager, which posts a
// `dir` form field).
//
// Why a ledger at all: "最近上传" used to mean "whatever sits in
// .clawbench/uploads/", because that is the only directory the chat upload
// endpoint writes to. Files uploaded into the project tree through the file
// manager are ordinary project files — they can live anywhere, so they cannot
// be found by listing a single directory. Without a record of them the tab
// silently omits them, which is what issue #573 reported: the user uploads a
// screenshot to the project root, sees it in the file tree, and then cannot
// find it in "最近上传".
//
// A file is used instead of a database table on purpose: the list is a
// per-project convenience index, not shared state, and keeping it next to the
// uploads directory means it travels with the project (and is deleted with it).
const uploadLedgerRelPath = ".clawbench/upload-log.json"

// uploadLedgerMaxEntries bounds the ledger. It is deliberately much larger than
// the 20 rows the tab renders: entries whose file has since been deleted or
// moved are dropped on read, so a small cap would let dead entries crowd out
// live ones.
const uploadLedgerMaxEntries = 200

// uploadLedgerMu serializes the read-modify-write of the ledger. Uploads run
// concurrently (a multi-file picker posts one request per file) and a lost
// update would silently drop an entry the user just created.
var uploadLedgerMu sync.Mutex

// uploadLedgerEntry is one recorded upload. Order is not stored: the tab sorts
// by the file's own modification time, like the uploads-directory rows do.
type uploadLedgerEntry struct {
	Path string `json:"path"`
}

func uploadLedgerFile(projectPath string) string {
	return filepath.Join(projectPath, filepath.FromSlash(uploadLedgerRelPath))
}

// readUploadLedger returns the recorded entries. A missing or unreadable ledger
// is not an error: it only means nothing has been recorded yet (or the user
// deleted the file), and the caller falls back to the uploads directory alone.
func readUploadLedger(projectPath string) []uploadLedgerEntry {
	data, err := os.ReadFile(uploadLedgerFile(projectPath))
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("upload ledger: read failed",
				slog.String("project", projectPath), slog.String("err", err.Error()))
		}
		return nil
	}
	var entries []uploadLedgerEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// A corrupt ledger must not break the whole tab — it is a cache of
		// convenience, and the uploads directory still lists normally.
		slog.Warn("upload ledger: corrupt, ignoring",
			slog.String("project", projectPath), slog.String("err", err.Error()))
		return nil
	}
	return entries
}

// writeUploadLedger replaces the ledger atomically (temp file + rename) so a
// crash mid-write cannot leave a half-written JSON that then reads as corrupt.
func writeUploadLedger(projectPath string, entries []uploadLedgerEntry) error {
	path := uploadLedgerFile(projectPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-log-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// sanitizeLedgerPath normalizes a project-relative path for the ledger and
// reports whether it may be stored at all.
//
// Both the WRITE side and the READ side must go through this. The ledger is a
// plain JSON file inside the project, so anything with write access to the
// project (the AI agent, another tool, the user in an editor) can put an
// arbitrary string in it. Validating only on write would let such an entry
// resolve outside the project when the list is rendered.
func sanitizeLedgerPath(relPath string) (string, bool) {
	if relPath == "" || filepath.IsAbs(relPath) {
		return "", false
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
	// Clean collapses "a/../../b" to "../b", so this single check also covers
	// every intermediate traversal attempt.
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	// Everything under .clawbench/ is app-managed scratch space with its own
	// tab: uploads/ is listed by scanning the directory, share-in/ backs the
	// "最近分享" tab. Listing them here would show the same file twice.
	if isClawbenchInternalPath(rel) {
		return "", false
	}
	return rel, true
}

// recordUploadedFile adds relPath to the ledger. Callers pass the project
// relative path returned to the client, so the ledger always holds exactly the
// path the attachment list will hand back.
//
// Best-effort: a failure to record must never fail an upload that already
// succeeded on disk — the user's file is there and the file tree shows it.
func recordUploadedFile(projectPath, relPath string) {
	rel, ok := sanitizeLedgerPath(relPath)
	if !ok {
		return
	}

	uploadLedgerMu.Lock()
	defer uploadLedgerMu.Unlock()

	prev := readUploadLedger(projectPath)
	next := make([]uploadLedgerEntry, 0, len(prev)+1)
	next = append(next, uploadLedgerEntry{Path: rel})
	for _, e := range prev {
		if e.Path == rel {
			continue // re-upload: move to the front instead of duplicating
		}
		next = append(next, e)
	}
	if len(next) > uploadLedgerMaxEntries {
		next = next[:uploadLedgerMaxEntries]
	}
	if err := writeUploadLedger(projectPath, next); err != nil {
		slog.Warn("upload ledger: record failed",
			slog.String("project", projectPath), slog.String("path", rel), slog.String("err", err.Error()))
	}
}

// forgetUploadedFile removes relPath from the ledger.
//
// Returns found=false when the path was never recorded, and a non-nil error
// only when the ledger could not be written — the two are distinct because one
// means "nothing to do" (403) and the other is a server fault (500). Reporting a
// write failure as "not recorded" would tell the user their row is unknown when
// the real problem is a read-only data directory.
//
// It deliberately does NOT delete the file: a ledger entry is an ordinary
// project file, and removing a row from "最近上传" must not delete the user's
// source tree from the attachment drawer.
func forgetUploadedFile(projectPath, relPath string) (found bool, err error) {
	rel, ok := sanitizeLedgerPath(relPath)
	if !ok {
		return false, nil
	}

	uploadLedgerMu.Lock()
	defer uploadLedgerMu.Unlock()

	prev := readUploadLedger(projectPath)
	next := make([]uploadLedgerEntry, 0, len(prev))
	for _, e := range prev {
		if e.Path == rel {
			found = true
			continue
		}
		next = append(next, e)
	}
	if !found {
		return false, nil
	}
	if err := writeUploadLedger(projectPath, next); err != nil {
		slog.Warn("upload ledger: forget failed",
			slog.String("project", projectPath), slog.String("path", rel), slog.String("err", err.Error()))
		return false, err
	}
	return true, nil
}

// clearUploadLedger forgets every recorded entry, leaving the files themselves
// untouched (see forgetUploadedFile for why).
//
// The returned count is the number of rows the user could actually see — entries
// whose file still exists — not the number of records. The ledger keeps dead
// entries on purpose (a file on an unmounted volume should come back), so
// counting records would claim "cleared 180 files" for a list showing three.
//
// `remaining` is what is still listed when the write fails, so the caller can
// report how many rows survived instead of a bare "1 failed" that says nothing
// about the size of the leftover list.
func clearUploadLedger(projectPath string) (cleared, remaining int, err error) {
	uploadLedgerMu.Lock()
	defer uploadLedgerMu.Unlock()

	// "Nothing to clear" is decided by the file's EXISTENCE, not by a
	// successful read: an unreadable ledger still has rows the user expects to
	// disappear, and skipping the write would report a clean clear while the
	// list is unchanged. Writing an empty ledger is what actually clears it.
	if _, statErr := os.Stat(uploadLedgerFile(projectPath)); statErr != nil {
		if os.IsNotExist(statErr) {
			return 0, 0, nil
		}
		return 0, 0, statErr
	}

	for _, e := range readUploadLedger(projectPath) {
		rel, ok := sanitizeLedgerPath(e.Path)
		if !ok {
			continue
		}
		if info, infoErr := os.Stat(filepath.Join(projectPath, filepath.FromSlash(rel))); infoErr == nil && !info.IsDir() {
			cleared++
		}
	}
	if writeErr := writeUploadLedger(projectPath, nil); writeErr != nil {
		slog.Warn("upload ledger: clear failed",
			slog.String("project", projectPath), slog.String("err", writeErr.Error()))
		return 0, cleared, writeErr
	}
	return cleared, 0, nil
}

// listLedgerUploads renders the ledger entries that still exist as files,
// in the same shape as listRecentFiles so the two lists can be merged.
func listLedgerUploads(projectPath string) []recentFile {
	uploadLedgerMu.Lock()
	entries := readUploadLedger(projectPath)
	uploadLedgerMu.Unlock()

	out := make([]recentFile, 0, len(entries))
	for _, e := range entries {
		// Re-validate on read: see sanitizeLedgerPath for why the write-side
		// check is not enough on its own.
		rel, ok := sanitizeLedgerPath(e.Path)
		if !ok {
			continue
		}
		abs := filepath.Join(projectPath, filepath.FromSlash(rel))
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			// Deleted or moved outside the app: skip it. The entry stays in the
			// ledger so a file that is merely offline (unmounted volume) comes
			// back rather than being forgotten.
			continue
		}
		out = append(out, recentFile{
			Name:    filepath.Base(abs),
			Path:    rel,
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
			Origin:  uploadOriginProject,
		})
	}
	return out
}

// listRecentUploads merges the uploads directory with the ledger and returns
// the newest `limit` entries. Both sources are sorted by the file's own
// modification time, which is what the rows display.
func listRecentUploads(projectPath string, limit int) []recentFile {
	files := listRecentFiles(projectPath, filepath.Join(".clawbench", "uploads"), limit)
	// Origin is what tells the client whether a delete on this row also deletes
	// the file. Tagged here rather than in listRecentFiles because the share-in
	// list uses the same helper and has a single origin of its own.
	for i := range files {
		files[i].Origin = uploadOriginUploads
	}
	files = append(files, listLedgerUploads(projectPath)...)
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].ModTime > files[j].ModTime
	})
	if len(files) > limit {
		files = files[:limit]
	}
	return files
}

// isClawbenchInternalPath reports whether a project-relative path is
// app-managed scratch space under .clawbench/.
func isClawbenchInternalPath(rel string) bool {
	return rel == ".clawbench" || strings.HasPrefix(rel, ".clawbench/")
}

// ledgerRelativePath resolves a DELETE request's path (project-relative or
// absolute) to the project-relative form the ledger stores. ok=false when the
// path cannot belong to this project.
func ledgerRelativePath(projectPath, reqPath string) (string, bool) {
	if reqPath == "" {
		return "", false
	}
	abs := reqPath
	if !filepath.IsAbs(abs) {
		baseAbs, err := filepath.Abs(projectPath)
		if err != nil {
			return "", false
		}
		abs = filepath.Join(baseAbs, filepath.FromSlash(reqPath))
	}
	return projectRelPath(projectPath, filepath.Clean(abs))
}

// projectRelPath converts an absolute path to the project-relative, slash
// separated form the ledger stores. ok=false when the path is outside the
// project.
func projectRelPath(projectPath, absPath string) (string, bool) {
	rel, err := filepath.Rel(projectPath, absPath)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// deleteLedgerEntry handles a DELETE of a "最近上传" row that is an ordinary
// project file recorded by the ledger. Returns true when the response has been
// written (success or refusal) and the caller must stop.
//
// The semantics are deliberately not "delete the file": the user asked to clear
// an attachment-history row, and the file itself is still theirs, still visible
// in the file tree, possibly tracked by git. So this only un-lists it.
//
// Paths inside .clawbench/ return false and fall through to the original
// directory-scoped delete, which is the code that owns those files. A path
// outside .clawbench/ that is NOT in the ledger is REFUSED rather than falling
// through — falling through would reach the file-deletion branch and delete a
// project file the user only meant to hide.
func deleteLedgerEntry(w http.ResponseWriter, r *http.Request, projectPath, reqPath string) bool {
	rel, ok := ledgerRelativePath(projectPath, reqPath)
	if !ok {
		// Outside the project, or inside .clawbench/: the directory-scoped
		// delete below owns those and must decide.
		return false
	}
	if isClawbenchInternalPath(rel) {
		return false
	}
	found, err := forgetUploadedFile(projectPath, rel)
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return true
	}
	if !found {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return true
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return true
}
