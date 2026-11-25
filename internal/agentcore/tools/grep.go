package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"clawbench/internal/symwalk"
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

func (GrepTool) Name() string { return toolGrepName }

func (GrepTool) Snippet() string { return "Search file contents" }

func (GrepTool) Guidelines() []string {
	return []string{"Use grep to find where something is defined or used, then read only the matching files."}
}

func (GrepTool) Spec() Spec {
	return Spec{
		Name: toolGrepName,
		Description: "Search file contents with a regular expression (RE2 syntax). " +
			"Returns matching lines with their file and line number. " +
			"Use include to restrict to certain file names, e.g. \"*.go\".",
		Parameters: map[string]any{
			schemaType: schemaObject,
			schemaProperties: map[string]any{
				argPattern: map[string]any{
					schemaType:        schemaString,
					schemaDescription: "Regular expression to search for.",
				},
				argPath: map[string]any{
					schemaType:        schemaString,
					schemaDescription: "File or directory to search. Defaults to the whole working directory.",
				},
				"include": map[string]any{
					schemaType:        schemaString,
					schemaDescription: "Only search files whose base name matches this glob, e.g. \"*.go\".",
				},
			},
			schemaRequired: []string{argPattern},
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
		target, err = env.resolve(args.Path)
		if err != nil {
			return Result{Text: err.Error(), IsError: true}
		}
	}

	info, statErr := os.Stat(target)
	if statErr != nil {
		return Result{Text: fmt.Sprintf("cannot access %s: %v", args.Path, statErr), IsError: true}
	}

	files, err := grepTargets(env, target, info, args.Include)
	if err != nil {
		return Result{Text: err.Error(), IsError: true}
	}
	sort.Strings(files)

	lines, hits, scanned := grepFiles(files, absRoot, re)

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

// grepTargets lists the files to search: the target itself, or every file
// under it when it is a directory, filtered by the include glob.
//
// Symlinks are followed (see symwalk), so content that only exists on the other
// side of a project symlink is searchable. Whether a linked directory may be
// entered is the turn's confinement rule, carried by env.walkOptions.
func grepTargets(env Env, target string, info os.FileInfo, include string) ([]string, error) {
	if !info.IsDir() {
		return []string{target}, nil
	}
	var files []string
	outcome := symwalk.Walk(context.Background(), target, env.walkOptions(target),
		func(e symwalk.Entry) symwalk.Result {
			return collectGrepFile(e, include, &files)
		})
	if outcome.RootError != nil {
		return nil, fmt.Errorf("cannot search: %w", outcome.RootError)
	}
	return files, nil
}

// collectGrepFile is the walk visitor behind grepTargets: it adds the entry to
// files when it passes the directory skip list and the include filter.
//
// A malformed include pattern matches nothing rather than failing: answering
// nothing at all would be worse than answering nothing for that pattern.
func collectGrepFile(e symwalk.Entry, include string, files *[]string) symwalk.Result {
	if e.Blocked {
		// A directory the turn is not allowed to enter. Adding it as a "file"
		// would only produce a read error dressed up as a scanned file.
		return symwalk.Continue
	}
	if e.IsDir {
		if globSkipDirs[e.Name] {
			return symwalk.SkipDir
		}
		return symwalk.Continue
	}
	if include != "" {
		ok, err := filepath.Match(include, e.Name)
		if err != nil || !ok {
			return symwalk.Continue
		}
	}
	*files = append(*files, e.Path)
	return symwalk.Continue
}

// grepFiles scans candidates until the match cap is reached and reports the
// formatted lines, the match count and how many files were actually read.
func grepFiles(files []string, absRoot string, re *regexp.Regexp) (lines []string, hits, scanned int) {
	for _, f := range files {
		if hits >= maxGrepMatches {
			break
		}
		fi, statErr := os.Stat(f)
		if statErr != nil || fi.Size() > maxGrepFileBytes {
			continue
		}
		if isLikelyBinary(f) {
			continue
		}
		scanned++
		hits += grepFile(f, absRoot, re, &lines)
	}
	return lines, hits, scanned
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
	// A search that left the working directory has no meaningful relative
	// form — "../../.." repeated per line is noise. Absolute is the readable
	// answer there.
	if strings.HasPrefix(rel, "../") || rel == ".." {
		rel = filepath.ToSlash(path)
	}

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
