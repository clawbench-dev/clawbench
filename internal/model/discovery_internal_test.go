package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- canDiscoverModels internal tests ---

func TestCanDiscoverModels(t *testing.T) {
	// Register a test discovery function to test the positive case
	RegisterDiscoverModelsFunc("test-can-discover", func() []AgentModel {
		return nil
	})

	tests := []struct {
		name     string
		spec     BackendSpec
		expected bool
	}{
		{
			name:     "with registered discovery function",
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
