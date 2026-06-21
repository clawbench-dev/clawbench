package vecli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVeCLIModelIDRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`id: "minimax-m2.5"`, true},
		{`id: "model-name"`, true},
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

func TestVeCLIModelIDRe_ExtractsID(t *testing.T) {
	m := vecliModelIDRe.FindStringSubmatch(`id: "minimax-m2.5"`)
	require.Len(t, m, 2)
	assert.Equal(t, "minimax-m2.5", m[1])
}

func TestVeCLIModelNameRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`name: "MiniMax M2.5"`, true},
		{`name: "Model Name"`, true},
		{`name:"no-space"`, true}, // \s* matches zero spaces
		{`id: "something"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, vecliModelNameRe.MatchString(tt.input))
		})
	}
}

func TestVeCLIModelNameRe_ExtractsName(t *testing.T) {
	m := vecliModelNameRe.FindStringSubmatch(`name: "MiniMax M2.5"`)
	require.Len(t, m, 2)
	assert.Equal(t, "MiniMax M2.5", m[1])
}

func TestDiscoverVeCLIModels_NoCLI(t *testing.T) {
	models := DiscoverVeCLIModels()
	// Result depends on installation; just verify no panic
	_ = models
}

func TestVeCLIModelEntry_NameFallback(t *testing.T) {
	entry := struct {
		id   string
		name string
	}{id: "minimax-m2.5", name: ""}

	name := entry.name
	if name == "" {
		name = entry.id
	}
	assert.Equal(t, "minimax-m2.5", name, "should fall back to ID when name is empty")
}

func TestVeCLIModelParsing_SimulatedRegistry(t *testing.T) {
	content := `MODEL_REGISTRY = [
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

	registryStart := -1
	for i := 0; i < len(content); i++ { //nolint:intrange // index used for content slicing
		if i+17 <= len(content) && content[i:i+17] == "MODEL_REGISTRY = " {
			registryStart = i
			break
		}
	}
	require.NotEqual(t, -1, registryStart)

	// Extract first model ID using regex
	idx := strings.Index(content, "{")
	require.NotEqual(t, -1, idx)
	entry := content[idx:]
	m := vecliModelIDRe.FindStringSubmatch(entry)
	require.Len(t, m, 2)
	assert.Equal(t, "minimax-m2.5", m[1])

	// Verify name extraction
	mName := vecliModelNameRe.FindStringSubmatch(entry)
	require.Len(t, mName, 2)
	assert.Equal(t, "MiniMax M2.5", mName[1])

	// Test third entry (no name) — find the third { block
	thirdEntryIdx := strings.Index(content, `"no-name-model"`)
	require.NotEqual(t, -1, thirdEntryIdx)
	// Search backwards to find the opening id: for this entry
	idPrefixIdx := strings.LastIndex(content[:thirdEntryIdx], "id:")
	noNameSection := content[idPrefixIdx:]
	mID := vecliModelIDRe.FindStringSubmatch(noNameSection)
	require.Len(t, mID, 2)
	assert.Equal(t, "no-name-model", mID[1])

	// Simulate the name fallback logic
	name := ""
	if m := vecliModelNameRe.FindStringSubmatch(noNameSection); len(m) >= 2 {
		name = m[1]
	}
	if name == "" {
		name = mID[1] // fallback to ID
	}
	assert.Equal(t, "no-name-model", name)
}
