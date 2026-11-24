package service_test

// Tests for the sync-failure record and for the self-authorship guard.
//
// These exist because both failures are INVISIBLE by default: a repository whose
// polling is broken still serves live reads, and an event we suppress leaves no
// trace anywhere the user can see. Both look exactly like "nothing happened".

import (
	"context"
	"errors"
	"testing"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func forgeSyncFailureRepo() service.ForgeRepoKey {
	return service.ForgeRepoKey{
		Platform: "gitea", Host: "codeberg.org", Owner: "acme", Repo: "widgets",
	}
}

func TestForgeSyncFailure_NilWhenNeverFailed(t *testing.T) {
	setupTestDBForForgeSync(t)

	failure, err := service.GetForgeSyncFailure(forgeSyncFailureRepo())
	require.NoError(t, err)
	assert.Nil(t, failure, "a repository that never failed reports no failure")
}

func TestForgeSyncFailure_RecordedThenCleared(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := forgeSyncFailureRepo()

	// A repository can fail before it ever succeeds (bad token, unreachable
	// host) — that is exactly the case the user most needs told about.
	service.RecordForgeSyncFailure(repo, errors.New("list issue page 1: 502 Bad Gateway"))

	failure, err := service.GetForgeSyncFailure(repo)
	require.NoError(t, err)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Message, "502 Bad Gateway")

	service.ClearForgeSyncFailure(repo)
	failure, err = service.GetForgeSyncFailure(repo)
	require.NoError(t, err)
	assert.Nil(t, failure, "a successful poll clears the recorded failure")
}

func TestForgeSyncFailure_IsPerRepository(t *testing.T) {
	setupTestDBForForgeSync(t)
	failing := forgeSyncFailureRepo()
	healthy := service.ForgeRepoKey{
		Platform: "github", Host: "github.com", Owner: "acme", Repo: "widgets",
	}

	service.RecordForgeSyncFailure(failing, errors.New("boom"))

	failure, err := service.GetForgeSyncFailure(healthy)
	require.NoError(t, err)
	assert.Nil(t, failure, "one repository's failure must not be reported for another")
}

// TestForgeSyncFailure_SurvivesAReadableWatermark guards the ON CONFLICT path:
// a repository that has a watermark row (i.e. has synced before) must still be
// able to record a failure onto that same row.
func TestForgeSyncFailure_SurvivesAReadableWatermark(t *testing.T) {
	setupTestDBForForgeSync(t)
	repo := forgeSyncFailureRepo()

	require.NoError(t, service.SetForgeSyncWatermark(repo, forge.ItemTypeIssue, time.Now().Add(-time.Hour)))
	service.RecordForgeSyncFailure(repo, errors.New("network unreachable"))

	failure, err := service.GetForgeSyncFailure(repo)
	require.NoError(t, err)
	require.NotNil(t, failure)
	assert.Equal(t, "network unreachable", failure.Message)

	// The watermark itself must be untouched by the failure write.
	wm, err := service.GetForgeSyncWatermark(repo, forge.ItemTypeIssue)
	require.NoError(t, err)
	assert.False(t, wm.IsZero(), "recording a failure must not disturb the cursor")
}

// ── Self-authorship guard ─────────────────────────────────────────────────
//
// The guard itself is intentional (an AI's own write must not re-trigger it).
// What these pin is the exact conditions, because "my issue did not fire" and
// "the guard ate it" are indistinguishable to a user.

func TestForgeTaskTrigger_SelfOpenedIssueIsSuppressed(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "me" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "issue.opened")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	// Derive the event the way the syncer does, so the Actor wiring is the real
	// one: an `opened` change carries the item creator as its actor.
	createdAt := time.Now()
	changes := forge.DeriveChanges(nil, forge.ItemState{
		State: "open", Author: "me", CreatedAt: createdAt,
	}, 1, createdAt.Add(-time.Second))
	require.Len(t, changes, 1)
	require.Equal(t, forge.EventOpened, changes[0].Type)

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1), changes[0])
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 0, fired,
		"an issue opened by the credential's own account must not fire: that is the anti-recursion guard")
}

func TestForgeTaskTrigger_OthersIssueFires(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "me" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "issue.opened")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	createdAt := time.Now()
	changes := forge.DeriveChanges(nil, forge.ItemState{
		State: "open", Author: "someone-else", CreatedAt: createdAt,
	}, 1, createdAt.Add(-time.Second))
	require.Len(t, changes, 1)

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1), changes[0])
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, fired, "an issue opened by another account must fire")
}

// TestForgeTaskTrigger_UnknownIdentityFailsOpen pins the deliberate failure
// mode: when the credential's login cannot be resolved (no credential, API
// failure), the event fires rather than being silently dropped. Being wrong in
// that direction costs one spurious run; being wrong the other way loses the
// event with no trace at all.
func TestForgeTaskTrigger_UnknownIdentityFailsOpen(t *testing.T) {
	cfg := fullNotifyConfig()
	bindProjectRepo(t, triggerRepo())
	var fired int

	tr := service.NewForgeTaskTrigger(nil, func() model.Config { return cfg },
		func(service.ForgeRepoRef) string { return "" })
	tr.SetListTasksForTest(func() ([]model.ScheduledTask, error) {
		return []model.ScheduledTask{forgeTriggerTask(1, "issue.opened")}, nil
	})
	tr.SetFireForTest(func(int64, service.ForgeQueuedEvent) bool { fired++; return true })

	tr.HandleChange(context.Background(), triggerRepo(), triggerItem(1),
		forge.Change{Type: forge.EventOpened, Number: 1, Actor: "me"})
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, fired, "an unresolved credential identity must not suppress the event")
}
