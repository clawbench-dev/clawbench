package service

import (
	"os"
	"path/filepath"
	"strings"
)

// RepoKind classifies how a project path sits inside a git repository. The
// recent-projects list groups paths that share one repository, and the kind
// tells the user which role each path plays inside that group.
type RepoKind string

const (
	// RepoKindPlain marks a path that is not inside any git repository.
	RepoKindPlain RepoKind = "plain"
	// RepoKindMain marks a path that is a repository root whose git dir IS the
	// common dir — the main worktree.
	RepoKindMain RepoKind = "main"
	// RepoKindWorktree marks a repository root backed by a linked worktree:
	// its `.git` is a FILE pointing into the main repository's git dir, and a
	// `commondir` file points back at the shared git dir.
	RepoKindWorktree RepoKind = "worktree"
	// RepoKindSubdir marks a path inside a repository that is NOT a repository
	// root — e.g. a subdirectory of the main worktree opened as its own project.
	RepoKindSubdir RepoKind = "subdir"
)

// RepoLayout describes where a project path sits inside git.
//
// All fields are derived by reading files on disk; no git subprocess runs, so
// the recent-projects dropdown can decorate every row cheaply. The layout is
// also what decides grouping: two paths belong to one group when their
// CommonDir is equal.
type RepoLayout struct {
	// Root is the repository root (the directory holding `.git`), or "" when
	// the path is not inside a repository.
	Root string
	// CommonDir is the shared git dir — the grouping key. Linked worktrees of
	// one repository all resolve to the same CommonDir. Empty when Root is "".
	CommonDir string
	// Kind classifies the path relative to the repository.
	Kind RepoKind
}

// IsRepo reports whether the path is inside a git repository.
func (l RepoLayout) IsRepo() bool { return l.Root != "" && l.CommonDir != "" }

// DetectRepoLayout resolves the repository layout for dir.
//
// It mirrors the discovery rules the gitignore package uses (and that git
// itself uses): walk up until an entry named `.git` exists; that entry is a
// directory for a normal clone, or a file containing `gitdir: <path>` for a
// linked worktree or submodule; a linked worktree additionally carries a
// `commondir` file inside its git dir pointing back at the shared git dir.
//
// A path that is not in a repository returns the zero layout (Kind plain).
// Unreadable or malformed `.git`/`commondir` files degrade to plain rather than
// erroring, because this decorates a UI list and a broken checkout must not
// break the dropdown.
func DetectRepoLayout(dir string) RepoLayout {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return RepoLayout{Kind: RepoKindPlain}
	}
	absDir = filepath.Clean(absDir)

	root := findGitRepoRoot(absDir)
	if root == "" {
		return RepoLayout{Kind: RepoKindPlain}
	}

	gitDir, linked := resolveGitDirPath(root)
	if gitDir == "" {
		return RepoLayout{Kind: RepoKindPlain}
	}

	commonDir := resolveCommonGitDir(gitDir)
	if commonDir == "" {
		return RepoLayout{Kind: RepoKindPlain}
	}

	kind := RepoKindWorktree
	switch {
	case !samePath(absDir, root):
		kind = RepoKindSubdir
	case !linked:
		kind = RepoKindMain
	}

	// Normalize both paths through symlinks so one repository yields one
	// grouping key no matter which route reached it. Without this, the same
	// repo opened via a symlink (macOS /var -> /private/var on every temp dir)
	// would split into two groups with two headers.
	root = canonicalize(root)
	commonDir = canonicalize(commonDir)

	return RepoLayout{Root: root, CommonDir: commonDir, Kind: kind}
}

// canonicalize resolves symlinks so path identity comparisons and grouping
// keys are stable. It falls back to the cleaned path when resolution fails
// (e.g. a dangling gitdir pointer), which keeps detection total.
func canonicalize(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// findGitRepoRoot walks up from dir until it finds a `.git` entry, which may be
// a directory (normal clone) or a file (linked worktree / submodule). Returns
// "" when the filesystem root is reached without a match.
func findGitRepoRoot(dir string) string {
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

// resolveGitDirPath returns the absolute git dir for repoRoot, plus whether the
// repository is a linked worktree.
//
// The linked-ness is decided by the SHAPE of `.git`: a directory means an
// ordinary clone whose git dir is the common dir, while a file containing
// "gitdir: <path>" means the git dir lives elsewhere (linked worktree or
// submodule). That distinction must not be inferred from the commondir file —
// a linked worktree whose commondir is missing or unreadable is still a linked
// worktree, and misreporting it as the main worktree would put the wrong label
// on the group header.
//
// A relative pointer target is resolved against the worktree root, matching
// git's own interpretation.
func resolveGitDirPath(repoRoot string) (string, bool) {
	p := filepath.Join(repoRoot, ".git")
	st, err := os.Stat(p)
	if err != nil {
		return "", false
	}
	if st.IsDir() {
		return p, false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	line := strings.TrimSpace(string(b))
	line = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if line == "" {
		return "", false
	}
	if !filepath.IsAbs(line) {
		line = filepath.Join(repoRoot, line)
	}
	return filepath.Clean(line), true
}

// resolveCommonGitDir resolves the shared git dir. A linked worktree keeps a
// `commondir` file (usually a relative path such as "../..") pointing at the
// main repository's git dir; without it the git dir is itself the common dir.
func resolveCommonGitDir(gitDir string) string {
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

// samePath compares two absolute paths for identity, tolerating the symlink
// indirection that differs per platform (macOS /var -> /private/var, Windows
// short names). It falls back to the cleaned string comparison when either side
// cannot be resolved.
func samePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return ra == rb
}
