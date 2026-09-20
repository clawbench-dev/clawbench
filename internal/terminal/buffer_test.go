package terminal

import (
	"bytes"
	"strings"
	"testing"
)

func TestRingBuffer_WriteAndReplay(t *testing.T) {
	rb := NewRingBuffer(5, 65536, 4*1024*1024)

	rb.Write([]byte("hello\nworld\n"))
	replay := rb.Replay()

	if string(replay) != "hello\nworld\n" {
		t.Errorf("expected 'hello\\nworld\\n', got %q", string(replay))
	}
}

func TestRingBuffer_ReplayPreservesANSI(t *testing.T) {
	rb := NewRingBuffer(10, 65536, 4*1024*1024)

	// Simulate ANSI color code spanning line boundary
	rb.Write([]byte("\x1b[31mred line\nstill red\x1b[0m\n"))
	replay := rb.Replay()

	expected := "\x1b[31mred line\nstill red\x1b[0m\n"
	if string(replay) != expected {
		t.Errorf("ANSI sequence not preserved\ngot:      %q\nexpected: %q", string(replay), expected)
	}
}

func TestRingBuffer_LineCountEviction(t *testing.T) {
	rb := NewRingBuffer(3, 65536, 4*1024*1024) // capacity = 3 lines

	rb.Write([]byte("line1\nline2\nline3\nline4\n"))

	replay := rb.Replay()
	expected := "line2\nline3\nline4\n"
	if string(replay) != expected {
		t.Errorf("expected %q, got %q", expected, string(replay))
	}
	if rb.count != 3 {
		t.Errorf("expected 3 lines, got %d", rb.count)
	}
}

func TestRingBuffer_TotalByteEviction(t *testing.T) {
	rb := NewRingBuffer(100, 65536, 30) // 30 byte total limit

	rb.Write([]byte("1234567890\n")) // 11 bytes
	rb.Write([]byte("1234567890\n")) // 22 bytes
	rb.Write([]byte("1234567890\n")) // 33 bytes > 30, evicts oldest

	replay := rb.Replay()
	// Should have last 2 lines (22 bytes <= 30)
	if !bytes.Contains(replay, []byte("1234567890\n1234567890\n")) {
		t.Errorf("expected last 2 lines, got %q", string(replay))
	}
}

func TestRingBuffer_PerLineTruncation(t *testing.T) {
	rb := NewRingBuffer(10, 10, 4*1024*1024) // 10 byte per-line limit

	longLine := strings.Repeat("x", 50) + "\n"
	rb.Write([]byte(longLine))

	replay := rb.Replay()
	if len(replay) > 50 { // 10 truncated + reset + indicator
		t.Errorf("line should be truncated, got %d bytes: %q", len(replay), string(replay))
	}
	// Should contain truncation indicator
	if !bytes.Contains(replay, []byte("[truncated]")) {
		t.Errorf("truncated line should contain [truncated], got %q", string(replay))
	}
	// Should contain ANSI reset before truncation marker
	if !bytes.Contains(replay, []byte("\x1b[0m")) {
		t.Errorf("truncated line should contain ANSI reset, got %q", string(replay))
	}
}

func TestRingBuffer_PartialLineNoNewline(t *testing.T) {
	rb := NewRingBuffer(10, 65536, 4*1024*1024)

	rb.Write([]byte("no newline here"))
	replay := rb.Replay()

	if string(replay) != "no newline here" {
		t.Errorf("expected 'no newline here', got %q", string(replay))
	}
}

func TestRingBuffer_MixedWrites(t *testing.T) {
	rb := NewRingBuffer(10, 65536, 4*1024*1024)

	rb.Write([]byte("first "))
	rb.Write([]byte("second\n"))
	rb.Write([]byte("third\n"))

	replay := rb.Replay()
	expected := "first second\nthird\n"
	if string(replay) != expected {
		t.Errorf("expected %q, got %q", expected, string(replay))
	}
}

func TestRingBuffer_Reset(t *testing.T) {
	rb := NewRingBuffer(10, 65536, 4*1024*1024)

	rb.Write([]byte("some data\n"))
	if rb.count != 1 {
		t.Errorf("expected 1 line, got %d", rb.count)
	}

	rb.Reset()
	if rb.count != 0 {
		t.Errorf("expected 0 lines after reset, got %d", rb.count)
	}
	if rb.totalBytes != 0 {
		t.Errorf("expected 0 bytes after reset, got %d", rb.totalBytes)
	}

	replay := rb.Replay()
	if replay != nil {
		t.Errorf("expected nil replay after reset, got %q", string(replay))
	}
}

func TestRingBuffer_EmptyWrite(t *testing.T) {
	rb := NewRingBuffer(10, 65536, 4*1024*1024)

	rb.Write(nil)
	rb.Write([]byte{})

	if rb.count != 0 {
		t.Errorf("expected 0 lines after empty write, got %d", rb.count)
	}
}

func TestRingBuffer_EmptyReplay(t *testing.T) {
	rb := NewRingBuffer(10, 65536, 4*1024*1024)

	replay := rb.Replay()
	if replay != nil {
		t.Errorf("expected nil for empty buffer, got %q", string(replay))
	}
}

func TestRingBuffer_LargeDataset(t *testing.T) {
	capacity := 2000
	rb := NewRingBuffer(capacity, 65536, 4*1024*1024)

	// Write more lines than capacity
	for range capacity + 500 {
		rb.Write([]byte("line\n"))
	}

	if rb.count != capacity {
		t.Errorf("expected %d lines, got %d", capacity, rb.count)
	}

	replay := rb.Replay()
	lineCount := bytes.Count(replay, []byte("\n"))
	if lineCount != capacity {
		t.Errorf("expected %d newlines in replay, got %d", capacity, lineCount)
	}
}

// Regression: Write must copy the caller's slice.
//
// readPTY reuses a single 4096-byte read buffer for the lifetime of the
// session (`buf := make([]byte, 4096)` outside the loop, then
// `s.buffer.Write(buf[:n])` on every read). Retaining a sub-slice of that
// buffer meant each new read overwrote lines already stored in the ring
// buffer, so the replay handed to a reconnecting client was a splice of
// stale and fresh bytes. Observed symptom: switching to another dock tab and
// back left the terminal output garbled, because the dock switch disconnects
// the WS and reconnects (replaying from this buffer) rather than keeping it
// alive.
func TestRingBuffer_WriteCopiesCallerSlice(t *testing.T) {
	rb := NewRingBuffer(10, 65536, 4*1024*1024)

	// One reused buffer, exactly like readPTY's loop.
	buf := make([]byte, 64)

	n := copy(buf, "first-line\nsecond-line\n")
	rb.Write(buf[:n])

	// A later read reuses the same backing array. It must not be able to
	// mutate what is already buffered.
	n = copy(buf, "later\n")
	rb.Write(buf[:n])

	replay := string(rb.Replay())
	if !strings.Contains(replay, "first-line") || !strings.Contains(replay, "second-line") {
		t.Errorf("earlier output was overwritten by a later read: %q", replay)
	}
	if replay != "first-line\nsecond-line\nlater\n" {
		t.Errorf("unexpected replay: %q", replay)
	}
}
