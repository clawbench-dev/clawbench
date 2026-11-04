package ai

import (
	"os"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// agentProcessEnv is the environment every spawned agent process starts from.
//
// The opt-in shell_env.inherit setting adds the variables the user exports from
// their shell startup files (.bashrc / .zshrc). A server started from a desktop
// launcher or a systemd unit never sourced those files, so without this an
// agent whose credentials live there comes up logged out (issue #619).
//
// Off by default: capturing that environment runs the user's startup files and
// hands the agent everything they export, which is a wider blast radius than
// most users want. Existing variables always win — see platform.MergeLoginShellEnv.
func agentProcessEnv() []string {
	env := os.Environ()
	if model.ConfigInstance.ShellEnv.Inherit {
		env = platform.MergeLoginShellEnv(env)
	}
	return env
}
