// Package symbol provides code symbol extraction using tree-sitter AST parsing.
// It extracts functions, classes, methods, structs, interfaces, variables, constants,
// types, and enums from source code in 200+ programming languages.
package symbol

import (
	"strings"
	"sync"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Symbol represents a code symbol (function, class, method, etc.) extracted from source.
type Symbol struct {
	Name    string `json:"name"`    // Symbol name (e.g., "main", "Server")
	Kind    string `json:"kind"`    // Symbol kind (e.g., "function", "class", "method")
	Line    int    `json:"line"`    // Start line (1-based)
	EndLine int    `json:"endLine"` // End line (1-based)
	Level   int    `json:"level"`   // Nesting level (1=top-level, 2=member)
}

// SymbolResult holds the result of symbol extraction for a file.
type SymbolResult struct {
	Lang    string   `json:"lang"`
	Symbols []Symbol `json:"symbols"`
}

// maxFileSize is the maximum file size (1MB) for symbol extraction.
// Larger files are skipped to avoid slow parsing.
const maxFileSize = 1 << 20

// taggerCache caches Tagger instances per language name to avoid repeated creation.
var taggerCache sync.Map // map[string]*cachedTagger

type cachedTagger struct {
	tagger *gotreesitter.Tagger
	lang   *gotreesitter.Language
}

// kindMapping maps tree-sitter tag kinds (e.g., "definition.function") to display kinds.
var kindMapping = map[string]string{
	"definition.function":    "function",
	"definition.method":      "method",
	"definition.class":       "class",
	"definition.struct":      "struct",
	"definition.interface":   "interface",
	"definition.type":        "type",
	"definition.enum":        "enum",
	"definition.variable":    "variable",
	"definition.constant":    "constant",
	"definition.module":      "module",
	"definition.namespace":   "namespace",
	"definition.field":       "field",
	"definition.property":    "property",
	"definition.constructor": "constructor",
	"definition.trait":       "trait",
	"definition.impl":        "impl",
	"definition.macro":       "macro",
}

// levelFromKind determines the nesting level based on symbol kind.
// Top-level declarations (types, classes, interfaces, enums, modules) → level 1.
// Members (functions, methods, variables, constants, fields) → level 2.
func levelFromKind(kind string) int {
	switch kind {
	case "class", "struct", "interface", "type", "enum", "module", "namespace", "trait", "impl":
		return 1
	default:
		return 2
	}
}

// getOrCreateTagger returns a cached Tagger for the given language, or creates one.
func getOrCreateTagger(entry *grammars.LangEntry) (*cachedTagger, error) {
	name := entry.Name
	if cached, ok := taggerCache.Load(name); ok {
		ct, _ := cached.(*cachedTagger)
		return ct, nil
	}

	tagsQuery := grammars.ResolveTagsQuery(*entry)
	if tagsQuery == "" {
		return nil, nil // no tags query available for this language
	}

	lang := entry.Language()
	tagger, err := gotreesitter.NewTagger(lang, tagsQuery)
	if err != nil {
		return nil, err
	}

	ct := &cachedTagger{tagger: tagger, lang: lang}
	taggerCache.Store(name, ct)
	return ct, nil
}

// ExtractSymbols extracts code symbols from the given source file content.
// filename is used for language detection via file extension.
// Returns a SymbolResult with the detected language and extracted symbols.
// If the language is not supported or the file is too large, returns an empty result.
func ExtractSymbols(filename string, content []byte) SymbolResult {
	if len(content) > maxFileSize {
		return SymbolResult{}
	}

	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		return SymbolResult{}
	}

	// Markdown has no tree-sitter tags query; use heading-based extraction instead.
	// Headings (# H1, ## H2, etc.) are treated as scope symbols for sticky scroll.
	if entry.Name == "markdown" {
		return ExtractMarkdownSymbols(content)
	}

	ct, err := getOrCreateTagger(entry)
	if err != nil || ct == nil {
		return SymbolResult{Lang: entry.Name}
	}

	tags := ct.tagger.Tag(content)
	symbols := make([]Symbol, 0, len(tags))

	// Deduplicate by start line: inferred tags queries (e.g., Go) may produce
	// multiple definition.* tags on the same line with the same range, such as
	// a function name and its return type both captured as "definition.function".
	// When multiple definitions land on the same start line, keep only the first
	// (tree-sitter returns the actual name before overlapping matches like return types).
	seenLines := make(map[int]bool, len(tags))

	for _, tag := range tags {
		// Only keep definition.* tags
		kind := tag.Kind
		if !strings.HasPrefix(kind, "definition.") {
			continue
		}

		// Map to display kind
		displayKind, ok := kindMapping[kind]
		if !ok {
			// Fallback: strip "definition." prefix
			displayKind = strings.TrimPrefix(kind, "definition.")
		}

		if tag.Name == "" {
			continue
		}

		line := int(tag.Range.StartPoint.Row) + 1
		if seenLines[line] {
			continue
		}
		seenLines[line] = true

		symbols = append(symbols, Symbol{
			Name:    tag.Name,
			Kind:    displayKind,
			Line:    line,
			EndLine: int(tag.Range.EndPoint.Row) + 1,
			Level:   levelFromKind(displayKind),
		})
	}

	return SymbolResult{
		Lang:    entry.Name,
		Symbols: symbols,
	}
}

// headingInfo is a temporary struct used during markdown heading extraction.
type headingInfo struct {
	level int    // heading depth: 1 for #, 2 for ##, etc.
	name  string // heading text
	line  int    // 1-based line number
}

// ExtractMarkdownSymbols extracts headings from markdown content as symbols.
// Each heading becomes a Symbol with kind "heading" and level = heading depth.
// endLine is computed as the line before the next heading of the same or lower level,
// or the last line of the file if no such heading follows.
func ExtractMarkdownSymbols(content []byte) SymbolResult {
	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// Phase 1: collect all headings
	var headings []headingInfo
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 2 || trimmed[0] != '#' {
			continue
		}
		// Count leading # characters (max 6)
		depth := 0
		for _, ch := range trimmed {
			if ch == '#' {
				depth++
			} else {
				break
			}
		}
		if depth > 6 || depth >= len(trimmed) {
			continue
		}
		// Must be followed by a space
		if trimmed[depth] != ' ' {
			continue
		}
		text := strings.TrimSpace(trimmed[depth:])
		if text == "" {
			continue
		}
		headings = append(headings, headingInfo{
			level: depth,
			name:  text,
			line:  i + 1, // 1-based
		})
	}

	if len(headings) == 0 {
		return SymbolResult{Lang: "markdown", Symbols: nil}
	}

	// Phase 2: compute endLine for each heading
	symbols := make([]Symbol, 0, len(headings))
	for i, h := range headings {
		endLine := totalLines
		// Find the next heading with level <= current level
		for j := i + 1; j < len(headings); j++ {
			if headings[j].level <= h.level {
				endLine = headings[j].line - 1
				break
			}
		}
		symbols = append(symbols, Symbol{
			Name:    h.name,
			Kind:    "heading",
			Line:    h.line,
			EndLine: endLine,
			Level:   h.level,
		})
	}

	return SymbolResult{
		Lang:    "markdown",
		Symbols: symbols,
	}
}
