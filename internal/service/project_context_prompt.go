package service

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/model"
)

// project_context_prompt.go injects a project's context file (AGENTS.md and
// friends) into an agent's system prompt.
//
// Why this exists separately from session_runtime.go's projectContext: that one
// feeds the RECOMMENDATION model a bounded (4 KiB), first-hit-only taste of the
// project, and stacks nothing. This one feeds the agent that will actually do
// the work, so it follows what the CLIs themselves do — walk from the project
// root down to the working directory, one file per directory, layered into a
// single section.
//
// The built-in agent is the ONLY backend that needs this: every external
// backend reads its own AGENTS.md (pi reads it unless --no-context-files, which
// we pass precisely because ClawBench injects its own rules instead). This
// agent has no process and no discovery of its own, so without this section a
// project's instructions are invisible to it — the user writes AGENTS.md, the
// other agents obey it, and this one silently does not.

// contextFileCandidates is the chain checked in each directory, in priority
// order. AGENTS.override.md wins over AGENTS.md (the same override convention
// pi and others use), because an override file exists to be the answer.
//
// The uppercase spellings are not decoration: on a case-sensitive filesystem
// (Linux) "AGENTS.MD" is a DIFFERENT file, and a repository that committed one
// would lose its rules silently — the worst failure mode for an injected
// prompt, because nothing in the UI says a file was expected. pi carries them
// for the same reason.
var contextFileCandidates = []string{
	"AGENTS.override.md", "AGENTS.md", "AGENTS.MD",
	"CLAUDE.md", "CLAUDE.MD",
	"CODEBUDDY.md", "CODEBUDDY.MD",
}

// contextFileMaxBytes caps ONE injected file. Unlike the recommendation's cap
// this one is generous: this text is the project's rules, and a truncated rule
// set ("never touch internal/... except") is worse than an expensive one.
const contextFileMaxBytes = 32 * 1024

// resolveContextWorkDir picks the directory the context chain walks down to.
//
// workDir when set, otherwise projectPath. It is capped at the project: a
// working directory outside the project (or none at all) would walk the chain
// from an unrelated place and inject someone else's rules.
func resolveContextWorkDir(projectPath, workDir string) string {
	if projectPath == "" {
		return ""
	}
	if workDir == "" {
		return projectPath
	}
	rel, err := filepath.Rel(projectPath, workDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return projectPath
	}
	return workDir
}

// AppendProjectContextSection appends the project's context files to a system
// prompt. Returns the input unchanged when the project has none.
//
// Files are stacked root-first (outermost rules first, the most specific ones
// last) and each is tagged with its path so the model can tell which file a
// rule came from and can re-read it. A file that is missing, unreadable, empty
// or huge is skipped rather than aborting: one bad file must not cost the agent
// the rest of its project instructions.
func AppendProjectContextSection(systemPrompt, projectPath, workDir string) string {
	section := buildProjectContextSection(projectPath, workDir)
	if section == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return section
	}
	return systemPrompt + "\n\n" + section
}

// buildProjectContextSection renders the <project_context> section, or "".
func buildProjectContextSection(projectPath, workDir string) string {
	dir := resolveContextWorkDir(projectPath, workDir)
	if dir == "" {
		return ""
	}

	files := contextFilesUpTo(dir)
	if len(files) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<project_context>\n")
	b.WriteString("Project instructions loaded from the files below. They are the user's rules for this ")
	b.WriteString("project and outrank the mechanical guidance in this prompt. Re-read a file when you need ")
	b.WriteString("the part that was left out.\n\n")
	for _, f := range files {
		b.WriteString(`<project_instructions path="`)
		b.WriteString(strings.ReplaceAll(f.path, `\`, "/"))
		b.WriteString(`">` + "\n")
		b.WriteString(f.content)
		if f.truncated {
			b.WriteString("\n[... file truncated at 32 KiB; read the file for the rest ...]")
		}
		b.WriteString("\n</project_instructions>\n\n")
	}
	b.WriteString("</project_context>")
	return b.String()
}

// contextFile is one loaded file.
type contextFile struct {
	path      string
	content   string
	truncated bool
}

// contextFilesUpTo collects the chain from the filesystem root's child down to
// dir, root-first. Intermediate directories are included — that is how a rule
// set scoped to a monorepo package ("only apps/web uses RTL") reaches a session
// started inside that package.
//
// A linked git worktree is the exception. Its working tree usually sits INSIDE
// the main repository (`.claude/worktrees/<name>`, `.worktrees/<name>`), so a
// plain ancestor walk delivers the main repo's AGENTS.md TWICE: once from the
// main repo root, once from the worktree root — and the worktree's copy is
// often that same file through a relative symlink. Both occupy one logical
// repository scope, so only the worktree's own copy is kept; that is what pi's
// findShadowedContextFile does, and what stops the model from reading one rule
// set as two (duplicated rules read as emphasis).
func contextFilesUpTo(dir string) []contextFile {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}

	shadowed := shadowedContextPath(dir)

	var reversed []contextFile
	seen := make(map[string]bool, 4)
	cur := filepath.Clean(dir)
	for {
		if f, ok := loadContextFile(cur); ok && !seen[f.path] && !isShadowedCopy(cur, f.path, shadowed) {
			seen[f.path] = true
			reversed = append(reversed, f)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	// Reverse in place: collected leaf-first, injected root-first.
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

// shadowedContextPath returns the main repository's context file that a linked
// worktree's own copy shadows, or "" when nothing is shadowed.
//
// Only a worktree whose root is INSIDE the main repository worktree is
// considered (`git worktree add ../feat` puts it as a sibling, where no
// ancestor walk would pick the main repo's file up anyway).
func shadowedContextPath(dir string) string {
	layout := DetectRepoLayout(dir)
	if layout.Kind != RepoKindWorktree {
		return ""
	}
	// mainWorktreeRoot handles the `--separate-git-dir` / bare shapes where the
	// common dir is not `<root>/.git`; deriving the root by stripping ".git"
	// here would get those wrong.
	mainRoot := mainWorktreeRoot(layout.CommonDir)
	if mainRoot == "" || mainRoot == layout.Root {
		return ""
	}
	rel, err := filepath.Rel(mainRoot, layout.Root)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	f, ok := loadContextFile(layout.Root)
	if !ok {
		return ""
	}
	// The MAIN root side gets the same treatment as the worktree side in
	// isShadowedCopy: resolve the directory, keep the file name as written.
	return filepath.Join(canonicalize(mainRoot), filepath.Base(f.path))
}

// isShadowedCopy reports whether the file at dir/name is the main repo's context
// file seen through the worktree.
//
// The comparison deliberately does NOT resolve the file: a worktree whose
// AGENTS.md is a symlink to the main repo's file would resolve onto the very
// path being shadowed, so both copies would look identical and both would be
// dropped — leaving the agent with no rules at all. Only the DIRECTORY is
// canonicalized (so a symlinked checkout still matches), and the file name is
// appended verbatim.
func isShadowedCopy(dir string, loadedPath string, shadowed string) bool {
	if shadowed == "" {
		return false
	}
	candidate := filepath.Join(canonicalize(dir), filepath.Base(loadedPath))
	return filepath.Clean(candidate) == filepath.Clean(shadowed)
}

// loadContextFile returns the first candidate file in dir that is a readable,
// non-empty regular file.
func loadContextFile(dir string) (contextFile, bool) {
	for _, name := range contextFileCandidates {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("project context: unreadable file, skipping",
				slog.String("path", path), slog.String("err", err.Error()))
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
		if text == "" {
			continue
		}
		truncated := false
		if len(text) > contextFileMaxBytes {
			text = strings.ToValidUTF8(text[:contextFileMaxBytes], "")
			truncated = true
		}
		return contextFile{path: path, content: text, truncated: truncated}, true
	}
	return contextFile{}, false
}

// agentSkipsProjectContext reports whether the user turned context-file
// injection off for this agent.
//
// Read from the live registry on every turn (not cached) so the settings
// toggle applies to the next message rather than the next restart. An unknown
// agent injects nothing anyway — the caller gates on the backend first — so the
// default of "do not skip" costs nothing here.
func agentSkipsProjectContext(agentID string) bool {
	agent := model.GetAgent(agentID)
	return agent != nil && agent.SkipProjectContext
}
