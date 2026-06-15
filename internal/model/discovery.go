//nolint:govet // shadowed err is acceptable in sequential blocks
package model

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"clawbench/internal/platform"

	"gopkg.in/yaml.v3"
)

// BackendSpec defines a known AI backend for auto-discovery.
type BackendSpec struct {
	ID                   string                    // agent id, e.g. "claude"
	Backend              string                    // backend type, e.g. "claude"
	DefaultCmd           string                    // command to detect on PATH, e.g. "claude"
	NoCLI                bool                      // if true, this backend has no CLI (e.g. mock); always considered "present"
	Name                 string                    // display name, e.g. "Claude"
	Icon                 string                    // emoji icon, e.g. "🤖"
	Specialty            string                    // short description, e.g. "代码编写与推理"
	ListModelsCmd        []string                  // optional: args to list models, e.g. ["models"]; empty = not supported
	ParseModels          func(string) []AgentModel // optional: parse command stdout into AgentModel list; nil = not supported
	DiscoverModelsFunc   func() []AgentModel       // optional: custom model discovery function (e.g. binary strings scan); takes priority over ListModelsCmd
	ThinkingEffortLevels []string                  // supported thinking effort levels, e.g. ["low","medium","high"]; nil = not supported
	AcpCommand           string                    // ACP spawn command for acp-stdio transport, e.g. "gemini --acp"; empty = no ACP support
}

// BackendRegistry lists all known AI backends for auto-discovery.
// When no agent DB records exist, each entry is checked: if DefaultCmd
// is found on PATH, a minimal agent record is inserted into the DB.
// For backends with ListModelsCmd+ParseModels, model lists are auto-discovered too.
var BackendRegistry = []BackendSpec{
	{
		ID: "claude", Backend: "claude", DefaultCmd: "claude", Name: "Claude", Icon: "🤖", Specialty: "代码编写与推理",
		DiscoverModelsFunc:   DiscoverClaudeModels,
		ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh", "max"},
		AcpCommand:           "npx -y @agentclientprotocol/claude-agent-acp@latest",
	},
	{
		ID: "codebuddy", Backend: "codebuddy", DefaultCmd: "codebuddy", Name: "Codebuddy", Icon: "🐛", Specialty: "全栈开发助手",
		DiscoverModelsFunc:   DiscoverCodebuddyModels,
		ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh"},
		AcpCommand:           "codebuddy --acp",
	},
	{
		ID: "opencode", Backend: "opencode", DefaultCmd: "opencode", Name: "OpenCode", Icon: "📟", Specialty: "终端编码工具",
		ListModelsCmd: []string{"models"}, ParseModels: ParseOpenCodeModels,
		ThinkingEffortLevels: []string{"minimal", "high", "max"},
		AcpCommand:           "opencode acp",
	},
	{
		ID: "gemini", Backend: "gemini", DefaultCmd: "gemini", Name: "Gemini", Icon: "💎", Specialty: "多模态推理",
		DiscoverModelsFunc: DiscoverGeminiModels,
		AcpCommand:         "gemini --acp",
	},
	{
		ID: "codex", Backend: "codex", DefaultCmd: "codex", Name: "Codex", Icon: "🐙", Specialty: "OpenAI 编码代理",
		DiscoverModelsFunc:   DiscoverCodexModels,
		ThinkingEffortLevels: []string{"low", "medium", "high"},
		AcpCommand:           "npx -y @agentclientprotocol/codex-acp@latest",
	},
	{
		ID: "qoder", Backend: "qoder", DefaultCmd: "qodercli", Name: "Qoder", Icon: "⚡", Specialty: "AI 编码助手",
		DiscoverModelsFunc: DiscoverQoderModels,
		AcpCommand:         "qodercli --acp",
	},
	{
		ID: "vecli", Backend: "vecli", DefaultCmd: "vecli", Name: "VeCLI", Icon: "🌿", Specialty: "字节跳动 AI 助手",
		DiscoverModelsFunc: DiscoverVeCLIModels,
	},
	{
		ID: "deepseek", Backend: "deepseek", DefaultCmd: "deepseek", Name: "DeepSeek", Icon: "🔍", Specialty: "DeepSeek 推理与编码",
		ListModelsCmd: []string{"models"}, ParseModels: ParseDeepSeekModels,
	},
	{
		ID: "pi", Backend: "pi", DefaultCmd: "pi", Name: "Pi", Icon: "🥧", Specialty: "极简编程智能体",
		DiscoverModelsFunc:   DiscoverPiModels,
		ThinkingEffortLevels: []string{"off", "minimal", "low", "medium", "high", "xhigh"},
	},
	{
		ID: "cline", Backend: "cline", DefaultCmd: "cline", Name: "Cline", Icon: "🔮", Specialty: "自主编码智能体",
		DiscoverModelsFunc:   DiscoverClineModels,
		ThinkingEffortLevels: []string{"none", "low", "medium", "high", "xhigh"},
		AcpCommand:           "cline --acp",
	},
	{
		ID: "kimi", Backend: "kimi", DefaultCmd: "kimi", Name: "Kimi", Icon: "🌙", Specialty: "Kimi AI 编码助手",
		DiscoverModelsFunc:   DiscoverKimiModels,
		ThinkingEffortLevels: []string{"off", "on"},
		AcpCommand:           "kimi acp",
	},
	{
		ID: "copilot", Backend: "copilot", DefaultCmd: "copilot", Name: "Copilot", Icon: "🤝", Specialty: "GitHub Copilot 编码助手",
		DiscoverModelsFunc:   DiscoverCopilotModels,
		ThinkingEffortLevels: []string{"none", "low", "medium", "high", "xhigh", "max"},
		AcpCommand:           "copilot --acp",
	},
	{
		ID: "mimo", Backend: "mimo", DefaultCmd: "mimo", Name: "MiMo-Code", Icon: "🚀", Specialty: "小米 MiMo 编码助手",
		DiscoverModelsFunc:   DiscoverMimoModels,
		ThinkingEffortLevels: []string{"minimal", "high", "max"},
		AcpCommand:           "mimo acp",
	},
}

// CheckCLIExists checks whether a CLI command is available on the system.
// It first tries `cmd --version` with a 5-second timeout.
// If that fails, it falls back to exec.LookPath — some CLIs (especially Node.js ones)
// may return non-zero exit codes for --version when run without a TTY or in certain
// environments, but the binary itself is still present and functional.
// For the "pi" command, also checks the embedded binary at .clawbench/pi/pi.
func CheckCLIExists(cmd string) bool {
	if cmd == "" {
		return false
	}

	// Special case: embedded Pi binary
	if cmd == "pi" && EmbeddedAgentPath() != "" {
		return true
	}

	// Primary check: run `cmd --version`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, cmd, "--version").Run()
	if err == nil {
		return true
	}

	// Fallback: check if the binary exists on PATH
	// This handles cases where --version fails (non-zero exit, timeout, etc.)
	// but the CLI is actually installed and usable for its primary function.
	if _, lookupErr := exec.LookPath(cmd); lookupErr == nil {
		slog.Warn("CLI --version failed but binary found on PATH, keeping agent",
			"cmd", cmd, "version_error", err)
		return true
	}

	slog.Warn("CLI not found on PATH",
		"cmd", cmd, "version_error", err)
	return false
}

// CheckCLIExistsErr returns an error describing why the CLI is not available,
// or nil if the CLI is available. This is used for more specific error reporting.
// For the "pi" command, also checks the embedded binary at .clawbench/pi/pi.
func CheckCLIExistsErr(cmd string) error {
	if cmd == "" {
		return fmt.Errorf("empty command")
	}

	// Special case: embedded Pi binary
	if cmd == "pi" && EmbeddedAgentPath() != "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, cmd, "--version").Run()
	if err == nil {
		return nil
	}

	_, lookupErr := exec.LookPath(cmd)
	if lookupErr == nil {
		// Binary exists but --version failed — CLI is still available
		return nil
	}

	return fmt.Errorf("CLI %q not found on PATH: %w", cmd, lookupErr)
}

// DiscoverModels runs the CLI's model-list command and returns parsed models.
// Returns nil if the CLI doesn't support model listing or if the command fails.
// Errors are logged but not propagated — model discovery is best-effort.
// This is a variable so it can be overridden in tests.
var DiscoverModels = discoverModels

func discoverModels(spec BackendSpec) []AgentModel {
	// Custom discovery function takes priority (e.g. binary strings scan for claude)
	if spec.DiscoverModelsFunc != nil {
		models := spec.DiscoverModelsFunc()
		if len(models) > 0 {
			slog.Info("model discovery succeeded", "backend", spec.ID, "models", len(models))
		}
		return models
	}

	if len(spec.ListModelsCmd) == 0 || spec.ParseModels == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, spec.DefaultCmd, spec.ListModelsCmd...)
	out, err := cmd.Output()
	if err != nil {
		slog.Debug("model discovery command failed", "cmd", spec.DefaultCmd, "args", spec.ListModelsCmd, "error", err)
		return nil
	}

	models := spec.ParseModels(string(out))
	if len(models) == 0 {
		slog.Debug("model discovery returned no models", "cmd", spec.DefaultCmd)
		return nil
	}

	slog.Info("model discovery succeeded", "backend", spec.ID, "models", len(models))
	return models
}

// FindSpecByBackend returns the BackendSpec for the given backend type, or nil.
func FindSpecByBackend(backend string) *BackendSpec {
	for i := range BackendRegistry {
		if BackendRegistry[i].Backend == backend {
			return &BackendRegistry[i]
		}
	}
	return nil
}

// CanDiscoverModels returns true if the spec supports model discovery
// via either DiscoverModelsFunc or ListModelsCmd+ParseModels.
func CanDiscoverModels(spec BackendSpec) bool {
	return spec.DiscoverModelsFunc != nil || (len(spec.ListModelsCmd) > 0 && spec.ParseModels != nil)
}

// SyncDiscoverModels runs DiscoverModels for all backends that support it
// and returns the results as a map keyed by backend type.
// This is called synchronously on first startup (when no DB models exist yet).
func SyncDiscoverModels() map[string][]AgentModel {
	result := make(map[string][]AgentModel)
	for _, spec := range BackendRegistry {
		if !CanDiscoverModels(spec) {
			continue
		}
		models := DiscoverModels(spec)
		if len(models) == 0 {
			continue
		}
		result[spec.Backend] = models
		slog.Info("discovered models", "backend", spec.Backend, "count", len(models))
	}
	return result
}

// AsyncRefreshModelCache runs DiscoverModels in a goroutine for all backends
// and updates in-memory Agent data + DB. Call this after startup — it does not block.
func AsyncRefreshModelCache(db *sql.DB) {
	go func() {
		for _, spec := range BackendRegistry {
			if !CanDiscoverModels(spec) {
				continue
			}
			models := DiscoverModels(spec)
			if len(models) == 0 {
				continue
			}
			slog.Info("refreshed discovered models", "backend", spec.Backend, "count", len(models))

			// Update in-memory and DB for agents whose models were auto-detected (not user-defined)
			modelsJSON, _ := json.Marshal(models)
			for _, agent := range AgentList {
				if agent.Backend == spec.Backend && agent.ModelsAutoDetected {
					agent.Models = models
					if db != nil {
						if _, err := db.Exec("UPDATE agents SET models = ? WHERE id = ?",
							string(modelsJSON), agent.ID); err != nil {
							slog.Warn("failed to persist refreshed models to DB", "id", agent.ID, "error", err)
						}
					}
				}
			}
		}
	}()
}

// --- Model list parsers ---

// codebuddyProductFile is the JSON file in the codebuddy installation that contains
// the authoritative model list with names, capabilities, and default status.
const codebuddyProductFile = "product.cloudhosted.json"

// codebuddyProductModel represents a model entry in codebuddy's product JSON.
type codebuddyProductModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// codebuddyProduct represents the top-level structure of codebuddy's product JSON.
type codebuddyProduct struct {
	Models []codebuddyProductModel `json:"models"`
}

// DiscoverCodebuddyModels discovers Codebuddy models by reading the product.cloudhosted.json
// file from the CLI installation directory. This JSON file contains the authoritative model
// list with proper names and default status, making it far more reliable than --help output
// (which launches a TUI that hangs without a TTY) or JS bundle scanning (which is fragile).
func DiscoverCodebuddyModels() []AgentModel {
	// Resolve the real path for the codebuddy CLI, handling Windows .cmd wrappers
	// Path is typically: .../node_modules/@tencent-ai/codebuddy-code/bin/codebuddy
	realPath := platform.ResolveCLIPath("codebuddy")
	if realPath == "" {
		return nil
	}

	// The product JSON is at .../codebuddy-code/product.cloudhosted.json
	// From .../bin/codebuddy: Dir → .../bin, Dir again → .../codebuddy-code
	pkgDir := filepath.Dir(filepath.Dir(realPath))
	productPath := filepath.Join(pkgDir, codebuddyProductFile)

	data, err := os.ReadFile(productPath)
	if err != nil {
		slog.Debug("codebuddy model discovery: product JSON not found", "path", productPath, "error", err)
		return nil
	}

	var product codebuddyProduct
	if err := json.Unmarshal(data, &product); err != nil {
		slog.Debug("codebuddy model discovery: failed to parse product JSON", "error", err)
		return nil
	}

	if len(product.Models) == 0 {
		slog.Debug("codebuddy model discovery: no models in product JSON")
		return nil
	}

	var models []AgentModel
	for _, m := range product.Models {
		// Skip pseudo-models like "default" and "auto" — these are selectors, not real model IDs
		if m.ID == "default" || m.ID == "auto" {
			continue
		}
		// Skip non-LLM models (e.g. text-to-image)
		if m.ID == "hunyuan-image-v3.0" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, AgentModel{
			ID:      m.ID,
			Name:    name,
			Default: m.IsDefault || (len(models) == 0 && m.ID != "default" && m.ID != "auto"),
		})
	}

	if len(models) == 0 {
		return nil
	}

	// First non-skipped model gets Default=true if none was marked isDefault
	if !models[0].Default {
		models[0].Default = true
	}

	slog.Info("codebuddy model discovery succeeded", "models", len(models))
	return models
}

// codebuddyModelRe extracts model IDs from codebuddy --help output (legacy, kept for ParseCodebuddyModels).
// Format: "Currently supported: (glm-4.7, glm-4.6, ...)"
var codebuddyModelRe = regexp.MustCompile(`Currently supported: \(([^)]+)\)`)

// ParseCodebuddyModels parses codebuddy --help output to extract model IDs.
// Output format: "... --model <model>  Model for the current session. ... Currently supported: (glm-4.7, glm-4.6, ...)"
//
// Deprecated: codebuddy --help launches a TUI that hangs without a TTY; use DiscoverCodebuddyModels instead.
func ParseCodebuddyModels(output string) []AgentModel {
	matches := codebuddyModelRe.FindStringSubmatch(output)
	if len(matches) < 2 {
		return nil
	}

	parts := strings.Split(matches[1], ",")
	var models []AgentModel
	for i, p := range parts {
		id := strings.TrimSpace(p)
		if id == "" {
			continue
		}
		models = append(models, AgentModel{
			ID:      id,
			Name:    id,
			Default: i == 0,
		})
	}
	return models
}

// claudeModelRe matches Claude model IDs like "claude-sonnet-4-6" or "claude-opus-4-5" from strings output.
// Requires exactly two version segments (major-minor), excludes:
// - date-stamped like "claude-opus-4-20250514" (8-digit date suffix)
// - short aliases like "claude-sonnet-4" (points to latest snapshot)
var claudeModelRe = regexp.MustCompile(`^claude-(sonnet|opus|haiku)-\d+-\d+$`)

// claudeModelOrder defines the preferred display order: sonnet first (default), then opus, then haiku.
var claudeModelOrder = map[string]int{"sonnet": 0, "opus": 1, "haiku": 2}

// claudeModelNames maps model ID prefixes to human-readable names.
var claudeModelNames = map[string]string{
	"sonnet": "Sonnet",
	"opus":   "Opus",
	"haiku":  "Haiku",
}

// claudeConfigDir returns the Claude config directory (~/.claude/).
// Overridable for testing (same pattern as DiscoverModels variable).
var claudeConfigDir = platform.ClaudeConfigDir

// LoadClaudeModelOverrides reads ~/.claude/settings.json and returns the
// modelOverrides map if present. Returns nil on any error (missing file,
// invalid JSON, no overrides key) — graceful degradation.
func LoadClaudeModelOverrides() map[string]string {
	path := filepath.Join(claudeConfigDir(), "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Debug("claude model overrides: settings.json not found", "path", path, "error", err)
		return nil
	}
	var cfg struct {
		ModelOverrides map[string]string `json:"modelOverrides"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		slog.Debug("claude model overrides: invalid JSON", "path", path, "error", err)
		return nil
	}
	if len(cfg.ModelOverrides) == 0 {
		return nil
	}
	return cfg.ModelOverrides
}

// claudeIsDateStamped returns true if the model ID contains an 8-digit date segment
// like "claude-opus-4-20250514", which are snapshot aliases we want to skip.
func claudeIsDateStamped(modelID string) bool {
	for _, seg := range strings.Split(modelID, "-") {
		if len(seg) == 8 {
			return true
		}
	}
	return false
}

// DiscoverClaudeModels discovers Claude model IDs by scanning the claude binary
// with `strings`. Claude CLI does not have a --list-models command, so we extract
// model IDs from the binary which contains hardcoded model name patterns.
func DiscoverClaudeModels() []AgentModel { //nolint:gocyclo,gocognit // binary scanning model discovery
	// Resolve the real path for the claude binary, handling Windows .cmd wrappers
	path := platform.ResolveCLIPath("claude")
	if path == "" {
		// Claude binary not found — fall back to known defaults
		models := make([]AgentModel, len(claudeDefaultModels))
		copy(models, claudeDefaultModels)
		if len(models) > 0 {
			models[0].Default = true
		}
		slog.Info("claude model discovery: binary not found, using defaults", "models", len(models))
		return models
	}

	// Extract printable strings from the binary (cross-platform replacement for
	// the POSIX "strings" command, which does not exist on Windows)
	lines, err := platform.ExtractStrings(path, 4)
	if err != nil {
		slog.Debug("claude model discovery: extract strings failed", "error", err)
		return nil
	}

	// Extract unique model IDs matching the pattern
	seen := make(map[string]bool)
	var models []AgentModel
	for _, line := range lines {
		if !claudeModelRe.MatchString(line) || seen[line] {
			continue
		}
		// Skip date-stamped versions like claude-opus-4-20250514
		if claudeIsDateStamped(line) {
			continue
		}
		seen[line] = true

		// Generate human-readable name: claude-sonnet-4-6 → "Claude Sonnet 4.6"
		parts := strings.SplitN(line, "-", 3) // ["claude", "sonnet", "4-6"]
		name := line
		if len(parts) == 3 {
			if family, ok := claudeModelNames[parts[1]]; ok {
				version := strings.ReplaceAll(parts[2], "-", ".")
				name = "Claude " + family + " " + version
			}
		}

		models = append(models, AgentModel{
			ID:   line,
			Name: name,
		})
	}

	// Sort: sonnet first, then opus, then haiku; within each family, newest first
	sort.Slice(models, func(i, j int) bool {
		familyI := strings.SplitN(models[i].ID, "-", 3)
		familyJ := strings.SplitN(models[j].ID, "-", 3)
		if len(familyI) >= 2 && len(familyJ) >= 2 {
			orderI, okI := claudeModelOrder[familyI[1]]
			orderJ, okJ := claudeModelOrder[familyJ[1]]
			if okI && okJ && orderI != orderJ {
				return orderI < orderJ
			}
		}
		// Same family: sort by ID descending (newest first)
		return models[i].ID > models[j].ID
	})

	// Mark first model as default
	if len(models) > 0 {
		models[0].Default = true
	}

	// Apply model name overrides from ~/.claude/settings.json
	// When modelOverrides maps a Claude model ID to another name (e.g. "MiniMax-M2.7"),
	// we replace the display name so the user sees which underlying model is actually used.
	// The model ID is NOT changed — CLI invocation always uses the original Claude model ID.
	if overrides := LoadClaudeModelOverrides(); len(overrides) > 0 {
		for i := range models {
			if name, ok := overrides[models[i].ID]; ok {
				slog.Debug("claude model override applied", "id", models[i].ID, "name", name)
				models[i].Name = name
			}
		}
	}

	// If binary scanning found no models, fall back to known defaults
	if len(models) == 0 {
		models = make([]AgentModel, len(claudeDefaultModels))
		copy(models, claudeDefaultModels)
		if len(models) > 0 {
			models[0].Default = true
		}
		slog.Info("claude model discovery: binary scan found nothing, using defaults", "models", len(models))
		return models
	}

	return models
}

// claudeDefaultModels lists known Claude models as a fallback when binary
// scanning fails (e.g. claude CLI not found or ExtractStrings returns nothing).
var claudeDefaultModels = []AgentModel{
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
	{ID: "claude-haiku-3-5-20241022", Name: "Claude 3.5 Haiku"},
}
var deepseekModelLineRe = regexp.MustCompile(`^(\*?)\s*(\S+)\s+\((\S+)\)`)

// deepseekDefaultRe extracts the default model from the header line.
var deepseekDefaultRe = regexp.MustCompile(`Available models \(default:\s*(\S+)\)`)

// ParseDeepSeekModels parses deepseek models output.
// Output format:
//
//	Available models (default: deepseek-v4-pro)
//	  deepseek-v4-flash (deepseek)
//	* deepseek-v4-pro (deepseek)
//
// The Name field includes the provider prefix for disambiguation (e.g., "deepseek/deepseek-v4-pro"),
// consistent with Pi and OpenCode model naming.
func ParseDeepSeekModels(output string) []AgentModel {
	// Extract default model name from header
	var defaultModel string
	if m := deepseekDefaultRe.FindStringSubmatch(output); len(m) >= 2 {
		defaultModel = m[1]
	}

	var models []AgentModel
	for _, line := range strings.Split(output, "\n") {
		m := deepseekModelLineRe.FindStringSubmatch(line)
		if len(m) < 4 {
			continue
		}
		isDefault := m[1] == "*" || m[2] == defaultModel || (defaultModel == "" && len(models) == 0)
		modelID := m[2]
		provider := m[3]

		// Only include the native deepseek provider
		if !strings.EqualFold(provider, "deepseek") {
			continue
		}

		fullID := provider + "/" + modelID
		models = append(models, AgentModel{
			ID:      fullID,
			Name:    fullID,
			Default: isDefault,
		})
	}
	return models
}

// opencodeModelLineRe matches lines like "minimax/MiniMax-M2.5" or "opencode/minimax-m2.5-free"
var opencodeModelLineRe = regexp.MustCompile(`^(\S+)/(\S+)$`)

// ParseOpenCodeModels parses opencode models output.
// Output format: one "provider/model" per line, e.g.:
//
//	opencode/minimax-m2.5-free
//	minimax/MiniMax-M2.5
//	anthropic/claude-sonnet-4-6
//
// The Name field includes the provider prefix for disambiguation,
// since different providers may offer models with identical names.
// The first model is marked as default.
func ParseOpenCodeModels(output string) []AgentModel {
	var models []AgentModel
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := opencodeModelLineRe.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}

		models = append(models, AgentModel{
			ID:      line,              // full "provider/model" as ID (opencode uses this format)
			Name:    m[1] + "/" + m[2], // include provider in display name for disambiguation
			Default: len(models) == 0,
		})
	}
	return models
}

// piModelLineRe matches lines from `pi --list-models` tabular output.
// Format: "provider        model                       context  max-out  thinking  images"
// We match any line with at least 2 whitespace-separated fields where the first
// doesn't look like a header.
var piModelLineRe = regexp.MustCompile(`^(\S+)\s+(\S+)`)

// ParsePiModels parses the output of `pi --list-models` into a list of AgentModel.
// Output format:
//
//	provider        model                       context  max-out  thinking  images
//	anthropic       claude-sonnet-4-6           1M       64K      yes       yes
//	openai          gpt-4o                      128K     4.1K     no        yes
//
// Models are prefixed with provider for disambiguation (e.g., "anthropic/claude-sonnet-4-6").
func ParsePiModels(output string) []AgentModel {
	var models []AgentModel
	for _, line := range strings.Split(output, "\n") {
		m := piModelLineRe.FindStringSubmatch(line)
		if len(m) < 3 {
			continue
		}
		provider := m[1]
		modelID := m[2]
		// Skip header line
		if provider == "provider" || modelID == "model" {
			continue
		}
		fullID := provider + "/" + modelID
		models = append(models, AgentModel{
			ID:      fullID,
			Name:    fullID,
			Default: len(models) == 0,
		})
	}
	return models
}

// DiscoverPiModels discovers Pi model IDs by running `pi --list-models` and parsing the output.
// Pi outputs the model table to stderr (not stdout), so we must capture both streams.
// It first tries the embedded Pi binary at .clawbench/pi/pi, then falls back to PATH.
func DiscoverPiModels() []AgentModel {
	piPath := EmbeddedAgentPath()
	if piPath == "" {
		piPath = "pi" // fallback to PATH
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, piPath, "--list-models")
	// Pi outputs the model table to stderr; use CombinedOutput to capture both.
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Debug("pi model discovery: command failed", "path", piPath, "error", err)
		return nil
	}

	models := ParsePiModels(string(out))
	if len(models) == 0 {
		slog.Debug("pi model discovery: no models parsed")
		return nil
	}

	slog.Info("pi model discovery succeeded", "models", len(models))
	return models
}

// --- Gemini model discovery ---

// geminiModelDefRe matches model definition keys in the Gemini CLI JS bundle.
// Format: "gemini-X.Y-ZZZ": { ... isVisible: true ... }
var geminiModelDefRe = regexp.MustCompile(`"(gemini-\d+(?:\.\d+)?(?:-[\w-]+))":\s*\{`)

// geminiIsVisibleRe checks whether isVisible: true appears within a model definition block.
var geminiIsVisibleRe = regexp.MustCompile(`isVisible:\s*true`)

// geminiModelOrder defines the display order for Gemini models: pro first, then flash, then flash-lite.
var geminiModelOrder = map[string]int{"pro": 0, "flash": 1, "flash-lite": 2}

// geminiModelFamilyOrder defines the order for model families: gemini-3.x first, then gemini-2.5.x.
var geminiModelFamilyOrder = map[string]int{"gemini-3": 0, "gemini-2.5": 1}

// geminiTierRe extracts the tier value from a model definition block.
var geminiTierRe = regexp.MustCompile(`tier:\s*"([^"]+)"`)

// geminiFamilyRe extracts the family value from a model definition block.
var geminiFamilyRe = regexp.MustCompile(`family:\s*"([^"]+)"`)

// hasChunkJS checks whether a directory listing contains any chunk-*.js files,
// which is how Gemini CLI organizes its JS bundle.
func hasChunkJS(entries []os.DirEntry) bool {
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "chunk-") && strings.HasSuffix(e.Name(), ".js") {
			return true
		}
	}
	return false
}

// DiscoverGeminiModels discovers Gemini model IDs by scanning the JS bundle files
// in the Gemini CLI npm package directory. The model definitions are embedded in
// chunk-*.js files with isVisible: true/false markers.
func DiscoverGeminiModels() []AgentModel { //nolint:gocognit,gocyclo // API-based model discovery with pagination
	// Resolve the real path for the gemini CLI, handling Windows .cmd wrappers
	realPath := platform.ResolveCLIPath("gemini")
	if realPath == "" {
		return nil
	}

	// Navigate to the bundle directory: .../node_modules/@google/gemini-cli/bundle/
	// The JS entry point is typically at .../bundle/gemini, so Dir() gives .../bundle/
	bundleDir := filepath.Dir(realPath)

	// On Windows, the resolved path from .cmd points directly to the JS entry file
	// inside the bundle/ directory, so Dir() correctly gives the bundle directory.
	// On Unix, the symlink also resolves to the bundle/ directory.
	// Verify by checking for chunk-*.js files.
	if filepath.Base(bundleDir) != "bundle" {
		// Try bundle/ subdirectory — the entry file might be at package root
		// and bundle/ is a subdirectory (e.g. if path resolves differently)
		altBundleDir := filepath.Join(bundleDir, "bundle")
		if entries, err := os.ReadDir(altBundleDir); err == nil && hasChunkJS(entries) {
			bundleDir = altBundleDir
		} else {
			slog.Debug("gemini model discovery: unexpected path layout", "path", realPath, "dir", bundleDir)
			return nil
		}
	}

	entries, err := os.ReadDir(bundleDir)
	if err != nil {
		slog.Debug("gemini model discovery: cannot read bundle directory", "dir", bundleDir, "error", err)
		return nil
	}

	type modelEntry struct {
		id     string
		tier   string
		family string
	}

	seen := make(map[string]bool)
	var found []modelEntry

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "chunk-") || !strings.HasSuffix(entry.Name(), ".js") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(bundleDir, entry.Name()))
		if err != nil {
			continue
		}

		content := string(data)
		matches := geminiModelDefRe.FindAllStringSubmatchIndex(content, -1)
		for _, match := range matches {
			if len(match) < 4 {
				continue
			}
			modelID := content[match[2]:match[3]]

			// Skip aliases (auto-gemini-*, single-word aliases)
			if strings.HasPrefix(modelID, "auto-gemini-") {
				continue
			}
			// Skip customtools and base variants
			if strings.HasSuffix(modelID, "-customtools") || strings.HasSuffix(modelID, "-base") {
				continue
			}
			if seen[modelID] {
				continue
			}

			// Check for isVisible: true within ~500 chars after the opening brace
			braceStart := match[1]
			lookEnd := braceStart + 500
			if lookEnd > len(content) {
				lookEnd = len(content)
			}
			block := content[braceStart:lookEnd]

			if !geminiIsVisibleRe.MatchString(block) {
				continue
			}

			seen[modelID] = true

			tier := ""
			family := ""
			if m := geminiTierRe.FindStringSubmatch(block); len(m) >= 2 {
				tier = m[1]
			}
			if m := geminiFamilyRe.FindStringSubmatch(block); len(m) >= 2 {
				family = m[1]
			}

			found = append(found, modelEntry{id: modelID, tier: tier, family: family})
		}
	}

	if len(found) == 0 {
		return nil
	}

	sort.Slice(found, func(i, j int) bool {
		fi, fj := found[i].family, found[j].family
		oi, oj := geminiModelFamilyOrder[fi], geminiModelFamilyOrder[fj]
		if oi != oj {
			return oi < oj
		}
		ti, tj := found[i].tier, found[j].tier
		tiOrder, tiOk := geminiModelOrder[ti]
		tjOrder, tjOk := geminiModelOrder[tj]
		if tiOk && tjOk && tiOrder != tjOrder {
			return tiOrder < tjOrder
		}
		return found[i].id > found[j].id
	})

	var models []AgentModel
	for i, e := range found {
		models = append(models, AgentModel{
			ID:      e.id,
			Name:    e.id,
			Default: i == 0,
		})
	}

	slog.Info("gemini model discovery succeeded", "models", len(models))
	return models
}

// --- Codex model discovery ---

// codexModelRe matches OpenAI model IDs in the Codex binary strings output.
var codexModelRe = regexp.MustCompile(`^(gpt-\d+\.\d+(-mini)?|o[34](-mini)?)$`)

// codexModelOrder defines the preferred display order for Codex models.
var codexModelOrder = map[string]int{
	"gpt-5.5":      0,
	"gpt-5.4":      1,
	"gpt-5.4-mini": 2,
	"o3":           3,
	"o4-mini":      4,
}

// codexTargetTriple returns the Rust target triple for the current platform.
func codexTargetTriple() string {
	arch := runtime.GOARCH
	switch runtime.GOOS {
	case "linux", "android":
		switch arch {
		case "amd64":
			return "x86_64-unknown-linux-musl"
		case "arm64":
			return "aarch64-unknown-linux-musl"
		}
	case "darwin":
		switch arch {
		case "amd64":
			return "x86_64-apple-darwin"
		case "arm64":
			return "aarch64-apple-darwin"
		}
	case "windows":
		switch arch {
		case "amd64":
			return "x86_64-pc-windows-msvc"
		case "arm64":
			return "aarch64-pc-windows-msvc"
		}
	}
	return ""
}

// DiscoverCodexModels discovers Codex model IDs using multiple strategies:
// 1. Run `strings` on the embedded Rust binary (works for unstripped binaries)
// 2. Read model info from the Codex state SQLite database (~/.codex/state_*.sqlite)
// 3. Fall back to hardcoded defaults based on the installed Codex version
func DiscoverCodexModels() []AgentModel {
	// Strategy 1: Try strings on the Rust binary
	if models := discoverCodexModelsFromBinary(); len(models) > 0 {
		return models
	}

	// Strategy 2: Read from Codex state SQLite database
	if models := discoverCodexModelsFromStateDB(); len(models) > 0 {
		return models
	}

	// Strategy 3: Hardcoded defaults for the current generation of Codex models
	// The Codex Rust binary is stripped, so strings extraction often fails.
	// We provide known model IDs based on the Codex version.
	return discoverCodexModelsDefaults()
}

// discoverCodexModelsFromBinary tries to extract model IDs by scanning the
// Codex Rust binary for printable strings. This works for unstripped or debug binaries.
func discoverCodexModelsFromBinary() []AgentModel {
	// Resolve the real path for the codex CLI, handling Windows .cmd wrappers
	realPath := platform.ResolveCLIPath("codex")
	if realPath == "" {
		return nil
	}

	// Navigate to the package directory: .../node_modules/@openai/codex/
	pkgDir := filepath.Dir(filepath.Dir(realPath))
	vendorDir := filepath.Join(pkgDir, "vendor")

	targetTriple := codexTargetTriple()
	if targetTriple == "" {
		return nil
	}

	binaryName := "codex"
	if runtime.GOOS == "windows" {
		binaryName = "codex.exe"
	}
	binaryPath := filepath.Join(vendorDir, targetTriple, "codex", binaryName)

	if _, err := os.Stat(binaryPath); err != nil {
		return nil
	}

	// Extract printable strings from the binary (cross-platform replacement for
	// the POSIX "strings" command, which does not exist on Windows)
	lines, err := platform.ExtractStrings(binaryPath, 4)
	if err != nil {
		slog.Debug("codex model discovery: extract strings failed", "path", binaryPath, "error", err)
		return nil
	}

	seen := make(map[string]bool)
	var models []AgentModel
	for _, line := range lines {
		if !codexModelRe.MatchString(line) || seen[line] {
			continue
		}
		seen[line] = true
		models = append(models, AgentModel{
			ID:   line,
			Name: line,
		})
	}

	if len(models) == 0 {
		return nil
	}

	sort.Slice(models, func(i, j int) bool {
		oi, okI := codexModelOrder[models[i].ID]
		oj, okJ := codexModelOrder[models[j].ID]
		if okI && okJ {
			return oi < oj
		}
		if okI {
			return true
		}
		if okJ {
			return false
		}
		return models[i].ID < models[j].ID
	})

	models[0].Default = true
	slog.Info("codex model discovery (strings) succeeded", "models", len(models))
	return models
}

// discoverCodexModelsFromStateDB reads model info from the Codex state SQLite database.
// The state database stores the model catalog that Codex fetched from OpenAI's API.
func discoverCodexModelsFromStateDB() []AgentModel {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Find the state SQLite database (e.g., state_5.sqlite)
	codexDir := filepath.Join(homeDir, ".codex")
	entries, err := os.ReadDir(codexDir)
	if err != nil {
		return nil
	}

	var dbPath string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "state_") && strings.HasSuffix(e.Name(), ".sqlite") {
			dbPath = filepath.Join(codexDir, e.Name())
			break
		}
	}

	if dbPath == "" {
		return nil
	}

	// Try to read models from the database
	// Codex stores model info in a "models" table or similar
	// Since we can't import C/sqlite3 directly, we use the codex CLI itself
	// to query models. But codex has no model listing command, so we skip this.
	return nil
}

// codexDefaultModels lists the known default models for the current Codex version.
// These are updated manually based on OpenAI's model catalog.
// When the strings approach or state DB approach works, those take priority.
var codexDefaultModels = []AgentModel{
	{ID: "gpt-5.5", Name: "GPT-5.5", Default: true},
	{ID: "gpt-5.4", Name: "GPT-5.4", Default: false},
	{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", Default: false},
}

// discoverCodexModelsDefaults returns hardcoded default models for Codex.
// This is the fallback when neither binary strings nor state DB extraction works.
func discoverCodexModelsDefaults() []AgentModel {
	// Only return defaults if codex is actually installed
	if platform.ResolveCLIPath("codex") == "" {
		return nil
	}

	models := make([]AgentModel, len(codexDefaultModels))
	copy(models, codexDefaultModels)
	slog.Info("codex model discovery: using hardcoded defaults", "models", len(models))
	return models
}

// --- Qoder model discovery ---

// qoderSkipModels are model IDs in the dynamic-texts.json that are tier-based
// selectors or routing aliases, not actual models.
var qoderSkipModels = map[string]bool{
	"auto":        true,
	"ultimate":    true,
	"performance": true,
	"efficient":   true,
	"lite":        true,
}

// qoderModelKeyRe matches keys like "modelSelector.item.qmodel" in the dynamic-texts JSON.
var qoderModelKeyRe = regexp.MustCompile(`^modelSelector\.item\.(.+)$`)

// DiscoverQoderModels discovers Qoder model IDs by reading the cached model catalog
// from ~/.qoder/.auth/dynamic-texts.json.
func DiscoverQoderModels() []AgentModel { //nolint:gocyclo // JSON-based model discovery
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("qoder model discovery: cannot determine home directory", "error", err)
		return nil
	}

	jsonPath := filepath.Join(homeDir, ".qoder", ".auth", "dynamic-texts.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		slog.Debug("qoder model discovery: dynamic-texts.json not found", "path", jsonPath, "error", err)
		return nil
	}

	var raw struct {
		Texts map[string]interface{} `json:"texts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Debug("qoder model discovery: failed to parse JSON", "error", err)
		return nil
	}

	if len(raw.Texts) == 0 {
		slog.Debug("qoder model discovery: empty texts in JSON")
		return nil
	}

	type modelInfo struct {
		id   string
		name string
	}
	var modelEntries []modelInfo

	for key, val := range raw.Texts {
		m := qoderModelKeyRe.FindStringSubmatch(key)
		if len(m) < 2 {
			continue
		}
		modelID := m[1]

		// Skip description/markdown suffixes
		if strings.HasSuffix(modelID, ".description") || strings.HasSuffix(modelID, ".markdownDescription") {
			continue
		}

		// Skip known tier/alias IDs
		if qoderSkipModels[modelID] {
			continue
		}

		// Skip experts-* entries
		if strings.HasPrefix(modelID, "experts-") {
			continue
		}

		// Skip quest-* entries
		if strings.HasPrefix(modelID, "quest-") {
			continue
		}

		// Skip internal preview/dogfooding models
		if strings.HasSuffix(modelID, "_preview") {
			continue
		}

		// Skip keys with dots in the remaining part (metadata like "lite.description.quest")
		if strings.Contains(modelID, ".") {
			continue
		}

		name := modelID
		if strVal, ok := val.(string); ok && strVal != "" {
			name = strVal
		}

		modelEntries = append(modelEntries, modelInfo{id: modelID, name: name})
	}

	if len(modelEntries) == 0 {
		return nil
	}

	var models []AgentModel
	for i, e := range modelEntries {
		models = append(models, AgentModel{
			ID:      e.id,
			Name:    e.name,
			Default: i == 0,
		})
	}

	slog.Info("qoder model discovery succeeded", "models", len(models))
	return models
}

// --- VeCLI model discovery ---

// vecliModelIDRe matches id: "xxx" in MODEL_REGISTRY entries.
var vecliModelIDRe = regexp.MustCompile(`id:\s*"([^"]+)"`)

// vecliModelNameRe matches name: "xxx" in MODEL_REGISTRY entries.
var vecliModelNameRe = regexp.MustCompile(`name:\s*"([^"]+)"`)

// DiscoverVeCLIModels discovers VeCLI model IDs by parsing the MODEL_REGISTRY array
// embedded in the VeCLI JS bundle. All models are included regardless of enabled status
// (users can still select disabled models via -m flag; enabled only controls the CLI's default UI).
func DiscoverVeCLIModels() []AgentModel { //nolint:gocyclo // binary parsing model discovery
	// Resolve the real path for the vecli CLI, handling Windows .cmd wrappers
	realPath := platform.ResolveCLIPath("vecli")
	if realPath == "" {
		return nil
	}

	data, err := os.ReadFile(realPath)
	if err != nil {
		slog.Debug("vecli model discovery: cannot read bundle file", "path", realPath, "error", err)
		return nil
	}

	content := string(data)

	registryStart := strings.Index(content, "MODEL_REGISTRY = [")
	if registryStart == -1 {
		slog.Debug("vecli model discovery: MODEL_REGISTRY not found in bundle")
		return nil
	}

	registryEnd := strings.Index(content[registryStart:], "];")
	if registryEnd == -1 {
		slog.Debug("vecli model discovery: MODEL_REGISTRY closing bracket not found")
		return nil
	}
	registrySection := content[registryStart : registryStart+registryEnd+2]

	type vecliEntry struct {
		id   string
		name string
	}

	var entries []vecliEntry
	entryStart := strings.Index(registrySection, "{")
	for entryStart != -1 {
		depth := 0
		i := entryStart
		for ; i < len(registrySection); i++ {
			if registrySection[i] == '{' {
				depth++
			} else if registrySection[i] == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if i >= len(registrySection) {
			break
		}

		block := registrySection[entryStart : i+1]

		var id, name string
		if m := vecliModelIDRe.FindStringSubmatch(block); len(m) >= 2 {
			id = m[1]
		}
		if m := vecliModelNameRe.FindStringSubmatch(block); len(m) >= 2 {
			name = m[1]
		}

		if id != "" {
			entries = append(entries, vecliEntry{id: id, name: name})
		}

		remaining := registrySection[i+1:]
		nextEntry := strings.Index(remaining, "{")
		if nextEntry == -1 {
			break
		}
		entryStart = i + 1 + nextEntry
	}

	if len(entries) == 0 {
		return nil
	}

	var models []AgentModel
	for i, e := range entries {
		name := e.name
		if name == "" {
			name = e.id
		}
		models = append(models, AgentModel{
			ID:      e.id,
			Name:    name,
			Default: i == 0,
		})
	}

	slog.Info("vecli model discovery succeeded", "models", len(models))
	return models
}

// --- Embedded Pi binary detection ---

// EmbeddedAgentPath returns the absolute path to the embedded Pi binary,
// or empty string if not found. Checks {binDir}/.clawbench/pi/ for the binary.
func EmbeddedAgentPath() string {
	exePath, err := os.Executable()
	if err != nil {
		slog.Error("failed to get executable path", "error", err)
		return ""
	}
	baseDir := filepath.Dir(exePath)
	for _, name := range []string{"pi", "pi.exe"} {
		p := filepath.Join(baseDir, ".clawbench", "pi", name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// EmbeddedAgentVersion extracts the version from the embedded Pi binary.
// First checks .clawbench/pi/VERSION file (fast), falls back to pi --version.
func EmbeddedAgentVersion() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	baseDir := filepath.Dir(exePath)

	// Fast path: read VERSION file
	versionFile := filepath.Join(baseDir, ".clawbench", "pi", "VERSION")
	if data, err := os.ReadFile(versionFile); err == nil {
		v := strings.TrimSpace(string(data))
		if v != "" {
			return v
		}
	}

	// Slow path: run pi --version
	piPath := EmbeddedAgentPath()
	if piPath == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, piPath, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// --- DB-based agent discovery and merge ---

// SyncDiscoverAgentsDB is the DB-based replacement for SyncDiscoverAgents.
// It detects installed CLIs from BackendRegistry and writes new agents to the database
// instead of YAML files. Existing DB records are never overwritten.
// It also checks for the embedded Pi binary.
// Returns a set of backend types whose CLI is currently present.
func SyncDiscoverAgentsDB(db *sql.DB) map[string]bool { //nolint:gocognit,gocyclo // multi-backend DB agent discovery
	type result struct {
		spec   BackendSpec
		exists bool
	}
	results := make([]result, len(BackendRegistry))
	var wg sync.WaitGroup
	for i, spec := range BackendRegistry {
		wg.Add(1)
		go func(i int, spec BackendSpec) {
			defer wg.Done()
			exists := spec.NoCLI || CheckCLIExists(spec.DefaultCmd)
			results[i] = result{spec: spec, exists: exists}
		}(i, spec)
	}
	wg.Wait()

	// Also check for embedded Pi binary
	embeddedPath := EmbeddedAgentPath()
	if embeddedPath != "" {
		// Mark pi as present for model discovery, but do NOT auto-create an agent.
		// The setup wizard handles agent creation with API key + model configuration.
		// Auto-creating here would set needs_setup=false, skipping the wizard,
		// and leave a broken agent with no API key.
		for i, r := range results {
			if r.spec.Backend == "pi" && !r.exists {
				results[i] = result{spec: r.spec, exists: true}
			}
		}
	}

	present := make(map[string]bool)

	for _, r := range results {
		if r.exists {
			present[r.spec.Backend] = true
		}

		// Skip auto-creation for Pi when it's only found via embedded binary.
		// The setup wizard handles Pi agent creation with API key + model config.
		// Auto-creating from embedded binary would leave a broken agent (no API key).
		if r.spec.Backend == "pi" && embeddedPath != "" {
			// Only auto-create if Pi CLI is genuinely installed on PATH (not just embedded)
			if _, lookupErr := exec.LookPath(r.spec.DefaultCmd); lookupErr != nil {
				continue
			}
		}

		// Check if DB already has an agent for this backend
		var count int
		var existingAcpCommand string
		err := db.QueryRow("SELECT COUNT(*), COALESCE(acp_command, '') FROM agents WHERE backend = ?", r.spec.Backend).Scan(&count, &existingAcpCommand)
		if err != nil {
			slog.Warn("failed to query agents table", "backend", r.spec.Backend, "error", err)
			continue
		}
		if count > 0 {
			// Update acp_command if it changed in BackendSpec (e.g., claude moved
			// from "claude acp" to the npx bridge adapter).
			if r.spec.AcpCommand != "" && existingAcpCommand != r.spec.AcpCommand {
				if _, updateErr := db.Exec("UPDATE agents SET acp_command = ? WHERE backend = ? AND source = 'auto'", r.spec.AcpCommand, r.spec.Backend); updateErr != nil {
					slog.Warn("failed to update acp_command", "backend", r.spec.Backend, "error", updateErr)
				} else {
					slog.Info("updated acp_command for auto-discovered agent", "backend", r.spec.Backend, "old", existingAcpCommand, "new", r.spec.AcpCommand)
				}
			}
			continue // Don't overwrite other existing DB fields
		}

		if !r.exists {
			continue
		}

		// New CLI found + no DB record → insert minimal agent
		agent := &Agent{
			ID:        r.spec.ID,
			Name:      r.spec.Name,
			Icon:      r.spec.Icon,
			Specialty: r.spec.Specialty,
			Backend:   r.spec.Backend,
			Source:    "auto",
		}

		// Set command to embedded path for pi backend
		if r.spec.Backend == "pi" && embeddedPath != "" {
			agent.Command = embeddedPath
		}

		// Store ACP command info from BackendSpec (transport defaults to "cli")
		if r.spec.AcpCommand != "" {
			agent.AcpCommand = r.spec.AcpCommand
		}

		if err := saveAgentToDB(db, agent); err != nil {
			slog.Warn("failed to insert agent to DB", "backend", r.spec.ID, "error", err)
			continue
		}
		slog.Info("auto-inserted agent to DB", "backend", r.spec.ID)
	}

	// Include backends that have existing DB records but are not in BackendRegistry
	// (e.g., wizard-created agents, manual agents, mock backend).
	// This ensures MergeDiscoveredDataDB doesn't soft-delete them.
	rows, err := db.Query("SELECT DISTINCT backend FROM agents")
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var backend string
			if err := rows.Scan(&backend); err == nil && !present[backend] {
				present[backend] = true
			}
		}
	}

	return present
}

// saveAgentToDB inserts a minimal agent record into the database.
func saveAgentToDB(db *sql.DB, agent *Agent) error {
	modelsJSON, err := json.Marshal(agent.Models)
	if err != nil {
		return fmt.Errorf("marshal models: %w", err)
	}
	// json.Marshal(nil slice) produces "null" instead of "[]" — normalize to "[]"
	if string(modelsJSON) == "null" {
		modelsJSON = []byte("[]")
	}
	levelsJSON, err := json.Marshal(agent.ThinkingEffortLevels)
	if err != nil {
		return fmt.Errorf("marshal thinking_effort_levels: %w", err)
	}

	transport := agent.Transport
	if transport == "" {
		transport = "cli"
	}

	_, err = db.Exec(`INSERT INTO agents (id, name, icon, specialty, backend, command,
		thinking_effort, thinking_effort_levels, preferred_model, preferred_thinking_effort,
		system_prompt, models, models_auto_detected, source, sort_order,
		transport, acp_command)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		agent.ID, agent.Name, agent.Icon, agent.Specialty, agent.Backend, agent.Command,
		agent.ThinkingEffort, string(levelsJSON), agent.PreferredModel, agent.PreferredThinkingEffort,
		agent.SystemPrompt, string(modelsJSON), agent.ModelsAutoDetected, agent.Source, agent.SortOrder,
		transport, agent.AcpCommand)
	return err
}

// yamlAgent represents the YAML structure for agent config files in config/agents/.
// This supports manually-defined agents (e.g., acp-mock for E2E testing) that are
// not in BackendRegistry and thus not auto-discovered by SyncDiscoverAgentsDB.
type yamlAgent struct {
	ID                      string       `yaml:"id"`
	Name                    string       `yaml:"name"`
	Icon                    string       `yaml:"icon"`
	Specialty               string       `yaml:"specialty"`
	Backend                 string       `yaml:"backend"`
	Command                 string       `yaml:"command"`
	ThinkingEffort          string       `yaml:"thinking_effort"`
	ThinkingEffortLevels    []string     `yaml:"thinking_effort_levels"`
	PreferredModel          string       `yaml:"preferred_model"`
	PreferredThinkingEffort string       `yaml:"preferred_thinking_effort"`
	SystemPrompt            string       `yaml:"system_prompt"`
	Transport               string       `yaml:"transport"`
	AcpCommand              string       `yaml:"acp_command"`
	Models                  []AgentModel `yaml:"models"`
	SortOrder               int          `yaml:"sort_order"`
}

// LoadYamlAgents reads agent definitions from config/agents/*.yaml and inserts
// them into the database if they don't already exist. This allows manually-defined
// agents (e.g., acp-mock for E2E testing) to be loaded without requiring an entry
// in BackendRegistry.
func LoadYamlAgents(db *sql.DB, configDir string) {
	agentsDir := filepath.Join(configDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read agents config dir", "path", agentsDir, "error", err)
		}
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(agentsDir, entry.Name()))
		if err != nil {
			slog.Warn("failed to read agent yaml", "file", entry.Name(), "error", err)
			continue
		}

		var ya yamlAgent
		if err := yaml.Unmarshal(data, &ya); err != nil {
			slog.Warn("failed to parse agent yaml", "file", entry.Name(), "error", err)
			continue
		}

		if ya.ID == "" || ya.Backend == "" {
			slog.Warn("agent yaml missing id or backend", "file", entry.Name())
			continue
		}

		// Check if agent already exists in DB
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM agents WHERE id = ?", ya.ID).Scan(&count)
		if err != nil {
			slog.Warn("failed to query agents table", "id", ya.ID, "error", err)
			continue
		}
		if count > 0 {
			continue // Don't overwrite existing DB records
		}

		agent := &Agent{
			ID:                      ya.ID,
			Name:                    ya.Name,
			Icon:                    ya.Icon,
			Specialty:               ya.Specialty,
			Backend:                 ya.Backend,
			Command:                 ya.Command,
			ThinkingEffort:          ya.ThinkingEffort,
			ThinkingEffortLevels:    ya.ThinkingEffortLevels,
			PreferredModel:          ya.PreferredModel,
			PreferredThinkingEffort: ya.PreferredThinkingEffort,
			SystemPrompt:            ya.SystemPrompt,
			Transport:               ya.Transport,
			AcpCommand:              ya.AcpCommand,
			Models:                  ya.Models,
			SortOrder:               ya.SortOrder,
			Source:                  "manual",
			ModelsAutoDetected:      len(ya.Models) == 0,
		}

		if err := saveAgentToDB(db, agent); err != nil {
			slog.Warn("failed to insert yaml agent to DB", "id", ya.ID, "error", err)
			continue
		}
		slog.Info("loaded agent from yaml config", "id", ya.ID, "file", entry.Name())
	}
}

// MergeDiscoveredDataDB is the DB-based replacement for MergeDiscoveredData.
// It performs three operations:
// 1. Soft-delete: DELETE auto-source agents whose backend is not in the present map
// 2. Fill ThinkingEffortLevels from BackendRegistry and update DB
// 3. Fill Models from cache for agents with empty models and update DB
// 4. Reload in-memory state from DB
func MergeDiscoveredDataDB(db *sql.DB, discoveredModels map[string][]AgentModel, present map[string]bool) { //nolint:gocognit,gocyclo // multi-step data merge
	// Step 1: Soft-delete auto agents whose CLI is not present
	if present != nil {
		// Build list of present backends for SQL
		presentBackends := make([]string, 0, len(present))
		for backend := range present {
			presentBackends = append(presentBackends, backend)
		}

		// Delete auto-source agents whose backend is NOT in present
		if len(presentBackends) > 0 {
			// Build placeholders
			placeholders := make([]string, len(presentBackends))
			args := make([]any, len(presentBackends)+1)
			args[0] = "auto" // source
			for i, b := range presentBackends {
				placeholders[i] = "?"
				args[i+1] = b
			}
			query := fmt.Sprintf("DELETE FROM agents WHERE source = ? AND backend NOT IN (%s)",
				strings.Join(placeholders, ","))
			result, err := db.Exec(query, args...)
			if err != nil {
				slog.Warn("failed to soft-delete missing CLI agents", "error", err)
			} else if rows, _ := result.RowsAffected(); rows > 0 {
				slog.Info("soft-deleted agents with missing CLIs", "count", rows)
			}
		} else {
			// No backends present — delete all auto agents
			result, err := db.Exec("DELETE FROM agents WHERE source = ?", "auto")
			if err != nil {
				slog.Warn("failed to soft-delete all auto agents", "error", err)
			} else if rows, _ := result.RowsAffected(); rows > 0 {
				slog.Info("soft-deleted all auto agents (no CLIs present)", "count", rows)
			}
		}
	}

	// Step 2: Fill ThinkingEffortLevels from BackendRegistry and update DB
	rows, err := db.Query("SELECT id, backend FROM agents")
	if err != nil {
		slog.Warn("failed to query agents for merge", "error", err)
		return
	}
	type agentRef struct {
		ID      string
		Backend string
	}
	var agentRefs []agentRef
	for rows.Next() {
		var ref agentRef
		if err := rows.Scan(&ref.ID, &ref.Backend); err != nil {
			continue
		}
		agentRefs = append(agentRefs, ref)
	}
	_ = rows.Close()

	for _, ref := range agentRefs {
		spec := FindSpecByBackend(ref.Backend)
		if spec == nil {
			continue
		}

		// Update ThinkingEffortLevels
		levelsJSON, _ := json.Marshal(spec.ThinkingEffortLevels)
		if _, err := db.Exec("UPDATE agents SET thinking_effort_levels = ? WHERE id = ?",
			string(levelsJSON), ref.ID); err != nil {
			slog.Warn("failed to update thinking_effort_levels", "id", ref.ID, "error", err)
		}
	}

	// Step 3: Fill Models from discovered results for agents with empty models
	rows, err = db.Query("SELECT id, backend, COALESCE(models, '[]') FROM agents WHERE (models IS NULL OR models = '[]' OR models = 'null') AND models_auto_detected = 0")
	if err != nil {
		slog.Warn("failed to query agents for model fill", "error", err)
		return
	}
	type agentModelRef struct {
		ID      string
		Backend string
	}
	var modelRefs []agentModelRef
	for rows.Next() {
		var ref agentModelRef
		var modelsStr string
		if err := rows.Scan(&ref.ID, &ref.Backend, &modelsStr); err != nil {
			continue
		}
		modelRefs = append(modelRefs, ref)
	}
	_ = rows.Close()

	for _, ref := range modelRefs {
		cached, ok := discoveredModels[ref.Backend]
		if !ok || len(cached) == 0 {
			continue
		}
		modelsJSON, _ := json.Marshal(cached)
		if _, err := db.Exec("UPDATE agents SET models = ?, models_auto_detected = 1 WHERE id = ?",
			string(modelsJSON), ref.ID); err != nil {
			slog.Warn("failed to update models from discovery", "id", ref.ID, "error", err)
		}
	}

	// Step 4: Reload in-memory state from DB
	agents, err := loadAgentsFromDBRows(db)
	if err != nil {
		slog.Warn("failed to reload agents from DB after merge", "error", err)
		return
	}

	Agents = make(map[string]*Agent)
	AgentList = agents
	for _, agent := range agents {
		Agents[agent.ID] = agent
		// Set CanRefreshModels from BackendRegistry (runtime only, not persisted)
		if spec := FindSpecByBackend(agent.Backend); spec != nil {
			agent.CanRefreshModels = CanDiscoverModels(*spec)
		}
	}

	// Build common prompt and prepend to each agent's system prompt
	commonPrompt := BuildCommonPrompt()
	for _, agent := range Agents {
		if commonPrompt != "" && agent.SystemPrompt != "" {
			agent.SystemPrompt = commonPrompt + "\n\n" + agent.SystemPrompt
		} else if commonPrompt != "" {
			agent.SystemPrompt = commonPrompt
		}
	}
}

// loadAgentsFromDBRows loads agents from the database into Agent structs.
func loadAgentsFromDBRows(db *sql.DB) ([]*Agent, error) {
	rows, err := db.Query(`SELECT id, name, icon, specialty, backend, command,
		thinking_effort, thinking_effort_levels, preferred_model, preferred_thinking_effort,
		system_prompt, models, models_auto_detected, source, sort_order,
		transport, acp_command
		FROM agents ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var agents []*Agent
	for rows.Next() {
		agent := &Agent{}
		var modelsJSON, levelsJSON string
		var autoDetected int

		err := rows.Scan(&agent.ID, &agent.Name, &agent.Icon, &agent.Specialty,
			&agent.Backend, &agent.Command, &agent.ThinkingEffort, &levelsJSON,
			&agent.PreferredModel, &agent.PreferredThinkingEffort,
			&agent.SystemPrompt, &modelsJSON, &autoDetected,
			&agent.Source, &agent.SortOrder,
			&agent.Transport, &agent.AcpCommand)
		if err != nil {
			return nil, err
		}

		agent.ModelsAutoDetected = autoDetected == 1

		if err := json.Unmarshal([]byte(modelsJSON), &agent.Models); err != nil {
			agent.Models = nil
		}
		if err := json.Unmarshal([]byte(levelsJSON), &agent.ThinkingEffortLevels); err != nil {
			agent.ThinkingEffortLevels = nil
		}

		agents = append(agents, agent)
	}
	return agents, nil
}

// --- Cline model discovery ---

// clineDefaultModels lists known models for Cline CLI.
// Cline supports multiple providers; these are the most commonly used models.
var clineDefaultModels = []AgentModel{
	{ID: "anthropic/claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "anthropic/claude-opus-4-20250514", Name: "Claude Opus 4"},
	{ID: "openai/gpt-4.1", Name: "GPT-4.1"},
	{ID: "openai/gpt-4o", Name: "GPT-4o"},
	{ID: "openai/o3", Name: "o3"},
	{ID: "openai/o4-mini", Name: "o4-mini"},
	{ID: "google/gemini-2.5-pro", Name: "Gemini 2.5 Pro"},
	{ID: "google/gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
	{ID: "minimax/MiniMax-M1", Name: "MiniMax-M1"},
	{ID: "minimax/MiniMax-M2.7", Name: "MiniMax-M2.7"},
}

// DiscoverClineModels discovers models for Cline CLI.
func DiscoverClineModels() []AgentModel {
	if _, err := exec.LookPath("cline"); err != nil {
		return nil
	}
	models := make([]AgentModel, len(clineDefaultModels))
	copy(models, clineDefaultModels)
	slog.Info("cline model discovery: using hardcoded defaults", "models", len(models))
	return models
}

// --- Kimi model discovery ---

// kimiDefaultModels lists known models for Kimi CLI.
var kimiDefaultModels = []AgentModel{
	{ID: "kimi-k2-0711-chat", Name: "Kimi K2"},
	{ID: "moonshot-v1-128k", Name: "Moonshot v1 128K"},
	{ID: "moonshot-v1-32k", Name: "Moonshot v1 32K"},
	{ID: "moonshot-v1-8k", Name: "Moonshot v1 8K"},
	{ID: "kimi-latest", Name: "Kimi Latest"},
}

// DiscoverKimiModels discovers models for Kimi CLI.
func DiscoverKimiModels() []AgentModel {
	if _, err := exec.LookPath("kimi"); err != nil {
		return nil
	}
	models := make([]AgentModel, len(kimiDefaultModels))
	copy(models, kimiDefaultModels)
	slog.Info("kimi model discovery: using hardcoded defaults", "models", len(models))
	return models
}

// --- Copilot model discovery ---

// copilotDefaultModels lists known models for GitHub Copilot CLI.
var copilotDefaultModels = []AgentModel{
	{ID: "gpt-4.1", Name: "GPT-4.1"},
	{ID: "gpt-4o", Name: "GPT-4o"},
	{ID: "o3", Name: "o3"},
	{ID: "o4-mini", Name: "o4-mini"},
	{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4"},
	{ID: "claude-opus-4-20250514", Name: "Claude Opus 4"},
	{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro"},
	{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
}

// DiscoverCopilotModels discovers models for GitHub Copilot CLI.
func DiscoverCopilotModels() []AgentModel {
	if _, err := exec.LookPath("copilot"); err != nil {
		return nil
	}
	models := make([]AgentModel, len(copilotDefaultModels))
	copy(models, copilotDefaultModels)
	slog.Info("copilot model discovery: using hardcoded defaults", "models", len(models))
	return models
}

// --- MiMo-Code model discovery ---

// mimoDefaultModels lists known models for MiMo-Code CLI.
// MiMo-Code supports multiple providers; these are the most commonly used models.
var mimoDefaultModels = []AgentModel{
	{ID: "mimo/mimo-auto", Name: "MiMo Auto", Default: true},
	{ID: "xiaomi/mimo-v2.5-pro-ultraspeed", Name: "MiMo V2.5 Pro Ultraspeed"},
	{ID: "xiaomi/mimo-v2.5-pro", Name: "MiMo V2.5 Pro"},
	{ID: "xiaomi/mimo-v2.5", Name: "MiMo V2.5"},
	{ID: "xiaomi/mimo-v2-pro", Name: "MiMo V2 Pro"},
	{ID: "xiaomi/mimo-v2-omni", Name: "MiMo V2 Omni"},
	{ID: "xiaomi/mimo-v2-flash", Name: "MiMo V2 Flash"},
}

// DiscoverMimoModels discovers models for MiMo-Code CLI.
// MiMo-Code is based on OpenCode and uses the same provider/model format.
func DiscoverMimoModels() []AgentModel {
	if _, err := exec.LookPath("mimo"); err != nil {
		return nil
	}
	models := make([]AgentModel, len(mimoDefaultModels))
	copy(models, mimoDefaultModels)
	slog.Info("mimo model discovery: using hardcoded defaults", "models", len(models))
	return models
}
