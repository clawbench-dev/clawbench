package handler

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A real 4x4 lossless WebP (36 bytes). WebP is decode-only in x/image, so the
// fixture has to be a literal rather than something encoded in the test.
const tinyWebPBase64 = "UklGRhwAAABXRUJQVlA4TA8AAAAvA8AAAAcQ/Y/+ByKi/wEA"

// Meeting key frames are stored as WebP. Without the x/image/webp decoder
// registered, every shot thumbnail answers "unsupported image format" — which
// the detail view renders as a broken image, far from the cause.
func TestFileThumb_WebP(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	raw, err := base64.StdEncoding.DecodeString(tinyWebPBase64)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(env.ProjectDir, "shot.webp"), raw, 0o644))

	req := newRequest(t, http.MethodGet, "/api/fs/thumb?target=shot.webp&w=50", nil)
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(FileThumb, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "image/jpeg", w.Header().Get("Content-Type"))
	assert.Greater(t, w.Body.Len(), 0)
}
