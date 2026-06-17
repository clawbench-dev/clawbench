package codebuddy

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

func init() {
	model.RegisterDiscoverModelsFunc("codebuddy", DiscoverCodebuddyModels)
}

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
// file from the CLI installation directory.
func DiscoverCodebuddyModels() []model.AgentModel {
	// Resolve the real path for the codebuddy CLI, handling Windows .cmd wrappers
	realPath := platform.ResolveCLIPath("codebuddy")
	if realPath == "" {
		return nil
	}

	// The product JSON is at .../codebuddy-code/product.cloudhosted.json
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

	var models []model.AgentModel
	for _, m := range product.Models {
		if m.ID == "default" || m.ID == "auto" {
			continue
		}
		if m.ID == "hunyuan-image-v3.0" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, model.AgentModel{
			ID:      m.ID,
			Name:    name,
			Default: m.IsDefault || (len(models) == 0 && m.ID != "default" && m.ID != "auto"),
		})
	}

	if len(models) == 0 {
		return nil
	}

	if !models[0].Default {
		models[0].Default = true
	}

	slog.Info("codebuddy model discovery succeeded", "models", len(models))
	return models
}

// codebuddyModelRe extracts model IDs from codebuddy --help output (legacy, kept for ParseCodebuddyModels).
var codebuddyModelRe = regexp.MustCompile(`Currently supported: \(([^)]+)\)`)

// ParseCodebuddyModels parses codebuddy --help output to extract model IDs.
//
// Deprecated: codebuddy --help launches a TUI that hangs without a TTY; use DiscoverCodebuddyModels instead.
func ParseCodebuddyModels(output string) []model.AgentModel {
	matches := codebuddyModelRe.FindStringSubmatch(output)
	if len(matches) < 2 {
		return nil
	}

	parts := strings.Split(matches[1], ",")
	var models []model.AgentModel
	for i, p := range parts {
		id := strings.TrimSpace(p)
		if id == "" {
			continue
		}
		models = append(models, model.AgentModel{
			ID:      id,
			Name:    id,
			Default: i == 0,
		})
	}
	return models
}
