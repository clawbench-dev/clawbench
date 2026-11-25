// Package symwalk 是「跟着符号链接走」的目录遍历。
//
// filepath.WalkDir 刻意不进符号链接，于是同一个文件会出现「点得开、搜不到」：
// 浏览界面沿着链接进去了，搜索却看不见里面的东西。文件管理器的两条搜索
// （文件名 / 内容）和内置智能体的 glob / grep 都栽在这上面，因此遍历抽到这里，
// 四处共用一份实现，不再各写一遍。
//
// 调用方只提供两件事：往下的规则（visit 返回 Continue/SkipDir/Stop），以及
// 「某个链接指向的目录能不能进」（Options.CanEnter）。后者是安全边界，两个宿主
// 的口径不同：文件管理器按配置的根目录判，内置智能体按「是否限制在工作目录内」判。
package symwalk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// Default bounds for one walk.
//
// A symlink can point anywhere (a link to "/" is legal), so an unbounded walk is
// one keystroke away from traversing the entire disk. The entry budget is that
// backstop: when it trips the caller is told the result is incomplete, and the
// search UIs surface that instead of pretending they found everything.
const (
	DefaultMaxDepth   = 64
	DefaultMaxEntries = 200_000
)

// Entry is one entry reached by Walk.
//
// Path is the LOGICAL path — the one spelled through whatever symlinks the walk
// descended — not the resolved target. Reporting the logical path is what keeps
// results addressable exactly like browsed rows: the caller re-requests that same
// string to open the file, and the read endpoints resolve it through the link.
// Info/IsDir come from the TARGET (the link is followed), so a link to a
// directory is a directory and a link to a file shows the file's size and mtime
// instead of the link's own.
type Entry struct {
	Path      string
	Rel       string // slash-normalized path relative to Options.Base
	Name      string
	Info      fs.FileInfo
	IsDir     bool
	IsSymlink bool
	// Blocked reports that the entry IS a directory but Options.CanEnter
	// refused its target. IsDir is false in that case, so it is not descended
	// and not addressed as a directory; this flag is what lets a caller skip it
	// entirely instead of treating it as a file that cannot be read.
	Blocked bool
}

// Result is what the visitor tells the walker to do next.
type Result int

const (
	// Continue descends into the entry if it is a directory.
	Continue Result = iota
	// SkipDir prunes the entry's subtree (no-op for files).
	SkipDir
	// Stop ends the whole walk (the visitor hit a result limit or was
	// cancelled).
	Stop
)

// Options carries the per-walk policy.
type Options struct {
	// Base is what Entry.Rel is relative to. Defaults to the walk root.
	Base string
	// MaxDepth and MaxEntries bound the walk; zero means the defaults.
	MaxDepth   int
	MaxEntries int
	// CanEnter reports whether a directory reached through a symlink may be
	// entered. nil means every target is allowed. A false answer keeps the
	// entry listed but not entered — callers that cannot open such a target
	// type it as a non-directory instead.
	CanEnter func(realPath string) bool
}

// Outcome reports what a walk actually covered.
type Outcome struct {
	// Visited is how many entries were handed to the visitor.
	Visited int
	// Exhausted reports that the entry budget ran out, so the results are
	// incomplete rather than "there is nothing more to find".
	Exhausted bool
	// RootError is set when the walk root itself could not be read. An
	// unreadable subdirectory is skipped silently (one locked directory must
	// not hide every match elsewhere), but an unreadable root means the answer
	// "nothing matched" would be a lie — the caller should report the failure.
	RootError error
}

// Walk walks root the way a file manager browses it: symlinks are followed, so
// files that only exist on the other side of a link are found.
//
// Guards, in the order they run:
//   - a link whose target Options.CanEnter refuses is not entered;
//   - a link that resolves back to one of its own ancestors is skipped, so a
//     self-referential link cannot recurse forever;
//   - MaxDepth and MaxEntries bound the rest.
//
// The visitor is called for every entry except the root itself, before any
// descent, so returning SkipDir prunes the subtree.
func Walk(ctx context.Context, root string, opts Options, visit func(Entry) Result) Outcome {
	if opts.Base == "" {
		opts.Base = root
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultMaxDepth
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = DefaultMaxEntries
	}
	w := &walker{ctx: ctx, opts: opts, visit: visit}

	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = filepath.Clean(root)
	}
	w.descend(root, true, map[string]bool{rootReal: true}, 0)
	w.out.Visited = w.visited
	return w.out
}

// walker carries the per-walk state. A separate type keeps the recursion
// signature to one argument and lets the guards read like guards.
type walker struct {
	ctx     context.Context
	opts    Options
	visit   func(Entry) Result
	visited int
	stopped bool
	out     Outcome
}

// descend walks one directory whose real path is already in ancestry. isRoot
// only affects error reporting, not traversal.
func (w *walker) descend(dirPath string, isRoot bool, ancestry map[string]bool, depth int) {
	if w.halted() {
		return
	}
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if isRoot {
			w.out.RootError = err
		}
		return // inaccessible entry: skip it, exactly like an unreadable dir
	}
	for _, d := range entries {
		if w.halted() {
			return
		}
		w.handleEntry(dirPath, d, ancestry, depth)
	}
}

// handleEntry reports one entry to the visitor and descends into it when the
// visitor allows and it is a directory the walk may enter.
func (w *walker) handleEntry(dirPath string, d fs.DirEntry, ancestry map[string]bool, depth int) {
	fullPath := filepath.Join(dirPath, d.Name())
	info, isLink, ok := FollowInfo(fullPath, d)
	if !ok {
		// Dangling link or unstattable entry: there is nothing to open, so it is
		// not a hit. A listing may still show it, flagged as broken — a search
		// answers "where can I go", not "what is here".
		return
	}
	rel, relErr := filepath.Rel(w.opts.Base, fullPath)
	if relErr != nil {
		return
	}

	isDir, realPath, blocked := w.classify(fullPath, info, isLink)
	enterable := isDir && depth < w.opts.MaxDepth

	if w.visited >= w.opts.MaxEntries {
		w.out.Exhausted = true
		w.stopped = true
		return
	}
	w.visited++

	switch w.visit(Entry{
		Path:      fullPath,
		Rel:       filepath.ToSlash(rel),
		Name:      d.Name(),
		Info:      info,
		IsDir:     isDir,
		IsSymlink: isLink,
		Blocked:   blocked,
	}) {
	case Stop:
		w.stopped = true
		return
	case SkipDir:
		return
	}

	// ancestry covers the self-referential case: a link back to an ancestor
	// resolves to a real path already on this chain, and entering it would
	// recurse forever.
	if !enterable || ancestry[realPath] {
		return
	}
	ancestry[realPath] = true
	w.descend(fullPath, false, ancestry, depth+1)
	delete(ancestry, realPath)
}

// classify decides whether an entry is a traversable directory, which real path
// identifies it for cycle detection, and whether it is a directory the caller
// refused.
//
// A directory reached through a link only counts as a directory when the caller
// accepts its target; one the caller refuses stays a non-directory, so nothing is
// reported as openable that the caller cannot actually open.
func (w *walker) classify(fullPath string, info fs.FileInfo, isLink bool) (isDir bool, realPath string, blocked bool) {
	if !info.IsDir() {
		return false, "", false
	}
	if !isLink {
		return true, filepath.Clean(fullPath), false
	}
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return false, "", false
	}
	if w.opts.CanEnter != nil && !w.opts.CanEnter(resolved) {
		return false, "", true
	}
	return true, resolved, false
}

// FollowInfo returns the target's FileInfo for a directory entry, following a
// symlink. isLink reports whether the entry itself is a link. ok is false for
// dangling links and entries that cannot be statted (permissions, races).
//
// Exported because a caller that lists one level itself (the non-recursive
// searches) needs the same "type and size of the target, not of the link" answer
// the walker uses.
func FollowInfo(fullPath string, d fs.DirEntry) (info fs.FileInfo, isLink bool, ok bool) {
	isLink = d.Type()&fs.ModeSymlink != 0
	if !isLink {
		info, err := d.Info()
		if err != nil {
			return nil, false, false
		}
		return info, false, true
	}
	target, err := os.Stat(fullPath) // follows the link
	if err != nil {
		return nil, true, false
	}
	return target, true, true
}

// halted reports whether the walk must stop, marking it on cancellation.
func (w *walker) halted() bool {
	if w.stopped {
		return true
	}
	select {
	case <-w.ctx.Done():
		w.stopped = true
		return true
	default:
		return false
	}
}
