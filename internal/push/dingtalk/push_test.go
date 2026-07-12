package dingtalk

import (
	"strings"
	"testing"
)

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"plain text", "hello world", "hello world"},
		{"asterisk", "a*b", "a\\*b"},
		{"hash", "# heading", "\\# heading"},
		{"underscore", "a_b", "a\\_b"},
		{"backtick", "`code`", "\\`code\\`"},
		{"pipe", "a|b", "a\\|b"},
		{"multiple", "*#_`", "\\*\\#\\_\\`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeMarkdown(tt.input)
			if got != tt.expected {
				t.Errorf("escapeMarkdown(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTruncatePreview(t *testing.T) {
	t.Run("short", func(t *testing.T) {
		got := truncatePreview("hello")
		if got != "hello" {
			t.Errorf("expected 'hello', got %q", got)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := truncatePreview("")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("exact limit", func(t *testing.T) {
		input := strings.Repeat("x", 200)
		got := truncatePreview(input)
		if len(got) != 200 {
			t.Errorf("expected 200 chars, got %d", len(got))
		}
	})

	t.Run("over limit", func(t *testing.T) {
		input := strings.Repeat("x", 250)
		got := truncatePreview(input)
		if len(got) != 203 { // 200 + "..."
			t.Errorf("expected 203 chars (200 + ...), got %d", len(got))
		}
		if !strings.HasSuffix(got, "...") {
			t.Error("expected ... suffix for truncated preview")
		}
	})
}

func TestIsStarted_NoManager(t *testing.T) {
	if IsStarted() {
		t.Error("IsStarted() should be false when no manager is set")
	}
}

func TestPushSessionEvent_NotStarted(t *testing.T) {
	PushSessionEvent("test-session", "completed", "Test", "Preview", "/path")
}

func TestPushTaskEvent_NotStarted(t *testing.T) {
	PushTaskEvent("test-task", "completed", "Test Task", "Preview", "/path")
}
