// Package tools implements the four tools the built-in agent can use.
//
// Every tool takes an absolute-or-relative path that MUST resolve inside the
// turn's working directory; see resolvePath. That confinement is enforced here,
// once, rather than in each tool, because a tool that forgets it is not a
// smaller bug than the others — it is arbitrary filesystem access.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Limits applied to tool input/output. They exist so one tool call cannot
// consume the whole context window or the whole turn budget.
const (
	// MaxReadBytes caps a single read.
	MaxReadBytes = 256 * 1024
	// MaxReadLines caps a single read by line count.
	MaxReadLines = 2000
	// MaxWriteBytes caps a single write.
	MaxWriteBytes = 1024 * 1024
	// MaxImageBytes caps an image handed to the model. Anything larger is
	// rejected with an explanation rather than silently truncated: a truncated
	// image is unreadable, not partially readable.
	MaxImageBytes = 8 * 1024 * 1024
	// MaxBashOutputBytes caps captured command output (head and tail kept).
	MaxBashOutputBytes = 30 * 1024
)

// Result is the outcome of one tool call.
type Result struct {
	// Text is what the model reads. Always set, even on failure, so the model
	// can correct itself instead of guessing why nothing happened.
	Text string
	// Image is an optional inline image (a read of a PNG/JPG). Only set by
	// tools that can return binary content the model should actually see.
	Image *Image
	// IsError marks a failure. The text still reaches the model.
	IsError bool
	// Display is a short one-line summary for the tool card, e.g. the path.
	// Empty means "derive from the input".
	Display string
}

// Image is an inline image result.
type Image struct {
	MediaType string
	Data      []byte
}

// Env is what a tool is allowed to touch. It is constructed per turn from the
// agent's policy, so a tool cannot widen its own reach.
type Env struct {
	// Root is the working directory. Every path must resolve inside it.
	Root string
	// CommandTimeoutSec bounds one shell command.
	CommandTimeoutSec int
	// Ctx is the turn's context. A tool that spawns a process must honour it,
	// otherwise the user's cancel button stops the loop but leaves the command
	// running.
	Ctx context.Context
}

// Tool is one callable tool.
type Tool interface {
	// Name is the identifier the model uses. It must match the spec.
	Name() string
	// Spec is the declaration sent to the model.
	Spec() Spec
	// Run executes one call with raw JSON arguments.
	Run(env Env, args json.RawMessage) Result
}

// Spec is a tool declaration.
type Spec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// resolvePath turns a caller-supplied path into an absolute path that is
// guaranteed to be inside root.
//
// It deliberately does NOT use filepath-securejoin (an indirect dependency
// today): the check needed here is "after cleaning, is this still under root",
// and doing it explicitly keeps the failure mode visible in this file instead
// of behind a library call.
//
// A symlink escaping root is caught by resolving the DEEPEST EXISTING ancestor
// and re-checking containment: a write to root/link/new.txt where link points
// at /etc must fail even though the final component does not exist yet.
func resolvePath(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path is required")
	}
	if root == "" {
		return "", fmt.Errorf("no working directory is set for this turn")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	candidate := p
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absRoot, candidate)
	}
	candidate = filepath.Clean(candidate)

	if !isWithin(absRoot, candidate) {
		return "", fmt.Errorf("path %q is outside the working directory (%s)", p, absRoot)
	}

	// Re-check after following the deepest existing ancestor's symlinks.
	resolved, err := resolveExistingPrefix(candidate)
	if err != nil {
		return "", err
	}
	if !isWithin(absRoot, resolved) {
		return "", fmt.Errorf("path %q resolves outside the working directory (to %s)", p, resolved)
	}
	return candidate, nil
}

// isWithin reports whether child is root or inside it.
func isWithin(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	// ".." or a leading ".."+separator means the child escaped.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveExistingPrefix follows symlinks on the deepest existing ancestor of p
// and re-appends the non-existent remainder.
//
// This is what makes a write through a symlinked directory safe: p itself does
// not exist yet, but the directory it would be created in does, and that
// directory may be a link pointing outside root.
func resolveExistingPrefix(p string) (string, error) {
	var missing []string
	cur := p
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			// Rebuild the tail onto the resolved ancestor.
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve path: %w", err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached the filesystem root without finding an existing ancestor.
			return p, nil
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}

// displayPath renders a path relative to root for the tool card, falling back
// to the absolute path when it is the root itself.
func displayPath(root, abs string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." {
		return abs
	}
	return rel
}

// decodeArgs unmarshals tool arguments, tolerating an empty payload.
func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return fmt.Errorf("no arguments were provided")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("arguments are not valid JSON for this tool: %w", err)
	}
	return nil
}

// All returns the tools enabled by a capability set, in a stable order.
//
// The mapping lives here so "which capability enables which tool" has exactly
// one definition. A caller that granted exec but forgot to register Bash would
// otherwise produce an agent that thinks it can run commands and cannot.
func All(fsRead, fsWrite, exec bool) []Tool {
	var out []Tool
	if fsRead {
		out = append(out, ReadTool{}, GlobTool{}, GrepTool{})
	}
	if fsWrite {
		out = append(out, WriteTool{}, EditTool{})
	}
	if exec {
		out = append(out, BashTool{})
	}
	return out
}
