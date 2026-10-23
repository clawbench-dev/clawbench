package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A malformed timestamp must be treated as "no timestamp" (keep the entry), not
// as "expired" — a hand-edited or truncated value must not silently delete rows.
func TestUploadLedgerEntry_UnparsableTimestampIsKept(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "weird.png", UploadedAt: "not-a-timestamp"},
	}))
	createTestFile(t, env.ProjectDir, "weird.png", "x")

	recordUploadedFile(env.ProjectDir, "new.png")
	createTestFile(t, env.ProjectDir, "new.png", "x")

	assert.Equal(t, []string{"new.png", "weird.png"}, ledgerPaths(t, env.ProjectDir))
}

func TestUploadLedgerEntry_UploadTime(t *testing.T) {
	_, ok := uploadLedgerEntry{}.uploadTime()
	assert.False(t, ok, "no timestamp means 'never expires'")

	_, ok = uploadLedgerEntry{UploadedAt: "garbage"}.uploadTime()
	assert.False(t, ok, "a malformed timestamp is treated as absent, not as expired")

	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	parsed, ok := uploadLedgerEntry{UploadedAt: stamp.Format(time.RFC3339)}.uploadTime()
	require.True(t, ok)
	assert.True(t, stamp.Equal(parsed))
}

// A rename that lands on the same relative path (e.g. same name, different
// spelling of the same directory) must not rewrite anything.
func TestMoveLedgerEntry_SameRelativePathIsANoOp(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "same.png")
	createTestFile(t, env.ProjectDir, "same.png", "x")
	before := ledgerPaths(t, env.ProjectDir)

	abs := filepath.Join(env.ProjectDir, "same.png")
	moveLedgerEntry(env.ProjectDir, abs, abs, false)

	assert.Equal(t, before, ledgerPaths(t, env.ProjectDir))
}

// A file rename must not touch an entry that merely shares a prefix.
func TestRemapLedgerPath_FileRenameDoesNotMatchPrefixes(t *testing.T) {
	got, moved := remapLedgerPath("docs/a.png.bak", "docs/a.png", "docs/b.png", false)
	assert.False(t, moved)
	assert.Equal(t, "docs/a.png.bak", got)

	got, moved = remapLedgerPath("docs/a.png", "docs/a.png", "docs/b.png", false)
	assert.True(t, moved)
	assert.Equal(t, "docs/b.png", got)
}

// A directory rename must not match a sibling whose name merely starts with the
// same characters (`docs2/x.png` is not under `docs/`).
func TestRemapLedgerPath_DirectoryRenameRespectsSegmentBoundary(t *testing.T) {
	got, moved := remapLedgerPath("docs2/x.png", "docs", "documentation", true)
	assert.False(t, moved)
	assert.Equal(t, "docs2/x.png", got)

	got, moved = remapLedgerPath("docs/x.png", "docs", "documentation", true)
	assert.True(t, moved)
	assert.Equal(t, "documentation/x.png", got)
}

// forgetLedgerPath must ignore anything outside the project or inside
// .clawbench/ — those are not ledger entries.
func TestForgetLedgerPath_IgnoresUnrecordablePaths(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "keep.png")
	createTestFile(t, env.ProjectDir, "keep.png", "x")
	before := ledgerPaths(t, env.ProjectDir)

	outside := filepath.Join(filepath.Dir(env.ProjectDir), "outside.png")
	forgetLedgerPath(env.ProjectDir, outside)
	forgetLedgerPath(env.ProjectDir, filepath.Join(env.ProjectDir, ".clawbench", "uploads", "x.png"))

	assert.Equal(t, before, ledgerPaths(t, env.ProjectDir))
}

// moveLedgerEntry must ignore paths that sanitize rejects on either side.
func TestMoveLedgerEntry_IgnoresUnrecordableTargets(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "keep.png")
	createTestFile(t, env.ProjectDir, "keep.png", "x")
	before := ledgerPaths(t, env.ProjectDir)

	// Destination inside .clawbench/ is not a ledger-storable path.
	moveLedgerEntry(env.ProjectDir,
		filepath.Join(env.ProjectDir, "keep.png"),
		filepath.Join(env.ProjectDir, ".clawbench", "uploads", "keep.png"), false)

	assert.Equal(t, before, ledgerPaths(t, env.ProjectDir))
}

// moveLedgerEntry reports nothing when no entry matched, so a rename of an
// unrecorded file never rewrites the ledger.
func TestMoveLedgerEntry_NoMatchDoesNotRewrite(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	recordUploadedFile(env.ProjectDir, "recorded.png")
	createTestFile(t, env.ProjectDir, "recorded.png", "x")
	createTestFile(t, env.ProjectDir, "unrecorded.png", "x")
	ledgerPath := uploadLedgerFile(env.ProjectDir)
	before, err := os.ReadFile(ledgerPath)
	require.NoError(t, err)

	moveLedgerEntry(env.ProjectDir,
		filepath.Join(env.ProjectDir, "unrecorded.png"),
		filepath.Join(env.ProjectDir, "still-unrecorded.png"), false)

	after, err := os.ReadFile(ledgerPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "an unrelated rename must not touch the file")
}

// The timestamp of an existing entry must survive a rename: the retention is
// measured from the upload, and a rename is not a new upload.
func TestMoveLedgerEntry_PreservesUploadTime(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	stamp := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)
	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "old.png", UploadedAt: stamp.Format(time.RFC3339)},
	}))
	createTestFile(t, env.ProjectDir, "old.png", "x")

	moveLedgerEntry(env.ProjectDir,
		filepath.Join(env.ProjectDir, "old.png"),
		filepath.Join(env.ProjectDir, "new.png"), false)

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, 1)
	assert.Equal(t, "new.png", entries[0].Path)
	assert.Equal(t, stamp.Format(time.RFC3339), entries[0].UploadedAt,
		"renaming must not reset the retention clock")
}

// A re-upload of an already-recorded path refreshes the timestamp: it is a new
// upload, so the retention starts again.
func TestRecordUploadedFile_ReuploadRefreshesTheTimestamp(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	old := time.Now().UTC().Add(-20 * 24 * time.Hour).Truncate(time.Second)
	require.NoError(t, writeUploadLedger(env.ProjectDir, []uploadLedgerEntry{
		{Path: "again.png", UploadedAt: old.Format(time.RFC3339)},
	}))
	createTestFile(t, env.ProjectDir, "again.png", "x")

	recordUploadedFile(env.ProjectDir, "again.png")

	entries := readUploadLedger(env.ProjectDir)
	require.Len(t, entries, 1)
	stamp, ok := entries[0].uploadTime()
	require.True(t, ok)
	assert.True(t, stamp.After(old), "a re-upload starts a new retention window")
}
