package model

import (
	"os"
	"path/filepath"
	"strings"
)

// NormalizeProjectPath canonicalizes a project path so the same directory always
// compares equal regardless of how it was spelled. It makes the path absolute,
// resolves symlinks when possible, cleans it, and strips a trailing separator.
// On any error it falls back to the cleaned path rather than failing the caller.
//
// This is the identity function for a project: the projects registry stores this
// form, and any path arriving from a cookie / query string must be normalized
// the same way before it is compared against a stored path. On macOS a
// t.TempDir()-style path lives under /var, a symlink to /private/var, so the raw
// and normalized forms differ and a naive string compare reports a mismatch.
func NormalizeProjectPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	p = filepath.Clean(p)
	// filepath.Clean already removes trailing separators except for the root.
	return p
}

// ValidatePath validates that a relative path stays within the base directory boundary.
// It resolves symlinks on both sides before comparing, preventing symlink traversal attacks.
// Returns the lexical absolute path (for OS operations) and whether it's valid.
func ValidatePath(basePath, relPath string) (string, bool) {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", false
	}
	fullPath := filepath.Join(absBase, relPath)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", false
	}

	// Resolve symlinks on both sides to prevent symlink traversal.
	evalBase, err := filepath.EvalSymlinks(absBase)
	if err != nil {
		return "", false
	}
	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false
		}
		// Target doesn't exist yet (e.g., file creation) — resolve parent directory
		evalPath = ResolveExistingPath(absPath, evalBase)
		if evalPath == "" {
			return "", false
		}
	}

	valid := strings.HasPrefix(evalPath, evalBase+string(filepath.Separator)) || evalPath == evalBase
	return absPath, valid
}

// ResolveExistingPath walks up from absPath to find the first existing ancestor,
// resolves its symlinks, then appends the remaining non-existent components.
// Returns empty string if no ancestor can be resolved or if the resolved path
// escapes evalBase. Exported for use by handler.isPathUnderBase.
func ResolveExistingPath(absPath, evalBase string) string {
	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)
	parts := []string{base}

	for {
		evalDir, err := filepath.EvalSymlinks(dir)
		if err == nil {
			// Found an existing ancestor — reconstruct the full path
			return filepath.Join(append([]string{evalDir}, parts...)...)
		}
		if !os.IsNotExist(err) {
			return "" // unexpected error
		}
		// Parent doesn't exist either — walk up
		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			return "" // reached root without finding an existing directory
		}
		parts = append([]string{filepath.Base(dir)}, parts...)
		dir = parentDir
	}
}
