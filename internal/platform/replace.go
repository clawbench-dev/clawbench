package platform

import (
	"io"
	"os"
	"path/filepath"
)

// renameFile is the rename ReplaceBinary uses. It is a variable so tests can
// force a cross-device failure (EXDEV on Unix, ERROR_NOT_SAME_DEVICE on
// Windows) and exercise the staged-copy fallback. Real deployments use
// os.Rename.
var renameFile = os.Rename

// ReplaceBinary replaces dst with the contents of src, leaving dst executable.
// It is how a self-upgrade swaps the running program's binary on disk.
//
// The defining property is that **dst is never removed or moved aside before
// its replacement already exists**: the old file stays valid until the instant
// it is overwritten. Callers are upgrade helpers, and their failure mode
// without this property is "the install directory no longer contains a
// binary" — the service cannot be started again and the user cannot retry from
// the UI, because there is no running process left to serve the retry. Keeping
// dst intact on every failure path turns a dead installation into a failed
// upgrade, which is recoverable.
//
// Two strategies, in order:
//
//  1. rename(src, dst). Atomic, and the only option when dst must change in a
//     single step. On Unix this succeeds even when dst is the running
//     process's own image (the process keeps the old inode). On Windows it
//     fails when src and dst are on different volumes, because os.Rename maps
//     to MoveFileEx with MOVEFILE_REPLACE_EXISTING but not MOVEFILE_COPY_ALLOWED.
//  2. Staged copy. Copy src into a temp file *in dst's directory*, then rename
//     that over dst. Both halves are same-filesystem, so the second rename
//     cannot fail with a cross-device error — this is what makes a temp file
//     under %TEMP% (often C:) able to replace an install on another drive
//     (often D:/E:/F:).
//
// Staging in dst's directory rather than writing dst in place also keeps the
// permission requirement at directory-write, matching what the upgrade
// preflight checks: overwriting dst directly would additionally require dst
// itself to be writable, which fails for e.g. a root-owned 0755 binary sitting
// in a user-writable directory.
func ReplaceBinary(src, dst string) error {
	if err := renameFile(src, dst); err == nil {
		return os.Chmod(dst, 0o755) //nolint:gosec // G302: binary must be executable
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".clawbench-replace-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	// os.Rename directly, not the hook: the source now lives in dst's
	// directory, so this is the real same-filesystem rename and must not be
	// simulated away.
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Chmod(dst, 0o755) //nolint:gosec // G302: binary must be executable
}
