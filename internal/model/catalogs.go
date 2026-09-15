package model

// Built-in model catalogs.
//
// These are the fallback lists for backends whose CLI cannot enumerate its own
// models (no --list-models command, or a list that requires authentication the
// server may not have). They previously lived inside thirteen separate
// discovery.go files, interleaved with parsing logic, which made it impossible
// to see — let alone update — the full set.
//
// Keeping them here has two consequences worth stating plainly:
//
//   - Updating a catalog is a one-file change, reviewable on its own.
//   - The lists DO go stale. When a CLI gains a real list command, prefer
//     migrating that backend to NewCLISource and deleting its catalog entry
//     rather than editing the catalog.
//
// A catalog entry's Default flag is honored; when no entry is flagged, the
// first one becomes the default (see markFirstDefault).

// CopilotCatalog is the known model set for GitHub Copilot CLI, which exposes
// no model-list command.
var CopilotCatalog = []AgentModel{
	{ID: "gpt-4.1", Name: "GPT-4.1"},
	{ID: "gpt-4o", Name: "GPT-4o"},
	{ID: "o3", Name: "o3"},
	{ID: "o4-mini", Name: "o4-mini"},
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
}

// KimiCatalog is the known model set for Kimi CLI.
var KimiCatalog = []AgentModel{
	{ID: "kimi-k3", Name: "Kimi K3", Default: true},
	{ID: "kimi-k2-0711-chat", Name: "Kimi K2"},
	{ID: "kimi-for-coding", Name: "Kimi K2.7 Code"},
	{ID: "kimi-for-coding-highspeed", Name: "Kimi K2.7 Code Highspeed"},
	{ID: "moonshot-v1-128k", Name: "Moonshot v1 128K"},
	{ID: "moonshot-v1-32k", Name: "Moonshot v1 32K"},
	{ID: "moonshot-v1-8k", Name: "Moonshot v1 8K"},
	{ID: "kimi-latest", Name: "Kimi Latest"},
}

// MimoCatalog is the known model set for MiMo-Code CLI.
var MimoCatalog = []AgentModel{
	{ID: "mimo/mimo-auto", Name: "MiMo Auto", Default: true},
	{ID: "xiaomi/mimo-v2.5-pro-ultraspeed", Name: "MiMo V2.5 Pro Ultraspeed"},
	{ID: "xiaomi/mimo-v2.5-pro", Name: "MiMo V2.5 Pro"},
	{ID: "xiaomi/mimo-v2.5", Name: "MiMo V2.5"},
	{ID: "xiaomi/mimo-v2-pro", Name: "MiMo V2 Pro"},
	{ID: "xiaomi/mimo-v2-omni", Name: "MiMo V2 Omni"},
	{ID: "xiaomi/mimo-v2-flash", Name: "MiMo V2 Flash"},
}

// AntigravityCatalog is the fallback for `agy models` when the CLI is missing,
// unauthenticated, or returns nothing parseable.
var AntigravityCatalog = []AgentModel{
	{ID: "gemini-3-pro", Name: "Gemini 3 Pro"},
	{ID: "gemini-3-flash", Name: "Gemini 3 Flash"},
	{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro"},
	{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
}

// AntigravityModelNames maps discovered IDs to the same display names the
// catalog uses, so a discovered model and a fallback entry render identically.
var AntigravityModelNames = map[string]string{
	"gemini-3.1-pro":   "Gemini 3.1 Pro",
	"gemini-3.1-flash": "Gemini 3.1 Flash",
	"gemini-3.5-flash": "Gemini 3.5 Flash",
	"gemini-3-pro":     "Gemini 3 Pro",
	"gemini-3-flash":   "Gemini 3 Flash",
	"gemini-2.5-pro":   "Gemini 2.5 Pro",
	"gemini-2.5-flash": "Gemini 2.5 Flash",
}

// GrokCatalog is the fallback for `grok models`.
var GrokCatalog = []AgentModel{
	{ID: "grok-4.5", Name: "Grok 4.5"},
	{ID: "grok-build", Name: "Grok Build"},
}

// GrokModelNames maps discovered IDs to display names.
var GrokModelNames = map[string]string{
	"grok":        "Grok",
	"grok-code":   "Grok Code",
	"grok-4.5":    "Grok 4.5",
	"grok-3":      "Grok 3",
	"grok-3-mini": "Grok 3 Mini",
	"grok-build":  "Grok Build",
}

// ClaudeCatalog is the fallback for binary string scanning of the claude CLI,
// which has no --list-models command. The claude binary is commonly stripped,
// so this path is reached often.
var ClaudeCatalog = []AgentModel{
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
	{ID: "claude-haiku-3-5-20241022", Name: "Claude 3.5 Haiku"},
}

// ClaudeModelNames maps model family to display label.
var ClaudeModelNames = map[string]string{
	"sonnet": "Sonnet",
	"opus":   "Opus",
	"haiku":  "Haiku",
}

// ClaudeModelOrder is the preferred display order: sonnet (default), opus, haiku.
var ClaudeModelOrder = map[string]int{"sonnet": 0, "opus": 1, "haiku": 2}

// CodexCatalog is the last-resort fallback for Codex. Codex has no model-list
// command and ships a stripped Rust binary, so string extraction frequently
// fails; this list is updated manually from OpenAI's catalog.
var CodexCatalog = []AgentModel{
	{ID: "gpt-5.5", Name: "GPT-5.5", Default: true},
	{ID: "gpt-5.4", Name: "GPT-5.4"},
	{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini"},
}

// CodexModelOrder is the preferred display order for Codex models.
var CodexModelOrder = map[string]int{
	"gpt-5.5":      0,
	"gpt-5.4":      1,
	"gpt-5.4-mini": 2,
	"o3":           3,
	"o4-mini":      4,
}
