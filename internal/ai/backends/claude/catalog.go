package claude

// ClaudeModelNames maps a model family to its display label.
//
// Display naming only: which models exist is never decided here. The Claude
// ACP adapter reports tier aliases (`sonnet` / `opus` / `haiku`) and
// model.ResolveModels resolves them against whatever the runtime reports, so a
// local list would only add a stale copy of the same answer.
var ClaudeModelNames = map[string]string{
	"sonnet": "Sonnet",
	"opus":   "Opus",
	"haiku":  "Haiku",
}

// ClaudeModelOrder is the preferred display order: sonnet (default), opus, haiku.
var ClaudeModelOrder = map[string]int{"sonnet": 0, "opus": 1, "haiku": 2}
