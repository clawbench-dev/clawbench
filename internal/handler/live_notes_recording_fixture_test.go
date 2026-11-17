package handler

// The meeting-recording upload tests, hosted from this file so the shared
// helper below is the only copy.
//
// Why the resolve step exists: `sameProjectPath` (the ownership check every
// notes endpoint goes through) compares EvalSymlinks'd paths. On Windows a temp
// dir is a short-name path (RUNNER~1 vs runneradmin) and the project cookie holds
// the RAW spelling — so a fixture that signs the cookie with one spelling and
// resolves with the other is rejected with 404 before the recording code runs.
// The cookie is therefore signed with the resolved path, which is also what a
// real browser sends (the server handed it out).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingProject returns a temp dir whose spelling survives EvalSymlinks, so
// the fixtures below can use it for both the cookie and the ownership check.
func recordingProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	if resolved == "" {
		return dir
	}
	// The resolved path must exist in its own right (it does: it names the same
	// directory), and it is what the server would have stored in the project
	// registry for this path.
	if _, err := os.Stat(resolved); err != nil {
		return dir
	}
	return resolved
}
