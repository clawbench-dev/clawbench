package service_test

import (
	"database/sql"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/store"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recentActivitySchema is the minimum the recency query touches: the projects
// registry plus chat_sessions with the columns it filters and aggregates on.
const recentActivitySchema = store.ProjectsDDL + `
CREATE TABLE IF NOT EXISTS chat_sessions (
	id TEXT PRIMARY KEY,
	project_id INTEGER NOT NULL,
	backend TEXT NOT NULL DEFAULT 'cli',
	title TEXT NOT NULL DEFAULT '',
	session_type TEXT NOT NULL DEFAULT 'chat',
	archived INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func setupRecentActivityDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// :memory: is per-connection — pin the pool so the schema and the query see
	// the same database.
	db.SetMaxOpenConns(1)
	_, err = db.Exec(recentActivitySchema)
	require.NoError(t, err)

	cleanup := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		cleanup()
		_ = db.Close()
	})
	return db
}

// insertSessionAt adds a session row for the project at `path` whose
// updated_at is the given instant, formatted the way SQLite stores it.
func insertSessionAt(t *testing.T, db *sql.DB, path, id string, updatedAt time.Time, opts ...func(*map[string]any)) {
	t.Helper()
	fields := map[string]any{
		"session_type": "chat",
		"archived":     0,
	}
	for _, opt := range opts {
		opt(&fields)
	}
	_, err := db.Exec(
		`INSERT INTO chat_sessions (id, project_id, session_type, archived, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, projectID(t, path), fields["session_type"], fields["archived"],
		updatedAt.UTC().Format("2006-01-02 15:04:05"),
	)
	require.NoError(t, err)
}

func TestListRecentlyActiveProjects_Empty(t *testing.T) {
	setupRecentActivityDB(t)

	got, err := service.ListRecentlyActiveProjects("")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListRecentlyActiveProjects_ReturnsPathAndNewestActivity(t *testing.T) {
	db := setupRecentActivityDB(t)
	dir := createTempProjectDir(t)
	now := time.Now().UTC()

	insertSessionAt(t, db, dir, "s-old", now.Add(-2*time.Hour))
	insertSessionAt(t, db, dir, "s-new", now.Add(-30*time.Minute))

	got, err := service.ListRecentlyActiveProjects("")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, dir, got[0].Path)
	assert.Equal(t,
		now.Add(-30*time.Minute).Format("2006-01-02 15:04:05"),
		got[0].LastActiveAt,
		"the card's timestamp is the project's newest session")
}

func TestListRecentlyActiveProjects_ExcludesOutsideWindow(t *testing.T) {
	db := setupRecentActivityDB(t)
	fresh := createTempProjectDir(t)
	stale := createTempProjectDir(t)
	now := time.Now().UTC()

	insertSessionAt(t, db, fresh, "fresh", now.Add(-time.Hour))
	// One second past the window: the boundary must be inclusive of "inside"
	// and exclusive of "outside", so this row is the sharp case.
	insertSessionAt(t, db, stale, "stale", now.Add(-service.DefaultRecentProjectActivityWindow-time.Second))

	got, err := service.ListRecentlyActiveProjects("")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, fresh, got[0].Path)
}

// The window is a setting, not a constant: widening it must pull in projects
// that were previously too old, without touching the query itself.
func TestListRecentlyActiveProjects_HonoursConfiguredWindow(t *testing.T) {
	db := setupRecentActivityDB(t)
	old := createTempProjectDir(t)
	now := time.Now().UTC()

	// Two days old: outside the default 24h window.
	insertSessionAt(t, db, old, "two-days", now.Add(-48*time.Hour))

	orig := model.ChatRecentProjectCardsWindowHours
	t.Cleanup(func() { model.ChatRecentProjectCardsWindowHours = orig })

	model.ChatRecentProjectCardsWindowHours = 24
	got, err := service.ListRecentlyActiveProjects("")
	require.NoError(t, err)
	assert.Empty(t, got, "48h-old activity is outside the default 24h window")

	model.ChatRecentProjectCardsWindowHours = 72
	got, err = service.ListRecentlyActiveProjects("")
	require.NoError(t, err)
	require.Len(t, got, 1, "the same project becomes recent once the window is widened")
	assert.Equal(t, old, got[0].Path)
}

// A non-positive configured window falls back to the default instead of
// reading as "nothing is ever recent" (which would silently hide every card).
func TestListRecentlyActiveProjects_NonPositiveWindowFallsBack(t *testing.T) {
	db := setupRecentActivityDB(t)
	dir := createTempProjectDir(t)
	now := time.Now().UTC()

	insertSessionAt(t, db, dir, "recent", now.Add(-time.Hour))

	orig := model.ChatRecentProjectCardsWindowHours
	t.Cleanup(func() { model.ChatRecentProjectCardsWindowHours = orig })

	for _, bad := range []int{0, -5} {
		model.ChatRecentProjectCardsWindowHours = bad
		got, err := service.ListRecentlyActiveProjects("")
		require.NoError(t, err)
		assert.Len(t, got, 1, "window=%d must fall back to the default, not hide everything", bad)
	}
}

func TestListRecentlyActiveProjects_OrdersByMostRecentFirst(t *testing.T) {
	db := setupRecentActivityDB(t)
	older := createTempProjectDir(t)
	newer := createTempProjectDir(t)
	now := time.Now().UTC()

	insertSessionAt(t, db, older, "older", now.Add(-5*time.Hour))
	insertSessionAt(t, db, newer, "newer", now.Add(-1*time.Hour))

	got, err := service.ListRecentlyActiveProjects("")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{newer, older}, []string{got[0].Path, got[1].Path})
}

func TestListRecentlyActiveProjects_SkipsExcludedProject(t *testing.T) {
	db := setupRecentActivityDB(t)
	current := createTempProjectDir(t)
	other := createTempProjectDir(t)
	now := time.Now().UTC()

	insertSessionAt(t, db, current, "cur", now.Add(-time.Hour))
	insertSessionAt(t, db, other, "oth", now.Add(-time.Hour))

	got, err := service.ListRecentlyActiveProjects(current)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, other, got[0].Path, "the caller's own project is already the main list")
}

func TestListRecentlyActiveProjects_SkipsArchivedAndNonInteractiveRows(t *testing.T) {
	db := setupRecentActivityDB(t)
	dir := createTempProjectDir(t)
	now := time.Now().UTC()

	insertSessionAt(t, db, dir, "archived", now.Add(-time.Hour), func(f *map[string]any) {
		(*f)["archived"] = 1
	})
	// A hidden group_member row must not make its project look recently used:
	// the user never sees those rows, so a card for them would be a dead end.
	insertSessionAt(t, db, dir, "member", now.Add(-time.Hour), func(f *map[string]any) {
		(*f)["session_type"] = "group_member"
	})

	got, err := service.ListRecentlyActiveProjects("")
	require.NoError(t, err)
	assert.Empty(t, got)
}
