package service_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/forge/gitea"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestForgeTaskTrigger_FiresFromGiteaRepoAndIssue reproduces issue #754 with
// payloads shaped like a real Forgejo (Gitea) instance:
//
//	a Gitea repository is bound to a project,
//	the adapter turns the instance's issue JSON into an item,
//	the syncer derives `opened` for an issue created after the baseline,
//	the sink chain (syncer → trigger) must run the subscribed task.
//
// The item is decoded by the Gitea adapter rather than hand-built, so the
// timestamps and the author come from that platform's JSON. The window gate
// itself is NOT what this test proves (the adapter's created_at is read but an
// absent value would also count as new); what it proves is that a Gitea-shaped
// event reaching the trigger runs the task subscribed to it. Everything up to
// the event already works for the reporter — the issue shows in the list — so
// the step under test is the last one: event → task run.
func TestForgeTaskTrigger_FiresFromGiteaRepoAndIssue(t *testing.T) {
	setupTestDBForForgeSync(t)

	binding := service.ProjectForge{
		ProjectPath: "/proj", Platform: "gitea", Host: "git.acme.internal",
		Owner: "acme", Repo: "widgets", Source: "manual",
	}
	require.NoError(t, service.UpsertProjectForge(binding))

	var mu sync.Mutex
	var fired []int64
	var firedTypes []string

	trigger := service.NewForgeTaskTrigger(nil, fullNotifyConfig, nil)
	trigger.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(7, "issue.opened")}, nil
	})
	trigger.SetFireForTest(func(taskID int64, ev service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired = append(fired, taskID)
		firedTypes = append(firedTypes, ev.EventTypeForTest())
		mu.Unlock()
		return true
	})

	now := time.Now().UTC()
	provider := &fakeProvider{pages: map[forge.ItemType]map[int]forge.ListResult{
		forge.ItemTypeIssue: {},
	}}
	syncer := service.NewForgeSyncer(
		func(service.ProjectForge) (forge.Provider, error) { return provider, nil },
		service.NewForgeCompositeSink(trigger),
	)

	// The baseline pass also proves the old issue is recorded silently: it is
	// created well before the window, so it must not fire even though the task
	// below subscribes to exactly its event.
	oldIssue := giteaIssueItem(t, "1", "2025-01-01T00:00:00Z", "2025-01-02T00:00:00Z")
	provider.pages[forge.ItemTypeIssue][1] = forge.ListResult{Items: []forge.Item{oldIssue}}

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{}))
	mu.Lock()
	require.Empty(t, fired, "a pre-existing issue must only be baselined, not dispatched")
	mu.Unlock()

	// Gitea's list reports `updated_at` with second precision (no fraction), so
	// the fresh issue is stamped the same way — a nanosecond-precision value
	// would make the test pass for a reason production never sees.
	fresh := now.Truncate(time.Second).Add(time.Second)
	freshIssue := giteaIssueItem(t, "2", fresh.Format(time.RFC3339), fresh.Format(time.RFC3339))
	require.Equal(t, forge.StateOpen, freshIssue.State, "the adapter must read Gitea's state field")
	require.Equal(t, "alice", freshIssue.Author.Login, "the adapter must read Gitea's user.login")
	require.Equal(t, 2, freshIssue.Number)
	provider.pages[forge.ItemTypeIssue][1] = forge.ListResult{Items: []forge.Item{freshIssue}}

	require.NoError(t, syncer.SyncRepoWithOptions(context.Background(), binding, service.SyncOptions{}))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fired) == 1
	}, 2*time.Second, 10*time.Millisecond,
		"an opened issue on a bound Gitea repository must fire its subscribed task")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int64{7}, fired)
	assert.Equal(t, []string{"opened"}, firedTypes)
}

// TestForgeTaskTrigger_NoSubscriberIsExplainedInLog guards the second half of
// issue #754: "the issue appears but nothing runs, and nothing says why". An
// event that matches no task must leave one line naming the event, the
// subscriptions that do exist and which projects are bound — otherwise the only
// evidence is an absence.
//
// The log is captured at INFO, the level the default file handler writes, so a
// regression to Debug (invisible in a normal install) fails this test.
func TestForgeTaskTrigger_NoSubscriberIsExplainedInLog(t *testing.T) {
	logs := captureLogsAtLevel(t, slog.LevelInfo)

	trigger := service.NewForgeTaskTrigger(nil, fullNotifyConfig, nil)
	trigger.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		// Subscribed to PRs only — exactly the mismatch a user miscounts.
		return []model.ScheduledTask{forgeTriggerTask(9, "pr.opened")}, nil
	})

	trigger.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventOpened, Number: 1})

	out := logs()
	assert.Contains(t, out, "level=INFO")
	assert.Contains(t, out, "no active task subscribed")
	assert.Contains(t, out, "event=opened")
	assert.Contains(t, out, "kind=issue")
	assert.Contains(t, out, "bindings=", "must say which projects are bound (possibly none)")
	assert.Contains(t, out, "subscriptions=9:pr.opened",
		"the log must name what the tasks DO listen to")
}

// TestForgeTaskTrigger_NoEventTaskStaysQuiet is the other half of that level
// choice: without a single event task, an unsubscribed event is the normal case
// and must not reach INFO, or a busy repository fills the log with it.
func TestForgeTaskTrigger_NoEventTaskStaysQuiet(t *testing.T) {
	logs := captureLogsAtLevel(t, slog.LevelInfo)

	trigger := service.NewForgeTaskTrigger(nil, fullNotifyConfig, nil)
	trigger.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return nil, nil
	})

	trigger.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventOpened, Number: 1})

	assert.NotContains(t, logs(), "not triggered")
}

// captureLogsAtLevel swaps the default slog handler for a text handler at the
// given level and returns a reader for what was written. Test-only: the behavior
// under test is a log line, which no return value exposes.
func captureLogsAtLevel(t *testing.T, level slog.Level) func() string {
	t.Helper()
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return buf.String
}

// giteaIssueItem decodes one issue through the Gitea adapter so the item fields
// are the ones production derives from that platform's JSON.
func giteaIssueItem(t *testing.T, number, createdAt, updatedAt string) forge.Item {
	t.Helper()
	body := `{
		"id": ` + number + `, "number": ` + number + `,
		"title": "New issue", "body": "body",
		"state": "open",
		"user": {"login": "alice", "full_name": "Alice"},
		"html_url": "http://git.acme.internal/acme/widgets/issues/` + number + `",
		"created_at": "` + createdAt + `", "updated_at": "` + updatedAt + `"
	}`
	provider, err := gitea.New(
		gitea.Config{Host: "git.acme.internal", Scheme: "http", HTTPClient: &http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					// Header names are canonicalized on Get, so this matches
					// X-HasMore the way the real response does.
					Header: http.Header{"X-Hasmore": []string{"false"}},
					Body:   io.NopCloser(strings.NewReader(`[` + body + `]`)),
				}, nil
			}),
		}},
		"acme", "widgets",
	)
	require.NoError(t, err)
	res, err := provider.ListItems(context.Background(), forge.ListOptions{Type: forge.ItemTypeIssue, State: "all", Page: 1})
	require.NoError(t, err)
	require.Len(t, res.Items, 1)
	return res.Items[0]
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
