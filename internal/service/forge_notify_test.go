package service_test

import (
	"context"
	"testing"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNotifier struct {
	calls int
}

func (f *fakeNotifier) PushForgeEvent(service.ForgeEvent, forge.Item) bool {
	f.calls++
	return true
}

func fullNotifyConfig() model.Config {
	return model.Config{Forge: model.ForgeConfig{Notify: model.ForgeNotifyConfig{
		Opened: true, Closed: true, Merged: true, Reopened: true, Commented: true, Pipeline: true,
	}}}
}

func testItem() forge.Item {
	return forge.Item{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: 1,
		Title: "t", URL: "https://github.com/a/b/issues/1", State: forge.StateOpen,
	}
}

func TestForgeDispatcher_NotifiesWhenEnabled(t *testing.T) {
	var broadcasts int
	notifier := &fakeNotifier{}
	d := service.NewForgeEventDispatcher(
		fullNotifyConfig,
		func(any) { broadcasts++ },
		notifier,
	)

	d.HandleChange(context.Background(), service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"},
		testItem(), forge.Change{Type: forge.EventClosed, Number: 1})

	assert.Equal(t, 1, broadcasts, "an enabled event must broadcast")
	assert.Equal(t, 1, notifier.calls, "an enabled event must push to IM")
}

// TestForgeDispatcher_ToggleOffStillBroadcasts is the regression guard for the
// unread badge going stale.
//
// The notification toggles govern IM push, NOT the WS broadcast. The frontend
// learns about new events from the broadcast and re-derives the badge from the
// server, so suppressing it here would freeze the badge for any user who muted
// a category — even though the event was still recorded as unread.
// recordingNotifier captures which events were pushed, for independence tests.
type recordingNotifier struct {
	onPush func(service.ForgeEvent)
}

func (n *recordingNotifier) PushForgeEvent(e service.ForgeEvent, _ forge.Item) bool {
	if n.onPush != nil {
		n.onPush(e)
	}
	return true
}

func TestForgeDispatcher_ToggleOffStillBroadcasts(t *testing.T) {
	cfg := fullNotifyConfig()
	cfg.Forge.Notify.Commented = false

	var broadcasts int
	notifier := &fakeNotifier{}
	d := service.NewForgeEventDispatcher(func() model.Config { return cfg }, func(any) { broadcasts++ }, notifier)

	d.HandleChange(context.Background(), service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"},
		testItem(), forge.Change{Type: forge.EventCommented, Number: 1})

	assert.Equal(t, 1, broadcasts, "a muted event must still broadcast so the badge stays live")
	assert.Zero(t, notifier.calls, "a disabled event must not push to IM")
}

func TestForgeDispatcher_EachToggleIsIndependent(t *testing.T) {
	// Turning off "merged" must not silence "closed". Asserted against IM push,
	// which is what the toggles actually govern (the broadcast always fires).
	cfg := fullNotifyConfig()
	cfg.Forge.Notify.Merged = false

	var pushed []string
	notifier := &recordingNotifier{onPush: func(e service.ForgeEvent) {
		pushed = append(pushed, e.EventType)
	}}
	d := service.NewForgeEventDispatcher(func() model.Config { return cfg }, func(any) {}, notifier)

	ref := service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"}
	d.HandleChange(context.Background(), ref, testItem(), forge.Change{Type: forge.EventMerged, Number: 1})
	d.HandleChange(context.Background(), ref, testItem(), forge.Change{Type: forge.EventClosed, Number: 1})

	require.Len(t, pushed, 1)
	assert.Equal(t, "closed", pushed[0])
}

// TestForgeDispatcher_UnreadIndependentOfToggles is the core R12 guard: with all
// notification toggles OFF, events still accrue unread — otherwise the badge
// would be permanently dead while changes keep happening.
func TestForgeDispatcher_UnreadIndependentOfToggles(t *testing.T) {
	setupTestDBForForgeSync(t)

	// All toggles off.
	cfg := model.Config{Forge: model.ForgeConfig{Notify: model.ForgeNotifyConfig{}}}
	d := service.NewForgeEventDispatcher(func() model.Config { return cfg }, nil, nil)

	ref := service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"}

	// Simulate what the syncer does: persist the event, then dispatch.
	_, err := service.InsertForgeEvent(service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "a", Repo: "b",
		ItemType: "issue", Number: 1, EventType: "closed", DedupeKey: "k1",
	})
	require.NoError(t, err)
	d.HandleChange(context.Background(), ref, testItem(), forge.Change{Type: forge.EventClosed, Number: 1})

	n, err := service.CountUnreadForgeEvents()
	require.NoError(t, err)
	assert.Equal(t, 1, n, "unread must accrue even when notifications are disabled")

	require.NoError(t, service.MarkForgeEventsRead())
	n, err = service.CountUnreadForgeEvents()
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestForgeDispatcher_NilDependenciesAreSafe(t *testing.T) {
	d := service.NewForgeEventDispatcher(fullNotifyConfig, nil, nil)
	// Must not panic with no broadcaster/notifier.
	d.HandleChange(context.Background(), service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"},
		testItem(), forge.Change{Type: forge.EventClosed, Number: 1})
}

func TestForgeDispatcher_UnknownEventTypeIsNotPushed(t *testing.T) {
	var broadcasts int
	notifier := &fakeNotifier{}
	d := service.NewForgeEventDispatcher(fullNotifyConfig, func(any) { broadcasts++ }, notifier)
	d.HandleChange(context.Background(), service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"},
		testItem(), forge.Change{Type: forge.EventType("bogus"), Number: 1})
	// Unknown types have no toggle, so notifyEnabled returns false -> no IM push.
	// The broadcast still happens so the badge never silently misses an event.
	assert.Zero(t, notifier.calls, "an unrecognized event type must not push to IM")
	assert.Equal(t, 1, broadcasts, "the badge must still learn about the event")
}

// TestForgeDispatcher_PayloadCarriesItemIdentity ensures the broadcast carries
// enough to deep-link back to the item.
func TestForgeDispatcher_PayloadCarriesItemIdentity(t *testing.T) {
	var payload map[string]any
	d := service.NewForgeEventDispatcher(fullNotifyConfig, func(msg any) {
		payload = msg.(map[string]any)
	}, nil)

	item := testItem()
	d.HandleChange(context.Background(), service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"},
		item, forge.Change{Type: forge.EventClosed, Number: 1})

	require.NotNil(t, payload)
	it := payload["item"].(map[string]any)
	assert.Equal(t, 1, it["number"])
	assert.Equal(t, item.URL, it["url"], "the item URL must be present for deep-linking")
}

func TestFormatForgeEventMessage(t *testing.T) {
	event := service.ForgeEvent{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
		ItemType: "pr", Number: 42, EventType: "merged",
	}
	item := forge.Item{
		Type: forge.ItemTypeChangeRequest, Number: 42, Title: "Fix the thing",
		URL: "https://github.com/acme/widgets/pull/42", Author: forge.Author{Login: "alice"},
	}

	title, body := service.FormatForgeEventMessage(event, item)
	assert.Contains(t, title, "acme/widgets")
	assert.Contains(t, title, "#42")
	assert.Contains(t, title, "合并")
	assert.Contains(t, body, "Fix the thing")
	assert.Contains(t, body, "alice")
	assert.Contains(t, body, "https://github.com/acme/widgets/pull/42")
	// The item type must render as the Chinese 合并请求, not the raw "pr" and
	// not a mixed-language "PR/MR" inside an otherwise Chinese sentence.
	assert.Contains(t, body, "合并请求")
}

func TestFormatForgeEventMessage_UnknownEventTypeFallsBack(t *testing.T) {
	event := service.ForgeEvent{Owner: "a", Repo: "b", ItemType: "issue", Number: 1, EventType: "weird"}
	item := forge.Item{Number: 1, Title: "t"}
	title, _ := service.FormatForgeEventMessage(event, item)
	assert.Contains(t, title, "weird", "an unrecognized event type must still render, not panic or blank out")
}
