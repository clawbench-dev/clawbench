package opencode

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOpenCodeModels_RealOutput(t *testing.T) {
	output := `opencode/minimax-m2.5-free
opencode/nemotron-3-super-free
minimax/MiniMax-M2.5
minimax/MiniMax-M2.7
anthropic/claude-sonnet-4-6
`

	models := parseOpenCodeModels(output)
	require.Len(t, models, 5)

	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].ID)
	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].Name, "Name should include provider for disambiguation")
	assert.True(t, models[0].Default, "first model should be default")

	assert.Equal(t, "minimax/MiniMax-M2.5", models[2].ID)
	assert.Equal(t, "minimax/MiniMax-M2.5", models[2].Name)

	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[4].ID)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[4].Name)
}

func TestParseOpenCodeModels_EmptyOutput(t *testing.T) {
	models := parseOpenCodeModels("")
	assert.Nil(t, models)
}

func TestParseOpenCodeModels_InvalidLines(t *testing.T) {
	output := `minimax/MiniMax-M2.5
not-a-valid-line
anthropic/claude-sonnet-4-6

`
	models := parseOpenCodeModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "minimax/MiniMax-M2.5", models[0].ID)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[1].ID)
}

func TestParseOpenCodeModels_SingleModel(t *testing.T) {
	output := `opencode/minimax-m2.5-free`
	models := parseOpenCodeModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "opencode/minimax-m2.5-free", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestDiscoverOpenCodeModels_RegistersWithModel(t *testing.T) {
	// Verify that init() registered the discovery function
	spec := model.BackendSpec{ID: "opencode", Backend: "opencode", DefaultCmd: "opencode"}
	assert.True(t, model.CanDiscoverModels(spec), "opencode should support model discovery via registry")
}
