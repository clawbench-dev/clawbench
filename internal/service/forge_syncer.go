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

	maxUpdated := watermark
	itemsSeen := 0

	// Drain both item types. The issues endpoint excludes PRs (github adapter)
	// and GitLab serves them separately, so both must be fetched.
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
				// Do not advance the watermark: the window was not fully read.
				return fmt.Errorf("list %s page %d: %w", typ, page, err)
			}

			for _, item := range res.Items {
				itemsSeen++
				if item.UpdatedAt.After(maxUpdated) {
					maxUpdated = item.UpdatedAt
				}
				if err := s.processItem(ctx, provider, repoKey, repoRef, remote, typ, item, firstSync, opts); err != nil {
					return err
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

	// Advance the watermark only now that every page has been consumed.
	if !maxUpdated.IsZero() && maxUpdated.After(watermark) {
		if err := SetForgeSyncWatermark(repoKey, maxUpdated); err != nil {
			return fmt.Errorf("advance watermark: %w", err)
		}
	} else if firstSync {
		// A first sync with no items still records "now" so history is not
		// re-fetched forever.
		if err := SetForgeSyncWatermark(repoKey, s.now().UTC()); err != nil {
			return fmt.Errorf("init watermark: %w", err)
		}
	}

	slog.Debug("forge sync complete",
		slog.String("repo", repoRef.Key()),
		slog.Int("items", itemsSeen),
		slog.Bool("first_sync", firstSync),
	)
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
	latestID, latestUpdated := int64(0), time.Time{}
	var commentErr error
	if opts.IncludeComments {
		latestID, latestUpdated, commentErr = s.latestComment(ctx, provider, typ, item.Number)
	} else if prev != nil {
		// Preserve the last known comment state so a state-only pass does not
		// look like "comments were removed".
		latestID, latestUpdated = prev.LastCommentID, prev.LastCommentUpdatedAt
	}
	if opts.IncludeComments && commentErr != nil {
		// A comment fetch failure must not abort the whole sync: the item state
		// is still valuable, and comments are retried next round.
		slog.Warn("forge: comment fetch failed",
			slog.String("repo", repoRef.Key()),
			slog.Int("number", item.Number),
			slog.String("err", commentErr.Error()))
		latestID, latestUpdated = 0, time.Time{}
		if prev != nil {
			latestID, latestUpdated = prev.LastCommentID, prev.LastCommentUpdatedAt
		}
	}

	var snapshot forge.Snapshot
	if prev != nil {
		snapshot = forge.Snapshot{
			State:                prev.State,
			Merged:               prev.Merged,
			LastCommentID:        prev.LastCommentID,
			LastCommentUpdatedAt: prev.LastCommentUpdatedAt,
		}
	}

	cur := forge.ItemState{
		State:                  string(item.State),
		Merged:                 item.State == forge.StateMerged,
		LatestCommentID:        latestID,
		LatestCommentUpdatedAt: latestUpdated,
	}

	// Derive events only when we have a baseline and this is not the first sync.
	var changes []forge.Change
	if !firstSync && prev != nil {
		changes = forge.DeriveChanges(&snapshot, cur, item.Number)
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
		LastCommentID:        latestID,
		LastCommentUpdatedAt: latestUpdated,
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

// latestComment returns the highest comment id and newest updated_at for an
// item, paging to the end. Only the last page matters, so we walk forward.
func (s *ForgeSyncer) latestComment(ctx context.Context, provider forge.Provider, typ forge.ItemType, number int) (int64, time.Time, error) {
	var maxID int64
	var maxUpdated time.Time
	page := 1
	for {
		comments, err := provider.ListComments(ctx, typ, number, page, 100)
		if err != nil {
			return 0, time.Time{}, err
		}
		for _, c := range comments {
			if c.ID > maxID {
				maxID = c.ID
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
	return maxID, maxUpdated, nil
}

// pfPlatform returns the platform string for a repo key.
func pfPlatform(k ForgeRepoKey) string { return k.Platform }

// ErrNoForgeProviderFactory is returned when the syncer has no factory.
var ErrNoForgeProviderFactory = errors.New("forge: no provider factory configured")
