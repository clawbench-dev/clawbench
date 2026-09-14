package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"clawbench/internal/forge"
)

// ForgeProviderFactory builds a read-only Provider for a bound repository. It is
// injected so the service package never imports the handler (which owns the
// credential + TLS policy) and so tests can substitute a fake.
type ForgeProviderFactory func(pf ProjectForge) (forge.Provider, error)

// ForgeChangeSink receives derived events. The poller owns derivation; the sink
// owns dispatch (notification + task triggering), keeping the two concerns
// independent as the design requires.
type ForgeChangeSink interface {
	// HandleChange is called once per freshly derived, not-yet-seen event.
	HandleChange(ctx context.Context, repo ForgeRepoRef, item forge.Item, change forge.Change)
}

// ForgeSyncer performs one incremental sync of a repository: it fetches items
// changed since the stored watermark, derives events by comparing against the
// snapshot table, persists snapshots, and hands fresh events to the sink.
type ForgeSyncer struct {
	factory ForgeProviderFactory
	sink    ForgeChangeSink
	// overlapWindow is subtracted from the stored watermark when querying, so an
	// item updated exactly at the boundary is re-fetched rather than missed.
	// Event-level dedupe makes the resulting re-derivation harmless.
	overlapWindow time.Duration
	// now is injectable for tests.
	now func() time.Time
}

// NewForgeSyncer builds a syncer.
func NewForgeSyncer(factory ForgeProviderFactory, sink ForgeChangeSink) *ForgeSyncer {
	return &ForgeSyncer{
		factory:       factory,
		sink:          sink,
		overlapWindow: time.Second,
		now:           time.Now,
	}
}

// SyncOptions controls what a sync pass fetches. Comments are the noisiest and
// least urgent signal, so the poller fetches them on a longer interval than
// state transitions — this flag is what makes that降频 real rather than
// cosmetic.
type SyncOptions struct {
	// IncludeComments fetches per-item comment activity (the expensive part).
	IncludeComments bool
	// IncludePipelines fetches CI run state. It is off by default because it
	// costs an extra API call per repository, and the poller only turns it on
	// when some active event task actually subscribes to a pipeline event.
	IncludePipelines bool
}

// SyncRepo runs one incremental sync for a repository binding.
//
// Watermark correctness: the watermark advances only after EVERY page of the
// incremental fetch has been consumed. A mid-pagination failure leaves the
// watermark untouched so the next run retries the same window — never skipping
// changes that were not fully read.
func (s *ForgeSyncer) SyncRepo(ctx context.Context, pf ProjectForge) error {
	return s.SyncRepoWithOptions(ctx, pf, SyncOptions{IncludeComments: true})
}

// SyncRepoWithOptions runs one incremental sync with explicit options.
func (s *ForgeSyncer) SyncRepoWithOptions(ctx context.Context, pf ProjectForge, opts SyncOptions) error {
	repoKey := ForgeRepoKey{Platform: pf.Platform, Host: pf.Host, Owner: pf.Owner, Repo: pf.Repo}
	repoRef := ForgeRepoRef{Platform: pf.Platform, Host: pf.Host, Owner: pf.Owner, Repo: pf.Repo}
	remote := forge.Remote{
		Platform: forge.Platform(pf.Platform),
		Host:     pf.Host,
		Owner:    pf.Owner,
		Repo:     pf.Repo,
	}

	provider, err := s.factory(pf)
	if err != nil {
		return fmt.Errorf("build provider: %w", err)
	}

	watermark, err := GetForgeSyncWatermark(repoKey)
	if err != nil {
		return fmt.Errorf("read watermark: %w", err)
	}
	firstSync := watermark.IsZero()

	// Query from the watermark minus the overlap window so boundary items are
	// not skipped. On a first sync this is the zero time (full fetch).
	since := time.Time{}
	if !firstSync {
		since = watermark.Add(-s.overlapWindow)
	}

	// Drain both item types. The issues endpoint excludes PRs (github adapter)
	// and GitLab serves them separately, so both must be fetched.
	maxUpdated, itemsSeen, err := s.drainAllTypes(ctx, provider, repoKey, repoRef, remote, since, firstSync, opts)
	if err != nil {
		return err
	}

	// Advance the item watermark before touching CI: the two are independent
	// (CI has its own per-run ledger), so a pipeline failure must not roll back
	// the item watermark and vice versa.
	//
	// Note the assignment (not `:=`): the outer `err` is reused below for the
	// pipeline pass, so shadowing it here would hide that pass's error.
	err = s.advanceWatermark(repoKey, watermark, maxUpdated, firstSync)
	if err != nil {
		return err
	}

	// CI runs are fetched after the items and independently of the watermark:
	// they derive their own baseline from their own ledger, and a pipeline
	// failure must not roll back the item watermark (or vice versa).
	// A platform without CI support simply does not implement PipelineLister.
	pipelinesSeen := 0
	if opts.IncludePipelines {
		pipelinesSeen, err = s.syncPipelines(ctx, provider, repoKey, repoRef, remote, since)
		if err != nil {
			return err
		}
	}

	slog.Debug(
		"forge sync complete",
		slog.String("repo", repoRef.Key()),
		slog.Int("items", itemsSeen),
		slog.Int("pipelines", pipelinesSeen),
		slog.Bool("first_sync", firstSync),
	)
	return nil
}

// syncPipelines fetches CI runs, records terminal ones, and dispatches the
// genuinely new ones.
//
// Three rules make this correct:
//
//   - Non-terminal runs are NOT recorded. If they were, the run would look
//     already-handled and its eventual completion would be lost forever.
//   - The baseline is derived from the CI LEDGER, not the item watermark. A
//     repository polled for months with no pipeline subscriber has a set item
//     watermark but an empty ledger; treating that as "not the first sync" would
//     dispatch every historical run newer than the watermark.
//   - On the first pass (empty ledger) terminal runs are recorded but NOT
//     dispatched, so a fresh install does not fire the task once per historical
//     run. A mid-walk failure leaves the ledger partially populated, and the
//     remaining runs are then correctly treated as new — they were never
//     baselined, so dispatching them is the right outcome rather than a leak.
func (s *ForgeSyncer) syncPipelines(
	ctx context.Context,
	provider forge.Provider,
	repoKey ForgeRepoKey,
	repoRef ForgeRepoRef,
	remote forge.Remote,
	since time.Time,
) (int, error) {
	lister, ok := provider.(forge.PipelineLister)
	if !ok {
		// The platform has no CI surface. This is not an error: it is the
		// expected shape for any provider that only implements Provider.
		return 0, nil
	}

	// First pass for THIS repo's CI: an empty ledger means nothing has been
	// recorded yet, whatever the item watermark says.
	baselined, err := HasAnyPipelineRun(repoKey)
	if err != nil {
		return 0, fmt.Errorf("read pipeline ledger: %w", err)
	}

	page := 1
	seen := 0
	for {
		res, err := lister.ListPipelineRuns(ctx, since, page, 100)
		if err != nil {
			return seen, fmt.Errorf("list pipelines page %d: %w", page, err)
		}

		for i := range res.Runs {
			run := res.Runs[i]
			if !forge.PipelineTerminal(run.Status) {
				// Still going: leave it unrecorded so it can fire on a later
				// pass once it reaches a terminal state.
				continue
			}
			seen++

			fresh, err := RecordPipelineRun(repoKey, run.ID)
			if err != nil {
				return seen, fmt.Errorf("record pipeline run %d: %w", run.ID, err)
			}
			if !fresh {
				continue // already handled on an earlier pass
			}
			if !baselined {
				// Baseline pass: recorded so it never fires, but deliberately
				// not dispatched.
				continue
			}
			if !forge.PipelineFiresTask(run.Status) {
				// Recorded so it is not re-evaluated forever, but cancelled /
				// skipped / unknown are not worth waking the user for.
				continue
			}

			item, change := pipelineEvent(remote, run)
			if err := s.persistAndDispatch(ctx, repoRef, remote, item, change); err != nil {
				return seen, err
			}
		}

		if !res.HasMore {
			break
		}
		page = res.NextPage
		if page <= 0 {
			break
		}
	}
	return seen, nil
}

// pipelineEvent builds the synthetic item and change for a CI run. A run has no
// issue/PR identity, so the item carries the run's own description and a
// synthetic type; every consumer that renders an item must tolerate that.
func pipelineEvent(remote forge.Remote, run forge.PipelineRun) (forge.Item, forge.Change) {
	item := forge.Item{
		Platform:  remote.Platform,
		Type:      forge.ItemTypePipeline,
		Number:    0,
		Title:     run.Name,
		State:     forge.State(run.Status),
		Author:    forge.Author{Login: run.Actor},
		URL:       run.URL,
		CreatedAt: run.CreatedAt,
		UpdatedAt: run.UpdatedAt,
	}
	change := forge.Change{
		Type:           forge.EventPipeline,
		NewState:       string(run.Status),
		Actor:          run.Actor,
		PipelineStatus: string(run.Status),
		PipelineURL:    run.URL,
		PipelineRunID:  run.ID,
	}
	return item, change
}

// drainAllTypes pages through every item type, processing each item and
// tracking the newest updated_at seen. The watermark is NOT advanced here: the
// caller only advances it once every page has been consumed, so a mid-pagination
// failure leaves the window intact for the next run.
func (s *ForgeSyncer) drainAllTypes(
	ctx context.Context,
	provider forge.Provider,
	repoKey ForgeRepoKey,
	repoRef ForgeRepoRef,
	remote forge.Remote,
	since time.Time,
	firstSync bool,
	opts SyncOptions,
) (maxUpdated time.Time, itemsSeen int, err error) {
	maxUpdated = since
	for _, typ := range []forge.ItemType{forge.ItemTypeIssue, forge.ItemTypeChangeRequest} {
		page := 1
		for {
			res, err := provider.ListItems(ctx, forge.ListOptions{
				Type:      typ,
				State:     "all",
				Page:      page,
				PerPage:   100,
				Since:     since,
				Sort:      "updated",
				Direction: "asc",
			})
			if err != nil {
				return time.Time{}, itemsSeen, fmt.Errorf("list %s page %d: %w", typ, page, err)
			}

			for _, item := range res.Items {
				itemsSeen++
				if item.UpdatedAt.After(maxUpdated) {
					maxUpdated = item.UpdatedAt
				}
				if err := s.processItem(ctx, provider, repoKey, repoRef, remote, typ, item, firstSync, opts); err != nil {
					return time.Time{}, itemsSeen, err
				}
			}

			if !res.HasMore {
				break
			}
			page = res.NextPage
			if page <= 0 {
				break
			}
		}
	}
	return maxUpdated, itemsSeen, nil
}

// advanceWatermark moves the stored watermark forward, or initializes it on a
// first sync that saw no items (so history is not re-fetched forever).
func (s *ForgeSyncer) advanceWatermark(repoKey ForgeRepoKey, prev, maxUpdated time.Time, firstSync bool) error {
	if !maxUpdated.IsZero() && maxUpdated.After(prev) {
		if err := SetForgeSyncWatermark(repoKey, maxUpdated); err != nil {
			return fmt.Errorf("advance watermark: %w", err)
		}
		return nil
	}
	if firstSync {
		if err := SetForgeSyncWatermark(repoKey, s.now().UTC()); err != nil {
			return fmt.Errorf("init watermark: %w", err)
		}
	}
	return nil
}

// processItem compares one fetched item against its snapshot, persists the new
// state, and dispatches any derived events.
func (s *ForgeSyncer) processItem(
	ctx context.Context,
	provider forge.Provider,
	repoKey ForgeRepoKey,
	repoRef ForgeRepoRef,
	remote forge.Remote,
	typ forge.ItemType,
	item forge.Item,
	firstSync bool,
	opts SyncOptions,
) error {
	prev, err := GetForgeItemSnapshot(repoKey, string(typ), item.Number)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}

	// Comment activity is derived from the newest comment on the item. Fetching
	// comments per item is the only option on GitLab (no repo-level endpoint),
	// and it is the expensive part, so it is skipped on state-only passes.
	cs := s.resolveCommentState(ctx, provider, repoRef, prev, typ, item.Number, opts.IncludeComments)

	cur := forge.ItemState{
		State:                  string(item.State),
		Merged:                 item.State == forge.StateMerged,
		LatestCommentID:        cs.id,
		LatestCommentUpdatedAt: cs.updated,
		LatestCommentAuthor:    cs.author,
		LatestCommentBody:      cs.body,
		Author:                 item.Author.Login,
	}

	// Derive events only when this is not the first sync. A nil prev on a later
	// sync is a genuinely new item (opened/merged/closed since the baseline);
	// on the first sync it would replay the whole history, so it is skipped.
	var changes []forge.Change
	if !firstSync {
		changes = forge.DeriveChanges(prevSnapshotOrNil(prev), cur, item.Number)
		// Comment events additionally require a baseline from a PREVIOUS pass.
		// A state transition is still reported without one — it is derived from
		// data this pass actually fetched — but comment activity cannot be,
		// because there is no "previous" comment to compare against.
		if !cs.prevBaselined {
			changes = dropCommentChanges(changes)
		}
	}

	// Persist the new snapshot BEFORE dispatching, so a dispatch failure does
	// not cause the same event to be derived again next round.
	if err := UpsertForgeItemSnapshot(ForgeItemSnapshot{
		Platform:             pfPlatform(repoKey),
		Host:                 repoKey.Host,
		Owner:                repoKey.Owner,
		Repo:                 repoKey.Repo,
		ItemType:             string(typ),
		Number:               item.Number,
		State:                cur.State,
		Merged:               cur.Merged,
		LastCommentID:        cs.id,
		LastCommentUpdatedAt: cs.updated,
		CommentsBaselined:    cs.baselined,
		ItemUpdatedAt:        item.UpdatedAt,
	}); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}

	if s.sink == nil {
		return nil
	}
	for _, ch := range changes {
		if err := s.persistAndDispatch(ctx, repoRef, remote, item, ch); err != nil {
			return err
		}
	}
	return nil
}

// persistAndDispatch is the single persist→dispatch path for derived events,
// shared by the item poller and the CI poller so notification, unread-count and
// task-triggering semantics cannot drift between them.
//
// An event that already exists is silently not dispatched (event-level dedupe),
// which is not an error: the caller has nothing to do differently.
func (s *ForgeSyncer) persistAndDispatch(
	ctx context.Context,
	repoRef ForgeRepoRef,
	remote forge.Remote,
	item forge.Item,
	ch forge.Change,
) error {
	if s.sink == nil {
		return nil
	}

	// Event-level dedupe: the same event is never dispatched twice, even if the
	// overlap window re-derives it. The key includes the item type, so a
	// pipeline event (itemType "pipeline", number 0) can never collide with a
	// real item's event.
	key := forge.DedupeKey(remote, item.Type, item.Number, ch)
	fresh, err := InsertForgeEvent(ForgeEvent{
		Platform:  repoRef.Platform,
		Host:      repoRef.Host,
		Owner:     repoRef.Owner,
		Repo:      repoRef.Repo,
		ItemType:  string(item.Type),
		Number:    item.Number,
		ItemKey:   forge.ItemKey(item.Type, item.Number, ch),
		EventType: string(ch.Type),
		DedupeKey: key,
		Payload:   item.URL,
	})
	if err != nil {
		return fmt.Errorf("persist event: %w", err)
	}
	if !fresh {
		return nil // already dispatched
	}
	s.sink.HandleChange(ctx, repoRef, item, ch)
	return nil
}

// commentState is the resolved comment state for one item, plus the pre-pass
// baseline flag the caller uses to gate comment events.
type commentState struct {
	id            int64
	updated       time.Time
	author        string
	body          string
	baselined     bool // baseline established after this pass
	prevBaselined bool // baseline existed before this pass
}

// resolveCommentState determines the comment state to persist for one item and
// reports whether the comment baseline was established before this pass.
//
// Fetching comments is the expensive part of a sync, so it only happens on
// comment-inclusive passes; state-only passes preserve the last known values so
// the item does not look like "comments were removed". A fetch failure is not
// fatal — the item state is still valuable and comments are retried next round.
//
// The returned prevBaselined is the PRE-pass baseline. It is tracked per item,
// not per repo: the repo-level watermark says "we have seen this repo before",
// but the comment baseline is established by a comment-inclusive pass, which
// can fail independently (rate limit, timeout). Without this distinction a zero
// baseline is ambiguous — "no comments exist" and "never fetched comments" look
// identical — so the first successful comment pass would replay every
// historical comment as new. The caller uses the PRE-pass value so the pass
// that establishes the baseline absorbs history silently.
func (s *ForgeSyncer) resolveCommentState(
	ctx context.Context,
	provider forge.Provider,
	repoRef ForgeRepoRef,
	prev *ForgeItemSnapshot,
	typ forge.ItemType,
	number int,
	includeComments bool,
) commentState {
	cs := commentState{prevBaselined: prev != nil && prev.CommentsBaselined}
	cs.baselined = cs.prevBaselined

	if includeComments {
		id, updated, author, body, err := s.latestComment(ctx, provider, typ, number)
		if err != nil {
			slog.Warn("forge: comment fetch failed",
				slog.String("repo", repoRef.Key()),
				slog.Int("number", number),
				slog.String("err", err.Error()))
		} else {
			// This pass read comments, so the baseline is now established (for
			// persistence on the snapshot written by the caller).
			cs.id, cs.updated, cs.author, cs.body = id, updated, author, body
			cs.baselined = true
			return cs
		}
	}

	if prev != nil {
		// Preserve the last known comment state so a state-only pass (or a
		// failed comment fetch) does not look like "comments were removed".
		cs.id, cs.updated = prev.LastCommentID, prev.LastCommentUpdatedAt
	}
	return cs
}

func (s *ForgeSyncer) latestComment(ctx context.Context, provider forge.Provider, typ forge.ItemType, number int) (int64, time.Time, string, string, error) {
	var maxID int64
	var maxUpdated time.Time
	var author, body string
	page := 1
	for {
		comments, err := provider.ListComments(ctx, typ, number, page, 100)
		if err != nil {
			return 0, time.Time{}, "", "", err
		}
		for _, c := range comments {
			if c.ID > maxID {
				maxID = c.ID
				// The newest comment by id is the one an event should describe.
				author, body = c.Author.Login, c.Body
			}
			if c.UpdatedAt.After(maxUpdated) {
				maxUpdated = c.UpdatedAt
			}
		}
		if len(comments) < 100 {
			break
		}
		page++
		if page > 50 {
			// Defensive bound: never page forever on a pathological thread.
			break
		}
	}
	return maxID, maxUpdated, author, body, nil
}

// prevSnapshotOrNil adapts a persisted snapshot row to the forge.Snapshot the
// derivation expects, returning nil when there is no row.
func prevSnapshotOrNil(prev *ForgeItemSnapshot) *forge.Snapshot {
	if prev == nil {
		return nil
	}
	return &forge.Snapshot{
		State:                prev.State,
		Merged:               prev.Merged,
		LastCommentID:        prev.LastCommentID,
		LastCommentUpdatedAt: prev.LastCommentUpdatedAt,
	}
}

// dropCommentChanges removes comment events from a derived batch.
//
// Used when an item has no comment baseline yet: a state transition is still
// trustworthy (it comes from data this pass fetched), but comment activity
// cannot be distinguished from pre-existing history.
func dropCommentChanges(changes []forge.Change) []forge.Change {
	if len(changes) == 0 {
		return changes
	}
	kept := changes[:0]
	for _, ch := range changes {
		if ch.Type != forge.EventCommented {
			kept = append(kept, ch)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// pfPlatform returns the platform string for a repo key.
func pfPlatform(k ForgeRepoKey) string { return k.Platform }

// ErrNoForgeProviderFactory is returned when the syncer has no factory.
var ErrNoForgeProviderFactory = errors.New("forge: no provider factory configured")
