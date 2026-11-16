package tools

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GlobTool lists files by pattern.
//
// It exists so the model can find files instead of guessing paths. Without it
// every "read the config" turn burns a round on a wrong guess, and with a
// read-only policy bash is not available to fall back on.
type GlobTool struct{}

// maxGlobResults bounds a listing. A pattern like "**/*" over a large repo
// would otherwise fill the context with paths and answer nothing.
const maxGlobResults = 300

// globSkipDirs are never descended into. They are large, generated, and
// almost never what the model is looking for.
var globSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	".venv":        true,
	"__pycache__":  true,
	"target":       true,
	".idea":        true,
}

func (GlobTool) Name() string { return "glob" }

func (GlobTool) Spec() Spec {
	return Spec{
		Name: "glob",
		Description: "Find files by name pattern. Supports ** to match across directories, " +
			"e.g. \"**/*.go\" or \"web/src/**/*.vue\". Returns paths relative to the working directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern, relative to the working directory.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

type globArgs struct {
	Pattern string `json:"pattern"`
}

func (GlobTool) Run(env Env, raw json.RawMessage) Result {
	var args globArgs
	if err := decodeArgs(raw, &args); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	pattern := strings.TrimSpace(args.Pattern)
	if pattern == "" {
		return Result{Text: "pattern is required", IsError: true}
	}

	absRoot, err := filepath.Abs(env.Root)
	if err != nil {
		return Result{Text: fmt.Sprintf("cannot resolve working directory: %v", err), IsError: true}
	}

	// The pattern's literal prefix decides where the walk starts, so a
	// pattern confined to a subdirectory does not scan the whole tree.
	base := absRoot
	if literal := literalPrefix(pattern); literal != "" {
		candidate := literal
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(absRoot, candidate)
		}
		if resolved, err := resolvePath(absRoot, candidate); err == nil {
			if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
				base = resolved
			}
		}
	}

	var matches []string
	walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			if globSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(absRoot, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if globMatch(pattern, rel) {
			matches = append(matches, rel)
			if len(matches) >= maxGlobResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return Result{Text: fmt.Sprintf("cannot list files: %v", walkErr), IsError: true}
	}

	if len(matches) == 0 {
		return Result{Text: fmt.Sprintf("No files match %q.", pattern), Display: pattern}
	}
	sort.Strings(matches)
	var b strings.Builder
	for _, m := range matches {
		b.WriteString(m)
		b.WriteString("\n")
	}
	if len(matches) >= maxGlobResults {
		fmt.Fprintf(&b, "\n[stopped after %d matches; narrow the pattern to see more]", maxGlobResults)
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), Display: pattern}
}

// literalPrefix returns the leading path segments of a pattern that contain no
// wildcard, i.e. the directory the walk can start from.
func literalPrefix(pattern string) string {
	parts := strings.Split(filepath.ToSlash(pattern), "/")
	var literal []string
	for _, p := range parts {
		if strings.ContainsAny(p, "*?[") {
			break
		}
		literal = append(literal, p)
	}
	// The last literal segment is the file name itself, not a directory.
	if len(literal) > 0 && !strings.HasSuffix(pattern, "/") {
		literal = literal[:len(literal)-1]
	}
	return strings.Join(literal, "/")
}

// globMatch matches a slash-separated path against a pattern with ** support.
//
// filepath.Match has no ** semantics, and ** is exactly what callers need
// ("find every .go under internal/"). The implementation is a small recursive
// matcher rather than a regexp so a pattern with many segments cannot build a
// pathological expression.
func globMatch(pattern, path string) bool {
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pat, path []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// "**" matches zero or more path segments.
			rest := pat[1:]
			for i := 0; i <= len(path); i++ {
				if matchSegments(rest, path[i:]) {
					return true
				}
			}
			return false
		}
		if len(path) == 0 {
			return false
		}
		ok, err := filepath.Match(pat[0], path[0])
		if err != nil || !ok {
			return false
		}
		pat, path = pat[1:], path[1:]
	}
	return len(path) == 0
}
