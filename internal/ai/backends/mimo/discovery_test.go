package mimo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The catalog is published through the plugin's ModelSource, registered by the
// same init() that registers the backend: models reachable in the picker and a
// spawnable CLI must be one fact, not two registrations that can drift.
func TestMimoSource_IsThePluginsOwnCatalog(t *testing.T) {
	t.Setenv("PATH", fakeMimoBinDir(t)) // the static source hides its list when the CLI is absent

	src, ok := model.LookupModelSource("mimo")
	require.True(t, ok, "mimo must register its model source")
	assert.Equal(t, model.SourceKindStatic, src.Kind())
	assert.Equal(t, "mimo", src.Backend())

	got, _ := src.Discover()
	require.Len(t, got, len(MimoCatalog), "the registered source is the catalog above, not a hand-copied subset")
	for i, want := range MimoCatalog {
		assert.Equal(t, want.ID, got[i].ID)
		assert.Equal(t, i == 0, got[i].Default, "exactly the first entry is the default")
	}
}

// fakeMimoBinDir creates a directory holding an executable named mimo, so the
// static source's CLI presence check passes without the real binary.
func fakeMimoBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "mimo"
	if runtime.GOOS == "windows" {
		name = "mimo.bat"
	}
	body := "#!/bin/sh\nexit 0\n"
	if runtime.GOOS == "windows" {
		body = "@echo off\r\nexit /b 0\r\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755))
	return dir
}

func TestMimoCatalog_Structure(t *testing.T) {
	require.NotEmpty(t, MimoCatalog, "should have a default catalog")

	defaultCount := 0
	for _, m := range MimoCatalog {
		assert.NotEmpty(t, m.ID, "model ID should not be empty")
		assert.NotEmpty(t, m.Name, "model Name should not be empty")
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be marked as default")
}

func TestMimoCatalog_FirstIsDefault(t *testing.T) {
	require.NotEmpty(t, MimoCatalog)
	assert.True(t, MimoCatalog[0].Default, "first model should be default")
	assert.Equal(t, "mimo/mimo-auto", MimoCatalog[0].ID)
}

func TestMimoCatalog_ContainsKnownModels(t *testing.T) {
	ids := make(map[string]bool)
	for _, m := range MimoCatalog {
		ids[m.ID] = true
	}
	assert.True(t, ids["mimo/mimo-auto"], "should contain MiMo Auto")
	assert.True(t, ids["xiaomi/mimo-v2.5-pro"], "should contain MiMo V2.5 Pro")
}

func TestMimoSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "mimo", Backend: "mimo", DefaultCmd: "mimo"}
	assert.True(t, model.CanDiscoverModels(spec), "mimo should support model discovery")

	src, ok := model.LookupModelSource("mimo")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindStatic, src.Kind())
}
