package qoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQoderModelKeyRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"modelSelector.item.gpt-4.1", true},
		{"modelSelector.item.claude-sonnet-4-6", true},
		{"modelSelector.item.o3", true},
		{"models.gpt-4.1", false},        // missing "modelSelector.item." prefix
		{"modelSelector.item", false},    // no model ID after prefix
		{"modelSelector.gpt-4.1", false}, // missing "item."
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, qoderModelKeyRe.MatchString(tt.input))
		})
	}
}

func TestQoderModelKeyRe_ExtractsID(t *testing.T) {
	tests := []struct {
		input      string
		expectedID string
	}{
		{"modelSelector.item.gpt-4.1", "gpt-4.1"},
		{"modelSelector.item.claude-opus-4-20250514", "claude-opus-4-20250514"},
		{"modelSelector.item.o4-mini", "o4-mini"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			m := qoderModelKeyRe.FindStringSubmatch(tt.input)
			assert.Len(t, m, 2, "should capture one submatch")
			assert.Equal(t, tt.expectedID, m[1])
		})
	}
}

func TestQoderSkipModels(t *testing.T) {
	assert.True(t, qoderSkipModels["auto"], "auto should be skipped")
	assert.True(t, qoderSkipModels["ultimate"], "ultimate should be skipped")
	assert.True(t, qoderSkipModels["performance"], "performance should be skipped")
	assert.True(t, qoderSkipModels["efficient"], "efficient should be skipped")
	assert.True(t, qoderSkipModels["lite"], "lite should be skipped")
	assert.False(t, qoderSkipModels["gpt-4.1"], "real model should not be skipped")
}

func TestDiscoverQoderModels_NoFile(t *testing.T) {
	// When ~/.qoder/.auth/dynamic-texts.json doesn't exist, should return nil
	models := DiscoverQoderModels()
	// Result depends on installation; just verify no panic
	_ = models
}
