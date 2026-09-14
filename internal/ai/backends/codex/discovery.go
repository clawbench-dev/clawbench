package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

func init() {
	model.RegisterModelSource(model.PluginSource("codex", discoverCodexModels))
}

// codexModelRe matches OpenAI model IDs in the Codex binary's string output.
var codexModelRe = regexp.MustCompile(`^(gpt-\d+\.\d+(-mini)?|o[34](-mini)?)$`)

// discoverCodexModels discovers Codex models, in order of trustworthiness:
//
//  1. The model catalog cached by modern Codex CLI versions. This is the list
//     the authenticated account can actually use, so it is preferred over
//     anything extracted from the binary.
//  2. Strings extracted from the bundled Rust binary. Only works for
//     unstripped builds.
//  3. The built-in catalog.
//
// Codex has no model-list command, so there is no CLI path.
func discoverCodexModels() ([]model.AgentModel, string) {
	if platform.ResolveCLIPath("codex") == "" {
		return nil, "codex: CLI not found on PATH"
	}

	if models := discoverCodexModelsFromCache(); len(models) > 0 {
		return models, ""
	}
	if models := discoverCodexModelsFromBinary(); len(models) > 0 {
		return models, ""
	}
	return model.CodexCatalog, "codex: no model catalog cached and binary strings unavailable, using built-in catalog"
}

// discoverCodexModelsFromCache reads the catalog modern Codex versions fetch
// from the API. Unlike binary strings, this reflects the authenticated
// account's entitlements.
func discoverCodexModelsFromCache() []model.AgentModel {
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "models_cache.json"))
	if err != nil {
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
		return nil
	}

	seen := make(map[string]struct{}, len(cache.Models))
	var models []model.AgentModel
	for _, item := range cache.Models {
		// Visibility other than "list" means the model exists but must not be
		// offered in a picker.
		if item.Slug == "" || (item.Visibility != "" && item.Visibility != "list") {
			continue
		}
		if _, dup := seen[item.Slug]; dup {
			continue
		}
		seen[item.Slug] = struct{}{}
		name := item.DisplayName
		if name == "" {
			name = item.Slug
		}
		models = append(models, model.AgentModel{ID: item.Slug, Name: name})
	}
	return models
}

// discoverCodexModelsFromBinary extracts model IDs from the Codex Rust binary.
func discoverCodexModelsFromBinary() []model.AgentModel {
	realPath := platform.ResolveCLIPath("codex")
	if realPath == "" {
		return nil
	}
	triple := codexTargetTriple()
	if triple == "" {
		return nil
	}

	// realPath is .../@openai/codex/<bin>; the binary lives under vendor/<triple>/codex/.
	pkgDir := filepath.Dir(filepath.Dir(realPath))
	binaryName := "codex"
	if runtime.GOOS == "windows" {
		binaryName = "codex.exe"
	}
	binaryPath := filepath.Join(pkgDir, "vendor", triple, "codex", binaryName)
	if _, err := os.Stat(binaryPath); err != nil {
		return nil
	}

	lines, err := platform.ExtractStrings(binaryPath, 4)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var models []model.AgentModel
	for _, line := range lines {
		if !codexModelRe.MatchString(line) || seen[line] {
			continue
		}
		seen[line] = true
		models = append(models, model.AgentModel{ID: line, Name: line})
	}
	if len(models) == 0 {
		return nil
	}

	sort.Slice(models, func(i, j int) bool {
		oi, okI := model.CodexModelOrder[models[i].ID]
		oj, okJ := model.CodexModelOrder[models[j].ID]
		switch {
		case okI && okJ:
			return oi < oj
		case okI:
			return true
		case okJ:
			return false
		default:
			return models[i].ID < models[j].ID
		}
	})
	return models
}

// codexTargetTriple returns the Rust target triple for the current platform,
// which determines the vendor subdirectory holding the binary.
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
