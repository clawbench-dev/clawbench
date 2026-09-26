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
		compacted         bool
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
		{
			// The whole point of the feature: the default interval is 0 (never
			// re-inject), but a compacted session must still get the prompt back.
			name:              "compacted overrides the disabled interval",
			systemPrompt:      "you are helpful",
			resume:            true,
			compacted:         true,
			assistantMsgCount: 7,
			promptInterval:    0,
			expected:          true,
		},
		{
			name:              "compacted overrides a non-boundary turn",
			systemPrompt:      "you are helpful",
			resume:            true,
			compacted:         true,
			assistantMsgCount: 5,
			promptInterval:    10,
			expected:          true,
		},
		{
			name:         "compacted without a system prompt injects nothing",
			systemPrompt: "",
			resume:       true,
			compacted:    true,
			expected:     false,
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
				Compacted:             tt.compacted,
				AssistantMessageCount: tt.assistantMsgCount,
			}
			assert.Equal(t, tt.expected, req.ShouldInjectSystemPrompt())
		})
	}
}

// TestQueueEventDataMessageIDJSON guards the queue_drain / queue_inject payload
// shape: camelCase keys, and no content fields. The message's text and files
// travel in its own user_message event (it is a real chat_history row by then),
// so re-adding them here would let a client render a second bubble for the
// same message.
func TestQueueEventDataMessageIDJSON(t *testing.T) {
	data := QueueEventData{SessionID: "s1", QueueID: "pending-abc", MessageID: 42}
	b, err := json.Marshal(data)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"messageId":42`)
	assert.Contains(t, string(b), `"queueId":"pending-abc"`)
	assert.NotContains(t, string(b), `"MessageID"`)

	var decoded QueueEventData
	assert.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, int64(42), decoded.MessageID)
	assert.Equal(t, "pending-abc", decoded.QueueID)

	// Structural guard: content must NOT be carried by these events.
	obj := map[string]any{}
	assert.NoError(t, json.Unmarshal(b, &obj))
	for _, banned := range []string{"text", "files", "filePaths", "queue"} {
		if _, ok := obj[banned]; ok {
			t.Fatalf("queue event must not carry %q — the message's own user_message event owns it", banned)
		}
	}
}

// TestQueueAddedDataJSON guards the queue_added payload: a queued message has no
// chat_history row yet, so it has no messageId — only the identity/content the
// queue panel needs.
func TestQueueAddedDataJSON(t *testing.T) {
	data := QueueAddedData{QueueID: "pending-abc", Text: "hello", SenderClientID: "c1"}
	b, err := json.Marshal(data)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"queueId":"pending-abc"`)
	assert.Contains(t, string(b), `"text":"hello"`)
	assert.Contains(t, string(b), `"senderClientId":"c1"`)
	assert.NotContains(t, string(b), "messageId")
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
