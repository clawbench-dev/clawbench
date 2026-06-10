package ai

import (
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopilotBackend_Fields(t *testing.T) {
	b := copilotBackend()

	assert.Equal(t, "copilot", b.Name())
	assert.Equal(t, "copilot", b.defaultCommand)
	assert.NotNil(t, b.buildArgs)
	assert.NotNil(t, b.newParser)
	assert.NotNil(t, b.filterLine)
	assert.NotNil(t, b.preStart)

	// Verify buildArgs produces correct args
	args := b.buildArgs(ChatRequest{Prompt: "test"})
	assert.Contains(t, args, "--output-format")
	assert.Contains(t, args, "json")
	assert.Contains(t, args, "--allow-all")

	// Verify parser is a StreamParser
	parser := b.newParser()
	assert.IsType(t, &StreamParser{}, parser)

	// Verify filterLine skips non-JSON lines
	line, ok := b.filterLine("")
	assert.False(t, ok, "empty line should be filtered")
	assert.Empty(t, line)

	_, ok = b.filterLine("not json")
	assert.False(t, ok, "non-JSON line should be filtered")

	line, ok = b.filterLine(`{"type":"result"}`)
	assert.True(t, ok, "JSON line should pass filter")
	assert.Equal(t, `{"type":"result"}`, line)

	// Verify preStart sets stdin
	cmd := fakeCmd()
	req := ChatRequest{Prompt: "hello from test"}
	b.preStart(cmd, req)
	assert.NotNil(t, cmd.Stdin)
	stdinReader, ok := cmd.Stdin.(*strings.Reader)
	assert.True(t, ok, "Stdin should be a strings.Reader")
	content, err := io.ReadAll(stdinReader)
	assert.NoError(t, err)
	assert.Equal(t, "hello from test", string(content))
}

// fakeCmd creates a minimal exec.Cmd for testing preStart.
func fakeCmd() *exec.Cmd {
	return exec.Command("echo")
}
