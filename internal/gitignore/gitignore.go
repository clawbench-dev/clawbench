// Package gitignore answers "would git ignore this path?" for a project tree.
//
// It backs two features that must agree with git rather than approximate it:
// the file manager dims entries that git would not track, and the code-inventory
// (cloc) panel must not count vendored or generated sources. Both are driven by
// .gitignore, whose semantics (nested files, negations, anchoring, directory-only
// patterns, .git/info/exclude, core.excludesFile) are subtle enough that a
// hand-rolled matcher drifts from git. The pattern engine is therefore
// go-git's gitignore package — the same code git-compatible tooling uses.
//
// What this package adds on top of that engine:
//
//   - Repository discovery, including linked worktrees, where .git is a FILE
//     pointing at the real git dir and info/exclude lives in the COMMON dir.
//   - Per-directory pattern chains: git applies every .gitignore from the
//     repository root down to the directory holding the path, so a query deep
//     in the tree must include the nested files along the way. Chains are built
//     lazily and memoized, which keeps both a single directory listing and a
//     full tree walk to one read per directory that actually has a .gitignore.
//   - The tracked-file rules from the index, which override pattern matching.
//
// The tracked rules are what make this match `git check-ignore` instead of a
// naive pattern test. Verified against git on 13k+ directories: a tracked file
// is never ignored, a directory holding tracked files is never ignored (git
// must keep it reachable), and a path stays ignored when any proper ancestor
// directory is excluded — so "!build/keep.txt" is inert while "build/" is
// excluded.
package gitignore

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gogitconfig "github.com/go-git/go-git/v5/config"
	gogitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/format/index"
)

// cacheTTL bounds how long a built Matcher is reused. Short because a project's
// ignore rules and index change whenever the user edits files, and rebuilding
// is cheap (a handful of small reads).
const cacheTTL = 10 * time.Second

// cacheCapacity bounds the number of cached repositories. The map is keyed by
// repository root, so one entry per project the user has browsed.
const cacheCapacity = 16

// Matcher decides whether paths inside one repository are ignored. A nil
// *Matcher is valid and reports everything as not ignored, which is what
// callers get for a non-repository directory.
//
// A Matcher is safe for concurrent use.
type Matcher struct {
	repoRoot string
	gitDir   string

	// base holds the patterns that apply to every path in the repository:
	// core.excludesFile plus .git/info/exclude, both with no domain.
	base []gogitignore.Pattern

	// tracked holds repo-root-relative slash paths of files in the index.
	tracked map[string]bool
	// trackedDirs holds every ancestor directory of a tracked file. git does
	// not ignore a directory that holds tracked files, because those files must
	// stay reachable.
	trackedDirs map[string]bool

	mu sync.Mutex
	// chains memoizes the full pattern list per repo-relative directory ("" is
	// the repository root). Each entry is the parent's list plus that
	// directory's own .gitignore, so a walk descends in order.
	chains map[string][]gogitignore.Pattern
}

// Ignored reports whether git would ignore absPath. The path may be anywhere
// inside the repository; paths outside it are never ignored.
//
// isDir must come from an LSTAT of the path, not from following symlinks: git
// classifies entries the same way, so a symlink counts as a FILE even when its
// target is a directory. A directory-only pattern like "build/" therefore does
// not match a symlink named "build".
func (m *Matcher) Ignored(absPath string, isDir bool) bool {
	if m == nil {
		return false
	}
	rel, ok := m.relToRepo(absPath)
	if !ok || rel == "" {
		return false
	}

	// Rule 1: tracked wins. A tracked file, or a directory containing tracked
	// files, is never ignored — git keeps it reachable.
	if isDir {
		if m.trackedDirs[rel] {
			return false
		}
	} else if m.tracked[rel] {
		return false
	}

	parts := strings.Split(rel, "/")
	patterns := m.patternsForDir(relDirOf(parts))
	matcher := gogitignore.NewMatcher(patterns)

	// Rule 2: ancestor exclusion. git never re-includes a path whose parent
	// directory is excluded, so a negation deeper down has no effect. Patterns
	// from a deeper .gitignore cannot match these shorter prefixes (their domain
	// is longer), so the full list is safe to reuse here.
	for i := 1; i < len(parts); i++ {
		if matcher.Match(parts[:i], true) {
			return true
		}
	}

	// Rule 3: the path's own last matching pattern decides; a later negation
	// can re-include it.
	return matcher.Match(parts, isDir)
}

// patternsForDir returns the pattern chain that applies to entries directly
// inside the repo-relative directory relDir ("" is the repository root).
//
// Chains are built by extending the nearest already-known ancestor, so listing
// one directory reads only the .gitignore files along its path, and walking a
// tree reads each directory's .gitignore once.
func (m *Matcher) patternsForDir(relDir string) []gogitignore.Pattern {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ps, ok := m.chains[relDir]; ok {
		return ps
	}

	// Walk up to the nearest directory whose chain is already known. The root
	// chain is seeded at construction, so this terminates.
	var missing []string
	d := relDir
	for {
		if _, ok := m.chains[d]; ok {
			break
		}
		missing = append(missing, d)
		d = parentDir(d)
	}

	// Extend downward: missing is ordered deepest-first, so iterate in reverse.
	for i := len(missing) - 1; i >= 0; i-- {
		cur := missing[i]
		parent := parentDir(cur)
		chain := m.chains[parent]

		var domain []string
		if cur != "" {
			domain = strings.Split(cur, "/")
		}
		if b, err := os.ReadFile(filepath.Join(m.repoRoot, filepath.FromSlash(cur), ".gitignore")); err == nil {
			extended := make([]gogitignore.Pattern, 0, len(chain))
			extended = append(extended, chain...)
			extended = append(extended, parsePatterns(string(b), domain)...)
			chain = extended
		}
		m.chains[cur] = chain
	}

	return m.chains[relDir]
}

// relToRepo converts an absolute path to a slash-separated path relative to the
// repository root. The bool is false when absPath lies outside the repository.
func (m *Matcher) relToRepo(absPath string) (string, bool) {
	rel, err := filepath.Rel(m.repoRoot, absPath)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	if rel == "." {
		return "", true
	}
	return rel, true
}

// RepoRoot returns the repository root the matcher was built for, or "" when
// the directory is not inside a repository.
func (m *Matcher) RepoRoot() string {
	if m == nil {
		return ""
	}
	return m.repoRoot
}

// relDirOf returns the slash-joined directory portion of a split path.
func relDirOf(parts []string) string {
	if len(parts) <= 1 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

// parentDir returns the parent of a repo-relative directory ("" -> "").
func parentDir(rel string) string {
	i := strings.LastIndex(rel, "/")
	if i < 0 {
		return ""
	}
	return rel[:i]
}

// ── construction ──────────────────────────────────────────────────────────────

// cache memoizes matchers per repository root. The pattern chains inside a
// matcher are keyed per directory, so one matcher serves every listing in that
// repository.
var cache = struct {
	sync.Mutex
	entries map[string]cacheEntry
}{entries: make(map[string]cacheEntry)}

type cacheEntry struct {
	m        *Matcher
	loadedAt time.Time
}

// ForDir returns a Matcher for the repository containing dir. It returns nil
// when dir is not inside a repository, or when the repository state cannot be
// read — callers then treat every path as not ignored, which keeps the file
// manager and cloc working outside git.
func ForDir(dir string) *Matcher {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	repoRoot := findRepoRoot(absDir)
	if repoRoot == "" {
		return nil
	}

	cache.Lock()
	if e, ok := cache.entries[repoRoot]; ok && time.Since(e.loadedAt) < cacheTTL {
		cache.Unlock()
		return e.m
	}
	cache.Unlock()

	m := build(repoRoot)

	cache.Lock()
	if len(cache.entries) >= cacheCapacity {
		now := time.Now()
		for k, v := range cache.entries {
			if now.Sub(v.loadedAt) >= cacheTTL {
				delete(cache.entries, k)
			}
		}
		if len(cache.entries) >= cacheCapacity {
			var oldestKey string
			var oldest time.Time
			for k, v := range cache.entries {
				if oldestKey == "" || v.loadedAt.Before(oldest) {
					oldestKey, oldest = k, v.loadedAt
				}
			}
			if oldestKey != "" {
				delete(cache.entries, oldestKey)
			}
		}
	}
	cache.entries[repoRoot] = cacheEntry{m: m, loadedAt: time.Now()}
	cache.Unlock()

	return m
}

// build assembles the matcher for one repository.
func build(repoRoot string) *Matcher {
	gitDir := resolveGitDir(repoRoot)
	if gitDir == "" {
		return nil
	}

	m := &Matcher{
		repoRoot: repoRoot,
		gitDir:   gitDir,
		base:     loadBasePatterns(gitDir),
		tracked:  readTracked(gitDir),
		chains:   make(map[string][]gogitignore.Pattern),
	}
	m.trackedDirs = ancestorDirs(m.tracked)
	// Seed the root chain so patternsForDir can always walk up to something:
	// repository-wide patterns plus the root .gitignore. Built as a fresh slice
	// so it cannot alias m.base's backing array.
	rootChain := make([]gogitignore.Pattern, 0, len(m.base))
	rootChain = append(rootChain, m.base...)
	rootChain = append(rootChain, readGitignore(repoRoot)...)
	m.chains[""] = rootChain
	return m
}

// readGitignore parses the .gitignore directly inside dir, if any. Patterns are
// bound to a nil domain because the caller decides the domain.
func readGitignore(dir string) []gogitignore.Pattern {
	b, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	return parsePatterns(string(b), nil)
}

// findRepoRoot walks up from dir until it finds a .git entry, which may be a
// directory (normal clone) or a file (linked worktree / submodule).
func findRepoRoot(dir string) string {
	d := dir
	for {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// resolveGitDir returns the absolute git dir for repoRoot. For a linked
// worktree .git is a file containing "gitdir: <path>".
func resolveGitDir(repoRoot string) string {
	p := filepath.Join(repoRoot, ".git")
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if st.IsDir() {
		return p
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	line = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if line == "" {
		return ""
	}
	if !filepath.IsAbs(line) {
		line = filepath.Join(repoRoot, line)
	}
	return filepath.Clean(line)
}

// commonGitDir resolves the shared git dir. A linked worktree keeps a
// `commondir` file pointing at the main .git, and info/exclude lives THERE, not
// in the per-worktree git dir.
func commonGitDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return gitDir
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(gitDir, p)
	}
	return filepath.Clean(p)
}

// loadBasePatterns collects the repository-wide patterns in ascending priority
// order, which is what the go-git matcher expects: core.excludesFile from the
// user's global config, then .git/info/exclude. Per-directory .gitignore files
// are appended later by patternsForDir.
func loadBasePatterns(gitDir string) []gogitignore.Pattern {
	var ps []gogitignore.Pattern

	ps = append(ps, loadGlobalExcludePatterns()...)

	// .git/info/exclude is shared by all worktrees, so read it from the common
	// git dir rather than the per-worktree one.
	if b, err := os.ReadFile(filepath.Join(commonGitDir(gitDir), "info", "exclude")); err == nil {
		ps = append(ps, parsePatterns(string(b), nil)...)
	}

	return ps
}

// loadGlobalExcludePatterns reads core.excludesFile from the user's global git
// config and parses that file.
//
// go-git's LoadGlobalPatterns cannot be used here: it locates the global config
// with os.UserHomeDir, which on Windows resolves to %USERPROFILE% and ignores
// HOME entirely. git itself honors HOME on every platform, so on Windows the
// two disagree whenever HOME differs from the profile directory and the user's
// excludes file is silently dropped. Resolving home the way git does keeps this
// package agreeing with git.
func loadGlobalExcludePatterns() []gogitignore.Pattern {
	// Ascending priority: XDG config is read first, then ~/.gitconfig, whose
	// value wins — the same order git applies.
	var excludesFile string
	for _, cfgPath := range globalGitConfigPaths() {
		b, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		cfg, err := gogitconfig.ReadConfig(bytes.NewReader(b))
		if err != nil || cfg.Raw == nil {
			continue
		}
		if v := cfg.Raw.Section("core").Option("excludesfile"); v != "" {
			excludesFile = v
		}
	}
	if excludesFile == "" {
		return nil
	}

	b, err := os.ReadFile(expandHome(excludesFile))
	if err != nil {
		return nil
	}
	return parsePatterns(string(b), nil)
}

// globalGitConfigPaths returns the user's global git config files in ascending
// priority order, matching git's lookup: the XDG config (honoring
// XDG_CONFIG_HOME, defaulting to ~/.config) and then ~/.gitconfig, whose value
// wins.
func globalGitConfigPaths() []string {
	home := homeDir()
	var out []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "git", "config"))
	} else if home != "" {
		// git falls back to ~/.config when XDG_CONFIG_HOME is unset.
		out = append(out, filepath.Join(home, ".config", "git", "config"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, ".gitconfig"))
	}
	return out
}

// homeDir returns the home directory as git resolves it: $HOME first, falling
// back to os.UserHomeDir. git prefers HOME on every platform, whereas
// os.UserHomeDir ignores it on Windows — the divergence this exists to avoid.
func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// expandHome expands a leading "~" in a config path, as git does for
// core.excludesFile. Other paths are returned unchanged.
func expandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home := homeDir()
	if home == "" {
		return p
	}
	return filepath.Join(home, p[2:])
}

// parsePatterns turns .gitignore file content into patterns bound to domain.
// Blank and whitespace-only lines and comments are skipped; every other line is
// passed through the engine so it applies git's own rules (negation, anchoring,
// directory-only, "**"), with backslash escapes rewritten first — see
// translateEscapes for why that is necessary.
func parsePatterns(content string, domain []string) []gogitignore.Pattern {
	var ps []gogitignore.Pattern
	sc := bufio.NewScanner(strings.NewReader(content))
	// .gitignore lines are short; raise the token limit a little so a
	// pathological single-line file cannot truncate silently.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		// A line of only spaces is blank to git (trailing whitespace is
		// stripped), so it must not become a pattern.
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ps = append(ps, gogitignore.ParsePattern(translateEscapes(line), domain))
	}
	return ps
}

// translateEscapes rewrites git's backslash escapes into an equivalent pattern
// with no backslashes.
//
// The go-git engine matches with filepath.Match, and Go's filepath.Match
// DISABLES backslash escaping on Windows, treating "\" as a path separator
// instead. A pattern like `spaced\ name.txt` therefore never matches on Windows
// even though git ignores the file, so escaped patterns silently stop working
// there. Rewriting each escaped character as a one-character class ("[ ]",
// "[#]") expresses the same literal without any backslash, so both platforms
// agree.
//
// A backslash not followed by anything (a trailing "\") is kept as-is: git
// treats such a pattern as matching nothing, and filepath.Match reports it as
// ErrBadPattern, which the engine already treats as "no match" — the same
// outcome on both platforms.
func translateEscapes(p string) string {
	if !strings.Contains(p, `\`) {
		return p
	}
	var b strings.Builder
	b.Grow(len(p) + 8)
	for i := 0; i < len(p); i++ {
		if p[i] != '\\' || i+1 == len(p) {
			b.WriteByte(p[i])
			continue
		}
		i++
		b.WriteString(escapeLiteral(p[i]))
	}
	return b.String()
}

// escapeLiteral renders one byte as a glob literal that contains no backslash.
//
// Characters that are glob metacharacters (or that need a class to stay
// literal, like the space git escapes) become a single-character class; every
// other byte is already a literal and is written through. "]" is left bare
// because it is only special inside a class.
func escapeLiteral(c byte) string {
	switch c {
	case '*', '?', '[', '!', ' ':
		return "[" + string(c) + "]"
	case '\\':
		// A class is required: a bare backslash is the escape character on
		// Unix and a separator on Windows.
		return `[\\]`
	default:
		return string(c)
	}
}

// readTracked decodes the git index into a set of repo-root-relative slash
// paths. A missing or unreadable index (fresh repo, bare repo, corrupt file)
// yields an empty set: pattern matching then decides everything, which is the
// same answer git gives when nothing is tracked.
func readTracked(gitDir string) map[string]bool {
	// The index is per-worktree, so read it from gitDir, not the common dir.
	f, err := os.Open(filepath.Join(gitDir, "index"))
	if err != nil {
		return map[string]bool{}
	}
	defer func() { _ = f.Close() }()

	idx := &index.Index{}
	if err := index.NewDecoder(f).Decode(idx); err != nil {
		return map[string]bool{}
	}

	out := make(map[string]bool, len(idx.Entries))
	for _, e := range idx.Entries {
		out[filepath.ToSlash(e.Name)] = true
	}
	return out
}

// ancestorDirs returns every directory that contains at least one tracked file.
// These directories are exempt from the ignore rules because git must keep the
// tracked files inside them reachable.
func ancestorDirs(tracked map[string]bool) map[string]bool {
	out := make(map[string]bool)
	for p := range tracked {
		for {
			i := strings.LastIndex(p, "/")
			if i < 0 {
				break
			}
			p = p[:i]
			if p == "" || out[p] {
				break
			}
			out[p] = true
		}
	}
	return out
}

// resetCacheForTest clears the memoization cache. Test-only helper so cases do
// not observe another case's repository state.
func resetCacheForTest() {
	cache.Lock()
	cache.entries = make(map[string]cacheEntry)
	cache.Unlock()
}
