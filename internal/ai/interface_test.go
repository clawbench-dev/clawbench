package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestShouldInjectSystemPrompt(t *testing.T) {
	tests := []struct {
		name              string
		systemPrompt      string
		resume            bool
		assistantMsgCount int
		promptInterval    int
		expected          bool
	}{
		{
			name:         "empty system prompt",
			systemPrompt: "",
			resume:       false,
			expected:     false,
		},
		{
			name:         "new session with system prompt",
			systemPrompt: "you are helpful",
			resume:       false,
			expected:     true,
		},
		{
			name:              "resume at interval boundary",
			systemPrompt:      "you are helpful",
			resume:            true,
			assistantMsgCount: 10,
			promptInterval:    10,
			expected:          true,
		},
		{
			name:              "resume not at interval boundary",
			systemPrompt:      "you are helpful",
			resume:            true,
			assistantMsgCount: 5,
			promptInterval:    10,
			expected:          false,
		},
		{
			name:              "resume with zero interval",
			systemPrompt:      "you are helpful",
			resume:            true,
			assistantMsgCount: 10,
			promptInterval:    0,
			expected:          false,
		},
		{
			name:              "resume with zero assistant count",
			systemPrompt:      "you are helpful",
			resume:            true,
			assistantMsgCount: 0,
			promptInterval:    10,
			expected:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := model.ChatSystemPromptInterval
			if tt.promptInterval > 0 || tt.resume {
				model.ChatSystemPromptInterval = tt.promptInterval
			}
			defer func() { model.ChatSystemPromptInterval = original }()

			req := ChatRequest{
				SystemPrompt:          tt.systemPrompt,
				Resume:                tt.resume,
				AssistantMessageCount: tt.assistantMsgCount,
			}
			assert.Equal(t, tt.expected, req.ShouldInjectSystemPrompt())
		})
	}
}

func TestQueueEventDataMessageIDJSON(t *testing.T) {
	data := QueueEventData{MessageID: 42, Text: "hello"}
	b, err := json.Marshal(data)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"messageId":42`)
	assert.NotContains(t, string(b), `"MessageID"`)

	var decoded QueueEventData
	assert.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, int64(42), decoded.MessageID)
	assert.Equal(t, "hello", decoded.Text)
}

func TestTruncateToolOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short output unchanged",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "exactly at limit",
			input:    strings.Repeat("a", maxToolOutputBytes),
			expected: strings.Repeat("a", maxToolOutputBytes),
		},
		{
			name:     "one over limit is truncated",
			input:    strings.Repeat("a", maxToolOutputBytes+1),
			expected: strings.Repeat("a", maxToolOutputBytes) + "\n[truncated: original 51201 bytes]",
		},
		{
			name:     "large output truncated",
			input:    strings.Repeat("x", maxToolOutputBytes*2),
			expected: strings.Repeat("x", maxToolOutputBytes) + "\n[truncated: original 102400 bytes]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateToolOutput(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
