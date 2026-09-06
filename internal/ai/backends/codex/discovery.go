package codex

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

func init() {
	model.RegisterDiscoverModelsFunc("codex", DiscoverCodexModels)
}

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
// 1. Read the model catalog cached by modern Codex CLI versions
// 2. Run `strings` on the embedded Rust binary (works for unstripped binaries)
// 3. Read model info from the Codex state SQLite database (~/.codex/state_*.sqlite)
// 4. Fall back to hardcoded defaults based on the installed Codex version
func DiscoverCodexModels() []model.AgentModel {
	// Strategy 1: Read the complete model catalog cached by modern Codex versions
	if platform.ResolveCLIPath("codex") != "" {
		if models := discoverCodexModelsFromCache(); len(models) > 0 {
			models[0].Default = true
			return models
		}
	}

	// Strategy 2: Try strings on the Rust binary
	if models := discoverCodexModelsFromBinary(); len(models) > 0 {
		return models
	}

	// Strategy 3: Read from Codex state SQLite database
	if models := discoverCodexModelsFromStateDB(); len(models) > 0 {
		return models
	}

	// Strategy 4: Hardcoded defaults for the current generation of Codex models
	// The Codex Rust binary is stripped, so strings extraction often fails.
	// We provide known model IDs based on the Codex version.
	return discoverCodexModelsDefaults()
}

// discoverCodexModelsFromCache reads the model catalog fetched by modern
// Codex CLI versions. Unlike binary strings, this contains the complete list
// currently available to the authenticated account.
func discoverCodexModelsFromCache() []model.AgentModel {
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil
	}
	cachePath := filepath.Join(codexHome, "models_cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		slog.Debug("codex model discovery: read cache failed", "path", cachePath, "error", err)
		return nil
	}
	var cache struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		slog.Debug("codex model discovery: parse cache failed", "path", cachePath, "error", err)
		return nil
	}
	models := make([]model.AgentModel, 0, len(cache.Models))
	seen := make(map[string]struct{}, len(cache.Models))
	for _, item := range cache.Models {
		if item.Slug == "" || (item.Visibility != "" && item.Visibility != "list") {
			continue
		}
		if _, ok := seen[item.Slug]; ok {
			continue
		}
		seen[item.Slug] = struct{}{}
		name := item.DisplayName
		if name == "" {
			name = item.Slug
		}
		models = append(models, model.AgentModel{ID: item.Slug, Name: name})
	}
	if len(models) > 0 {
		slog.Info("codex model discovery (cache) succeeded", "models", len(models))
	}
	return models
}

// discoverCodexModelsFromBinary tries to extract model IDs by scanning the
// Codex Rust binary for printable strings. This works for unstripped or debug binaries.
func discoverCodexModelsFromBinary() []model.AgentModel {
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
	var models []model.AgentModel
	for _, line := range lines {
		if !codexModelRe.MatchString(line) || seen[line] {
			continue
		}
		seen[line] = true
		models = append(models, model.AgentModel{
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
func discoverCodexModelsFromStateDB() []model.AgentModel {
	codexDir, err := resolveCodexHome()
	if err != nil {
		return nil
	}

	// Find the state SQLite database (e.g., state_5.sqlite)
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
var codexDefaultModels = []model.AgentModel{
	{ID: "gpt-5.5", Name: "GPT-5.5", Default: true},
	{ID: "gpt-5.4", Name: "GPT-5.4", Default: false},
	{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", Default: false},
}

// discoverCodexModelsDefaults returns hardcoded default models for Codex.
// This is the fallback when neither binary strings nor state DB extraction works.
func discoverCodexModelsDefaults() []model.AgentModel {
	// Only return defaults if codex is actually installed
	if platform.ResolveCLIPath("codex") == "" {
		return nil
	}

	models := make([]model.AgentModel, len(codexDefaultModels))
	copy(models, codexDefaultModels)
	slog.Info("codex model discovery: using hardcoded defaults", "models", len(models))
	return models
}
