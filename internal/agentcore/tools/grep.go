package tools

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// GrepTool searches file contents by regular expression.
type GrepTool struct{}

const (
	// maxGrepMatches bounds total hits across all files.
	maxGrepMatches = 200
	// maxGrepLineLen keeps one pathological line from filling the context.
	maxGrepLineLen = 400
	// maxGrepFileBytes skips files too large to be source.
	maxGrepFileBytes = 4 * 1024 * 1024
)

func (GrepTool) Name() string { return "grep" }

func (GrepTool) Spec() Spec {
	return Spec{
		Name: "grep",
		Description: "Search file contents with a regular expression (RE2 syntax). " +
			"Returns matching lines with their file and line number. " +
			"Use include to restrict to certain file names, e.g. \"*.go\".",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regular expression to search for.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "File or directory to search. Defaults to the whole working directory.",
				},
				"include": map[string]any{
					"type":        "string",
					"description": "Only search files whose base name matches this glob, e.g. \"*.go\".",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Include string `json:"include"`
}

func (GrepTool) Run(env Env, raw json.RawMessage) Result {
	var args grepArgs
	if err := decodeArgs(raw, &args); err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return Result{Text: "pattern is required", IsError: true}
	}
	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return Result{Text: fmt.Sprintf("invalid regular expression: %v", err), IsError: true}
	}

	absRoot, err := filepath.Abs(env.Root)
	if err != nil {
		return Result{Text: fmt.Sprintf("cannot resolve working directory: %v", err), IsError: true}
	}

	target := absRoot
	if strings.TrimSpace(args.Path) != "" {
		target, err = resolvePath(absRoot, args.Path)
		if err != nil {
			return Result{Text: err.Error(), IsError: true}
		}
	}

	info, statErr := os.Stat(target)
	if statErr != nil {
		return Result{Text: fmt.Sprintf("cannot access %s: %v", args.Path, statErr), IsError: true}
	}

	var files []string
	if info.IsDir() {
		walkErr := filepath.WalkDir(target, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if globSkipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if args.Include != "" {
				if ok, matchErr := filepath.Match(args.Include, d.Name()); matchErr != nil || !ok {
					return nil
				}
			}
			files = append(files, path)
			return nil
		})
		if walkErr != nil {
			return Result{Text: fmt.Sprintf("cannot search: %v", walkErr), IsError: true}
		}
	} else {
		files = []string{target}
	}
	sort.Strings(files)

	var (
		lines   []string
		hits    int
		scanned int
	)
	for _, f := range files {
		if hits >= maxGrepMatches {
			break
		}
		if fi, statErr := os.Stat(f); statErr != nil || fi.Size() > maxGrepFileBytes {
			continue
		}
		if isLikelyBinary(f) {
			continue
		}
		scanned++
		hits += grepFile(f, absRoot, re, &lines)
	}

	if len(lines) == 0 {
		return Result{
			Text:    fmt.Sprintf("No matches for %q in %d file(s).", args.Pattern, scanned),
			Display: args.Pattern,
		}
	}
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if hits >= maxGrepMatches {
		fmt.Fprintf(&b, "\n[stopped after %d matches; narrow the pattern or path]", maxGrepMatches)
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), Display: args.Pattern}
}

// grepFile appends formatted matches for one file and returns how many it
// found. The caller owns the total cap so it can stop the outer loop.
func grepFile(path, root string, re *regexp.Regexp, out *[]string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()

	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxGrepLineLen*4)
	count := 0
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !re.MatchString(line) {
			continue
		}
		if len(line) > maxGrepLineLen {
			line = line[:maxGrepLineLen] + "…"
		}
		*out = append(*out, fmt.Sprintf("%s:%d: %s", rel, lineNo, line))
		count++
		if count >= maxGrepMatches {
			break
		}
	}
	return count
}
