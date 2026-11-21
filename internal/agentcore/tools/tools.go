// Package tools implements the four tools the built-in agent can use.
//
// Every tool takes an absolute-or-relative path, and WHETHER it may leave the
// turn's working directory is decided in one place (Env.ConfineToWorkDir; see
// resolvePath and resolveAnywhere). The check lives here rather than in each
// tool because a tool that forgets it is not a smaller bug than the others —
// it is arbitrary filesystem access.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// JSON Schema vocabulary shared by every Spec(). They are literal keyword
// names, not tunables: keeping them here is what stops five copies of "type"
// from drifting apart.
const (
	schemaType        = "type"
	schemaProperties  = "properties"
	schemaDescription = "description"
	schemaRequired    = "required"

	schemaObject  = "object"
	schemaString  = "string"
	schemaInteger = "integer"
	schemaBoolean = "boolean"
)

// Tool names, and the argument names that recur across specs. A name is the
// identifier the model calls AND the one the spec declares; keeping the two in
// one constant is what stops them from drifting apart.
//
// toolReadName is shared: ReadTool and ReadToolNoVision publish under the same
// identifier.
const (
	toolReadName   = "read"
	toolWriteName  = "write"
	toolEditName   = "edit"
	toolGlobName   = "glob"
	toolGrepName   = "grep"
	toolBashName   = "bash"
	argPath        = "path"
	argPattern     = "pattern"
	argCommand     = "command"
	argContent     = "content"
	argDescription = "description"
	argOffset      = "offset"
	argLimit       = "limit"
)

// argPathDescription documents the shared `path` argument: four specs resolve
// it identically (see Env.resolve), so one rephrasing would make one tool lie
// about the others.
const argPathDescription = "File path, absolute or relative to the working directory."

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
	// Root is the working directory. It is the base for relative paths, and —
	// when ConfineToWorkDir is set — the fence every path must stay inside.
	Root string
	// ConfineToWorkDir refuses any path that resolves outside Root.
	//
	// The zero value (false) lets the file tools reach anywhere the process
	// can: absolute paths, ".." outside the project, symlinks pointing out.
	// That is the default because the project directory is a starting point,
	// not a fence — and an agent that is refused spends rounds hunting for a
	// way around the refusal instead of doing the task.
	//
	// true is the sandbox: use it when the agent runs on a machine with files
	// the user does not want read. It covers the FILE TOOLS only; a shell can
	// read the whole disk whatever this says.
	ConfineToWorkDir bool
	// CommandTimeoutSec bounds one shell command.
	CommandTimeoutSec int
	// Ctx is the turn's context. A tool that spawns a process must honor it,
	// otherwise the user's cancel button stops the loop but leaves the command
	// running.
	Ctx context.Context
	// CallID is the id of the call being executed. Most tools ignore it; a
	// tool that spawns sub-work uses it to attribute that work to this call.
	CallID string
}

// Tool is one callable tool.
type Tool interface {
	// Name is the identifier the model uses. It must match the spec.
	Name() string
	// Snippet is a one-line "what is this for" shown in the system prompt's
	// tool list.
	//
	// It is deliberately separate from Spec().Description: the prompt lists
	// every tool together, so the model needs a compact phrase to CHOOSE
	// between them, while the schema description can afford detail about one.
	// Listing bare names (as the first version did) tells the model nothing
	// about which to reach for, and it then shells out to bash for things a
	// dedicated tool does better.
	Snippet() string
	// Guidelines are usage rules for this tool, rendered into the prompt's
	// rules section.
	//
	// They live here rather than in a central table so a tool's advice cannot
	// drift away from the tool. Most tools return nil; the ones with a real
	// footgun (reading through the shell, rewriting a whole file for a
	// one-line change) return one line each.
	Guidelines() []string
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

// resolve turns a caller-supplied path into an absolute path, applying this
// turn's confinement rule.
//
// Every tool goes through here rather than calling resolvePath directly, so the
// "may it leave the working directory" decision has exactly one implementation
// and cannot drift between tools.
func (e Env) resolve(p string) (string, error) {
	if e.ConfineToWorkDir {
		return resolvePath(e.Root, p)
	}
	return resolveAnywhere(e.Root, p)
}

// resolvePath turns a caller-supplied path into an absolute path that is
// guaranteed to be inside root.
//
// It deliberately does NOT use filepath-securejoin (an indirect dependency
// today): the check needed here is "after cleaning, is this still under root",
// and doing it explicitly keeps the failure mode visible in this file instead
// of behind a library call.
//
// Both the root AND the candidate are compared in their symlink-resolved form.
// Comparing a raw root against a resolved candidate is what broke the whole
// sandbox when the working directory itself is a link (macOS "/tmp",
// "~/work/proj -> /data/proj"): every path then resolved to somewhere outside
// the raw root string, so read/write/edit/glob/grep all failed with "outside the
// working directory" and nothing the user could do made it work.
//
// The escape check is unaffected: a symlink escaping root is caught by resolving
// the DEEPEST EXISTING ancestor and re-checking containment against the resolved
// root — a write to root/link/new.txt where link points at /etc still fails even
// though the final component does not exist yet.
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
	// Fall back to the raw absolute path when the root does not exist (yet):
	// a missing root then fails at the tool's own stat/open with a message about
	// the file, instead of a containment error that is not the real problem.
	realRoot := absRoot
	if resolved, evalErr := filepath.EvalSymlinks(absRoot); evalErr == nil {
		realRoot = resolved
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
	if !isWithin(realRoot, resolved) {
		return "", fmt.Errorf("path %q resolves outside the working directory (to %s)", p, resolved)
	}
	return candidate, nil
}

// resolveAnywhere is resolvePath without the fence: the path is still made
// absolute (relative paths stay relative to the working directory, so "src/x.go"
// means the same thing either way) and cleaned, but containment is not checked.
//
// The containment re-check is skipped too, which is the point: a symlink inside
// the project that points at another directory is a legitimate layout once the
// fence is off.
func resolveAnywhere(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if root == "" {
		return "", fmt.Errorf("no working directory is set for this turn, so a relative path cannot be resolved")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Clean(filepath.Join(absRoot, p)), nil
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
