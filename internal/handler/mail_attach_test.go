package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMailAttachEnv points the outbox at a temp dir so staging never writes
// into the developer's real data directory.
func setupMailAttachEnv(t *testing.T) *testEnv {
	t.Helper()
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)
	origDataDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = origDataDir })
	return env
}

func decodeStagedAttachments(t *testing.T, body []byte) []service.MailStagedAttachment {
	t.Helper()
	var resp struct {
		OK          bool                           `json:"ok"`
		Attachments []service.MailStagedAttachment `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.True(t, resp.OK)
	return resp.Attachments
}

func TestServeMailAttachments_StagesProjectFile(t *testing.T) {
	env := setupMailAttachEnv(t)
	createTestFile(t, env.ProjectDir, "notes/a.txt", "hello")

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/mail/attachments",
		map[string]any{"paths": []string{"notes/a.txt"}}), env.ProjectDir)
	w := callHandler(ServeMailAttachments, req)
	assertStatus(t, w, http.StatusOK)

	atts := decodeStagedAttachments(t, w.Body.Bytes())
	require.Len(t, atts, 1)
	// The composer's send endpoint reads an absolute path, so a project-relative
	// request must come back resolved.
	assert.Equal(t, filepath.Join(env.ProjectDir, "notes", "a.txt"), atts[0].Path)
	assert.Equal(t, "a.txt", atts[0].Filename)
	assert.Equal(t, int64(5), atts[0].Size)
}

func TestServeMailAttachments_PacksDirectory(t *testing.T) {
	env := setupMailAttachEnv(t)
	createTestFile(t, env.ProjectDir, "assets/one.txt", "1")
	createTestFile(t, env.ProjectDir, "assets/two.txt", "2")

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/mail/attachments",
		map[string]any{"paths": []string{"assets"}}), env.ProjectDir)
	w := callHandler(ServeMailAttachments, req)
	assertStatus(t, w, http.StatusOK)

	atts := decodeStagedAttachments(t, w.Body.Bytes())
	require.Len(t, atts, 1)
	assert.Equal(t, "assets.zip", atts[0].Filename)
	assert.FileExists(t, atts[0].Path)
	assert.Equal(t, filepath.Join(model.DataDir, "mail", "outbox"), filepath.Dir(atts[0].Path))
}

func TestServeMailAttachments_MissingPathIs404(t *testing.T) {
	env := setupMailAttachEnv(t)

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/mail/attachments",
		map[string]any{"paths": []string{"nope.txt"}}), env.ProjectDir)
	w := callHandler(ServeMailAttachments, req)
	assertStatus(t, w, http.StatusNotFound)
}

func TestServeMailAttachments_RejectsBadRequests(t *testing.T) {
	env := setupMailAttachEnv(t)

	t.Run("wrong method", func(t *testing.T) {
		req := withProjectCookie(newRequest(t, http.MethodGet, "/api/mail/attachments", nil), env.ProjectDir)
		w := callHandler(ServeMailAttachments, req)
		assertStatus(t, w, http.StatusMethodNotAllowed)
	})

	t.Run("empty path list", func(t *testing.T) {
		req := withProjectCookie(newRequest(t, http.MethodPost, "/api/mail/attachments",
			map[string]any{"paths": []string{}}), env.ProjectDir)
		w := callHandler(ServeMailAttachments, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("too many paths", func(t *testing.T) {
		paths := make([]string, maxMailAttachPaths+1)
		for i := range paths {
			paths[i] = "a.txt"
		}
		req := withProjectCookie(newRequest(t, http.MethodPost, "/api/mail/attachments",
			map[string]any{"paths": paths}), env.ProjectDir)
		w := callHandler(ServeMailAttachments, req)
		assertStatus(t, w, http.StatusBadRequest)
	})

	t.Run("relative path without a project", func(t *testing.T) {
		req := newRequest(t, http.MethodPost, "/api/mail/attachments",
			map[string]any{"paths": []string{"a.txt"}})
		w := callHandler(ServeMailAttachments, req)
		assertStatus(t, w, http.StatusForbidden)
	})
}

func TestServeMailAttachments_OversizedFileIsRejected(t *testing.T) {
	env := setupMailAttachEnv(t)
	path := filepath.Join(env.ProjectDir, "big.bin")
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(service.MailMaxAttachmentBytes()+1))
	require.NoError(t, f.Close())

	req := withProjectCookie(newRequest(t, http.MethodPost, "/api/mail/attachments",
		map[string]any{"paths": []string{"big.bin"}}), env.ProjectDir)
	w := callHandler(ServeMailAttachments, req)
	assertStatus(t, w, http.StatusBadRequest)
	assert.Contains(t, w.Body.String(), "25")
}
