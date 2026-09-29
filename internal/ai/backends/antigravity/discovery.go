package antigravity

import (
	"regexp"
	"strings"
	"time"

	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.NewCLISource("antigravity", model.CLIOptions{
		Command: "agy",
		Args:    []string{"models"},
		Parse:   parseAgyModels,
		// agy 未登录时会联网重试 ~16s 才输出错误（实测 16.0/16.5/16.0s），
		// 而结果永远是 Fallback 里的 AntigravityCatalog。默认 10s 会在每次
		// 启动白等 10s，这里收紧到 5s：登录用户的一次真实拉取足够，
		// 未登录用户少等一半。
		Timeout:  5 * time.Second,
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
