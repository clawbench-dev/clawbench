// Package userguide embeds the ClawBench user manual — the Markdown files in
// this directory — into the binary and materializes them under the data
// directory at startup.
//
// The manual lives here rather than in a copied build directory on purpose:
// //go:embed patterns are relative to the package directory and cannot escape
// it with "..", so the only way to embed files that are already tracked as
// sources is to put the directive next to them. That keeps the embedded copy
// from ever drifting from the documentation, and needs no build.sh or CI step.
package userguide

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

//go:embed *.md
var embedded embed.FS

// baseDirName is the version-independent directory under the data dir. The
// extracted copy lives at <dataDir>/user-guide/<version>/ so each build's
// manual is addressable on its own and older versions can be pruned wholesale.
const baseDirName = "user-guide"

var (
	mu  sync.RWMutex
	dir string
)

// Dir returns the absolute path of the extracted manual directory, or "" when
// extraction has not run or failed. The /cb-user-guide command uses it both as
// the precondition (no directory means the command cannot be served) and as the
// path it hands to the AI.
func Dir() string {
	mu.RLock()
	defer mu.RUnlock()
	return dir
}

// SetDir overrides the extracted directory. Extract calls it in production;
// tests use it to point the handler at a fixture without touching disk layout.
func SetDir(d string) {
	mu.Lock()
	defer mu.Unlock()
	dir = d
}

// unsafeVersion matches every character that may not appear in a single path
// element. The version is injected from git/ldflags and can contain "/" (a
// branch name via `git describe`), ":" or spaces — any of which would turn the
// join below into a path escape or an invalid name on some platform.
var unsafeVersion = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// safeVersion maps an arbitrary version string to one safe path element. The
// result is never "." or "..", so joining it can never point at the parent.
func safeVersion(v string) string {
	v = unsafeVersion.ReplaceAllString(v, "_")
	v = strings.Trim(v, ".")
	if v == "" {
		return "unknown"
	}
	return v
}

// Extract writes the embedded manual to <dataDir>/user-guide/<version>/ and
// removes every other version directory, returning the extracted path.
//
// It runs on every startup and is cheap (the manual is a few hundred KB), which
// is also how a new build's version replaces the previous directory. The write
// is staged in a temporary sibling and renamed into place so a crash can never
// leave a half-written directory that Dir would advertise as complete.
//
// Failure is non-fatal to the caller: it logs and continues, and the
// /cb-user-guide command degrades to a clear "not available" error rather than
// injecting a prompt that points at files which do not exist.
func Extract(dataDir, version string) (string, error) {
	if dataDir == "" {
		return "", fmt.Errorf("userguide: empty data dir")
	}
	parent := filepath.Join(dataDir, baseDirName)
	final := filepath.Join(parent, safeVersion(version))

	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("userguide: create %s: %w", parent, err)
	}

	tmp, err := os.MkdirTemp(parent, ".tmp-*")
	if err != nil {
		return "", fmt.Errorf("userguide: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err := writeFiles(embedded, tmp); err != nil {
		return "", err
	}

	// Re-extracting the same version (a rebuilt dev binary) must replace the
	// existing copy: os.Rename onto a non-empty directory fails, so clear it
	// first. A failure here is reported rather than ignored — proceeding would
	// leave the stale copy in place while reporting success.
	if err := os.RemoveAll(final); err != nil {
		return "", fmt.Errorf("userguide: clear %s: %w", final, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", fmt.Errorf("userguide: rename into %s: %w", final, err)
	}

	pruneOldVersions(parent, safeVersion(version))
	SetDir(final)
	return final, nil
}

// writeFiles copies every regular file at the root of fsys into dst.
func writeFiles(fsys fs.FS, dst string) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("userguide: read embedded: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return fmt.Errorf("userguide: read %s: %w", e.Name(), err)
		}
		// 0644 matches the other data-dir files (config, theme, …).
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			return fmt.Errorf("userguide: write %s: %w", e.Name(), err)
		}
	}
	return nil
}

// pruneOldVersions removes every directory under parent except keep and the
// staging temp dirs (which the caller's deferred cleanup handles). A failure is
// logged, not returned: a stale manual directory wastes a little disk but must
// not fail the extraction that just succeeded.
func pruneOldVersions(parent, keep string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		slog.Warn("userguide: cannot list versions for pruning", "error", err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keep || strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(parent, e.Name())); err != nil {
			slog.Warn("userguide: failed to remove old version", "dir", e.Name(), "error", err)
		}
	}
}
