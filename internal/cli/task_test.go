package cli

import (
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestRunTaskCommand_NoArgs(t *testing.T) {
	// No args now prints help and returns 0
	exitCode := RunTaskCommand([]string{})
	assert.Equal(t, 0, exitCode)
}

func TestRunTaskCommand_HelpFlag(t *testing.T) {
	exitCode := RunTaskCommand([]string{"--help"})
	assert.Equal(t, 0, exitCode)
}

func TestRunTaskCommand_ShortHelpFlag(t *testing.T) {
	exitCode := RunTaskCommand([]string{"-h"})
	assert.Equal(t, 0, exitCode)
}

func TestRunTaskCommand_UnknownSubcommand(t *testing.T) {
	exitCode := RunTaskCommand([]string{"foo"})
	assert.Equal(t, 1, exitCode)
}

func TestCreateTask_MissingFields(t *testing.T) {
	exitCode := RunTaskCommand([]string{
		"create",
		"--name", "Test Task",
	})
	assert.Equal(t, 1, exitCode)
}

func TestCreateTask_ScheduledExecution(t *testing.T) {
	_ = os.Setenv("CLAWBENCH_SCHEDULED", "1")
	defer os.Unsetenv("CLAWBENCH_SCHEDULED")

	exitCode := RunTaskCommand([]string{
		"create",
		"--name", "Test Task",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "Test",
	})
	assert.Equal(t, 1, exitCode)
}

func TestCreateTask_LimitedRepeatWithoutMaxRuns(t *testing.T) {
	exitCode := RunTaskCommand([]string{
		"create",
		"--name", "Test Task",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "Test",
		"--repeat", "limited",
	})
	assert.Equal(t, 1, exitCode)
}

func TestCreateTask_ServerNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{
		Port: 59999,
	}

	exitCode := RunTaskCommand([]string{
		"create",
		"--name", "Test Task",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "Test",
	})
	assert.Equal(t, 1, exitCode)
}

func TestDeleteTask_NoTaskID(t *testing.T) {
	exitCode := RunTaskCommand([]string{"delete"})
	assert.Equal(t, 1, exitCode)
}

func TestPauseTask_NoTaskID(t *testing.T) {
	exitCode := RunTaskCommand([]string{"pause"})
	assert.Equal(t, 1, exitCode)
}

func TestResumeTask_NoTaskID(t *testing.T) {
	exitCode := RunTaskCommand([]string{"resume"})
	assert.Equal(t, 1, exitCode)
}

func TestTriggerTask_NoTaskID(t *testing.T) {
	exitCode := RunTaskCommand([]string{"trigger"})
	assert.Equal(t, 1, exitCode)
}

func TestUpdateTask_NoTaskID(t *testing.T) {
	exitCode := RunTaskCommand([]string{"update"})
	assert.Equal(t, 1, exitCode)
}

func TestUpdateTask_InvalidRepeat(t *testing.T) {
	exitCode := RunTaskCommand([]string{
		"update", "some-id",
		"--repeat", "invalid",
	})
	assert.Equal(t, 1, exitCode)
}

func TestCreateTask_InvalidRepeat(t *testing.T) {
	exitCode := RunTaskCommand([]string{
		"create",
		"--name", "Test",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "Test",
		"--repeat", "invalid",
	})
	assert.Equal(t, 1, exitCode)
}

func TestReorderFlagsFirst_AllFlagsFirst(t *testing.T) {
	// Flags already before positional — no change needed
	args := []string{"--project", "/path", "--prompt", "hello", "1"}
	result := reorderFlagsFirst(args)
	assert.Equal(t, []string{"--project", "/path", "--prompt", "hello", "1"}, result)
}

func TestReorderFlagsFirst_PositionalBetweenFlags(t *testing.T) {
	// The exact bug: task-ID before --prompt causes Go flag to skip --prompt
	args := []string{"1", "--prompt", "hello", "--project", "/path"}
	result := reorderFlagsFirst(args)
	assert.Equal(t, []string{"--prompt", "hello", "--project", "/path", "1"}, result)
}

func TestReorderFlagsFirst_MixedOrder(t *testing.T) {
	args := []string{"--project", "/path", "1", "--prompt", "hello"}
	result := reorderFlagsFirst(args)
	assert.Equal(t, []string{"--project", "/path", "--prompt", "hello", "1"}, result)
}

func TestReorderFlagsFirst_FlagWithEquals(t *testing.T) {
	args := []string{"1", "--project=/path", "--prompt=hello"}
	result := reorderFlagsFirst(args)
	assert.Equal(t, []string{"--project=/path", "--prompt=hello", "1"}, result)
}

func TestReorderFlagsFirst_NoFlags(t *testing.T) {
	args := []string{"1"}
	result := reorderFlagsFirst(args)
	assert.Equal(t, []string{"1"}, result)
}

func TestReorderFlagsFirst_NoPositional(t *testing.T) {
	args := []string{"--project", "/path"}
	result := reorderFlagsFirst(args)
	assert.Equal(t, []string{"--project", "/path"}, result)
}

func TestReadFlagOrFile_PlainValue(t *testing.T) {
	val, err := readFlagOrFile("hello world", "")
	assert.NoError(t, err)
	assert.Equal(t, "hello world", val)
}

func TestReadFlagOrFile_FileReference(t *testing.T) {
	// Create a temp file with content
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "prompt.txt")
	content := "This is a test prompt with $VARIABLE"
	err := os.WriteFile(promptFile, []byte(content), 0o644)
	assert.NoError(t, err)

	val, err := readFlagOrFile("@"+promptFile, tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, content, val)
}

func TestReadFlagOrFile_FileNotFound(t *testing.T) {
	_, err := readFlagOrFile("@/nonexistent/path/file.txt", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read file")
}

func TestReadFlagOrFile_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "empty.txt")
	err := os.WriteFile(promptFile, []byte(""), 0o644)
	assert.NoError(t, err)

	val, err := readFlagOrFile("@"+promptFile, tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, "", val)
}

func TestReadFlagOrFile_AtSignAlone(t *testing.T) {
	// "@" alone means read from a file named "" — should error
	_, err := readFlagOrFile("@", "")
	assert.Error(t, err)
}

func TestReadFlagOrFile_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	// Try to read a file outside the project — use a path that resolves
	// outside tmpDir regardless of OS. On Unix: /etc/hostname; on Windows
	// that doesn't exist, so we create a file in the temp dir's parent instead.
	outsideDir := filepath.Join(tmpDir, "..")
	outsideFile := filepath.Join(outsideDir, "outside-traversal-test.txt")
	_ = os.WriteFile(outsideFile, []byte("secret"), 0o644)
	t.Cleanup(func() { os.Remove(outsideFile) })

	_, err := readFlagOrFile("@"+outsideFile, tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestReadFlagOrFile_OutsideProject(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "project")
	_ = os.MkdirAll(subDir, 0o755)
	outsideFile := filepath.Join(tmpDir, "outside.txt")
	_ = os.WriteFile(outsideFile, []byte("secret"), 0o644)

	// Reading a file outside the project should be denied
	_, err := readFlagOrFile("@"+outsideFile, subDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestReadFlagOrFile_InsideProject(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "prompt.txt")
	content := "project prompt"
	err := os.WriteFile(promptFile, []byte(content), 0o644)
	assert.NoError(t, err)

	val, err := readFlagOrFile("@"+promptFile, tmpDir)
	assert.NoError(t, err)
	assert.Equal(t, content, val)
}

func TestReadFlagOrFile_NoProjectRestriction(t *testing.T) {
	// When projectPath is empty, @path can read any file (backward compat)
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "prompt.txt")
	content := "any content"
	err := os.WriteFile(promptFile, []byte(content), 0o644)
	assert.NoError(t, err)

	val, err := readFlagOrFile("@"+promptFile, "")
	assert.NoError(t, err)
	assert.Equal(t, content, val)
}
