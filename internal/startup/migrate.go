package startup

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// migrateFromBinDir moves data and config from the legacy BinDir layout to the
// new DataDir layout. Called once at startup when --data-dir was not explicitly
// provided.
//
// Legacy layout:
//
//	<BinDir>/.clawbench/ClawBench.db  (database)
//	<BinDir>/.clawbench/logs/         (logs)
//	<BinDir>/.clawbench/auto-password (password)
//	<BinDir>/config/config.yaml       (config file)
//	<BinDir>/config/agents/           (agent YAMLs)
//
// New layout:
//
//	<DataDir>/ClawBench.db            (database)
//	<DataDir>/logs/                   (logs)
//	<DataDir>/auto-password           (password)
//	<DataDir>/config/config.yaml      (config file)
//	<DataDir>/config/agents/          (agent YAMLs)
func MigrateFromBinDir(binDir, dataDir string) {
	oldDataDir := filepath.Join(binDir, ".clawbench")

	// Check if legacy data directory exists
	oldInfo, err := os.Stat(oldDataDir)
	if err != nil || !oldInfo.IsDir() {
		return // no legacy data, nothing to migrate
	}

	// Check if new data directory already has content
	newInfo, _ := os.Stat(dataDir)
	if newInfo != nil && dirHasFiles(dataDir) {
		// New dir exists and has files — skip migration to avoid overwrite
		slog.Info("skipping migration: new data directory already exists",
			slog.String("old", oldDataDir), slog.String("new", dataDir))
		return
	}

	fmt.Printf("Migrating data directory: %s -> %s\n", oldDataDir, dataDir)

	// Create target directory
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		slog.Error("failed to create data directory", slog.String("path", dataDir), slog.String("err", err.Error()))
		return
	}

	// Move all contents from old data dir to new
	moveDirContents(oldDataDir, dataDir)

	// Migrate config directory: <BinDir>/config/ -> <DataDir>/config/
	oldConfigDir := filepath.Join(binDir, "config")
	newConfigDir := filepath.Join(dataDir, "config")
	if configInfo, err := os.Stat(oldConfigDir); err == nil && configInfo.IsDir() {
		if _, err := os.Stat(newConfigDir); err != nil {
			// New config dir doesn't exist — move the whole directory
			fmt.Printf("Migrating config directory: %s -> %s\n", oldConfigDir, newConfigDir)
			if err := os.Rename(oldConfigDir, newConfigDir); err != nil {
				// Rename may fail across filesystems — fall back to copy
				slog.Warn("rename failed, copying instead", slog.String("err", err.Error()))
				if err := os.MkdirAll(newConfigDir, 0o755); err == nil {
					moveDirContents(oldConfigDir, newConfigDir)
				}
			}
		} else {
			// New config dir already exists — move individual files
			moveDirContents(oldConfigDir, newConfigDir)
		}
	}

	// Try to remove old data dir if empty
	removeDirIfEmpty(oldDataDir)

	fmt.Println("Migration complete.")
}

// moveDirContents moves all files/subdirectories from src into dst.
// Files that already exist in dst are skipped.
func moveDirContents(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		slog.Warn("failed to read source directory", slog.String("path", src), slog.String("err", err.Error()))
		return
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Skip if target already exists
		if _, err := os.Stat(dstPath); err == nil {
			slog.Info("skipping: already exists in target", slog.String("path", dstPath))
			continue
		}

		if err := os.Rename(srcPath, dstPath); err != nil {
			// Rename may fail across filesystems — fall back to recursive copy
			slog.Warn("rename failed, copying instead",
				slog.String("src", srcPath), slog.String("dst", dstPath), slog.String("err", err.Error()))
			copyRecursive(srcPath, dstPath)
		}
	}
}

// copyRecursive copies a file or directory tree from src to dst.
func copyRecursive(src, dst string) {
	info, err := os.Stat(src)
	if err != nil {
		slog.Warn("copy: cannot stat source", slog.String("path", src), slog.String("err", err.Error()))
		return
	}

	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode()); err != nil {
			slog.Warn("copy: cannot create directory", slog.String("path", dst), slog.String("err", err.Error()))
			return
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			slog.Warn("copy: cannot read directory", slog.String("path", src), slog.String("err", err.Error()))
			return
		}
		for _, entry := range entries {
			copyRecursive(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()))
		}
	} else {
		data, err := os.ReadFile(src)
		if err != nil {
			slog.Warn("copy: cannot read file", slog.String("path", src), slog.String("err", err.Error()))
			return
		}
		if err := os.WriteFile(dst, data, info.Mode()); err != nil {
			slog.Warn("copy: cannot write file", slog.String("path", dst), slog.String("err", err.Error()))
		}
	}
}

// dirHasFiles returns true if the directory contains at least one entry.
func dirHasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// removeDirIfEmpty removes the directory if it has no entries.
func removeDirIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = os.Remove(dir)
}
