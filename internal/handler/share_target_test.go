package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildShareTargetRequest assembles the multipart POST the browser sends for a
// Web Share Target, using the field name the manifest declares.
func buildShareTargetRequest(t *testing.T, field string, files map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for name, content := range files {
		part, err := writer.CreateFormFile(field, name)
		require.NoError(t, err)
		_, err = part.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, shareTargetPath, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

// shareTargetRedirectParams parses the redirect Location into the query values
// the frontend reads.
func shareTargetRedirectParams(t *testing.T, w *httptest.ResponseRecorder) url.Values {
	t.Helper()
	require.Equal(t, http.StatusSeeOther, w.Code, "share target must answer with 303 so the browser re-issues a GET")
	loc := w.Header().Get("Location")
	require.NotEmpty(t, loc)
	parsed, err := url.Parse(loc)
	require.NoError(t, err)
	require.Equal(t, "/", parsed.Path)
	return parsed.Query()
}

func TestShareTarget_StoresFilesAndRedirectsWithPaths(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := buildShareTargetRequest(t, "files", map[string]string{
		"shot.png": "png-bytes",
		"notes.md": "md-bytes",
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)

	raw := params.Get("share_target")
	require.NotEmpty(t, raw)

	var paths []string
	require.NoError(t, json.Unmarshal([]byte(raw), &paths))
	assert.ElementsMatch(t, []string{
		".clawbench/uploads/shot.png",
		".clawbench/uploads/notes.md",
	}, paths)

	// The files must really be on disk under the project, not just claimed in
	// the redirect — the composer would attach paths that do not exist.
	for _, rel := range paths {
		onDisk := filepath.Join(env.ProjectDir, filepath.FromSlash(rel))
		data, err := os.ReadFile(onDisk)
		require.NoError(t, err, "%s must exist on disk", rel)
		assert.NotEmpty(t, data)
	}
}

// The manifest declares `files`, but a share sheet is free to send `file`.
// Dropping the unknown key would make the share silently do nothing.
func TestShareTarget_AcceptsSingularFileField(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := buildShareTargetRequest(t, "file", map[string]string{"one.png": "x"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)

	var paths []string
	require.NoError(t, json.Unmarshal([]byte(params.Get("share_target")), &paths))
	assert.Equal(t, []string{".clawbench/uploads/one.png"}, paths)
}

func TestShareTarget_NoProjectRedirectsToApp(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := buildShareTargetRequest(t, "files", map[string]string{"a.png": "x"})

	w := callHandler(ShareTarget, req)

	// The browser navigated here, so a JSON 403 would be an unreachable error
	// page. Send the user into the app instead.
	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))
}

func TestShareTarget_NoFilesReportsEmpty(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, shareTargetPath, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)

	assert.Equal(t, "empty", params.Get("share_target"))
}

func TestShareTarget_GetRedirectsToApp(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, shareTargetPath, http.NoBody)
	withProjectCookie(req, "")

	w := callHandler(ShareTarget, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))
}

func TestShareTarget_DeduplicatesCollidingNames(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Pre-create the name the share will want.
	uploads := filepath.Join(env.ProjectDir, ".clawbench", "uploads")
	require.NoError(t, os.MkdirAll(uploads, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "shot.png"), []byte("old"), 0o644))

	req := buildShareTargetRequest(t, "files", map[string]string{"shot.png": "new"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)

	var paths []string
	require.NoError(t, json.Unmarshal([]byte(params.Get("share_target")), &paths))
	require.Len(t, paths, 1)
	// Must not overwrite the existing upload: an older message may reference it.
	assert.NotEqual(t, ".clawbench/uploads/shot.png", paths[0])
	assert.Contains(t, paths[0], "shot_1")

	old, err := os.ReadFile(filepath.Join(uploads, "shot.png"))
	require.NoError(t, err)
	assert.Equal(t, "old", string(old), "an existing upload must never be overwritten")
}

func TestShareTarget_SanitizesPathTraversalFilename(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := buildShareTargetRequest(t, "files", map[string]string{"../../escape.png": "x"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)

	var paths []string
	require.NoError(t, json.Unmarshal([]byte(params.Get("share_target")), &paths))
	require.Len(t, paths, 1)
	assert.Equal(t, ".clawbench/uploads/escape.png", paths[0])

	// Nothing may land outside the uploads directory.
	escaped := filepath.Join(env.ProjectDir, "..", "escape.png")
	_, err := os.Stat(escaped)
	assert.True(t, os.IsNotExist(err), "a traversal filename must not write outside the project")
}

func TestShareTarget_ResponseIsNotCached(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := buildShareTargetRequest(t, "files", map[string]string{"a.png": "x"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)

	// A cached 303 would replay a stale path list on the next share.
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

// The manifest is the only place that tells a browser this endpoint exists, so
// the two must agree — a typo here would make sharing silently unavailable.
func TestShareTarget_ManifestActionMatchesRoute(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "assets", "manifest.json"))
	require.NoError(t, err)

	var manifest struct {
		ShareTarget struct {
			Action  string `json:"action"`
			Method  string `json:"method"`
			Enctype string `json:"enctype"`
			Params  struct {
				Files []struct {
					Name   string   `json:"name"`
					Accept []string `json:"accept"`
				} `json:"files"`
			} `json:"params"`
		} `json:"share_target"`
	}
	require.NoError(t, json.Unmarshal(raw, &manifest))

	assert.Equal(t, shareTargetPath, manifest.ShareTarget.Action,
		"manifest share_target.action must match the registered route")
	assert.Equal(t, http.MethodPost, manifest.ShareTarget.Method)
	assert.Equal(t, "multipart/form-data", manifest.ShareTarget.Enctype)
	require.Len(t, manifest.ShareTarget.Params.Files, 1)
	// The handler reads this exact field name.
	assert.Equal(t, "files", manifest.ShareTarget.Params.Files[0].Name)
	assert.NotEmpty(t, manifest.ShareTarget.Params.Files[0].Accept,
		"an empty accept list would make the OS offer ClawBench for every file type")

	// The route must actually be registered, authenticated. RegisterRoutes is
	// called here rather than relying on another test having populated the
	// table: this test must not depend on execution order.
	RegisterRoutes(http.NewServeMux())
	routes := RegisteredRoutes()
	var found *Route
	for i := range routes {
		if routes[i].Pattern == shareTargetPath {
			found = &routes[i]
			break
		}
	}
	require.NotNil(t, found, "%s must be registered", shareTargetPath)
	assert.True(t, found.Authenticated, "an unauthenticated share must not be able to write files")
}

func TestExtForContentType(t *testing.T) {
	cases := map[string]string{
		"image/png":                 "png",
		"image/jpeg":                "jpg",
		"image/jpg":                 "jpg",
		"image/webp":                "webp",
		"image/gif":                 "gif",
		"image/heic":                "heic",
		"image/svg+xml":             "svg",
		"application/pdf":           "pdf",
		"IMAGE/PNG":                 "png",
		"image/png; charset=binary": "png",
		"application/octet-stream":  "bin",
		"":                          "bin",
	}
	for ct, want := range cases {
		assert.Equal(t, want, extForContentType(ct), "content type %q", ct)
	}
}

// A share can arrive with a name that cannot be used as a file name. Go's
// multipart parser never yields an empty FileName (a part without `filename=`
// is a form value, not a file), but "." and ".." arrive intact and would
// otherwise target the uploads directory itself.
func TestShareTarget_UnusableFilenameGetsReplaced(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	for _, bad := range []string{".", ".."} {
		t.Run(bad, func(t *testing.T) {
			req := buildShareTargetRequest(t, "files", map[string]string{bad: "bytes"})
			withProjectCookie(req, env.ProjectDir)

			w := callHandler(ShareTarget, req)
			params := shareTargetRedirectParams(t, w)

			var paths []string
			require.NoError(t, json.Unmarshal([]byte(params.Get("share_target")), &paths))
			require.Len(t, paths, 1)
			// The stored name must be a real file under uploads/, not the
			// directory itself.
			assert.True(t, strings.HasPrefix(paths[0], ".clawbench/uploads/shared_"),
				"want a derived name, got %q", paths[0])

			onDisk := filepath.Join(env.ProjectDir, filepath.FromSlash(paths[0]))
			info, err := os.Stat(onDisk)
			require.NoError(t, err)
			assert.True(t, info.Mode().IsRegular(), "the destination must be a file, not a directory")
		})
	}
}

// A file over the per-file cap must be rejected, not written: the body limit is
// per-BATCH, so without an explicit check one oversized "photo" would slip past
// upload.max_size_mb that every other upload path honors.
func TestShareTarget_RejectsOversizedFileButKeepsTheRest(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Shrink the cap instead of writing a large file.
	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	// Just over the per-file cap but far below the per-BATCH body limit
	// (maxUploadSize()*maxShareTargetFiles = 20 MiB), so this exercises the
	// per-file check rather than MaxBytesReader.
	big := strings.Repeat("x", 1024*1024+1024) // ~1 MiB + 1 KiB

	req := buildShareTargetRequest(t, "files", map[string]string{
		"ok.txt":  "small",
		"big.bin": big,
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)

	var paths []string
	require.NoError(t, json.Unmarshal([]byte(params.Get("share_target")), &paths))
	assert.Equal(t, []string{".clawbench/uploads/ok.txt"}, paths)

	// The oversize count must reach the app, or the user is told everything
	// succeeded while a file silently vanished.
	assert.Equal(t, "1", params.Get("share_target_failed"))
	_, err := os.Stat(filepath.Join(env.ProjectDir, ".clawbench", "uploads", "big.bin"))
	assert.True(t, os.IsNotExist(err), "an oversized file must not be written")
}

// A full batch must not carry a failure count: the frontend would show a
// warning for a share that actually succeeded completely.
func TestShareTarget_SuccessCarriesNoFailureCount(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := buildShareTargetRequest(t, "files", map[string]string{"a.png": "x"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)

	assert.Equal(t, "", params.Get("share_target_failed"))
}

// The route is registered outside middleware.Auth, so the handler must enforce
// authentication itself — otherwise an unauthenticated share writes files.
func TestShareTarget_UnauthenticatedDoesNotWrite(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// setupTestEnv clears both tokens (open access), so a password must be
	// configured here for "unauthenticated" to mean anything.
	origSession := model.SessionToken
	model.SessionToken = "test-token"
	t.Cleanup(func() { model.SessionToken = origSession })

	req := buildShareTargetRequest(t, "files", map[string]string{"a.png": "x"})
	withProjectCookie(req, env.ProjectDir)
	// Deliberately no session cookie.

	w := callHandler(ShareTarget, req)

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, "/", w.Header().Get("Location"))

	_, err := os.Stat(filepath.Join(env.ProjectDir, ".clawbench", "uploads", "a.png"))
	assert.True(t, os.IsNotExist(err), "an unauthenticated share must not write files")
}

// A body that is not valid multipart must bounce back into the app with a
// reason, not answer 400 (the browser navigated here and would render JSON).
func TestShareTarget_InvalidMultipartRedirectsWithReason(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := httptest.NewRequest(http.MethodPost, shareTargetPath, strings.NewReader("not multipart"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=nope")
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)
	assert.Equal(t, "invalid", params.Get("share_target"))
}

// If the uploads directory cannot be created there is nowhere to write, and the
// user must be told rather than getting a silent no-op.
func TestShareTarget_UnwritableProjectRedirectsWithSaveReason(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0o000 does not make a directory unreadable on Windows (mode bits are not enforced)")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	env, teardown := setupTestEnv(t)
	defer teardown()

	// Make the project read-only so MkdirAll of .clawbench/uploads fails.
	require.NoError(t, os.Chmod(env.ProjectDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(env.ProjectDir, 0o755) })

	req := buildShareTargetRequest(t, "files", map[string]string{"a.png": "x"})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)
	assert.Equal(t, "save", params.Get("share_target"))
}

// Every file over the cap means nothing is stored, which must read as a save
// failure rather than an empty share.
func TestShareTarget_AllFilesOversizedReportsSave(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	orig := model.UploadMaxSizeMB
	model.UploadMaxSizeMB = 1
	t.Cleanup(func() { model.UploadMaxSizeMB = orig })

	req := buildShareTargetRequest(t, "files", map[string]string{
		"big1.bin": strings.Repeat("x", 1024*1024+1024),
	})
	withProjectCookie(req, env.ProjectDir)

	w := callHandler(ShareTarget, req)
	params := shareTargetRedirectParams(t, w)
	assert.Equal(t, "save", params.Get("share_target"))
}
