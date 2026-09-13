package handler

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"clawbench/internal/frontend"
)

const (
	apkFilename  = "clawbench-android.apk"
	apkEmbedPath = "assets/" + apkFilename
)

// ServeAPK serves the embedded Android APK file for download.
// No authentication required — APK is a public resource.
//
// The APK is always read from the build-time embedded FS, never from disk.
// The APK must match the running binary's version (the upgrade overlay relies
// on it), and frontend.GetFS() would instead return the disk public/ dir
// whenever the server's CWD contains one — shadowing the embedded copy and
// making /api/apk 404 even though the APK is embedded.
func ServeAPK(w http.ResponseWriter, r *http.Request) {
	serveAPK(frontend.EmbeddedFS(), w, r)
}

// serveAPK is the testable core of ServeAPK: the frontend FS is injected so
// tests can supply an in-memory APK without a real --android build.
func serveAPK(fsys fs.FS, w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(fsys, apkEmbedPath)
	if err != nil {
		slog.Debug("apk: not found in embed", "err", err)
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="clawbench-android.apk"`)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, apkFilename, time.Time{}, NewReaderSeeker(data))
}

// ReaderSeeker wraps a byte slice to implement io.ReadSeeker for http.ServeContent.
type ReaderSeeker struct {
	data   []byte
	offset int64
}

func NewReaderSeeker(data []byte) *ReaderSeeker {
	return &ReaderSeeker{data: data}
}

func (rs *ReaderSeeker) Read(p []byte) (int, error) {
	if rs.offset >= int64(len(rs.data)) {
		return 0, io.EOF
	}
	n := copy(p, rs.data[rs.offset:])
	rs.offset += int64(n)
	return n, nil
}

func (rs *ReaderSeeker) Seek(offset int64, whence int) (int64, error) {
	var newOff int64
	switch whence {
	case io.SeekStart:
		newOff = offset
	case io.SeekCurrent:
		newOff = rs.offset + offset
	case io.SeekEnd:
		newOff = int64(len(rs.data)) + offset
	default:
		return 0, fs.ErrInvalid
	}
	if newOff < 0 {
		return 0, fs.ErrInvalid
	}
	rs.offset = newOff
	return newOff, nil
}
