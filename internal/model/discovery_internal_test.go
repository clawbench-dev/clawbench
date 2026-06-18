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
}
