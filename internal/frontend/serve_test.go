package frontend

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const probeBody = "console.log('hello from a compressible asset')\n"

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// assetFS builds a MapFS holding the plain file plus (optionally) its .gz
// sibling, both with the given modification time.
func assetFS(t *testing.T, name string, withGz bool, modTime time.Time) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{
		name: &fstest.MapFile{Data: []byte(probeBody), ModTime: modTime},
	}
	if withGz {
		m[name+precompressedSuffix] = &fstest.MapFile{Data: gzipped(t, probeBody), ModTime: modTime}
	}
	return m
}

func request(t *testing.T, fsys fstest.MapFS, name string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/"+name, http.NoBody)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	ServeFileFromFS(rec, req, fsys, name)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Header().Get("Content-Encoding") != "gzip" {
		return rec.Body.String()
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	return string(out)
}

func TestServeFileFromFS_ServesGzipSiblingWhenAccepted(t *testing.T) {
	fsys := assetFS(t, "main-abc12345.js", true, time.Now())

	rec := request(t, fsys, "main-abc12345.js", map[string]string{"Accept-Encoding": "gzip, deflate, br"})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want it to include Accept-Encoding", got)
	}
	// http.ServeContent drops Content-Length when Content-Encoding is set; the
	// handler restores it from the .gz file so the response is not chunked.
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(len(fsys["main-abc12345.js"+precompressedSuffix].Data)); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	// Content type must describe the original file, not the .gz wrapper.
	//
	// Both spellings are correct for .js: Go's mime table says text/javascript,
	// but Windows resolves the type through the registry, which answers
	// application/javascript. Asserting one spelling makes the test pass on
	// Linux/macOS and fail on Windows for a reason that has nothing to do with
	// the gzip sibling.
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/javascript") && !strings.HasPrefix(got, "application/javascript") {
		t.Fatalf("Content-Type = %q, want a JavaScript media type", got)
	}
	if got := decodeBody(t, rec); got != probeBody {
		t.Fatalf("decoded body = %q, want the original file", got)
	}
}

func TestServeFileFromFS_ServesIdentityWithoutAcceptEncoding(t *testing.T) {
	fsys := assetFS(t, "main-abc12345.js", true, time.Now())

	rec := request(t, fsys, "main-abc12345.js", nil)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if got := rec.Body.String(); got != probeBody {
		t.Fatalf("body = %q, want the original file", got)
	}
	// The identity response must still carry Vary so a shared cache cannot
	// reuse a stored gzip copy for this client.
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want it to include Accept-Encoding", got)
	}
}

func TestServeFileFromFS_RefusesGzipWhenClientOptedOut(t *testing.T) {
	fsys := assetFS(t, "main-abc12345.js", true, time.Now())

	rec := request(t, fsys, "main-abc12345.js", map[string]string{"Accept-Encoding": "gzip;q=0, deflate"})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for gzip;q=0", got)
	}
	if got := rec.Body.String(); got != probeBody {
		t.Fatalf("body = %q, want the identity representation", got)
	}
}

func TestServeFileFromFS_RangeRequestStaysIdentity(t *testing.T) {
	fsys := assetFS(t, "main-abc12345.js", true, time.Now())

	rec := request(t, fsys, "main-abc12345.js", map[string]string{
		"Accept-Encoding": "gzip",
		"Range":           "bytes=0-3",
	})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for a Range request", got)
	}
	if got := rec.Body.String(); got != probeBody[:4] {
		t.Fatalf("body = %q, want the first four identity bytes", got)
	}
}

func TestServeFileFromFS_FallsBackWhenSiblingMissing(t *testing.T) {
	fsys := assetFS(t, "main-abc12345.js", false, time.Now())

	rec := request(t, fsys, "main-abc12345.js", map[string]string{"Accept-Encoding": "gzip"})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
	if got := rec.Body.String(); got != probeBody {
		t.Fatalf("body = %q, want the original file", got)
	}
}

func TestServeFileFromFS_IgnoresStaleSibling(t *testing.T) {
	now := time.Now()
	fsys := fstest.MapFS{
		"index.html":         &fstest.MapFile{Data: []byte(probeBody), ModTime: now},
		"index.html" + ".gz": &fstest.MapFile{Data: gzipped(t, "old build"), ModTime: now.Add(-time.Hour)},
	}

	rec := request(t, fsys, "index.html", map[string]string{"Accept-Encoding": "gzip"})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for a stale sibling", got)
	}
	if got := rec.Body.String(); got != probeBody {
		t.Fatalf("body = %q, want the current file, not the stale gzip copy", got)
	}
}

func TestServeFileFromFS_DoesNotGzipAlreadyCompressedTypes(t *testing.T) {
	// A stray .gz for an image must never be used: images are already
	// compressed and the build never produces these.
	fsys := fstest.MapFS{
		"logo-abc12345.png":         &fstest.MapFile{Data: []byte("not really a png")},
		"logo-abc12345.png" + ".gz": &fstest.MapFile{Data: gzipped(t, "not really a png")},
	}

	rec := request(t, fsys, "logo-abc12345.png", map[string]string{"Accept-Encoding": "gzip"})

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty for an image", got)
	}
}

func TestServeFileFromFS_EntryHTMLStaysUncachedOnGzipPath(t *testing.T) {
	fsys := assetFS(t, "index.html", true, time.Now())

	rec := request(t, fsys, "index.html", map[string]string{"Accept-Encoding": "gzip"})

	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", got)
	}
	if got := decodeBody(t, rec); got != probeBody {
		t.Fatalf("decoded body = %q, want the original file", got)
	}
}

func TestServeFileFromFS_MissingFileIs404(t *testing.T) {
	fsys := assetFS(t, "main-abc12345.js", true, time.Now())

	rec := request(t, fsys, "nope.js", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"GZIP", true},
		{"gzip, deflate, br", true},
		{"deflate, gzip;q=0.5", true},
		{"gzip;q=0", false},
		{"gzip;q=0.000", false},
		{"br, *", true},
		{"br, *;q=0", false},
		{"identity", false},
	}
	for _, c := range cases {
		if got := acceptsGzip(c.header); got != c.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", c.header, got, c.want)
		}
	}
}
