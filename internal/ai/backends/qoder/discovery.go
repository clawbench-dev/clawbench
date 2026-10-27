package qoder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"clawbench/internal/model"
)

// QoderTextsRelPath is where both Qoder distributions cache the localized
// dynamic-texts table inside their own config root.
//
// The root differs per distribution (`~/.qoder` vs `~/.qoder-cn`) but the
// layout inside it does not.
const QoderTextsRelPath = ".auth/dynamic-texts.json"

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
	return TextsPathFor("~/.qoder")
}

// TextsPathFor resolves the dynamic-texts cache path for a config root.
// configRoot may start with "~/" (expanded to the user's home).
func TextsPathFor(configRoot string) (string, error) {
	root := configRoot
	if strings.HasPrefix(root, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(root, "~"), "/"))
	}
	return filepath.Join(root, filepath.FromSlash(QoderTextsRelPath)), nil
}

// DiscoverModelsIn reads one Qoder distribution's cached model catalog.
//
// label is the backend id used in diagnostic strings ("qoder" / "qodercn"),
// configRoot is that distribution's user config root. The file is written by
// the CLI on login, so its absence means the CLI has never authenticated.
func DiscoverModelsIn(label, configRoot string) ([]model.AgentModel, string) {
	path, err := TextsPathFor(configRoot)
	if err != nil {
		return nil, fmt.Sprintf("%s: cannot determine home directory: %v", label, err)
	}
	return discoverModelsAt(label, path)
}

// discoverQoderModels reads the international build's catalog.
func discoverQoderModels() ([]model.AgentModel, string) {
	path, err := qoderTextsPath()
	if err != nil {
		return nil, fmt.Sprintf("qoder: cannot determine home directory: %v", err)
	}
	return discoverModelsAt("qoder", path)
}

func discoverModelsAt(label, path string) ([]model.AgentModel, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("%s: %s not found (log in with the %s CLI first)", label, path, label)
	}

	texts, err := parseQoderTexts(data)
	if err != nil {
		return nil, fmt.Sprintf("%s: %s: %v", label, path, err)
	}
	if len(texts) == 0 {
		return nil, fmt.Sprintf("%s: %s contains no texts", label, path)
	}

	type entry struct{ id, name string }
	var entries []entry

	for key, val := range texts {
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
		return nil, fmt.Sprintf("%s: no model entries under modelSelector.item.* in %s", label, path)
	}

	models := make([]model.AgentModel, 0, len(entries))
	for _, e := range entries {
		models = append(models, model.AgentModel{ID: e.id, Name: e.name})
	}
	return models, ""
}

// parseQoderTexts extracts the flat key→text table from a dynamic-texts file.
//
// Two on-disk shapes exist:
//
//	schemaVersion 1–2 (legacy): {"texts": {"modelSelector.item.x": "X"}}
//	schemaVersion 3   (current): {"locales": {"en": {...}, "zh-CN": {...}}}
//
// The current shape is what ships today. A parser that only reads `texts`
// finds nothing and reports "contains no texts" — which is exactly what the
// old implementation did against every real install. Both shapes are accepted;
// when both are present `texts` wins (it is the older, more specific table).
//
// Locale selection: English first, then the remaining locales in sorted order.
// Model *ids* are identical across locales (only display names differ), so a
// non-preferred locale would only change labels — but the choice is made
// deterministic anyway, since map iteration order is randomized and a flapping
// label source makes discovery results look unstable.
func parseQoderTexts(data []byte) (map[string]interface{}, error) {
	var raw struct {
		Texts   map[string]interface{}            `json:"texts"`
		Locales map[string]map[string]interface{} `json:"locales"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	if len(raw.Texts) > 0 {
		return raw.Texts, nil
	}
	if len(raw.Locales) == 0 {
		return nil, nil
	}
	if en, ok := raw.Locales["en"]; ok && len(en) > 0 {
		return en, nil
	}
	names := make([]string, 0, len(raw.Locales))
	for name := range raw.Locales {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(raw.Locales[name]) > 0 {
			return raw.Locales[name], nil
		}
	}
	return nil, nil
}

// isQoderModelID filters out the keys that share the modelSelector.item.*
// prefix but are not selectable models: description variants, tier aliases,
// internal preview builds and metadata keys.
//
// The metadata check looks for a KNOWN metadata suffix rather than any dot:
// real model IDs contain dots too ("gpt-4.1"), so a blanket dot rule would drop
// them. Metadata keys observed in the wild: "lite.description.quest",
// "auto.markdownDescription", "kmodel.functionSwitch.highspeed.description",
// "experts-ultimate.teams.markdownDescription".
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
