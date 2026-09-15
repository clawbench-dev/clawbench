//nolint:noctx // db global singleton, context not applicable
package service

import (
	"database/sql"
	"errors"
	"time"

	"clawbench/internal/forge"
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
// Freshness comes from RowsAffected on the INSERT itself, which is a single
// atomic statement: `ON CONFLICT DO NOTHING` reports 1 row for an insert and 0
// for a conflict, so the answer cannot go stale between a check and a write. A
// separate existence query followed by an insert would leave that window open.
func RecordPipelineRun(repo ForgeRepoKey, runID int64) (bool, error) {
	if db == nil {
		return false, nil
	}
	res, err := WriteExec(
		`INSERT INTO forge_pipeline_runs
		   (platform, host, owner, repo, run_id, seen_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(platform, host, owner, repo, run_id) DO NOTHING`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, runID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}

	// Already recorded. Refresh seen_at so a run still inside the overlap window
	// is not pruned while it is still current.
	_, err = WriteExec(
		`UPDATE forge_pipeline_runs SET seen_at = CURRENT_TIMESTAMP
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND run_id = ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, runID,
	)
	return false, err
}

// HasAnyPipelineRun reports whether ANY run has been recorded for a repository,
// i.e. whether the CI baseline has been established.
//
// This is deliberately separate from the item watermark: a repository can have
// been polled for months with no pipeline subscriber, so its item watermark is
// set while its CI ledger is empty. Deciding "is this the CI baseline?" from the
// item watermark would then answer "no" and dispatch every historical run whose
// timestamp happens to be newer than that watermark.
func HasAnyPipelineRun(repo ForgeRepoKey) (bool, error) {
	if dbRead == nil {
		return false, nil
	}
	var n int
	err := dbRead.QueryRow(
		`SELECT COUNT(*) FROM forge_pipeline_runs
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
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
//
// item_key identifies the THING the event is about, and is what makes the unread
// badge answer "how many items have new activity" rather than "how many events
// happened". It cannot be derived from (item_type, number) because a pipeline
// event carries Number 0 for every run — the run id only exists inside
// dedupe_key. So the key is stored explicitly:
//
//	issue/<number>, pr/<number>, pipeline/run:<runID>
const ForgeEventDDL = `
CREATE TABLE IF NOT EXISTS forge_events (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	platform       TEXT NOT NULL,
	host           TEXT NOT NULL,
	owner          TEXT NOT NULL,
	repo           TEXT NOT NULL,
	item_type      TEXT NOT NULL,
	number         INTEGER NOT NULL,
	item_key       TEXT NOT NULL DEFAULT '',
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

// ForgeEventItemIndexDDL creates the item_key index.
//
// It is kept OUT of ForgeEventDDL on purpose: that constant runs as one batch,
// and on an existing database `CREATE TABLE IF NOT EXISTS` is a no-op, so an
// index over item_key would be created before the migration has added the
// column — failing the whole batch with "no such column". The migration adds the
// column first, then runs this.
const ForgeEventItemIndexDDL = `
CREATE INDEX IF NOT EXISTS idx_forge_events_item ON forge_events(platform, host, owner, repo, item_key, read_at);
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
	ItemKey   string
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
		   (platform, host, owner, repo, item_type, number, item_key, event_type, dedupe_key, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(dedupe_key) DO NOTHING`,
		e.Platform, e.Host, e.Owner, e.Repo, e.ItemType, e.Number, e.ItemKey, e.EventType, e.DedupeKey, e.Payload,
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

// CountUnreadForgeEvents returns the number of unread ITEMS for one repository,
// i.e. how many distinct things have new activity.
//
// Distinct-by-item (not by event) is what makes the dock badge match what the
// user can actually see: three comments on one issue is one unread row, not
// three. It is scoped to a repo because the panel it points at is a single
// project's bound repository — a global count is a number the user cannot act
// on.
//
// The unread count is intentionally independent of the notification toggles: it
// answers "are there new changes", not "did we notify".
func CountUnreadForgeEvents(repo ForgeRepoKey) (int, error) {
	if dbRead == nil {
		return 0, nil
	}
	var n int
	err := dbRead.QueryRow(
		`SELECT COUNT(DISTINCT item_key) FROM forge_events
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ?
		   AND read_at IS NULL AND item_key != ''`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
	).Scan(&n)
	return n, err
}

// UnreadForgeItemKeys returns the set of item keys with at least one unread
// event in this repository.
//
// The list endpoints use this to tag each row, so a row's unread dot and the
// dock badge are computed from the same rows and cannot disagree.
func UnreadForgeItemKeys(repo ForgeRepoKey) (map[string]bool, error) {
	out := make(map[string]bool)
	if dbRead == nil {
		return out, nil
	}
	rows, err := dbRead.Query(
		`SELECT DISTINCT item_key FROM forge_events
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ?
		   AND read_at IS NULL AND item_key != ''`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

// UnreadForgeItem is one row of the unread overview: a single thing (issue, PR
// or CI run) with at least one unread event.
type UnreadForgeItem struct {
	// ItemKey is the identity the read endpoint expects. Callers must pass it
	// back verbatim rather than rebuilding it from Type/Number: a pipeline's
	// Number is 0, so a rebuilt key would be "pipeline/0" and match nothing.
	ItemKey string
	// ItemType is "issue", "pr" (change_request) or "pipeline".
	ItemType string
	// Number is the issue/PR number. It is ALWAYS 0 for a pipeline.
	Number int
	// RunID is the CI run id, parsed from ItemKey. Only set for pipelines.
	RunID int64
	// EventType is the newest event's type ("commented", "closed",
	// "pipeline_done", …), used to label why the row is unread.
	EventType string
	// Payload is the item URL.
	Payload string
	// EventCount is how many events this item has in total.
	EventCount int
	CreatedAt  time.Time
}

// UnreadForgeItems lists the items with unread activity in one repository,
// newest activity first, for the unread-overview panel.
//
// This is a separate query from CountUnreadForgeEvents on purpose: the count is
// fetched on every live event and on every project switch, so it must stay O(1);
// the rows are only needed while the overview is on screen.
//
// The newest event per item is selected with a MAX(id) self-join rather than a
// bare MAX(id) alongside the other columns — SQLite does not define which row's
// non-aggregated columns a bare MAX returns. The join is exact, and MAX(id) is
// guaranteed to be an unread row: MarkForgeEventsRead sets read_at on every
// unread row of an item at once, and ids only grow, so if the newest row were
// read the item would have no unread rows left and be excluded by the HAVING.
func UnreadForgeItems(repo ForgeRepoKey, limit int) ([]UnreadForgeItem, error) {
	out := []UnreadForgeItem{}
	if dbRead == nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 200
	}
	rows, err := dbRead.Query(
		`SELECT e.item_key, e.item_type, e.number, e.event_type, e.payload,
		        e.created_at, g.event_count
		   FROM forge_events e
		   JOIN (
		         SELECT item_key, COUNT(*) AS event_count, MAX(id) AS last_id
		           FROM forge_events
		          WHERE platform = ? AND host = ? AND owner = ? AND repo = ?
		            AND item_key != ''
		          GROUP BY item_key
		         HAVING SUM(CASE WHEN read_at IS NULL THEN 1 ELSE 0 END) > 0
		        ) g
		     ON g.item_key = e.item_key AND e.id = g.last_id
		  WHERE e.platform = ? AND e.host = ? AND e.owner = ? AND e.repo = ?
		  ORDER BY e.created_at DESC, e.id DESC
		  LIMIT ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
		repo.Platform, repo.Host, repo.Owner, repo.Repo,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var it UnreadForgeItem
		if err := rows.Scan(&it.ItemKey, &it.ItemType, &it.Number, &it.EventType,
			&it.Payload, &it.CreatedAt, &it.EventCount); err != nil {
			return nil, err
		}
		// The run id only exists inside the key, and only for pipelines.
		if it.ItemType == string(forge.ItemTypePipeline) {
			if id, ok := forge.ParsePipelineRunID(it.ItemKey); ok {
				it.RunID = id
			}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// MarkForgeEventsRead marks unread events as read.
//
// An empty itemKey marks the whole repository (the "mark all read" action); a
// specific key marks just that item (opening one row). Only existing rows are
// touched, so a later event for the same item starts unread again — no watermark
// bookkeeping is needed to re-arm it.
func MarkForgeEventsRead(repo ForgeRepoKey, itemKey string) error {
	if db == nil {
		return nil
	}
	if itemKey == "" {
		_, err := WriteExec(
			`UPDATE forge_events SET read_at = CURRENT_TIMESTAMP
			 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND read_at IS NULL`,
			repo.Platform, repo.Host, repo.Owner, repo.Repo,
		)
		return err
	}
	_, err := WriteExec(
		`UPDATE forge_events SET read_at = CURRENT_TIMESTAMP
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ?
		   AND item_key = ? AND read_at IS NULL`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, itemKey,
	)
	return err
}

// SetForgeEventCreatedAtForTest backdates one item's events so a test can
// exercise the retention cutoff without waiting. Test-only.
func SetForgeEventCreatedAtForTest(repo ForgeRepoKey, itemKey string, at time.Time) error {
	if db == nil {
		return nil
	}
	_, err := WriteExec(
		`UPDATE forge_events SET created_at = ?
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ? AND item_key = ?`,
		at, repo.Platform, repo.Host, repo.Owner, repo.Repo, itemKey,
	)
	return err
}

// PruneForgeEvents deletes events older than the cutoff.
//
// Only READ rows are deleted. An unread row is the user's only record that
// something changed, so dropping one would silently lower the badge without the
// user ever having seen it.
//
// The row-level dedupe key is what stops a pruned event from being re-dispatched
// if the provider still reports the change: deleting the row re-arms that dedupe,
// which is why the cutoff is far longer than any overlap window.
func PruneForgeEvents(repo ForgeRepoKey, createdBefore time.Time) (int64, error) {
	if db == nil {
		return 0, nil
	}
	res, err := WriteExec(
		`DELETE FROM forge_events
		 WHERE platform = ? AND host = ? AND owner = ? AND repo = ?
		   AND read_at IS NOT NULL AND created_at < ?`,
		repo.Platform, repo.Host, repo.Owner, repo.Repo, createdBefore.UTC(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
