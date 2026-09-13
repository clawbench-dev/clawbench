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

	if err := s.advanceWatermark(repoKey, watermark, maxUpdated, firstSync); err != nil {
		return err
	}

	slog.Debug(
		"forge sync complete",
		slog.String("repo", repoRef.Key()),
		slog.Int("items", itemsSeen),
		slog.Bool("first_sync", firstSync),
	)
	return nil
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
		// Event-level dedupe: the same event is never dispatched twice, even if
		// the overlap window re-derives it.
		key := forge.DedupeKey(remote, typ, item.Number, ch)
		fresh, err := InsertForgeEvent(ForgeEvent{
			Platform:  repoKey.Platform,
			Host:      repoKey.Host,
			Owner:     repoKey.Owner,
			Repo:      repoKey.Repo,
			ItemType:  string(typ),
			Number:    item.Number,
			EventType: string(ch.Type),
			DedupeKey: key,
			Payload:   item.URL,
		})
		if err != nil {
			return fmt.Errorf("persist event: %w", err)
		}
		if !fresh {
			continue // already dispatched
		}
		s.sink.HandleChange(ctx, repoRef, item, ch)
	}
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
