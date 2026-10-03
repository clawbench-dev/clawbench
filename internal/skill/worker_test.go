package skill

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// setupWorkerEnv installs a fresh config/registry and restores the globals and
// worker state afterwards, so a test never leaks a running goroutine or a
// persisted sync result into the next one.
func setupWorkerEnv(t *testing.T) {
	t.Helper()

	origCfg := model.ConfigInstance
	origDataDir := model.DataDir
	origPersist := persistSyncStateFn
	t.Cleanup(func() {
		ResetSyncForTest()
		model.ConfigInstance = origCfg
		model.DataDir = origDataDir
		persistSyncStateFn = origPersist
		ResetForTest()
	})

	root := t.TempDir()
	model.DataDir = root
	setTestHome(t, t.TempDir())
	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{Enabled: true},
	}
	ResetSyncForTest()
	ResetForTest()
}

// TestFirstError pins the coarse "did anything fail" helper: an empty map
// yields "", and a populated one yields one of its messages (order is
// deliberately unspecified because it is only used for display).
func TestFirstError(t *testing.T) {
	assert.Equal(t, "", firstError(nil))
	assert.Equal(t, "", firstError(map[string]string{}))

	got := firstError(map[string]string{"a": "boom"})
	assert.Equal(t, "boom", got)
}

// TestDoSync_NoReposScansAndRecords pins the baseline pass: with no repos
// configured the sync still rescans local sources, reports the discovered
// count, and stamps LastSyncAt so the settings page shows a fresh timestamp.
func TestDoSync_NoReposScansAndRecords(t *testing.T) {
	setupWorkerEnv(t)

	// Seed one skill so SkillCount is non-zero.
	userDir := filepath.Join(model.DataDir, "user")
	writeSkill(t, userDir, "alpha", "---\nname: alpha\ndescription: Alpha skill\n---\nBody")

	model.ConfigInstance.Skills.Dirs = []string{userDir}

	res := doSync()
	assert.Empty(t, res.Errors)
	assert.GreaterOrEqual(t, res.SkillCount, 1)
	assert.NotZero(t, model.ConfigInstance.Skills.LastSyncAt)
	assert.Empty(t, model.ConfigInstance.Skills.LastError)
}

// TestDoSync_PerRepoErrorIsRecorded pins the failure path: an unreachable repo
// is recorded in the result map AND mirrored onto the matching Repo entry's
// LastError, so the settings UI can show the reason inline.
func TestDoSync_PerRepoErrorIsRecorded(t *testing.T) {
	setupWorkerEnv(t)

	model.ConfigInstance.Skills.Repos = []model.SkillRepo{
		{URL: "file:///nonexistent/repo.git", Slug: "broken-repo"},
	}

	res := doSync()
	require.Contains(t, res.Errors, "broken-repo")
	assert.NotEmpty(t, model.ConfigInstance.Skills.LastError)
	require.Len(t, model.ConfigInstance.Skills.Repos, 1)
	assert.NotEmpty(t, model.ConfigInstance.Skills.Repos[0].LastError)
}

// TestRescanFilesystem_DiscoversWithoutSyncState pins the pure-local rescan:
// a skill dropped into a directory after the last scan is discovered, and the
// sync state is left untouched (no network work happened).
func TestRescanFilesystem_DiscoversWithoutSyncState(t *testing.T) {
	setupWorkerEnv(t)

	userDir := filepath.Join(model.DataDir, "user")
	model.ConfigInstance.Skills.Dirs = []string{userDir}
	// A stale sync outcome that rescan must NOT clear or overwrite.
	model.ConfigInstance.Skills.LastSyncAt = 123
	model.ConfigInstance.Skills.LastError = "previous failure"

	// Nothing on disk yet.
	assert.Equal(t, 0, RescanFilesystem())

	writeSkill(t, userDir, "later", "---\nname: later\ndescription: added later\n---\n")

	n := RescanFilesystem()
	assert.GreaterOrEqual(t, n, 1, "a skill added after the last scan must be discovered")

	assert.Equal(t, int64(123), model.ConfigInstance.Skills.LastSyncAt,
		"rescan must not stamp a sync time")
	assert.Equal(t, "previous failure", model.ConfigInstance.Skills.LastError,
		"rescan must not clear or overwrite the sync error")
}

// TestRecordSyncResult_ClearsStalePerRepoError pins that a successful pass
// clears a previous failure message: a stale error must not outlive its fix.
func TestRecordSyncResult_ClearsStalePerRepoError(t *testing.T) {
	setupWorkerEnv(t)

	model.ConfigInstance.Skills.Repos = []model.SkillRepo{
		{URL: "https://example.invalid/skills.git", Slug: "ok-repo", LastError: "old failure"},
	}

	recordSyncResult(SyncResult{Errors: map[string]string{}})

	assert.Empty(t, model.ConfigInstance.Skills.Repos[0].LastError,
		"a clean pass must clear the repo's previous error")
	assert.Empty(t, model.ConfigInstance.Skills.LastError)
	assert.NotZero(t, model.ConfigInstance.Skills.LastSyncAt)
}

// TestRecordSyncResult_InvokesPersistCallback pins the injected persistence
// hook: main.go wires it so the outcome survives a restart.
func TestRecordSyncResult_InvokesPersistCallback(t *testing.T) {
	setupWorkerEnv(t)

	var captured SyncResult
	persistSyncStateFn = func(res SyncResult) error {
		captured = res
		return nil
	}

	recordSyncResult(SyncResult{SkillCount: 7})

	assert.Equal(t, 7, captured.SkillCount)
}

// TestSyncNow_JoinsInFlightPass pins the concurrency contract: a second caller
// while a pass is running waits for the first rather than starting a second
// concurrent clone (which would race on the checkout directory).
func TestSyncNow_JoinsInFlightPass(t *testing.T) {
	setupWorkerEnv(t)

	// Simulate an in-flight pass by installing a done channel that we close
	// from the test after seeding the result.
	syncMu.Lock()
	done := make(chan struct{})
	syncDone = done
	syncResult = SyncResult{SkillCount: 42}
	syncMu.Unlock()

	// Unblock the "pass" shortly; the caller must observe the seeded result.
	go func() {
		close(done)
	}()

	res := TriggerGitSync()
	assert.Equal(t, 42, res.SkillCount, "the joiner must return the in-flight pass's result")
}

// TestStartStopGitSyncWorker pins the lifecycle: start is idempotent, running
// reflects the state, and stop is safe (including a second stop).
func TestStartStopGitSyncWorker(t *testing.T) {
	setupWorkerEnv(t)

	// A non-positive interval disables the periodic part, so the goroutine does
	// its startup pass then parks on the cancel channel.
	model.ConfigInstance.Skills.RefreshHours = 0

	StartGitSyncWorker()
	assert.True(t, GitSyncRunning())

	// Second start is a no-op, not a second goroutine.
	StartGitSyncWorker()
	assert.True(t, GitSyncRunning())

	StopGitSyncWorker()
	assert.False(t, GitSyncRunning())

	// Stop when not running must not panic or close a nil channel.
	StopGitSyncWorker()
	assert.False(t, GitSyncRunning())
}

// TestResetSyncForTest_ClearsState pins the test helper itself: it must stop a
// running worker and clear the cached result so tests do not leak into each
// other.
func TestResetSyncForTest_ClearsState(t *testing.T) {
	setupWorkerEnv(t)
	model.ConfigInstance.Skills.RefreshHours = 0

	StartGitSyncWorker()
	require.True(t, GitSyncRunning())

	ResetSyncForTest()
	assert.False(t, GitSyncRunning())

	syncMu.Lock()
	defer syncMu.Unlock()
	assert.Nil(t, syncDone)
	assert.Equal(t, SyncResult{}, syncResult)
}
