package audio

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// tinyMP3Base64 is a 0.3 s 440 Hz sine, 24 kHz mono, 48 kbps MP3 generated with
// ffmpeg. It is embedded so the decode test needs no external tools or fixture
// files.
const tinyMP3Base64 = "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"

func TestDecodeMP3ToPCMFile(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(tinyMP3Base64)
	if err != nil {
		t.Fatalf("fixture must decode: %v", err)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "in.mp3")
	dst := filepath.Join(dir, "out.pcm")
	if err := os.WriteFile(src, raw, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	format, err := DecodeMP3ToPCMFile(src, dst)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if format.SampleRate != 24000 {
		t.Errorf("sample rate = %d, want 24000", format.SampleRate)
	}
	if format.Channels != 1 {
		t.Errorf("channels = %d, want 1", format.Channels)
	}
	if format.Bits != 16 {
		t.Errorf("bits = %d, want 16", format.Bits)
	}

	st, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	// 0.3 s at 24 kHz mono 16-bit is about 14400 bytes; allow MP3 padding.
	if st.Size() < 8000 || st.Size() > 20000 {
		t.Errorf("pcm size = %d bytes, want roughly 14400", st.Size())
	}
	if st.Size()%2 != 0 {
		t.Errorf("pcm size %d is not sample aligned", st.Size())
	}
}

func TestDecodeMP3ToPCMFile_InvalidInput(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.mp3")
	if err := os.WriteFile(src, []byte("not an mp3"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := DecodeMP3ToPCMFile(src, filepath.Join(dir, "out.pcm")); err == nil {
		t.Fatal("expected an error for non-MP3 input")
	}
}
