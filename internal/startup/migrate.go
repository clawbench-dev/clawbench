package startup

import (
	"fmt"
	"os"
	"path/filepath"
)

// CheckLegacyLayout checks if the legacy BinDir data layout exists and prints
// a prominent warning asking the user to manually migrate to the new DataDir layout.
func CheckLegacyLayout(binDir, dataDir string) {
	oldDataDir := filepath.Join(binDir, ".clawbench")

	oldInfo, err := os.Stat(oldDataDir)
	if err != nil || !oldInfo.IsDir() {
		return // no legacy data, nothing to warn about
	}

	// Also check for legacy config directory
	oldConfigDir := filepath.Join(binDir, "config")
	hasLegacyConfig := false
	if configInfo, err := os.Stat(oldConfigDir); err == nil && configInfo.IsDir() {
		hasLegacyConfig = true
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("  WARNING: Legacy data layout detected!")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Printf("  Old data directory: %s\n", oldDataDir)
	if hasLegacyConfig {
		fmt.Printf("  Old config directory: %s\n", oldConfigDir)
	}
	fmt.Printf("  New data directory:  %s\n", dataDir)
	fmt.Println()
	fmt.Println("  Please migrate manually:")
	fmt.Printf("    mv %s/* %s/\n", oldDataDir, dataDir)
	if hasLegacyConfig {
		fmt.Printf("    mv %s %s/config\n", oldConfigDir, dataDir)
	}
	fmt.Println()
	fmt.Println("  See documentation for details.")
	fmt.Println("========================================")
	fmt.Println()
}
