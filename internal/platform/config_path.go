package platform

import (
	"os"
	"path/filepath"
)

// FindConfigPath searches for config.yaml in priority order:
//  1. <DataDir>/config/config.yaml (data directory)
//  2. config/config.yaml (CWD-relative, standard layout)
//
// It is used by the server at startup to locate its configuration file.
func FindConfigPath(dataDir string) string {
	configPath := filepath.Join(dataDir, "config", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join("config", "config.yaml")
	}
	return configPath
}
