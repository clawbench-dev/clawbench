package deepseek

import (
	"regexp"
	"strings"

	"clawbench/internal/model"
)

func init() {
	// Prefer the current CLI and scope the probe to the deepseek provider: a
	// current CLI without --provider prints only the *active* provider's models,
	// which is not necessarily deepseek. Older versions reject the flag with a
	// nonzero exit, so the unscoped invocations remain as fallbacks.
	model.RegisterModelSource(model.NewCLISource("deepseek", model.CLIOptions{
		Commands: []model.CommandSpec{
			{Command: "codewhale", Args: []string{"models", "--provider", "deepseek"}, CombineStderr: true},
			{Command: "codewhale", Args: []string{"models"}, CombineStderr: true},
			{Command: "deepseek", Args: []string{"models"}, CombineStderr: true},
		},
		Parse: parseDeepSeekModels,
	}))
}

// deepseekModelLineRe matches one model row in either output format:
//
//	  deepseek-v4-flash (deepseek)   legacy: every provider is listed, tagged
//	* deepseek-v4-pro (deepseek)
//	  deepseek-flash                 current: scoped to one provider, untagged
//	* deepseek-v4-pro
//
// Group 1 is the default bullet, group 2 the model ID, group 3 the optional
// provider tag (absent in the current format). The trailing anchor keeps
// banner and footer lines (e.g. "source=provider_models ...") from matching.
var deepseekModelLineRe = `^\s*(\*?)\s*([A-Za-z0-9][A-Za-z0-9._/:-]*)(?:\s+\(([A-Za-z0-9._-]+)\))?\s*$`

// deepseekDefaultLineRe matches the header naming the default model in either
// locale:
//
//	Available models (default: deepseek-v4-pro)
//	deepseek 的模型（默认：deepseek-v4-pro）
var deepseekDefaultLineRe = `(?:\(default:|（默认：)\s*([^\s\)）]+)`

// Header patterns naming the provider a listing is scoped to. The current CLI
// prints "<provider> 的模型（默认：…）" (zh-Hans) or "<provider> models
// (default: …)" (en-US); the legacy CLI printed the provider-neutral
// "Available models (default: …)" and tagged every row instead.
var (
	deepseekHeaderZhRe = regexp.MustCompile(`^\s*(\S+)\s+的模型`)
	deepseekHeaderEnRe = regexp.MustCompile(`^\s*(\S+)\s+models\s*\(default:`)
)

// scopedProvider reports which provider the output header names, or "" when the
// listing is unscoped (legacy "Available models", or no header at all).
//
// This matters because current-format rows carry no provider tag: without the
// header, an unscoped fallback that happened to run with another provider active
// would have its rows mislabeled as deepseek/*.
func scopedProvider(output string) string {
	for _, raw := range strings.Split(output, "\n") {
		if m := deepseekHeaderZhRe.FindStringSubmatch(raw); m != nil {
			return m[1]
		}
		if m := deepseekHeaderEnRe.FindStringSubmatch(raw); m != nil {
			// "Available" is the legacy sentinel for an unscoped listing, whose
			// rows are tagged per provider instead.
			if strings.EqualFold(m[1], "Available") {
				return ""
			}
			return m[1]
		}
	}
	return ""
}

// parseDeepSeekModels parses the `models` output of the CodeWhale / legacy
// deepseek-tui CLI:
//
//	Available models (default: deepseek-v4-pro)     # legacy: unscoped, tagged
//	  deepseek-v4-flash (deepseek)
//	* deepseek-v4-pro (deepseek)
//
//	deepseek 的模型（默认：deepseek-v4-pro）          # current: scoped, untagged
//	  deepseek-flash
//	* deepseek-v4-pro
//
// Only the native deepseek provider is kept. The provider prefix is included in
// the ID and name for disambiguation, consistent with Pi and OpenCode.
func parseDeepSeekModels(output string) []model.AgentModel {
	scoped := scopedProvider(output)

	return model.ParseRegexCapture(output, model.RegexOptions{
		Pattern:            deepseekModelLineRe,
		IDGroup:            2,
		DefaultGroup:       1,
		DefaultValue:       "*",
		DefaultLinePattern: deepseekDefaultLineRe,
		DefaultLineGroup:   1,
		Filter: func(m []string) bool {
			if m[3] != "" {
				// Legacy row: the tag is authoritative.
				return strings.EqualFold(m[3], "deepseek")
			}
			// Current-format row: trust it only when the header confirms the
			// listing belongs to deepseek (or names no provider at all).
			return scoped == "" || strings.EqualFold(scoped, "deepseek")
		},
		Transform: func(m []string) string {
			provider := m[3]
			if provider == "" {
				provider = "deepseek"
			}
			return provider + "/" + m[2]
		},
	})
}
