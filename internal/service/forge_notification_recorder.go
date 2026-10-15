package service

import (
	"context"
	"strconv"
	"strings"

	"clawbench/internal/forge"
)

// ForgeNotificationRecorder is a ForgeChangeSink that appends each derived forge
// change to the notification-center log.
//
// It is a separate sink (registered alongside the dispatcher and the task
// trigger via NewForgeCompositeSink) rather than a branch inside the dispatcher,
// because the two have different gates: the dispatcher's IM push is controlled
// by the per-event notification toggles, while the log must record every change
// regardless. Folding them together would let a muted event type silently
// vanish from the history.
type ForgeNotificationRecorder struct{}

// NewForgeNotificationRecorder builds the recorder sink.
func NewForgeNotificationRecorder() *ForgeNotificationRecorder {
	return &ForgeNotificationRecorder{}
}

// HandleChange appends one log row for the change.
func (r *ForgeNotificationRecorder) HandleChange(_ context.Context, repo ForgeRepoRef, item forge.Item, change forge.Change) {
	title := forgeNotificationTitle(repo, item, change)
	body := item.Title
	if body == "" {
		body = string(change.Type)
	}
	itemKey := forgeItemKeyFor(change, item)
	RecordForgeNotification(forgeEventID(repo, item, change), repo, string(change.Type), title, body, itemKey)
}

// forgeEventID is the log's dedup identity for a forge change.
//
// It reuses forge.DedupeKey — the very key the syncer already dedupes dispatch
// on (forge_syncer.go). Reusing it means a change that somehow reaches the sink
// twice produces one log row, and the identity is derived from the change
// itself rather than minted at random. Scheme is deliberately not set: it is
// not part of a repository's identity (see forge.Remote.Scheme) and DedupeKey
// does not read it.
func forgeEventID(repo ForgeRepoRef, item forge.Item, change forge.Change) string {
	return forge.DedupeKey(
		forge.Remote{Platform: forge.Platform(repo.Platform), Host: repo.Host, Owner: repo.Owner, Repo: repo.Repo},
		item.Type, item.Number, change,
	)
}

// forgeNotificationTitle renders the same "owner/repo · kind #num · reason" shape
// the system notification and completion card use, so the log reads identically.
func forgeNotificationTitle(repo ForgeRepoRef, item forge.Item, change forge.Change) string {
	slug := strings.Trim(repo.Owner+"/"+repo.Repo, "/")
	parts := []string{}
	if slug != "" {
		parts = append(parts, slug)
	}
	kind := forgeItemKindLabel(item.Type)
	ref := kind
	if item.Number > 0 {
		ref = kind + " #" + strconv.Itoa(item.Number)
	}
	if ref != "" {
		parts = append(parts, ref)
	}
	if change.Type != "" {
		parts = append(parts, string(change.Type))
	}
	return strings.Join(parts, " · ")
}

// forgeItemKindLabel names an item type in the log's own terms.
func forgeItemKindLabel(t forge.ItemType) string {
	switch t {
	case forge.ItemTypeIssue:
		return "issue"
	case forge.ItemTypeChangeRequest:
		return "pr"
	case forge.ItemTypePipeline:
		return "pipeline"
	default:
		return string(t)
	}
}

// forgeItemKeyFor builds the opaque item key used for deep-linking. It mirrors
// the frontend's forgeTargetItemKey: a pipeline has no number (always 0), so its
// identity is the run id.
func forgeItemKeyFor(change forge.Change, item forge.Item) string {
	switch item.Type {
	case forge.ItemTypePipeline:
		return "pipeline/run:" + strconv.FormatInt(change.PipelineRunID, 10)
	case forge.ItemTypeIssue, forge.ItemTypeChangeRequest:
		return forgeItemKindLabel(item.Type) + "/" + strconv.Itoa(item.Number)
	default:
		return ""
	}
}
