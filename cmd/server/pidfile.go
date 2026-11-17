package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// writePIDFile records this process's PID in the data directory.
//
// Why a file at all: the instance lock tells a second server that the data
// directory is taken, but not WHO holds it. An embedder (the desktop shell) that
// finds a leftover server on its port has no way to ask "is this mine?" without
// a PID — and killing by image name would take down an unrelated standalone
// install the user is running. The file is best-effort: a failure only costs the
// crash-recovery path, which is why startup continues.
//
// The file is removed on a graceful exit. A hard kill leaves it behind, which is
// exactly the case the next launch needs it for; that reader must therefore
// verify the PID still belongs to a ClawBench process before signaling it.
func writePIDFile(dataDir string) (cleanup func(), err error) {
	path := filepath.Join(dataDir, "server.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}
