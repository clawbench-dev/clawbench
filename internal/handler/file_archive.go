package handler

import (
	"archive/zip"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// maxArchivePaths limits the number of paths in a single archive request.
const maxArchivePaths = 1000

// ServeFileArchive handles POST /api/file/archive
// Accepts { paths: ["rel/path1", "rel/path2"] } and streams a zip archive.
// Paths can be files or directories; each is walked and added to the zip.
func ServeFileArchive(w http.ResponseWriter, r *http.Request) { //nolint:gocognit,gocyclo // multi-format archive creation
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "MissingPath")
		return
	}
	if len(req.Paths) > maxArchivePaths {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ArchiveFailed")
		return
	}

	// Resolve all paths to absolute, validate access
	type absEntry struct {
		absPath string
		relPath string // original relative path for zip entry prefix
	}
	var entries []absEntry
	for _, p := range req.Paths {
		absPath, ok := resolveAbsPath(w, r, p)
		if !ok {
			return
		}
		entries = append(entries, absEntry{absPath: absPath, relPath: p})
	}

	// Pre-validate: at least one path must be accessible
	accessible := 0
	for _, entry := range entries {
		if _, err := os.Stat(entry.absPath); err == nil {
			accessible++
		}
	}
	if accessible == 0 {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ArchiveFailed")
		return
	}

	// Compute a friendly zip filename from the first entry
	zipName := "archive.zip"
	if len(entries) == 1 {
		base := filepath.Base(entries[0].relPath)
		base = strings.TrimRight(base, "/")
		if base != "" && base != "." {
			zipName = base + ".zip"
		}
	}
	// Set response headers before writing any data
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(zipName))
	w.Header().Set("Cache-Control", "no-store")

	// Stream zip directly to response writer
	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	written := 0
	for _, entry := range entries {
		info, err := os.Stat(entry.absPath)
		if err != nil {
			slog.Warn("archive: skip missing path", "path", entry.absPath, "err", err)
			continue
		}

		if info.IsDir() {
			err := filepath.Walk(entry.absPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}

				// Skip symlinks that escape root paths (prevent traversal & infinite loops)
				if fi.Mode()&os.ModeSymlink != 0 {
					target, linkErr := filepath.EvalSymlinks(path)
					if linkErr != nil || !isPathUnderAnyRoot(target) {
						slog.Warn("archive: skip symlink escaping watchDir", "path", path)
						if fi.IsDir() {
							return filepath.SkipDir
						}
						return nil
					}
				}

				rel, err := filepath.Rel(filepath.Dir(entry.absPath), path)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)

				if fi.IsDir() {
					_, err = zw.Create(rel + "/")
					return err
				}
				return addFileToZip(zw, path, rel, fi)
			})
			if err != nil {
				slog.Warn("archive: walk error", "dir", entry.absPath, "err", err)
			}
		} else {
			rel := filepath.Base(entry.absPath)
			if len(entries) > 1 {
				parentRel := filepath.Dir(entry.relPath)
				if parentRel != "." {
					rel = filepath.ToSlash(parentRel) + "/" + filepath.Base(entry.absPath)
				}
			}
			if err := addFileToZip(zw, entry.absPath, rel, info); err != nil {
				slog.Warn("archive: add file error", "path", entry.absPath, "err", err)
			}
		}
		written++
	}

	if written == 0 {
		slog.Warn("archive: no files written")
	}
}

// sanitizeArchiveName removes or replaces characters that could break
// the Content-Disposition header (quotes, backslashes, control chars).
func sanitizeArchiveName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' || r < 0x20 {
			return '_'
		}
		return r
	}, name)
}

// contentDispositionAttachment builds a Content-Disposition header value
// with both filename (ASCII fallback) and filename* (RFC 5987/6267).
// This ensures CJK and other non-ASCII filenames display correctly
// across browsers, proxies, and the Android WebView.
func contentDispositionAttachment(name string) string {
	safe := sanitizeArchiveName(name)
	encoded := rfc5987Encode(name)
	return fmt.Sprintf(`attachment; filename=%q; filename*=UTF-8''%s`, safe, encoded)
}

// rfc5987Encode percent-encodes a string per RFC 5987.
// ASCII alphanums and !#$&+-.^_`|~ are left unencoded.
func rfc5987Encode(name string) string {
	var encoded strings.Builder
	encoded.Grow(len(name) * 3)
	for _, r := range name {
		if isRFC5987Unreserved(r) {
			encoded.WriteRune(r)
		} else {
			var buf [4]byte
			n := utf8.EncodeRune(buf[:], r)
			for i := range n {
				fmt.Fprintf(&encoded, "%%%02X", buf[i])
			}
		}
	}
	return encoded.String()
}

// rfc5987Unreserved is the set of unreserved characters per RFC 5987
// (ASCII alphanums plus !#$&+-.^_`|~).
var rfc5987Unreserved = func() map[rune]bool {
	m := make(map[rune]bool, 26+26+10+15)
	for r := 'a'; r <= 'z'; r++ {
		m[r] = true
	}
	for r := 'A'; r <= 'Z'; r++ {
		m[r] = true
	}
	for r := '0'; r <= '9'; r++ {
		m[r] = true
	}
	for _, r := range "!#$&+-.^_`|~" {
		m[r] = true
	}
	return m
}()

// isRFC5987Unreserved reports whether r is an unreserved character per RFC 5987.
func isRFC5987Unreserved(r rune) bool {
	return rfc5987Unreserved[r]
}

// addFileToZip adds a single file to the zip writer.
func addFileToZip(zw *zip.Writer, absPath, zipRelPath string, fi os.FileInfo) error {
	fh, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}
	fh.Name = zipRelPath
	fh.Method = zip.Deflate

	w, err := zw.CreateHeader(fh)
	if err != nil {
		return err
	}

	f, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = io.Copy(w, f)
	return err
}
