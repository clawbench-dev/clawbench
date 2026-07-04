package service_test

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
)

const recentProjectsSchema = `
CREATE TABLE IF NOT EXISTS recent_projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_path TEXT UNIQUE NOT NULL,
	accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	is_default INTEGER NOT NULL DEFAULT 0
);
`

func setupRecentProjectsDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)

	_, err = db.Exec(recentProjectsSchema)
	assert.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		db.Close()
	})
	return db
}

// insertProjectWithTime inserts a project with an explicit accessed_at timestamp
// to ensure deterministic ordering in tests (CURRENT_TIMESTAMP has only second precision).
func insertProjectWithTime(t *testing.T, db *sql.DB, path string, accessedAt time.Time) {
	t.Helper()
	_, err := db.Exec(
		"INSERT INTO recent_projects (project_path, accessed_at) VALUES (?, ?)",
		path, accessedAt.Format("2006-01-02 15:04:05"),
	)
	assert.NoError(t, err)
}

// createTempProjectDir creates a temporary directory and returns its path.
// The directory is cleaned up after the test.
func createTempProjectDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "recent-project-test-*")
	assert.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestGetRecentProjects_Empty(t *testing.T) {
	setupRecentProjectsDB(t)

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Empty(t, paths)
}

func TestAddRecentProject(t *testing.T) {
	setupRecentProjectsDB(t)
	projectDir := createTempProjectDir(t)

	err := service.AddRecentProject(projectDir)
	assert.NoError(t, err)

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Equal(t, []string{projectDir}, paths)
}

func TestGetRecentProjects_OrderedByAccessedAtDesc(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dirA := createTempProjectDir(t)
	dirB := createTempProjectDir(t)
	dirC := createTempProjectDir(t)

	// Insert with explicit timestamps to guarantee ordering
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, dirA, baseTime)
	insertProjectWithTime(t, db, dirB, baseTime.Add(1*time.Second))
	insertProjectWithTime(t, db, dirC, baseTime.Add(2*time.Second))

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 3)

	// Most recently accessed should be first
	assert.Equal(t, dirC, paths[0])
	assert.Equal(t, dirB, paths[1])
	assert.Equal(t, dirA, paths[2])
}

func TestAddRecentProject_Upsert(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dirA := createTempProjectDir(t)
	dirB := createTempProjectDir(t)

	// Insert with explicit timestamps so the upsert bump is testable
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, dirA, baseTime)
	insertProjectWithTime(t, db, dirB, baseTime.Add(1*time.Second))

	// Re-add dirA via the service function - should update timestamp
	err := service.AddRecentProject(dirA)
	assert.NoError(t, err)

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 2) // Still only 2 entries, not 3

	// dirA should now be the most recent (its timestamp was just updated)
	assert.Equal(t, dirA, paths[0])
	assert.Equal(t, dirB, paths[1])
}

func TestAddRecentProject_PruneBeyond10(t *testing.T) {
	setupRecentProjectsDB(t)

	// Add 12 projects via the service (timestamps will be close but pruning still works)
	for range 12 {
		dir := createTempProjectDir(t)
		err := service.AddRecentProject(dir)
		assert.NoError(t, err)
	}

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 10) // Should be capped at 10
}

func TestGetRecentProjects_Limit10(t *testing.T) {
	setupRecentProjectsDB(t)

	// Add exactly 10 projects
	for range 10 {
		dir := createTempProjectDir(t)
		err := service.AddRecentProject(dir)
		assert.NoError(t, err)
	}

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 10)
}

func TestAddRecentProject_PruneKeepsMostRecent(t *testing.T) {
	db := setupRecentProjectsDB(t)

	// Insert 10 projects with explicit timestamps
	dirs := make([]string, 0, 10)
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 10 {
		dir := createTempProjectDir(t)
		dirs = append(dirs, dir)
		insertProjectWithTime(t, db, dir, baseTime.Add(time.Duration(i)*time.Second))
	}

	// Add an 11th via service - should prune the oldest (dirs[0])
	newDir := createTempProjectDir(t)
	err := service.AddRecentProject(newDir)
	assert.NoError(t, err)

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 10)

	// dirs[0] should have been pruned (it had the oldest timestamp)
	for _, p := range paths {
		assert.NotEqual(t, dirs[0], p)
	}

	// newDir should be present (it was just added)
	found := false
	for _, p := range paths {
		if p == newDir {
			found = true
			break
		}
	}
	assert.True(t, found, "new project should be in the list")
}

// --- GetDefaultProject / SetDefaultProject tests ---

func TestGetDefaultProject_IsDefaultRow(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dirA := createTempProjectDir(t)
	dirB := createTempProjectDir(t)

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, dirA, baseTime)
	insertProjectWithTime(t, db, dirB, baseTime.Add(1*time.Second))

	// Set dirA as default (not the most recently accessed)
	_, err := db.Exec("UPDATE recent_projects SET is_default = 1 WHERE project_path = ?", dirA)
	assert.NoError(t, err)

	path, err := service.GetDefaultProject()
	assert.NoError(t, err)
	assert.Equal(t, dirA, path, "should return the is_default=1 project, not the most recent")
}

func TestGetDefaultProject_FallbackToMostRecent(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dirA := createTempProjectDir(t)
	dirB := createTempProjectDir(t)

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, dirA, baseTime)
	insertProjectWithTime(t, db, dirB, baseTime.Add(1*time.Second))

	// No is_default=1 set — should fall back to most recent
	path, err := service.GetDefaultProject()
	assert.NoError(t, err)
	assert.Equal(t, dirB, path, "should fall back to most recently accessed project")
}

func TestGetDefaultProject_FallbackToHomeDir(t *testing.T) {
	setupRecentProjectsDB(t)

	// Empty DB — should fall back to home directory
	path, err := service.GetDefaultProject()
	assert.NoError(t, err)
	assert.NotEmpty(t, path, "should return home directory when no recent projects")
}

func TestGetDefaultProject_StaleDefaultCleared(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dirA := createTempProjectDir(t)
	nonExistentPath := "/tmp/clawbench-default-stale-" + fmt.Sprintf("%d", time.Now().UnixNano())

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, dirA, baseTime)
	insertProjectWithTime(t, db, nonExistentPath, baseTime.Add(1*time.Second))

	// Set nonexistent path as default
	_, err := db.Exec("UPDATE recent_projects SET is_default = 1 WHERE project_path = ?", nonExistentPath)
	assert.NoError(t, err)

	path, err := service.GetDefaultProject()
	assert.NoError(t, err)
	assert.Equal(t, dirA, path, "should fall back to existing project after stale default is cleared")

	// Verify stale default was cleared
	var isDefault int
	err = db.QueryRow("SELECT is_default FROM recent_projects WHERE project_path = ?", nonExistentPath).Scan(&isDefault)
	if err == nil { // row may have been removed by GetRecentProjects stale cleanup
		assert.Equal(t, 0, isDefault, "stale default should be cleared")
	}
}

func TestSetDefaultProject_ClearsOldDefault(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dirA := createTempProjectDir(t)
	dirB := createTempProjectDir(t)

	assert.NoError(t, service.AddRecentProject(dirA))
	assert.NoError(t, service.AddRecentProject(dirB))

	// Set dirA as default
	assert.NoError(t, service.SetDefaultProject(dirA))

	// Set dirB as default — should clear dirA's flag
	assert.NoError(t, service.SetDefaultProject(dirB))

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM recent_projects WHERE is_default = 1").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count, "only one project should be default")

	var defaultPath string
	err = db.QueryRow("SELECT project_path FROM recent_projects WHERE is_default = 1").Scan(&defaultPath)
	assert.NoError(t, err)
	assert.Equal(t, dirB, defaultPath, "dirB should be the default")
}

func TestSetDefaultProject_UpsertsProject(t *testing.T) {
	db := setupRecentProjectsDB(t)

	newDir := createTempProjectDir(t)

	// Set default on a project not yet in recent_projects
	assert.NoError(t, service.SetDefaultProject(newDir))

	var defaultPath string
	err := db.QueryRow("SELECT project_path FROM recent_projects WHERE is_default = 1").Scan(&defaultPath)
	assert.NoError(t, err)
	assert.Equal(t, newDir, defaultPath)

	// Verify it's also in recent_projects
	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Contains(t, paths, newDir)
}

func TestRemoveRecentProject_ClearsDefaultFlag(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dirA := createTempProjectDir(t)
	assert.NoError(t, service.AddRecentProject(dirA))
	assert.NoError(t, service.SetDefaultProject(dirA))

	// Verify it's default
	var isDefault int
	err := db.QueryRow("SELECT is_default FROM recent_projects WHERE project_path = ?", dirA).Scan(&isDefault)
	assert.NoError(t, err)
	assert.Equal(t, 1, isDefault)

	// Remove it
	assert.NoError(t, service.RemoveRecentProject(dirA))

	// Verify it's gone
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM recent_projects WHERE project_path = ?", dirA).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "removed project should not be in DB")
}

func TestAddRecentProject_DuplicateDoesNotIncreaseCount(t *testing.T) {
	setupRecentProjectsDB(t)

	dir := createTempProjectDir(t)
	service.AddRecentProject(dir)
	service.AddRecentProject(dir)
	service.AddRecentProject(dir)

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 1) // Still just one entry
}

func TestGetRecentProjects_FiltersNonExistent(t *testing.T) {
	db := setupRecentProjectsDB(t)

	existingDir := createTempProjectDir(t)
	nonExistentPath := "/tmp/clawbench-test-nonexistent-" + fmt.Sprintf("%d", time.Now().UnixNano())

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, existingDir, baseTime)
	insertProjectWithTime(t, db, nonExistentPath, baseTime.Add(1*time.Second))

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Equal(t, []string{existingDir}, paths)

	// Verify the non-existent entry was cleaned from the database
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM recent_projects WHERE project_path = ?", nonExistentPath).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "non-existent project should be removed from DB")
}

func TestGetRecentProjects_FiltersFilePath(t *testing.T) {
	db := setupRecentProjectsDB(t)

	existingDir := createTempProjectDir(t)
	// Create a regular file (not a directory)
	tmpFile, err := os.CreateTemp("", "recent-project-file-*")
	assert.NoError(t, err)
	filePath := tmpFile.Name()
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(filePath) })

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, existingDir, baseTime)
	insertProjectWithTime(t, db, filePath, baseTime.Add(1*time.Second))

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Equal(t, []string{existingDir}, paths)

	// File path should have been removed from the database
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM recent_projects WHERE project_path = ?", filePath).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "file path (not a directory) should be removed from DB")
}

func TestGetRecentProjects_AllNonExistent(t *testing.T) {
	db := setupRecentProjectsDB(t)

	// Insert only non-existent paths
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 3 {
		insertProjectWithTime(t, db, fmt.Sprintf("/tmp/clawbench-nonexistent-%d-%d", time.Now().UnixNano(), i), baseTime.Add(time.Duration(i)*time.Second))
	}

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Empty(t, paths)

	// Database should be empty now
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM recent_projects").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "all stale entries should be removed from DB")
}

func TestGetRecentProjects_DeletedAfterListed(t *testing.T) {
	db := setupRecentProjectsDB(t)

	dir := createTempProjectDir(t)

	// Insert and verify it shows up
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, dir, baseTime)

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Equal(t, []string{dir}, paths)

	// Now delete the directory on disk
	_ = os.RemoveAll(dir)

	// GetRecentProjects should filter it out and clean up the database
	paths, err = service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Empty(t, paths)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM recent_projects WHERE project_path = ?", dir).Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 0, count, "deleted project should be removed from DB")
}

func TestGetRecentProjects_DBQueryError(t *testing.T) {
	// Set up a closed DB to trigger query errors
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(cleanup)

	// Close the DB before querying to trigger an error
	db.Close()

	_, err = service.GetRecentProjects()
	assert.Error(t, err, "should return error when DB query fails")
}

func TestGetRecentProjects_RemoveStaleFails(t *testing.T) {
	// Set up DB but close it mid-way to make RemoveRecentProject fail
	db := setupRecentProjectsDB(t)

	existingDir := createTempProjectDir(t)
	nonExistentPath := "/tmp/clawbench-stale-remove-fail-" + fmt.Sprintf("%d", time.Now().UnixNano())

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insertProjectWithTime(t, db, existingDir, baseTime)
	insertProjectWithTime(t, db, nonExistentPath, baseTime.Add(1*time.Second))

	// Close the write DB to make RemoveRecentProject fail
	// but keep the read DB working for the query
	closedDB, _ := sql.Open("sqlite", ":memory:")
	closedDB.Close()
	cleanup := service.SetDBForTest(closedDB, db)
	t.Cleanup(cleanup)

	// GetRecentProjects should still succeed (return valid paths)
	// even though RemoveRecentProject fails for stale entries
	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Equal(t, []string{existingDir}, paths)
}

func TestAddRecentProject_DBError(t *testing.T) {
	// Set up a closed DB to trigger exec errors
	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)

	cleanup := service.SetDBForTest(db, db)
	t.Cleanup(cleanup)

	// Close the DB before operation
	db.Close()

	err = service.AddRecentProject("/some/path")
	assert.Error(t, err, "should return error when DB exec fails")
}

// --- Configurable limit tests ---

func TestGetRecentProjects_ConfigurableLimit(t *testing.T) {
	setupRecentProjectsDB(t)

	// Set limit to 3
	origLimit := model.RecentProjectsMaxCount
	model.RecentProjectsMaxCount = 3
	t.Cleanup(func() { model.RecentProjectsMaxCount = origLimit })

	// Add 5 projects
	for range 5 {
		dir := createTempProjectDir(t)
		err := service.AddRecentProject(dir)
		assert.NoError(t, err)
	}

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 3, "should return at most 3 projects when limit is 3")
}

func TestAddRecentProject_PruneToConfigurableLimit(t *testing.T) {
	setupRecentProjectsDB(t)

	// Set limit to 5
	origLimit := model.RecentProjectsMaxCount
	model.RecentProjectsMaxCount = 5
	t.Cleanup(func() { model.RecentProjectsMaxCount = origLimit })

	// Add 8 projects
	for range 8 {
		dir := createTempProjectDir(t)
		err := service.AddRecentProject(dir)
		assert.NoError(t, err)
	}

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 5, "should be pruned to 5 when limit is 5")
}

func TestGetRecentProjects_FallbackToDefaultLimit(t *testing.T) {
	setupRecentProjectsDB(t)

	// Set limit to 0 (should fallback to 10)
	origLimit := model.RecentProjectsMaxCount
	model.RecentProjectsMaxCount = 0
	t.Cleanup(func() { model.RecentProjectsMaxCount = origLimit })

	// Add 12 projects
	for range 12 {
		dir := createTempProjectDir(t)
		err := service.AddRecentProject(dir)
		assert.NoError(t, err)
	}

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 10, "should fallback to default 10 when limit is 0")
}

func TestAddRecentProject_PruneKeepsMostRecentWithCustomLimit(t *testing.T) {
	db := setupRecentProjectsDB(t)

	// Set limit to 3
	origLimit := model.RecentProjectsMaxCount
	model.RecentProjectsMaxCount = 3
	t.Cleanup(func() { model.RecentProjectsMaxCount = origLimit })

	// Insert 3 projects with explicit timestamps
	dirs := make([]string, 0, 3)
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 3 {
		dir := createTempProjectDir(t)
		dirs = append(dirs, dir)
		insertProjectWithTime(t, db, dir, baseTime.Add(time.Duration(i)*time.Second))
	}

	// Add a 4th via service — should prune the oldest (dirs[0])
	newDir := createTempProjectDir(t)
	err := service.AddRecentProject(newDir)
	assert.NoError(t, err)

	paths, err := service.GetRecentProjects()
	assert.NoError(t, err)
	assert.Len(t, paths, 3)

	// dirs[0] should have been pruned (oldest timestamp)
	for _, p := range paths {
		assert.NotEqual(t, dirs[0], p)
	}

	// newDir should be present
	found := false
	for _, p := range paths {
		if p == newDir {
			found = true
			break
		}
	}
	assert.True(t, found, "new project should be in the list")
}
