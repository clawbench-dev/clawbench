//go:build windows

package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// fileLockOverlapped keeps the OVERLAPPED alive for as long as the lock is
// held: Windows may still reference it after LockFileEx returns.
type fileLockOverlapped = windows.Overlapped

// lockFileExclusive takes a non-blocking exclusive byte-range lock on the first
// byte of f. Windows releases it when the process exits, same guarantee flock
// gives on POSIX.
func lockFileExclusive(f *os.File) (*fileLockOverlapped, error) {
	ov := &windows.Overlapped{}
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ov,
	)
	if err != nil {
		return nil, err
	}
	return ov, nil
}

func unlockFile(f *os.File, ov *fileLockOverlapped) {
	if ov == nil {
		ov = &windows.Overlapped{}
	}
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ov)
}

// isLockBusy reports whether err means "someone else holds it" rather than a
// real I/O failure.
func isLockBusy(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
