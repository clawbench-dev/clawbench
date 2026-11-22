package codebuddy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// CodeBuddy flags EVERY update emitted during a compaction, and may restate the
// purpose in compactType instead. Either signal is enough; an explicit false is
// not a compaction.
func TestIsCompactionMeta(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"nil meta", nil, false},
		{"empty meta", map[string]any{}, false},
		{"boolean flag", map[string]any{metaKeyIsCompactInternal: true}, true},
		{"flag serialized as string", map[string]any{metaKeyIsCompactInternal: "true"}, true},
		{"flag serialized as number", map[string]any{metaKeyIsCompactInternal: float64(1)}, true},
		{"explicit false flag", map[string]any{metaKeyIsCompactInternal: false}, false},
		{"compactType alone (manual)", map[string]any{metaKeyCompactType: "user-command"}, true},
		{"compactType alone (auto)", map[string]any{metaKeyCompactType: "pre-message-auto"}, true},
		{"unrelated meta", map[string]any{"codebuddy.ai/requestId": "abc"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCompactionMeta(tt.meta))
		})
	}
}

// A cancelled or limit-reached compaction restores the original history, so it
// must clear any pending report rather than be treated as a compaction.
func TestIsCompactionCancelledMeta(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"nil meta", nil, false},
		{"cancelled", map[string]any{metaKeyCompactCancelled: map[string]any{"cancelled": true}}, true},
		{"limit reached", map[string]any{metaKeyCompactLimitReached: map[string]any{"limitReached": true}}, true},
		{"successful compaction is not a cancellation", map[string]any{metaKeyIsCompactInternal: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsCompactionCancelledMeta(tt.meta))
		})
	}
}

// The steer boundary is the echo of the messageId we issued. Reading the key is
// this backend's job; deciding whether the id is ours is the shared loop's.
func TestSteerBoundaryID(t *testing.T) {
	assert.Equal(t, "abc", SteerBoundaryID(map[string]any{"codebuddy.ai/messageId": "abc"}))
	assert.Equal(t, "", SteerBoundaryID(map[string]any{"codebuddy.ai/requestId": "req"}),
		"a frame with no messageId is not an injection echo")
	assert.Equal(t, "", SteerBoundaryID(nil))
}
