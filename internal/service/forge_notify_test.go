package service_test

import (
	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"context"
	"testing"

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

func TestForgeDispatcher_SuppressedWhenToggleOff(t *testing.T) {
	cfg := fullNotifyConfig()
	cfg.Forge.Notify.Commented = false

	var broadcasts int
	notifier := &fakeNotifier{}
	d := service.NewForgeEventDispatcher(func() model.Config { return cfg }, func(any) { broadcasts++ }, notifier)

	d.HandleChange(context.Background(), service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"},
		testItem(), forge.Change{Type: forge.EventCommented, Number: 1})

	assert.Zero(t, broadcasts, "a disabled event must not broadcast")
	assert.Zero(t, notifier.calls, "a disabled event must not push")
}

func TestForgeDispatcher_EachToggleIsIndependent(t *testing.T) {
	// Turning off "merged" must not silence "closed".
	cfg := fullNotifyConfig()
	cfg.Forge.Notify.Merged = false

	var got []string
	d := service.NewForgeEventDispatcher(func() model.Config { return cfg }, func(msg any) {
		m := msg.(map[string]any)
		got = append(got, m["event"].(service.ForgeEvent).EventType)
	}, nil)

	ref := service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"}
	d.HandleChange(context.Background(), ref, testItem(), forge.Change{Type: forge.EventMerged, Number: 1})
	d.HandleChange(context.Background(), ref, testItem(), forge.Change{Type: forge.EventClosed, Number: 1})

	require.Len(t, got, 1)
	assert.Equal(t, "closed", got[0])
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

func TestForgeDispatcher_UnknownEventTypeIsNotNotified(t *testing.T) {
	var broadcasts int
	d := service.NewForgeEventDispatcher(fullNotifyConfig, func(any) { broadcasts++ }, nil)
	d.HandleChange(context.Background(), service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "a", Repo: "b"},
		testItem(), forge.Change{Type: forge.EventType("bogus"), Number: 1})
	assert.Zero(t, broadcasts, "an unrecognized event type must not notify")
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
