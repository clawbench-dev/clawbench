package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// writeContextFile creates a file with content under dir.
func writeContextFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// TestAppendProjectContextSection_LoadsAgentsMD pins the basic contract: the
// project's AGENTS.md reaches the system prompt, tagged with its path so the
// model knows which file a rule came from.
func TestAppendProjectContextSection_LoadsAgentsMD(t *testing.T) {
	root := t.TempDir()
	src := writeContextFile(t, root, "AGENTS.md", "Use tabs. Never touch vendor/.")

	got := AppendProjectContextSection("BASE", root, "")

	// The tag carries the path AS THE LOADER SAW IT, which is the resolved one:
	// on macOS a temp dir is under /var, a symlink to /private/var, so the raw
	// t.TempDir() path does not appear in the prompt at all.
	resolved, err := filepath.EvalSymlinks(src)
	require.NoError(t, err)
	assert.Contains(t, got, "BASE", "the caller's prompt must survive")
	assert.Contains(t, got, "<project_context>")
	assert.Contains(t, got, `<project_instructions path="`+filepath.ToSlash(resolved)+`">`)
	assert.Contains(t, got, "Use tabs. Never touch vendor/.")
	assert.Contains(t, got, "</project_context>")
}

// TestAppendProjectContextSection_NothingToInject pins that a project without
// any context file leaves the prompt byte-identical — no empty tag for the
// model to interpret.
func TestAppendProjectContextSection_NothingToInject(t *testing.T) {
	root := t.TempDir()
	assert.Equal(t, "BASE", AppendProjectContextSection("BASE", root, ""))
	assert.Equal(t, "", AppendProjectContextSection("", root, ""))
	assert.Equal(t, "BASE", AppendProjectContextSection("BASE", "", ""))
}

// TestAppendProjectContextSection_CandidatePriority pins the chain: AGENTS.md
// wins over CLAUDE.md, and AGENTS.override.md wins over AGENTS.md. One file per
// directory — stacking both AGENTS.md and CLAUDE.md in the same directory would
// duplicate the same rules.
func TestAppendProjectContextSection_CandidatePriority(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "CLAUDE.md", "CLAUDE-RULES")
	writeContextFile(t, root, "AGENTS.md", "AGENTS-RULES")

	got := buildProjectContextSection(root, "")
	assert.Contains(t, got, "AGENTS-RULES")
	assert.NotContains(t, got, "CLAUDE-RULES")

	writeContextFile(t, root, "AGENTS.override.md", "OVERRIDE-RULES")
	got = buildProjectContextSection(root, "")
	assert.Contains(t, got, "OVERRIDE-RULES")
	assert.NotContains(t, got, "AGENTS-RULES", "the override replaces AGENTS.md in the same dir")
}

// TestAppendProjectContextSection_AncestorsRootFirst pins the layering that
// makes a monorepo work: the rules of every directory between the working
// directory and the filesystem root are stacked, outermost first, so the most
// specific rules come last.
func TestAppendProjectContextSection_AncestorsRootFirst(t *testing.T) {
	outer := t.TempDir()
	writeContextFile(t, outer, "AGENTS.md", "OUTER-RULES")
	nested := filepath.Join(outer, "apps", "web")
	writeContextFile(t, nested, "AGENTS.md", "NESTED-RULES")

	got := buildProjectContextSection(nested, "")

	require.Contains(t, got, "OUTER-RULES")
	require.Contains(t, got, "NESTED-RULES")
	assert.Less(t, strings.Index(got, "OUTER-RULES"), strings.Index(got, "NESTED-RULES"),
		"ancestor rules must come before the more specific ones")
}

// TestAppendProjectContextSection_WorkDirDeeperThanProject pins that a session
// started in a subdirectory sees the subdirectory's rules as well as the
// project's.
func TestAppendProjectContextSection_WorkDirDeeperThanProject(t *testing.T) {
	project := t.TempDir()
	writeContextFile(t, project, "AGENTS.md", "PROJECT-RULES")
	sub := filepath.Join(project, "pkg", "api")
	writeContextFile(t, sub, "AGENTS.md", "SUBPKG-RULES")

	got := AppendProjectContextSection("", project, sub)

	assert.Contains(t, got, "PROJECT-RULES")
	assert.Contains(t, got, "SUBPKG-RULES")
}

// TestAppendProjectContextSection_WorkDirOutsideProjectFallsBack pins the
// boundary: a working directory that is not inside the project must not walk
// the chain from somewhere unrelated (that would inject another project's
// rules, or the user's home directory rules).
func TestAppendProjectContextSection_WorkDirOutsideProjectFallsBack(t *testing.T) {
	project := t.TempDir()
	writeContextFile(t, project, "AGENTS.md", "PROJECT-RULES")
	outside := t.TempDir()
	writeContextFile(t, outside, "AGENTS.md", "SOMEONE-ELSE-RULES")

	got := AppendProjectContextSection("", project, outside)

	assert.Contains(t, got, "PROJECT-RULES")
	assert.NotContains(t, got, "SOMEONE-ELSE-RULES")
}

// TestAppendProjectContextSection_SkipsEmptyAndHuge pins the two failure modes
// that must not abort the section: an empty file is skipped (an empty tag reads
// as "no rules"), and an oversized one is truncated with a marker rather than
// silently cut.
func TestAppendProjectContextSection_SkipsEmptyAndHuge(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "AGENTS.md", "   \n\t\n")
	assert.Equal(t, "", buildProjectContextSection(root, ""),
		"a whitespace-only file injects nothing")

	big := "HEAD-MARKER\n" + strings.Repeat("x", contextFileMaxBytes)
	writeContextFile(t, root, "AGENTS.md", big)
	got := buildProjectContextSection(root, "")
	assert.Contains(t, got, "HEAD-MARKER")
	assert.Contains(t, got, "truncated")
}

// TestAppendProjectContextSection_StripsBOM pins that a BOM-prefixed file (a
// Windows editor's default) does not inject an invisible character before the
// first rule.
func TestAppendProjectContextSection_StripsBOM(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "AGENTS.md", "\ufeffFIRST-RULE")
	got := buildProjectContextSection(root, "")
	assert.Contains(t, got, ">", "has a tag")
	assert.NotContains(t, got, "\ufeff")
	assert.Contains(t, got, "FIRST-RULE")
}

// TestProjectContextSectionWiredIntoBothProducers mirrors the skills guard: a
// behavior test can only reach the chat-turn path, so the scheduled-task call
// site (which builds its request directly) is asserted from the source. Without
// it, a task would run the built-in agent with no project rules while a chat
// turn in the same project had them.
func TestProjectContextSectionWiredIntoBothProducers(t *testing.T) {
	call := regexp.MustCompile(`AppendProjectContextSection\(systemPrompt,\s*projectPath,\s*\w+\)`)
	cases := []struct {
		file string
		why  string
	}{
		{"chat_request.go", "direct sends, queue drain and push all build the request here"},
		{"scheduler.go", "scheduled tasks build the request directly and would otherwise miss the rules"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(tc.file)
			require.NoError(t, err)
			assert.Regexp(t, call, string(src),
				"%s must inject the project context (%s)", tc.file, tc.why)
			assert.Regexp(t, regexp.MustCompile(`isBuiltinBackend\(`), string(src),
				"%s must gate the injection on the built-in backend", tc.file)
		})
	}
}

// TestProjectContextSection_WiredIntoBuiltinSystemPrompt pins the wiring: the
// built-in agent's turn must carry the project rules, and an external backend's
// turn must NOT (it discovers context files itself; injecting them twice would
// duplicate the rules in its prompt).
func TestProjectContextSection_WiredIntoBuiltinSystemPrompt(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	project := t.TempDir()
	writeContextFile(t, project, "AGENTS.md", "PROJECT-RULE-MARKER")

	withAgents(t, map[string]*model.Agent{
		"b1": {ID: "b1", Backend: "builtin", RuntimeSystemPrompt: "B1"},
		"c1": {ID: "c1", Backend: "codebuddy", RuntimeSystemPrompt: "C1"},
	})

	builtinReq := BuildChatRequest("hi", "sess-builtin", project, "builtin", "b1", "", "", "", project, false)
	assert.Contains(t, builtinReq.SystemPrompt, "PROJECT-RULE-MARKER",
		"the built-in agent has no discovery of its own, so it must be injected")

	cliReq := BuildChatRequest("hi", "sess-cli", project, "codebuddy", "c1", "", "", "", project, false)
	assert.NotContains(t, cliReq.SystemPrompt, "PROJECT-RULE-MARKER",
		"external backends read their own AGENTS.md")
}

// TestProjectContextSection_Placeholder confirms the section does not
// accidentally depend on {{PROJECT_PATH}} substitution ordering: it reads the
// file itself, so it is correct on every turn.
func TestProjectContextSection_DirectPath(t *testing.T) {
	root := t.TempDir()
	src := writeContextFile(t, root, "AGENTS.md", fmt.Sprintf("path is %s", filepath.ToSlash(root)))
	got := buildProjectContextSection(root, "")
	// The tag carries the path as the LOADER saw it, i.e. resolved: on Windows a
	// temp dir is a short-name path (RUNNER~1), on macOS /var is a link to
	// /private/var, so the raw t.TempDir() spelling is not what appears.
	resolved, err := filepath.EvalSymlinks(src)
	require.NoError(t, err)
	assert.Contains(t, got, filepath.ToSlash(resolved))
}

// TestAppendProjectContextSection_UppercaseSpelling pins the .MD variants.
// On a case-sensitive filesystem "AGENTS.MD" is a different file from
// "AGENTS.md": a repository that committed only the uppercase spelling would
// otherwise lose its rules with nothing in the UI to explain why.
func TestAppendProjectContextSection_UppercaseSpelling(t *testing.T) {
	for _, name := range []string{"AGENTS.MD", "CLAUDE.MD", "AGENTS.override.md"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeContextFile(t, root, name, "UPPER-RULES")
			assert.Contains(t, buildProjectContextSection(root, ""), "UPPER-RULES")
		})
	}
}

// TestAppendProjectContextSection_LowercaseWinsOverUppercase pins the order
// inside a directory: when both spellings exist, the canonical lowercase name
// is the one read (they are two files, so picking is required).
func TestAppendProjectContextSection_LowercaseWinsOverUppercase(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "AGENTS.MD", "UPPER-RULES")
	writeContextFile(t, root, "AGENTS.md", "LOWER-RULES")

	got := buildProjectContextSection(root, "")
	assert.Contains(t, got, "LOWER-RULES")
	assert.NotContains(t, got, "UPPER-RULES")
}

// gitStub creates a fake repository layout on disk: a main worktree, a linked
// worktree inside it, and the git plumbing both need. It writes only the files
// DetectRepoLayout reads (.git directory/file + commondir), so the test does
// not need a git binary.
func gitStub(t *testing.T) (mainRoot, worktreeRoot string) {
	t.Helper()
	mainRoot = t.TempDir()
	commonDir := filepath.Join(mainRoot, ".git")
	require.NoError(t, os.MkdirAll(commonDir, 0o755))

	worktreeRoot = filepath.Join(mainRoot, ".worktrees", "feat")
	require.NoError(t, os.MkdirAll(worktreeRoot, 0o755))
	wtGitDir := filepath.Join(commonDir, "worktrees", "feat")
	require.NoError(t, os.MkdirAll(wtGitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(worktreeRoot, ".git"),
		[]byte("gitdir: "+wtGitDir+"\n"), 0o644))
	// commondir is resolved relative to the WORKTREE's git dir, so this points
	// at mainRoot/.git — exactly what `git worktree add` writes.
	require.NoError(t, os.WriteFile(filepath.Join(wtGitDir, "commondir"),
		[]byte(filepath.Join("..", "..")+"\n"), 0o644))
	return mainRoot, worktreeRoot
}

// TestAppendProjectContextSection_WorktreeInsideMainRepoInjectsOnce pins pi's
// shadowing rule: a worktree nested inside the main repo walks past the main
// repo's AGENTS.md on its way up, so without dedup one rule set is delivered
// twice — which reads to a model as emphasis on the duplicated rules.
func TestAppendProjectContextSection_WorktreeInsideMainRepoInjectsOnce(t *testing.T) {
	mainRoot, worktreeRoot := gitStub(t)
	writeContextFile(t, mainRoot, "AGENTS.md", "MAIN-RULES")
	writeContextFile(t, worktreeRoot, "AGENTS.md", "WORKTREE-RULES")

	got := buildProjectContextSection(worktreeRoot, "")

	assert.Equal(t, 1, strings.Count(got, "<project_instructions"),
		"exactly one context file must be injected, got:\n%s", got)
	assert.Contains(t, got, "WORKTREE-RULES")
}

// TestAppendProjectContextSection_WorktreeSymlinkToMainInjectsOnce pins the
// symlink case: the worktree's AGENTS.md often points at the main repo's file,
// so comparing RESOLVED file paths would say "two different files" and keep
// both. Identity is decided by the directory plus the file's base name.
func TestAppendProjectContextSection_WorktreeSymlinkToMainInjectsOnce(t *testing.T) {
	mainRoot, worktreeRoot := gitStub(t)
	mainFile := writeContextFile(t, mainRoot, "AGENTS.md", "MAIN-RULES")
	if err := os.Symlink(mainFile, filepath.Join(worktreeRoot, "AGENTS.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got := buildProjectContextSection(worktreeRoot, "")
	assert.Equal(t, 1, strings.Count(got, "<project_instructions"),
		"a symlinked worktree copy must not be injected twice, got:\n%s", got)
}

// TestAppendProjectContextSection_PlainRepoStillLayers pins that the dedup is
// scoped to worktrees: an ordinary repository still layers its own AGENTS.md
// under an outer directory's, and a SIBLING worktree (outside the main repo)
// keeps both files because no ancestor walk would duplicate them.
func TestAppendProjectContextSection_PlainRepoStillLayers(t *testing.T) {
	outer := t.TempDir()
	writeContextFile(t, outer, "AGENTS.md", "OUTER-RULES")
	repo := filepath.Join(outer, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	writeContextFile(t, repo, "AGENTS.md", "REPO-RULES")

	got := buildProjectContextSection(repo, "")
	assert.Contains(t, got, "OUTER-RULES")
	assert.Contains(t, got, "REPO-RULES")
}

// TestAgentSkipsProjectContext pins the switch: on means no injection on a
// chat turn, and off (the zero value, i.e. every pre-existing row) means the
// project rules still arrive.
func TestAgentSkipsProjectContext(t *testing.T) {
	db := setupTestDBForSessionCommand(t)
	defer func() { _ = db.Close() }()
	project := t.TempDir()
	writeContextFile(t, project, "AGENTS.md", "PROJECT-RULE-MARKER")

	withAgents(t, map[string]*model.Agent{
		"on":  {ID: "on", Backend: "builtin", RuntimeSystemPrompt: "B", SkipProjectContext: true},
		"off": {ID: "off", Backend: "builtin", RuntimeSystemPrompt: "B"},
	})

	skipped := BuildChatRequest("hi", "sess-skip", project, "builtin", "on", "", "", "", project, false)
	assert.NotContains(t, skipped.SystemPrompt, "PROJECT-RULE-MARKER",
		"the switch must turn injection off")

	kept := BuildChatRequest("hi", "sess-keep", project, "builtin", "off", "", "", "", project, false)
	assert.Contains(t, kept.SystemPrompt, "PROJECT-RULE-MARKER",
		"the zero value must keep injecting")
}
