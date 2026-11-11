package model

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// configReferencePath is the user-facing reference for every config.yaml field.
// The path is relative to this package so the test works from any checkout
// location (including a git worktree under .worktrees/).
const configReferencePath = "../../docs/user-guide/configuration.md"

// configReferenceSection marks the part of the reference that documents
// config.yaml. Tables outside it (data directory layout, per-device
// preferences, settings-panel index) are deliberately not parsed: they talk
// about the same words without being config keys.
const configReferenceSection = "## 一、服务端配置文件"

// configReferenceExempt lists yaml paths that intentionally have no row in the
// reference. A key ending in "." exempts every field beneath it.
//
// Every entry needs a reason. "It was easier than writing a row" is not one —
// the point of this list is that a NEW user-facing field cannot be added
// without someone deciding, in the diff, where it is documented.
var configReferenceExempt = map[string]string{
	"tls.enabled":   "legacy field, documented as a note under the tls section",
	"tls.cert_file": "legacy field, documented as a note under the tls section",
	"tls.key_file":  "legacy field, documented as a note under the tls section",

	"skills.dir": "legacy single-directory key, migrated into skills.dirs on load",

	"dingtalk.enabled": "derived from push_mode on every load, never hand-written",
	"feishu.enabled":   "derived from push_mode on every load, never hand-written",

	"ai_summary.api.":   "legacy hand-written endpoint, superseded by provider_id + model",
	"ai_summary.format": "legacy hand-written field, the settings page never writes it",

	"rag.ollama_base_url": "legacy key, migrated into rag.base_url on load",
	"rag.ollama_model":    "legacy key, migrated into rag.model on load",

	"skills.last_error":       "server-owned sync state, not user configuration",
	"skills.last_sync_at":     "server-owned sync state, not user configuration",
	"skills.repos.last_error": "server-owned per-repo sync error",

	"usage_meter.notified_cycle_start": "server-owned reminder state",
	"usage_meter.notified_stage":       "server-owned reminder state",

	"appearance.local.items.": "gallery manifest written by the upload endpoint",
	"appearance.bing.":        "wallpaper cache written by the fetch worker",
}

// TestConfigReferenceMatchesConfigFile keeps docs/user-guide/configuration.md
// and the Config struct in step, in both directions:
//
//   - every yaml field in the struct is documented (otherwise a new setting
//     ships with no user-facing explanation);
//   - every key the reference lists resolves to a real field (otherwise the
//     manual describes a field that was renamed or deleted).
//
// It is a drift guard, not a proof of correctness: it checks presence, not
// whether the description is still true.
func TestConfigReferenceMatchesConfigFile(t *testing.T) {
	raw, err := os.ReadFile(configReferencePath)
	if err != nil {
		t.Fatalf("read config reference: %v", err)
	}
	doc := string(raw)

	codePaths := map[string]bool{}
	collectYAMLPaths(reflect.TypeOf(Config{}), "", codePaths)
	if len(codePaths) < 50 {
		t.Fatalf("reflection produced %d config paths; the walker is broken", len(codePaths))
	}

	documented, problems := parseDocumentedConfigPaths(doc, codePaths)
	if len(documented) == 0 {
		t.Fatalf("parsed 0 documented config keys — did the section heading %q change?", configReferenceSection)
	}

	var undocumented []string
	for path := range codePaths {
		if documented[path] || exemptFromReference(path) {
			continue
		}
		undocumented = append(undocumented, path)
	}
	sort.Strings(undocumented)

	if len(undocumented) > 0 {
		t.Errorf("%d config field(s) are missing from %s.\n"+
			"Add a row for each (or an entry with a reason in configReferenceExempt):\n  %s",
			len(undocumented), configReferencePath, strings.Join(undocumented, "\n  "))
	}
	if len(problems) > 0 {
		t.Errorf("%d key(s) in %s match no field of model.Config "+
			"(renamed or removed?):\n  %s",
			len(problems), configReferencePath, strings.Join(problems, "\n  "))
	}

	// An exemption for a field that no longer exists is worse than no
	// exemption: it silently pre-approves a name that a future field could
	// reuse. Every entry has to still point at something real.
	var rotten []string
	for key := range configReferenceExempt {
		matches := false
		for path := range codePaths {
			if path == key || (strings.HasSuffix(key, ".") && strings.HasPrefix(path, key)) {
				matches = true
				break
			}
		}
		if !matches {
			rotten = append(rotten, key)
		}
	}
	sort.Strings(rotten)
	if len(rotten) > 0 {
		t.Errorf("configReferenceExempt lists %d path(s) that no longer exist in "+
			"model.Config; drop them:\n  %s", len(rotten), strings.Join(rotten, "\n  "))
	}
}

// collectYAMLPaths walks a type and records the dotted yaml path of every leaf
// field. Structs recurse, and so do slices/arrays/maps of structs, so a
// []MCPServer yields "mcp.servers.name" rather than stopping at the container.
// Fields without a yaml tag (LogDir) and "-" tags are skipped: they are not
// part of the file format.
func collectYAMLPaths(t reflect.Type, prefix string, out map[string]bool) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			tag := strings.Split(field.Tag.Get("yaml"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			collectYAMLPaths(field.Type, joinYAMLPath(prefix, tag), out)
		}
	case reflect.Slice, reflect.Array:
		collectYAMLPaths(t.Elem(), prefix, out)
	case reflect.Map:
		collectYAMLPaths(t.Elem(), prefix, out)
	default:
		if prefix != "" {
			out[prefix] = true
		}
	}
}

func joinYAMLPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func exemptFromReference(path string) bool {
	if _, ok := configReferenceExempt[path]; ok {
		return true
	}
	for prefix := range configReferenceExempt {
		if strings.HasSuffix(prefix, ".") && strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// parseDocumentedConfigPaths reads the config.yaml section of the reference and
// resolves every key it finds in a table row back to a full yaml path.
//
// Resolution mirrors how the reference is written: a section heading names its
// yaml key ("### 聊天 `chat`"), rows may then use bare names ("page_size") or
// dotted ones ("openai.base_url"), and a bare name after a dotted one inherits
// that row's parent ("`notify.opened` / `closed`"). Headings that cover several
// keys at once ("`push_mode` / `dingtalk` / ...") are tried in order.
func parseDocumentedConfigPaths(doc string, known map[string]bool) (map[string]bool, []string) {
	var (
		documented = map[string]bool{}
		problems   []string
		inSection  bool
		prefixes   []string
		inherited  string
	)

	for i, line := range strings.Split(doc, "\n") {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "## ") {
			inSection = strings.HasPrefix(trimmed, configReferenceSection)
			prefixes = nil
			inherited = ""
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			prefixes = keySpans(trimmed)
			inherited = ""
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}

		cell := firstTableCell(trimmed)
		if isTableSeparator(cell) {
			continue
		}
		for _, name := range keySpans(cell) {
			candidates := resolveCandidates(name, inherited, prefixes)
			picked := ""
			for _, candidate := range candidates {
				if known[candidate] {
					picked = candidate
					break
				}
			}
			if picked == "" {
				problems = append(problems, fmt.Sprintf(
					"line %d: %q matches no config field (tried %s)",
					lineNo, name, strings.Join(candidates, ", ")))
				continue
			}
			documented[picked] = true
			inherited = parentPath(picked)
		}
	}

	return documented, problems
}

// resolveCandidates orders the plausible full paths for a key written in the
// reference, most specific first: a bare name continues the current row's
// parent, then any key named by the section heading, then the name itself
// (which covers top-level keys and names that are already fully qualified).
func resolveCandidates(name, inherited string, prefixes []string) []string {
	var candidates []string
	seen := map[string]bool{}
	add := func(candidate string) {
		if candidate == "" || seen[candidate] {
			return
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}

	if !strings.Contains(name, ".") && inherited != "" {
		add(joinYAMLPath(inherited, name))
	}
	if strings.Contains(name, ".") {
		add(name)
	}
	for _, prefix := range prefixes {
		add(joinYAMLPath(prefix, name))
	}
	add(name)
	return candidates
}

func parentPath(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[:i]
	}
	return ""
}

// keySpans extracts the inline-code spans that look like config keys. Values
// such as `-1`, `10m`, `0600` or `1024-65535` are ignored.
func keySpans(s string) []string {
	var keys []string
	for {
		start := strings.Index(s, "`")
		if start < 0 {
			return keys
		}
		rest := s[start+1:]
		end := strings.Index(rest, "`")
		if end < 0 {
			return keys
		}
		span := rest[:end]
		s = rest[end+1:]
		for _, part := range strings.Split(span, "/") {
			part = strings.TrimSpace(part)
			if isConfigKeyToken(part) {
				keys = append(keys, part)
			}
		}
	}
}

func isConfigKeyToken(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

func firstTableCell(row string) string {
	row = strings.TrimPrefix(strings.TrimSpace(row), "|")
	if i := strings.Index(row, "|"); i >= 0 {
		row = row[:i]
	}
	return strings.TrimSpace(row)
}

func isTableSeparator(cell string) bool {
	cell = strings.ReplaceAll(cell, " ", "")
	if cell == "" {
		return false
	}
	return strings.Trim(cell, "-:") == ""
}
