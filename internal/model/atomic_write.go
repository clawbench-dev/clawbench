package model

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path's contents atomically: it writes a sibling
// temporary file, gives it path's existing permission bits (0600 for a new
// file), and renames it into place.
//
// It lives in the model layer because two writers need it — the per-agent
// config-directory writer (service) and a backend's own config-file writer
// (a backend sub-package) — and neither may import the other. A plain
// os.WriteFile would be visibly worse here: an interrupted write leaves the
// user's own config file truncated, and these files hold the login state and
// provider settings a CLI needs to start at all.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".clawbench-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
