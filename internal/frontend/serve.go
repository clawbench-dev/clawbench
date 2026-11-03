package frontend

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ServeFileFromFS serves a single file from an fs.FS using http.ServeContent
// for proper Content-Type detection and conditional request handling.
// Unlike http.ServeFile, this works with both disk-backed (os.DirFS) and
// embedded (embed.FS) filesystems.
//
// When the client accepts gzip and a build-time `.gz` sibling exists, the
// compressed copy is served instead (see compress.go). Everything else about
// the response — type, validators, status codes — is derived from the original
// file name, so callers do not need to know which representation was chosen.
func ServeFileFromFS(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	f, err := fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if stat.IsDir() {
		http.NotFound(w, r)
		return
	}

	// The entry HTML must never be cached: it references content-hashed asset
	// filenames, so a cached index.html would keep serving stale JS/CSS after a
	// rebuild (the Android WebView uses LOAD_DEFAULT and otherwise serves the
	// old index.html indefinitely). Force revalidation on every load.
	if strings.HasSuffix(strings.ToLower(name), ".html") {
		w.Header().Set("Cache-Control", "no-cache")
	}

	if gz, gzInfo, ok := openPrecompressed(fsys, r, name, stat); ok {
		defer func() { _ = gz.Close() }()
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Set("Content-Encoding", "gzip")
		// http.ServeContent skips Content-Length whenever the caller set
		// Content-Encoding (it assumes an on-the-fly encoder with an unknown
		// final size). We know the exact encoded size — it is a file — so set it
		// and let the response use a length instead of chunked framing.
		w.Header().Set("Content-Length", strconv.FormatInt(gzInfo.Size(), 10))
		serveFileContent(w, r, name, gzInfo.ModTime(), gz)
		return
	}

	// Identity response for a compressible file still varies by encoding: a
	// shared cache that stored the gzip copy must not hand it to a client that
	// cannot decode it (and vice versa).
	if isCompressible(name) {
		w.Header().Add("Vary", "Accept-Encoding")
	}

	serveFileContent(w, r, name, stat.ModTime(), f)
}

// serveFileContent writes f through http.ServeContent, which needs an
// io.ReadSeeker for Range support and Content-Length. Files that cannot seek
// are buffered first — they are small enough for that to be free.
func serveFileContent(w http.ResponseWriter, r *http.Request, name string, modTime time.Time, f fs.File) {
	if seeker, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, modTime, seeker)
		return
	}

	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, name, modTime, bytes.NewReader(data))
}
