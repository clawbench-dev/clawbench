package startup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckLegacyLayout_NoLegacyDir(t *testing.T) {
	binDir := t.TempDir()
	dataDir := t.TempDir()

	// No .clawbench under binDir — nothing should happen
	CheckLegacyLayout(binDir, dataDir)

	// dataDir should be empty
	entries, _ := os.ReadDir(dataDir)
	assert.Empty(t, entries)
}

func TestCheckLegacyLayout_LegacyDataDirExists(t *testing.T) {
	binDir := t.TempDir()
	dataDir := t.TempDir()

	// Create legacy data dir
	oldDataDir := filepath.Join(binDir, ".clawbench")
	require.NoError(t, os.MkdirAll(oldDataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDataDir, "ClawBench.db"), []byte("db"), 0o644))

	// Should not panic or error — just prints warning
	CheckLegacyLayout(binDir, dataDir)

	// Files should NOT be moved (no auto-migration)
	_, err := os.Stat(filepath.Join(dataDir, "ClawBench.db"))
	assert.True(t, os.IsNotExist(err), "file should not be auto-migrated")
}

func TestCheckLegacyLayout_LegacyDataAndConfig(t *testing.T) {
	binDir := t.TempDir()
	dataDir := t.TempDir()

	// Create legacy data dir
	oldDataDir := filepath.Join(binDir, ".clawbench")
	require.NoError(t, os.MkdirAll(oldDataDir, 0o755))

	// Create legacy config dir
	oldConfigDir := filepath.Join(binDir, "config")
	require.NoError(t, os.MkdirAll(oldConfigDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldConfigDir, "config.yaml"), []byte("port: 20000\n"), 0o644))

	// Should not panic or error — just prints warning
	CheckLegacyLayout(binDir, dataDir)

	// Config should NOT be moved (no auto-migration)
	_, err := os.Stat(filepath.Join(dataDir, "config", "config.yaml"))
	assert.True(t, os.IsNotExist(err), "config should not be auto-migrated")
}
