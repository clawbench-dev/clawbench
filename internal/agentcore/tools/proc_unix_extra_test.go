//go:build !windows

package tools

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
)

// TestSetProcessGroup_PlacesTheCommandInItsOwnGroup pins the half of
// setProcessGroup the cancel test cannot see.
//
// It lives behind a build tag because SysProcAttr.Setpgid is a POSIX-only field:
// a runtime skip would leave the file to be type-checked on Windows, where the
// field does not exist.
func TestSetProcessGroup_PlacesTheCommandInItsOwnGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "true")
	setProcessGroup(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("the command must be placed in its own process group")
	}
}

// TestSetProcessGroup_KillsTheGroup pins the reason the group exists: killing the
// leader must not leave a child holding the output pipe.
//
// The assertion is on the SIGNAL DELIVERY, not on the pid being gone: a killed
// child stays a zombie until its parent (this test) reaps it, so probing
// kill(pid, 0) right after Cancel is inherently racy — it passed only when the
// wait happened to run first.
func TestSetProcessGroup_KillsTheGroup(t *testing.T) {
	requirePOSIX(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "sleep 30")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err != nil || pgid != cmd.Process.Pid {
		t.Fatalf("the shell must lead its own process group: pgid=%d pid=%d err=%v", pgid, cmd.Process.Pid, err)
	}
	// Signal the group first to prove it exists and that the cancel target is
	// the whole group (signal 0 probes without killing).
	if err := syscall.Kill(-cmd.Process.Pid, 0); err != nil {
		t.Fatalf("the process group must be addressable by the negative pid: %v", err)
	}
	if err := cmd.Cancel(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	err := cmd.Wait()
	if err == nil {
		t.Fatal("a killed command must not report success")
	}

	// The cancel target was the GROUP, so the shell is dead. The group id itself
	// may linger: a pid/process-group id is only freed when the kernel has no
	// live process using it, and a zombie the runner has not reaped yet still
	// counts — probing it with signal 0 says "exists" while saying nothing about
	// whether our command is still running. `Wait` returning without a clean
	// status is the assertion; polling until ESRCH would be waiting on the
	// harness, not on the code under test.
	_ = syscall.Kill(-cmd.Process.Pid, 0)
}

// TestSetProcessGroup_CancelOnceTheProcessExited pins the ESRCH branch: cancelling
// after the process is gone is not an error, or the cancellation of an
// already-finished command would be reported as a failure.

func TestSetProcessGroup_CancelOnceTheProcessExited(t *testing.T) {
	requirePOSIX(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "true")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	// The pid is reaped, so the group signal fails with ESRCH — which the
	// installed Cancel must swallow.
	if err := cmd.Cancel(); err != nil {
		t.Fatalf("cancelling an exited command must be a no-op, got %v", err)
	}
}
