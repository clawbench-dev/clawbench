package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// LiveNoteRecordingDirName is the folder inside a meeting directory that holds
// screen recordings. Kept beside audio/ and shots/ so deleting the meeting
// removes everything with one os.RemoveAll.
const LiveNoteRecordingDirName = "recording"

// LiveNoteRecordingMaxBytes bounds one recording file.
//
// A server-side ceiling, not a product rule: at the client's ~2.5 Mbps this is
// several hours, far beyond any real meeting. It exists so a buggy or hostile
// client cannot fill the disk through an endpoint that is otherwise
// authenticated.
const LiveNoteRecordingMaxBytes int64 = 4 << 30

// liveNoteRecordingIDPattern is what a client-generated recording id may
// contain. The id ends up in a file name, so it is validated here rather than
// sanitized: a caller sending anything else is a bug worth surfacing.
var liveNoteRecordingIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidLiveNoteRecordingID reports whether an id is safe to put in a file name.
func ValidLiveNoteRecordingID(id string) bool {
	return liveNoteRecordingIDPattern.MatchString(id)
}

// EnsureLiveNoteRecordingDir creates <meeting>/recording and keeps the video
// files out of git, the same way audio/ and shots/ do.
func EnsureLiveNoteRecordingDir(paths LiveNotesPaths) error {
	dir := filepath.Join(paths.Dir, LiveNoteRecordingDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("live notes: create recording dir: %w", err)
	}
	ignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(ignore); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(ignore, []byte("*\n"), 0o644)
	}
	return nil
}

// LiveNoteRecordingFileName names one recording inside the meeting directory.
//
// The start time sorts the files in the order they were made, and the client id
// keeps two recordings started in the same millisecond from overwriting each
// other. The extension is always webm: the recorder only ever produces that
// container (or the engine's default, which is still WebM in every Chromium-
// and Firefox-based host this app runs in).
func LiveNoteRecordingFileName(startMS int64, id string) string {
	if startMS <= 0 {
		startMS = time.Now().UnixMilli()
	}
	return fmt.Sprintf("%s/%013d-%s.webm", LiveNoteRecordingDirName, startMS, id)
}
