package service_test

// Diagnostics for "the event arrived but nothing ran". Both the sync-failure
// record and the no-match log exist because silence is indistinguishable from a
// healthy system; these tests pin that they stay reachable (including the paths
// where the database is absent or a write fails — those must degrade quietly,
// never panic).

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Without a database every helper is a no-op. A poll failure must not crash the
// syncer when the store has not been initialized yet (early startup).
func TestForgeSyncFailure_WithoutDatabase(t *testing.T) {
	cleanup := store.SetDBForTest(nil, nil)
	defer cleanup()

	repo := forgeSyncFailureRepo()
	service.RecordForgeSyncFailure(repo, errors.New("boom"))
	service.ClearForgeSyncFailure(repo)

	failure, err := service.GetForgeSyncFailure(repo)
	require.NoError(t, err)
	assert.Nil(t, failure)
}

// A write failure is logged, not returned: recording the reason a poll failed
// must never itself break the poll loop. The read side still works, so this
// exercises both warn branches (record and clear).
func TestForgeSyncFailure_WriteFailureIsSwallowed(t *testing.T) {
	read := setupTestDBForForgeSync(t)
	repo := forgeSyncFailureRepo()
	service.RecordForgeSyncFailure(repo, errors.New("first failure"))

	closed, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, closed.Close())

	cleanup := store.SetDBForTest(closed, read)
	defer cleanup()

	// Write fails (closed DB), read still sees the recorded failure.
	service.RecordForgeSyncFailure(repo, errors.New("second failure"))
	service.ClearForgeSyncFailure(repo)

	failure, err := service.GetForgeSyncFailure(repo)
	require.NoError(t, err)
	require.NotNil(t, failure, "the read side must be unaffected by the failed write")
	assert.Equal(t, "first failure", failure.Message)
}

// When listTasks itself fails there is nothing to name, and the handler must
// still return without firing anything.
func TestForgeTaskTrigger_ListTasksFailureDoesNotFire(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	fired := 0
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "someone-else" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return nil, errors.New("db down")
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventOpened, Number: 1, Actor: "alice"})

	assert.Equal(t, 0, fired)
}

// A task that exists but is not event-triggered is not a subscription; the
// diagnostic line must say "no active event task" rather than naming it.
func TestForgeTaskTrigger_NonEventTaskIsNotASubscription(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())

	fired := 0
	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "someone-else" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{{
			ID:          7,
			ProjectPath: "/proj",
			Name:        "cron task",
			Status:      store.SessionArchiveFilterActive,
			TriggerMode: "cron",
		}}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventOpened, Number: 1, Actor: "alice"})

	assert.Equal(t, 0, fired, "a cron task must not be treated as an event subscription")
}
