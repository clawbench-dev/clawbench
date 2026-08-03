package model

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// RunCommandContext runs name with args, capturing stdout and stderr separately
// to temporary files instead of pipes.
//
// exec.Cmd.Output() / CombinedOutput() can hang forever even when the command
// is killed by a Context deadline: if the spawned CLI leaves a grandchild
// holding the stdout/stderr pipe open, Output() waits for pipe EOF and the
// Context kill of the direct child never unblocks it. This blocks server
// startup (SyncDiscoverModels runs synchronously in main), so the service
// "stops but never comes back up".
//
// Capturing to files makes Cmd.Run() return as soon as the direct child exits,
// so the Context kill works reliably and the hang cannot occur.
func RunCommandContext(ctx context.Context, name string, args ...string) (stdout string, stderr string, err error) {
	stdoutFile, err := os.CreateTemp("", "clawbench-cmd-out-*")
	if err != nil {
		return "", "", err
	}
	defer func() { _ = os.Remove(stdoutFile.Name()) }()

	stderrFile, err := os.CreateTemp("", "clawbench-cmd-err-*")
	if err != nil {
		_ = stdoutFile.Close()
		return "", "", err
	}
	defer func() { _ = os.Remove(stderrFile.Name()) }()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	runErr := cmd.Run()

	// Flush to disk and rewind so the files read back cleanly regardless of
	// whether cmd.Run returned before or after all child writes landed.
	_ = stdoutFile.Sync()
	_ = stderrFile.Sync()
	_, _ = stdoutFile.Seek(0, io.SeekStart)
	_, _ = stderrFile.Seek(0, io.SeekStart)
	stdoutBytes, _ := io.ReadAll(stdoutFile)
	stderrBytes, _ := io.ReadAll(stderrFile)
	_ = stdoutFile.Close()
	_ = stderrFile.Close()

	return string(stdoutBytes), string(stderrBytes), runErr
}
