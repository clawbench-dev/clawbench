//nolint:noctx // db global singleton, context not applicable
package service

import (
	"database/sql"
	"errors"
	"time"
)

// ForgeItemsDDL creates the forge_items snapshot table.
//
// One row per (platform, host, owner, repo, item_type, number). The snapshot is
// the basis for event derivation: the poller compares the freshly fetched state
// against the stored row and emits created/closed/merged/reopened/commented.
// Exported so tests can create the table in their own databases.
const ForgeItemsDDL = `
CREATE TABLE IF NOT EXISTS forge_items (
	platform                TEXT NOT NULL,
	host                    TEXT NOT NULL,
	owner                   TEXT NOT NULL,
	repo                    TEXT NOT NULL,
	item_type               TEXT NOT NULL,
	number                  INTEGER NOT NULL,
	state                   TEXT NOT NULL,
	merged                  INTEGER NOT NULL DEFAULT 0,
	last_comment_id         INTEGER NOT NULL DEFAULT 0,
	last_comment_updated_at DATETIME,
	comments_baselined      INTEGER NOT NULL DEFAULT 0,
	item_updated_at         DATETIME,
	seen_at                 DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (platform, host, owner, repo, item_type, number)
);
CREATE INDEX IF NOT EXISTS idx_forge_items_repo ON forge_items(platform, host, owner, repo);
`

// ForgeItemSnapshot is the persisted state of a single issue or PR, used to
// derive events by comparison on the next poll.
type ForgeItemSnapshot struct {
	Platform             string
	Host                 string
	Owner                string
	Repo                 string
	ItemType             string
	Number               int
	State                string
	Merged               bool
	LastCommentID        int64
	LastCommentUpdatedAt time.Time
	// CommentsBaselined records that this item's comment history has been
	// recorded at least once. Until it is true, comment activity must never be
	// reported: the item's "previous" comment state is unknown, not empty.
	//
	// This is separate from LastCommentID because an item can legitimately have
	// zero comments. Without it, a zero baseline is indistinguishable from
	// "never fetched comments", and every historical comment on an item whose
	// baseline pass skipped comments would be replayed as new.
	CommentsBaselined bool
	ItemUpdatedAt     time.Time
}

// ForgeRepoKey identifies a repository for snapshot scoping.
type ForgeRepoKey struct {
	Platform string
	Host     string
	Owner    string
	Repo     string
}

// GetForgeItemSnapshot loads the stored snapshot for an item, or (nil, nil) when
// none exists (a first sighting).
func GetForgeItemSnapshot(repo ForgeRepoKey, itemType string, number int) (*ForgeItemSnapshot, error) {
	if dbRead == nil {
		return nil, nil
	}
	row := dbRead.QueryRow(
		`SELECT platform, host, owner, repo, item_type, number, state, merged,
		        last_comment_id, last_comment_updated_at, comments_baselined, item_updated_at
		 FROM forge_items
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND item_type = ? AND number = ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, itemType, number,
	)
	var s ForgeItemSnapshot
	var lastCommentUpdated, itemUpdated sql.NullTime
	err := row.Scan(&s.Platform, &s.Host, &s.Owner, &s.Repo, &s.ItemType, &s.Number,
		&s.State, &s.Merged, &s.LastCommentID, &lastCommentUpdated, &s.CommentsBaselined, &itemUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastCommentUpdated.Valid {
		s.LastCommentUpdatedAt = lastCommentUpdated.Time
	}
	if itemUpdated.Valid {
		s.ItemUpdatedAt = itemUpdated.Time
	}
	return &s, nil
}

// UpsertForgeItemSnapshot writes the latest observed state for an item.
func UpsertForgeItemSnapshot(s ForgeItemSnapshot) error {
	if db == nil {
		return nil
	}
	merged := 0
	if s.Merged {
		merged = 1
	}
	baselined := 0
	if s.CommentsBaselined {
		baselined = 1
	}
	var lastCommentUpdated, itemUpdated any
	if !s.LastCommentUpdatedAt.IsZero() {
		lastCommentUpdated = s.LastCommentUpdatedAt.UTC()
	}
	if !s.ItemUpdatedAt.IsZero() {
		itemUpdated = s.ItemUpdatedAt.UTC()
	}
	_, err := WriteExec(
		`INSERT INTO forge_items
		   (platform, host, owner, repo, item_type, number, state, merged,
		    last_comment_id, last_comment_updated_at, comments_baselined, item_updated_at, seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(platform, host, owner, repo, item_type, number) DO UPDATE SET
		   state = excluded.state,
		   merged = excluded.merged,
		   last_comment_id = excluded.last_comment_id,
		   last_comment_updated_at = excluded.last_comment_updated_at,
		   comments_baselined = excluded.comments_baselined,
		   item_updated_at = excluded.item_updated_at,
		   seen_at = CURRENT_TIMESTAMP`,
		s.Platform, s.Host, s.Owner, s.Repo, s.ItemType, s.Number,
		s.State, merged, s.LastCommentID, lastCommentUpdated, baselined, itemUpdated,
	)
	return err
}

// PruneForgeItems deletes snapshot rows for a repo that were not seen since the
// given time, removing items deleted on the remote side and preventing unbounded
// growth. Callers pass the timestamp of the most recent full scan.
func PruneForgeItems(repo ForgeRepoKey, seenBefore time.Time) (int64, error) {
	if db == nil {
		return 0, nil
	}
	res, err := WriteExec(
		`DELETE FROM forge_items
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND seen_at < ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, seenBefore.UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListForgeItemSnapshots returns every snapshot row for a repository. Used by
// tests and by the prune path to reason about the stored set.
func ListForgeItemSnapshots(repo ForgeRepoKey) ([]ForgeItemSnapshot, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT platform, host, owner, repo, item_type, number, state, merged,
		        last_comment_id, last_comment_updated_at, item_updated_at
		 FROM forge_items
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ForgeItemSnapshot
	for rows.Next() {
		var s ForgeItemSnapshot
		var lastCommentUpdated, itemUpdated sql.NullTime
		if err := rows.Scan(&s.Platform, &s.Host, &s.Owner, &s.Repo, &s.ItemType, &s.Number,
			&s.State, &s.Merged, &s.LastCommentID, &lastCommentUpdated, &itemUpdated); err != nil {
			return nil, err
		}
		if lastCommentUpdated.Valid {
			s.LastCommentUpdatedAt = lastCommentUpdated.Time
		}
		if itemUpdated.Valid {
			s.ItemUpdatedAt = itemUpdated.Time
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetForgeSyncWatermark records the sync watermark for a repository. It is a
// separate table so the watermark can advance only after a complete paginated
// fetch (see the design's watermark-correctness rule).
func SetForgeSyncWatermark(repo ForgeRepoKey, watermark time.Time) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`INSERT INTO forge_sync_state (platform, host, owner, repo, watermark, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(platform, host, owner, repo) DO UPDATE SET
		   watermark = excluded.watermark,
		   updated_at = CURRENT_TIMESTAMP`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, watermark.UTC(),
	)
	return err
}

// GetForgeSyncWatermark returns the stored watermark for a repository, or the
// zero time when none is recorded (a first sync).
func GetForgeSyncWatermark(repo ForgeRepoKey) (time.Time, error) {
	if dbRead == nil {
		return time.Time{}, nil
	}
	var watermark sql.NullTime
	err := dbRead.QueryRow(
		`SELECT watermark FROM forge_sync_state
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
	).Scan(&watermark)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if watermark.Valid {
		return watermark.Time, nil
	}
	return time.Time{}, nil
}

// ForgeSyncStateDDL creates the per-repo sync watermark table.
const ForgeSyncStateDDL = `
CREATE TABLE IF NOT EXISTS forge_sync_state (
	platform   TEXT NOT NULL,
	host       TEXT NOT NULL,
	owner      TEXT NOT NULL,
	repo       TEXT NOT NULL,
	watermark  DATETIME,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (platform, host, owner, repo)
);
`

// ForgePipelineRunsDDL creates the per-run ledger for CI events.
//
// One row per (repo, run id), meaning "this run has been handled and must never
// be dispatched". It serves two purposes at once:
//
//   - baseline: on a repository's first sync, already-finished runs are
//     recorded WITHOUT dispatching, so a fresh install does not fire a task for
//     every historical run;
//   - dedupe: a run recorded once is never dispatched again.
//
// There is deliberately no "dispatched" column: a baseline row and a dispatched
// row are the same thing to every reader — a run that must not fire again — so
// the flag would always be misleading in one of the two cases.
//
// A single scalar "last seen run id" watermark would be WRONG here. A run that
// is still in progress at first sight is deliberately not recorded, so run 101
// finishing first would advance the watermark past run 100; when 100 later
// finishes it would be below the watermark and silently dropped. A row per run
// id is immune to that, and to out-of-order completion and pagination.
const ForgePipelineRunsDDL = `
CREATE TABLE IF NOT EXISTS forge_pipeline_runs (
	platform TEXT NOT NULL,
	host     TEXT NOT NULL,
	owner    TEXT NOT NULL,
	repo     TEXT NOT NULL,
	run_id   INTEGER NOT NULL,
	seen_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (platform, host, owner, repo, run_id)
);
CREATE INDEX IF NOT EXISTS idx_forge_pipeline_runs_seen ON forge_pipeline_runs(platform, host, owner, repo, seen_at);
`

// RecordPipelineRun records that a run has been handled, returning true when
// this is the first sighting. A false return means the run was already recorded
// and must not be dispatched again.
//
// Freshness is determined by an explicit existence check rather than by
// RowsAffected: the insert is a no-op on conflict, so an affected-row count
// would be indistinguishable from a silent conflict.
func RecordPipelineRun(repo ForgeRepoKey, runID int64) (bool, error) {
	if db == nil {
		return false, nil
	}
	seen, err := HasPipelineRun(repo, runID)
	if err != nil {
		return false, err
	}
	if seen {
		// Re-seeing a run refreshes seen_at so a run still inside the overlap
		// window is not pruned while it is still current.
		_, err = WriteExec(
			`UPDATE forge_pipeline_runs SET seen_at = CURRENT_TIMESTAMP
			 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND run_id = ?`,
			repo.Platform, repo.Host, repo.Owner, repo.Repo, runID,
		)
		return false, err
	}

	_, err = WriteExec(
		`INSERT INTO forge_pipeline_runs
		   (platform, host, owner, repo, run_id, seen_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(platform, host, owner, repo, run_id) DO NOTHING`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, runID,
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

// HasPipelineRun reports whether a run has already been recorded.
func HasPipelineRun(repo ForgeRepoKey, runID int64) (bool, error) {
	if dbRead == nil {
		return false, nil
	}
	var n int
	err := dbRead.QueryRow(
		`SELECT COUNT(*) FROM forge_pipeline_runs
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND run_id = ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, runID,
	).Scan(&n)
	return n > 0, err
}

// PruneForgePipelineRuns deletes run rows not seen since the given time,
// bounding growth. Callers pass the timestamp of the most recent full scan,
// mirroring PruneForgeItems.
func PruneForgePipelineRuns(repo ForgeRepoKey, seenBefore time.Time) (int64, error) {
	if db == nil {
		return 0, nil
	}
	res, err := WriteExec(
		`DELETE FROM forge_pipeline_runs
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND seen_at < ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, seenBefore.UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListForgePipelineRuns returns every recorded run id for a repository, for
// tests and diagnostics.
func ListForgePipelineRuns(repo ForgeRepoKey) ([]int64, error) {
	if dbRead == nil {
		return nil, nil
	}
	rows, err := dbRead.Query(
		`SELECT run_id FROM forge_pipeline_runs
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? ORDER BY run_id`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ForgeEventDDL creates the derived-event table. Events are persisted so that
// notification and unread counting survive a restart, and so the same event is
// never dispatched twice (event-level dedupe).
const ForgeEventDDL = `
CREATE TABLE IF NOT EXISTS forge_events (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	platform       TEXT NOT NULL,
	host           TEXT NOT NULL,
	owner          TEXT NOT NULL,
	repo           TEXT NOT NULL,
	item_type      TEXT NOT NULL,
	number         INTEGER NOT NULL,
	event_type     TEXT NOT NULL,
	dedupe_key     TEXT NOT NULL,
	payload        TEXT NOT NULL DEFAULT '',
	read_at        DATETIME,
	created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_forge_events_dedupe ON forge_events(dedupe_key);
CREATE INDEX IF NOT EXISTS idx_forge_events_unread ON forge_events(read_at);
CREATE INDEX IF NOT EXISTS idx_forge_events_repo ON forge_events(platform, host, owner, repo, id);
`

// ForgeEvent is a derived change event persisted for notification + unread.
type ForgeEvent struct {
	ID        int64
	Platform  string
	Host      string
	Owner     string
	Repo      string
	ItemType  string
	Number    int
	EventType string
	DedupeKey string
	Payload   string
	ReadAt    sql.NullTime
	CreatedAt time.Time
}

// InsertForgeEvent persists a derived event, ignoring duplicates by dedupe key.
// Returns true when the row was newly inserted (i.e. the event is fresh).
func InsertForgeEvent(e ForgeEvent) (bool, error) {
	if db == nil {
		return false, nil
	}
	res, err := WriteExec(
		`INSERT INTO forge_events
		   (platform, host, owner, repo, item_type, number, event_type, dedupe_key, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(dedupe_key) DO NOTHING`,
		e.Platform, e.Host, e.Owner, e.Repo, e.ItemType, e.Number, e.EventType, e.DedupeKey, e.Payload,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountUnreadForgeEvents returns the number of unread events. The unread count
// is intentionally independent of the notification toggles: it answers "are
// there new changes", not "did we notify".
func CountUnreadForgeEvents() (int, error) {
	if dbRead == nil {
		return 0, nil
	}
	var n int
	err := dbRead.QueryRow(`SELECT COUNT(*) FROM forge_events WHERE read_at IS NULL`).Scan(&n)
	return n, err
}

// MarkForgeEventsRead marks every unread event as read (called when the user
// opens the Issues & PRs tab).
func MarkForgeEventsRead() error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(`UPDATE forge_events SET read_at = CURRENT_TIMESTAMP WHERE read_at IS NULL`)
	return err
}
