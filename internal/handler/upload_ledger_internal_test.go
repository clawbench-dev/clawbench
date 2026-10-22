package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A ledger entry written by anything other than the upload endpoint (the AI
// agent, another tool, a hand edit) must not be able to point outside the
// project. The write path validates, but the ledger is a plain file inside the
// project and the read path must not trust it either.
func TestListLedgerUploads_IgnoresEntriesOutsideTheProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// A real file outside the project that the entry points at.
	outsideDir := filepath.Join(filepath.Dir(env.ProjectDir), "elsewhere")
	require.NoError(t, os.MkdirAll(outsideDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outsideDir, "secret.png"), []byte("x"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(env.ProjectDir, ".clawbench"), 0o755))
	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "../elsewhere/secret.png"},
		{Path: "../../etc/passwd"},
		{Path: "/etc/passwd"},
		{Path: ".clawbench/uploads/chat.png"},
		{Path: "legit.png"},
	}))
	createTestFile(t, env.ProjectDir, "legit.png", "real")

	files := recentUploadPaths(t, env)
	require.Len(t, files, 1, "only the in-project, non-internal entry may be listed")
	assert.Equal(t, "legit.png", files[0].Path)
}

// Sanitization is the single gate used by both the write and read paths, so its
// rejections are worth pinning directly.
func TestSanitizeLedgerPath(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "plain file", input: "a.png", want: "a.png", ok: true},
		{name: "nested", input: "docs/images/a.png", want: "docs/images/a.png", ok: true},
		{name: "empty", input: "", ok: false},
		{name: "absolute", input: "/etc/passwd", ok: false},
		{name: "traversal", input: "../a.png", ok: false},
		{name: "traversal through a segment", input: "docs/../../a.png", ok: false},
		{name: "the project root itself", input: ".", ok: false},
		{name: "parent itself", input: "..", ok: false},
		{name: "clawbench root", input: ".clawbench", ok: false},
		{name: "uploads dir", input: ".clawbench/uploads/a.png", ok: false},
		{name: "share-in dir", input: ".clawbench/share-in/a.png", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := sanitizeLedgerPath(tc.input)
			assert.Equal(t, tc.ok, ok)
			if tc.ok {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestRecordUploadedFile_RejectsUnstorablePaths(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for _, p := range []string{"", "/abs/x.png", "../out.png", ".clawbench/uploads/x.png", "."} {
		recordUploadedFile(env.ProjectDir, p)
	}
	assert.Empty(t, readUploadLedger(env.ProjectDir), "none of these may be recorded")
}

func TestRecordUploadedFile_CapsAtMaxEntries(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Seed one past the cap so the trim branch is exercised.
	seed := make([]uploadLedgerEntry, 0, uploadLedgerMaxEntries+1)
	for i := 0; i <= uploadLedgerMaxEntries; i++ {
		seed = append(seed, uploadLedgerEntry{Path: filepath.ToSlash(filepath.Join("f", ledgerTestIndex(i)+".png"))})
	}
	require.NoError(t, writeUploadLedger(env.ProjectDir, seed))

	recordUploadedFile(env.ProjectDir, "newest.png")

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, uploadLedgerMaxEntries)
	assert.Equal(t, "newest.png", entries[0].Path)
}

func TestListRecentUploads_TrimsToLimit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for i := range 5 {
		recordUploadedFile(env.ProjectDir, filepath.ToSlash(filepath.Join("p", ledgerTestIndex(i)+".png")))
		createTestFile(t, env.ProjectDir, filepath.Join("p", ledgerTestIndex(i)+".png"), "x")
	}

	assert.Len(t, listRecentUploads(env.ProjectDir, 3), 3)
}

func TestForgetUploadedFile_EmptyPathIsNotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	found, err := forgetUploadedFile(env.ProjectDir, "")
	require.NoError(t, err)
	assert.False(t, found)
}

// Removing one row must leave every other row intact. A `break` instead of a
// `continue` in the filter loop would silently drop every entry after the match
// — the user removes one row and the rest of their history disappears — so this
// pins the filter rather than just the count.
func TestForgetUploadedFile_KeepsTheOtherEntries(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for _, name := range []string{"a.png", "b.png", "c.png"} {
		recordUploadedFile(env.ProjectDir, name)
		createTestFile(t, env.ProjectDir, name, "x")
	}
	// recordUploadedFile inserts at the front, so the ledger order is c, b, a.
	require.Len(t, readUploadLedger(env.ProjectDir), 3)

	found, err := forgetUploadedFile(env.ProjectDir, "b.png")
	require.NoError(t, err)
	require.True(t, found)

	paths := make([]string, 0, 2)
	for _, e := range readUploadLedger(env.ProjectDir) {
		paths = append(paths, e.Path)
	}
	assert.ElementsMatch(t, []string{"c.png", "a.png"}, paths,
		"only the requested entry may be removed")

	// And the file itself is untouched: this is an un-list, not a delete.
	assert.FileExists(t, filepath.Join(env.ProjectDir, "b.png"))
}

// A row that was never recorded is "not found", not a silent success: the
// caller turns that into 403 rather than pretending it removed something.
func TestForgetUploadedFile_UnknownPathLeavesTheLedgerAlone(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "kept.png")

	found, err := forgetUploadedFile(env.ProjectDir, "never-recorded.png")
	require.NoError(t, err)
	assert.False(t, found)

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, 1)
	assert.Equal(t, "kept.png", entries[0].Path)
}

// A failed record must not leak the temp file it staged.
func TestRecordUploadedFile_FailedWriteLeavesNoTempFile(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Turn the ledger path into a directory: CreateTemp inside it then fails.
	ledgerPath := uploadLedgerFile(env.ProjectDir)
	require.NoError(t, os.MkdirAll(filepath.Join(ledgerPath, "blocker"), 0o755))

	recordUploadedFile(env.ProjectDir, "b.png")
	createTestFile(t, env.ProjectDir, "b.png", "x")

	entries, readErr := os.ReadDir(filepath.Dir(ledgerPath))
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".upload-log-", "a failed write must not leak a temp file")
	}
}

// A write failure must be reported as a server fault, not as "this row is not
// in the ledger": the two look identical to the caller otherwise, and a
// read-only data directory would tell the user their own row is unknown.
func TestForgetUploadedFile_WriteFailureIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not make a directory unwritable on Windows (mode bits are not enforced)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "a.png")
	createTestFile(t, env.ProjectDir, "a.png", "x")

	// Read must still work (the file is readable), only the write must fail:
	// that is what distinguishes "found but could not persist" from "absent".
	clawbenchDir := filepath.Join(env.ProjectDir, ".clawbench")
	require.NoError(t, os.Chmod(clawbenchDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(clawbenchDir, 0o755) })

	found, err := forgetUploadedFile(env.ProjectDir, "a.png")
	require.Error(t, err, "a write failure must surface, not read as 'not recorded'")
	assert.False(t, found)

	// The file itself is untouched either way.
	assert.FileExists(t, filepath.Join(env.ProjectDir, "a.png"))
}

// A corrupt ledger must not take the tab down, and a later upload must repair
// it rather than failing to record forever.
func TestRecordUploadedFile_RepairsCorruptLedger(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	ledgerPath := uploadLedgerFile(env.ProjectDir)
	require.NoError(t, os.MkdirAll(filepath.Dir(ledgerPath), 0o755))
	require.NoError(t, os.WriteFile(ledgerPath, []byte("{not json"), 0o644))

	recordUploadedFile(env.ProjectDir, "fresh.png")

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, 1)
	assert.Equal(t, "fresh.png", entries[0].Path)
}

// Clearing reports the rows the user could SEE, not the records: dead entries
// are kept on purpose, so counting them would claim far more than the tab held.
func TestClearUploadLedger_CountsOnlyExistingFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "live.png")
	createTestFile(t, env.ProjectDir, "live.png", "x")
	// Two dead entries: their files were deleted.
	recordUploadedFile(env.ProjectDir, "dead1.png")
	recordUploadedFile(env.ProjectDir, "dead2.png")
	// And a hostile entry that never resolved to a file.
	require.NoError(t, writeUploadLedger(env.ProjectDir, append(readUploadLedger(env.ProjectDir),
		uploadLedgerEntry{Path: "../outside.png"})))

	cleared, remaining, err := clearUploadLedger(env.ProjectDir)
	require.NoError(t, err)
	assert.Equal(t, 1, cleared, "only the file that still exists counts")
	assert.Equal(t, 0, remaining)
	assert.Empty(t, readUploadLedger(env.ProjectDir))
}

// A ledger write failure during clear-all must surface as a partial result, not
// as a clean success: the toast would otherwise claim the list is empty while
// the project rows are still there. `failed` counts the rows that SURVIVED —
// reporting a bare 1 would read as one stray file when the whole project half
// is untouched.
func TestUploadRecentClearAll_ReportsLedgerWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not make a directory unwritable on Windows (mode bits are not enforced)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	seedRecentDir(t, env.ProjectDir, "uploads", "u.png")
	recordUploadedFile(env.ProjectDir, "p1.png")
	createTestFile(t, env.ProjectDir, "p1.png", "x")
	recordUploadedFile(env.ProjectDir, "p2.png")
	createTestFile(t, env.ProjectDir, "p2.png", "x")

	// Readable but not writable: the ledger can still be parsed (so both rows
	// are known), only the rewrite fails.
	clawbenchDir := filepath.Join(env.ProjectDir, ".clawbench")
	require.NoError(t, os.Chmod(clawbenchDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(clawbenchDir, 0o755) })

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{"all": true}))
	resp := decodeClearResponse(t, w)
	assert.Equal(t, true, resp["ok"])
	assert.Equal(t, float64(1), resp["deleted"], "the uploads-directory file was deleted")
	assert.Equal(t, float64(2), resp["failed"], "both surviving ledger rows must be counted")
}

// The ledger must not be consulted for a path the directory-scoped delete owns:
// those files are app-owned and deleting them is correct.
func TestDeleteLedgerEntry_LeavesClawbenchPathsToTheDirectoryDelete(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/upload/recent", nil)
	withProjectCookie(req, env.ProjectDir)

	assert.False(t, deleteLedgerEntry(discardWriter{}, req, env.ProjectDir, ".clawbench/uploads/a.png"))
	assert.False(t, deleteLedgerEntry(discardWriter{}, req, env.ProjectDir, ".clawbench/share-in/a.png"))
	assert.False(t, deleteLedgerEntry(discardWriter{}, req, env.ProjectDir, "../outside.png"))
}

// A write failure while un-listing must be a 500, not a success: the row would
// stay in the list while the UI reported it removed.
func TestDeleteLedgerEntry_WriteFailureIsA500(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not make a directory unwritable on Windows (mode bits are not enforced)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "a.png")
	createTestFile(t, env.ProjectDir, "a.png", "x")

	clawbenchDir := filepath.Join(env.ProjectDir, ".clawbench")
	require.NoError(t, os.Chmod(clawbenchDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(clawbenchDir, 0o755) })

	req := newRequest(t, http.MethodDelete, "/api/upload/recent", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(UploadRecent, clearAllRequest(t, "/api/upload/recent", env.ProjectDir, map[string]any{
		"path": "a.png",
	}))
	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"a ledger write failure must not be reported as success")
	assert.FileExists(t, filepath.Join(env.ProjectDir, "a.png"))
}

// discardWriter absorbs a response so a predicate can be tested without
// asserting on the body it may or may not have written.
type discardWriter struct{}

func (discardWriter) Header() http.Header         { return http.Header{} }
func (discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardWriter) WriteHeader(int)             {}

// ledgerTestIndex names test fixtures without importing strconv.
func ledgerTestIndex(n int) string { return fmt.Sprintf("%d", n) }
