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

// bindProjectRepo binds a project to a repository so an event task rooted at
// that project can match events. Matching is driven entirely by this binding —
// there is no per-task repository scope.
func bindProjectRepo(t *testing.T, repo service.ForgeRepoRef) {
	t.Helper()
	setupTestDBForForgeSync(t)
	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: "/proj",
		Platform:    repo.Platform,
		Host:        repo.Host,
		Owner:       repo.Owner,
		Repo:        repo.Repo,
	}))
}

// eventTask builds an active event-triggered task. It carries no repository
// scope: it always watches its own project's binding.
func forgeTriggerTask(id int64, eventTypes string) model.ScheduledTask {
	return model.ScheduledTask{
		ID:          id,
		ProjectPath: "/proj",
		Name:        "task",
		Status:      "active",
		TriggerMode: "event",
		EventTypes:  eventTypes,
	}
}

func TestForgeTaskTrigger_FiresMatchingTask(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var fired []int64
	var mu sync.Mutex

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{
			forgeTriggerTask(1, "commented"),
			forgeTriggerTask(2, "opened"), // not subscribed to commented
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

// TestForgePipelineEventTypeIsOffered pins the change that makes CI events
// usable: pipeline_done is now a real, derivable event, so it IS offered.
//
// It is offered in its BARE form only. A pipeline belongs to the repository,
// not to a PR, so "pr.pipeline_done" stays accepted (a task that stored that
// spelling while the event was retired must not be rejected on its next edit)
// but is not presented as a checkbox.
func TestForgePipelineEventTypeIsOffered(t *testing.T) {
	offered := service.OfferedForgeEventTypesForTest()
	assert.Contains(t, offered, "pipeline_done", "the repository-level event must be offered")
	assert.NotContains(t, offered, "pr.pipeline_done", "the PR-scoped spelling must not be offered")
	assert.NotContains(t, offered, "issue.pipeline_done", "an issue has no CI")

	// Bare form is valid (the offered one).
	assert.NoError(t, service.ValidateEventSubscription("pipeline_done"))
	// The legacy PR-scoped spelling stays valid so an existing task survives an
	// edit, since the editor renders no checkbox to remove it.
	assert.NoError(t, service.ValidateEventSubscription("pr.pipeline_done"))
	// An issue can never have a pipeline.
	assert.Error(t, service.ValidateEventSubscription("issue.pipeline_done"))

	// Unknown types are still rejected.
	assert.Error(t, service.ValidateEventSubscription("pr.bogus"))
}

// TestForgeTaskTrigger_PipelineEventFiresTask covers the subscription match for
// a repository-level event: the bare key matches.
func TestForgeTaskTrigger_PipelineEventFiresTask(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	var fired []int64
	var mu sync.Mutex
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "pipeline_done")}, nil
	})
	tr.SetFireForTest(func(taskID int64, _ service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired = append(fired, taskID)
		mu.Unlock()
		return true
	})

	tr.HandleChange(context.Background(), triggerRepo(), pipelineTriggerItem(),
		forge.Change{Type: forge.EventPipeline, PipelineRunID: 42, PipelineStatus: "failure"})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fired) == 1
	}, 2*time.Second, 10*time.Millisecond, "a pipeline_done subscription must fire")
}

// TestForgeTaskTrigger_PipelineDoesNotFireIssueOrPRTask is the cross-kind
// guard: a repository-level event must not wake a task that only watches
// issues or PRs.
func TestForgeTaskTrigger_PipelineDoesNotFireIssueOrPRTask(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	for _, subscribed := range []string{"issue.opened", "pr.opened", "commented"} {
		t.Run(subscribed, func(t *testing.T) {
			var fired int
			var mu sync.Mutex
			tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
			tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
				return []model.ScheduledTask{forgeTriggerTask(1, subscribed)}, nil
			})
			tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool {
				mu.Lock()
				fired++
				mu.Unlock()
				return true
			})

			tr.HandleChange(context.Background(), triggerRepo(), pipelineTriggerItem(),
				forge.Change{Type: forge.EventPipeline, PipelineRunID: 42})

			time.Sleep(120 * time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			assert.Zero(t, fired, "a pipeline event must not fire an issue/PR task")
		})
	}
}

// TestForgeTaskTrigger_PipelineNotSuppressedAsSelfAuthored is the user-facing
// requirement: a pipeline succeeding or failing is NOT a user-initiated action,
// so the anti-recursion guard must not swallow the user's own pipelines.
//
// Suppressing them would break the most valuable loop there is — the AI pushes
// a fix, CI fails, and the repair task never runs.
func TestForgeTaskTrigger_PipelineNotSuppressedAsSelfAuthored(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	var fired int
	var mu sync.Mutex
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "ai-bot" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "pipeline_done")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired++
		mu.Unlock()
		return true
	})

	// The credential's OWN account triggered the run, and it still must fire.
	item := pipelineTriggerItem()
	item.Author = forge.Author{Login: "ai-bot"}
	tr.HandleChange(context.Background(), triggerRepo(), item,
		forge.Change{Type: forge.EventPipeline, PipelineRunID: 42, Actor: "ai-bot"})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return fired == 1
	}, 2*time.Second, 10*time.Millisecond,
		"a self-triggered pipeline must still fire; the prompt decides what to do")
}

// TestForgeTaskTrigger_PipelineDebounceIsPerRun: two different runs finishing
// inside the debounce window are separate events. Keying the debounce by
// repo+kind+type (as issue/PR events do) would collapse them and silently drop
// every run but the first.
func TestForgeTaskTrigger_PipelineDebounceIsPerRun(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	var fired []int64
	var mu sync.Mutex
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	// A window far longer than the test, so only the run-id key can save us.
	tr.SetDebounceWindowForTest(time.Hour)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "pipeline_done")}, nil
	})
	tr.SetFireForTest(func(taskID int64, ev service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired = append(fired, ev.PipelineRunIDForTest())
		mu.Unlock()
		return true
	})

	for _, runID := range []int64{100, 101} {
		tr.HandleChange(context.Background(), triggerRepo(), pipelineTriggerItem(),
			forge.Change{Type: forge.EventPipeline, PipelineRunID: runID, PipelineStatus: "success"})
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(fired) == 2
	}, 2*time.Second, 10*time.Millisecond, "each run must fire separately")
	assert.ElementsMatch(t, []int64{100, 101}, fired)
}

// TestForgeTaskTrigger_PipelineDebounceStillCollapsesSameRun guards the other
// side: the SAME run re-delivered (a re-derivation) must not fire twice.
func TestForgeTaskTrigger_PipelineDebounceStillCollapsesSameRun(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	var fired int
	var mu sync.Mutex
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(time.Hour)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "pipeline_done")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool {
		mu.Lock()
		fired++
		mu.Unlock()
		return true
	})

	for range 3 {
		tr.HandleChange(context.Background(), triggerRepo(), pipelineTriggerItem(),
			forge.Change{Type: forge.EventPipeline, PipelineRunID: 42, PipelineStatus: "success"})
	}

	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, fired, "the same run must fire once")
}

// pipelineTriggerItem builds the synthetic item a CI event travels on.
func pipelineTriggerItem() forge.Item {
	return forge.Item{
		Platform: forge.PlatformGitHub, Type: forge.ItemTypePipeline, Number: 0,
		Title: "CI", State: forge.State("failure"),
		URL: "https://github.com/acme/widgets/actions/runs/42",
	}
}

// TestValidateEventSubscription_KindScopedKeys covers the accepted vocabulary.
func TestValidateEventSubscription_KindScopedKeys(t *testing.T) {
	valid := []string{
		"issue.opened", "pr.opened", "pr.merged", "issue.commented",
		"issue.opened,pr.merged",
		"opened", // bare legacy key still accepted
		// The repository-level pipeline event, in its offered bare form.
		"pipeline_done",
		// The PR-scoped spelling predates the bare form and stays accepted so an
		// existing task is not rejected on its next edit (the editor renders no
		// checkbox for it).
		"pr.pipeline_done",
	}
	for _, v := range valid {
		assert.NoError(t, service.ValidateEventSubscription(v), "expected %q to be valid", v)
	}

	invalid := []string{
		"issue.merged",        // an issue can never be merged
		"issue.pipeline_done", // issues have no CI
		"bogus.opened",
		"pr.bogus",
	}
	for _, v := range invalid {
		assert.Error(t, service.ValidateEventSubscription(v), "expected %q to be rejected", v)
	}
}

// TestForgeTaskTrigger_SplitsIssueAndPR verifies that "a new issue" and "a new
// PR" are independent triggers: subscribing to issue.opened must NOT fire for a
// PR, and vice versa. Before the split both collapsed onto the same "opened".
func TestForgeTaskTrigger_SplitsIssueAndPR(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	prItem := triggerItem(2)
	prItem.Type = forge.ItemTypeChangeRequest

	cases := []struct {
		name       string
		subscribed string
		item       forge.Item
		wantFired  bool
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
				return []model.ScheduledTask{forgeTriggerTask(1, tc.subscribed)}, nil
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
	bindProjectRepo(t, triggerRepo())
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
				return []model.ScheduledTask{forgeTriggerTask(1, "opened")}, nil
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
	bindProjectRepo(t, triggerRepo())
	prItem := triggerItem(2)
	prItem.Type = forge.ItemTypeChangeRequest

	var fired []int64
	var mu sync.Mutex
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(10 * time.Minute) // long, so only key separation can let the 2nd through
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "issue.opened,pr.opened")}, nil
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

func TestForgeTaskTrigger_KillSwitchSuppresses(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	cfg.Forge.PauseEventTasks = true
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fired, "kill-switch must prevent firing")
}

func TestForgeTaskTrigger_SelfAuthoredSuppressed(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "alice" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
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
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "ai-bot" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
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
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "bot-account" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
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
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	require.Eventually(t, func() bool { return fired == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestForgeTaskTrigger_DebounceCollapsesBurst(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(time.Hour) // effectively "only the first fires"
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
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
	bindProjectRepo(t, triggerRepo())
	var mu sync.Mutex
	var fired []int64
	gate := make(chan struct{})

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(0) // disable debounce so every event is offered
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
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

// TestForgeTaskTrigger_ProjectBoundToOtherRepoIgnoresEvent verifies the
// project binding is the sole gate: a task whose project is bound to a
// different repository must not fire for this event.
func TestForgeTaskTrigger_ProjectBoundToOtherRepoIgnoresEvent(t *testing.T) {
	cfg := fullNotifyConfig()
	// The project is bound to another repo, so events from triggerRepo() are
	// not its own.
	bindProjectRepo(t, service.ForgeRepoRef{
		Platform: "github", Host: "github.com", Owner: "other", Repo: "repo",
	})
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fired, "a task whose project binds another repo must not fire")
}

// TestForgeTaskTrigger_UnboundProjectNeverFires pins the "no binding, no
// trigger" behavior: an event task on a project with no repository binding is
// silently inert. This is the case the task form warns about.
func TestForgeTaskTrigger_UnboundProjectNeverFires(t *testing.T) {
	cfg := fullNotifyConfig()
	setupTestDBForForgeSync(t) // DB present, but no binding row for /proj
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventCommented, Number: 1})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fired, "a task on an unbound project must not fire")
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
	bindProjectRepo(t, triggerRepo())
	var mu sync.Mutex
	attempts := 0
	succeeded := 0

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "commented")}, nil
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
	bindProjectRepo(t, triggerRepo())
	var mu sync.Mutex
	var firedTypes []string

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(time.Hour) // only the first of each type fires
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{
			forgeTriggerTask(1, "closed"),
			forgeTriggerTask(2, "commented"),
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

// --- One event, many matching tasks ---
//
// matchingTasks accumulates every match and HandleChange enqueues each one
// independently, so a single event fans out to all subscribed tasks. That
// behavior is load-bearing (a user may register several tasks for the same
// event) but was previously untested: the only fan-out case covered multiple
// *sinks*, not multiple tasks. A `break` in matchingTasks or a taskRunning key
// change would have gone unnoticed.

func TestForgeTaskTrigger_FiresAllMatchingTasks(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var mu sync.Mutex
	var fired []int64

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(0)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{
			forgeTriggerTask(1, "commented"),
			forgeTriggerTask(2, "commented"),
			forgeTriggerTask(3, "commented"),
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
		return len(fired) == 3
	}, 3*time.Second, 10*time.Millisecond,
		"every subscribed task must fire")

	mu.Lock()
	defer mu.Unlock()
	// Order is not guaranteed: each task drains on its own goroutine.
	assert.ElementsMatch(t, []int64{1, 2, 3}, fired)
}

// TestForgeTaskTrigger_OneEventFiresEachTaskOnce asserts the fan-out does not
// multiply. The debounce key is repo+kind+eventType with no task component, so
// one event produces exactly one fire per task — not one per (task, task) pair
// and not a re-fire for each subsequent task examined.
func TestForgeTaskTrigger_OneEventFiresEachTaskOnce(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var mu sync.Mutex
	var fired []int64

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(0)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{
			forgeTriggerTask(1, "commented"),
			forgeTriggerTask(2, "commented"),
			forgeTriggerTask(3, "commented"),
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
		return len(fired) >= 3
	}, 3*time.Second, 10*time.Millisecond)

	// Give any spurious extra fires a chance to appear before asserting.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, fired, 3, "each task fires exactly once for one event")
	counts := map[int64]int{}
	for _, id := range fired {
		counts[id]++
	}
	for id, n := range counts {
		assert.Equalf(t, 1, n, "task %d fired %d times", id, n)
	}
}

// TestForgeTaskTrigger_MixedSubscriptionsFireOnlySubscribed asserts the fan-out
// stays precise: a task subscribed to a different event must not fire. Guards
// against the matching predicate loosening into "fire everything".
func TestForgeTaskTrigger_MixedSubscriptionsFireOnlySubscribed(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var mu sync.Mutex
	var fired []int64

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg }, nil)
	tr.SetDebounceWindowForTest(0)
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{
			forgeTriggerTask(1, "commented"),
			forgeTriggerTask(2, "commented"),
			forgeTriggerTask(3, "opened"), // not subscribed
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
		return len(fired) >= 2
	}, 3*time.Second, 10*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []int64{1, 2}, fired, "only subscribed tasks fire")
	assert.NotContains(t, fired, int64(3), "a task subscribed to another event must not fire")
}
