package ai

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"
)

// The switch must be off by default: with shell_env.inherit unset, the agent
// process environment is exactly the server's, with no shell executed.
func TestAgentProcessEnvDefaultsToServerEnvironment(t *testing.T) {
	orig := model.ConfigInstance.ShellEnv.Inherit
	t.Cleanup(func() { model.ConfigInstance.ShellEnv.Inherit = orig })
	model.ConfigInstance.ShellEnv.Inherit = false

	got := agentProcessEnv()
	want := os.Environ()
	if len(got) != len(want) {
		t.Fatalf("agentProcessEnv() returned %d entries, want %d (server environment)", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// With the switch on, variables exported by the user's shell startup files must
// reach the agent process — this is the whole point of issue #619.
func TestAgentProcessEnvInheritsShellStartupFilesWhenEnabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login shell startup files are POSIX-only")
	}

	shell := filepath.Join(t.TempDir(), "fakeshell")
	script := "#!/bin/sh\nexport CLAWBENCH_AI_ENV_MARK=1\nexec /bin/sh \"$@\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake shell: %v", err)
	}

	origShell, hadShell := os.LookupEnv("SHELL")
	t.Cleanup(func() {
		if hadShell {
			_ = os.Setenv("SHELL", origShell)
		} else {
			_ = os.Unsetenv("SHELL")
		}
	})
	_ = os.Setenv("SHELL", shell)
	t.Setenv("HOME", t.TempDir())

	orig := model.ConfigInstance.ShellEnv.Inherit
	t.Cleanup(func() { model.ConfigInstance.ShellEnv.Inherit = orig })
	model.ConfigInstance.ShellEnv.Inherit = true

	if got := agentProcessEnv(); !hasEnvEntry(got, "CLAWBENCH_AI_ENV_MARK=1") {
		t.Fatalf("shell export missing from agent env (%d entries)", len(got))
	}
}

func hasEnvEntry(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}
