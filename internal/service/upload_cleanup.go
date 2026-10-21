package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"
)

// uploadsRelDir is the per-project directory the chat upload endpoint writes
// into (see internal/handler/upload.go). Everything under it is a temporary
// attachment: the message references it by path, and the AI reads it on demand.
const uploadsRelDir = ".clawbench/uploads"

// UploadCleanupWorker deletes upload files that no message references any more.
//
// Why this exists: uploads are the only place a phone screenshot or a camera
// shot lands, and nothing ever removed them. The directory therefore grew
// without bound — a project that had ever received an image kept every image it
// had ever received, including ones whose messages had since been deleted.
//
// What it must NOT do is break a live attachment. Two rules cover that:
//
//   - A file still named by any chat_history/queued_messages/file_shares row is
//     kept, however old it is. "Old" only means "nobody looked at it recently",
//     which is not the same as "nobody needs it": a message can sit untouched
//     for months and still be sent to the AI tomorrow.
//   - References are matched by their path SUFFIX under .clawbench/uploads/,
//     not by absolute path. Project directories get renamed and moved, and the
//     stored file paths are NOT rewritten when that happens — comparing
//     absolute paths would make every attachment of a renamed project look
//     unreferenced and delete them.
type UploadCleanupWorker struct {
	cfg      model.Config
	stopCh   chan struct{}
	doneCh   chan struct{}
	mu       sync.Mutex
	running  bool
	startup  time.Duration // delay before the first run
	interval time.Duration // interval between runs

	// listProjects returns every registered project (id, path). Overridable so
	// tests do not need a database.
	listProjects func() ([]uploadCleanupProject, error)
	// referencedUploads returns the upload-relative keys ("name.png") that rows
	// of the given project still reference. Overridable for tests.
	referencedUploads func(projectID int64, projectPath string) (map[string]struct{}, error)
}

type uploadCleanupProject struct {
	ID   int64
	Path string
}

var (
	globalUploadCleanup *UploadCleanupWorker
	uploadCleanupMu     sync.Mutex
)

// NewUploadCleanupWorker creates a worker backed by the real database.
func NewUploadCleanupWorker(cfg model.Config) *UploadCleanupWorker {
	return &UploadCleanupWorker{
		cfg:               cfg,
		stopCh:            make(chan struct{}),
		doneCh:            make(chan struct{}),
		startup:           10 * time.Minute,
		interval:          24 * time.Hour,
		listProjects:      listProjectsForUploadCleanup,
		referencedUploads: referencedUploadsForProject,
	}
}

// StartUploadCleanupWorker starts the global upload cleanup worker.
func StartUploadCleanupWorker(cfg model.Config) {
	uploadCleanupMu.Lock()
	globalUploadCleanup = NewUploadCleanupWorker(cfg)
	globalUploadCleanup.Start()
	uploadCleanupMu.Unlock()
}

// StopUploadCleanupWorker stops the global upload cleanup worker.
func StopUploadCleanupWorker() {
	uploadCleanupMu.Lock()
	if globalUploadCleanup != nil {
		globalUploadCleanup.Stop()
		globalUploadCleanup = nil
	}
	uploadCleanupMu.Unlock()
}

// ReconfigureUploadCleanup recreates the worker so a changed retention policy
// takes effect without a restart.
func ReconfigureUploadCleanup(cfg model.Config) {
	StopUploadCleanupWorker()
	StartUploadCleanupWorker(cfg)
}

// Start begins the cleanup loop in a goroutine.
func (w *UploadCleanupWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	go w.run()
	slog.Info(
		"upload cleanup worker started",
		slog.Bool("enabled", w.cfg.Upload.RetentionEnabled),
		slog.Int("retention_days", w.cfg.Upload.RetentionDays),
	)
}

// Stop signals the worker to stop and waits for it to finish.
func (w *UploadCleanupWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.mu.Unlock()

	close(w.stopCh)
	<-w.doneCh

	w.mu.Lock()
	w.running = false
	w.mu.Unlock()

	slog.Info("upload cleanup worker stopped")
}

func (w *UploadCleanupWorker) run() {
	defer close(w.doneCh)

	select {
	case <-time.After(w.startup):
	case <-w.stopCh:
		return
	}

	w.cleanup()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.cleanup()
		}
	}
}

// cleanup performs one purge cycle over every registered project.
func (w *UploadCleanupWorker) cleanup() {
	if !w.cfg.Upload.RetentionEnabled {
		slog.Debug("upload cleanup: disabled, skipping")
		return
	}
	if w.cfg.Upload.RetentionDays <= 0 {
		slog.Debug("upload cleanup: retention days is 0 (keep forever), skipping")
		return
	}

	projects, err := w.listProjects()
	if err != nil {
		slog.Error("upload cleanup: failed to list projects", slog.String("err", err.Error()))
		return
	}

	cutoff := time.Now().AddDate(0, 0, -w.cfg.Upload.RetentionDays)
	var totalRemoved int
	for _, p := range projects {
		// A project whose directory is gone has no uploads to clean. Skipping
		// the query keeps a large registry of dead projects cheap.
		if p.Path == "" || !dirExistsForCleanup(p.Path) {
			continue
		}
		referenced, refErr := w.referencedUploads(p.ID, p.Path)
		if refErr != nil {
			// Deleting without the reference set would be the one failure mode
			// that loses live attachments, so skip this project entirely.
			slog.Error("upload cleanup: failed to read references, skipping project",
				slog.Int64("project_id", p.ID), slog.String("err", refErr.Error()))
			continue
		}
		removed, rmErr := cleanupProjectUploads(p.Path, cutoff, referenced)
		if rmErr != nil {
			slog.Error("upload cleanup: failed to clean project",
				slog.Int64("project_id", p.ID), slog.String("err", rmErr.Error()))
			continue
		}
		totalRemoved += removed
	}

	if totalRemoved > 0 {
		slog.Info(
			"upload cleanup: removed unreferenced uploads",
			slog.Int("files", totalRemoved),
			slog.Int("retention_days", w.cfg.Upload.RetentionDays),
		)
	}
}

// cleanupProjectUploads deletes files under <project>/.clawbench/uploads that
// are older than cutoff and not present in referenced (keyed by base name).
//
// Only regular files are touched: a subdirectory here would come from a
// directory upload and may hold a whole tree the user still expects, so it is
// left alone rather than guessed at.
func cleanupProjectUploads(projectPath string, cutoff time.Time, referenced map[string]struct{}) (int, error) {
	dir := filepath.Join(projectPath, filepath.FromSlash(uploadsRelDir))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, keep := referenced[entry.Name()]; keep {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			// Vanished between ReadDir and Stat — nothing to do.
			if os.IsNotExist(statErr) {
				continue
			}
			return removed, statErr
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if rmErr := os.Remove(filepath.Join(dir, entry.Name())); rmErr != nil {
			if os.IsNotExist(rmErr) {
				continue
			}
			// One undeletable file must not abort the rest of the project.
			slog.Warn("upload cleanup: failed to remove file",
				slog.String("path", filepath.Join(dir, entry.Name())),
				slog.String("err", rmErr.Error()))
			continue
		}
		removed++
	}
	return removed, nil
}

// uploadKey extracts the "<name>" of a stored attachment path that lives under
// .clawbench/uploads/. Returns "" for anything else (project files, quotes,
// URLs, share-in entries), which are never cleaned by this worker.
//
// Matching on this key rather than the absolute path is what makes the check
// survive a project rename: chat_history.files is NOT rewritten when a project
// directory moves, so an absolute comparison would report every old attachment
// as unreferenced.
func uploadKey(p string) string {
	if p == "" {
		return ""
	}
	// Normalize BOTH separators, not just filepath.ToSlash: the stored path may
	// have been written on another platform (a project directory shared between
	// a Windows client and a Linux server), and filepath.ToSlash is a no-op for
	// backslashes on Unix.
	slashed := strings.ReplaceAll(p, `\`, "/")
	marker := uploadsRelDir + "/"
	idx := strings.LastIndex(slashed, marker)
	if idx < 0 {
		return ""
	}
	rest := slashed[idx+len(marker):]
	// A nested path (directory upload) is not a flat uploads entry; the worker
	// only ever deletes flat files, so such a key can never match a candidate.
	if rest == "" || strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// listProjectsForUploadCleanup reads the project registry directly instead of
// going through ListAllProjects: the listing joins chat_sessions and stats
// every directory, both wasted work for a sweep that only needs id + path.
func listProjectsForUploadCleanup() ([]uploadCleanupProject, error) {
	rows, err := store.ReadDB().QueryContext(context.Background(),
		`SELECT id, path FROM projects WHERE path != ''`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var projects []uploadCleanupProject
	for rows.Next() {
		var p uploadCleanupProject
		if scanErr := rows.Scan(&p.ID, &p.Path); scanErr != nil {
			return nil, scanErr
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// referencedUploadsForProject collects every upload file name still named by a
// row of this project.
//
// Three tables can reference an attachment: chat_history (the message that
// carried it), queued_messages (a message typed but not yet answered — its
// files are already on disk), and file_shares (a share link pointing at an
// uploaded file). Missing any one of them would delete a file that is still
// reachable.
//
// The LIKE prefilter keeps this cheap on a project with a long history; the
// parse below is what actually decides.
func referencedUploadsForProject(projectID int64, _ string) (map[string]struct{}, error) {
	refs := make(map[string]struct{})
	// The prefilter deliberately matches on the two stable components with `%`
	// between them instead of the literal ".clawbench/uploads". A stored path
	// may use either separator, and inside the `files` JSON column a backslash
	// is escaped AGAIN ("\\"), so any single literal spelling would filter real
	// references out before uploadKey ever sees them — which would classify a
	// live attachment as unreferenced and delete it. `%` covers both separators
	// and both escaping forms; uploadKey makes the actual decision.
	like := "%" + ".clawbench" + "%" + "uploads" + "%"

	queries := []struct {
		sql  string
		args []any
	}{
		{
			`SELECT files FROM chat_history
			  WHERE project_id = ? AND files IS NOT NULL
			    AND files LIKE ?`,
			[]any{projectID, like},
		},
		{
			`SELECT files FROM queued_messages
			  WHERE project_id = ? AND files IS NOT NULL
			    AND files LIKE ?`,
			[]any{projectID, like},
		},
		{
			// file_shares.path is a single path, not a files JSON array, and its
			// project_id is the project the share was CREATED from — which need
			// not be the project the file lives in (a share can be created while
			// another project is active). Scoping this query by project_id would
			// therefore miss a real reference and let the sweep delete a file
			// that still has a working public link, so it is matched by path
			// alone; uploadKey's per-project key keeps the result scoped.
			`SELECT path FROM file_shares
			  WHERE path LIKE ?`,
			[]any{like},
		},
	}

	for _, q := range queries {
		if err := collectUploadRefs(q.sql, q.args, refs); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

// collectUploadRefs runs one reference query and merges its upload keys into
// refs. Split out so the row set is closed by a single defer on every exit
// path, including a mid-iteration scan error.
func collectUploadRefs(query string, args []any, refs map[string]struct{}) error {
	rows, err := store.ReadDB().QueryContext(context.Background(), query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var raw string
		if scanErr := rows.Scan(&raw); scanErr != nil {
			return scanErr
		}
		for _, path := range attachmentPathsFromColumn(raw) {
			if key := uploadKey(path); key != "" {
				refs[key] = struct{}{}
			}
		}
	}
	return rows.Err()
}

// attachmentPathsFromColumn pulls paths out of one `files` column value.
//
// The column has two historical shapes: a JSON array of FileEntry objects
// (current) and a JSON array of bare strings (legacy). file_shares rows hold a
// single bare path instead of JSON, so a value that does not parse as JSON is
// treated as one path. Returning nothing for unparseable input is safe in the
// only direction that matters here — it would mean "not referenced", so the
// caller must not treat an empty result as authoritative for a malformed row;
// that is why the JSON branch is attempted first and bare strings are still
// accepted.
func attachmentPathsFromColumn(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var entries []model.FileEntry
	if err := json.Unmarshal([]byte(raw), &entries); err == nil {
		paths := make([]string, 0, len(entries))
		for _, e := range entries {
			// Quote/URL entries carry a label or an address, not a local file.
			if e.IsQuote() || e.IsURL() {
				continue
			}
			paths = append(paths, e.Path)
		}
		return paths
	}

	var bare []string
	if err := json.Unmarshal([]byte(raw), &bare); err == nil {
		return bare
	}

	// Not JSON at all — a single path (file_shares.path).
	return []string{raw}
}

// dirExistsForCleanup reports whether path is an existing directory. Kept
// local so this file has no dependency on the handler helpers.
func dirExistsForCleanup(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
