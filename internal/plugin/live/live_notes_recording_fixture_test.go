package live

// The meeting-recording upload tests, hosted from this file so the shared
// helper below is the only copy.
//
// Two platform traps live here, both about the project cookie:
//
//  1. `sameProjectPath` (the ownership check every notes endpoint goes through)
//     compares EvalSymlinks'd paths, and on Windows a temp dir is a short-name
//     path (RUNNER~1 vs runneradmin). The fixture therefore signs the cookie with
//     the resolved spelling — the same one the server handed the browser.
//  2. The server stores the path URL-ESCAPED in the cookie (`setAppCookie(...,
//     url.QueryEscape(path))`), and Go's net/http DROPS a Cookie whose value
//     contains a raw backslash. On Windows `C:\\Users\\...` is exactly that, so an
//     unescaped fixture cookie vanished and every request answered 404 before the
//     handler ran. Every cookie built in these tests goes through url.QueryEscape.

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
