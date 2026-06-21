package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// initGitRepo initializes a real git repo in dir with an initial commit.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(_ string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command git %v failed: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "test@test.com")
	run("git", "config", "user.name", "Test")

	// Create initial file and commit
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "initial commit")

	// Ensure branch is named "main" for test consistency
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	currentBranch := strings.TrimSpace(string(out))
	if currentBranch != "main" {
		run("git", "branch", "-m", currentBranch, "main")
	}
}

// gitCommitAll stages all changes and commits with the given message.
func gitCommitAll(t *testing.T, dir, message string) {
	t.Helper()
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "add", ".")
	run("git", "commit", "-m", message)
}

// getHeadSHA returns the SHA of the HEAD commit.
func getHeadSHA(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get HEAD SHA: %v", err)
	}
	return string(out[:len(out)-1]) // trim newline
}

// --- ServeGitStatus ---

func TestServeGitStatus_UncommittedChanges(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Modify a file to create uncommitted changes
	createTestFile(t, env.ProjectDir, "README.md", "# Modified")

	req := newRequest(t, http.MethodGet, "/api/git/status?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitStatus, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	assert.Equal(t, true, resp["hasUncommitted"])
}

func TestServeGitStatus_NoChanges(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/status?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitStatus, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	assert.Equal(t, false, resp["hasUncommitted"])
}

func TestServeGitStatus_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/status?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitStatus, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["isGit"])
	assert.Equal(t, false, resp["hasUncommitted"])
}

func TestServeGitStatus_MissingPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/status", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitStatus, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// --- ServeGitHistory ---

func TestServeGitHistory_WithCommits(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Make another commit on the file
	createTestFile(t, env.ProjectDir, "README.md", "# Updated")
	gitCommitAll(t, env.ProjectDir, "update readme")

	req := newRequest(t, http.MethodGet, "/api/git/history?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitHistory, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	commits, ok := resp["commits"].([]interface{})
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(commits), 2)
}

func TestServeGitHistory_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/history?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitHistory, req)
	assertStatus(t, w, http.StatusOK)

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["isGit"] != false {
		t.Errorf("expected isGit=false, got %v", body["isGit"])
	}
}

func TestServeGitHistory_MissingPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/history", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitHistory, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitHistory_PathTraversal(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/history?path=../../../etc/passwd", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitHistory, req)
	assertStatus(t, w, http.StatusForbidden)
}

// --- ServeGitDiff ---

func TestServeGitDiff_UncommittedChanges(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Modify file without committing
	createTestFile(t, env.ProjectDir, "README.md", "# Modified content")

	req := newRequest(t, http.MethodGet, "/api/git/diff?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitDiff, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["empty"])
	assert.Contains(t, resp["diff"], "# Modified content")
}

func TestServeGitDiff_NoChanges(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/diff?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitDiff, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["empty"])
}

func TestServeGitDiff_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/diff?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitDiff, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitDiff_SpecificCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Make a second commit
	createTestFile(t, env.ProjectDir, "README.md", "# Updated")
	gitCommitAll(t, env.ProjectDir, "update readme")
	sha := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/diff?path=README.md&commit="+sha, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitDiff, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["empty"])
	assert.Contains(t, resp["diff"], "# Updated")
}

// --- ServeGitProjectHistory ---

func TestServeGitProjectHistory_WithCommits(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/project-history", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitProjectHistory, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	commits, ok := resp["commits"].([]interface{})
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(commits), 1)
}

func TestServeGitProjectHistory_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/project-history", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitProjectHistory, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["isGit"])
	commits, ok := resp["commits"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 0, len(commits))
	assert.Equal(t, false, resp["hasMore"])
}

func TestServeGitProjectHistory_Pagination(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create multiple commits
	for i := range 5 {
		createTestFile(t, env.ProjectDir, fmt.Sprintf("file%d.txt", i), "content")
		gitCommitAll(t, env.ProjectDir, fmt.Sprintf("add file%d", i))
	}

	// Without skip
	req := newRequest(t, http.MethodGet, "/api/git/project-history", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitProjectHistory, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	commits, _ := resp["commits"].([]interface{})
	totalCommits := len(commits)
	assert.GreaterOrEqual(t, totalCommits, 6) // initial + 5

	// With skip=1
	req2 := newRequest(t, http.MethodGet, "/api/git/project-history?skip=1", nil)
	withProjectCookie(req2, env.ProjectDir)

	w2 := callHandler(ServeGitProjectHistory, req2)
	assertOK(t, w2)

	var resp2 map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
	commits2, _ := resp2["commits"].([]interface{})
	assert.Equal(t, totalCommits-1, len(commits2))
}

// --- ServeGitCommitFiles ---

func TestServeGitCommitFiles_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Make a commit that adds a new file
	createTestFile(t, env.ProjectDir, "newfile.txt", "hello")
	gitCommitAll(t, env.ProjectDir, "add newfile")
	sha := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/commit-files?sha="+sha, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCommitFiles, req)
	assertOK(t, w)

	var files []map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &files)
	assert.GreaterOrEqual(t, len(files), 1)

	// Check that newfile.txt is in the list
	found := false
	for _, f := range files {
		if f["path"] == "newfile.txt" {
			found = true
			assert.Equal(t, "A", f["type"])
			break
		}
	}
	assert.True(t, found, "expected newfile.txt in commit files")
}

func TestServeGitCommitFiles_MissingSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/commit-files", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCommitFiles, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitCommitFiles_MergeCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	// Create a branch, add a file, commit
	run("git", "checkout", "-b", "feature-branch")
	createTestFile(t, env.ProjectDir, "feature.txt", "feature work")
	gitCommitAll(t, env.ProjectDir, "add feature.txt")

	// Switch back to main, add a different file, commit
	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "main.txt", "main work")
	gitCommitAll(t, env.ProjectDir, "add main.txt")

	// Merge feature-branch into main
	run("git", "merge", "feature-branch", "-m", "Merge branch 'feature-branch' into main")
	mergeSHA := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/commit-files?sha="+mergeSHA, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCommitFiles, req)
	assertOK(t, w)

	var result map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &result)

	// Verify it's a merge response
	assert.Equal(t, true, result["merge"])

	// Verify groups exist
	groups, ok := result["groups"].([]interface{})
	assert.True(t, ok, "expected groups array")
	assert.GreaterOrEqual(t, len(groups), 2, "expected at least 2 groups for a merge commit")

	// Verify each group has label and files
	for _, g := range groups {
		group, _ := g.(map[string]interface{})
		assert.NotEmpty(t, group["label"])
		files, ok := group["files"].([]interface{})
		assert.True(t, ok, "expected files array in group")
		assert.Greater(t, len(files), 0, "each group should have at least one file")
	}

	// Verify no duplicate files across groups (dedup works)
	allPaths := map[string]bool{}
	for _, g := range groups {
		group, _ := g.(map[string]interface{})
		for _, f := range group["files"].([]interface{}) {
			file, _ := f.(map[string]interface{})
			path, _ := file["path"].(string)
			assert.False(t, allPaths[path], "duplicate file path: %s", path)
			allPaths[path] = true
		}
	}
}

// --- ServeGitFileDiff ---

func TestServeGitFileDiff_SpecificCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Make a second commit modifying README
	createTestFile(t, env.ProjectDir, "README.md", "# Changed")
	gitCommitAll(t, env.ProjectDir, "change readme")
	sha := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/file-diff?sha="+sha+"&path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitFileDiff, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["empty"])
	assert.Contains(t, resp["diff"], "# Changed")
}

func TestServeGitFileDiff_HeadDiff(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Modify without committing
	createTestFile(t, env.ProjectDir, "README.md", "# Working tree change")

	req := newRequest(t, http.MethodGet, "/api/git/file-diff?sha=HEAD&path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitFileDiff, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["empty"])
	assert.Contains(t, resp["diff"], "# Working tree change")
}

func TestServeGitFileDiff_MissingSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/file-diff?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitFileDiff, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitFileDiff_MissingPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	sha := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/file-diff?sha="+sha, nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitFileDiff, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// --- ServeGitWorkingTreeFiles ---

func TestServeGitWorkingTreeFiles_UncommittedFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Modify tracked file and add a new file
	createTestFile(t, env.ProjectDir, "README.md", "# Changed")
	createTestFile(t, env.ProjectDir, "new.txt", "new file")

	req := newRequest(t, http.MethodGet, "/api/git/working-tree-files", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorkingTreeFiles, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	assert.Equal(t, true, resp["hasUncommitted"])
}

func TestServeGitWorkingTreeFiles_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/working-tree-files", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorkingTreeFiles, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["isGit"])
	assert.Equal(t, false, resp["hasUncommitted"])
}

func TestServeGitWorkingTreeFiles_SpecificFileCheck(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Modify a tracked file
	createTestFile(t, env.ProjectDir, "README.md", "# Changed")

	req := newRequest(t, http.MethodGet, "/api/git/working-tree-files?path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorkingTreeFiles, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	assert.Equal(t, true, resp["hasUncommitted"])
}

func TestServeGitWorkingTreeFiles_NoChanges(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/working-tree-files", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorkingTreeFiles, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	assert.Equal(t, false, resp["hasUncommitted"])
}

// --- ServeGitVerifyCommits ---

func TestServeGitVerifyCommits_SingleCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	sha := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]interface{}{
		"shas": []string{sha},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)
	info, ok := results[sha].(map[string]interface{})
	assert.True(t, ok, "SHA should be a valid commit")
	assert.Equal(t, sha, info["sha"])
	assert.Equal(t, "initial commit", info["msg"])
}

func TestServeGitVerifyCommits_MultipleCommits(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	sha1 := getHeadSHA(t, env.ProjectDir)

	createTestFile(t, env.ProjectDir, "newfile.txt", "hello")
	gitCommitAll(t, env.ProjectDir, "add newfile")
	sha2 := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]interface{}{
		"shas": []string{sha1, sha2},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, _ := resp["results"].(map[string]interface{})
	assert.NotNil(t, results[sha1])
	assert.NotNil(t, results[sha2])
}

func TestServeGitVerifyCommits_MixedValidInvalid(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	sha := getHeadSHA(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]interface{}{
		"shas": []string{sha, "0000000000000000000000000000000000000000"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, _ := resp["results"].(map[string]interface{})
	assert.NotNil(t, results[sha])
	assert.Nil(t, results["0000000000000000000000000000000000000000"])
}

func TestServeGitVerifyCommits_AbbreviatedSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)
	sha := getHeadSHA(t, env.ProjectDir)
	abbrevSHA := sha[:7] // Frontend may send abbreviated SHAs

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]interface{}{
		"shas": []string{abbrevSHA},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, _ := resp["results"].(map[string]interface{})
	// Result should be keyed by the requested abbreviated SHA, not the full SHA
	info, ok := results[abbrevSHA].(map[string]interface{})
	assert.True(t, ok, "abbreviated SHA should resolve to a valid commit")
	assert.Equal(t, sha, info["sha"])
}

// --- validateFilePath ---

func TestValidateFilePath_ValidRelative(t *testing.T) {
	dir := t.TempDir()
	absPath, ok := model.ValidatePath(dir, "subdir/file.txt")
	assert.True(t, ok)
	expected := filepath.Join(dir, "subdir", "file.txt")
	assert.Equal(t, expected, absPath)
}

func TestValidateFilePath_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	absPath, ok := model.ValidatePath(dir, "../../../etc/passwd")
	assert.False(t, ok)
	// absPath may be non-empty (the resolved path), but ok must be false
	_ = absPath
}

func TestValidateFilePath_SimpleFile(t *testing.T) {
	dir := t.TempDir()
	absPath, ok := model.ValidatePath(dir, "README.md")
	assert.True(t, ok)
	assert.Equal(t, filepath.Join(dir, "README.md"), absPath)
}

// --- parseGitLog & parseDecorateRefs ---

func TestParseGitLog_WithParents(t *testing.T) {
	output := "abc123|parent1 parent2|merge commit|2026-01-01|Test\nsha456||initial commit|2026-01-02|Test"
	commits := parseGitLog(output)
	assert.Len(t, commits, 2)
	assert.Equal(t, []string{"parent1", "parent2"}, commits[0].Parents)
	assert.Equal(t, "merge commit", commits[0].Msg)
	assert.Empty(t, commits[1].Parents)
	assert.Equal(t, "initial commit", commits[1].Msg)
}

func TestParseGitLog_WithRefs(t *testing.T) {
	output := "abc123||initial commit|2026-01-01|Test (HEAD -> main, tag: v1.0)"
	commits := parseGitLog(output)
	assert.Len(t, commits, 1)
	assert.Equal(t, "Test", commits[0].Author)
	assert.Contains(t, commits[0].Refs, "HEAD")
	assert.Contains(t, commits[0].Refs, "main")
	assert.Contains(t, commits[0].Refs, "tag: v1.0")
}

func TestParseGitLog_NoRefs(t *testing.T) {
	output := "abc123||some commit|2026-01-01|Test"
	commits := parseGitLog(output)
	assert.Len(t, commits, 1)
	assert.Equal(t, "Test", commits[0].Author)
	assert.Empty(t, commits[0].Refs)
}

func TestParseDecorateRefs_RemoteExcluded(t *testing.T) {
	// In production, --decorate-refs-exclude=refs/remotes already strips remote refs,
	// so they won't appear in the decoration string. Local branches with "/" are kept.
	refs := parseDecorateRefs(" (HEAD -> main, feature-x)")
	assert.Contains(t, refs, "HEAD")
	assert.Contains(t, refs, "main")
	assert.Contains(t, refs, "feature-x")
}

func TestParseDecorateRefs_LocalBranchWithSlash(t *testing.T) {
	// Local branches with slashes (e.g., feature/login) should be preserved
	refs := parseDecorateRefs(" (HEAD -> main, feature/login)")
	assert.Contains(t, refs, "HEAD")
	assert.Contains(t, refs, "main")
	assert.Contains(t, refs, "feature/login")
}

func TestParseDecorateRefs_Tag(t *testing.T) {
	refs := parseDecorateRefs(" (tag: v1.0)")
	assert.Len(t, refs, 1)
	assert.Equal(t, "tag: v1.0", refs[0])
}

func TestParseFileCountFromShortstat(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{" 2 files changed, 10 insertions(+), 5 deletions(-)", 2},
		{" 1 file changed, 3 insertions(+)", 1},
		{" 15 files changed, 200 insertions(+), 50 deletions(-)", 15},
		{" 1 file changed, 2 deletions(-)", 1},
		{"", 0},
		{"some unrelated text", 0},
	}
	for _, tt := range tests {
		got := parseFileCountFromShortstat(tt.input)
		assert.Equal(t, tt.want, got, "parseFileCountFromShortstat(%q)", tt.input)
	}
}

func TestParseGitLogWithStats(t *testing.T) {
	// Simulate actual git log output with null byte separator and shortstat
	output := "abc123|parent1|fix bug|2026-01-01|Author (HEAD -> main)\x00\n\n 2 files changed, 10 insertions(+), 5 deletions(-)\nsha456||initial commit|2026-01-02|Author\x00\n\n 1 file changed, 3 insertions(+)\n"
	commits := parseGitLogWithStats(output)
	assert.Len(t, commits, 2)
	assert.Equal(t, 2, commits[0].FileCount)
	assert.Equal(t, "fix bug", commits[0].Msg)
	assert.Equal(t, 1, commits[1].FileCount)
	assert.Equal(t, "initial commit", commits[1].Msg)
}

func TestParseGitLogWithStats_NoShortstat(t *testing.T) {
	// When shortstat is missing (e.g. merge commit with no changes), fileCount should be 0
	output := "abc123|parent1 parent2|merge commit|2026-01-01|Author\x00\n"
	commits := parseGitLogWithStats(output)
	assert.Len(t, commits, 1)
	assert.Equal(t, 0, commits[0].FileCount)
	assert.Equal(t, "merge commit", commits[0].Msg)
}

func TestServeGitProjectHistory_IncludesParents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Make a second commit to have a parent
	createTestFile(t, env.ProjectDir, "README.md", "# Updated")
	gitCommitAll(t, env.ProjectDir, "update readme")

	req := newRequest(t, http.MethodGet, "/api/git/project-history", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitProjectHistory, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	commits, ok := resp["commits"].([]interface{})
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(commits), 2)

	// First commit (most recent) should have parents
	first, _ := commits[0].(map[string]interface{})
	parents, ok := first["parents"].([]interface{})
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(parents), 1)

	// First commit should have fileCount > 0 (it changed README.md)
	fc, ok := first["fileCount"].(float64)
	assert.True(t, ok)
	assert.GreaterOrEqual(t, int(fc), 1)
}

// --- parseWorktreePorcelain ---

func TestParseWorktreePorcelain_SingleWorktree(t *testing.T) {
	output := `worktree /home/user/project
HEAD abc123def456
branch refs/heads/main
`
	trees := parseWorktreePorcelain(output, "/home/user/project")
	assert.Len(t, trees, 1)
	assert.Equal(t, "/home/user/project", trees[0].Path)
	assert.Equal(t, ".", trees[0].DisplayPath)
	assert.Equal(t, "main", trees[0].Branch)
	assert.True(t, trees[0].IsCurrent)
	assert.False(t, trees[0].Locked)
}

func TestParseWorktreePorcelain_MultipleWorktrees(t *testing.T) {
	output := `worktree /home/user/project
HEAD abc123def456
branch refs/heads/main

worktree /home/user/project/.worktrees/feature-x
HEAD def789abc012
branch refs/heads/feature-x
`
	trees := parseWorktreePorcelain(output, "/home/user/project")
	assert.Len(t, trees, 2)

	// Main worktree
	assert.Equal(t, "/home/user/project", trees[0].Path)
	assert.Equal(t, ".", trees[0].DisplayPath)
	assert.Equal(t, "main", trees[0].Branch)
	assert.True(t, trees[0].IsCurrent)

	// Linked worktree
	assert.Equal(t, "/home/user/project/.worktrees/feature-x", trees[1].Path)
	assert.Equal(t, "./.worktrees/feature-x", trees[1].DisplayPath)
	assert.Equal(t, "feature-x", trees[1].Branch)
	assert.False(t, trees[1].IsCurrent)
}

func TestParseWorktreePorcelain_LockedWorktree(t *testing.T) {
	output := `worktree /home/user/project
HEAD abc123def456
branch refs/heads/main

worktree /home/user/project/.worktrees/hotfix
HEAD def789abc012
branch refs/heads/hotfix
locked
`
	trees := parseWorktreePorcelain(output, "/home/user/project")
	assert.Len(t, trees, 2)
	assert.False(t, trees[0].Locked)
	assert.True(t, trees[1].Locked)
}

func TestParseWorktreePorcelain_LockedWithReason(t *testing.T) {
	output := `worktree /home/user/project
HEAD abc123def456
branch refs/heads/main

worktree /home/user/project/.worktrees/hotfix
HEAD def789abc012
branch refs/heads/hotfix
locked reason for locking
`
	trees := parseWorktreePorcelain(output, "/home/user/project")
	assert.Len(t, trees, 2)
	assert.True(t, trees[1].Locked)
}

func TestParseWorktreePorcelain_DetachedHead(t *testing.T) {
	output := `worktree /home/user/project
HEAD abc123def456
branch refs/heads/main

worktree /home/user/project/.worktrees/detached
HEAD def789abc012
detached
`
	trees := parseWorktreePorcelain(output, "/home/user/project")
	assert.Len(t, trees, 2)
	assert.Equal(t, "", trees[1].Branch) // detached has no branch
}

func TestParseWorktreePorcelain_EmptyOutput(t *testing.T) {
	trees := parseWorktreePorcelain("", "/home/user/project")
	assert.Len(t, trees, 0)
}

func TestParseWorktreePorcelain_WorktreeOutsideProjectPath(t *testing.T) {
	output := `worktree /home/user/project
HEAD abc123def456
branch refs/heads/main

worktree /tmp/other-worktree
HEAD def789abc012
branch refs/heads/feature-x
`
	trees := parseWorktreePorcelain(output, "/home/user/project")
	assert.Len(t, trees, 2)
	assert.Equal(t, ".", trees[0].DisplayPath)
	assert.Equal(t, "/tmp/other-worktree", trees[1].DisplayPath) // absolute since outside project
}

// --- ServeGitWorktrees ---

func TestServeGitWorktrees_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/worktrees", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["isGit"])
	worktrees, ok := resp["worktrees"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 0, len(worktrees))
}

func TestServeGitWorktrees_SingleWorktree(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/worktrees", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	worktrees, ok := resp["worktrees"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(worktrees))

	wt, _ := worktrees[0].(map[string]interface{})
	assert.Equal(t, true, wt["isCurrent"])
	// Resolve symlinks before comparing — macOS /var is a symlink to /private/var,
	// so os.Getwd() returns /private/var/... while t.TempDir() returns /var/...
	pathStr, _ := wt["path"].(string)
	actualPath, _ := filepath.EvalSymlinks(pathStr)
	expectedPath, _ := filepath.EvalSymlinks(env.ProjectDir)
	assert.Equal(t, expectedPath, actualPath)
}

func TestServeGitWorktrees_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/git/worktrees", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// --- parseTrackInfo ---

func TestParseTrackInfo_AheadAndBehind(t *testing.T) {
	ahead, behind := parseTrackInfo("[ahead 3, behind 2]")
	assert.Equal(t, 3, ahead)
	assert.Equal(t, 2, behind)
}

func TestParseTrackInfo_AheadOnly(t *testing.T) {
	ahead, behind := parseTrackInfo("[ahead 5]")
	assert.Equal(t, 5, ahead)
	assert.Equal(t, 0, behind)
}

func TestParseTrackInfo_BehindOnly(t *testing.T) {
	ahead, behind := parseTrackInfo("[behind 7]")
	assert.Equal(t, 0, ahead)
	assert.Equal(t, 7, behind)
}

func TestParseTrackInfo_Empty(t *testing.T) {
	ahead, behind := parseTrackInfo("")
	assert.Equal(t, 0, ahead)
	assert.Equal(t, 0, behind)
}

func TestParseTrackInfo_Gone(t *testing.T) {
	// git outputs [gone] when remote branch is deleted
	ahead, behind := parseTrackInfo("[gone]")
	assert.Equal(t, 0, ahead)
	assert.Equal(t, 0, behind)
}

// --- parseBranchForEachRef ---

func TestParseBranchForEachRef_Basic(t *testing.T) {
	output := "main|origin/main|[ahead 1]\nfeature-x||\ndevelop|origin/develop|[ahead 2, behind 3]"
	branches := parseBranchForEachRef(output)
	assert.Len(t, branches, 3)

	assert.Equal(t, "main", branches[0].Name)
	assert.Equal(t, "origin/main", branches[0].RemoteTracking)
	assert.Equal(t, 1, branches[0].Ahead)
	assert.Equal(t, 0, branches[0].Behind)

	assert.Equal(t, "feature-x", branches[1].Name)
	assert.Equal(t, "", branches[1].RemoteTracking)
	assert.Equal(t, 0, branches[1].Ahead)
	assert.Equal(t, 0, branches[1].Behind)

	assert.Equal(t, "develop", branches[2].Name)
	assert.Equal(t, "origin/develop", branches[2].RemoteTracking)
	assert.Equal(t, 2, branches[2].Ahead)
	assert.Equal(t, 3, branches[2].Behind)
}

func TestParseBranchForEachRef_NoTrackInfo(t *testing.T) {
	output := "main|origin/main|\nfeature-x||"
	branches := parseBranchForEachRef(output)
	assert.Len(t, branches, 2)
	assert.Equal(t, "main", branches[0].Name)
	assert.Equal(t, "origin/main", branches[0].RemoteTracking)
	assert.Equal(t, 0, branches[0].Ahead)
	assert.Equal(t, 0, branches[0].Behind)
}

func TestParseBranchForEachRef_Empty(t *testing.T) {
	branches := parseBranchForEachRef("")
	assert.Len(t, branches, 0)
}

func TestParseBranchForEachRef_WhitespaceOnly(t *testing.T) {
	branches := parseBranchForEachRef("  \n  \n")
	assert.Len(t, branches, 0)
}

// --- ServeGitBranches ---

func TestServeGitBranches_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/branches", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranches, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["isGit"])
	assert.Equal(t, "", resp["defaultBranch"])
	assert.Equal(t, "", resp["currentBranch"])
	branches, ok := resp["branches"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 0, len(branches))
}

func TestServeGitBranches_SingleBranch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/branches", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranches, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	branches, ok := resp["branches"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(branches))

	b, _ := branches[0].(map[string]interface{})
	assert.Equal(t, true, b["isCurrent"])
	assert.Equal(t, true, b["isDefault"])
}

func TestServeGitBranches_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/git/branches", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranches, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

// --- ServeGitCheckout ---

func TestServeGitCheckout_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/checkout", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeGitCheckout_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "main",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitCheckout_EmptyBranch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitCheckout_DirtyWorktree(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create and switch to a second branch so we can try to switch back
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "checkout", "-b", "feature-x")
	run("git", "checkout", "main")

	// Make working tree dirty
	createTestFile(t, env.ProjectDir, "dirty.txt", "dirty content")

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "feature-x",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "dirty_worktree", resp["error"])
	assert.NotNil(t, resp["untrackedCount"])
}

func TestServeGitCheckout_DirtyWithStash(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create and switch to a second branch
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "checkout", "-b", "feature-x")
	run("git", "checkout", "main")

	// Make working tree dirty
	createTestFile(t, env.ProjectDir, "dirty.txt", "dirty content")

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "feature-x",
		"stash":  true,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])
	assert.Equal(t, "feature-x", resp["branch"])
	assert.Equal(t, true, resp["stashed"])
}

func TestServeGitCheckout_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create a second branch to switch to
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "checkout", "-b", "feature-y")

	// Switch back to main first
	run("git", "checkout", "main")

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "feature-y",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])
	assert.Equal(t, "feature-y", resp["branch"])
	assert.Equal(t, false, resp["stashed"])

	// Verify we actually switched
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = env.ProjectDir
	out, err := cmd.Output()
	assert.NoError(t, err)
	assert.Equal(t, "feature-y", strings.TrimSpace(string(out)))
}

func TestServeGitCheckout_Conflict(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	// Create diverging branches with conflicting changes
	run("git", "checkout", "-b", "feature-conflict")
	createTestFile(t, env.ProjectDir, "README.md", "conflict content")
	gitCommitAll(t, env.ProjectDir, "conflict change")

	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "README.md", "main content")
	gitCommitAll(t, env.ProjectDir, "main change")

	// Try to switch with force — git switch -f won't resolve merge conflicts,
	// but git switch to a branch that has diverged will fail with local changes.
	// For a cleaner test, use a non-existent branch to get checkout_failed.
	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "nonexistent-branch-xyz",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "checkout_failed", resp["error"])
	assert.NotNil(t, resp["errorDetail"])
}

func TestServeGitCheckout_MutexConcurrency(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Lock the mutex manually to simulate a concurrent checkout
	checkoutMu.Lock()

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "main",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertStatus(t, w, http.StatusConflict)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "checkout_in_progress", resp["error"])

	// Unlock so other tests aren't affected
	checkoutMu.Unlock()
}

func TestServeGitCheckout_ForceFlag(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create a second branch
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "checkout", "-b", "feature-force")
	run("git", "checkout", "main")

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "feature-force",
		"force":  true,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])
	assert.Equal(t, "feature-force", resp["branch"])
}

// --- getCommitParents ---

func TestGetCommitParents_RegularCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Make a second commit so HEAD has a parent
	createTestFile(t, env.ProjectDir, "second.txt", "content")
	gitCommitAll(t, env.ProjectDir, "second commit")
	sha := getHeadSHA(t, env.ProjectDir)

	parents := getCommitParents(env.ProjectDir, sha)
	assert.Len(t, parents, 1, "regular commit should have 1 parent")
}

func TestGetCommitParents_InitialCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	sha := getHeadSHA(t, env.ProjectDir)
	parents := getCommitParents(env.ProjectDir, sha)
	assert.Len(t, parents, 0, "initial commit should have 0 parents")
}

func TestGetCommitParents_MergeCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	run("git", "checkout", "-b", "feature-branch")
	createTestFile(t, env.ProjectDir, "feature.txt", "feature work")
	gitCommitAll(t, env.ProjectDir, "add feature.txt")

	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "main.txt", "main work")
	gitCommitAll(t, env.ProjectDir, "add main.txt")

	run("git", "merge", "feature-branch", "-m", "Merge branch 'feature-branch' into main")
	mergeSHA := getHeadSHA(t, env.ProjectDir)

	parents := getCommitParents(env.ProjectDir, mergeSHA)
	assert.Len(t, parents, 2, "merge commit should have 2 parents")
}

func TestGetCommitParents_InvalidSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	parents := getCommitParents(env.ProjectDir, "0000000000000000000")
	assert.Nil(t, parents, "invalid SHA should return nil")
}

// --- extractMergeLabels ---

func TestExtractMergeLabels_IntoFormat(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	run("git", "checkout", "-b", "feature-xyz")
	createTestFile(t, env.ProjectDir, "feature.txt", "content")
	gitCommitAll(t, env.ProjectDir, "add feature.txt")

	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "main.txt", "main content")
	gitCommitAll(t, env.ProjectDir, "add main.txt")

	run("git", "merge", "feature-xyz", "-m", "Merge branch 'feature-xyz' into main")
	mergeSHA := getHeadSHA(t, env.ProjectDir)
	parents := getCommitParents(env.ProjectDir, mergeSHA)

	labels := extractMergeLabels(env.ProjectDir, mergeSHA, parents)
	assert.Equal(t, "main", labels[0])
	assert.Equal(t, "feature-xyz", labels[1])
}

func TestExtractMergeLabels_NoInto(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	run("git", "checkout", "-b", "my-branch")
	createTestFile(t, env.ProjectDir, "feature.txt", "content")
	gitCommitAll(t, env.ProjectDir, "add feature.txt")

	run("git", "checkout", "main")

	// Use --no-ff to force a merge commit even when fast-forward is possible
	// Default merge message "Merge branch 'my-branch'" doesn't have "into"
	run("git", "merge", "--no-ff", "my-branch")
	mergeSHA := getHeadSHA(t, env.ProjectDir)
	parents := getCommitParents(env.ProjectDir, mergeSHA)

	labels := extractMergeLabels(env.ProjectDir, mergeSHA, parents)
	// Default merge into current branch doesn't have "into" in message
	assert.Equal(t, "my-branch", labels[1], "source branch should be parsed from 'Merge branch X'")
}

func TestExtractMergeLabels_PullRequestFormat(t *testing.T) {
	// Test parsing "Merge pull request #N from user/branch" format
	// This is a unit test of the parsing logic, not a full integration test
	// We test indirectly via a repo with a manually crafted merge commit
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	run("git", "checkout", "-b", "fix/some-bug")
	createTestFile(t, env.ProjectDir, "fix.txt", "fix content")
	gitCommitAll(t, env.ProjectDir, "add fix")

	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "main2.txt", "main content")
	gitCommitAll(t, env.ProjectDir, "add main2")

	// Create merge with PR-style message
	run("git", "merge", "fix/some-bug", "-m", "Merge pull request #42 from user/fix/some-bug")
	mergeSHA := getHeadSHA(t, env.ProjectDir)
	parents := getCommitParents(env.ProjectDir, mergeSHA)

	labels := extractMergeLabels(env.ProjectDir, mergeSHA, parents)
	assert.Equal(t, "some-bug", labels[1], "should extract branch name after last slash from PR merge")
}

func TestExtractMergeLabels_FallbackToShortSHA(t *testing.T) {
	// When the merge message doesn't match known patterns,
	// extractMergeLabels should fall back to short SHAs
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	run("git", "checkout", "-b", "branch-a")
	createTestFile(t, env.ProjectDir, "a.txt", "a")
	gitCommitAll(t, env.ProjectDir, "add a")

	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "b.txt", "b")
	gitCommitAll(t, env.ProjectDir, "add b")

	// Custom merge message that doesn't match any pattern
	run("git", "merge", "branch-a", "-m", "Custom merge message")
	mergeSHA := getHeadSHA(t, env.ProjectDir)
	parents := getCommitParents(env.ProjectDir, mergeSHA)

	labels := extractMergeLabels(env.ProjectDir, mergeSHA, parents)
	// Should fall back to short SHAs since message doesn't match patterns
	assert.NotEmpty(t, labels[0], "should have a fallback label for parent 0")
	assert.NotEmpty(t, labels[1], "should have a fallback label for parent 1")
	// Verify it's a valid short SHA (7+ hex chars)
	assert.Regexp(t, `^[0-9a-f]{7,}$`, labels[0])
	assert.Regexp(t, `^[0-9a-f]{7,}$`, labels[1])
}

// --- fallbackMergeFiles ---

func TestFallbackMergeFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	run("git", "checkout", "-b", "feature-fallback")
	createTestFile(t, env.ProjectDir, "fb.txt", "fallback content")
	gitCommitAll(t, env.ProjectDir, "add fb")

	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "main3.txt", "main3 content")
	gitCommitAll(t, env.ProjectDir, "add main3")

	run("git", "merge", "feature-fallback", "-m", "Merge for fallback test")
	mergeSHA := getHeadSHA(t, env.ProjectDir)

	result := fallbackMergeFiles(env.ProjectDir, mergeSHA)
	assert.Equal(t, true, result["merge"])

	groups, ok := result["groups"].([]mergeFileGroup)
	assert.True(t, ok, "expected groups to be []mergeFileGroup")
	assert.GreaterOrEqual(t, len(groups), 1, "should have at least one group")

	// The "all changes" group should have files
	allChangesGroup := groups[0]
	assert.Equal(t, "all changes", allChangesGroup.Label)
	assert.Greater(t, len(allChangesGroup.Files), 0, "all changes group should have files")
}

func TestFallbackMergeFiles_InvalidSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	result := fallbackMergeFiles(env.ProjectDir, "invalidsha")
	assert.Equal(t, true, result["merge"])

	groups, ok := result["groups"].([]mergeFileGroup)
	assert.True(t, ok)
	// With invalid SHA, diff-tree will fail so files will be empty
	assert.Equal(t, 1, len(groups))
	assert.Equal(t, 0, len(groups[0].Files))
}

// --- buildMergeFileGroups ---

func TestBuildMergeFileGroups_DeduplicatesFiles(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	// Create a branch that adds a unique file (no conflicts)
	run("git", "checkout", "-b", "dedup-branch")
	createTestFile(t, env.ProjectDir, "branch-file.txt", "branch content")
	gitCommitAll(t, env.ProjectDir, "add branch-file")

	run("git", "checkout", "main")
	// Add a different file on main
	createTestFile(t, env.ProjectDir, "main-file.txt", "main content")
	gitCommitAll(t, env.ProjectDir, "add main-file")

	run("git", "merge", "dedup-branch", "-m", "Merge branch 'dedup-branch' into main")
	mergeSHA := getHeadSHA(t, env.ProjectDir)
	parents := getCommitParents(env.ProjectDir, mergeSHA)

	result := buildMergeFileGroups(env.ProjectDir, mergeSHA, parents)
	assert.Equal(t, true, result["merge"])

	groups, ok := result["groups"].([]mergeFileGroup)
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(groups), 2, "should have at least 2 groups")

	// Verify no duplicate paths across groups
	allPaths := map[string]bool{}
	for _, g := range groups {
		for _, f := range g.Files {
			assert.False(t, allPaths[f.Path], "duplicate path %s in merge file groups", f.Path)
			allPaths[f.Path] = true
		}
	}
}

func TestBuildMergeFileGroups_LabelsFromMergeMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	initGitRepo(t, env.ProjectDir)

	run("git", "checkout", "-b", "label-branch")
	createTestFile(t, env.ProjectDir, "label.txt", "label content")
	gitCommitAll(t, env.ProjectDir, "add label.txt")

	run("git", "checkout", "main")
	createTestFile(t, env.ProjectDir, "label-main.txt", "main content")
	gitCommitAll(t, env.ProjectDir, "add label-main.txt")

	run("git", "merge", "label-branch", "-m", "Merge branch 'label-branch' into main")
	mergeSHA := getHeadSHA(t, env.ProjectDir)
	parents := getCommitParents(env.ProjectDir, mergeSHA)

	result := buildMergeFileGroups(env.ProjectDir, mergeSHA, parents)
	groups, ok := result["groups"].([]mergeFileGroup)
	assert.True(t, ok)
	assert.GreaterOrEqual(t, len(groups), 2)

	// First group should be labeled "main" (destination)
	assert.Equal(t, "main", groups[0].Label)
	// Second group should be labeled "label-branch" (source)
	assert.Equal(t, "label-branch", groups[1].Label)
}

// --- ServeGitVerifyCommits edge cases ---

func TestServeGitVerifyCommits_NonCommitObject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create a tree object (not a commit)
	cmd := exec.Command("git", "mktree")
	cmd.Dir = env.ProjectDir
	cmd.Stdin = strings.NewReader("")
	treeOut, treeErr := cmd.Output()
	if treeErr != nil {
		t.Fatalf("failed to create tree object: %v", treeErr)
	}
	treeSHA := strings.TrimSpace(string(treeOut))

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]any{
		"shas": []string{treeSHA},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	results, ok := result["results"].(map[string]interface{})
	require.True(t, ok)
	// Tree objects are not commits, should be null
	assert.Nil(t, results[treeSHA], "non-commit object should have null result")
}

func TestServeGitVerifyCommits_MalformedBody(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", strings.NewReader("not json"))
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	results, ok := result["results"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, results, "malformed body should return empty results")
}

// --- parseGitStatusPorcelain edge cases ---

func TestParseGitStatusPorcelain_Rename(t *testing.T) {
	output := "R  old.txt -> new.txt\n"
	files := parseGitStatusPorcelain(output)
	assert.Len(t, files, 1)
	assert.Equal(t, "new.txt", files[0].Path)
	assert.Equal(t, "R", files[0].Type)
	assert.True(t, files[0].Staged)
}

func TestParseGitStatusPorcelain_StagedModifiedAlsoDirty(t *testing.T) {
	// AM = staged add, also modified in worktree
	output := "AM file.txt\n"
	files := parseGitStatusPorcelain(output)
	assert.Len(t, files, 2)
	assert.Equal(t, "A", files[0].Type)
	assert.True(t, files[0].Staged)
	assert.Equal(t, "M", files[1].Type)
	assert.False(t, files[1].Staged)
}

func TestParseGitStatusPorcelain_Untracked(t *testing.T) {
	output := "?? newfile.txt\n"
	files := parseGitStatusPorcelain(output)
	assert.Len(t, files, 1)
	assert.Equal(t, "?", files[0].Type)
	assert.False(t, files[0].Staged)
	assert.Equal(t, "newfile.txt", files[0].Path)
}

func TestParseGitStatusPorcelain_UnstagedModification(t *testing.T) {
	// Use a multi-line input so TrimSpace doesn't strip the leading space from the " M" line.
	// The first line "A  staged.txt" ensures TrimSpace only strips outer whitespace.
	output := "A  staged.txt\n M modified.txt\n"
	files := parseGitStatusPorcelain(output)
	assert.Len(t, files, 2)
	// First file: staged add
	assert.Equal(t, "A", files[0].Type)
	assert.True(t, files[0].Staged)
	// Second file: unstaged modification (space M)
	assert.Equal(t, "M", files[1].Type)
	assert.False(t, files[1].Staged)
}

func TestParseGitStatusPorcelain_Deleted(t *testing.T) {
	// Use multi-line to preserve leading space in " D"
	output := "A  staged.txt\n D deleted.txt\n"
	files := parseGitStatusPorcelain(output)
	assert.Len(t, files, 2)
	assert.Equal(t, "D", files[1].Type)
	assert.False(t, files[1].Staged)
}

func TestParseGitStatusPorcelain_Empty(t *testing.T) {
	files := parseGitStatusPorcelain("")
	assert.Len(t, files, 0)
}

func TestParseGitStatusPorcelain_QuotedPath(t *testing.T) {
	output := `?? "path with spaces.txt"` + "\n"
	files := parseGitStatusPorcelain(output)
	assert.Len(t, files, 1)
	assert.Equal(t, "path with spaces.txt", files[0].Path)
}

// --- gitDiff staged + unstaged combination ---

func TestGitDiff_StagedAndUnstaged(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Stage some changes
	createTestFile(t, env.ProjectDir, "README.md", "# Staged")
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "add", "README.md")

	// Also make unstaged changes on top
	createTestFile(t, env.ProjectDir, "README.md", "# Staged + Unstaged")

	output, err := gitDiff(env.ProjectDir, "README.md", "HEAD")
	assert.NoError(t, err)
	// Should contain both staged and unstaged changes
	assert.Contains(t, string(output), "# Staged")
}

func TestGitDiff_SpecificCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Make a commit
	createTestFile(t, env.ProjectDir, "README.md", "# Updated")
	gitCommitAll(t, env.ProjectDir, "update readme")
	sha := getHeadSHA(t, env.ProjectDir)

	output, err := gitDiff(env.ProjectDir, "README.md", sha)
	assert.NoError(t, err)
	assert.NotEmpty(t, output)
}

// --- writeDiffResponse edge cases ---

func TestWriteDiffResponse_ErrorWithNoOutput(t *testing.T) {
	w := httptest.NewRecorder()
	writeDiffResponse(w, nil, fmt.Errorf("some error"))

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, "", result["diff"])
	assert.Equal(t, true, result["empty"])
}

func TestWriteDiffResponse_EmptyDiffOutput(t *testing.T) {
	w := httptest.NewRecorder()
	writeDiffResponse(w, []byte(""), nil)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.Equal(t, true, result["empty"])
}

// --- ServeGitTags ---

func TestServeGitTags_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/tags", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["isGit"])
	tags, ok := resp["tags"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 0, len(tags))
}

func TestServeGitTags_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/tags", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeGitTags_NoProjectCookie(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/git/tags", nil)
	// No project cookie

	w := callHandler(ServeGitTags, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeGitTags_NoTags(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/tags", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	tags, ok := resp["tags"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 0, len(tags))
}

func TestServeGitTags_LightweightTag(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create a lightweight tag (no message)
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	sha := getHeadSHA(t, env.ProjectDir)
	run("git", "tag", "v1.0")

	req := newRequest(t, http.MethodGet, "/api/git/tags", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	tags, ok := resp["tags"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(tags))

	tag, _ := tags[0].(map[string]interface{})
	assert.Equal(t, "v1.0", tag["name"])
	assert.Equal(t, sha, tag["sha"])
	assert.NotEmpty(t, tag["date"])
	// Lightweight tags show the commit message via `git tag -n1`, not a tag message
	assert.NotEmpty(t, tag["msg"])
}

func TestServeGitTags_AnnotatedTag(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create an annotated tag (has message)
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "tag", "-a", "v2.0", "-m", "Release v2.0")

	req := newRequest(t, http.MethodGet, "/api/git/tags", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	tags, ok := resp["tags"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(tags))

	tag, _ := tags[0].(map[string]interface{})
	assert.Equal(t, "v2.0", tag["name"])
	assert.Equal(t, "Release v2.0", tag["msg"])
	assert.NotEmpty(t, tag["date"])
	assert.NotEmpty(t, tag["author"])
}

func TestServeGitTags_MultipleTags(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "tag", "v1.0")
	run("git", "tag", "-a", "v2.0", "-m", "Second release")

	req := newRequest(t, http.MethodGet, "/api/git/tags", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	tags, ok := resp["tags"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 2, len(tags))
}

// --- Worktree ChangeCount ---

func TestServeGitWorktrees_ChangeCountDirty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create dirty state: modify tracked file + add untracked file
	createTestFile(t, env.ProjectDir, "README.md", "# Modified")
	createTestFile(t, env.ProjectDir, "newfile.txt", "new content")

	req := newRequest(t, http.MethodGet, "/api/git/worktrees", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["isGit"])
	worktrees, ok := resp["worktrees"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(worktrees))

	wt, _ := worktrees[0].(map[string]interface{})
	assert.Equal(t, true, wt["dirty"])
	// changeCount should be >= 2 (one modified, one untracked)
	changeCount, ok := wt["changeCount"].(float64)
	assert.True(t, ok, "changeCount should be a number")
	assert.GreaterOrEqual(t, int(changeCount), 2)
	// untrackedCount should be >= 1
	untrackedCount, ok := wt["untrackedCount"].(float64)
	assert.True(t, ok, "untrackedCount should be a number")
	assert.GreaterOrEqual(t, int(untrackedCount), 1)
	// changeCount should be > untrackedCount since we also have a modified file
	assert.Greater(t, int(changeCount), int(untrackedCount))
}

func TestServeGitWorktrees_ChangeCountClean(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/worktrees", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	worktrees, ok := resp["worktrees"].([]interface{})
	assert.True(t, ok)
	assert.Equal(t, 1, len(worktrees))

	wt, _ := worktrees[0].(map[string]interface{})
	assert.Equal(t, false, wt["dirty"])
	// changeCount should be 0 for clean worktree
	changeCount, ok := wt["changeCount"].(float64)
	assert.True(t, ok, "changeCount should be a number")
	assert.Equal(t, 0, int(changeCount))
}

// --- serveGitDeleteBranch ---

func TestServeGitDeleteBranch_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create a second branch
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "branch", "feature-x")

	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "feature-x",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])
}

func TestServeGitDeleteBranch_CurrentBranch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "main",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "cannot_delete_current", resp["error"])
}

func TestServeGitDeleteBranch_DefaultBranch(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Create and switch to a non-default branch
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "checkout", "-b", "feature-y")
	// Now current branch is feature-y, but main is still default

	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "main",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "cannot_delete_default", resp["error"])
}

func TestServeGitDeleteBranch_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "nonexistent",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "branch_not_found", resp["error"])
}

func TestServeGitDeleteBranch_EmptyName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitDeleteBranch_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "some-branch",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitDeleteBranch_UnmergedForce(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	// Create branch, make a commit on it, switch back to main
	run("git", "checkout", "-b", "unmerged-branch")
	createTestFile(t, env.ProjectDir, "unmerged.txt", "unmerged content")
	run("git", "add", ".")
	run("git", "commit", "-m", "unmerged commit")
	run("git", "checkout", "main")

	// Try to delete unmerged branch — should force delete
	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "unmerged-branch",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])
}

// --- serveGitDeleteWorktree ---

func TestServeGitDeleteWorktree_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	// Create a worktree
	wtPath := filepath.Join(filepath.Dir(env.ProjectDir), "wt-test")
	run("git", "worktree", "add", wtPath, "-b", "wt-branch")

	req := newRequest(t, http.MethodDelete, "/api/git/worktrees", map[string]interface{}{
		"path": wtPath,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])
}

func TestServeGitDeleteWorktree_CurrentWorktree(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/worktrees", map[string]interface{}{
		"path": env.ProjectDir,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "cannot_delete_current", resp["error"])
}

func TestServeGitDeleteWorktree_EmptyPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/worktrees", map[string]interface{}{
		"path": "",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitDeleteWorktree_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/git/worktrees", map[string]interface{}{
		"path": "/some/path",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// ISS-208: Path traversal in worktree delete must be rejected.
func TestServeGitDeleteWorktree_PathTraversalRejected(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Request to delete a path outside the root paths
	req := newRequest(t, http.MethodDelete, "/api/git/worktrees", map[string]interface{}{
		"path": "/tmp",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "path_not_allowed", resp["error"])
}

// Dirty worktree: without force flag, should return dirty_worktree error.
func TestServeGitDeleteWorktree_DirtyWithoutForce(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	// Create a worktree with uncommitted changes
	wtPath := filepath.Join(filepath.Dir(env.ProjectDir), "wt-dirty")
	run("git", "worktree", "add", wtPath, "-b", "wt-dirty-branch")
	if err := os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatalf("failed to write dirty file: %v", err)
	}

	req := newRequest(t, http.MethodDelete, "/api/git/worktrees", map[string]interface{}{
		"path": wtPath,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "dirty_worktree", resp["error"])

	// Worktree should still exist
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("dirty worktree should not be deleted without force flag")
	}
}

// Dirty worktree: with force flag, should successfully delete.
func TestServeGitDeleteWorktree_DirtyWithForce(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	// Create a worktree with uncommitted changes
	wtPath := filepath.Join(filepath.Dir(env.ProjectDir), "wt-dirty-force")
	run("git", "worktree", "add", wtPath, "-b", "wt-dirty-force-branch")
	if err := os.WriteFile(filepath.Join(wtPath, "dirty.txt"), []byte("uncommitted"), 0o644); err != nil {
		t.Fatalf("failed to write dirty file: %v", err)
	}

	req := newRequest(t, http.MethodDelete, "/api/git/worktrees", map[string]interface{}{
		"path":  wtPath,
		"force": true,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])

	// Worktree should be deleted
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("dirty worktree should be deleted with force flag")
	}
}

// --- serveGitDeleteTag ---

func TestServeGitDeleteTag_Success(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "tag", "v1.0")

	req := newRequest(t, http.MethodDelete, "/api/git/tags", map[string]interface{}{
		"name": "v1.0",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, true, resp["success"])
}

func TestServeGitDeleteTag_NotFound(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/tags", map[string]interface{}{
		"name": "nonexistent",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, false, resp["success"])
	assert.Equal(t, "delete_failed", resp["error"])
}

func TestServeGitDeleteTag_EmptyName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/tags", map[string]interface{}{
		"name": "",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitDeleteTag_NotGitRepo(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/git/tags", map[string]interface{}{
		"name": "v1.0",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// Verify DELETE on branch/worktree/tags doesn't break GET (method dispatch)

func TestServeGitBranch_GET_StillWorks(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/branch", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertOK(t, w)
}

func TestServeGitTags_GET_StillWorks(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/tags", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertOK(t, w)
}

// --- ServeGitVerifyWorktrees ---

func TestServeGitVerifyWorktrees_SingleWorktree(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": []string{env.ProjectDir},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)
	info, ok := results[env.ProjectDir].(map[string]interface{})
	assert.True(t, ok, "project dir should be a valid worktree")
	assert.Equal(t, "main", info["branch"])
	assert.Equal(t, true, info["isCurrent"])
}

func TestServeGitVerifyWorktrees_NonWorktreePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": []string{"/tmp/nonexistent-worktree-path"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)
	assert.Nil(t, results["/tmp/nonexistent-worktree-path"])
}

func TestServeGitVerifyWorktrees_RelativePath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}
	run("git", "branch", "feature-x")
	run("git", "worktree", "add", ".worktrees/feature-x", "feature-x")
	defer func() { _ = os.RemoveAll(filepath.Join(env.ProjectDir, ".worktrees")) }()

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": []string{".worktrees/feature-x"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)

	absKey := filepath.Join(env.ProjectDir, ".worktrees/feature-x")
	info, ok := results[absKey].(map[string]interface{})
	assert.True(t, ok, "resolved path %q should be found in results (keys: %v)", absKey, keysOfMap(results))
	assert.Equal(t, "feature-x", info["branch"])
}

func keysOfMap(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestServeGitVerifyWorktrees_EmptyPaths(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": []string{},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)
	assert.Empty(t, results)
}

func TestServeGitVerifyWorktrees_WrongMethod(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/verify-worktrees", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertStatus(t, w, http.StatusMethodNotAllowed)
}

func TestServeGitVerifyWorktrees_InvalidJSON(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", strings.NewReader("not json"))
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	results, ok := result["results"].(map[string]interface{})
	require.True(t, ok)
	assert.Empty(t, results, "invalid JSON body should return empty results")
}

func TestServeGitVerifyWorktrees_NoProject(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": []string{env.ProjectDir},
	})

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertStatus(t, w, http.StatusForbidden)
}

func TestServeGitVerifyWorktrees_NonGitDir(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": []string{env.ProjectDir},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)
	assert.Empty(t, results, "non-git dir should return empty results")
}

func TestServeGitVerifyWorktrees_MaxPathsTruncation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	paths := make([]string, 105)
	for i := range paths {
		paths[i] = fmt.Sprintf("/tmp/worktree-%d", i)
	}

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": paths,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)
	assert.LessOrEqual(t, len(results), 100, "results should be truncated to maxPaths=100")
}

func TestServeGitVerifyWorktrees_MultipleWorktrees(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Dir = env.ProjectDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("command %s %v failed: %v\n%s", name, args, err, out)
		}
	}

	run("git", "branch", "feature-a")
	run("git", "worktree", "add", ".worktrees/feature-a", "feature-a")
	run("git", "branch", "feature-b")
	run("git", "worktree", "add", ".worktrees/feature-b", "feature-b")
	defer func() { _ = os.RemoveAll(filepath.Join(env.ProjectDir, ".worktrees")) }()

	featureAPath := filepath.Join(env.ProjectDir, ".worktrees/feature-a")
	featureBPath := filepath.Join(env.ProjectDir, ".worktrees/feature-b")

	req := newRequest(t, http.MethodPost, "/api/git/verify-worktrees", map[string]interface{}{
		"paths": []string{featureAPath, featureBPath, "/tmp/nonexistent"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyWorktrees, req)
	assertOK(t, w)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	results, ok := resp["results"].(map[string]interface{})
	assert.True(t, ok)

	infoA, ok := results[featureAPath].(map[string]interface{})
	assert.True(t, ok, "feature-a should be a valid worktree")
	assert.Equal(t, "feature-a", infoA["branch"])

	infoB, ok := results[featureBPath].(map[string]interface{})
	assert.True(t, ok, "feature-b should be a valid worktree")
	assert.Equal(t, "feature-b", infoB["branch"])

	assert.Nil(t, results["/tmp/nonexistent"])
}

// --- isValidGitSHA / isValidGitRefName (ISS-132, ISS-151, ISS-152) ---

func TestIsValidGitSHA(t *testing.T) {
	tests := []struct {
		sha  string
		want bool
	}{
		{"abc1234", true},      // 7-char abbreviated SHA
		{"abc123def456", true}, // 12-char abbreviated SHA
		{"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", true}, // 40-char full SHA
		{"abcdef", true},              // 6-char minimum
		{"ABCDE", false},              // uppercase, too short
		{"abcde", false},              // 5 chars, too short
		{"", false},                   // empty
		{"--upload-pack=evil", false}, // argument injection attempt
		{"-c", false},                 // flag-like
		{"abc123; rm -rf /", false},   // shell injection attempt
		{"g123456", false},            // non-hex char 'g'
		{"12345abcde", true},          // digits + hex
		{"HEAD", true},                // HEAD is a safe git ref for working tree diffs
	}
	for _, tt := range tests {
		t.Run(tt.sha, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidGitSHA(tt.sha))
		})
	}
}

func TestIsValidGitRefName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"main", true},
		{"feature-x", true},
		{"feature/login", true},
		{"release/v1.0", true},
		{"fix_bug", true},
		{"v1.0", true},
		{"", false},                   // empty
		{"-c", false},                 // starts with dash
		{"--upload-pack=evil", false}, // starts with dash
		{"-m", false},                 // starts with dash
		{"branch with spaces", false}, // contains spaces
		{"branch\twith\ttabs", false}, // contains tabs
		{"branch\nnewline", false},    // contains newline
		{"branch\x00null", false},     // contains NUL byte
		{"feature-x.y", true},         // dots are fine
		{"a", true},                   // single char
		{"release/1.0.0", true},       // complex but valid
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidGitRefName(tt.name))
		})
	}
}

// --- SHA validation in API endpoints (ISS-132) ---

func TestServeGitFileDiff_InvalidSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Argument injection attempt via SHA parameter
	req := newRequest(t, http.MethodGet, "/api/git/file-diff?sha=--upload-pack%3Devil&path=README.md", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitFileDiff, req)
	assertStatus(t, w, http.StatusBadRequest)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "InvalidSHA", resp["msgKey"])
}

func TestServeGitCommitFiles_InvalidSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodGet, "/api/git/commit-files?sha=-c", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCommitFiles, req)
	assertStatus(t, w, http.StatusBadRequest)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "InvalidSHA", resp["msgKey"])
}

func TestServeGitVerifyCommits_ArgInjectionSHA(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/verify-commits", map[string]interface{}{
		"shas": []string{"--upload-pack=evil"},
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitVerifyCommits, req)
	assertStatus(t, w, http.StatusBadRequest)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "InvalidSHA", resp["msgKey"])
}

func TestServeGitDiff_InvalidCommit(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// commit parameter with argument injection attempt
	req := newRequest(t, http.MethodGet, "/api/git/diff?path=README.md&commit=--upload-pack%3Devil", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitDiff, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// --- Branch/tag name validation in API endpoints (ISS-151, ISS-152) ---

func TestServeGitCheckout_FlagAsBranchName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Argument injection attempt via branch name
	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "--upload-pack=evil",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertStatus(t, w, http.StatusBadRequest)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "InvalidBranchName", resp["msgKey"])
}

func TestServeGitCheckout_DashCAsBranchName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodPost, "/api/git/checkout", map[string]interface{}{
		"branch": "-c",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitCheckout, req)
	assertStatus(t, w, http.StatusBadRequest)
}

func TestServeGitDeleteBranch_FlagAsBranchName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/branch", map[string]interface{}{
		"name": "--upload-pack=evil",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitBranch, req)
	assertStatus(t, w, http.StatusBadRequest)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "invalid_branch_name", resp["error"])
}

func TestServeGitDeleteTag_FlagAsTagName(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/tags", map[string]interface{}{
		"name": "-m",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertStatus(t, w, http.StatusBadRequest)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "invalid_tag_name", resp["error"])
}

func TestServeGitDeleteTag_UploadPackInjection(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	req := newRequest(t, http.MethodDelete, "/api/git/tags", map[string]interface{}{
		"name": "--upload-pack=evil",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ServeGitTags, req)
	assertStatus(t, w, http.StatusBadRequest)
}

// --- ISS-179: Tightened isValidGitRefName regex ---

func TestIsValidGitRefName_ValidNames(t *testing.T) {
	valid := []string{
		"main",
		"feature-x",
		"feature_x",
		"release/v1.0",
		"v1.0",
		"fix/issue-123",
		"my-branch",
		"123branch",
		"a",
	}
	for _, name := range valid {
		assert.True(t, isValidGitRefName(name), "expected %q to be valid", name)
	}
}

func TestIsValidGitRefName_InvalidNames(t *testing.T) {
	invalid := []string{
		// Leading dash (flag injection)
		"-evil",
		// Shell metacharacters (ISS-179: tightened to reject these)
		"foo;rm",
		"foo|bar",
		"foo$(cmd)",
		"foo`cmd`",
		"foo!bar",
		"foo\\bar",
		// Whitespace and control characters
		"foo bar",
		"foo\tbar",
		"foo\nbar",
		// Empty string
		"",
	}
	for _, name := range invalid {
		assert.False(t, isValidGitRefName(name), "expected %q to be invalid", name)
	}
}

// --- ISS-117/131/183: Cookie token decoupled from password hash ---

func TestServeLogin_CookieTokenDiffersFromPasswordHash(t *testing.T) {
	// After login, the cookie value must NOT equal the password hash (SessionToken).
	// The cookie value should be a cryptographically random CookieToken instead.
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte("testpass"), bcrypt.MinCost)
	require.NoError(t, err)
	model.PasswordHash = bcryptHash

	req := newRequest(t, http.MethodPost, "/login", map[string]string{
		"password": "testpass",
	})
	w := callHandler(ServeLogin, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Find the session cookie
	var cookieValue string
	for _, c := range w.Result().Cookies() {
		if c.Name == model.SessionCookie {
			cookieValue = c.Value
		}
	}
	assert.NotEmpty(t, cookieValue, "expected session cookie to be set")
	// Cookie must differ from the password hash (SessionToken)
	assert.NotEqual(t, model.SessionToken, cookieValue,
		"cookie value must NOT equal the password-derived SessionToken (ISS-117, ISS-131, ISS-183)")
	// CookieToken must be set and match the cookie
	assert.NotEmpty(t, model.CookieToken, "CookieToken must be set after login")
	assert.Equal(t, model.CookieToken, cookieValue, "cookie value must equal CookieToken")
}

func TestServeAuthCheck_UsesCookieToken(t *testing.T) {
	// ServeAuthCheck should validate against CookieToken, not SessionToken
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.SessionToken = hashPassword("testpass")
	model.CookieToken = model.GenerateRandomToken(32)

	// Request with CookieToken should succeed
	req := newRequest(t, http.MethodGet, "/api/auth/check", nil)
	withAuthCookie(req, model.CookieToken)
	w := callHandler(ServeAuthCheck, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Request with SessionToken (password hash) should fail
	req2 := newRequest(t, http.MethodGet, "/api/auth/check", nil)
	withAuthCookie(req2, model.SessionToken)
	w2 := callHandler(ServeAuthCheck, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code,
		"password hash should NOT be accepted as cookie value (ISS-117, ISS-131, ISS-183)")
}

// --- isDirtyWorktreeError ---

func TestIsDirtyWorktreeError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		{"modified files", "error: Cannot delete worktree with modified files", true},
		{"untracked files", "error: Cannot delete worktree with untracked files", true},
		{"uncommitted changes", "error: Cannot delete worktree with uncommitted changes", true},
		{"no dirty indicators", "fatal: not a git repository", false},
		{"empty message", "", false},
		{"other error", "error: unknown option", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isDirtyWorktreeError(tt.errMsg))
		})
	}
}

// --- resolveSymlinkPath ---

func TestResolveSymlinkPath(t *testing.T) {
	tmpDir := t.TempDir()
	realFile := filepath.Join(tmpDir, "real")
	require.NoError(t, os.WriteFile(realFile, []byte("test"), 0o644))

	linkPath := filepath.Join(tmpDir, "link")
	require.NoError(t, os.Symlink(realFile, linkPath))

	// Resolving a symlink should return the fully resolved target.
	// On macOS, EvalSymlinks resolves /var → /private/var for symlink targets
	// but not necessarily for the direct path, so we compare both through EvalSymlinks.
	result := resolveSymlinkPath(linkPath)
	resolvedLinkTarget, _ := filepath.EvalSymlinks(result)
	resolvedReal, _ := filepath.EvalSymlinks(realFile)
	assert.Equal(t, resolvedReal, resolvedLinkTarget)

	// Resolving a non-existent path should return the original path
	nonExistent := filepath.Join(tmpDir, "nonexistent")
	assert.Equal(t, nonExistent, resolveSymlinkPath(nonExistent))

	// A regular path resolves to its canonical form
	resultRegular := resolveSymlinkPath(realFile)
	resolvedResult, _ := filepath.EvalSymlinks(resultRegular)
	assert.Equal(t, resolvedReal, resolvedResult)
}

// --- forceRemoveWorktree ---

func TestForceRemoveWorktree_Failure(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	initGitRepo(t, env.ProjectDir)

	// Force remove a non-existent path should fail
	w := httptest.NewRecorder()
	result := forceRemoveWorktree(w, env.ProjectDir, "/tmp/nonexistent-wt-force-xyz")
	assert.False(t, result)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "delete_failed", resp["error"])
}
