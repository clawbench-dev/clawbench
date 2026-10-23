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

	"clawbench/internal/middleware"
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
// the 20 rows the tab renders: entries are pruned lazily (a file that was
// renamed or deleted is dropped the next time the ledger is written), so a small
// cap would let not-yet-pruned entries crowd out live ones.
const uploadLedgerMaxEntries = 200

// uploadLedgerRetention is how long an entry is kept after its upload.
//
// It is a RETENTION, not a capacity, and the two answer different questions:
// the cap bounds the file, the retention bounds how far back the tab can reach.
// A project that uploads a hundred files a week hits the cap long before 30 days
// and a quiet one hits the retention long before the cap; both are needed.
//
// The clock is the UPLOAD time, not the file's mtime: editing a file must not
// keep resurrecting it in a list of "things I uploaded recently". Entries
// written before this field existed have no timestamp and are kept (they are
// pruned by the existence check instead) — dropping them on upgrade would empty
// the tab for every existing user.
const uploadLedgerRetention = 30 * 24 * time.Hour

// uploadLedgerMu serializes the read-modify-write of the ledger. Uploads run
// concurrently (a multi-file picker posts one request per file) and a lost
// update would silently drop an entry the user just created.
var uploadLedgerMu sync.Mutex

// uploadLedgerEntry is one recorded upload.
//
// UploadedAt is what the retention is measured against, so it is load-bearing
// (not debugging metadata). It is the time of the UPLOAD, so moving or editing
// the file does not extend its life. Order is not stored: the tab sorts by the
// file's own modification time, like the uploads-directory rows do.
type uploadLedgerEntry struct {
	Path       string `json:"path"`
	UploadedAt string `json:"uploadedAt,omitempty"`
}

// uploadTime parses the entry's timestamp. ok=false means the entry predates
// the field (or is corrupt) and must not be aged out.
func (e uploadLedgerEntry) uploadTime() (time.Time, bool) {
	if e.UploadedAt == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, e.UploadedAt)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
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
	if relPath == "" {
		return "", false
	}
	// Absolute paths are rejected on their SYNTAX, not via filepath.IsAbs: on
	// Windows that returns false for a rooted-but-drive-less path like
	// `/etc/passwd` (it means "root of the current drive" there), so an entry
	// written on Linux would pass and then resolve outside the project. A
	// leading separator — in either slash direction — is never a project-relative
	// path on any platform we run on.
	if filepath.IsAbs(relPath) || strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, `\`) {
		return "", false
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relPath)))
	// Clean collapses "a/../../b" to "../b", so this single check also covers
	// every intermediate traversal attempt. A Windows drive-qualified path
	// (`C:a.png`) is also not project-relative, and VolumeName only recognizes
	// it when running ON Windows — the ledger travels with the project, so the
	// drive-letter shape is rejected syntactically as well.
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") ||
		filepath.VolumeName(rel) != "" || hasDriveLetter(rel) {
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

	now := time.Now().UTC()
	prev := readUploadLedger(projectPath)
	next := make([]uploadLedgerEntry, 0, len(prev)+1)
	next = append(next, uploadLedgerEntry{Path: rel, UploadedAt: now.Format(time.RFC3339)})
	for _, e := range prev {
		if e.Path == rel {
			continue // re-upload: move to the front instead of duplicating
		}
		next = append(next, e)
	}
	// Pruning on write is free: the lock is already held, the file is already
	// being rewritten, and it is the only place a stale entry can be noticed
	// without a background worker.
	next = pruneUploadLedger(projectPath, next, now, rel)
	if err := writeUploadLedger(projectPath, next); err != nil {
		slog.Warn("upload ledger: record failed",
			slog.String("project", projectPath), slog.String("path", rel), slog.String("err", err.Error()))
	}
}

// pruneUploadLedger drops entries that can no longer be shown, then applies the
// capacity cap. Two independent reasons to drop an entry:
//
//   - The file is gone (renamed, moved out of the project, deleted by the user
//     or by another tool). The list already skips such rows on read, so keeping
//     the record only wastes capacity — and after enough renames the whole
//     ledger would consist of entries that render as nothing.
//   - The upload is older than uploadLedgerRetention.
//
// Entries without a timestamp predate the field and are kept: aging them out
// would empty the tab for every existing user on upgrade, and the existence
// check already removes the ones whose files are gone.
//
// keepPath is the entry that was just recorded. It is exempt from BOTH rules:
// the caller has already written the file, but a stat that races a slow or
// eventually-consistent filesystem must not throw away the row for the upload
// the user is watching.
func pruneUploadLedger(projectPath string, entries []uploadLedgerEntry, now time.Time, keepPath string) []uploadLedgerEntry {
	cutoff := now.Add(-uploadLedgerRetention)
	kept := entries[:0]
	for _, e := range entries {
		if e.Path == keepPath {
			kept = append(kept, e)
			continue
		}
		if uploadedAt, ok := e.uploadTime(); ok && uploadedAt.Before(cutoff) {
			continue
		}
		rel, ok := sanitizeLedgerPath(e.Path)
		if !ok {
			continue
		}
		info, err := os.Stat(filepath.Join(projectPath, filepath.FromSlash(rel)))
		if err != nil {
			// Only "this file is not there" justifies dropping the record. A
			// permission error, an I/O error, or an unmounted volume (EACCES /
			// EIO / ESTALE) also surfaces as an error, and treating those as
			// "gone" would wipe the whole list the first time the user uploads
			// while the project's volume is offline — then leave the tab empty
			// forever after it comes back.
			if !os.IsNotExist(err) {
				kept = append(kept, e)
			}
			continue
		}
		if info.IsDir() {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) > uploadLedgerMaxEntries {
		kept = kept[:uploadLedgerMaxEntries]
	}
	return kept
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
// whose file still exists — not the number of records. An entry whose file is
// gone is invisible in the tab, so counting records would claim "cleared 180
// files" for a list showing three.
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
			// Renamed, moved out of the project, or deleted: the row would point
			// at nothing, so it is not rendered. The record itself is dropped by
			// pruneUploadLedger on the next write — reading never writes.
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

// forgetLedgerPath drops every recorded entry at or beneath absPath. Used after
// a delete, where the file no longer exists.
//
// A directory delete removes everything beneath it, so entries under that prefix
// go too. Best-effort: the list would skip them on read anyway (their files are
// gone), so a failure only means the record lingers until the next write prunes
// it.
func forgetLedgerPath(projectPath, absPath string) {
	rel, ok := projectRelPath(projectPath, absPath)
	if !ok {
		return
	}
	rel, ok = sanitizeLedgerPath(rel)
	if !ok {
		return
	}

	uploadLedgerMu.Lock()
	defer uploadLedgerMu.Unlock()

	prev := readUploadLedger(projectPath)
	next := make([]uploadLedgerEntry, 0, len(prev))
	changed := false
	prefix := rel + "/"
	for _, e := range prev {
		if e.Path == rel || strings.HasPrefix(e.Path, prefix) {
			changed = true
			continue
		}
		next = append(next, e)
	}
	if !changed {
		return
	}
	if err := writeUploadLedger(projectPath, next); err != nil {
		slog.Warn("upload ledger: forget after delete failed",
			slog.String("project", projectPath), slog.String("path", rel), slog.String("err", err.Error()))
	}
}

// projectPathForRequest returns the project root for the current request, if one
// is selected.
//
// The ledger hooks live in the generic file operations (rename/move/delete),
// which are NOT project-scoped: the file manager browses the whole machine and
// those endpoints accept absolute paths anywhere under a root. So "no project
// cookie" is a normal state there, not an error — it simply means there is no
// ledger to update, because the ledger is per project.
func projectPathForRequest(r *http.Request) (string, bool) {
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		return "", false
	}
	return projectPath, true
}

// moveLedgerEntry rewrites a recorded upload when its file is renamed or moved,
// so the row keeps pointing at the file instead of silently vanishing.
//
// Why this is needed at all: the ledger stores a PATH, and rename/move change
// it. Without this the tab would lose a row the moment the user tidies up the
// project — the same complaint as #573 (uploaded something, cannot find it in
// the list), just triggered by a different action. The file is still the file
// the user uploaded, so the entry (and its upload time, which the retention is
// measured against) travels with it.
//
// A directory rename/move moves everything beneath it, so entries under the old
// prefix are rewritten too. `isDir` is decided by the caller from the stat it
// already performed; a file only ever matches its exact path.
//
// Best-effort like recordUploadedFile: failing to rewrite must not fail a rename
// that already succeeded on disk. A missed rewrite is self-healing — the entry
// then points at a missing file and is pruned on the next write.
func moveLedgerEntry(projectPath, oldAbs, newAbs string, isDir bool) {
	oldRel, ok := projectRelPath(projectPath, oldAbs)
	if !ok {
		return
	}
	newRel, ok := projectRelPath(projectPath, newAbs)
	if !ok {
		return
	}
	oldRel, ok = sanitizeLedgerPath(oldRel)
	if !ok {
		return
	}
	newRel, ok = sanitizeLedgerPath(newRel)
	if !ok {
		return
	}
	if oldRel == newRel {
		return
	}

	uploadLedgerMu.Lock()
	defer uploadLedgerMu.Unlock()

	prev := readUploadLedger(projectPath)
	next := make([]uploadLedgerEntry, 0, len(prev))
	changed := false
	for _, e := range prev {
		moved, didMove := remapLedgerPath(e.Path, oldRel, newRel, isDir)
		if !didMove {
			next = append(next, e)
			continue
		}
		changed = true
		e.Path = moved
		next = append(next, e)
	}
	if !changed {
		return
	}
	// Rename has no "destination exists" guard (unlike move, which answers 409),
	// so on Unix a rename onto an existing name silently replaces the target —
	// and the ledger would then hold the same path twice: two identical rows,
	// plus a duplicate Vue key. Collapse them, keeping the FIRST occurrence,
	// which is the moved entry carrying the upload time of the content that now
	// lives at that path.
	next = dedupeLedgerPaths(next)
	if err := writeUploadLedger(projectPath, next); err != nil {
		slog.Warn("upload ledger: move failed",
			slog.String("project", projectPath),
			slog.String("from", oldRel), slog.String("to", newRel),
			slog.String("err", err.Error()))
	}
}

// dedupeLedgerPaths keeps the first entry for each path, preserving order.
func dedupeLedgerPaths(entries []uploadLedgerEntry) []uploadLedgerEntry {
	seen := make(map[string]struct{}, len(entries))
	out := entries[:0]
	for _, e := range entries {
		if _, dup := seen[e.Path]; dup {
			continue
		}
		seen[e.Path] = struct{}{}
		out = append(out, e)
	}
	return out
}

// remapLedgerPath rewrites one entry's path for a rename/move of oldRel→newRel.
// didMove=false means the entry is unrelated and must be kept as is.
func remapLedgerPath(entryPath, oldRel, newRel string, isDir bool) (string, bool) {
	if !isDir {
		if entryPath != oldRel {
			return entryPath, false
		}
		return newRel, true
	}
	// A directory: the entry is either the directory itself (not recorded — only
	// files are) or a file beneath it.
	prefix := oldRel + "/"
	if !strings.HasPrefix(entryPath, prefix) {
		return entryPath, false
	}
	return newRel + "/" + strings.TrimPrefix(entryPath, prefix), true
}

// hasDriveLetter reports whether p starts with a Windows drive designator
// (`C:`, `z:`), which is a volume-qualified path rather than a project-relative
// one. Checked syntactically because filepath.VolumeName only recognizes the
// form on Windows, and the ledger is a file inside the project that can be
// carried between machines.
func hasDriveLetter(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
