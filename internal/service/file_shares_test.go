package service_test

import (
	"database/sql"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForFileShares creates an in-memory SQLite with the file_shares table.
func setupTestDBForFileShares(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.Exec(service.FileSharesDDL)
	require.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	return db
}

func TestFileShares_UpsertCreatesNewToken(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	token, created, err := service.UpsertFileShare("/tmp/a.md", "a.md", "/tmp")
	require.NoError(t, err)
	assert.True(t, created)
	assert.Len(t, token, 32)

	path, name, _, ok, err := service.GetFileShareByToken(token)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "/tmp/a.md", path)
	assert.Equal(t, "a.md", name)
}

func TestFileShares_UpsertRotatesToken(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	token1, created, err := service.UpsertFileShare("/tmp/a.md", "a.md", "/tmp")
	require.NoError(t, err)
	assert.True(t, created)

	token2, created, err := service.UpsertFileShare("/tmp/a.md", "a.md", "/tmp")
	require.NoError(t, err)
	assert.False(t, created)
	assert.NotEqual(t, token1, token2, "rotating must issue a fresh token")

	// Old token revoked, new token live.
	_, _, _, ok, err := service.GetFileShareByToken(token1)
	require.NoError(t, err)
	assert.False(t, ok, "old token must be invalid after rotation")
	_, _, _, ok, err = service.GetFileShareByToken(token2)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestFileShares_GetFileShareByPath(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	// No share yet.
	_, _, ok, err := service.GetFileShareByPath("/tmp/none.md")
	require.NoError(t, err)
	assert.False(t, ok)

	token, _, err := service.UpsertFileShare("/tmp/b.md", "b.md", "/tmp")
	require.NoError(t, err)

	gotToken, name, ok, err := service.GetFileShareByPath("/tmp/b.md")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, token, gotToken)
	assert.Equal(t, "b.md", name)
}

func TestFileShares_GetByTokenUnknownReturnsFalse(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	_, _, _, ok, err := service.GetFileShareByToken("deadbeefdeadbeefdeadbeefdeadbeef")
	require.NoError(t, err)
	assert.False(t, ok)
	// Empty token also resolves false without error.
	_, _, _, ok, err = service.GetFileShareByToken("")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestFileShares_DeleteByToken(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	token, _, err := service.UpsertFileShare("/tmp/c.md", "c.md", "/tmp")
	require.NoError(t, err)

	require.NoError(t, service.DeleteFileShareByToken(token))
	_, _, _, ok, err := service.GetFileShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestFileShares_DeleteByPath(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	token, _, err := service.UpsertFileShare("/tmp/d.md", "d.md", "/tmp")
	require.NoError(t, err)

	require.NoError(t, service.DeleteFileShareByPath("/tmp/d.md"))
	_, _, _, ok, err := service.GetFileShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestFileShares_DeleteSharesUnderPath(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	// Shares at the dir itself, directly under it, and nested.
	_, _, err := service.UpsertFileShare("/tmp/docs", "docs", "/tmp")
	require.NoError(t, err)
	_, _, err = service.UpsertFileShare("/tmp/docs/readme.md", "readme.md", "/tmp")
	require.NoError(t, err)
	_, _, err = service.UpsertFileShare("/tmp/docs/sub/deep.txt", "deep.txt", "/tmp")
	require.NoError(t, err)
	// Unrelated path sharing a prefix must survive.
	unrelated, _, err := service.UpsertFileShare("/tmp/docs-other/x.md", "x.md", "/tmp")
	require.NoError(t, err)

	require.NoError(t, service.DeleteFileSharesUnderPath("/tmp/docs"))

	for _, p := range []string{"/tmp/docs", "/tmp/docs/readme.md", "/tmp/docs/sub/deep.txt"} {
		_, _, ok, err := service.GetFileShareByPath(p)
		require.NoError(t, err)
		assert.False(t, ok, "share for %s should be removed", p)
	}
	_, _, _, ok, err := service.GetFileShareByToken(unrelated)
	require.NoError(t, err)
	assert.True(t, ok, "unrelated prefix share must survive")
}

func TestFileShares_DeleteSharesUnderPath_EscapesLikeWildcards(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	// A directory containing SQL LIKE wildcards — must still be literal.
	dir := "/tmp/doc_100%/readme.md"
	token, _, err := service.UpsertFileShare(dir, "readme.md", "/tmp")
	require.NoError(t, err)

	// A share under a directory that would match if wildcards were not escaped.
	_, _, err = service.UpsertFileShare("/tmp/docX100Xreadme.md", "readme.md", "/tmp")
	require.NoError(t, err)

	require.NoError(t, service.DeleteFileSharesUnderPath("/tmp/doc_100%"))

	_, _, _, ok, err := service.GetFileShareByToken(token)
	require.NoError(t, err)
	assert.False(t, ok, "literal-prefix share should be removed")

	// Count remaining — only the non-matching row.
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM file_shares").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestFileShares_DeleteSharesUnderPath_WindowsBackslashChildren guards against
// a cross-platform regression: the child-prefix LIKE pattern must be built from
// the real path separator. A hardcoded '/' separator (with ESCAPE '\') fails to
// revoke shares under backslash-separated (Windows) paths, which surfaced as a
// CI failure in TestShareCleanup_OnDirectoryDelete on windows-latest.
func TestFileShares_DeleteSharesUnderPath_WindowsBackslashChildren(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	dir := `C:\repo\clean\docs`
	childFile := dir + `\a.md`
	nestedFile := dir + `\sub\b.md`

	childTok, _, err := service.UpsertFileShare(childFile, "a.md", "/tmp")
	require.NoError(t, err)
	nestedTok, _, err := service.UpsertFileShare(nestedFile, "b.md", "/tmp")
	require.NoError(t, err)
	// Sibling sharing a prefix (docs-other) must survive.
	otherTok, _, err := service.UpsertFileShare(dir+`-other\x.md`, "x.md", "/tmp")
	require.NoError(t, err)

	require.NoError(t, service.DeleteFileSharesUnderPath(dir))

	_, _, _, ok, err := service.GetFileShareByToken(childTok)
	require.NoError(t, err)
	assert.False(t, ok, "direct child share must be revoked")
	_, _, _, ok, err = service.GetFileShareByToken(nestedTok)
	require.NoError(t, err)
	assert.False(t, ok, "nested child share must be revoked")
	_, _, _, ok, err = service.GetFileShareByToken(otherTok)
	require.NoError(t, err)
	assert.True(t, ok, "prefix sibling share must survive")
}

func TestFileShares_DeleteByPaths(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	_, _, err := service.UpsertFileShare("/tmp/e1.md", "e1.md", "/tmp")
	require.NoError(t, err)
	_, _, err = service.UpsertFileShare("/tmp/e2.md", "e2.md", "/tmp")
	require.NoError(t, err)
	keepToken, _, err := service.UpsertFileShare("/tmp/keep.md", "keep.md", "/tmp")
	require.NoError(t, err)

	require.NoError(t, service.DeleteFileShareByPaths([]string{"/tmp/e1.md", "/tmp/e2.md", ""}))

	_, _, ok, err := service.GetFileShareByPath("/tmp/e1.md")
	require.NoError(t, err)
	assert.False(t, ok)
	_, _, ok, err = service.GetFileShareByPath("/tmp/e2.md")
	require.NoError(t, err)
	assert.False(t, ok)
	_, _, _, ok, err = service.GetFileShareByToken(keepToken)
	require.NoError(t, err)
	assert.True(t, ok)

	// Empty input is a no-op.
	require.NoError(t, service.DeleteFileShareByPaths(nil))
}

// ─── DB error branches ───────────────────────────────────────────────────────

// closedSQLite returns an already-closed in-memory SQLite handle.
func closedSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	return db
}

func TestFileShares_Upsert_GetByPathReadError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	// Healthy write handle, closed read handle → GetFileShareByPath errors first.
	cleanup := service.SetDBForTest(db, closedSQLite(t))
	defer cleanup()

	_, _, err := service.UpsertFileShare("/tmp/err.md", "err.md", "/tmp")
	assert.Error(t, err, "read failure in UpsertFileShare must surface")
}

func TestFileShares_Upsert_WriteErrorOnInsert(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	// Healthy read handle (so GetFileShareByPath succeeds and reports no
	// existing share), closed write handle → INSERT fails.
	cleanup := service.SetDBForTest(closedSQLite(t), db)
	defer cleanup()

	_, _, err := service.UpsertFileShare("/tmp/err2.md", "err2.md", "/tmp")
	assert.Error(t, err, "write failure in UpsertFileShare must surface")
}

func TestFileShares_Upsert_RotateWriteError(t *testing.T) {
	db := setupTestDBForFileShares(t)

	// Pre-create an existing share so the rotate path (DELETE then INSERT) runs.
	token, created, err := service.UpsertFileShare("/tmp/rot.md", "rot.md", "/tmp")
	require.NoError(t, err)
	require.True(t, created)
	require.NotEmpty(t, token)

	// Now break the write handle → the DELETE (rotate) fails.
	cleanup := service.SetDBForTest(closedSQLite(t), db)
	defer cleanup()

	_, _, err = service.UpsertFileShare("/tmp/rot.md", "rot.md", "/tmp")
	assert.Error(t, err, "rotate DELETE failure must surface")
}

func TestFileShares_GetByToken_ScanError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	// Drop the name column so a SELECT ... name scan fails on a live row.
	_, err := service.UnsafeDBForTest().Exec("ALTER TABLE file_shares DROP COLUMN name")
	require.NoError(t, err)
	// Insert a row directly (no name column).
	_, err = db.Exec("INSERT INTO file_shares (token, path) VALUES (?, ?)", "tok1", "/tmp/t.md")
	require.NoError(t, err)

	_, _, _, ok, err := service.GetFileShareByToken("tok1")
	assert.False(t, ok)
	assert.Error(t, err, "scan failure must surface as an error")
}

func TestFileShares_GetByToken_SQLQueryError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(db, closedSQLite(t))
	defer cleanup()

	_, _, _, _, err := service.GetFileShareByToken("abc")
	assert.Error(t, err, "query on a closed read DB must error")
}

func TestFileShares_GetByPath_SQLQueryError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(db, closedSQLite(t))
	defer cleanup()

	_, _, _, err := service.GetFileShareByPath("/tmp/x.md")
	assert.Error(t, err, "query on a closed read DB must error")
}

func TestFileShares_DeleteByToken_WriteError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(closedSQLite(t), db)
	defer cleanup()

	assert.Error(t, service.DeleteFileShareByToken("abc"))
}

func TestFileShares_DeleteByPath_WriteError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(closedSQLite(t), db)
	defer cleanup()

	assert.Error(t, service.DeleteFileShareByPath("/tmp/x.md"))
}

func TestFileShares_DeleteUnderPath_WriteError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(closedSQLite(t), db)
	defer cleanup()

	assert.Error(t, service.DeleteFileSharesUnderPath("/tmp/docs"))
}

func TestFileShares_DeleteByPaths_WriteError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(closedSQLite(t), db)
	defer cleanup()

	assert.Error(t, service.DeleteFileShareByPaths([]string{"/tmp/a.md"}))
}

func TestFileShares_DeleteAll_WriteError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(closedSQLite(t), db)
	defer cleanup()

	assert.Error(t, service.DeleteAllFileShares())
}

func TestFileShares_List_QueryError(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	cleanup := service.SetDBForTest(db, closedSQLite(t))
	defer cleanup()

	_, err := service.ListFileShares()
	assert.Error(t, err, "list on a closed read DB must error")
}

func TestGenerateShareToken_IsUniqueAndHex(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	seen := map[string]bool{}
	for range 200 {
		token, err := service.GenerateShareToken()
		require.NoError(t, err)
		assert.Len(t, token, 32)
		seen[token] = true
	}
	assert.Len(t, seen, 200, "tokens must not collide")
}

func TestListFileShares_Empty(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	shares, err := service.ListFileShares()
	require.NoError(t, err)
	require.NotNil(t, shares, "empty list must be a non-nil slice so JSON encodes as []")
	assert.Empty(t, shares)
}

func TestListFileShares_ReturnsAllNewestFirst(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	_, _, err := service.UpsertFileShare("/tmp/l1.md", "l1.md", "/tmp")
	require.NoError(t, err)
	_, _, err = service.UpsertFileShare("/tmp/l2.md", "l2.md", "/tmp")
	require.NoError(t, err)

	shares, err := service.ListFileShares()
	require.NoError(t, err)
	require.Len(t, shares, 2)

	// Newest inserted first (rowid DESC proxy — created_at has only 1s precision).
	assert.Equal(t, "/tmp/l2.md", shares[0].Path)
	assert.Equal(t, "l2.md", shares[0].Name)
	assert.Equal(t, "/tmp/l1.md", shares[1].Path)
	assert.NotEmpty(t, shares[0].Token)
	assert.NotEmpty(t, shares[0].CreatedAt)

	// Token/name/createdAt all populated.
	assert.Len(t, shares[0].Token, 32)
}

func TestDeleteAllFileShares_RemovesEveryShare(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	_, _, err := service.UpsertFileShare("/tmp/a.md", "a.md", "/tmp")
	require.NoError(t, err)
	_, _, err = service.UpsertFileShare("/tmp/b.md", "b.md", "/tmp")
	require.NoError(t, err)

	require.NoError(t, service.DeleteAllFileShares())

	shares, err := service.ListFileShares()
	require.NoError(t, err)
	assert.Empty(t, shares)

	// Double delete is a no-op, not an error.
	require.NoError(t, service.DeleteAllFileShares())
}

// ─── root confinement column ─────────────────────────────────────────────────

func TestFileShares_UpsertPersistsRoot(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	token, _, err := service.UpsertFileShare("/proj/docs/a.md", "a.md", "/proj")
	require.NoError(t, err)

	path, name, root, ok, err := service.GetFileShareByToken(token)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "/proj/docs/a.md", path)
	assert.Equal(t, "a.md", name)
	assert.Equal(t, "/proj", root)
}

func TestFileShares_RotateUpdatesRoot(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	tok1, _, err := service.UpsertFileShare("/proj/docs/a.md", "a.md", "/proj")
	require.NoError(t, err)

	// Re-sharing the same path rotates the token AND refreshes the boundary:
	// the project may have been re-selected since the first share.
	tok2, created, err := service.UpsertFileShare("/proj/docs/a.md", "a.md", "/proj/sub")
	require.NoError(t, err)
	assert.False(t, created)
	assert.NotEqual(t, tok1, tok2)

	_, _, root, ok, err := service.GetFileShareByToken(tok2)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "/proj/sub", root)
}

func TestFileShares_RootDefaultsEmptyForLegacyRows(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	// Simulate a row written before the root column existed.
	_, err := service.WriteExec(
		"INSERT INTO file_shares (token, path, name) VALUES (?, ?, ?)",
		"legacytoken", "/proj/docs/old.md", "old.md")
	require.NoError(t, err)

	_, _, root, ok, err := service.GetFileShareByToken("legacytoken")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Empty(t, root, "legacy rows must read back an empty root so readers fail closed")
}

func TestFileSharesDDL_HasRootColumn(t *testing.T) {
	db := setupTestDBForFileShares(t)
	defer func() { _ = db.Close() }()

	var hasRoot int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('file_shares') WHERE name='root'").Scan(&hasRoot))
	assert.Equal(t, 1, hasRoot, "file_shares must define the root column")
}
