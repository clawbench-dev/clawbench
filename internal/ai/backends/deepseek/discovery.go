package deepseek

import (
	"strings"

	"clawbench/internal/model"
)

func init() {
	// Try the current CLI name first, then the legacy one. The model table may
	// be printed to either stream depending on version.
	model.RegisterModelSource(model.NewCLISource("deepseek", model.CLIOptions{
		Commands: []model.CommandSpec{
			{Command: "codewhale", Args: []string{"models"}, CombineStderr: true},
			{Command: "deepseek", Args: []string{"models"}, CombineStderr: true},
		},
		Parse: parseDeepSeekModels,
	}))
}

// deepseekModelLineRe matches "  deepseek-v4-flash (deepseek)" and
// "* deepseek-v4-pro (deepseek)". Group 1 is the default bullet.
var deepseekModelLineRe = `^(\*?)\s*(\S+)\s+\((\S+)\)`

// deepseekDefaultLineRe matches the header naming the default model:
// "Available models (default: deepseek-v4-pro)".
var deepseekDefaultLineRe = `Available models \(default:\s*(\S+)\)`

// parseDeepSeekModels parses `deepseek models` output:
//
//	Available models (default: deepseek-v4-pro)
//	  deepseek-v4-flash (deepseek)
//	* deepseek-v4-pro (deepseek)
//
// Only the native deepseek provider is kept. The provider prefix is included in
// the ID and name for disambiguation, consistent with Pi and OpenCode.
func parseDeepSeekModels(output string) []model.AgentModel {
	return model.ParseRegexCapture(output, model.RegexOptions{
		Pattern:            deepseekModelLineRe,
		IDGroup:            2,
		DefaultGroup:       1,
		DefaultValue:       "*",
		DefaultLinePattern: deepseekDefaultLineRe,
		DefaultLineGroup:   1,
		Filter: func(m []string) bool {
			return strings.EqualFold(m[3], "deepseek")
		},
		Transform: func(m []string) string {
			return m[3] + "/" + m[2]
		},
	})
}
