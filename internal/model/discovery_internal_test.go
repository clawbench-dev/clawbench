package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- canDiscoverModels internal tests ---

func TestCanDiscoverModels(t *testing.T) {
	tests := []struct {
		name     string
		spec     BackendSpec
		expected bool
	}{
		{
			name:     "with DiscoverModelsFunc",
			spec:     BackendSpec{DiscoverModelsFunc: func() []AgentModel { return nil }},
			expected: true,
		},
		{
			name:     "with ListModelsCmd and ParseModels",
			spec:     BackendSpec{ListModelsCmd: []string{"models"}, ParseModels: ParseOpenCodeModels},
			expected: true,
		},
		{
			name:     "with ListModelsCmd only",
			spec:     BackendSpec{ListModelsCmd: []string{"models"}},
			expected: false,
		},
		{
			name:     "with ParseModels only",
			spec:     BackendSpec{ParseModels: ParseOpenCodeModels},
			expected: false,
		},
		{
			name:     "with nothing",
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
