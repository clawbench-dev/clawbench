package mimo

import (
	"clawbench/internal/ai"
	"clawbench/internal/ai/backends"
	"clawbench/internal/ai/backends/opencode"
	"clawbench/internal/model"
)

func init() {
	ai.RegisterBackend("mimo", newMimoBackend)
	backends.Register(&backends.BackendPlugin{
		ID: "mimo",
		Spec: model.BackendSpec{
			ID: "mimo", Backend: "mimo", DefaultCmd: "mimo", Name: "MiMo-Code", Specialty: "小米 MiMo 编码助手",
			ThinkingEffortLevels: []string{"minimal", "high", "max"},
			AcpCommand:           "mimo acp",
			ACPLoadSession:       true,
			InstallCmd:           "npm install -g @mimo-ai/cli",
			SortOrder:            12,
		},
		ACP: &backends.ACPPlugin{
			InputRemaps: opencode.OpenCodeACPInputRemaps,
		},
	})
}

// newMimoBackend returns a CLIBackend instance for MiMo-Code CLI.
// MiMo-Code is a fork of OpenCode and reuses its stream parser, tool mappings,
// argument builder, and line filter.
func newMimoBackend() ai.AIBackend {
	return &ai.CLIBackend{
		BackendName: "mimo",
		Cmd:         "mimo",
		BuildArgsFn: opencode.BuildOpenCodeStreamArgs,
		NewParserFn: func() ai.LineParser {
			return &ai.OpenCodeStreamParser{
				ToolNameMap: opencode.OpenCodeToolNameMap,
				InputRemaps: opencode.OpenCodeInputRemaps,
			}
		},
		FilterLineFn: opencode.OpenCodeFilterLine,
		PreStartFn:   nil,
	}
}
