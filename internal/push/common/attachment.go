package common

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clawbench/internal/model"
)

// uploadsSubdir is where IM attachments land inside a project. It matches the
// web upload endpoint's default directory so the AI prompt, the file manager
// and the attachment drawer all treat IM files like any other upload.
const uploadsSubdir = ".clawbench/uploads"

// AttachmentDir returns the directory an IM attachment should be written to
// for the given project, creating it if needed.
func AttachmentDir(projectPath string) (string, error) {
	if projectPath == "" {
		return "", fmt.Errorf("no project path for session")
	}
	dir := filepath.Join(projectPath, ".clawbench", "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create uploads dir: %w", err)
	}
	return dir, nil
}

// AttachmentMaxBytes is the download size cap for an IM attachment. It reuses
// the web upload limit so a user who tightened that setting also bounds what
// the bot will pull down from IM; model.UploadMaxSizeMB is populated from
// config at startup and defaults to 100 when the config was never loaded.
func AttachmentMaxBytes() int64 {
	mb := model.UploadMaxSizeMB
	if mb <= 0 {
		mb = 100
	}
	return int64(mb) * 1024 * 1024
}

// AttachmentMaxFiles is the per-message cap on how many attachments the bot
// will download. It reuses the web upload limit for the same reason as
// AttachmentMaxBytes: one setting bounds both paths. A rich-text IM message can
// embed an arbitrary number of images, and each download is a separate API call
// plus a file on disk, so the count needs its own bound.
func AttachmentMaxFiles() int {
	n := model.UploadMaxFiles
	if n <= 0 {
		n = 20
	}
	return n
}

// ErrAttachmentTooLarge is returned when a download exceeds AttachmentMaxBytes.
var ErrAttachmentTooLarge = fmt.Errorf("attachment exceeds the size limit")

// SanitizeFilename makes a remote-provided filename safe to join onto a path
// and to store as a project-relative attachment.
//
// It strips any directory component (a remote name like "../../etc/passwd"
// must not escape the uploads directory), replaces characters that are
// troublesome on Windows or in shell contexts, and falls back to a generated
// name when nothing usable remains. The extension is preserved because the AI
// tooling and the file viewer both key off it.
func SanitizeFilename(name string) string {
	// A remote filename may use either separator; take the base of both so
	// "../../x" and "..\\..\\x" are both reduced to "x".
	name = strings.ReplaceAll(name, "\\", "/")
	name = pathBase(name)

	name = strings.Map(func(r rune) rune {
		switch r {
		case 0, '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			return '_'
		}
		return r
	}, name)

	name = strings.Trim(name, " .")
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	// Guard against a name so long the filesystem rejects it. The extension is
	// kept; 120 bytes leaves room for the collision suffix added later.
	if len(name) > 120 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		keep := 120 - len(ext)
		if keep < 1 {
			keep = 1
		}
		name = name[:keep] + ext
	}
	return name
}

// pathBase is filepath.Base for slash-separated input, independent of the host
// separator so a Windows-authored remote name is handled the same everywhere.
func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// SaveAttachment writes r to a new file under the project's uploads directory
// and returns the project-relative FileEntry for the prompt. maxBytes bounds
// the write; exceeding it removes the partial file and returns
// ErrAttachmentTooLarge.
//
// The returned Path is slash-separated and project-relative (e.g.
// ".clawbench/uploads/report.pdf"), matching what the web upload endpoint
// returns so every downstream consumer treats it identically.
func SaveAttachment(projectPath, filename string, r io.Reader, maxBytes int64) (model.FileEntry, error) {
	dir, err := AttachmentDir(projectPath)
	if err != nil {
		return model.FileEntry{}, err
	}

	f, dst, err := createAttachmentFile(dir, SanitizeFilename(filename))
	if err != nil {
		return model.FileEntry{}, err
	}
	// The handle must be closed before any os.Remove below: Windows refuses to
	// delete a file that is still open, so relying on the deferred Close would
	// silently leave the partial file behind there (the deferred call runs
	// after the Remove). closed tracks that so the defer stays a no-op once we
	// have already closed it.
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
	}()
	closeAndRemove := func() {
		_ = f.Close()
		closed = true
		_ = os.Remove(dst)
	}

	if maxBytes <= 0 {
		maxBytes = AttachmentMaxBytes()
	}
	// Read one byte past the limit so "exactly at the limit" succeeds while
	// anything larger is detected rather than silently truncated.
	written, err := io.Copy(f, io.LimitReader(r, maxBytes+1))
	if err != nil {
		closeAndRemove()
		return model.FileEntry{}, fmt.Errorf("write attachment: %w", err)
	}
	if written > maxBytes {
		closeAndRemove()
		return model.FileEntry{}, ErrAttachmentTooLarge
	}

	rel := filepath.ToSlash(filepath.Join(uploadsSubdir, filepath.Base(dst)))
	return model.FileEntry{Path: rel}, nil
}

// createAttachmentFile creates filename inside dir without overwriting an
// existing file, adding a numeric suffix on collision.
//
// It uses O_EXCL rather than a stat-then-create pair because media downloads
// run concurrently: two files sent in quick succession (or the same name from
// two chats) would otherwise both observe "no such file" and the second create
// would clobber the first. O_EXCL makes the check and the create atomic.
func createAttachmentFile(dir, filename string) (*os.File, string, error) {
	ext := filepath.Ext(filename)
	stem := strings.TrimSuffix(filename, ext)

	candidate := filepath.Join(dir, filename)
	for i := range 10000 {
		if i > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s_%d%s", stem, i, ext))
		}
		//nolint:gosec // G302: attachments are user-visible project files, same mode as the web upload endpoint
		f, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, candidate, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, "", fmt.Errorf("create attachment: %w", err)
		}
	}
	return nil, "", fmt.Errorf("create attachment: no free filename for %q", filename)
}

// DownloadAttachment fetches url and saves it as an attachment of the given
// project, enforcing AttachmentMaxBytes. The HTTP client is passed in so
// callers can reuse their configured client (timeouts, transports).
func DownloadAttachment(client *http.Client, url, projectPath, filename string) (model.FileEntry, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("attachment request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return model.FileEntry{}, fmt.Errorf("attachment fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return model.FileEntry{}, fmt.Errorf("attachment fetch: status %d", resp.StatusCode)
	}

	// A declared Content-Length over the limit lets us reject before reading.
	if resp.ContentLength > 0 && resp.ContentLength > AttachmentMaxBytes() {
		return model.FileEntry{}, ErrAttachmentTooLarge
	}

	entry, err := SaveAttachment(projectPath, filename, resp.Body, AttachmentMaxBytes())
	if err != nil {
		return model.FileEntry{}, err
	}
	slog.Debug("push: attachment saved", "path", entry.Path, "bytes", resp.ContentLength)
	return entry, nil
}
