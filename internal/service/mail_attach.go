package service

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"clawbench/internal/model"
)

// ErrMailAttachmentMissing means the path handed to the composer no longer
// exists (or is not something that can be attached).
var ErrMailAttachmentMissing = errors.New("mail: attachment source does not exist")

// ErrMailAttachmentTooLarge means the attachment (or the zip built from a
// directory) is over the per-attachment limit.
var ErrMailAttachmentTooLarge = errors.New("mail: attachment exceeds the size limit")

// MailStagedAttachment is a file the composer can attach, described the same
// way an uploaded attachment is: a name, an absolute path SendMail can read,
// and the size shown in the composer's chip.
type MailStagedAttachment struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
}

// mailOutboxDir holds zips built from directories the user mails. It lives
// under the data directory (not the project) because these files are the app's
// scratch output, not the user's content; they are removed once the mail has
// been sent.
func mailOutboxDir() string {
	return filepath.Join(model.DataDir, "mail", "outbox")
}

// mailOutboxMaxAge is how long a staged zip may sit unused. A zip is removed
// when its mail is sent, but a composer the user abandons leaves it behind —
// this sweep keeps the directory from growing without bound.
//
// A week, not an hour: a composer left open overnight (staged the folder, got
// distracted, came back the next day) is a normal thing to do, and deleting its
// attachment would turn the eventual send into "file not found".
const mailOutboxMaxAge = 7 * 24 * time.Hour

// StageMailAttachments turns absolute paths into attachments.
//
// A regular file is attached in place — no copy, so a large file costs nothing
// extra and the user's edits are what gets mailed. A directory is packed into
// one zip under the outbox, because a mail attachment has to be a file; the
// archive keeps the directory itself as its top-level entry, matching what the
// file manager's "打包下载" produces.
func StageMailAttachments(absPaths []string) ([]MailStagedAttachment, error) {
	sweepMailOutbox()
	out := make([]MailStagedAttachment, 0, len(absPaths))
	for _, p := range absPaths {
		att, err := stageMailAttachment(p)
		if err != nil {
			return nil, err
		}
		out = append(out, att)
	}
	return out, nil
}

// sweepMailOutbox removes abandoned archives. Failures are ignored: a zip that
// cannot be removed is harmless, and it must not fail the user's send.
func sweepMailOutbox() {
	entries, err := os.ReadDir(mailOutboxDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-mailOutboxMaxAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(mailOutboxDir(), e.Name()))
	}
}

func stageMailAttachment(absPath string) (MailStagedAttachment, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		return MailStagedAttachment{}, ErrMailAttachmentMissing
	}
	// Anything that is neither a directory nor a regular file (a socket, a
	// device node) cannot be mailed, and is reported the same way as a path
	// that is gone.
	if !info.IsDir() && !info.Mode().IsRegular() {
		return MailStagedAttachment{}, ErrMailAttachmentMissing
	}
	if info.IsDir() {
		return stageDirAsZip(absPath)
	}
	if info.Size() > mailMaxAttachmentBytes {
		return MailStagedAttachment{}, ErrMailAttachmentTooLarge
	}
	return MailStagedAttachment{
		Filename: sanitizeMailFilename(filepath.Base(absPath)),
		Path:     absPath,
		Size:     info.Size(),
	}, nil
}

// stageDirAsZip packs absDir into a zip inside the outbox. The walk is capped
// at the attachment limit while writing, so a huge tree fails fast instead of
// filling the disk first.
func stageDirAsZip(absDir string) (MailStagedAttachment, error) {
	if err := os.MkdirAll(mailOutboxDir(), 0o755); err != nil {
		return MailStagedAttachment{}, fmt.Errorf("mail: create outbox: %w", err)
	}
	base := sanitizeMailFilename(filepath.Base(absDir))
	f, err := os.CreateTemp(mailOutboxDir(), base+"-*.zip")
	if err != nil {
		return MailStagedAttachment{}, fmt.Errorf("mail: create archive: %w", err)
	}
	fail := func(err error) (MailStagedAttachment, error) {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return MailStagedAttachment{}, err
	}

	capped := &cappedWriter{w: f, max: mailMaxAttachmentBytes}
	zw := zip.NewWriter(capped)
	walkErr := filepath.Walk(absDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Symlinks are skipped rather than followed: a link can point outside
		// the tree the user picked, and a loop would never finish.
		if fi.Mode()&os.ModeSymlink != 0 {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(absDir), path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if fi.IsDir() {
			_, err = zw.Create(rel + "/")
			return err
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		return writeZipFile(zw, path, rel, fi)
	})
	// Close even after a walk error: it flushes the central directory, and a
	// failure there is irrelevant once we are discarding the archive.
	closeErr := zw.Close()
	if walkErr == nil {
		walkErr = closeErr
	}
	if walkErr != nil {
		if errors.Is(walkErr, ErrMailAttachmentTooLarge) {
			return fail(ErrMailAttachmentTooLarge)
		}
		return fail(walkErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		return fail(closeErr)
	}
	info, err := os.Stat(f.Name())
	if err != nil {
		return fail(err)
	}
	return MailStagedAttachment{
		Filename: base + ".zip",
		Path:     f.Name(),
		Size:     info.Size(),
	}, nil
}

// writeZipFile adds one regular file to the archive.
func writeZipFile(zw *zip.Writer, absPath, name string, fi os.FileInfo) error {
	fh, err := zip.FileInfoHeader(fi)
	if err != nil {
		return err
	}
	fh.Name = name
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

// cappedWriter fails the write that would exceed max, so an oversized archive
// stops at the limit instead of being built to completion.
type cappedWriter struct {
	w   io.Writer
	n   int64
	max int64
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.n+int64(len(p)) > c.max {
		return 0, ErrMailAttachmentTooLarge
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// cleanupStagedMailAttachments removes zips this request created. Only files
// sitting directly in the outbox are touched: an attachment that is an
// ordinary project file must survive the send.
func cleanupStagedMailAttachments(atts []MailOutgoingAttachment) {
	outbox := filepath.Clean(mailOutboxDir())
	for _, a := range atts {
		p := filepath.Clean(a.Path)
		if filepath.Dir(p) == outbox {
			_ = os.Remove(p)
		}
	}
}
