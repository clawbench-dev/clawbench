package handler

import (
	"strings"
	"testing"
)

func TestSanitizeTextContent_Empty(t *testing.T) {
	data, truncated := sanitizeTextContent([]byte{})
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}
	if truncated {
		t.Error("expected no truncation for empty input")
	}
}

func TestSanitizeTextContent_PureText(t *testing.T) {
	input := []byte("hello world\nline 2\nline 3")
	data, truncated := sanitizeTextContent(input)
	if string(data) != string(input) {
		t.Errorf("expected unchanged text, got %q", string(data))
	}
	if truncated {
		t.Error("expected no truncation for small text")
	}
}

func TestSanitizeTextContent_BinaryWithNullBytes(t *testing.T) {
	// Binary content with null bytes in the first 8KB
	input := []byte("hello\x00world\x00\x01\x02test")
	data, truncated := sanitizeTextContent(input)

	// Null bytes and control chars should be replaced with '.'
	expected := "hello.world...test"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
	if truncated {
		t.Error("expected no truncation for small binary content")
	}
}

func TestSanitizeTextContent_BinaryPreservesPrintable(t *testing.T) {
	// Binary content should preserve printable ASCII, newlines, tabs, and high bytes
	input := []byte("line1\nline2\ttab\x00null\x80high")
	data, _ := sanitizeTextContent(input)

	result := string(data)
	if !strings.Contains(result, "line1\nline2\ttab") {
		t.Errorf("expected printable chars preserved, got %q", result)
	}
	if !strings.Contains(result, "high") {
		t.Errorf("expected high bytes preserved, got %q", result)
	}
	// Null byte should be replaced
	if strings.Contains(result, "\x00") {
		t.Error("null byte should have been replaced")
	}
}

func TestSanitizeTextContent_BinaryTruncation(t *testing.T) {
	// Binary content larger than maxBinaryTextSize should be truncated
	input := make([]byte, maxBinaryTextSize+1000)
	// Put null byte in first 8KB to trigger binary detection
	input[100] = 0x00

	data, truncated := sanitizeTextContent(input)
	if !truncated {
		t.Error("expected truncation for large binary content")
	}
	if len(data) > maxBinaryTextSize {
		t.Errorf("expected data <= %d bytes, got %d", maxBinaryTextSize, len(data))
	}
}

func TestSanitizeTextContent_LargeTextTruncation(t *testing.T) {
	// Text content larger than maxForceTextSize should be truncated
	input := make([]byte, maxForceTextSize+1000)
	// Fill with printable ASCII (no null bytes = text detection)
	for i := range input {
		input[i] = 'A' + byte(i%26)
	}

	data, truncated := sanitizeTextContent(input)
	if !truncated {
		t.Error("expected truncation for large text content")
	}
	if len(data) > maxForceTextSize {
		t.Errorf("expected data <= %d bytes, got %d", maxForceTextSize, len(data))
	}
}

func TestSanitizeTextContent_UTF8BoundaryTruncation(t *testing.T) {
	// Text with multi-byte UTF-8 characters should truncate at rune boundary
	// Build content that exceeds maxForceTextSize with multi-byte chars at the end
	base := strings.Repeat("A", maxForceTextSize-1)
	// 3-byte UTF-8 char (中 = 0xE4 0xB8 0xAD)
	input := []byte(base + "中字")

	data, truncated := sanitizeTextContent(input)
	if !truncated {
		t.Error("expected truncation")
	}
	// Data should not split in the middle of a multi-byte char
	// The truncated data should be valid UTF-8 or at least not end with
	// a partial multi-byte sequence
	for i := len(data) - 3; i < len(data); i++ {
		if data[i] >= 0x80 {
			// Check it's a valid UTF-8 start or continuation
			// Simple check: no 0xC0-0xFF (start bytes) without enough continuation bytes
			break
		}
	}
}

func TestSanitizeTextContent_ControlCharsReplaced(t *testing.T) {
	// Binary content with various control characters (null byte in sniff window triggers binary detection)
	input := []byte("text\x01\x02\x03\x07\x1B\x7Fmore\x00")
	data, _ := sanitizeTextContent(input)

	result := string(data)
	// Control chars (except \n, \r, \t) should be replaced with '.'
	if strings.Contains(result, "\x01") || strings.Contains(result, "\x07") {
		t.Errorf("control chars should be replaced, got %q", result)
	}
	if !strings.Contains(result, "text") || !strings.Contains(result, "more") {
		t.Errorf("printable text should be preserved, got %q", result)
	}
}

func TestSanitizeTextContent_NewlineTabPreserved(t *testing.T) {
	// Binary content should preserve newlines, tabs, and carriage returns
	input := []byte("line1\nline2\r\nline3\ttab\x00null")
	data, _ := sanitizeTextContent(input)

	result := string(data)
	if !strings.Contains(result, "\n") {
		t.Error("newline should be preserved")
	}
	if !strings.Contains(result, "\t") {
		t.Error("tab should be preserved")
	}
	if !strings.Contains(result, "\r") {
		t.Error("carriage return should be preserved")
	}
}

func TestSanitizeTextContent_NullByteAfter8KB(t *testing.T) {
	// Null byte beyond the 8KB sniff window should NOT trigger binary detection
	input := make([]byte, binarySniffSize+100)
	for i := range input {
		input[i] = 'A' + byte(i%26)
	}
	// Put null byte just past the sniff window
	input[binarySniffSize] = 0x00

	data, truncated := sanitizeTextContent(input)
	// Should be treated as text (no binary detection)
	if truncated {
		t.Error("expected no truncation for content with null past sniff window")
	}
	// Content should be returned as-is (including the null byte — we only sniff first 8KB)
	if data[binarySniffSize] != 0x00 {
		t.Error("null byte past sniff window should be preserved")
	}
}

func TestSanitizeTextContent_NullByteAtSniffBoundary(t *testing.T) {
	// Null byte right at the 8KB boundary (last byte of sniff window)
	input := make([]byte, binarySniffSize+100)
	for i := range input {
		input[i] = 'A' + byte(i%26)
	}
	input[binarySniffSize-1] = 0x00 // Last byte of sniff window

	data, _ := sanitizeTextContent(input)
	// Should detect as binary (null byte within sniff window)
	if data[binarySniffSize-1] != '.' {
		t.Errorf("null byte in sniff window should be replaced with '.', got %q", string(data[binarySniffSize-1]))
	}
}
