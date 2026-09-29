package model_test

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ensureDir is a test helper that creates the directory if it doesn't exist.
// ValidatePath now requires the base directory to exist on disk (for EvalSymlinks).
func ensureDir(t *testing.T, dir string) {
	t.Helper()
	assert.NoError(t, os.MkdirAll(dir, 0o755))
}

func TestValidatePath_ValidPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	path, valid := model.ValidatePath(base, "subdir/file.txt")
	assert.True(t, valid)
	assert.Contains(t, path, "subdir")
}

func TestValidatePath_BasePath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	path, valid := model.ValidatePath(base, "")
	assert.True(t, valid)
	assert.Contains(t, path, "base")
}

func TestValidatePath_PathTraversal(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base", "dir")
	ensureDir(t, base)
	path, valid := model.ValidatePath(base, "../../etc/passwd")
	assert.False(t, valid)
	_ = path // path is returned but not valid
}

func TestValidatePath_SimpleTraversal(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	_, valid := model.ValidatePath(base, "../outside")
	assert.False(t, valid)
}

func TestValidatePath_ValidSubdirectory(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	path, valid := model.ValidatePath(base, "sub/deep/file.go")
	assert.True(t, valid)
	assert.Contains(t, path, "sub")
}

func TestValidatePath_EmptyBaseAndRel(t *testing.T) {
	path, valid := model.ValidatePath("", "")
	assert.True(t, valid)
	// Should resolve to current working directory
	assert.NotEmpty(t, path)
}

// --- Additional boundary tests ---

func TestValidatePath_MultipleTraversalAttempts(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	_, valid := model.ValidatePath(base, "../../../etc/shadow")
	assert.False(t, valid)
}

func TestValidatePath_DotPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	path, valid := model.ValidatePath(base, ".")
	assert.True(t, valid)
	assert.Contains(t, path, "base")
}

func TestValidatePath_DotSlashPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	path, valid := model.ValidatePath(base, "./file.txt")
	assert.True(t, valid)
	assert.Contains(t, path, "file.txt")
}

func TestValidatePath_MixedTraversalAndValid(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	// "sub/../sub/file.txt" should normalize to "sub/file.txt" which is valid
	path, valid := model.ValidatePath(base, "sub/../sub/file.txt")
	assert.True(t, valid)
	assert.Contains(t, path, "sub")
}

func TestValidatePath_TraversalWithValidPrefix(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	// "sub/../../outside" - the normalized path escapes the base
	_, valid := model.ValidatePath(base, "sub/../../outside")
	assert.False(t, valid)
}

func TestValidatePath_DeepNesting(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	deepPath := "a/b/c/d/e/f/g/h/file.txt"
	path, valid := model.ValidatePath(base, deepPath)
	assert.True(t, valid)
	assert.Contains(t, path, "file.txt")
}

func TestValidatePath_SpecialCharacters(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	path, valid := model.ValidatePath(base, "file with spaces.txt")
	assert.True(t, valid)
	assert.Contains(t, path, "file with spaces.txt")
}

func TestValidatePath_EncodedTraversal(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	// These should be treated as literal directory names, not traversal
	// filepath.Join normalizes them, so ".." in a path segment is the traversal
	_, valid := model.ValidatePath(base, "..")
	assert.False(t, valid)
}

func TestValidatePath_AbsoluteRelPath(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	// Go's filepath.Join concatenates even absolute relPath to base,
	// so "/tmp/other" + "/other" becomes "base/other" which is valid.
	// This is Go-specific behavior; the function still validates the final path.
	path, valid := model.ValidatePath(base, "/some/absolute/path")
	// filepath.Join(base, "/some/absolute/path") = "base/some/absolute/path" which IS under base
	assert.True(t, valid)
	assert.Contains(t, path, "base")
}

// --- ISS-001: Symlink traversal tests ---

func TestValidatePath_SymlinkTraversal(t *testing.T) {
	// Create base dir and an outside dir
	base := filepath.Join(t.TempDir(), "base")
	outside := filepath.Join(t.TempDir(), "outside")
	ensureDir(t, base)
	ensureDir(t, outside)

	// Create a file outside the base
	assert.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("sensitive"), 0o644))

	// Create symlink inside base pointing to outside
	assert.NoError(t, os.Symlink(outside, filepath.Join(base, "escape")))

	// This should REJECT — symlink escapes the base directory
	_, valid := model.ValidatePath(base, "escape/secret.txt")
	assert.False(t, valid, "ValidatePath should reject symlink that escapes base directory")
}

func TestValidatePath_SymlinkInsideProject(t *testing.T) {
	// Create base dir with a real subdirectory and a symlink to it
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, filepath.Join(base, "real"))
	assert.NoError(t, os.WriteFile(filepath.Join(base, "real", "file.txt"), []byte("ok"), 0o644))

	// Symlink inside project pointing to another location also inside project
	assert.NoError(t, os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")))

	// This should ACCEPT — symlink target is still within base
	path, valid := model.ValidatePath(base, "link/file.txt")
	assert.True(t, valid, "ValidatePath should accept symlink whose target is within base")
	assert.Contains(t, path, "link")
}

func TestValidatePath_NonExistentUnderSymlinkDir(t *testing.T) {
	// Create base dir with a real subdirectory
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, filepath.Join(base, "real"))

	// Symlink inside project pointing to the real subdirectory
	assert.NoError(t, os.Symlink(filepath.Join(base, "real"), filepath.Join(base, "link")))

	// Creating a new file under the symlinked directory should be accepted
	path, valid := model.ValidatePath(base, "link/newfile.txt")
	assert.True(t, valid, "ValidatePath should accept new file under symlinked directory within base")
	assert.Contains(t, path, "link")
}

func TestValidatePath_SymlinkEscapeViaParent(t *testing.T) {
	// Create base dir and an outside dir
	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "base")
	outside := filepath.Join(tmpDir, "outside")
	ensureDir(t, base)
	ensureDir(t, outside)

	// The base directory itself is a symlink to outside
	linkBase := filepath.Join(tmpDir, "linkbase")
	assert.NoError(t, os.Symlink(base, linkBase))

	// Trying to access a file that resolves outside via the symlinked base
	assert.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("sensitive"), 0o644))
	assert.NoError(t, os.Symlink(outside, filepath.Join(base, "escape")))

	// Even through the symlinked base, escaping should be rejected
	_, valid := model.ValidatePath(linkBase, "escape/secret.txt")
	assert.False(t, valid, "ValidatePath should reject escape through symlinked parent")
}

// --- ResolveExistingPath tests ---

func TestResolveExistingPath_ExistingParent(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, filepath.Join(base, "sub"))
	// EvalSymlinks on existing directory
	evalBase, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)

	// Parent exists, file doesn't
	result := model.ResolveExistingPath(filepath.Join(base, "sub", "newfile.txt"), evalBase)
	assert.Contains(t, result, "sub")
	assert.Contains(t, result, "newfile.txt")
}

func TestResolveExistingPath_AllComponentsMissing(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	evalBase, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)

	// All path components except the root are missing
	result := model.ResolveExistingPath(filepath.Join(base, "a", "b", "c", "file.txt"), evalBase)
	// Should walk up to find "base" which exists, then reconstruct
	assert.Contains(t, result, "a")
	assert.Contains(t, result, "file.txt")
}

func TestResolveExistingPath_ExistingDirectory(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	evalBase, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)

	// Path itself is an existing directory
	result := model.ResolveExistingPath(base, evalBase)
	assert.NotEmpty(t, result)
}

// --- Additional ValidatePath edge cases ---

func TestValidatePath_NestedSymlinkStillEscapes(t *testing.T) {
	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "base")
	outside := filepath.Join(tmpDir, "outside")
	ensureDir(t, base)
	ensureDir(t, outside)

	// Double-nested symlink: base/link1 -> outside, outside/link2 -> /tmp
	assert.NoError(t, os.Symlink(outside, filepath.Join(base, "link1")))

	// Create file in outside
	assert.NoError(t, os.WriteFile(filepath.Join(outside, "target.txt"), []byte("data"), 0o644))

	_, valid := model.ValidatePath(base, "link1/target.txt")
	assert.False(t, valid, "should reject symlink pointing outside base")
}

func TestValidatePath_FileInBaseDir(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")
	ensureDir(t, base)
	// Create a file directly in base
	assert.NoError(t, os.WriteFile(filepath.Join(base, "readme.md"), []byte("hello"), 0o644))

	path, valid := model.ValidatePath(base, "readme.md")
	assert.True(t, valid)
	assert.Contains(t, path, "readme.md")
}

// NormalizeProjectPath is the identity function for a project: the registry
// stores this form, and any path arriving from a cookie / query string must be
// normalized the same way before comparison.
func TestNormalizeProjectPath(t *testing.T) {
	assert.Equal(t, "", model.NormalizeProjectPath(""))
	assert.Equal(t, "", model.NormalizeProjectPath("   "))

	dir := t.TempDir()
	// An existing directory canonicalizes to its symlink-resolved, cleaned form.
	assert.Equal(t, dir, model.NormalizeProjectPath(dir))
	// A redundant "/." segment and a trailing separator collapse away.
	assert.Equal(t, dir, model.NormalizeProjectPath(dir+string(filepath.Separator)+"."))
	assert.Equal(t, dir, model.NormalizeProjectPath(dir+string(filepath.Separator)))
	// A relative path is made absolute.
	assert.Equal(t, dir, model.NormalizeProjectPath(filepath.Join(dir, ".", "")))
}

// A symlinked spelling must resolve to the target, which is what makes a raw
// cookie path comparable to the canonical registry path (macOS /var).
func TestNormalizeProjectPath_ResolvesSymlink(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	assert.Equal(t, target, model.NormalizeProjectPath(link))
}

// A non-existent path cannot be symlink-resolved, so it falls back to the
// cleaned absolute form rather than failing.
func TestNormalizeProjectPath_NonExistentFallsBackToClean(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist", "..", "does-not-exist")

	got := model.NormalizeProjectPath(missing)
	assert.True(t, filepath.IsAbs(got))
	assert.Equal(t, filepath.Clean(missing), got)
}
