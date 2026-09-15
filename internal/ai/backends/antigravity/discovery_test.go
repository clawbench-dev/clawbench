package antigravity

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

func TestParseAgyModels_SimpleIDs(t *testing.T) {
	output := `Fetching available models...
gemini-3-pro
gemini-3-flash
gemini-2.5-pro
`
	models := parseAgyModels(output)
	require.Len(t, models, 3)

	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.Equal(t, "Gemini 3 Pro", models[0].Name, "known models should use pretty names")
	assert.True(t, models[0].Default)
	assert.Equal(t, "gemini-3-flash", models[1].ID)
	assert.Equal(t, "Gemini 3 Flash", models[1].Name)
	assert.False(t, models[1].Default)
	assert.Equal(t, "gemini-2.5-pro", models[2].ID)
	assert.False(t, models[2].Default)
}

func TestParseAgyModels_DuplicateLines_Collapse(t *testing.T) {
	output := "gemini-3-pro\ngemini-3-pro\ngemini-3-flash\n"
	models := parseAgyModels(output)
	require.Len(t, models, 2)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.Equal(t, "gemini-3-flash", models[1].ID)
}

func TestParseAgyModels_UnknownModel_UsesRawID(t *testing.T) {
	models := parseAgyModels("some-future-model\n")
	require.Len(t, models, 1)
	assert.Equal(t, "some-future-model", models[0].ID)
	assert.Equal(t, "some-future-model", models[0].Name, "unknown models fall back to raw ID")
}

func TestParseAgyModels_FiltersStatusLines(t *testing.T) {
	output := `You are not logged into Antigravity
Fetching available models...
I0428 10:00:00.000000   1234 loading config
error something went wrong
Failed to fetch models

gemini-3-pro
`
	models := parseAgyModels(output)
	require.Len(t, models, 1)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
}

func TestParseAgyModels_EmptyOutput(t *testing.T) {
	assert.Empty(t, parseAgyModels(""))
	assert.Empty(t, parseAgyModels("\n\n"))
	assert.Empty(t, parseAgyModels("Fetching available models...\nYou are not logged into Antigravity\n"))
}

func TestParseAgyModels_CRLF(t *testing.T) {
	models := parseAgyModels("gemini-3-pro\r\ngemini-3-flash\r\n")
	require.Len(t, models, 2)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.Equal(t, "gemini-3-flash", models[1].ID)
}

func TestAntigravityCatalog_FirstIsDefault(t *testing.T) {
	src := model.NewCLISource("antigravity-test", model.CLIOptions{
		Command:  "definitely-not-a-real-cli-xyz",
		Parse:    parseAgyModels,
		Fallback: model.AntigravityCatalog,
	})

	models, _ := src.Discover()
	require.Len(t, models, len(model.AntigravityCatalog))
	assert.True(t, models[0].Default)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.False(t, models[1].Default)
}

func TestAntigravitySource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "antigravity", Backend: "antigravity", DefaultCmd: "agy"}
	assert.True(t, model.CanDiscoverModels(spec), "antigravity should support model discovery")

	src, ok := model.LookupModelSource("antigravity")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindCLI, src.Kind())
}

func TestAntigravitySource_FallsBackOnMissingBinary(t *testing.T) {
	// The real source falls back to the catalog when `agy` is absent or
	// unauthenticated, so the result must never be empty.
	models := model.DiscoverModels("antigravity")
	require.NotEmpty(t, models)
	assert.True(t, models[0].Default)
}

func TestAntigravitySource_SucceedsWithMockBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock script relies on sh, not available on Windows")
	}
	tmpDir := t.TempDir()
	script := "#!/bin/sh\necho gemini-3-pro\necho gemini-3-flash\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agy"), []byte(script), 0o755))
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	src := model.NewCLISource("antigravity-mock", model.CLIOptions{
		Command:  "agy",
		Args:     []string{"models"},
		Parse:    parseAgyModels,
		Fallback: model.AntigravityCatalog,
	})

	models, _ := src.Discover()
	require.Len(t, models, 2)
	assert.Equal(t, "gemini-3-pro", models[0].ID)
	assert.Equal(t, "Gemini 3 Pro", models[0].Name)
	assert.True(t, models[0].Default)
	assert.Equal(t, "gemini-3-flash", models[1].ID)
	assert.False(t, models[1].Default)
}
