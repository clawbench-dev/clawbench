package ai

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Claude/Codebuddy: --effort <level> (via BuildBaseStreamArgs)
// ============================================================================

func TestBuildBaseStreamArgs_ThinkingEffort_Set(t *testing.T) {
	req := ChatRequest{
		Prompt:         "hello world",
		SystemPrompt:   "you are helpful",
		Model:          "claude-4",
		WorkDir:        "/home/user/project",
		ThinkingEffort: "high",
	}
	args := BuildBaseStreamArgs(req, nil)

	assert.Contains(t, args, "--effort")
	idx := slices.Index(args, "--effort")
	assert.Equal(t, "high", args[idx+1], "--effort value should be 'high'")
}

func TestBuildBaseStreamArgs_ThinkingEffort_Empty(t *testing.T) {
	req := ChatRequest{
		Prompt:       "hello world",
		SystemPrompt: "you are helpful",
		Model:        "claude-4",
		WorkDir:      "/home/user/project",
	}
	args := BuildBaseStreamArgs(req, nil)

	assert.NotContains(t, args, "--effort", "--effort should not appear when ThinkingEffort is empty")
}
