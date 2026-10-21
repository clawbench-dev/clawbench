package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeUpload creates a file under <project>/.clawbench/uploads with the given
// modification time.
func writeUpload(t *testing.T, projectPath, name string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(projectPath, ".clawbench", "uploads")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	require.NoError(t, os.Chtimes(p, modTime, modTime))
	return p
}

func exists(t *testing.T, p string) bool {
	t.Helper()
	_, err := os.Stat(p)
	return err == nil
}

// ---------- cleanupProjectUploads ----------

func TestCleanupProjectUploads_DeletesOldUnreferencedFiles(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -40)
	stale := writeUpload(t, dir, "old.png", old)

	removed, err := cleanupProjectUploads(dir, time.Now().AddDate(0, 0, -30), nil)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.False(t, exists(t, stale), "old unreferenced upload must be deleted")
}

func TestCleanupProjectUploads_KeepsReferencedFilesHoweverOld(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -400)
	kept := writeUpload(t, dir, "pinned.png", old)
	doomed := writeUpload(t, dir, "other.png", old)

	removed, err := cleanupProjectUploads(dir, time.Now().AddDate(0, 0, -30),
		map[string]struct{}{"pinned.png": {}})
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	assert.True(t, exists(t, kept), "a file a message still references must survive any age")
	assert.False(t, exists(t, doomed))
}

func TestCleanupProjectUploads_KeepsRecentUnreferencedFiles(t *testing.T) {
	dir := t.TempDir()
	// Two days old, well inside a 30-day window: nobody has referenced it yet,
	// but it may be attached to a message the user is still typing.
	fresh := writeUpload(t, dir, "fresh.png", time.Now().AddDate(0, 0, -2))

	removed, err := cleanupProjectUploads(dir, time.Now().AddDate(0, 0, -30), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	assert.True(t, exists(t, fresh))
}

func TestCleanupProjectUploads_MissingDirIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	removed, err := cleanupProjectUploads(dir, time.Now(), nil)
	require.NoError(t, err, "a project that never uploaded anything must not error")
	assert.Equal(t, 0, removed)
}

func TestCleanupProjectUploads_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	uploads := filepath.Join(dir, ".clawbench", "uploads")
	nested := filepath.Join(uploads, "batch")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	inner := filepath.Join(nested, "a.png")
	require.NoError(t, os.WriteFile(inner, []byte("x"), 0o644))
	old := time.Now().AddDate(0, 0, -40)
	require.NoError(t, os.Chtimes(inner, old, old))
	require.NoError(t, os.Chtimes(nested, old, old))

	removed, err := cleanupProjectUploads(dir, time.Now().AddDate(0, 0, -30), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, removed, "directory uploads are left alone, not guessed at")
	assert.True(t, exists(t, inner))
}

// ---------- uploadKey ----------

func TestUploadKey(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"relative upload", ".clawbench/uploads/a.png", "a.png"},
		{"absolute upload", "/home/u/proj/.clawbench/uploads/a.png", "a.png"},
		{"windows separators", `C:\proj\.clawbench\uploads\a.png`, "a.png"},
		{"project file", "src/main.go", ""},
		{"share-in", ".clawbench/share-in/a.png", ""},
		{"generated", ".clawbench/generated/img.png", ""},
		{"empty", "", ""},
		{"nested under uploads", ".clawbench/uploads/batch/a.png", ""},
		{"dir itself", ".clawbench/uploads/", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, uploadKey(tc.in))
		})
	}
}

// uploadKey must survive a project rename: the stored path keeps the OLD
// directory, but the key is derived from the suffix, so it still matches the
// file sitting in the NEW directory.
func TestUploadKey_IsStableAcrossProjectRename(t *testing.T) {
	stored := "/old/location/proj/.clawbench/uploads/shot.png"
	assert.Equal(t, "shot.png", uploadKey(stored))
}

// ---------- attachmentPathsFromColumn ----------

func TestAttachmentPathsFromColumn(t *testing.T) {
	t.Run("file entry objects", func(t *testing.T) {
		got := attachmentPathsFromColumn(
			`[{"path":".clawbench/uploads/a.png","isDir":false},{"path":"src/b.go","isDir":false}]`,
		)
		assert.Equal(t, []string{".clawbench/uploads/a.png", "src/b.go"}, got)
	})

	t.Run("legacy bare strings", func(t *testing.T) {
		got := attachmentPathsFromColumn(`[".clawbench/uploads/a.png"]`)
		assert.Equal(t, []string{".clawbench/uploads/a.png"}, got)
	})

	t.Run("single bare path from file_shares", func(t *testing.T) {
		got := attachmentPathsFromColumn("/p/.clawbench/uploads/a.png")
		assert.Equal(t, []string{"/p/.clawbench/uploads/a.png"}, got)
	})

	t.Run("quote and url entries are not files", func(t *testing.T) {
		// A quote's Path is a human label and a URL's Path is empty; neither
		// names a file on disk, so neither may be treated as a reference.
		got := attachmentPathsFromColumn(
			`[{"path":"src/a.go:1-3","kind":"quote"},{"path":"","kind":"url","url":"https://x"}]`,
		)
		assert.Empty(t, got)
	})

	t.Run("empty column", func(t *testing.T) {
		assert.Empty(t, attachmentPathsFromColumn(""))
		assert.Empty(t, attachmentPathsFromColumn("   "))
	})
}

// ---------- worker gating ----------

// newTestUploadWorker builds a worker with fake project/reference sources so
// the gating logic can be tested without a database.
func newTestUploadWorker(cfg model.Config, projects []uploadCleanupProject, refs map[int64]map[string]struct{}) *UploadCleanupWorker {
	w := NewUploadCleanupWorker(cfg)
	w.listProjects = func() ([]uploadCleanupProject, error) { return projects, nil }
	w.referencedUploads = func(id int64, _ string) (map[string]struct{}, error) {
		if r, ok := refs[id]; ok {
			return r, nil
		}
		return map[string]struct{}{}, nil
	}
	return w
}

func TestUploadCleanup_SkipsWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	stale := writeUpload(t, dir, "old.png", time.Now().AddDate(0, 0, -90))

	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = false
	cfg.Upload.RetentionDays = 30
	w := newTestUploadWorker(cfg, []uploadCleanupProject{{ID: 1, Path: dir}}, nil)

	w.cleanup()

	assert.True(t, exists(t, stale), "disabled must mean untouched, not merely logged")
}

func TestUploadCleanup_SkipsWhenRetentionDaysIsZero(t *testing.T) {
	dir := t.TempDir()
	stale := writeUpload(t, dir, "old.png", time.Now().AddDate(0, 0, -90))

	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = true
	cfg.Upload.RetentionDays = 0
	w := newTestUploadWorker(cfg, []uploadCleanupProject{{ID: 1, Path: dir}}, nil)

	w.cleanup()

	assert.True(t, exists(t, stale), "0 days means keep forever")
}

func TestUploadCleanup_CleansEveryProjectWithItsOwnReferences(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	old := time.Now().AddDate(0, 0, -60)
	aKept := writeUpload(t, dirA, "keep.png", old)
	aDoomed := writeUpload(t, dirA, "drop.png", old)
	bDoomed := writeUpload(t, dirB, "drop.png", old)

	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = true
	cfg.Upload.RetentionDays = 30
	w := newTestUploadWorker(
		cfg,
		[]uploadCleanupProject{{ID: 1, Path: dirA}, {ID: 2, Path: dirB}},
		// Same file NAME in both projects: only project 1 references it, so
		// project 2's copy must still go. This is what a global reference set
		// would get wrong.
		map[int64]map[string]struct{}{1: {"keep.png": {}}},
	)

	w.cleanup()

	assert.True(t, exists(t, aKept))
	assert.False(t, exists(t, aDoomed))
	assert.False(t, exists(t, bDoomed), "a reference in another project must not protect this file")
}

func TestUploadCleanup_SkipsProjectWhenReferencesCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	stale := writeUpload(t, dir, "old.png", time.Now().AddDate(0, 0, -60))

	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = true
	cfg.Upload.RetentionDays = 30
	w := NewUploadCleanupWorker(cfg)
	w.listProjects = func() ([]uploadCleanupProject, error) {
		return []uploadCleanupProject{{ID: 1, Path: dir}}, nil
	}
	w.referencedUploads = func(int64, string) (map[string]struct{}, error) {
		return nil, os.ErrPermission
	}

	w.cleanup()

	// Deleting with an unknown reference set is the one failure mode that
	// loses live attachments, so the project must be left alone entirely.
	assert.True(t, exists(t, stale))
}

func TestUploadCleanup_SkipsProjectWhoseDirectoryIsGone(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "deleted-project")

	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = true
	cfg.Upload.RetentionDays = 30
	w := newTestUploadWorker(cfg, []uploadCleanupProject{{ID: 1, Path: gone}}, nil)

	// Must not panic or error out; the registry keeps projects whose directory
	// was removed.
	require.NotPanics(t, w.cleanup)
}

// ---------- referencedUploadsForProject (real database) ----------

// referencedUploadsTestSchema is a minimal fixture: only the three tables the
// reference query touches. Using the full production schema would make this
// test depend on unrelated migrations.
const referencedUploadsTestSchema = `
CREATE TABLE IF NOT EXISTS chat_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL,
	files TEXT
);
CREATE TABLE IF NOT EXISTS queued_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL,
	files TEXT
);
CREATE TABLE IF NOT EXISTS file_shares (
	token TEXT PRIMARY KEY,
	path TEXT NOT NULL,
	project_id INTEGER NOT NULL DEFAULT 0
);
`

func setupReferencedUploadsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// :memory: gives each pooled connection its own database.
	db.SetMaxOpenConns(1)
	_, err = db.Exec(referencedUploadsTestSchema)
	require.NoError(t, err)
	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	return db
}

func TestReferencedUploadsForProject_CollectsAllThreeTables(t *testing.T) {
	db := setupReferencedUploadsDB(t)

	_, err := db.Exec(`INSERT INTO chat_history (project_id, files) VALUES
		(1, '[{"path":".clawbench/uploads/history.png","isDir":false}]'),
		(2, '[{"path":".clawbench/uploads/other-project.png","isDir":false}]'),
		(1, '[{"path":"src/main.go","isDir":false}]'),
		(1, NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO queued_messages (project_id, files) VALUES
		(1, '[{"path":".clawbench/uploads/queued.png","isDir":false}]')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO file_shares (token, path, project_id) VALUES
		('t1', '/p/.clawbench/uploads/shared.png', 1)`)
	require.NoError(t, err)

	refs, err := referencedUploadsForProject(1, "/p")
	require.NoError(t, err)

	assert.Equal(t, map[string]struct{}{
		"history.png": {},
		"queued.png":  {},
		"shared.png":  {},
	}, refs, "chat_history, queued_messages and file_shares must all protect their uploads")
}

func TestReferencedUploadsForProject_IgnoresOtherProjects(t *testing.T) {
	db := setupReferencedUploadsDB(t)
	_, err := db.Exec(`INSERT INTO chat_history (project_id, files) VALUES
		(2, '[{"path":".clawbench/uploads/theirs.png","isDir":false}]')`)
	require.NoError(t, err)

	refs, err := referencedUploadsForProject(1, "/p")
	require.NoError(t, err)
	assert.Empty(t, refs, "a sibling project's attachment must not protect a same-named file here")
}

func TestReferencedUploadsForProject_HandlesLegacyStringArray(t *testing.T) {
	db := setupReferencedUploadsDB(t)
	// Pre-migration rows stored a bare array of paths.
	_, err := db.Exec(`INSERT INTO chat_history (project_id, files) VALUES
		(1, '[".clawbench/uploads/legacy.png"]')`)
	require.NoError(t, err)

	refs, err := referencedUploadsForProject(1, "/p")
	require.NoError(t, err)
	assert.Contains(t, refs, "legacy.png")
}

// A path stored with backslashes (a project directory shared between a Windows
// client and a Unix server) must still count as a reference. The LIKE prefilter
// is what could drop it before uploadKey ever runs, so this must go through the
// real query, not just uploadKey.
func TestReferencedUploadsForProject_MatchesBackslashPaths(t *testing.T) {
	db := setupReferencedUploadsDB(t)
	_, err := db.Exec(`INSERT INTO chat_history (project_id, files) VALUES
		(1, '[{"path":"C:\\proj\\.clawbench\\uploads\\win.png","isDir":false}]')`)
	require.NoError(t, err)

	refs, err := referencedUploadsForProject(1, "/p")
	require.NoError(t, err)
	assert.Contains(t, refs, "win.png",
		"a backslash path must survive the LIKE prefilter, or the sweep deletes a live attachment")
}

// A share can be created while a DIFFERENT project is active (the public link
// only needs an absolute path), so its row carries that other project's id.
// Scoping the query by project_id would miss the reference and let the sweep
// delete a file whose public link still works.
func TestReferencedUploadsForProject_FileShareFromAnotherProject(t *testing.T) {
	db := setupReferencedUploadsDB(t)
	_, err := db.Exec(`INSERT INTO file_shares (token, path, project_id) VALUES
		('t1', '/p/.clawbench/uploads/shared.png', 2)`)
	require.NoError(t, err)

	refs, err := referencedUploadsForProject(1, "/p")
	require.NoError(t, err)
	assert.Contains(t, refs, "shared.png",
		"a share created from another project still references this file")
}

// ---------- worker lifecycle ----------

// newFastUploadWorker shortens the loop's timing so the lifecycle can be
// exercised in milliseconds. The production constructor is used, so the
// gating/config wiring is the real one.
func newFastUploadWorker(t *testing.T, cfg model.Config) *UploadCleanupWorker {
	t.Helper()
	w := NewUploadCleanupWorker(cfg)
	w.startup = 10 * time.Millisecond
	w.interval = 20 * time.Millisecond
	return w
}

func TestUploadCleanupWorker_StartSweepsThenStop(t *testing.T) {
	dir := t.TempDir()
	stale := writeUpload(t, dir, "old.png", time.Now().AddDate(0, 0, -90))

	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = true
	cfg.Upload.RetentionDays = 30

	w := newFastUploadWorker(t, cfg)
	w.listProjects = func() ([]uploadCleanupProject, error) {
		return []uploadCleanupProject{{ID: 1, Path: dir}}, nil
	}
	w.referencedUploads = func(int64, string) (map[string]struct{}, error) {
		return map[string]struct{}{}, nil
	}

	w.Start()
	// Wait for the sweep instead of sleeping a fixed amount: the assertion is
	// about the outcome, so poll until it happens (or fail).
	require.Eventually(t, func() bool { return !exists(t, stale) }, 3*time.Second, 10*time.Millisecond,
		"the started worker must delete an unreferenced old upload")
	w.Stop()

	// Stop must be idempotent — hot reload calls it unconditionally.
	require.NotPanics(t, w.Stop)
}

func TestUploadCleanupWorker_StartIsIdempotent(t *testing.T) {
	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = false // no sweep; only lifecycle
	w := newFastUploadWorker(t, cfg)
	w.listProjects = func() ([]uploadCleanupProject, error) { return nil, nil }

	w.Start()
	w.Start() // second call must not spawn a second loop or panic
	w.Stop()
	require.NotPanics(t, w.Stop)
}

func TestUploadCleanupWorker_StopBeforeStartIsSafe(t *testing.T) {
	w := newFastUploadWorker(t, model.Config{})
	require.NotPanics(t, w.Stop)
}

func TestStartStopReconfigureUploadCleanup(t *testing.T) {
	cfg := model.Config{}
	cfg.Upload.RetentionEnabled = false

	require.NotPanics(t, func() {
		StartUploadCleanupWorker(cfg)
		ReconfigureUploadCleanup(cfg)
		StopUploadCleanupWorker()
		// A second stop must be a no-op (global already nil).
		StopUploadCleanupWorker()
	})
}

// ---------- listProjectsForUploadCleanup (real database) ----------

func TestListProjectsForUploadCleanup(t *testing.T) {
	db := setupReferencedUploadsDB(t)
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL,
		UNIQUE(path)
	)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO projects (id, path) VALUES
		(1, '/a'), (2, '/b'), (3, '')`)
	require.NoError(t, err)

	projects, err := listProjectsForUploadCleanup()
	require.NoError(t, err)

	// Rows with an empty path are skipped: they have no directory to sweep.
	assert.ElementsMatch(t, []uploadCleanupProject{{ID: 1, Path: "/a"}, {ID: 2, Path: "/b"}}, projects)
}

// A typo in the projects query fails SAFE (no project is swept), so nothing
// else would catch it. This pins the table and columns it reads.
func TestListProjectsForUploadCleanup_EmptyRegistry(t *testing.T) {
	setupReferencedUploadsDB(t)
	_, err := store.WriteDBRaw().Exec(`CREATE TABLE IF NOT EXISTS projects (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT NOT NULL
	)`)
	require.NoError(t, err)

	projects, err := listProjectsForUploadCleanup()
	require.NoError(t, err)
	assert.Empty(t, projects)
}

// A permission error reading the uploads directory must surface as an error
// (so cleanup skips the project) rather than being swallowed as "empty".
func TestCleanupProjectUploads_UnreadableDirReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	uploads := filepath.Join(dir, ".clawbench", "uploads")
	require.NoError(t, os.MkdirAll(uploads, 0o755))
	writeUpload(t, dir, "a.png", time.Now().AddDate(0, 0, -90))
	require.NoError(t, os.Chmod(uploads, 0o000))
	t.Cleanup(func() { _ = os.Chmod(uploads, 0o755) })

	_, err := cleanupProjectUploads(dir, time.Now().AddDate(0, 0, -30), nil)
	require.Error(t, err, "an unreadable directory must not be reported as nothing-to-do")
}

// A non-regular entry (e.g. a symlink) is left alone: removing it would delete
// the link, and following it could touch a file outside the project.
func TestCleanupProjectUploads_SkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))

	uploads := filepath.Join(dir, ".clawbench", "uploads")
	require.NoError(t, os.MkdirAll(uploads, 0o755))
	link := filepath.Join(uploads, "link.png")
	require.NoError(t, os.Symlink(target, link))
	old := time.Now().AddDate(0, 0, -90)
	require.NoError(t, os.Chtimes(link, old, old))

	removed, err := cleanupProjectUploads(dir, time.Now().AddDate(0, 0, -30), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, removed)
	assert.True(t, exists(t, link), "a symlink must not be swept")
	assert.True(t, exists(t, target))
}
