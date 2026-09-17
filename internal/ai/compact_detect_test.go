package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsCompactCommand(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"bare command", "/compact", true},
		{"with surrounding whitespace", "  /compact  ", true},
		{"with arguments", "/compact keep the API notes", true},
		{"with newline argument", "/compact\nkeep it short", true},
		{"not a slash command", "compact", false},
		{"different command sharing a prefix", "/compaction", false},
		{"a command merely containing compact", "/reload-compact", false},
		{"mentioned mid-sentence", "please run /compact now", false},
		{"empty", "", false},
		{"slash only", "/", false},
		{"other command", "/reload-plugins", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCompactCommand(tt.text))
		})
	}
}

func TestIsCodeBuddyCompactionMeta(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"nil meta", nil, false},
		{"empty meta", map[string]any{}, false},
		{
			"boolean flag",
			map[string]any{"codebuddy.ai/isCompactInternal": true},
			true,
		},
		{
			"flag serialized as string",
			map[string]any{"codebuddy.ai/isCompactInternal": "true"},
			true,
		},
		{
			"flag serialized as number",
			map[string]any{"codebuddy.ai/isCompactInternal": float64(1)},
			true,
		},
		{
			"explicit false flag",
			map[string]any{"codebuddy.ai/isCompactInternal": false},
			false,
		},
		{
			"compactType alone (manual)",
			map[string]any{"codebuddy.ai/compactType": "user-command"},
			true,
		},
		{
			"compactType alone (auto)",
			map[string]any{"codebuddy.ai/compactType": "pre-message-auto"},
			true,
		},
		{
			"unrelated meta",
			map[string]any{"codebuddy.ai/requestId": "abc"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCodeBuddyCompactionMeta(tt.meta))
		})
	}
}

func TestIsCodeBuddyCompactionCancelledMeta(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"nil meta", nil, false},
		{
			"cancelled",
			map[string]any{"codebuddy.ai/compact-cancelled": map[string]any{"cancelled": true}},
			true,
		},
		{
			"limit reached",
			map[string]any{"codebuddy.ai/compact-limit-reached": map[string]any{"limitReached": true}},
			true,
		},
		{
			"successful compaction is not a cancellation",
			map[string]any{"codebuddy.ai/isCompactInternal": true},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCodeBuddyCompactionCancelledMeta(tt.meta))
		})
	}
}

func TestIsClaudeCompactionText(t *testing.T) {
	assert.True(t, IsClaudeCompactionText("Compacting completed."))
	assert.True(t, IsClaudeCompactionText("\n\nCompacting completed."))
	assert.False(t, IsClaudeCompactionText("Compacting..."))
	assert.False(t, IsClaudeCompactionText("compacting completed")) // case-sensitive
	assert.False(t, IsClaudeCompactionText(""))
}

func TestIsGrokCompactionCompleted(t *testing.T) {
	assert.True(t, isGrokCompactionCompleted("compact_completed"))
	assert.True(t, isGrokCompactionCompleted("auto_compact_completed"))
	// Started/failed/cancelled events do not mean the context was rewritten.
	assert.False(t, isGrokCompactionCompleted("auto_compact_started"))
	assert.False(t, isGrokCompactionCompleted("auto_compact_failed"))
	assert.False(t, isGrokCompactionCompleted("compact_cancelled"))
	assert.False(t, isGrokCompactionCompleted("text"))
}

func TestIsCLICompactionMessage(t *testing.T) {
	t.Run("claude compact_boundary", func(t *testing.T) {
		assert.True(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "system", Subtype: "compact_boundary"}))
	})
	t.Run("codebuddy compacting status", func(t *testing.T) {
		assert.True(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "system", Subtype: "status", Status: "compacting"}))
	})
	t.Run("codebuddy status null is not compaction", func(t *testing.T) {
		assert.False(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "system", Subtype: "status"}))
	})
	t.Run("codebuddy providerData flag", func(t *testing.T) {
		msg := &ClaudeStreamMessage{Type: "message"}
		msg.ProviderData = &struct {
			Model string `json:"model,omitempty"`
			Usage *struct {
				InputTokens  int `json:"inputTokens"`
				OutputTokens int `json:"outputTokens"`
			} `json:"usage,omitempty"`
			IsCompactInternal bool   `json:"isCompactInternal,omitempty"`
			CompactType       string `json:"compactType,omitempty"`
		}{IsCompactInternal: true}
		assert.True(t, isCLICompactionMessage(msg))
	})
	t.Run("codebuddy _meta flag", func(t *testing.T) {
		assert.True(t, isCLICompactionMessage(&ClaudeStreamMessage{
			Type: "message",
			Meta: map[string]any{"codebuddy.ai/isCompactInternal": true},
		}))
	})
	t.Run("plain assistant text is not compaction", func(t *testing.T) {
		assert.False(t, isCLICompactionMessage(&ClaudeStreamMessage{Type: "assistant", Subtype: "text", Text: "hello"}))
	})
	t.Run("nil message", func(t *testing.T) {
		assert.False(t, isCLICompactionMessage(nil))
	})
}

// TestStreamParser_ReportsCompactionOnce pins the latch: the CLI repeats the
// compaction flag on every item emitted while compacting, but the service layer
// must be told about the compaction a single time.
func TestStreamParser_ReportsCompactionOnce(t *testing.T) {
	p := &StreamParser{}
	ch := make(chan StreamEvent, 32)

	p.ParseLine(`{"type":"system","subtype":"compact_boundary"}`, ch)
	p.ParseLine(`{"type":"system","subtype":"compact_boundary"}`, ch)
	p.ParseLine(`{"type":"message","_meta":{"codebuddy.ai/isCompactInternal":true}}`, ch)

	detected := 0
	for {
		select {
		case ev := <-ch:
			if ev.Type == "compact_detected" {
				detected++
			}
		default:
			assert.Equal(t, 1, detected, "a compaction must be reported exactly once per parser")
			return
		}
	}
}

// TestStreamParser_NoCompactionWithoutSignal guards the negative direction: a
// normal turn must not flag the session as compacted.
func TestStreamParser_NoCompactionWithoutSignal(t *testing.T) {
	p := &StreamParser{}
	ch := make(chan StreamEvent, 32)
	p.ParseLine(`{"type":"assistant","subtype":"text","text":"hello"}`, ch)
	p.ParseLine(`{"type":"system","subtype":"init"}`, ch)
	p.ParseLine(`{"type":"system","subtype":"status"}`, ch)

	for {
		select {
		case ev := <-ch:
			assert.NotEqual(t, "compact_detected", ev.Type)
		default:
			return
		}
	}
}

func TestGrokStreamParser_ReportsCompaction(t *testing.T) {
	p := &GrokStreamParser{}
	ch := make(chan StreamEvent, 16)
	p.ParseLine(`{"type":"auto_compact_completed"}`, ch)
	p.ParseLine(`{"type":"auto_compact_completed"}`, ch)

	detected := 0
	for {
		select {
		case ev := <-ch:
			if ev.Type == "compact_detected" {
				detected++
			}
		default:
			assert.Equal(t, 1, detected, "grok compaction must be reported exactly once")
			return
		}
	}
}

func TestGrokStreamParser_IgnoresFailedCompaction(t *testing.T) {
	p := &GrokStreamParser{}
	ch := make(chan StreamEvent, 16)
	p.ParseLine(`{"type":"auto_compact_started"}`, ch)
	p.ParseLine(`{"type":"auto_compact_failed"}`, ch)

	for {
		select {
		case ev := <-ch:
			assert.NotEqual(t, "compact_detected", ev.Type)
		default:
			return
		}
	}
}
