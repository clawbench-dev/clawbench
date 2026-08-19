package ai

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawbench/internal/model"
)

// codebuddyPluginLoadDelay is the delay before re-emitting the commands_update
// event after stream start, to allow time for CodeBuddy's PluginManager to
// finish loading and send an updated AvailableCommandsUpdate. Based on observed
// plugin load times of ~2-3s (issue #383 logs: PluginsProductProvider.provide
// active=2 at T+3s), plus a 1s safety margin.
const codebuddyPluginLoadDelay = 4 * time.Second

// isCodeBuddyBackend returns true if the agent uses CodeBuddy as its ACP backend.
func isCodeBuddyBackend(agent *model.Agent) bool {
	return agent != nil && agent.Backend == "codebuddy"
}

// ScanCodeBuddyPluginCommands scans ~/.codebuddy/plugins/cache/ for slash commands
// defined in commands/*.md files. Returns []AvailableCommandInfo parsed from YAML
// frontmatter (filename = command name, description from frontmatter).
// Returns nil if directory doesn't exist or no commands found.
// Not goroutine-safe — call once during connection spawn.
func ScanCodeBuddyPluginCommands() []AvailableCommandInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("acp plugin scan: cannot resolve home dir", "error", err)
		return nil
	}
	cacheDir := filepath.Join(home, ".codebuddy", "plugins", "cache")
	return scanPluginCommandsFromDir(cacheDir)
}

// scanPluginCommandsFromDir scans the given directory tree for commands/*.md files.
// Separated for testability (injectable directory path).
func scanPluginCommandsFromDir(cacheDir string) []AvailableCommandInfo {
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		slog.Debug("acp plugin scan: cache directory does not exist", "path", cacheDir)
		return nil
	}

	var cmds []AvailableCommandInfo

	err := filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			slog.Debug("acp plugin scan: walk error", "path", path, "error", err)
			return nil // continue walking
		}
		if d.IsDir() {
			return nil // descend into all directories
		}
		// Only process .md files inside "commands" directories
		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		parentDir := filepath.Base(filepath.Dir(path))
		if parentDir != "commands" {
			return nil
		}

		cmdName := strings.TrimSuffix(d.Name(), ".md")
		if cmdName == "" {
			return nil
		}

		data, err := os.ReadFile(path) //nolint:gosec // G122: path is from filepath.WalkDir which resolves symlinks; plugin commands are trusted local files
		if err != nil {
			slog.Debug("acp plugin scan: cannot read command file", "path", path, "error", err)
			return nil
		}

		description, ok := parseCommandFrontmatter(data)
		if !ok {
			slog.Debug("acp plugin scan: no valid frontmatter in command file", "path", path)
			return nil
		}

		cmds = append(cmds, AvailableCommandInfo{
			Name:        cmdName,
			Description: description,
		})
		return nil
	})
	if err != nil {
		slog.Debug("acp plugin scan: walk failed", "error", err)
		return nil
	}

	if len(cmds) == 0 {
		return nil
	}

	// Sort by name for deterministic output
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Name < cmds[j].Name
	})

	slog.Info("acp plugin scan: found CodeBuddy plugin commands", "count", len(cmds))
	return cmds
}

// parseCommandFrontmatter parses YAML frontmatter from a commands/*.md file.
// Returns (description, true) on success, ("", false) on failure.
//
// Expected format:
//
//	---
//	description: "Some description text"
//	disable-model-invocation: true
//	---
//
// NOTE: This is a minimal frontmatter parser for the known CodeBuddy plugin format.
// It handles single-line descriptions (quoted or unquoted). Multi-line YAML scalars
// (folded > / literal |) are not supported — they're not used in current plugins.
// The opening --- must appear at the start of the file (line 1, column 1).
func parseCommandFrontmatter(data []byte) (string, bool) {
	content := string(data)

	// Opening --- must be at the start of the file (line 1, column 1).
	// This avoids matching --- that appears inside the markdown body.
	if !strings.HasPrefix(content, "---") {
		return "", false
	}
	afterOpen := content[3:]

	// Find closing --- (must be on its own line)
	closeIdx := strings.Index(afterOpen, "\n---")
	if closeIdx < 0 {
		return "", false
	}
	frontmatter := afterOpen[:closeIdx]

	// Extract description key
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			// Strip surrounding quotes if present
			desc = strings.Trim(desc, "\"'")
			if desc == "" {
				return "", false
			}
			return desc, true
		}
	}

	return "", false
}
