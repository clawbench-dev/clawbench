package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
)

// ForgeTaskTrigger matches freshly derived forge events against event-triggered
// tasks and fires them.
//
// It exists because the plain TriggerTask path drops events: taskRunning is a
// load-or-store flag, so an event arriving while its task is already running is
// discarded. Here, concurrent events are queued and drained once the task
// finishes, so a burst is deferred rather than lost. A per-repo debounce merges
// same-repo bursts, and a global kill-switch (config) stops all firing.
type ForgeTaskTrigger struct {
	cfgFn func() model.Config
	// fire starts one execution of a task with the event attached. It returns
	// false when the task is busy and the event should be retried later.
	// Injectable so tests can assert matching/queueing without running an AI
	// backend.
	fire func(taskID int64, ev ForgeQueuedEvent) bool
	// identityFn resolves the credential's login for a repo so events authored
	// by the AI itself can be suppressed (anti-recursion). May be nil.
	identityFn func(repo ForgeRepoRef) string
	// listTasks returns the candidate event tasks. Injectable for tests.
	listTasks func() ([]model.ScheduledTask, error)

	mu       sync.Mutex
	queues   map[int64][]ForgeQueuedEvent // task ID -> pending events
	running  map[int64]bool               // task ID -> currently draining
	lastFire map[string]time.Time         // repo key+type -> last fire time (debounce)
	identity map[string]identityEntry     // repo key -> resolved login + expiry

	// debounceWindow collapses bursts on the same repo into one fire.
	debounceWindow time.Duration
	// identityTTL bounds how long a resolved credential login is trusted, so a
	// credential change is picked up without a restart.
	identityTTL time.Duration
	// maxQueue caps pending events per task so a runaway repo cannot grow
	// memory without bound. Oldest events are dropped first.
	maxQueue int
	// maxFireAttempts bounds retries when the task is busy for a reason outside
	// this queue (a cron/manual run), so a stuck task cannot spin forever.
	maxFireAttempts int
	// retryBackoff is the pause between busy retries.
	retryBackoff time.Duration
	// now is injectable for tests.
	now func() time.Time
}

// ForgeQueuedEvent is one pending event awaiting its task's turn to run.
type ForgeQueuedEvent struct {
	repo   ForgeRepoRef
	item   forge.Item
	change forge.Change
}

// forgeEventKindForItem maps a forge item type to its subscription kind. It
// lives here rather than in scheduler.go so the scheduler does not need to
// import the forge package.
func forgeEventKindForItem(t forge.ItemType) string {
	if t == forge.ItemTypeChangeRequest {
		return forgeEventKindPR
	}
	return forgeEventKindIssue
}

// identityEntry is a cached credential login with its expiry.
type identityEntry struct {
	login   string
	expires time.Time
}

// NewForgeTaskTrigger builds a trigger bound to the given scheduler. A nil
// scheduler disables firing (used by tests that only exercise matching).
func NewForgeTaskTrigger(scheduler *Scheduler, cfgFn func() model.Config, identityFn func(ForgeRepoRef) string) *ForgeTaskTrigger {
	if cfgFn == nil {
		cfgFn = func() model.Config { return model.ConfigInstance }
	}
	t := &ForgeTaskTrigger{
		cfgFn:           cfgFn,
		identityFn:      identityFn,
		listTasks:       func() ([]model.ScheduledTask, error) { return GetTasks("") },
		queues:          make(map[int64][]ForgeQueuedEvent),
		running:         make(map[int64]bool),
		lastFire:        make(map[string]time.Time),
		identity:        make(map[string]identityEntry),
		debounceWindow:  30 * time.Second,
		identityTTL:     10 * time.Minute,
		maxQueue:        100,
		maxFireAttempts: 30,
		retryBackoff:    10 * time.Second,
		now:             time.Now,
	}
	if scheduler != nil {
		t.fire = func(taskID int64, ev ForgeQueuedEvent) bool {
			return scheduler.triggerTaskWithContext(taskID, "event", EventContextFromChange(ev.repo, ev.item, ev.change))
		}
	}
	return t
}

// SetFireForTest overrides the fire function (test helper).
func (t *ForgeTaskTrigger) SetFireForTest(fn func(taskID int64, ev ForgeQueuedEvent) bool) {
	t.fire = fn
}

// SetListTasksForTest overrides task enumeration (test helper).
func (t *ForgeTaskTrigger) SetListTasksForTest(fn func() ([]model.ScheduledTask, error)) {
	t.listTasks = fn
}

// SetDebounceWindowForTest overrides the debounce window (test helper).
func (t *ForgeTaskTrigger) SetDebounceWindowForTest(d time.Duration) { t.debounceWindow = d }

// SetRetryBackoffForTest overrides the busy-retry backoff (test helper).
func (t *ForgeTaskTrigger) SetRetryBackoffForTest(d time.Duration) { t.retryBackoff = d }

// EventTypeForTest exposes the queued event's type to tests.
func (e ForgeQueuedEvent) EventTypeForTest() string { return string(e.change.Type) }

// HandleChange matches an event against the event-triggered tasks and fires the
// matching ones. It implements ForgeChangeSink alongside the notifier, so both
// run off the same derived event.
func (t *ForgeTaskTrigger) HandleChange(_ context.Context, repo ForgeRepoRef, item forge.Item, change forge.Change) {
	cfg := t.cfgFn()

	// Global kill-switch: when paused, no event task fires. Notifications are
	// unaffected — the user still sees what happened.
	if cfg.Forge.PauseEventTasks {
		slog.Debug("forge event task suppressed by kill-switch", slog.String("event", string(change.Type)))
		return
	}

	// Anti-recursion: an event authored by the credential's own account is
	// almost certainly a side effect of a previous AI action. Firing on it would
	// let the AI's own write re-trigger itself indefinitely.
	if t.isSelfAuthored(repo, item, change) {
		slog.Info("forge event suppressed as self-authored",
			slog.String("repo", repo.Key()),
			slog.Int("number", item.Number),
			slog.String("actor", change.Actor))
		return
	}

	tasks := t.matchingTasks(repo, item, change)
	if len(tasks) == 0 {
		return
	}

	// Per-repo, per-type debounce: a burst of the same event (e.g. five comments
	// in a row) fires the task once, not five times. The event type is part of
	// the key so a comment arriving right after a close is not swallowed, and
	// the item kind is too so a new issue right after a new PR is not either.
	debounceKey := repo.Key() + "\x00" + forgeEventKindForItem(item.Type) + "\x00" + string(change.Type)
	t.mu.Lock()
	if last, ok := t.lastFire[debounceKey]; ok && t.now().Sub(last) < t.debounceWindow {
		t.mu.Unlock()
		slog.Debug("forge event task debounced",
			slog.String("repo", repo.Key()), slog.String("event", string(change.Type)))
		return
	}
	t.lastFire[debounceKey] = t.now()
	t.mu.Unlock()

	ev := ForgeQueuedEvent{repo: repo, item: item, change: change}
	for _, task := range tasks {
		t.enqueueAndDrain(task.ID, ev)
	}
}

// isSelfAuthored reports whether the event was produced by the credential's own
// account. Resolving the identity is best-effort: if it cannot be determined,
// the event is treated as external (firing is the safer default for usefulness,
// and the kill-switch remains available).
//
// The comparison must use the *acting* user, not the item creator: for a
// comment the author is the commenter, and for close/merge it is whoever
// performed the transition. Comparing the item creator would miss exactly the
// case this guard exists for — the AI commenting on someone else's issue.
func (t *ForgeTaskTrigger) isSelfAuthored(repo ForgeRepoRef, item forge.Item, change forge.Change) bool {
	if t.identityFn == nil {
		return false
	}
	login := t.identityFor(repo)
	if login == "" {
		return false
	}
	// Prefer the actor that caused this specific event; fall back to the item
	// creator only for events where the creator is the actor (e.g. opened).
	actor := change.Actor
	if actor == "" && change.Type == forge.EventOpened {
		actor = item.Author.Login
	}
	return actor != "" && actor == login
}

func (t *ForgeTaskTrigger) identityFor(repo ForgeRepoRef) string {
	key := repo.Key()
	now := t.now()
	t.mu.Lock()
	if v, ok := t.identity[key]; ok && now.Before(v.expires) {
		t.mu.Unlock()
		return v.login
	}
	t.mu.Unlock()

	login := t.identityFn(repo)

	t.mu.Lock()
	t.identity[key] = identityEntry{login: login, expires: now.Add(t.identityTTL)}
	t.mu.Unlock()
	return login
}

// matchingTasks returns the active event tasks that subscribe to this event and
// whose project is bound to its repository.
//
// An event task always watches the repository its project is bound to. There is
// no per-task repo scope: the binding is the single source of truth, so a task
// cannot be pointed at a repository the project does not own.
func (t *ForgeTaskTrigger) matchingTasks(repo ForgeRepoRef, item forge.Item, change forge.Change) []model.ScheduledTask {
	tasks, err := t.listTasks()
	if err != nil {
		slog.Warn("forge task trigger: list tasks failed", slog.String("err", err.Error()))
		return nil
	}
	var out []model.ScheduledTask
	for i := range tasks {
		task := &tasks[i]
		if !task.IsEventTriggered() || task.Status != SessionArchiveFilterActive {
			continue
		}
		if !eventTypeSubscribed(task, item.Type, change.Type) {
			continue
		}
		if !t.projectBindsRepo(task.ProjectPath, repo) {
			continue
		}
		out = append(out, *task)
	}
	return out
}

// projectBindsRepo reports whether the task's project is bound to the repo.
func (t *ForgeTaskTrigger) projectBindsRepo(projectPath string, repo ForgeRepoRef) bool {
	pf, err := GetProjectForge(projectPath)
	if err != nil || pf == nil {
		return false
	}
	return pf.Platform == repo.Platform && pf.Host == repo.Host &&
		pf.Owner == repo.Owner && pf.Repo == repo.Repo
}

// eventTypeSubscribed reports whether the task subscribes to this event.
//
// An event is identified by both the item kind (issue vs PR) and the
// transition, so a task can subscribe to "a new issue" without also firing for
// "a new PR". Two spellings match:
//   - the kind-scoped key, e.g. "issue.opened" / "pr.opened";
//   - the bare legacy key, e.g. "opened", which matches either kind. Tasks
//     stored before the split used bare keys, so this keeps them working
//     without a migration.
func eventTypeSubscribed(task *model.ScheduledTask, itemType forge.ItemType, typ forge.EventType) bool {
	kind := forgeEventKindForItem(itemType)
	scoped := forgeEventKey(kind, string(typ))
	for _, t := range task.EventTypeList() {
		if t == scoped || t == string(typ) {
			return true
		}
	}
	return false
}

// enqueueAndDrain appends an event and, if the task is idle, starts draining.
func (t *ForgeTaskTrigger) enqueueAndDrain(taskID int64, ev ForgeQueuedEvent) {
	t.mu.Lock()
	if t.running[taskID] {
		q := t.queues[taskID]
		q = append(q, ev)
		if len(q) > t.maxQueue {
			q = q[len(q)-t.maxQueue:]
		}
		t.queues[taskID] = q
		t.mu.Unlock()
		slog.Debug("forge event queued behind running task",
			slog.Int64("task_id", taskID), slog.Int("queued", len(q)))
		return
	}
	t.running[taskID] = true
	t.mu.Unlock()

	go t.drain(taskID, ev)
}

// drain runs queued events for a task one at a time until the queue is empty.
//
// If the task is busy for a reason outside this queue (a cron or manual run
// holds the scheduler's running flag), firing returns false. The event is then
// retried after a short backoff rather than dropped — losing it is the exact
// failure this queue exists to prevent. Retries are bounded so a permanently
// stuck task cannot spin forever.
func (t *ForgeTaskTrigger) drain(taskID int64, first ForgeQueuedEvent) {
	current := first
	attempts := 0
	for {
		started := true
		if t.fire != nil {
			started = t.fire(taskID, current)
		}

		if !started {
			attempts++
			if attempts >= t.maxFireAttempts {
				slog.Warn("forge event dropped after repeated busy attempts",
					slog.Int64("task_id", taskID), slog.Int("attempts", attempts))
			} else {
				// Back off, then retry the SAME event (do not advance the queue).
				time.Sleep(t.retryBackoff)
				continue
			}
		}
		attempts = 0

		t.mu.Lock()
		q := t.queues[taskID]
		if len(q) == 0 {
			delete(t.queues, taskID)
			delete(t.running, taskID)
			t.mu.Unlock()
			return
		}
		current = q[0]
		t.queues[taskID] = q[1:]
		t.mu.Unlock()
	}
}

// PendingCount reports queued events for a task (test/diagnostic helper).
func (t *ForgeTaskTrigger) PendingCount(taskID int64) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.queues[taskID])
}

// IsDraining reports whether a task currently has a drain loop running (test
// helper).
func (t *ForgeTaskTrigger) IsDraining(taskID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running[taskID]
}
