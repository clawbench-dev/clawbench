package platform_test

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/platform"

	"github.com/stretchr/testify/assert"
)

func TestFindConfigPath_DataDirConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(configDir, 0o755)
	_ = os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("port: 12345"), 0o644)

	path := platform.FindConfigPath(tmpDir)
	assert.Equal(t, filepath.Join(tmpDir, "config", "config.yaml"), path)
}

func TestFindConfigPath_FallbackToCWD(t *testing.T) {
	tmpDir := t.TempDir()
	// No config dir under tmpDir — should fall back to CWD-relative path
	path := platform.FindConfigPath(tmpDir)
	assert.Equal(t, filepath.Join("config", "config.yaml"), path)
}
