package vecli

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVeCLIModelIDRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`id: "minimax-m2.5"`, true},
		{`id:"no-space"`, true}, // \s* matches zero spaces
		{`name: "something"`, false},
		{`  id: "indented"`, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, vecliModelIDRe.MatchString(tt.input))
		})
	}
}

func TestVeCLIModelNameRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`name: "MiniMax M2.5"`, true},
		{`name:"no-space"`, true},
		{`id: "something"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, vecliModelNameRe.MatchString(tt.input))
		})
	}
}

func TestExtractRegistrySection(t *testing.T) {
	content := "junk before\nMODEL_REGISTRY = [\n{id: \"a\"},\n];\njunk after"

	got, ok := extractRegistrySection(content)

	require.True(t, ok)
	assert.Contains(t, got, `id: "a"`)
	assert.NotContains(t, got, "junk before")
	assert.NotContains(t, got, "junk after")
}

func TestExtractRegistrySection_Missing(t *testing.T) {
	_, ok := extractRegistrySection("no registry here")
	assert.False(t, ok)
}

func TestExtractRegistrySection_Unterminated(t *testing.T) {
	_, ok := extractRegistrySection("MODEL_REGISTRY = [{id: \"a\"}")
	assert.False(t, ok, "a registry without its closing bracket must not be parsed")
}

func TestParseRegistryEntries(t *testing.T) {
	registry := `MODEL_REGISTRY = [
  {
    id: "minimax-m2.5",
    name: "MiniMax M2.5",
  },
  {
    id: "minimax-m2.7",
    name: "MiniMax M2.7",
  },
  {
    id: "no-name-model",
  },
];`

	entries := parseRegistryEntries(registry)

	require.Len(t, entries, 3)
	assert.Equal(t, "minimax-m2.5", entries[0].id)
	assert.Equal(t, "MiniMax M2.5", entries[0].name)
	assert.Equal(t, "no-name-model", entries[2].id)
	assert.Empty(t, entries[2].name)
}

func TestParseRegistryEntries_NestedBraces(t *testing.T) {
	// An entry may carry nested objects (e.g. capabilities); the brace scanner
	// must not cut the entry short at the first closing brace.
	registry := `MODEL_REGISTRY = [
  {
    id: "nested-model",
    meta: { tier: "pro", caps: { tools: true } },
    name: "Nested",
  },
];`

	entries := parseRegistryEntries(registry)

	require.Len(t, entries, 1)
	assert.Equal(t, "nested-model", entries[0].id)
	assert.Equal(t, "Nested", entries[0].name, "the name field sits after a nested object")
}

func TestBraceBlocks_StopsOnUnbalancedInput(t *testing.T) {
	assert.Empty(t, braceBlocks("{ id: \"a\""), "an unterminated block must not be emitted")
}

func TestVeCLISource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "vecli", Backend: "vecli", DefaultCmd: "vecli"}
	assert.True(t, model.CanDiscoverModels(spec), "vecli should support model discovery")

	src, ok := model.LookupModelSource("vecli")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindPlugin, src.Kind())
}

func TestVeCLISource_MissingCLIReportsDetail(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	models, detail := discoverVeCLIModels()

	assert.Nil(t, models)
	assert.Contains(t, detail, "not found", "the reason must say the CLI is missing")
}
