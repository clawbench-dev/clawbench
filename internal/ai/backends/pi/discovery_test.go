package pi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePiModels_RealOutput(t *testing.T) {
	output := `provider        model                       context  max-out  thinking  images
anthropic       claude-sonnet-4-6           1M       64K      yes       yes
openai          gpt-4o                      128K     4.1K     no        yes
google          gemini-2.5-pro              1M       64K      yes       yes
`

	models := ParsePiModels(output)
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
	models := ParsePiModels("")
	assert.Nil(t, models)
}

func TestParsePiModels_HeaderOnly(t *testing.T) {
	output := `provider        model                       context  max-out  thinking  images`
	models := ParsePiModels(output)
	assert.Nil(t, models, "should skip header line")
}

func TestParsePiModels_SingleModel(t *testing.T) {
	output := `anthropic       claude-sonnet-4-6           1M       64K      yes       yes`
	models := ParsePiModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestParsePiModels_ProviderPrefixFormat(t *testing.T) {
	output := `anthropic       claude-sonnet-4-6
openai          gpt-4o
`
	models := ParsePiModels(output)
	require.Len(t, models, 2)

	// Verify provider/model format
	assert.Contains(t, models[0].ID, "/")
	assert.Contains(t, models[1].ID, "/")

	// Verify the provider is correctly extracted
	parts := splitID(models[0].ID)
	assert.Equal(t, "anthropic", parts[0])
	assert.Equal(t, "claude-sonnet-4-6", parts[1])
}

func TestParsePiModels_BlankLines(t *testing.T) {
	output := `provider        model
anthropic       claude-sonnet-4-6

openai          gpt-4o

`
	models := ParsePiModels(output)
	require.Len(t, models, 2)
}

func TestPiModelLineRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"anthropic       claude-sonnet-4-6           1M", true},
		{"provider        model                       context", true}, // regex matches, but ParsePiModels filters it
		{"", false},
		{"   ", false},
		{"single", false}, // only one field
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, piModelLineRe.MatchString(tt.input))
		})
	}
}

func TestPiModelLineRe_ExtractsFields(t *testing.T) {
	m := piModelLineRe.FindStringSubmatch("anthropic       claude-sonnet-4-6           1M")
	require.Len(t, m, 3)
	assert.Equal(t, "anthropic", m[1])
	assert.Equal(t, "claude-sonnet-4-6", m[2])
}

func TestDiscoverPiModels_NoCLI(t *testing.T) {
	models := DiscoverPiModels()
	// Result depends on installation; just verify no panic
	_ = models
}

// splitID splits a "provider/model" ID into [provider, model]
func splitID(id string) [2]string {
	parts := [2]string{}
	for i := 0; i < len(id); i++ { //nolint:intrange // index used for string indexing
		if id[i] == '/' {
			parts[0] = id[:i]
			parts[1] = id[i+1:]
			break
		}
	}
	return parts
}
