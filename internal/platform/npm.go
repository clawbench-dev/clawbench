package platform

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// cmdEntryRe extracts the Node.js entry point path from a Windows .cmd wrapper.
// Matches the node_modules-relative path inside quoted strings, e.g.:
//
//	"%dp0%\node_modules\@tencent-ai\codebuddy-code\bin\codebuddy" %*
//	node  "%dp0%\node_modules\@anthropic\claude-code\bin\claude" %*
//
// The captured group is the path after node_modules/, e.g.:
//
//	@tencent-ai\codebuddy-code\bin\codebuddy
var cmdEntryRe = regexp.MustCompile(`node_modules[\\/](.+?)"\s`)

// ResolveCLIPath resolves the real file path for a CLI command, handling
// Windows npm .cmd wrappers. On non-Windows (or when the command is not a
// .cmd file), it uses exec.LookPath + filepath.EvalSymlinks — the same
// logic that was used before this function was introduced.
//
// On Windows with .cmd files, it parses the .cmd content to find the actual
// JS entry point path (e.g. %dp0%\node_modules\@scope\package\bin\cmdname)
// and resolves it relative to the .cmd file's directory.
//
// Returns the resolved file path (always a file, never a directory),
// or empty string if the command cannot be found.
func ResolveCLIPath(cmdName string) string {
	path, err := exec.LookPath(cmdName)
	if err != nil {
		return ""
	}

	// On Windows, check if we got a .cmd wrapper that needs special handling
	if IsWindows() && strings.HasSuffix(strings.ToLower(path), ".cmd") {
		if resolved := resolveCmdWrapper(path); resolved != "" {
			return resolved
		}
		// Fallback: EvalSymlinks on the .cmd file itself
		// (won't help for npm .cmd wrappers, but covers non-npm .cmd files)
		slog.Debug("resolveCLIPath: .cmd wrapper parsing failed, falling back to EvalSymlinks", "cmd", cmdName, "path", path)
	}

	// Non-Windows or non-.cmd: resolve symlinks (existing behavior)
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return realPath
}

// resolveCmdWrapper parses a Windows .cmd file to extract the actual JS entry
// point path. npm .cmd wrappers contain lines like:
//
//	"%dp0%\node.exe"  "%dp0%\node_modules\@tencent-ai\codebuddy-code\bin\codebuddy" %*
//
// We extract the node_modules-relative path and resolve it against the .cmd
// file's directory to produce an absolute path to the actual JS file.
func resolveCmdWrapper(cmdPath string) string {
	data, err := os.ReadFile(cmdPath)
	if err != nil {
		slog.Debug("resolveCmdWrapper: cannot read .cmd file", "path", cmdPath, "error", err)
		return ""
	}

	content := string(data)
	matches := cmdEntryRe.FindStringSubmatch(content)
	if len(matches) < 2 {
		slog.Debug("resolveCmdWrapper: no node_modules path found in .cmd file", "path", cmdPath)
		return ""
	}

	// matches[1] is the relative path inside node_modules, e.g.:
	//   @tencent-ai\codebuddy-code\bin\codebuddy
	//   @anthropic\claude-code\bin\claude
	//   @openai\codex\bin\codex
	relPath := matches[1]

	// Normalize separators for the current OS
	if runtime.GOOS == "windows" {
		relPath = strings.ReplaceAll(relPath, "/", string(filepath.Separator))
	} else {
		relPath = strings.ReplaceAll(relPath, `\`, string(filepath.Separator))
	}

	// The .cmd file lives in the npm global bin directory (e.g. %APPDATA%\npm\).
	// node_modules/ is a sibling directory next to the .cmd files.
	// So we go up from the .cmd file to its parent, then join node_modules/<relPath>.
	cmdDir := filepath.Dir(cmdPath)
	resolved := filepath.Join(cmdDir, "node_modules", relPath)

	// Verify the file exists
	if _, err := os.Stat(resolved); err != nil {
		slog.Debug("resolveCmdWrapper: resolved path does not exist", "resolved", resolved, "error", err)
		return ""
	}

	slog.Debug("resolveCmdWrapper: resolved .cmd to JS entry point", "cmd", cmdPath, "resolved", resolved)
	return resolved
}
