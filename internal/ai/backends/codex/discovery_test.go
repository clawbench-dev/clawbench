package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexModelRe(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"gpt-5.5", true},
		{"gpt-5.4", true},
		{"gpt-5.4-mini", true},
		{"o3", true},
		{"o4-mini", true},
		{"gpt-4", false},         // single version segment
		{"gpt-4.1", true},        // matches gpt-\d+\.\d+
		{"o3-mini", true},        // matches o[34](-mini)?
		{"gpt-3.5-turbo", false}, // only a -mini suffix is allowed
		{"claude-sonnet-4", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, codexModelRe.MatchString(tt.input))
		})
	}
}

func TestCodexTargetTriple(t *testing.T) {
	triple := codexTargetTriple()

	switch runtime.GOOS {
	case "linux", "android":
		switch runtime.GOARCH {
		case "amd64":
			assert.Equal(t, "x86_64-unknown-linux-musl", triple)
		case "arm64":
			assert.Equal(t, "aarch64-unknown-linux-musl", triple)
		default:
			assert.Empty(t, triple, "unsupported arch should return empty")
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			assert.Equal(t, "x86_64-apple-darwin", triple)
		case "arm64":
			assert.Equal(t, "aarch64-apple-darwin", triple)
		default:
			assert.Empty(t, triple, "unsupported arch should return empty")
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			assert.Equal(t, "x86_64-pc-windows-msvc", triple)
		case "arm64":
			assert.Equal(t, "aarch64-pc-windows-msvc", triple)
		default:
			assert.Empty(t, triple, "unsupported arch should return empty")
		}
	default:
		assert.Empty(t, triple, "unsupported OS should return empty")
	}
}

func TestCodexCatalog_Structure(t *testing.T) {
	require.NotEmpty(t, model.CodexCatalog)

	defaultCount := 0
	for _, m := range model.CodexCatalog {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be default")
	assert.Equal(t, "gpt-5.5", model.CodexCatalog[0].ID)
	assert.True(t, model.CodexCatalog[0].Default)
}

func TestCodexSource_Registered(t *testing.T) {
	spec := model.BackendSpec{ID: "codex", Backend: "codex", DefaultCmd: "codex"}
	assert.True(t, model.CanDiscoverModels(spec), "codex should support model discovery")

	src, ok := model.LookupModelSource("codex")
	require.True(t, ok)
	assert.Equal(t, model.SourceKindPlugin, src.Kind())
}

func TestCodexSource_NotInstalledReportsDetail(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	models, detail := discoverCodexModels()
	assert.Nil(t, models)
	assert.Contains(t, detail, "not found", "the reason must say the CLI is missing")
}

// mockCodexInstall builds a fake codex npm-style installation on disk:
//
//	tmp/bin/codex                      — executable so ResolveCLIPath finds it
//	tmp/vendor/<triple>/codex/codex    — "binary" whose printable strings are
//	                                     extracted for model discovery
//
// realPath = tmp/bin/codex → pkgDir = tmp → vendorDir = tmp/vendor, mirroring
// the real `node_modules/@openai/codex/` layout.
func mockCodexInstall(t *testing.T, binaryStrings []string) {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))

	codexName := "codex"
	if runtime.GOOS == "windows" {
		codexName = "codex.exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(binDir, codexName), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	triple := codexTargetTriple()
	require.NotEmpty(t, triple, "test platform must map to a known target triple")
	binaryPath := filepath.Join(root, "vendor", triple, "codex", codexName)
	require.NoError(t, os.MkdirAll(filepath.Dir(binaryPath), 0o755))

	var buf bytes.Buffer
	for _, s := range binaryStrings {
		buf.WriteString("\x00")
		buf.WriteString(s)
		buf.WriteString("\x00")
	}
	require.NoError(t, os.WriteFile(binaryPath, buf.Bytes(), 0o644))

	t.Setenv("PATH", binDir)
}

func TestDiscoverCodexModelsFromBinary_Success(t *testing.T) {
	mockCodexInstall(t, []string{"gpt-5.5", "gpt-5.4", "gpt-5.4-mini", "not-a-model", "gpt-5.4-mini"})

	models := discoverCodexModelsFromBinary()

	require.Len(t, models, 3, "duplicates and non-model strings must be dropped")
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.Equal(t, "gpt-5.4", models[1].ID)
	assert.Equal(t, "gpt-5.4-mini", models[2].ID)
}

func TestDiscoverCodexModelsFromBinary_OrdersByKnownPreference(t *testing.T) {
	// o4-mini is later in CodexModelOrder than gpt-5.5, so the sorted result
	// must not follow the raw extraction order.
	mockCodexInstall(t, []string{"o4-mini", "gpt-5.5"})

	models := discoverCodexModelsFromBinary()

	require.Len(t, models, 2)
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.Equal(t, "o4-mini", models[1].ID)
}

func TestDiscoverCodexModelsFromBinary_NoModels(t *testing.T) {
	mockCodexInstall(t, []string{"claude-sonnet-4", "random text"})
	assert.Nil(t, discoverCodexModelsFromBinary())
}

func TestDiscoverCodexModelsFromBinary_BinaryMissing(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", binDir)

	assert.Nil(t, discoverCodexModelsFromBinary(), "no vendor/ tree means no binary to scan")
}

func TestDiscoverCodexModelsFromBinary_CLINotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	assert.Nil(t, discoverCodexModelsFromBinary())
}

func TestDiscoverCodexModelsFromCache(t *testing.T) {
	codexDir := t.TempDir()
	cache := `{"models":[
		{"slug":"gpt-6-astra","display_name":"GPT-6-Astra","visibility":"list"},
		{"slug":"gpt-6-astra","display_name":"duplicate","visibility":"list"},
		{"slug":"gpt-5.6-luna","display_name":"GPT-5.6 Luna","visibility":"list"},
		{"slug":"hidden","visibility":"hide"}
	]}`
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o644))
	t.Setenv("CODEX_HOME", codexDir)

	models := discoverCodexModelsFromCache()

	require.Len(t, models, 2, "duplicates collapse and non-listable models are excluded")
	assert.Equal(t, "gpt-6-astra", models[0].ID)
	assert.Equal(t, "GPT-6-Astra", models[0].Name)
	assert.Equal(t, "GPT-5.6 Luna", models[1].Name)
}

func TestDiscoverCodexModelsFromCache_NoFile(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	assert.Nil(t, discoverCodexModelsFromCache())
}

func TestDiscoverCodexModelsFromCache_MalformedJSON(t *testing.T) {
	codexDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte("{not json"), 0o644))
	t.Setenv("CODEX_HOME", codexDir)

	assert.Nil(t, discoverCodexModelsFromCache())
}

func TestDiscoverCodexModels_CacheWinsOverBinary(t *testing.T) {
	codexDir := t.TempDir()
	cache := `{"models":[
		{"slug":"gpt-6-astra","display_name":"GPT-6-Astra","visibility":"list"},
		{"slug":"gpt-5.6-luna","display_name":"GPT-5.6 Luna","visibility":"list"}
	]}`
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "models_cache.json"), []byte(cache), 0o644))
	t.Setenv("CODEX_HOME", codexDir)
	mockCodexInstall(t, []string{"gpt-5.5"})

	models, detail := discoverCodexModels()

	require.Len(t, models, 2, "the account's cached catalog is preferred over binary strings")
	assert.Empty(t, detail)
	assert.Equal(t, "gpt-6-astra", models[0].ID)
}

func TestDiscoverCodexModels_BinaryBeatsCatalog(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir()) // no cache file
	mockCodexInstall(t, []string{"gpt-5.5", "o4-mini"})

	models, detail := discoverCodexModels()

	require.Len(t, models, 2)
	assert.Empty(t, detail)
	assert.Equal(t, "gpt-5.5", models[0].ID)
}

func TestDiscoverCodexModels_FallsBackToCatalog(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir()) // no cache file
	mockCodexInstall(t, []string{"unrelated string"})

	models, detail := discoverCodexModels()

	require.Len(t, models, len(model.CodexCatalog))
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.Contains(t, detail, "built-in catalog", "the fallback must be reported, not silent")
}

func TestCodexSource_MarksDefaultAtTheBoundary(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	mockCodexInstall(t, []string{"o4-mini", "gpt-5.4"})

	src, ok := model.LookupModelSource("codex")
	require.True(t, ok)
	models, _ := src.Discover()

	require.Len(t, models, 2)
	assert.True(t, models[0].Default, "the source wrapper is responsible for marking a default")
	assert.False(t, models[1].Default)
}
