package handler

import (
	"context"

	"clawbench/internal/symwalk"
)

// 遍历本身在 internal/symwalk，内置智能体的 glob / grep 用同一份实现。
// 这里只保留本包的名字和策略：文件管理器按「目标是否在配置的根目录下」
// 决定能不能进一个链接目录，和列表页拒绝进入越界链接的口径一致。
//
// Bounds for one follow-symlink walk. A var rather than a const only so the
// budget test can lower it; nothing assigns it outside tests.
var (
	maxWalkedEntries = symwalk.DefaultMaxEntries
	maxWalkDepth     = symwalk.DefaultMaxDepth
)

type (
	walkEntry   = symwalk.Entry
	walkResult  = symwalk.Result
	walkOutcome = symwalk.Outcome
)

const (
	walkContinue = symwalk.Continue
	walkSkipDir  = symwalk.SkipDir
	walkStop     = symwalk.Stop
)

// statFollowing reads an entry the way the walker does: through the link, so the
// type and size are the target's. The non-recursive searches list one level
// themselves and need the same answer.
var statFollowing = symwalk.FollowInfo

// walkFollowSymlinks walks root the way the file manager browses it: symlinks are
// followed, so files that only exist on the other side of a link are found.
//
// filepath.WalkDir deliberately does not follow symlinks, which is why the
// searches used to miss them while browsing showed them — the two disagreed
// about what exists.
func walkFollowSymlinks(ctx context.Context, root, base string, visit func(walkEntry) walkResult) walkOutcome {
	return symwalk.Walk(ctx, root, symwalk.Options{
		Base:       base,
		MaxDepth:   maxWalkDepth,
		MaxEntries: maxWalkedEntries,
		CanEnter:   isPathUnderAnyRoot,
	}, visit)
}
