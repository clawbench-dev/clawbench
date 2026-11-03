// Package service — archiving the files a meeting looked at.
//
// The transcript records WHEN a file was opened; this file keeps WHAT was in it.
// A meeting's resources are read months later, from the meeting folder, so a
// path pointing at the publisher's disk would rot the moment the project moves.
// The copy is the resource.
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// LiveNoteFileArchiveDir is the subfolder of the meeting folder that holds
	// the copies.
	LiveNoteFileArchiveDir = "files"
	// LiveNoteFileCopyMaxBytes caps ONE copied file. Bigger than any document a
	// meeting is about, small enough that a stray video does not fill the disk.
	LiveNoteFileCopyMaxBytes = 5 << 20
	// LiveNoteFileCopyMaxCount caps the copies per meeting. A meeting opens tens
	// of files; a client bug that reports thousands must not copy them all.
	LiveNoteFileCopyMaxCount = 40
	// LiveNoteFileManifestName is the human-readable timeline written beside the
	// copies. It is also what the resources tab lists first.
	LiveNoteFileManifestName = "files.md"
	// liveNoteFileExcerptMaxFiles / Runes bound what the summariser reads.
	liveNoteFileExcerptMaxFiles = 5
	liveNoteFileExcerptMaxRunes = 1500
	liveNoteFileExcerptBudget   = 8000
)

// ErrLiveNoteFileTooBig reports a file skipped because of its size.
var ErrLiveNoteFileTooBig = errors.New("live notes: file too large to archive")

// ArchiveLiveNoteFiles copies the meeting's opened files into
// <meeting>/files/ and writes files.md.
//
// Failures are per file, never fatal: one unreadable file must not cost the
// meeting its transcript and minutes. The manifest records what happened, so a
// skipped file is visible rather than silently missing.
func ArchiveLiveNoteFiles(paths LiveNotesPaths, projectPath string, files []LiveNoteFile, startedAt time.Time) (string, error) {
	if strings.TrimSpace(paths.Dir) == "" {
		return "", errors.New("live notes: no project path")
	}
	if len(files) == 0 {
		return "", nil
	}
	ordered := make([]LiveNoteFile, len(files))
	copy(ordered, files)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OpenedAt < ordered[j].OpenedAt })

	archiveDir := filepath.Join(paths.Dir, LiveNoteFileArchiveDir)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("live notes: create files dir: %w", err)
	}

	type row = liveNoteFileRow
	rows := make([]liveNoteFileRow, 0, len(ordered))
	seen := make(map[string]string) // sha256 -> archived name
	copied := 0
	for _, f := range ordered {
		if f.OpenedAt <= 0 {
			continue
		}
		r := row{file: f}
		source := resolveLiveNoteFilePath(projectPath, f.Path)
		switch {
		case source == "":
			r.reason = liveNoteT("MeetingFilesReasonUnknownPath")
		case copied >= LiveNoteFileCopyMaxCount:
			r.reason = liveNoteT("MeetingFilesReasonTooMany")
		default:
			name, digest, err := copyLiveNoteFile(archiveDir, source)
			if err != nil {
				r.reason = liveNoteArchiveReason(err)
				break
			}
			if prev, dup := seen[digest]; dup {
				// The copy was written before the digest was known; drop it so
				// the folder holds one copy per distinct content, as promised.
				_ = os.Remove(filepath.Join(archiveDir, name))
				r.archive = LiveNoteFileArchiveDir + "/" + prev
				r.reason = liveNoteT("MeetingFilesReasonDuplicate")
				break
			}
			seen[digest] = name
			r.archive = LiveNoteFileArchiveDir + "/" + name
			copied++
		}
		rows = append(rows, r)
	}

	manifest := liveNoteFileManifest(rows, startedAt)
	if err := os.WriteFile(filepath.Join(paths.Dir, LiveNoteFileManifestName), []byte(manifest), 0o644); err != nil {
		return "", fmt.Errorf("live notes: write files manifest: %w", err)
	}
	// Nothing was copied: an empty directory would show up in the resources tab
	// as a folder that opens to nothing.
	if copied == 0 {
		_ = os.Remove(archiveDir)
	}
	return filepath.Join(paths.Dir, LiveNoteFileManifestName), nil
}

// liveNoteFileRow is one manifest line: the interval, the copy (when there is
// one) and why there is not.
type liveNoteFileRow struct {
	file    LiveNoteFile
	archive string // relative to the meeting folder, "" when not copied
	reason  string
}

// liveNoteFileManifest renders the timeline table.
func liveNoteFileManifest(rows []liveNoteFileRow, startedAt time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", liveNoteT("MeetingFilesManifestTitle",
		map[string]interface{}{"Time": startedAt.Format("2006-01-02 15:04")}))
	fmt.Fprintf(&b, "> %s\n\n", liveNoteT("MeetingFilesManifestNote"))
	fmt.Fprintf(&b, "| %s | %s | %s | %s |\n|---|---|---|---|\n",
		liveNoteT("MeetingFilesColumnTime"), liveNoteT("MeetingFilesColumnFile"),
		liveNoteT("MeetingFilesColumnStay"), liveNoteT("MeetingFilesColumnArchive"))
	for _, r := range rows {
		label := strings.TrimSpace(r.file.Path)
		if label == "" {
			label = r.file.Name
		}
		span := liveNoteClock(r.file.OpenedAt)
		if r.file.ClosedAt > r.file.OpenedAt {
			span += "–" + liveNoteClock(r.file.ClosedAt)
		} else {
			span += " " + liveNoteT("MeetingFilesSince")
		}
		stay := "—"
		if r.file.ClosedAt > r.file.OpenedAt {
			stay = liveNoteDurationValue(r.file.ClosedAt - r.file.OpenedAt)
		}
		target := r.archive
		if target == "" {
			target = "—"
		}
		if r.reason != "" {
			target += liveNoteT("MeetingFilesReasonWrap", map[string]interface{}{"Text": r.reason})
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", span, liveNoteEscapeTable(label), stay, liveNoteEscapeTable(target))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// liveNoteEscapeTable keeps a path from breaking the Markdown table it sits in.
func liveNoteEscapeTable(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(s)
}

// resolveLiveNoteFilePath turns a stored path into a readable absolute path.
//
// The client sends a project-relative path for files inside the project and an
// absolute one otherwise; both have to work, and an empty path (a viewer's copy
// of the row) has none.
func resolveLiveNoteFilePath(projectPath, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	if strings.TrimSpace(projectPath) == "" {
		return ""
	}
	return filepath.Join(projectPath, p)
}

// copyLiveNoteFile copies one file into dir, returning its name and digest.
//
// The name is the base name, suffixed on collision: two files called "index.ts"
// from different folders are a normal meeting, not an error.
func copyLiveNoteFile(dir, source string) (name, digest string, err error) {
	info, err := os.Stat(source)
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("%s is not a regular file", source)
	}
	if info.Size() > LiveNoteFileCopyMaxBytes {
		return "", "", ErrLiveNoteFileTooBig
	}

	in, err := os.Open(source)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = in.Close() }()

	base := liveNoteFileBase(source)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "file"
	}
	target := uniqueLiveNoteFileTarget(dir, base)

	out, err := os.Create(target)
	if err != nil {
		return "", "", err
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hasher), io.LimitReader(in, LiveNoteFileCopyMaxBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return "", "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return "", "", closeErr
	}
	if written > LiveNoteFileCopyMaxBytes {
		_ = os.Remove(target)
		return "", "", ErrLiveNoteFileTooBig
	}
	return filepath.Base(target), hex.EncodeToString(hasher.Sum(nil)), nil
}

// uniqueLiveNoteFileTarget returns a path inside dir that does not exist yet.
func uniqueLiveNoteFileTarget(dir, base string) string {
	target := filepath.Join(dir, base)
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return target
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i < 1000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext))
}

func liveNoteArchiveReason(err error) string {
	switch {
	case errors.Is(err, ErrLiveNoteFileTooBig):
		return liveNoteT("MeetingFilesReasonTooBig")
	case errors.Is(err, os.ErrNotExist):
		return liveNoteT("MeetingFilesReasonMissing")
	case errors.Is(err, os.ErrPermission):
		return liveNoteT("MeetingFilesReasonPermission")
	default:
		return liveNoteT("MeetingFilesReasonUnreadable")
	}
}
