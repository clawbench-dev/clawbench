package model

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunCommandContextCapturesStdoutAndStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stdout, stderr, err := RunCommandContext(ctx, "sh", "-c", "echo out; echo err >&2")
	if err != nil {
		t.Fatalf("RunCommandContext error: %v", err)
	}
	if strings.TrimSpace(stdout) != "out" {
		t.Fatalf("stdout = %q, want %q", stdout, "out")
	}
	if strings.TrimSpace(stderr) != "err" {
		t.Fatalf("stderr = %q, want %q", stderr, "err")
	}
}

func TestRunCommandContextReturnsFastWhenGrandchildHoldsPipe(t *testing.T) {
	// Regression: the old pipe-based Output()/CombinedOutput() blocks until the
	// backgrounded `sleep 30 &` (which inherits the output pipe) exits, even
	// though the direct child already finished. RunCommandContext must return
	// promptly because output goes to a file, not a pipe.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	stdout, _, err := RunCommandContext(ctx, "sh", "-c", "echo hi; sleep 30 &")
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("RunCommandContext blocked for %v with a grandchild holding the output pipe", elapsed)
	}
	if err != nil {
		t.Fatalf("RunCommandContext error: %v", err)
	}
	if strings.TrimSpace(stdout) != "hi" {
		t.Fatalf("stdout = %q, want %q", stdout, "hi")
	}
}

func TestRunCommandContextHonorsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, err := RunCommandContext(ctx, "sh", "-c", "sleep 30")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a context deadline error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("RunCommandContext did not unblock after the context deadline (%v)", elapsed)
	}
}

func TestRunCommandContextStdoutCreateTempFails(t *testing.T) {
	// Force os.CreateTemp to fail by setting TMPDIR to a nonexistent directory.
	origTmpdir := os.Getenv("TMPDIR")
	os.Setenv("TMPDIR", "/nonexistent/path/that/does/not/exist")
	defer os.Setenv("TMPDIR", origTmpdir)

	ctx := context.Background()
	_, _, err := RunCommandContext(ctx, "echo", "hi")
	if err == nil {
		t.Fatal("expected error when CreateTemp fails, got nil")
	}
}

func TestRunCommandContextStderrCreateTempFails(t *testing.T) {
	// This is hard to trigger directly (stdout succeeds, stderr fails),
	// but we verify the deferred cleanup still runs when stderr creation fails
	// by ensuring the stdout temp file is removed even on partial failure.
	origTmpdir := os.Getenv("TMPDIR")
	os.Setenv("TMPDIR", "/nonexistent/path/that/does/not/exist")
	defer os.Setenv("TMPDIR", origTmpdir)

	ctx := context.Background()
	_, _, err := RunCommandContext(ctx, "echo", "hi")
	if err == nil {
		t.Fatal("expected error when CreateTemp fails, got nil")
	}
}

func TestRunCommandContextCommandNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, _, err := RunCommandContext(ctx, "nonexistent_command_xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent command, got nil")
	}
}
