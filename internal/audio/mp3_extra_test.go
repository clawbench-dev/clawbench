package audio

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"testing/iotest"
)

// writeTinyMP3 drops the embedded fixture into dir and returns its path.
func writeTinyMP3(t *testing.T, dir, name string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(tinyMP3Base64)
	if err != nil {
		t.Fatalf("fixture must decode: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// dirNames lists the entry names in dir, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// TestDecodeMP3ToPCMFile_MissingSource covers the open failure: a caller that
// asks for a source the cache has already evicted must get an error naming the
// open, not a zero Format that looks like a successful silent decode.
func TestDecodeMP3ToPCMFile_MissingSource(t *testing.T) {
	dir := t.TempDir()
	_, err := DecodeMP3ToPCMFile(filepath.Join(dir, "absent.mp3"), filepath.Join(dir, "out.pcm"))
	if err == nil {
		t.Fatal("expected an error for a missing source")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want it to wrap ErrNotExist", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out.pcm")); !os.IsNotExist(statErr) {
		t.Error("a failed decode must not publish an output file")
	}
}

// TestDecodeMP3ToPCMFile_OutputDirUnusable covers a destination whose parent
// path is a regular file: MkdirAll fails and nothing may be written.
func TestDecodeMP3ToPCMFile_OutputDirUnusable(t *testing.T) {
	dir := t.TempDir()
	src := writeTinyMP3(t, dir, "in.mp3")

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	_, err := DecodeMP3ToPCMFile(src, filepath.Join(blocker, "out.pcm"))
	if err == nil {
		t.Fatal("expected an error when the output directory cannot be created")
	}
}

// TestDecodeMP3ToPCMFile_OutputDirNotWritable covers the temp-file creation
// failure. It needs a non-root user, because root ignores the mode bits.
func TestDecodeMP3ToPCMFile_OutputDirNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions, so CreateTemp cannot fail")
	}

	dir := t.TempDir()
	src := writeTinyMP3(t, dir, "in.mp3")

	outDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(outDir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o700) })

	_, err := DecodeMP3ToPCMFile(src, filepath.Join(outDir, "out.pcm"))
	if err == nil {
		t.Fatal("expected an error when the output directory is not writable")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want it to wrap ErrPermission", err)
	}
}

// TestDecodeMP3ToPCMFile_RenameFails covers a destination that is an existing
// directory: the decode itself succeeds, the publish does not, and the temp file
// must be cleaned up rather than left behind in the cache directory.
func TestDecodeMP3ToPCMFile_RenameFails(t *testing.T) {
	dir := t.TempDir()
	src := writeTinyMP3(t, dir, "in.mp3")

	dst := filepath.Join(dir, "out.pcm")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}

	_, err := DecodeMP3ToPCMFile(src, dst)
	if err == nil {
		t.Fatal("expected an error when the destination is a directory")
	}

	names := dirNames(t, dir)
	if len(names) != 2 || names[0] != "in.mp3" || names[1] != "out.pcm" {
		t.Fatalf("leftover files after a failed publish: %v", names)
	}
}

// TestDownmixToMono_AveragesAndCarriesPartialFrames covers the frame-alignment
// contract: a reader that splits a stereo frame across reads must still produce
// one averaged mono sample per input frame, and a trailing partial frame must be
// dropped rather than written half-formed.
func TestDownmixToMono_AveragesAndCarriesPartialFrames(t *testing.T) {
	// (16,48) → 32, (0,0) → 0, (-1,1) → 0, then two stray bytes.
	in := []byte{
		0x10, 0x00, 0x30, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0xFF, 0xFF, 0x01, 0x00,
		0x7F, 0x7F,
	}
	var out bytes.Buffer

	// OneByteReader guarantees every read ends inside a frame.
	if err := downmixToMono(iotest.OneByteReader(bytes.NewReader(in)), &out); err != nil {
		t.Fatalf("downmixToMono: %v", err)
	}

	want := []byte{0x20, 0x00, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("pcm = % X, want % X", out.Bytes(), want)
	}
}

// TestDownmixToMono_WriteError covers a destination that stops accepting bytes:
// the decode must fail instead of reporting a complete file.
func TestDownmixToMono_WriteError(t *testing.T) {
	in := []byte{0x10, 0x00, 0x30, 0x00}
	err := downmixToMono(bytes.NewReader(in), failingWriter{})
	if err == nil || !errors.Is(err, errWriteFailed) {
		t.Fatalf("want a write error, got %v", err)
	}
}

// TestDownmixToMono_ReadError covers a source that dies mid-decode.
func TestDownmixToMono_ReadError(t *testing.T) {
	err := downmixToMono(iotest.ErrReader(errReadFailed), io.Discard)
	if err == nil || !errors.Is(err, errReadFailed) {
		t.Fatalf("want a read error, got %v", err)
	}
}

var (
	errWriteFailed = errors.New("writer is closed")
	errReadFailed  = errors.New("source disappeared")
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriteFailed }
