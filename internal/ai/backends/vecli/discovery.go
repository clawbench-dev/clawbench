package vecli

import (
	"log/slog"
	"os"
	"regexp"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

func init() {
	model.RegisterDiscoverModelsFunc("vecli", DiscoverVeCLIModels)
}

// vecliModelIDRe matches id: "xxx" in MODEL_REGISTRY entries.
var vecliModelIDRe = regexp.MustCompile(`id:\s*"([^"]+)"`)

// vecliModelNameRe matches name: "xxx" in MODEL_REGISTRY entries.
var vecliModelNameRe = regexp.MustCompile(`name:\s*"([^"]+)"`)

// DiscoverVeCLIModels discovers VeCLI model IDs by parsing the MODEL_REGISTRY array
// embedded in the VeCLI JS bundle. All models are included regardless of enabled status
// (users can still select disabled models via -m flag; enabled only controls the CLI's default UI).
func DiscoverVeCLIModels() []model.AgentModel { //nolint:gocyclo // binary parsing model discovery
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

	var models []model.AgentModel
	for i, e := range entries {
		name := e.name
		if name == "" {
			name = e.id
		}
		models = append(models, model.AgentModel{
			ID:      e.id,
			Name:    name,
			Default: i == 0,
		})
	}

	slog.Info("vecli model discovery succeeded", "models", len(models))
	return models
}
