package service

import (
	"context"
	"fmt"
	"log/slog"

	"clawbench/internal/forge"
	"clawbench/internal/model"
)

// ForgeNotifier is the interface the dispatcher uses to push to IM robots. It is
// injected so the service package does not import the push backends directly
// (matching the existing push wiring) and so tests can substitute a fake.
type ForgeNotifier interface {
	// PushForgeEvent delivers a forge event to IM subscribers. Returns true when
	// at least one channel accepted it.
	PushForgeEvent(event ForgeEvent, item forge.Item) bool
}

// ForgeBroadcaster broadcasts a WS event to connected clients. Injected to avoid
// importing the ws package's concrete manager in tests.
type ForgeBroadcaster func(msg any)

// ForgeEventDispatcher implements ForgeChangeSink: it turns derived events into
// user-visible notifications (WS broadcast + IM push), gated by the global
// notification toggles.
//
// Crucially, the unread count is NOT gated by those toggles — it is written by
// the syncer's event persistence, independent of whether a notification was
// sent. This is what prevents "all toggles off ⇒ badge permanently dead while
// events keep happening".
type ForgeEventDispatcher struct {
	cfgFn       func() model.Config
	broadcaster ForgeBroadcaster
	notifier    ForgeNotifier
}

// NewForgeEventDispatcher builds a dispatcher.
func NewForgeEventDispatcher(cfgFn func() model.Config, broadcaster ForgeBroadcaster, notifier ForgeNotifier) *ForgeEventDispatcher {
	return &ForgeEventDispatcher{cfgFn: cfgFn, broadcaster: broadcaster, notifier: notifier}
}

// HandleChange is invoked once per freshly derived event.
func (d *ForgeEventDispatcher) HandleChange(_ context.Context, repo ForgeRepoRef, item forge.Item, change forge.Change) {
	cfg := d.cfgFn()

	// The event has already been persisted by the syncer, so the unread count is
	// correct regardless of the notification decision below.
	if !d.notifyEnabled(cfg, change.Type) {
		slog.Debug("forge event suppressed by notification toggle",
			slog.String("repo", repo.Key()),
			slog.String("event", string(change.Type)))
		return
	}

	event := ForgeEvent{
		Platform:  repo.Platform,
		Host:      repo.Host,
		Owner:     repo.Owner,
		Repo:      repo.Repo,
		ItemType:  string(item.Type),
		Number:    item.Number,
		EventType: string(change.Type),
		Payload:   item.URL,
	}

	if d.broadcaster != nil {
		d.broadcaster(map[string]any{
			contentKeyType: "forge_event",
			"event":        event,
			"item": map[string]any{
				contentKeyType: string(item.Type),
				"number":       item.Number,
				"title":        item.Title,
				"url":          item.URL,
				"state":        string(item.State),
				"author":       item.Author.Login,
			},
		})
	}

	if d.notifier != nil {
		if d.notifier.PushForgeEvent(event, item) {
			slog.Debug("forge event pushed to IM", slog.String("event", string(change.Type)))
		}
	}
}

// FormatForgeEventMessage renders a forge event as an IM-ready title + body.
// It lives in the service package so the wording is shared by every push
// backend, while the backends own only the transport.
func FormatForgeEventMessage(event ForgeEvent, item forge.Item) (title, body string) {
	kind := "issue"
	if event.ItemType == string(forge.ItemTypeChangeRequest) {
		kind = "PR/MR"
	}
	verb := map[string]string{
		string(forge.EventOpened):    "新开",
		string(forge.EventClosed):    "关闭",
		string(forge.EventMerged):    "合并",
		string(forge.EventReopened):  "重新打开",
		string(forge.EventCommented): "有新评论",
		string(forge.EventPipeline):  "流水线完成",
	}[event.EventType]
	if verb == "" {
		verb = event.EventType
	}

	title = fmt.Sprintf("%s %s #%d %s", event.Owner+"/"+event.Repo, kind, event.Number, verb)
	body = fmt.Sprintf("### %s\n\n**仓库**: %s/%s\n\n**%s**: #%d %s\n\n**事件**: %s",
		title, event.Owner, event.Repo, kind, event.Number, item.Title, verb)
	if item.Author.Login != "" {
		body += fmt.Sprintf("\n\n**作者**: %s", item.Author.Login)
	}
	if item.URL != "" {
		body += fmt.Sprintf("\n\n[查看详情](%s)", item.URL)
	}
	return title, body
}

// notifyEnabled reports whether the given event type should notify, based on
// the global toggles.
func (d *ForgeEventDispatcher) notifyEnabled(cfg model.Config, t forge.EventType) bool {
	n := cfg.Forge.Notify
	switch t {
	case forge.EventOpened:
		return n.Opened
	case forge.EventClosed:
		return n.Closed
	case forge.EventMerged:
		return n.Merged
	case forge.EventReopened:
		return n.Reopened
	case forge.EventCommented:
		return n.Commented
	case forge.EventPipeline:
		return n.Pipeline
	default:
		return false
	}
}
