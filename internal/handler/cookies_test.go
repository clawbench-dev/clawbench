package handler

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Application cookies must never be Secure, and must never be built outside
// setAppCookie. Both rules exist for the same failure: the server is HTTP or
// HTTPS depending on whether a certificate exists at startup, users switch
// between the two, and a Secure cookie is invisible over HTTP — while the
// browser refuses to let a plain-HTTP response overwrite it. The result is a
// permanent 403 NoProjectSelected that re-logging in cannot fix.
//
// A unit test per cookie cannot catch a newly added one, so this scans the
// package instead.
func TestApplicationCookies_AreOnlyBuiltBySetAppCookie(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == "cookies.go" {
			continue // the one place allowed to touch http.SetCookie
		}
		src, err := os.ReadFile(name)
		require.NoError(t, err)
		text := string(src)
		checked++
		assert.NotContains(t, text, "http.SetCookie(",
			"%s builds a cookie directly; route it through setAppCookie so it survives an HTTP/HTTPS switch", name)
		assert.NotContains(t, text, "Secure:",
			"%s marks a cookie Secure; it will vanish after switching back to HTTP", name)
	}
	// Guard against the scan silently matching nothing (e.g. a future layout
	// change moving the package): the assertion above only means something if
	// there are files to scan.
	assert.Positive(t, checked, "expected to scan handler source files")
}
