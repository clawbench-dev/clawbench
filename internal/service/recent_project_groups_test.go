package service_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pathsOf flattens a group result into its item paths, in order.
func pathsOf(groups []service.RecentProjectGroup) []string {
	var out []string
	for _, g := range groups {
		for _, it := range g.Items {
			out = append(out, it.Path)
		}
	}
	return out
}

// groupSizes reports each group's item count, in order.
func groupSizes(groups []service.RecentProjectGroup) []int {
	out := make([]int, 0, len(groups))
	for _, g := range groups {
		out = append(out, len(g.Items))
	}
	return out
}

func TestGetRecentProjectGroups_Empty(t *testing.T) {
	setupRecentProjectsDB(t)

	groups, err := service.GetRecentProjectGroups()
	assert.NoError(t, err)
	assert.Empty(t, groups)
}

func TestGetRecentProjectGroups_NonRepoPathsAreSingletons(t *testing.T) {
	db := setupRecentProjectsDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	dirA := createTempProjectDir(t)
	dirB := createTempProjectDir(t)
	insertProjectWithTime(t, db, dirA, base)
	insertProjectWithTime(t, db, dirB, base.Add(time.Second))

	groups, err := service.GetRecentProjectGroups()
	require.NoError(t, err)

	assert.Equal(t, []int{1, 1}, groupSizes(groups), "non-repo paths form single-item groups")
	assert.Equal(t, []string{dirB, dirA}, pathsOf(groups), "time order preserved")
	for _, g := range groups {
		assert.Empty(t, g.RepoRoot, "non-repo groups have no repo root")
		assert.Equal(t, service.RepoKindPlain, g.Items[0].Kind)
	}
}

func TestGetRecentProjectGroups_GroupsWorktreesOfOneRepo(t *testing.T) {
	db := setupRecentProjectsDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	parent := t.TempDir()
	repo := filepath.Join(parent, "clawbench")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	makeRepo(t, repo)

	sub := filepath.Join(repo, "android")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	wt := filepath.Join(repo, ".worktrees", "doc-sync")
	makeLinkedWorktree(t, repo, wt, "../..")

	other := filepath.Join(parent, "playground")
	require.NoError(t, os.MkdirAll(other, 0o755))

	// Interleave timestamps: the linked worktree is the oldest, so without
	// sibling backfill it would fall outside a window that fits the others.
	insertProjectWithTime(t, db, repo, base.Add(4*time.Second))
	insertProjectWithTime(t, db, other, base.Add(3*time.Second))
	insertProjectWithTime(t, db, sub, base.Add(2*time.Second))
	insertProjectWithTime(t, db, wt, base.Add(1*time.Second))

	groups, err := service.GetRecentProjectGroups()
	require.NoError(t, err)

	require.Len(t, groups, 2, "one group for the repo, one for the unrelated project")

	repoGroup := groups[0]
	assert.Equal(t, repo, repoGroup.RepoRoot)
	assert.Equal(t, "clawbench", repoGroup.GroupName)
	assert.Equal(t, []string{repo, sub, wt}, pathsOf([]service.RecentProjectGroup{repoGroup}),
		"members ordered by access time, main worktree not pinned first")
	assert.Equal(t, []service.RepoKind{
		service.RepoKindMain,
		service.RepoKindSubdir,
		service.RepoKindWorktree,
	}, []service.RepoKind{repoGroup.Items[0].Kind, repoGroup.Items[1].Kind, repoGroup.Items[2].Kind})

	assert.Equal(t, other, groups[1].Items[0].Path)
}

func TestGetRecentProjectGroups_SeparateReposStaySeparate(t *testing.T) {
	db := setupRecentProjectsDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	parent := t.TempDir()
	repoA := filepath.Join(parent, "clawbench")
	repoB := filepath.Join(parent, "clawbench-master-backup")
	require.NoError(t, os.MkdirAll(repoA, 0o755))
	require.NoError(t, os.MkdirAll(repoB, 0o755))
	makeRepo(t, repoA)
	makeRepo(t, repoB)

	insertProjectWithTime(t, db, repoA, base)
	insertProjectWithTime(t, db, repoB, base.Add(time.Second))

	groups, err := service.GetRecentProjectGroups()
	require.NoError(t, err)

	assert.Equal(t, []int{1, 1}, groupSizes(groups),
		"two independent clones are two groups even when their names look alike")
	assert.Equal(t, repoB, groups[0].RepoRoot)
	assert.Equal(t, repoA, groups[1].RepoRoot)
}

func TestGetRecentProjectGroups_BackfillsSiblingBeyondWindow(t *testing.T) {
	db := setupRecentProjectsDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	origLimit := model.RecentProjectsMaxCount
	model.RecentProjectsMaxCount = 2
	t.Cleanup(func() { model.RecentProjectsMaxCount = origLimit })

	parent := t.TempDir()
	repo := filepath.Join(parent, "clawbench")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	makeRepo(t, repo)

	sub := filepath.Join(repo, "android")
	require.NoError(t, os.MkdirAll(sub, 0o755))

	other := filepath.Join(parent, "playground")
	require.NoError(t, os.MkdirAll(other, 0o755))

	// Window of 2 = {repo, other}. The subdir sits at rank 3 and must still be
	// pulled in as a sibling of `repo`, without displacing `other`.
	insertProjectWithTime(t, db, repo, base.Add(3*time.Second))
	insertProjectWithTime(t, db, other, base.Add(2*time.Second))
	insertProjectWithTime(t, db, sub, base.Add(1*time.Second))

	groups, err := service.GetRecentProjectGroups()
	require.NoError(t, err)

	require.Len(t, groups, 2)
	assert.Equal(t, []int{2, 1}, groupSizes(groups),
		"the sibling beyond the window joins its repo group")
	assert.Equal(t, []string{repo, sub, other}, pathsOf(groups),
		"the unrelated project still appears, in time order")
}

func TestGetRecentProjectGroups_DoesNotBackfillUnrelatedOutsideWindow(t *testing.T) {
	db := setupRecentProjectsDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	origLimit := model.RecentProjectsMaxCount
	model.RecentProjectsMaxCount = 2
	t.Cleanup(func() { model.RecentProjectsMaxCount = origLimit })

	dirA := createTempProjectDir(t)
	dirB := createTempProjectDir(t)
	dirC := createTempProjectDir(t)

	insertProjectWithTime(t, db, dirA, base.Add(2*time.Second))
	insertProjectWithTime(t, db, dirB, base.Add(time.Second))
	insertProjectWithTime(t, db, dirC, base)

	groups, err := service.GetRecentProjectGroups()
	require.NoError(t, err)

	assert.Len(t, groups, 2, "only the windowed projects appear")
	assert.Equal(t, []string{dirA, dirB}, pathsOf(groups))
}

func TestGetRecentProjectGroups_FiltersAndCleansStalePaths(t *testing.T) {
	db := setupRecentProjectsDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	alive := createTempProjectDir(t)
	dead := filepath.Join(t.TempDir(), "already-deleted")
	insertProjectWithTime(t, db, dead, base.Add(time.Second))
	insertProjectWithTime(t, db, alive, base)

	groups, err := service.GetRecentProjectGroups()
	require.NoError(t, err)

	assert.Equal(t, []string{alive}, pathsOf(groups))

	var count int
	require.NoError(t, db.QueryRow(
		"SELECT COUNT(*) FROM recent_projects WHERE project_path = ?", dead,
	).Scan(&count))
	assert.Zero(t, count, "stale row is cleaned up, as the flat listing did")
}

func TestGetRecentProjectGroups_DBQueryError(t *testing.T) {
	// A closed DB makes the query fail, which must surface as an error rather
	// than an empty list (the frontend distinguishes the two).
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(cleanup)
	require.NoError(t, db.Close())

	_, err = service.GetRecentProjectGroups()
	assert.Error(t, err, "should return error when DB query fails")
}

func TestGetRecentProjectGroups_GroupOrderFollowsMostRecentMember(t *testing.T) {
	db := setupRecentProjectsDB(t)
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	parent := t.TempDir()
	repoOld := filepath.Join(parent, "old-repo")
	repoNew := filepath.Join(parent, "new-repo")
	require.NoError(t, os.MkdirAll(repoOld, 0o755))
	require.NoError(t, os.MkdirAll(repoNew, 0o755))
	makeRepo(t, repoOld)
	makeRepo(t, repoNew)

	oldSub := filepath.Join(repoOld, "sub")
	newSub := filepath.Join(repoNew, "sub")
	require.NoError(t, os.MkdirAll(oldSub, 0o755))
	require.NoError(t, os.MkdirAll(newSub, 0o755))

	// The most recent row belongs to new-repo, so new-repo's group leads even
	// though old-repo also has a recent-ish member.
	insertProjectWithTime(t, db, oldSub, base.Add(3*time.Second))
	insertProjectWithTime(t, db, newSub, base.Add(4*time.Second))
	insertProjectWithTime(t, db, repoOld, base.Add(time.Second))
	insertProjectWithTime(t, db, repoNew, base.Add(2*time.Second))

	groups, err := service.GetRecentProjectGroups()
	require.NoError(t, err)

	require.Len(t, groups, 2)
	assert.Equal(t, repoNew, groups[0].RepoRoot)
	assert.Equal(t, repoOld, groups[1].RepoRoot)
	assert.Equal(t, []string{newSub, repoNew}, pathsOf(groups[:1]))
}
