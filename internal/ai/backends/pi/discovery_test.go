package pi

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePiModels_RealOutput(t *testing.T) {
	output := `provider        model                       context  max-out  thinking  images
anthropic       claude-sonnet-4-6           1M       64K      yes       yes
openai          gpt-4o                      128K     4.1K     no        yes
google          gemini-2.5-pro              1M       64K      yes       yes
`

	models := model.ParseTabular(output)
	require.Len(t, models, 3)

	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].ID)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].Name)
	assert.True(t, models[0].Default, "first model should be default")

	assert.Equal(t, "openai/gpt-4o", models[1].ID)
	assert.False(t, models[1].Default)

	assert.Equal(t, "google/gemini-2.5-pro", models[2].ID)
	assert.False(t, models[2].Default)
}

func TestParsePiModels_EmptyOutput(t *testing.T) {
	assert.Nil(t, model.ParseTabular(""))
}

func TestParsePiModels_HeaderOnly(t *testing.T) {
	output := `provider        model                       context  max-out  thinking  images`
	assert.Nil(t, model.ParseTabular(output), "should skip header line")
}

func TestParsePiModels_SingleModel(t *testing.T) {
	output := `anthropic       claude-sonnet-4-6           1M       64K      yes       yes`
	models := model.ParseTabular(output)
	require.Len(t, models, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestParsePiModels_BlankLines(t *testing.T) {
	output := `provider        model
anthropic       claude-sonnet-4-6

openai          gpt-4o

`
	models := model.ParseTabular(output)
	require.Len(t, models, 2)
}

func TestParsePiModels_ProviderPrefixInID(t *testing.T) {
	models := model.ParseTabular("anthropic       claude-sonnet-4-6\n")

	require.Len(t, models, 1)
	assert.Contains(t, models[0].ID, "/", "provider must be part of the ID")
}

func TestPiSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "pi", Backend: "pi", DefaultCmd: "pi"}
	assert.True(t, model.CanDiscoverModels(spec), "pi should support model discovery")

	src, ok := model.LookupModelSource("pi")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindCLI, src.Kind())
}
