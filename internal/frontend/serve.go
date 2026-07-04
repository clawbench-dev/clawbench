package frontend

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
)

// ServeFileFromFS serves a single file from an fs.FS using http.ServeContent
// for proper Content-Type detection and conditional request handling.
// Unlike http.ServeFile, this works with both disk-backed (os.DirFS) and
// embedded (embed.FS) filesystems.
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

	// If the file implements io.ReadSeeker, use it directly (zero-copy).
	// embed.FS files implement ReadSeeker, so this avoids buffering the
	// entire file into memory for the common embed-only deployment.
	if seeker, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, stat.ModTime(), seeker)
		return
	}

	// Fallback: buffer the file for non-seekable FS (some os.DirFS platforms).
	// http.ServeContent requires a ReadSeeker for Range request handling.
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.ServeContent(w, r, name, stat.ModTime(), bytes.NewReader(data))
}
