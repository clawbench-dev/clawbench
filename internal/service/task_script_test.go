package service

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// skipOnWindows skips tests that rely on POSIX shell syntax.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix-specific shell syntax")
	}
}

func TestRunTaskScript_SkippedOnNoOutput(t *testing.T) {
	res := RunTaskScript(context.Background(), "true", "", 0)
	if res.Outcome != ScriptSkipped {
		t.Fatalf("outcome = %v, want ScriptSkipped (err=%v)", res.Outcome, res.Err)
	}
	if res.Stdout != "" || res.Stderr != "" {
		t.Fatalf("stdout=%q stderr=%q, want both empty", res.Stdout, res.Stderr)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
}

func TestRunTaskScript_SkippedOnWhitespaceOnlyOutput(t *testing.T) {
	// `echo` emits only a trailing newline; whitespace-only output must still
	// count as no output.
	res := RunTaskScript(context.Background(), "echo", "", 0)
	if res.Outcome != ScriptSkipped {
		t.Fatalf("outcome = %v, want ScriptSkipped (stdout=%q stderr=%q err=%v)",
			res.Outcome, res.Stdout, res.Stderr, res.Err)
	}
}

func TestRunTaskScript_ProducedWithStdout(t *testing.T) {
	res := RunTaskScript(context.Background(), "echo hello-stdout", "", 0)
	if res.Outcome != ScriptProduced {
		t.Fatalf("outcome = %v, want ScriptProduced (err=%v)", res.Outcome, res.Err)
	}
	if got := res.Stdout; got != "hello-stdout\n" {
		t.Fatalf("stdout = %q, want %q", got, "hello-stdout\n")
	}
	if res.Stderr != "" {
		t.Fatalf("stderr = %q, want empty", res.Stderr)
	}
}

func TestRunTaskScript_ProducedWithStderrOnly(t *testing.T) {
	skipOnWindows(t)
	// Output goes to stderr only: proves the two streams are captured
	// separately rather than merged.
	res := RunTaskScript(context.Background(), "echo hello-stderr 1>&2", "", 0)
	if res.Outcome != ScriptProduced {
		t.Fatalf("outcome = %v, want ScriptProduced (err=%v)", res.Outcome, res.Err)
	}
	if got := res.Stderr; got != "hello-stderr\n" {
		t.Fatalf("stderr = %q, want %q", got, "hello-stderr\n")
	}
	if res.Stdout != "" {
		t.Fatalf("stdout = %q, want empty", res.Stdout)
	}
}

func TestRunTaskScript_FailedOnNonZeroExit(t *testing.T) {
	res := RunTaskScript(context.Background(), "exit 3", "", 0)
	if res.Outcome != ScriptFailed {
		t.Fatalf("outcome = %v, want ScriptFailed (err=%v)", res.Outcome, res.Err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
	if res.Err == nil {
		t.Fatal("err = nil, want non-nil for a failed script")
	}
}

func TestRunTaskScript_TimedOut(t *testing.T) {
	skipOnWindows(t)
	start := time.Now()
	res := RunTaskScript(context.Background(), "sleep 5", "", 200*time.Millisecond)
	elapsed := time.Since(start)

	if res.Outcome != ScriptTimedOut {
		t.Fatalf("outcome = %v, want ScriptTimedOut (err=%v)", res.Outcome, res.Err)
	}
	if res.Err == nil {
		t.Fatal("err = nil, want non-nil for a timed-out script")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("call took %v, want it to return promptly after the 200ms timeout", elapsed)
	}
}

func TestRunTaskScript_Canceled(t *testing.T) {
	skipOnWindows(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	start := time.Now()
	res := RunTaskScript(ctx, "sleep 5", "", 30*time.Second)
	elapsed := time.Since(start)

	if res.Outcome != ScriptCanceled {
		t.Fatalf("outcome = %v, want ScriptCanceled (err=%v)", res.Outcome, res.Err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("call took %v, want it to return promptly after cancellation", elapsed)
	}
}

func TestRunTaskScript_GrandchildHoldingPipeDoesNotHang(t *testing.T) {
	skipOnWindows(t)
	// The shell itself exits immediately, but the backgrounded grandchild
	// inherits the stdout pipe and holds it open for 15s. Without WaitDelay,
	// cmd.Wait() would block for that whole time even though the shell is gone.
	// WaitDelay forcibly closes the inherited pipe after 5s, so the call must
	// return around that bound — well before the grandchild's own lifetime.
	start := time.Now()
	res := RunTaskScript(context.Background(), "sleep 15 & echo started", "", 500*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 10*time.Second {
		t.Fatalf("call took %v, want it bounded by WaitDelay (~5s) rather than the 15s grandchild sleep", elapsed)
	}
	// The grandchild was still holding the pipe when the deadline elapsed, so
	// the call is classified as timed out rather than produced.
	if res.Outcome != ScriptTimedOut {
		t.Fatalf("outcome = %v, want ScriptTimedOut (stdout=%q err=%v)", res.Outcome, res.Stdout, res.Err)
	}
}

func TestRunTaskScript_WorkDir(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	res := RunTaskScript(context.Background(), "pwd", dir, 0)
	if res.Outcome != ScriptProduced {
		t.Fatalf("outcome = %v, want ScriptProduced (err=%v)", res.Outcome, res.Err)
	}
	got := res.Stdout
	if len(got) > 0 && got[len(got)-1] == '\n' {
		got = got[:len(got)-1]
	}
	if got != dir {
		t.Fatalf("pwd = %q, want %q", got, dir)
	}
}

func TestRunTaskScript_OutputCappedAtCaptureTime(t *testing.T) {
	skipOnWindows(t)
	// The script emits ~2 MiB, far beyond the 64 KiB cap. The cap must be
	// applied while the process writes: buffering the whole output and
	// truncating after cmd.Run() would let a runaway producer grow the heap
	// without bound. If the writer ever blocks or the buffer is unbounded,
	// this test either blows memory or fails the bound assertion.
	start := time.Now()
	res := RunTaskScript(context.Background(), "head -c 2000000 /dev/zero | tr '\\0' 'a'", "", 0)
	elapsed := time.Since(start)

	if res.Outcome != ScriptProduced {
		t.Fatalf("outcome = %v, want ScriptProduced — a capped script still produced output (err=%v)", res.Outcome, res.Err)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("call took %v, want it to return promptly", elapsed)
	}
	if len(res.Stdout) > scriptOutputCap {
		t.Fatalf("len(stdout) = %d, want it bounded at the %d-byte cap", len(res.Stdout), scriptOutputCap)
	}
	if res.Stdout == "" {
		t.Fatal("stdout is empty, want the retained prefix of the script's output")
	}
}

func TestRunTaskScript_StderrCappedAtCaptureTime(t *testing.T) {
	skipOnWindows(t)
	// Same cap on the other stream, and it must not disturb the classification:
	// stderr-only output is still ScriptProduced.
	res := RunTaskScript(context.Background(), "head -c 2000000 /dev/zero | tr '\\0' 'b' 1>&2", "", 0)

	if res.Outcome != ScriptProduced {
		t.Fatalf("outcome = %v, want ScriptProduced (err=%v)", res.Outcome, res.Err)
	}
	if len(res.Stderr) > scriptOutputCap {
		t.Fatalf("len(stderr) = %d, want it bounded at the %d-byte cap", len(res.Stderr), scriptOutputCap)
	}
	if res.Stdout != "" {
		t.Fatalf("stdout = %q, want empty", res.Stdout)
	}
}

func TestCappedBuffer(t *testing.T) {
	var b cappedBuffer
	b.cap = 4

	// Under the cap: everything is retained and no truncation is flagged.
	if n, err := b.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("Write = (%d, %v), want (3, nil)", n, err)
	}
	if b.truncated {
		t.Fatal("truncated = true, want false while under the cap")
	}

	// Crossing the cap in one write: the excess is dropped, the full length is
	// still reported (a short count would look like a broken pipe).
	if n, err := b.Write([]byte("defgh")); n != 5 || err != nil {
		t.Fatalf("Write = (%d, %v), want (5, nil) — the full length must be reported", n, err)
	}
	if !b.truncated {
		t.Fatal("truncated = false, want true after exceeding the cap")
	}
	if got := b.String(); got != "abcd" {
		t.Fatalf("String() = %q, want %q", got, "abcd")
	}

	// A further write past a full buffer is discarded but still acknowledged.
	if n, err := b.Write([]byte("ij")); n != 2 || err != nil {
		t.Fatalf("Write = (%d, %v), want (2, nil)", n, err)
	}
	if got := b.String(); got != "abcd" {
		t.Fatalf("String() = %q, want the buffer unchanged at %q", got, "abcd")
	}
}
