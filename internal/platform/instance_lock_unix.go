//go:build !windows

package platform

import (
	"errors"
	"os"
	"syscall"
)

// fileLockOverlapped carries whatever the platform needs to keep alive for the
// lifetime of a held lock. POSIX flock keeps no such state, so it is empty.
type fileLockOverlapped = struct{}

// lockFileExclusive takes a non-blocking exclusive flock on f. flock is tied to
// the open file description, so the lock disappears when the process exits
// without any cleanup — that is what makes a crashed server unable to block the
// next start.
func lockFileExclusive(f *os.File) (*fileLockOverlapped, error) {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	return nil, nil
}

func unlockFile(f *os.File, _ *fileLockOverlapped) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}

// isLockBusy reports whether err means "someone else holds it" rather than a
// real I/O failure. Linux returns EWOULDBLOCK (== EAGAIN), macOS EAGAIN.
func isLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
