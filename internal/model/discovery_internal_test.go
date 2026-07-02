package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// --- Embedded binary path resolution tests ---

func TestEmbeddedBinaryPathFromBase_NewLocation(t *testing.T) {
	baseDir := t.TempDir()
	subDir := "opencode"

	// Create agents/opencode/opencode
	agentDir := filepath.Join(baseDir, "agents", subDir)
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	binPath := filepath.Join(agentDir, subDir)
	require.NoError(t, os.WriteFile(binPath, []byte("#!/bin/sh\n"), 0o755))

	result := embeddedBinaryPathFromBase(baseDir, subDir)
	assert.Equal(t, binPath, result)
}

func TestEmbeddedBinaryPathFromBase_NotFound(t *testing.T) {
	baseDir := t.TempDir()
	result := embeddedBinaryPathFromBase(baseDir, "nonexistent")
	assert.Empty(t, result)
}

func TestEmbeddedBinaryVersionFromBase_NewLocation(t *testing.T) {
	baseDir := t.TempDir()
	subDir := "opencode"

	// Create agents/opencode/VERSION
	agentDir := filepath.Join(baseDir, "agents", subDir)
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "VERSION"), []byte("1.2.3\n"), 0o644))

	result := EmbeddedBinaryVersionFromBase(baseDir, subDir, "VERSION")
	assert.Equal(t, "1.2.3", result)
}

func TestEmbeddedBinaryVersionFromBase_NotFound(t *testing.T) {
	baseDir := t.TempDir()
	result := EmbeddedBinaryVersionFromBase(baseDir, "opencode", "VERSION")
	assert.Empty(t, result)
}
