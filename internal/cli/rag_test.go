package cli

import (
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

func TestRunRAGCommand_NoArgs(t *testing.T) {
	// No args now prints help and returns 0
	exitCode := RunRAGCommand([]string{})
	assert.Equal(t, 0, exitCode)
}

func TestRunRAGCommand_HelpFlag(t *testing.T) {
	exitCode := RunRAGCommand([]string{"--help"})
	assert.Equal(t, 0, exitCode)
}

func TestRunRAGCommand_ShortHelpFlag(t *testing.T) {
	exitCode := RunRAGCommand([]string{"-h"})
	assert.Equal(t, 0, exitCode)
}

func TestRunRAGCommand_UnknownSubcommand(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{Port: 30000}

	exitCode := RunRAGCommand([]string{"foo"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSearch_MissingQuery(t *testing.T) {
	exitCode := RunRAGCommand([]string{"search"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGMessage_MissingID(t *testing.T) {
	exitCode := RunRAGCommand([]string{"message"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGMessage_InvalidID(t *testing.T) {
	exitCode := RunRAGCommand([]string{"message", "--id", "abc"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSession_MissingID(t *testing.T) {
	exitCode := RunRAGCommand([]string{"session"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSearch_ServerNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{
		Port: 59999,
	}

	exitCode := RunRAGCommand([]string{"search", "-q", "test query"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGMessage_ServerNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{
		Port: 59999,
	}

	exitCode := RunRAGCommand([]string{"message", "--id", "42"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSession_ServerNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{
		Port: 59999,
	}

	exitCode := RunRAGCommand([]string{"session", "--id", "test-session-id"})
	assert.Equal(t, 1, exitCode)
}
