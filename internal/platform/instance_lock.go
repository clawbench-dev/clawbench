package platform

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// instanceLockFile is the lock file a running server holds inside its data
// directory for the whole process lifetime.
const instanceLockFile = ".instance.lock"

// instanceLockPoll is how often a starting process retries the lock while it
// waits for the outgoing process to finish exiting.
const instanceLockPoll = 250 * time.Millisecond

// ErrDataDirInUse reports that another live ClawBench process holds the lock
// on this data directory.
//
// Two servers on ONE data directory corrupt each other rather than merely
// colliding on ports: the startup "a turn was interrupted by a restart" repair
// (InitDB's orphan rewrite + StartRestartResume) reads a streaming=1 row as
// proof that the owning process died, so a second instance rewrites the FIRST
// instance's in-flight reply into "cancelled" and starts a duplicate turn. The
// user then sees a finished bubble while the still-running turn keeps the
// stop button and the list animation alive. Refusing to start is the only
// cheap way to keep that invariant true.
var ErrDataDirInUse = errors.New("data directory is already in use by another ClawBench instance")

// InstanceLock is an exclusive OS-level lock on a data directory.
//
// The lock is held by the open file description, so the OS drops it when the
// process exits for ANY reason (clean shutdown, panic, SIGKILL). There is no
// stale-lock problem: a crashed server never blocks the next start.
type InstanceLock struct {
	file *os.File
	// ov is the OVERLAPPED the Windows implementation must keep alive for the
	// lifetime of the lock. Unused (nil) elsewhere.
	ov *fileLockOverlapped
}

// AcquireInstanceLock takes the exclusive lock for dir, waiting up to wait for
// the previous holder to go away.
//
// The wait exists for restarts, not for a second server: a replacement process
// is launched while the outgoing one is still draining (it releases the PORT
// first and does its final writes afterwards, and the lock — which is what
// protects the database — is only released when the process exits). Failing
// immediately would turn every restart into an error; waiting the full window
// and then failing still rejects a genuine second instance.
//
// Because the lock outlives the port, `wait` must cover the whole shutdown
// drain, not just the port handover. See cmd/server/main.go's instanceLockWait
// for the sizing and for the 2026-11-11 outage this used to cause.
func AcquireInstanceLock(dir string, wait time.Duration) (*InstanceLock, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	// 0600: the lock file carries no data, but it lives in the data directory
	// and a restrictive mode keeps it from being world-readable/writable.
	f, err := os.OpenFile(filepath.Join(dir, instanceLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}

	start := time.Now()
	deadline := start.Add(wait)
	waitLogged := false
	for {
		ov, lockErr := lockFileExclusive(f)
		if lockErr == nil {
			if waitLogged {
				slog.Info("data directory lock acquired after waiting",
					slog.String("data_dir", dir),
					slog.Duration("waited", time.Since(start)))
			}
			return &InstanceLock{file: f, ov: ov}, nil
		}
		if !isLockBusy(lockErr) {
			_ = f.Close()
			return nil, fmt.Errorf("lock data directory: %w", lockErr)
		}
		// Log once, not per poll: a restart's drain is expected, but a silent
		// wait is what made the 2026-11-11 outage hard to read — the log only
		// showed "server starting" followed by "refusing to start".
		if !waitLogged && wait > 0 {
			waitLogged = true
			slog.Info("data directory is locked by another instance, waiting for it to exit",
				slog.String("data_dir", dir),
				slog.Duration("wait", wait))
		}
		if !time.Now().Before(deadline) {
			_ = f.Close()
			return nil, ErrDataDirInUse
		}
		time.Sleep(instanceLockPoll)
	}
}

// Release drops the lock and closes the file. Safe to call once; the OS would
// release everything at exit anyway, this just frees the next start sooner.
func (l *InstanceLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	unlockFile(l.file, l.ov)
	_ = l.file.Close()
	l.file = nil
}
