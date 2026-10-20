// Package platform provides cross-platform utility functions for path resolution.
package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IsWindows returns true when running on Windows.
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// UserHomeDir returns the user's home directory in a cross-platform way.
func UserHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback: try environment variables
		if IsWindows() {
			if home = os.Getenv("USERPROFILE"); home != "" {
				return home
			}
		} else {
			home = os.Getenv("HOME")
		}
	}
	return home
}

// ClaudeConfigDir returns the directory where Claude CLI stores configuration.
// All platforms: ~/.claude/
func ClaudeConfigDir() string {
	return filepath.Join(UserHomeDir(), ".claude")
}

// ClaudeProjectDir returns the session directory for a given project path.
// On Unix:    ~/.claude/projects/-home-user-project/
// On Windows: ~/.claude/projects/-C-Users-user-project/
//
// Claude CLI mangles the absolute path by replacing path separators with "-".
// On Windows both "/" and "\" need to be replaced.
//
// Note: Under WSL, Claude runs as a native Windows binary and mangles paths
// using Windows conventions, but this function uses the current Go binary's
// runtime.GOOS. If running under WSL with a Linux binary while Claude runs
// natively on Windows, the mangling may not match.
func ClaudeProjectDir(projectPath string) string {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		absPath = projectPath
	}
	mangled := ManglePath(absPath)
	return filepath.Join(ClaudeConfigDir(), "projects", mangled)
}

// ExpandTilde expands a leading ~/ in a path to the user's home directory.
// On Windows, ~\ is also handled.
// If the path does not start with ~, it is returned unchanged.
func ExpandTilde(path string) string {
	if path == "" {
		return path
	}
	// Handle ~/ (Unix) and ~\ (Windows)
	if len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == '\\') {
		return filepath.Join(UserHomeDir(), path[2:])
	}
	// Handle bare ~
	if path == "~" {
		return UserHomeDir()
	}
	return path
}

// ListRootPaths returns the filesystem root paths accessible to this application.
// On Linux/macOS: returns ["/"].
// On Windows: returns available drive roots (e.g. ["C:\\", "D:\\"]) followed by
// the UNC roots of remembered network shares (e.g. `\\nas\media`), so the
// project picker can offer SMB shares that were never mapped to a drive letter.
// The result is cached after the first call.
func ListRootPaths() []string {
	if cachedRootPaths != nil {
		return cachedRootPaths
	}
	if IsWindows() {
		cachedRootPaths = listWindowsDrives()
		cachedRootPaths = append(cachedRootPaths, listWindowsNetworkShares()...)
	} else {
		cachedRootPaths = []string{"/"}
	}
	return cachedRootPaths
}

var cachedRootPaths []string

// IsPathUnderAnyRoot checks whether absPath is under at least one of the
// given root paths. On Unix this is effectively "is it an absolute path";
// on Windows it ensures the path is under an available drive — or is a UNC
// share path, which belongs to no drive and is therefore its own root.
// Both absPath and each root must be absolute paths.
func IsPathUnderAnyRoot(absPath string, roots []string) bool {
	// A UNC path belongs to no drive, so no drive-letter root can ever contain
	// it. Accepting share paths is what lets a user select \\server\share (and
	// browse inside it) without mapping a drive letter first. The share is the
	// volume boundary on Windows: ".." cannot climb above it, and the device
	// namespaces (\\.\, \\?) are rejected by UNCShareRoot.
	//
	// Windows-only on purpose: on POSIX "//server/share" is an ordinary
	// absolute path (the kernel collapses leading slashes), so honoring it
	// unconditionally would let "//etc/passwd" bypass a narrower root.
	if IsWindows() {
		if _, ok := UNCShareRoot(absPath); ok {
			return true
		}
	}
	for _, root := range roots {
		if isPathUnderRoot(absPath, root) {
			return true
		}
	}
	return false
}

// UNCShareRoot reports the share root (`\\server\share`) of a Windows UNC path
// and whether the path is a UNC share path at all.
//
// Accepted: `\\server\share`, `//server/share/docs`, mixed separators — the
// frontend sends forward slashes (it normalizes them for DOM attributes) while
// the OS hands us backslashes.
//
// Rejected: a bare `\\server` (the network neighborhood, which os.ReadDir
// cannot list), and the device namespaces `\\.\` / `\\?\` — those address
// devices rather than shares and would otherwise slip through every root check.
//
// Deliberately pure string parsing (no filepath.VolumeName) so it behaves the
// same in tests on every platform; the OS-specific reachability is decided by
// the caller.
func UNCShareRoot(p string) (string, bool) {
	if len(p) < 2 || !isPathSep(p[0]) || !isPathSep(p[1]) {
		return "", false
	}
	parts := strings.FieldsFunc(p[2:], func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) < 2 {
		return "", false
	}
	server, share := parts[0], parts[1]
	if server == "." || server == "?" || server == ".." {
		return "", false
	}
	if share == "." || share == ".." {
		return "", false
	}
	return `\\` + server + `\` + share, true
}

// IsUNCShareRoot reports whether p IS a UNC share root (`\\server\share`), not
// merely a path under one. Such a path is its own top level: the network
// neighborhood above it cannot be listed, so there is no parent to offer.
//
// Case-insensitive (share names are), and separator-agnostic, so the two
// spellings the frontend and the OS use compare equal.
func IsUNCShareRoot(p string) bool {
	share, ok := UNCShareRoot(p)
	if !ok {
		return false
	}
	return strings.EqualFold(canonicalUNC(p), canonicalUNC(share))
}

// canonicalUNC strips trailing separators and unifies separator style, so two
// spellings of the same share compare equal.
func canonicalUNC(p string) string {
	p = trimTrailingSeps(p)
	return strings.NewReplacer("/", `\`).Replace(p)
}

func trimTrailingSeps(p string) string {
	return strings.TrimRight(p, `/\`)
}

func isPathSep(c byte) bool {
	return c == '/' || c == '\\'
}

// isPathUnderRoot checks that absPath is under root by resolving symlinks
// on both sides before comparing, preventing symlink traversal attacks.
func isPathUnderRoot(absPath, root string) bool {
	evalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// Root doesn't exist — fall back to lexical comparison
		evalRoot = filepath.Clean(root)
	} else {
		evalRoot = filepath.Clean(evalRoot)
	}

	// Ensure root ends with separator for prefix matching (unless root is "/" itself)
	prefix := evalRoot
	if !strings.HasSuffix(evalRoot, string(filepath.Separator)) {
		prefix = evalRoot + string(filepath.Separator)
	}

	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			// Some other error (permission, etc.) — fall back to lexical check
			cleanPath := filepath.Clean(absPath)
			return strings.HasPrefix(cleanPath, prefix) || cleanPath == evalRoot
		}
		// Target doesn't exist — resolve parent directory step by step
		evalPath = resolveExistingPath(absPath, evalRoot)
		if evalPath == "" {
			// Can't resolve — fall back to lexical comparison
			cleanPath := filepath.Clean(absPath)
			return strings.HasPrefix(cleanPath, prefix) || cleanPath == evalRoot
		}
	}
	evalPath = filepath.Clean(evalPath)
	return strings.HasPrefix(evalPath, prefix) || evalPath == evalRoot
}

// resolveExistingPath walks up from absPath until it finds an existing
// directory, then resolves from there. Returns empty string if resolution fails.
// root should be the eval'd (symlink-resolved) root path.
func resolveExistingPath(absPath, root string) string {
	dir := absPath
	for {
		evalDir, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return evalDir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		// Check whether parent is still under root.  On macOS /var is a
		// symlink to /private/var, so a lexical prefix check fails; resolve
		// the parent's symlinks before comparing against the eval'd root.
		evalParent, pErr := filepath.EvalSymlinks(parent)
		if pErr != nil {
			// Parent doesn't exist either — fall back to lexical check
			cleanParent := filepath.Clean(parent)
			if !strings.HasPrefix(cleanParent, root) && cleanParent != root {
				return ""
			}
		} else {
			cleanParent := filepath.Clean(evalParent)
			if !strings.HasPrefix(cleanParent, root) && cleanParent != root {
				return ""
			}
		}
		dir = parent
	}
}

// ManglePath converts an absolute path into Claude's mangled directory name.
// All path separators (/ and \) are replaced with "-".
// Drive letters on Windows (e.g. "C:") become "C-" in the result.
func ManglePath(absPath string) string {
	return ManglePathForOS(absPath, runtime.GOOS)
}

// ManglePathForOS converts an absolute path into Claude's mangled directory name
// for a specific OS. This is useful for testing and for WSL scenarios where
// the binary's runtime.GOOS differs from the Claude CLI's platform.
func ManglePathForOS(absPath string, goos string) string {
	// Replace all backslashes with forward slashes first
	normalized := strings.ReplaceAll(absPath, "\\", "/")
	// Replace all forward slashes with dashes
	mangled := strings.ReplaceAll(normalized, "/", "-")
	// On Windows, replace colon after drive letter
	if goos == "windows" && len(mangled) > 1 && mangled[1] == ':' {
		mangled = mangled[:1] + "-" + mangled[2:]
	}
	return mangled
}
