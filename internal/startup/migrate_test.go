package startup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateFromBinDir_NoLegacyDir(t *testing.T) {
	binDir := t.TempDir()
	dataDir := t.TempDir()

	// No .clawbench under binDir — nothing should happen
	MigrateFromBinDir(binDir, dataDir)

	// dataDir should be empty
	entries, _ := os.ReadDir(dataDir)
	assert.Empty(t, entries)
}

func TestMigrateFromBinDir_LegacyDataDirOnly(t *testing.T) {
	binDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), ".clawbench")

	// Create legacy data dir with a file
	oldDataDir := filepath.Join(binDir, ".clawbench")
	require.NoError(t, os.MkdirAll(oldDataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDataDir, "auto-password"), []byte("test-pass"), 0o644))

	MigrateFromBinDir(binDir, dataDir)

	// File should be moved to new data dir
	data, err := os.ReadFile(filepath.Join(dataDir, "auto-password"))
	require.NoError(t, err)
	assert.Equal(t, "test-pass", string(data))

	// Old data dir should be removed (was empty after move)
	_, err = os.Stat(oldDataDir)
	assert.True(t, os.IsNotExist(err), "old data dir should be removed")
}

func TestMigrateFromBinDir_LegacyDataAndConfig(t *testing.T) {
	binDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), ".clawbench")

	// Create legacy data dir
	oldDataDir := filepath.Join(binDir, ".clawbench")
	require.NoError(t, os.MkdirAll(oldDataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDataDir, "ClawBench.db"), []byte("db-content"), 0o644))

	// Create legacy config dir
	oldConfigDir := filepath.Join(binDir, "config")
	require.NoError(t, os.MkdirAll(oldConfigDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldConfigDir, "config.yaml"), []byte("port: 20000\n"), 0o644))

	MigrateFromBinDir(binDir, dataDir)

	// Data file should be moved
	data, err := os.ReadFile(filepath.Join(dataDir, "ClawBench.db"))
	require.NoError(t, err)
	assert.Equal(t, "db-content", string(data))

	// Config should be moved
	data, err = os.ReadFile(filepath.Join(dataDir, "config", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "port: 20000\n", string(data))
}

func TestMigrateFromBinDir_NewDirAlreadyHasFiles(t *testing.T) {
	binDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), ".clawbench")

	// Create legacy data dir
	oldDataDir := filepath.Join(binDir, ".clawbench")
	require.NoError(t, os.MkdirAll(oldDataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDataDir, "auto-password"), []byte("old-pass"), 0o644))

	// Create new data dir with existing content
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "auto-password"), []byte("new-pass"), 0o644))

	MigrateFromBinDir(binDir, dataDir)

	// New dir should keep its existing file (not overwritten)
	data, err := os.ReadFile(filepath.Join(dataDir, "auto-password"))
	require.NoError(t, err)
	assert.Equal(t, "new-pass", string(data))
}

func TestMigrateFromBinDir_ConfigWithAgents(t *testing.T) {
	binDir := t.TempDir()
	dataDir := filepath.Join(t.TempDir(), ".clawbench")

	// Create legacy config with agents subdirectory
	oldConfigDir := filepath.Join(binDir, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(oldConfigDir, "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldConfigDir, "config.yaml"), []byte("port: 30000\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(oldConfigDir, "agents", "mock.yaml"), []byte("name: mock\n"), 0o644))

	// Also create a legacy data dir (otherwise migration won't trigger)
	oldDataDir := filepath.Join(binDir, ".clawbench")
	require.NoError(t, os.MkdirAll(oldDataDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(oldDataDir, "ClawBench.db"), []byte("db"), 0o644))

	MigrateFromBinDir(binDir, dataDir)

	// Config + agents should be moved
	data, err := os.ReadFile(filepath.Join(dataDir, "config", "config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "port: 30000\n", string(data))

	data, err = os.ReadFile(filepath.Join(dataDir, "config", "agents", "mock.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "name: mock\n", string(data))
}

func TestDirHasFiles(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, dirHasFiles(dir))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test"), []byte("x"), 0o644))
	assert.True(t, dirHasFiles(dir))
}
