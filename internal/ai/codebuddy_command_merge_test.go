package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeCommands_NoOverlap(t *testing.T) {
	acp := []AvailableCommandInfo{
		{Name: "compact", Description: "Compact context"},
	}
	plugin := []AvailableCommandInfo{
		{Name: "brainstorm", Description: "Brainstorm ideas"},
	}

	result := MergeCommands(acp, plugin)
	assert.Len(t, result, 2)
	assert.Equal(t, "compact", result[0].Name)
	assert.Equal(t, "brainstorm", result[1].Name)
}

func TestMergeCommands_OverlapACPWins(t *testing.T) {
	acp := []AvailableCommandInfo{
		{Name: "compact", Description: "ACP: Compact context", InputHint: "optional reason"},
	}
	plugin := []AvailableCommandInfo{
		{Name: "compact", Description: "Plugin: Compact context"}, // duplicate
		{Name: "brainstorm", Description: "Brainstorm ideas"},
	}

	result := MergeCommands(acp, plugin)
	assert.Len(t, result, 2)
	// ACP version wins for "compact"
	assert.Equal(t, "ACP: Compact context", result[0].Description)
	assert.Equal(t, "optional reason", result[0].InputHint)
	// Plugin-only command is added
	assert.Equal(t, "brainstorm", result[1].Name)
}

func TestMergeCommands_EmptyACP(t *testing.T) {
	plugin := []AvailableCommandInfo{
		{Name: "brainstorm", Description: "Brainstorm ideas"},
	}

	result := MergeCommands(nil, plugin)
	assert.Len(t, result, 1)
	assert.Equal(t, "brainstorm", result[0].Name)
}

func TestMergeCommands_EmptyPlugin(t *testing.T) {
	acp := []AvailableCommandInfo{
		{Name: "compact", Description: "Compact context"},
	}

	result := MergeCommands(acp, nil)
	assert.Len(t, result, 1)
	assert.Equal(t, "compact", result[0].Name)
}

func TestMergeCommands_BothEmpty(t *testing.T) {
	result := MergeCommands(nil, nil)
	assert.Nil(t, result)
}

func TestMergeCommands_MultipleOverlaps(t *testing.T) {
	acp := []AvailableCommandInfo{
		{Name: "compact", Description: "ACP compact"},
		{Name: "review", Description: "ACP review"},
	}
	plugin := []AvailableCommandInfo{
		{Name: "compact", Description: "Plugin compact"}, // duplicate
		{Name: "review", Description: "Plugin review"},   // duplicate
		{Name: "brainstorm", Description: "Plugin brainstorm"},
		{Name: "execute-plan", Description: "Plugin execute-plan"},
	}

	result := MergeCommands(acp, plugin)
	assert.Len(t, result, 4)
	assert.Equal(t, "ACP compact", result[0].Description)
	assert.Equal(t, "ACP review", result[1].Description)
	assert.Equal(t, "brainstorm", result[2].Name)
	assert.Equal(t, "execute-plan", result[3].Name)
}

func TestMergeCommands_SlashInconsistencyDedupes(t *testing.T) {
	// CodeBuddy's own AvailableCommandsUpdate strips the leading slash from
	// skill commands ("mmx-cli"), while our pre-scan (SkillsToCommands) emits
	// "/mmx-cli". The merge must treat them as the same command so the slash
	// menu does not show a double-slash duplicate.
	acp := []AvailableCommandInfo{
		{Name: "mmx-cli", Description: "ACP skill: mmx-cli"}, // no slash (CodeBuddy ACP)
		{Name: "/compact", Description: "ACP compact"},       // slash present (built-in)
	}
	plugin := []AvailableCommandInfo{
		{Name: "/mmx-cli", Description: "Pre-scanned skill: mmx-cli"}, // slash (our scan)
		{Name: "compact", Description: "Plugin compact"},              // no slash (our scan)
		{Name: "brainstorm", Description: "Brainstorm ideas"},
	}

	result := MergeCommands(acp, plugin)
	require.Len(t, result, 3)
	// ACP versions win; neither skill nor compact is duplicated
	assert.Equal(t, "mmx-cli", result[0].Name)
	assert.Equal(t, "/compact", result[1].Name)
	assert.Equal(t, "brainstorm", result[2].Name)
}
