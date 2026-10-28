package codebuddy

import (
	"os/exec"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("codebuddy", newCodebuddyBackend)
	backends.Register(&backends.BackendPlugin{
		ID: "codebuddy",
		Spec: model.BackendSpec{
			ID: "codebuddy", Backend: "codebuddy", DefaultCmd: "codebuddy", Name: "Codebuddy", Specialty: "全栈开发助手",
			ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh"},
			AcpCommand:           "codebuddy --acp",
			// CodeBuddy genuinely supports session/load (verified by integration
			// test: the RPC succeeds and restores conversation context). Set true
			// so ServeACPLoadSession allows resuming CodeBuddy sessions.
			ACPLoadSession: true,
			InstallCmd:     "npm install -g @tencent-ai/codebuddy-code",
			SortOrder:      2,
			// This comment used to claim CodeBuddy's ACP process does NOT scan
			// ~/.codebuddy/skills. That is no longer true (measured on 2.177.0:
			// `codebuddy --acp` reports all 21 user skills with
			// _meta.source="user", plus <cwd>/.codebuddy/skills as "project" —
			// both keyed off the PROCESS cwd, which spawnLocked sets to the
			// project root).
			//
			// AutoLoadsNativeSkills stays false anyway, so ClawBench keeps
			// injecting them: that is the fallback for older builds that really
			// did not scan, and the injected entries still shadow same-named
			// skills from lower-priority sources. On current builds this means
			// both sides advertise the same skill; MergeCommands dedupes on the
			// canonical name (leading "/" ignored), so the slash menu shows it
			// once and the cost is a redundant scan, not a duplicate entry.
			//
			// Its plugin-cache skills are a separate mechanism (issue #383).
			NativeSkillsDirs: []string{".codebuddy/skills"},
		},
		// CodeBuddy can inject a message into the running turn via its private
		// session/steer method, instead of queueing it for the next turn.
		// It echoes the injected message back with a correlation id, so the
		// reply can be SPLIT exactly at the injection point.
		MidTurn:            &midTurnInjector{},
		MidTurnSplitsReply: true,
	})
}

// newCodebuddyBackend returns a CLIBackend instance configured for Codebuddy CLI.
func newCodebuddyBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "codebuddy",
		Cmd:         "codebuddy",
		BuildArgsFn: buildCodebuddyStreamArgs,
		NewParserFn: func() ai.LineParser { return &ai.StreamParser{} },
		FilterLineFn: func(line string) (string, bool) {
			line = strings.TrimPrefix(line, "\xEF\xBB\xBF") // UTF-8 BOM
			if line == "" {
				return "", false
			}
			return line, true
		},
		PreStartFn: func(cmd *exec.Cmd, req ai.ChatRequest) {
			// Codebuddy CLI in --print mode with stdout piped (non-TTY) requires
			// prompt via stdin — positional prompt argument is not recognized.
			cmd.Stdin = strings.NewReader(req.Prompt)
		},
	}
}

// buildCodebuddyStreamArgs constructs the CLI arguments for Codebuddy streaming.
func buildCodebuddyStreamArgs(req ai.ChatRequest) []string {
	return ai.BuildBaseStreamArgs(req, nil)
}
