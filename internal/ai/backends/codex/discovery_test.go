package codex

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
		{"o4", true},             // matches o[34]
		{"gpt-3.5-turbo", false}, // "turbo" is not "-mini", regex only allows -mini suffix
		{"claude-sonnet-4", false},
		{"model-x", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, codexModelRe.MatchString(tt.input))
		})
	}
}

func TestCodexModelOrder(t *testing.T) {
	assert.Equal(t, 0, codexModelOrder["gpt-5.5"], "gpt-5.5 should come first")
	assert.Equal(t, 1, codexModelOrder["gpt-5.4"])
	assert.Equal(t, 2, codexModelOrder["gpt-5.4-mini"])
	assert.Equal(t, 3, codexModelOrder["o3"])
	assert.Equal(t, 4, codexModelOrder["o4-mini"])
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

func TestCodexDefaultModels_Structure(t *testing.T) {
	assert.NotEmpty(t, codexDefaultModels)

	defaultCount := 0
	for _, m := range codexDefaultModels {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
		if m.Default {
			defaultCount++
		}
	}
	assert.Equal(t, 1, defaultCount, "exactly one model should be default")
}

func TestCodexDefaultModels_FirstIsDefault(t *testing.T) {
	assert.NotEmpty(t, codexDefaultModels)
	assert.True(t, codexDefaultModels[0].Default)
	assert.Equal(t, "gpt-5.5", codexDefaultModels[0].ID)
}

func TestDiscoverCodexModels_NoCLI(t *testing.T) {
	// When codex CLI is not installed, all strategies return nil.
	models := DiscoverCodexModels()
	// Result depends on installation; just verify no panic
	_ = models
}

func TestDiscoverCodexModelsDefaults_NoCLI(t *testing.T) {
	// When codex is not on PATH, defaults should return nil
	models := discoverCodexModelsDefaults()
	_ = models // may be nil if not installed, just verify no panic
}

// mockCodexInstall builds a fake codex npm-style installation on disk:
//
//	tmp/bin/codex                      — executable so exec.LookPath finds it
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
	codexBin := filepath.Join(binDir, "codex")
	require.NoError(t, os.WriteFile(codexBin, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	triple := codexTargetTriple()
	require.NotEmpty(t, triple, "test platform must map to a known target triple")
	binaryPath := filepath.Join(root, "vendor", triple, "codex", "codex")
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
	// Duplicate gpt-5.4-mini must be deduped; non-model strings skipped.
	models := discoverCodexModelsFromBinary()

	require.Len(t, models, 3)
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.True(t, models[0].Default, "first model must be marked default")
	assert.Equal(t, "gpt-5.4", models[1].ID)
	assert.Equal(t, "gpt-5.4-mini", models[2].ID)
	assert.False(t, models[1].Default, "only first model is default")
}

func TestDiscoverCodexModelsFromBinary_NoModels(t *testing.T) {
	// Binary exists but contains no recognizable model strings.
	mockCodexInstall(t, []string{"claude-sonnet-4", "random text"})
	models := discoverCodexModelsFromBinary()
	assert.Nil(t, models)
}

func TestDiscoverCodexModelsFromBinary_BinaryMissing(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	codexBin := filepath.Join(binDir, "codex")
	require.NoError(t, os.WriteFile(codexBin, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	// No vendor/ tree — the Rust binary path does not exist.
	t.Setenv("PATH", binDir)

	models := discoverCodexModelsFromBinary()
	assert.Nil(t, models)
}

func TestDiscoverCodexModelsFromBinary_CLINotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	models := discoverCodexModelsFromBinary()
	assert.Nil(t, models)
}

func TestDiscoverCodexModels_BinaryStrategyWins(t *testing.T) {
	// When the strings strategy finds models, it must take priority over the
	// hardcoded defaults. (Strings below minLen=4 are not extracted, so use
	// multi-char model IDs.)
	mockCodexInstall(t, []string{"gpt-5.5", "o4-mini"})
	models := DiscoverCodexModels()

	require.Len(t, models, 2)
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.Equal(t, "o4-mini", models[1].ID)
}

func TestDiscoverCodexModels_FallsBackToDefaults(t *testing.T) {
	// Codex installed but the binary tree has no extractable models → defaults.
	mockCodexInstall(t, []string{"unrelated string"})
	models := DiscoverCodexModels()

	require.Len(t, models, 3)
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestDiscoverCodexModelsDefaults_Installed(t *testing.T) {
	mockCodexInstall(t, nil)
	models := discoverCodexModelsDefaults()

	require.Len(t, models, 3)
	assert.Equal(t, "gpt-5.5", models[0].ID)
	assert.True(t, models[0].Default)
}

func TestDiscoverCodexModelsDefaults_NotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	models := discoverCodexModelsDefaults()
	assert.Nil(t, models)
}

func TestDiscoverCodexModelsFromStateDB_NoCodexDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	models := discoverCodexModelsFromStateDB()
	assert.Nil(t, models)
}

func TestDiscoverCodexModelsFromStateDB_NoStateFile(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
	t.Setenv("HOME", home)
	models := discoverCodexModelsFromStateDB()
	assert.Nil(t, models)
}

func TestDiscoverCodexModelsFromStateDB_UnrelatedFilesOnly(t *testing.T) {
	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	require.NoError(t, os.MkdirAll(codexDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("[x]\n"), 0o644))
	t.Setenv("HOME", home)
	models := discoverCodexModelsFromStateDB()
	assert.Nil(t, models)
}
