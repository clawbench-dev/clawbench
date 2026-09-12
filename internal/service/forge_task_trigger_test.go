package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func triggerRepo() service.ForgeRepoRef {
	return service.ForgeRepoRef{Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets"}
}

func triggerItem(number int) forge.Item {
	return forge.Item{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypeIssue, Number: number,
		Title: "t", URL: "https://github.com/acme/widgets/issues/1", State: forge.StateOpen,
		Author: forge.Author{Login: "alice"},
	}
}

// eventTask builds an active event-triggered task scoped to a repo slug.
func forgeTriggerTask(id int64, eventTypes, repoSlug string) model.ScheduledTask {
	return model.ScheduledTask{
		ID:          id,
		ProjectPath: "/proj",
		Name:        "task",
		Status:      "active",
		TriggerMode: "event",
		EventTypes:  eventTypes,
		EventRepo:   repoSlug,
	}
}

func TestForgeTaskTrigger_FiresMatchingTask(t *testing.T) {
	cfg := fullNotifyConfig()
	var fired []int64
	var mu sync.Mutex

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{
			forgeTriggerTask(1, "commented", triggerRepo().Key()),
			forgeTriggerTask(2, "opened", triggerRepo().Key()), // not subscribed to commented
		}, nil
	})
	tr.SetFireForTest(func(taskID int64, _ service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired = append(fired, taskID)
		mu.Unlock()
		return true
	})

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fired) == 1
	}, 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []int64{1}, fired, "only the subscribed task fires")
}

// TestForgeTaskTrigger_SplitsIssueAndPR verifies that "a new issue" and "a new
// PR" are independent triggers: subscribing to issue.opened must NOT fire for a
// PR, and vice versa. Before the split both collapsed onto the same "opened".
func TestForgeTaskTrigger_SplitsIssueAndPR(t *testing.T) {
	cfg := fullNotifyConfig()

	prItem := triggerItem(2)
	prItem.Type = forge.ItemTypeChangeRequest

	cases := []struct {
		name        string
		subscribed  string
		item        forge.Item
		wantFired   bool
	}{
		{"issue.opened fires for an issue", "issue.opened", triggerItem(1), true},
		{"issue.opened ignores a PR", "issue.opened", prItem, false},
		{"pr.opened fires for a PR", "pr.opened", prItem, true},
		{"pr.opened ignores an issue", "pr.opened", triggerItem(1), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var fired []int64
			var mu sync.Mutex
			tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
			tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
				return []model.ScheduledTask{forgeTriggerTask(1, tc.subscribed, triggerRepo().Key())}, nil
			})
			tr.SetFireForTest(func(taskID int64, _ service.ForgeQueuedEvent) bool {
				mu.Lock()
				fired = append(fired, taskID)
				mu.Unlock()
				return true
			})

			tr.HandleChange(context.Background(), triggerRepo(), tc.item,
				forge.Change{Type: forge.EventOpened, Number: tc.item.Number})

			if tc.wantFired {
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					return len(fired) == 1
				}, 2*time.Second, 10*time.Millisecond, "the subscribed task should fire")
			} else {
				// Give the async drain a chance to (wrongly) fire before asserting.
				time.Sleep(120 * time.Millisecond)
				mu.Lock()
				defer mu.Unlock()
				assert.Empty(t, fired, "a task subscribed to the other kind must not fire")
			}
		})
	}
}

// TestForgeTaskTrigger_BareLegacyKeyMatchesBothKinds verifies backward
// compatibility: a task stored before the split carries the bare key "opened",
// which must keep matching both issues and PRs without a migration.
func TestForgeTaskTrigger_BareLegacyKeyMatchesBothKinds(t *testing.T) {
	cfg := fullNotifyConfig()
	prItem := triggerItem(2)
	prItem.Type = forge.ItemTypeChangeRequest

	for _, tc := range []struct {
		name string
		item forge.Item
	}{
		{"bare key matches an issue", triggerItem(1)},
		{"bare key matches a PR", prItem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fired []int64
			var mu sync.Mutex
			tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
			tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
				return []model.ScheduledTask{forgeTriggerTask(1, "opened", triggerRepo().Key())}, nil
			})
			tr.SetFireForTest(func(taskID int64, _ service.ForgeQueuedEvent) bool {
				mu.Lock()
				fired = append(fired, taskID)
				mu.Unlock()
				return true
			})

			tr.HandleChange(context.Background(), triggerRepo(), tc.item,
				forge.Change{Type: forge.EventOpened, Number: tc.item.Number})

			require.Eventually(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(fired) == 1
			}, 2*time.Second, 10*time.Millisecond)
		})
	}
}

// TestForgeTaskTrigger_DebounceIsPerKind verifies the debounce key includes the
// item kind: a new issue arriving right after a new PR must not be swallowed by
// the PR's debounce window.
func TestForgeTaskTrigger_DebounceIsPerKind(t *testing.T) {
	cfg := fullNotifyConfig()
	prItem := triggerItem(2)
	prItem.Type = forge.ItemTypeChangeRequest

	var fired []int64
	var mu sync.Mutex
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(10 * time.Minute) // long, so only key separation can let the 2nd through
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "issue.opened,pr.opened", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(taskID int64, _ service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired = append(fired, taskID)
		mu.Unlock()
		return true
	})

	tr.HandleChange(context.Background(), triggerRepo(), prItem, forge.Change{Type: forge.EventOpened, Number: 2})
	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1), forge.Change{Type: forge.EventOpened, Number: 1})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fired) == 2
	}, 2*time.Second, 10*time.Millisecond, "the issue event must not be debounced away by the PR event")
}

// TestValidateEventSubscription_KindScopedKeys covers the accepted vocabulary.
func TestValidateEventSubscription_KindScopedKeys(t *testing.T) {
	valid := []string{
		"issue.opened", "pr.opened", "pr.merged", "issue.commented", "pr.pipeline_done",
		"issue.opened,pr.merged",
		"opened", // bare legacy key still accepted
	}
	for _, v := range valid {
		assert.NoError(t, service.ValidateEventSubscription(v), "expected %q to be valid", v)
	}

	invalid := []string{
		"issue.merged",   // an issue can never be merged
		"issue.pipeline_done", // issues have no CI
		"bogus.opened",
		"pr.bogus",
	}
	for _, v := range invalid {
		assert.Error(t, service.ValidateEventSubscription(v), "expected %q to be rejected", v)
	}
}

func TestForgeTaskTrigger_KillSwitchSuppresses(t *testing.T) {
	cfg := fullNotifyConfig()
	cfg.Forge.PauseEventTasks = true
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fired, "kill-switch must prevent firing")
}

func TestForgeTaskTrigger_SelfAuthoredSuppressed(t *testing.T) {
	cfg := fullNotifyConfig()
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "alice" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	// The acting user is the commenter (change.Actor), not the item creator.
	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1, Actor: "alice"})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fired, "an event authored by our own account must not fire a task")
}

// TestForgeTaskTrigger_SelfAuthoredCommentOnOthersItemSuppressed guards the
// anti-recursion fix: the AI commenting on someone else's issue must be
// suppressed even though the item creator is a different user. Comparing the
// item creator (the old behavior) would miss exactly this loop.
func TestForgeTaskTrigger_SelfAuthoredCommentOnOthersItemSuppressed(t *testing.T) {
	cfg := fullNotifyConfig()
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "ai-bot" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	// triggerItem's creator is "alice"; our credential is "ai-bot" and the
	// comment we are reacting to was written by "ai-bot".
	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1, Actor: "ai-bot"})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fired, "the AI's own comment on another user's issue must be suppressed")
}

func TestForgeTaskTrigger_ExternalAuthorFires(t *testing.T) {
	cfg := fullNotifyConfig()
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "bot-account" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1, Actor: "alice"})

	require.Eventually(t, func() bool {
		return fired == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestForgeTaskTrigger_UnknownIdentityDoesNotSuppress(t *testing.T) {
	cfg := fullNotifyConfig()
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	require.Eventually(t, func() bool { return fired == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestForgeTaskTrigger_DebounceCollapsesBurst(t *testing.T) {
	cfg := fullNotifyConfig()
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(time.Hour) // effectively "only the first fires"
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	for i := range 5 {
		tr.HandleChange(context.Background(), triggerRepo(), triggerItem(i+1),
			forge.Change{Type: forge.EventCommented, Number: i + 1})
	}

	time.Sleep(80 * time.Millisecond)
	assert.Equal(t, 1, fired, "a burst on one repo must collapse to a single fire")
}

func TestForgeTaskTrigger_QueueDropsNothingWhileRunning(t *testing.T) {
	cfg := fullNotifyConfig()
	var mu sync.Mutex
	var fired []int64
	gate := make(chan struct{})

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(0) // disable debounce so every event is offered
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(taskID int64, _ service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired = append(fired, taskID)
		first := len(fired) == 1
		mu.Unlock()
		if first {
			<-gate // hold the drain loop open while more events arrive
		}
		return true
	})

	// First event starts the drain and blocks.
	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	// Wait until the first fire is in flight.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fired) == 1
	}, 2*time.Second, 5*time.Millisecond)

	// Three more events arrive while the task is running: they must be queued,
	// not dropped.
	for i := 2; i <= 4; i++ {
		tr.HandleChange(context.Background(), triggerRepo(), triggerItem(i),
			forge.Change{Type: forge.EventCommented, Number: i})
	}

	require.Eventually(t, func() bool { return tr.PendingCount(1) >= 1 }, 2*time.Second, 5*time.Millisecond,
		"events arriving during a run must be queued")

	close(gate) // release the drain loop

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fired) == 4
	}, 3*time.Second, 10*time.Millisecond, "all queued events must eventually fire")

	assert.Equal(t, 0, tr.PendingCount(1), "queue must be empty after draining")
}

func TestForgeTaskTrigger_RepoScopedTaskIgnoresOtherRepos(t *testing.T) {
	cfg := fullNotifyConfig()
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", "github|github.com|other/repo")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fired, "a task scoped to another repo must not fire")
}

func TestForgeCompositeSink_FansOut(t *testing.T) {
	var a, b int
	sinkA := sinkFunc(func(context.Context, service.ForgeRepoRef, forge.Item, forge.Change) { a++ })
	sinkB := sinkFunc(func(context.Context, service.ForgeRepoRef, forge.Item, forge.Change) { b++ })

	composite := service.NewForgeCompositeSink(sinkA, nil, sinkB)
	composite.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventOpened, Number: 1})

	assert.Equal(t, 1, a, "first sink must receive the event")
	assert.Equal(t, 1, b, "second sink must receive the event")
}

type sinkFunc func(context.Context, service.ForgeRepoRef, forge.Item, forge.Change)

func (f sinkFunc) HandleChange(ctx context.Context, r service.ForgeRepoRef, i forge.Item, c forge.Change) {
	f(ctx, r, i, c)
}

// TestForgeTaskTrigger_RetriesWhileTaskBusy guards the R6 fix: when the task is
// busy for a reason outside the event queue (e.g. a cron run holds the running
// flag), the event must be retried rather than dropped.
func TestForgeTaskTrigger_RetriesWhileTaskBusy(t *testing.T) {
	cfg := fullNotifyConfig()
	var mu sync.Mutex
	attempts := 0
	succeeded := 0

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented", triggerRepo().Key())}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		// Fail the first two attempts (busy), succeed afterwards.
		if attempts <= 2 {
			return false
		}
		succeeded++
		return true
	})
	tr.SetRetryBackoffForTest(time.Millisecond)

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return succeeded == 1
	}, 2*time.Second, 5*time.Millisecond, "a busy task must be retried until it starts")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, attempts, "two failed attempts then one success")
}

// TestForgeTaskTrigger_DebounceIsPerEventType guards the fix that the debounce
// key includes the event type: a comment arriving right after a close must not
// be swallowed by the close's debounce window.
func TestForgeTaskTrigger_DebounceIsPerEventType(t *testing.T) {
	cfg := fullNotifyConfig()
	var mu sync.Mutex
	var firedTypes []string

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(time.Hour) // only the first of each type fires
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{
			forgeTriggerTask(1, "closed", triggerRepo().Key()),
			forgeTriggerTask(2, "commented", triggerRepo().Key()),
		}, nil
	})
	tr.SetFireForTest(func(_ int64, ev service.ForgeQueuedEvent) bool {
		mu.Lock()
		firedTypes = append(firedTypes, ev.EventTypeForTest())
		mu.Unlock()
		return true
	})

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventClosed, Number: 1})
	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1, Actor: "bob"})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(firedTypes) == 2
	}, 2*time.Second, 10*time.Millisecond,
		"a different event type must not be debounced away by the previous one")
}
