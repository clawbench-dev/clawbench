package antigravity

import (
	"regexp"
	"strings"

	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.NewCLISource("antigravity", model.CLIOptions{
		Command:  "agy",
		Args:     []string{"models"},
		Parse:    parseAgyModels,
		Fallback: model.AntigravityCatalog,
	}))
}

// agyLogPrefixRE matches Go log-prefix lines, e.g. "I0428 10:00:00.000000 1234 msg".
// Mirrors the agy-acp bridge's status-line filter so parsing stays in sync.
var agyLogPrefixRE = regexp.MustCompile(`^[IWEF]\d{4}\s`)

// parseAgyModels parses `agy models`: each non-status line is a model ID or a
// display name. Diagnostics (progress, auth failures, log noise) are dropped.
func parseAgyModels(output string) []model.AgentModel {
	return model.ParsePlainLines(output, model.PlainLineOptions{
		Names: model.AntigravityModelNames,
		Skip:  isAgyStatusLine,
	})
}

// isAgyStatusLine reports whether a `agy models` line is diagnostic output
// rather than a model entry.
func isAgyStatusLine(line string) bool {
	switch {
	case line == "Fetching available models...":
		return true
	case line == "You are not logged into Antigravity":
		return true
	case strings.HasPrefix(line, "error "):
		return true
	case strings.Contains(line, "Failed to"):
		return true
	case agyLogPrefixRE.MatchString(line):
		return true
	}
	return false
}
