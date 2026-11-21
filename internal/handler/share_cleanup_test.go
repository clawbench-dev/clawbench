package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	plugshare "clawbench/internal/plugin/share"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 从 share 包搬回：这些用例测的是"文件/目录被删除时撤销它的分享链接"，
// 触发点是文件管理接口（ServeFileDelete），分享只是被撤销的一方。

// shareExistsByToken reports whether the share record is still present.
func shareExistsByToken(t *testing.T, token string) bool {
	t.Helper()
	_, _, _, ok, err := service.GetFileShareByToken(token)
	require.NoError(t, err)
	return ok
}

// TestShareCleanup_OnFileDelete revokes a share when its file is deleted.
func TestShareCleanup_OnFileDelete(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "clean/delete.md", "hi")
	token := createShareViaAPI(t, env, absPath)
	require.True(t, shareExistsByToken(t, token))

	req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{"path": absPath})
	w := callHandler(ServeFileDelete, req)
	assertOK(t, w)

	assert.False(t, shareExistsByToken(t, token), "share must be revoked after file delete")
}

// TestShareCleanup_OnDirectoryDelete revokes shares for every file inside.
func TestShareCleanup_OnDirectoryDelete(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	dir := filepath.Join(env.ProjectDir, "clean", "docs")
	createTestFile(t, env.ProjectDir, "clean/docs/a.md", "a")
	createTestFile(t, env.ProjectDir, "clean/docs/sub/b.md", "b")

	tokA := createShareViaAPI(t, env, filepath.Join(dir, "a.md"))
	tokB := createShareViaAPI(t, env, filepath.Join(dir, "sub", "b.md"))

	req := newRequest(t, http.MethodPost, "/api/file/delete", map[string]string{"path": dir})
	w := callHandler(ServeFileDelete, req)
	assertOK(t, w)

	assert.False(t, shareExistsByToken(t, tokA))
	assert.False(t, shareExistsByToken(t, tokB))
}

// TestShareCleanup_OnRename revokes a share when its file is renamed.
func TestShareCleanup_OnRename(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "clean/old.md", "hi")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodPost, "/api/file/rename", map[string]string{
		"path": absPath,
		"name": "new.md",
	})
	w := callHandler(ServeFileRename, req)
	assertOK(t, w)

	assert.False(t, shareExistsByToken(t, token), "share must be revoked after rename")
}

// TestShareCleanup_OnMove revokes a share when its file is moved.
func TestShareCleanup_OnMove(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	src := filepath.Join(env.ProjectDir, "clean", "move.md")
	createTestFile(t, env.ProjectDir, "clean/move.md", "hi")
	token := createShareViaAPI(t, env, src)

	dest := filepath.Join(env.ProjectDir, "clean", "dest", "move.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o755))
	req := newRequest(t, http.MethodPost, "/api/file/move", map[string]string{
		"path": src,
		"dest": dest,
	})
	w := callHandler(ServeFileMove, req)
	assertOK(t, w)

	assert.False(t, shareExistsByToken(t, token), "share must be revoked after move")
}

// TestShareCleanup_WriteDoesNotRevoke verifies editing a shared file keeps the link live.
func TestShareCleanup_WriteDoesNotRevoke(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	absPath := createShareTestFile(t, env, "clean/edit.md", "v1")
	token := createShareViaAPI(t, env, absPath)

	req := newRequest(t, http.MethodPost, "/api/file/write", map[string]string{
		"path":    absPath,
		"content": "v2 updated",
	})
	w := callHandler(ServeFileWrite, req)
	assertOK(t, w)

	assert.True(t, shareExistsByToken(t, token), "write must keep share live")

	// Public endpoint now returns the new content.
	req2 := newRequest(t, http.MethodGet, "/api/share/"+token+"/file", nil)
	w2 := callHandler(plugshare.ServeSharePublic, req2)
	assertOK(t, w2)
	assert.Contains(t, w2.Body.String(), "v2 updated")
}

// createShareTestFile 在项目里写一个文件并返回绝对路径。
func createShareTestFile(t *testing.T, env *testEnv, relPath, content string) string {
	t.Helper()
	createTestFile(t, env.ProjectDir, relPath, content)
	return filepath.Join(env.ProjectDir, relPath)
}

// createShareViaAPI 通过分享接口创建一个链接并返回 token。
func createShareViaAPI(t *testing.T, env *testEnv, absPath string) string {
	t.Helper()
	req := newRequest(t, http.MethodPost, "/api/share", map[string]string{"path": absPath})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(plugshare.ServeShareManage, req)
	assertOK(t, w)

	var resp struct {
		Token string `json:"token"`
		Path  string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Token)
	assert.Equal(t, "/share/"+resp.Token, resp.Path)
	return resp.Token
}
