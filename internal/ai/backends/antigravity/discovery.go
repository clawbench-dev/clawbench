package antigravity

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"clawbench/internal/model"
)

// agyLogPrefixRE matches Go log-prefix lines, e.g. "I0428 10:00:00.000000 1234 msg".
// Matches agy's own log-prefix lines so they are dropped rather than parsed as
// model IDs.
var agyLogPrefixRE = regexp.MustCompile(`^[IWEF]\d{4}\s`)

// discoverAntigravityModels lists models by running `agy models`.
//
// Listing is a CLI feature, not a chat transport. It needs a signed-in CLI
// (`agy models` answers "Please sign in to view available models" otherwise),
// so an unauthenticated install still gets AntigravityCatalog — the same
// behaviour the picker had before the CLI transport was removed. Once signed
// in, the live list replaces the fallback.
func discoverAntigravityModels() ([]model.AgentModel, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "agy", "models").Output()
	if err != nil {
		return AntigravityCatalog, fmt.Sprintf("agy: `agy models` failed: %v", err)
	}
	if models := parseAgyModels(string(out)); len(models) > 0 {
		return models, ""
	}
	return AntigravityCatalog, "agy: no models parsed from `agy models` (sign in to see the live list)"
}

// parseAgyModels parses `agy models`: each non-status line is a model ID or a
// display name. Diagnostics (progress, auth failures, log noise) are dropped.
func parseAgyModels(output string) []model.AgentModel {
	return model.ParsePlainLines(output, model.PlainLineOptions{
		Names: AntigravityModelNames,
		Skip:  isAgyStatusLine,
	})
}

// isAgyStatusLine reports whether a `agy models` line is diagnostic output
// rather than a model entry.
func isAgyStatusLine(line string) bool {
	lower := strings.ToLower(line)
	switch {
	case line == "Fetching available models...":
		return true
	case line == "You are not logged into Antigravity":
		return true
	// agy 实际输出是 "Error: ..."（首字母大写、带冒号），旧代码只匹配
	// 小写 "error "，两者都对不上。按不区分大小写的前缀匹配，且同时接受
	// 有无冒号两种形态。
	case strings.HasPrefix(lower, "error"):
		return true
	case strings.Contains(line, "Failed to"):
		return true
	case agyLogPrefixRE.MatchString(line):
		return true
	}
	return false
}
