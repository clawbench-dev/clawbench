package speech

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// shortMP3 is shorter than the sniff window: only finish() can validate it.
var shortMP3 = []byte{0xFF, 0xFB, 0x10, 0x00}

// longMP3 crosses mp3SniffPrefixBytes, so Write() validates and flushes.
func longMP3() []byte {
	b := []byte{0xFF, 0xFB, 0x10, 0x00}
	for len(b) < mp3SniffPrefixBytes+8 {
		b = append(b, 0x11)
	}
	return b
}

func TestAudioSink_AcceptsMP3InOneWrite(t *testing.T) {
	var buf bytes.Buffer
	s := newAudioSink(&buf, "test")

	payload := longMP3()
	n, err := s.Write(payload)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(payload) {
		t.Errorf("Write returned %d, want %d (the caller must see its full write accepted)", n, len(payload))
	}
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("destination got %x, want the whole payload", buf.Bytes())
	}
	if s.total != int64(len(payload)) {
		t.Errorf("total = %d, want %d", s.total, len(payload))
	}
}

func TestAudioSink_AcceptsShortStreamOnFinish(t *testing.T) {
	// A valid MP3 shorter than the sniff window must not be rejected: a tiny
	// synthesized clip (a chime) is legitimately just a couple of frames.
	var buf bytes.Buffer
	s := newAudioSink(&buf, "test")

	if _, err := s.Write(shortMP3); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("bytes were flushed before validation: %x", buf.Bytes())
	}
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), shortMP3) {
		t.Errorf("destination got %x, want the short clip", buf.Bytes())
	}
}

func TestAudioSink_RejectsNonMP3(t *testing.T) {
	// The whole point: a server answering with WAV must not be cached under an
	// .mp3 name to fail silently at playback time.
	var buf bytes.Buffer
	s := newAudioSink(&buf, "test")

	wav := append([]byte("RIFF....WAVE"), make([]byte, mp3SniffPrefixBytes)...)
	_, err := s.Write(wav)
	if err == nil || !strings.Contains(err.Error(), "not mp3") {
		t.Fatalf("err = %v, want a not-mp3 error", err)
	}
	if !strings.Contains(err.Error(), "WAV") {
		t.Errorf("err = %v, want the actual container named (WAV)", err)
	}
	if buf.Len() != 0 {
		t.Errorf("rejected bytes reached the destination: %x", buf.Bytes())
	}
	// The error sticks: a later write must not sneak bytes past it.
	if _, err := s.Write(longMP3()); err == nil {
		t.Error("a sink that rejected the payload must stay failed")
	}
	if err := s.finish(); err == nil {
		t.Error("finish after a rejection must report the same failure")
	}
}

func TestAudioSink_RejectsNonMP3ShortStreamOnFinish(t *testing.T) {
	var buf bytes.Buffer
	s := newAudioSink(&buf, "test")

	if _, err := s.Write([]byte("OggSshort")); err != nil {
		t.Fatalf("Write: %v", err) // buffered, undecided yet
	}
	err := s.finish()
	if err == nil || !strings.Contains(err.Error(), "not mp3") {
		t.Fatalf("finish err = %v, want a not-mp3 error", err)
	}
	if !strings.Contains(err.Error(), "Ogg") {
		t.Errorf("err = %v, want the actual container named (Ogg)", err)
	}
	if buf.Len() != 0 {
		t.Errorf("rejected bytes reached the destination: %x", buf.Bytes())
	}
}

func TestAudioSink_EmptyStreamFinishesClean(t *testing.T) {
	// A provider that produced nothing reports that through its own zero-byte
	// check; the sink must not mislabel it a format error.
	s := newAudioSink(io.Discard, "test")
	if err := s.finish(); err != nil {
		t.Fatalf("finish on an empty stream: %v", err)
	}
}

func TestAudioSink_WriteErrorIsWrappedAndSticky(t *testing.T) {
	fail := &failingWriter{err: errors.New("disk full")}
	s := newAudioSink(fail, "test")

	if _, err := s.Write(longMP3()); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err = %v, want the writer's error wrapped", err)
	}
	if s.total != 0 {
		t.Errorf("total = %d, want 0 (nothing landed)", s.total)
	}
	if _, err := s.Write(longMP3()); err == nil {
		t.Error("a sink whose write failed must stay failed")
	}
}

func TestAudioSink_FinishFlushWriteErrorIsWrapped(t *testing.T) {
	fail := &failingWriter{err: errors.New("disk full")}
	s := newAudioSink(fail, "test")

	if _, err := s.Write(shortMP3); err != nil {
		t.Fatalf("Write: %v", err) // buffered, no destination call yet
	}
	err := s.finish()
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("finish err = %v, want the writer's error wrapped", err)
	}
}

type failingWriter struct {
	err error
}

func (w *failingWriter) Write([]byte) (int, error) { return 0, w.err }
