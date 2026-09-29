package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- canDiscoverModels internal tests ---

func TestCanDiscoverModels(t *testing.T) {
	// Register a test source to exercise the positive case
	RegisterModelSource(StaticSource("test-can-discover", "", []AgentModel{{ID: "m"}}))

	tests := []struct {
		name     string
		spec     BackendSpec
		expected bool
	}{
		{
			name:     "with registered model source",
			spec:     BackendSpec{Backend: "test-can-discover"},
			expected: true,
		},
		{
			name:     "with nothing registered",
			spec:     BackendSpec{Backend: "nonexistent_backend_xyz"},
			expected: false,
		},
		{
			name:     "empty spec",
			spec:     BackendSpec{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, CanDiscoverModels(tt.spec))
		})
	}
}

// --- BuildCommonPrompt edge cases ---

func TestBuildCommonPrompt_ReturnsContent(t *testing.T) {
	// BuildCommonPrompt always returns the embedded rules content
	result := BuildCommonPrompt()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "User Interaction")
	assert.Contains(t, result, "Media Generation")
	// Multi-Agent removed from common prompt
	assert.NotContains(t, result, "Multi-Agent")
	// Media reading rules are separate — must NOT appear in common prompt
	assert.NotContains(t, result, "Media File Handling")
}

// The ask-question rules must tell the model where the description separator
// goes relative to a bold option title. Without it the model bolds the whole
// "label — gloss" phrase and then adds " — explanation", and the parser used to
// split on the inner dash, truncating the label to "**A" (message 52484).
func TestBuildCommonPrompt_AskOptionSeparatorRule(t *testing.T) {
	result := BuildCommonPrompt()
	assert.Contains(t, result, "never inside it, so the whole bold phrase stays the title")
	assert.Contains(t, result, "inside the bold run is part of the title")
	// The «» placeholders are an internal encoding; leaking one into the
	// rendered prompt would reach the model verbatim.
	assert.NotContains(t, result, "«")
	assert.NotContains(t, result, "»")
}

func TestBuildMediaPrompt_ReturnsContent(t *testing.T) {
	result := BuildMediaPrompt()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Media File Handling")
	assert.Contains(t, result, "Upload path")
	assert.Contains(t, result, "Reading:")
	// Generation rules are in common prompt, not media prompt
	assert.NotContains(t, result, "Generation:")
}

func TestBuildCommonPrompt_MediaRulesSeparated(t *testing.T) {
	common := BuildCommonPrompt()
	media := BuildMediaPrompt()
	// Common and media prompts are distinct, non-overlapping
	assert.NotContains(t, common, "Media File Handling")
	assert.Contains(t, media, "Media File Handling")
	// Concatenation should produce the full original rules
	full := common + "\n\n" + media
	assert.Contains(t, full, "User Interaction")
	assert.Contains(t, full, "Media Generation")
	assert.Contains(t, full, "Media File Handling")
}
