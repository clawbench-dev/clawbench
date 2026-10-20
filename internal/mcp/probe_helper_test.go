package mcp

// A real child process is the point of these tests: the stdio path is the one
// whose framing, stderr capture and process reaping can actually break, and a
// fake transport would exercise none of it.
//
// The child is compiled from testdata/helper, NOT written as a shell script: a
// `#!/bin/sh` fixture cannot run on Windows ("%1 is not a valid Win32
// application"), which turned every stdio test red there.
//
// It must be a main package: `go build` on a library package produces an archive,
// which the OS then refuses to exec ("exec format error").

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"
)

// helperEnvVar carries the fixture mode into the child. Set on the child
// process only, so the parent's own test run is unaffected.
const helperEnvVar = "MCP_PROBE_TEST_HELPER"

// helperPath is the compiled fixture, shared by every test in the package.
//
// The directory comes from os.MkdirTemp, NOT t.TempDir: the path is reused
// across tests, and t.TempDir is removed when the test that created it finishes
// — a later test would then point at a deleted binary.
var helperPath string

// TestMain builds the fixture once, so a build failure is attributed to the
// package rather than to whichever test happened to run first.
func TestMain(m *testing.M) {
	if err := buildHelper(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// buildHelper compiles testdata/helper into a temp directory.
func buildHelper() error {
	dir, err := os.MkdirTemp("", "mcp-probe-helper-")
	if err != nil {
		return err
	}

	name := "mcp-probe-test-helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)

	cmd := exec.Command("go", "build", "-o", out, "./testdata/helper")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if buildOut, buildErr := cmd.CombinedOutput(); buildErr != nil {
		return fmt.Errorf("build fixture server: %w\n%s", buildErr, buildOut)
	}
	// `go build` does not always leave the output executable (restrictive umask
	// on some /tmp mounts). The probe resolves the command with exec.LookPath
	// and then execs it, so a non-executable file reads as "not found in PATH".
	if chmodErr := os.Chmod(out, 0o755); chmodErr != nil {
		return fmt.Errorf("chmod fixture server: %w", chmodErr)
	}
	helperPath = out
	return nil
}

// helperServer returns a server definition whose stdio command is the fixture
// and whose mode travels in the environment.
//
// The mode CANNOT be a command-line argument: the probe resolves the command
// with exec.LookPath on the whole string, so "path server" would be looked up as
// a single filename and never found. env is how a real MCP server is configured
// anyway (tokens, API keys), so this also exercises the env-passthrough path.
func helperServer(t *testing.T, mode string) model.MCPServer {
	t.Helper()
	if helperPath == "" {
		t.Fatal("fixture server was not built — TestMain must run first")
	}
	return model.MCPServer{
		Name:      "probe-test",
		Transport: model.MCPTransportStdio,
		Command:   helperPath,
		Env:       map[string]string{helperEnvVar: mode},
	}
}
