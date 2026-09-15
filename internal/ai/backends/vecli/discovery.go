package vecli

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"clawbench/internal/model"
	"clawbench/internal/platform"
)

func init() {
	model.RegisterModelSource(model.PluginSource("vecli", discoverVeCLIModels))
}

// vecliModelIDRe matches id: "xxx" in MODEL_REGISTRY entries.
var vecliModelIDRe = regexp.MustCompile(`id:\s*"([^"]+)"`)

// vecliModelNameRe matches name: "xxx" in MODEL_REGISTRY entries.
var vecliModelNameRe = regexp.MustCompile(`name:\s*"([^"]+)"`)

// discoverVeCLIModels parses the MODEL_REGISTRY array embedded in the VeCLI JS
// bundle. All entries are included regardless of their enabled flag: the flag
// only controls the CLI's default UI, and users can still select a disabled
// model with -m.
func discoverVeCLIModels() ([]model.AgentModel, string) {
	realPath := platform.ResolveCLIPath("vecli")
	if realPath == "" {
		return nil, "vecli: CLI not found on PATH"
	}

	data, err := os.ReadFile(realPath)
	if err != nil {
		return nil, fmt.Sprintf("vecli: cannot read bundle %s: %v", realPath, err)
	}
	content := string(data)

	registry, ok := extractRegistrySection(content)
	if !ok {
		return nil, fmt.Sprintf("vecli: MODEL_REGISTRY not found in %s", realPath)
	}

	entries := parseRegistryEntries(registry)
	if len(entries) == 0 {
		return nil, fmt.Sprintf("vecli: MODEL_REGISTRY in %s contained no model entries", realPath)
	}

	models := make([]model.AgentModel, 0, len(entries))
	for _, e := range entries {
		name := e.name
		if name == "" {
			name = e.id
		}
		models = append(models, model.AgentModel{ID: e.id, Name: name})
	}
	slog.Debug("vecli model discovery: parsed registry", "models", len(models))
	return models, ""
}

// extractRegistrySection returns the text of the MODEL_REGISTRY array literal.
func extractRegistrySection(content string) (string, bool) {
	start := strings.Index(content, "MODEL_REGISTRY = [")
	if start == -1 {
		return "", false
	}
	rest := content[start:]
	end := strings.Index(rest, "];")
	if end == -1 {
		return "", false
	}
	return rest[:end+2], true
}

type vecliEntry struct{ id, name string }

// parseRegistryEntries walks brace-balanced object literals inside the registry
// array and pulls the id/name string fields out of each.
func parseRegistryEntries(registry string) []vecliEntry {
	var entries []vecliEntry
	for _, block := range braceBlocks(registry) {
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
	}
	return entries
}

// braceBlocks yields top-level {...} substrings at depth 1, tolerating nested
// braces inside an entry.
func braceBlocks(s string) []string {
	var blocks []string
	for i := 0; i < len(s); {
		if s[i] != '{' {
			i++
			continue
		}
		depth := 0
		j := i
		for ; j < len(s); j++ {
			switch s[j] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if j >= len(s) {
			break // unbalanced — stop rather than emit a truncated block
		}
		blocks = append(blocks, s[i:j+1])
		i = j + 1
	}
	return blocks
}
