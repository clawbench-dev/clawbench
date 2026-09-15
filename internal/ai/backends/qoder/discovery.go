package qoder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"clawbench/internal/model"
)

func init() {
	model.RegisterModelSource(model.PluginSource("qoder", discoverQoderModels))
}

// qoderSkipModels are keys in dynamic-texts.json that are tier selectors or
// routing aliases rather than concrete models.
var qoderSkipModels = map[string]bool{
	"auto":        true,
	"ultimate":    true,
	"performance": true,
	"efficient":   true,
	"lite":        true,
}

// qoderModelKeyRe matches keys like "modelSelector.item.qmodel".
var qoderModelKeyRe = regexp.MustCompile(`^modelSelector\.item\.(.+)$`)

// qoderTextsPath is overridable in tests.
var qoderTextsPath = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".qoder", ".auth", "dynamic-texts.json"), nil
}

// discoverQoderModels reads Qoder's cached model catalog from
// ~/.qoder/.auth/dynamic-texts.json. The file is written by the Qoder CLI on
// login, so its absence means the CLI has never authenticated.
func discoverQoderModels() ([]model.AgentModel, string) {
	path, err := qoderTextsPath()
	if err != nil {
		return nil, fmt.Sprintf("qoder: cannot determine home directory: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("qoder: %s not found (log in with the qoder CLI first)", path)
	}

	var raw struct {
		Texts map[string]interface{} `json:"texts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Sprintf("qoder: %s is not valid JSON: %v", path, err)
	}
	if len(raw.Texts) == 0 {
		return nil, fmt.Sprintf("qoder: %s contains no texts", path)
	}

	type entry struct{ id, name string }
	var entries []entry

	for key, val := range raw.Texts {
		m := qoderModelKeyRe.FindStringSubmatch(key)
		if len(m) < 2 {
			continue
		}
		id := m[1]
		if !isQoderModelID(id) {
			continue
		}
		name := id
		if s, ok := val.(string); ok && s != "" {
			name = s
		}
		entries = append(entries, entry{id: id, name: name})
	}

	if len(entries) == 0 {
		return nil, fmt.Sprintf("qoder: no model entries under modelSelector.item.* in %s", path)
	}

	models := make([]model.AgentModel, 0, len(entries))
	for _, e := range entries {
		models = append(models, model.AgentModel{ID: e.id, Name: e.name})
	}
	return models, ""
}

// isQoderModelID filters out the keys that share the modelSelector.item.*
// prefix but are not selectable models: description variants, tier aliases,
// internal preview builds and metadata keys.
//
// The metadata check looks for a KNOWN metadata suffix rather than any dot:
// real model IDs contain dots too ("gpt-4.1"), so a blanket dot rule would drop
// them. Metadata keys observed in the wild: "lite.description.quest",
// "auto.markdownDescription".
func isQoderModelID(id string) bool {
	if strings.Contains(id, ".") {
		for _, suffix := range qoderMetadataSuffixes {
			if strings.HasSuffix(id, suffix) {
				return false
			}
		}
	}
	switch {
	case strings.HasPrefix(id, "experts-"),
		strings.HasPrefix(id, "quest-"),
		strings.HasSuffix(id, "_preview"):
		return false
	}
	return !qoderSkipModels[id]
}

// qoderMetadataSuffixes are the key suffixes that mark a metadata entry rather
// than a model.
var qoderMetadataSuffixes = []string{
	".description",
	".markdownDescription",
	".quest",
	".lite",
}
