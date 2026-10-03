package skill

import (
	"log/slog"
	"sync"
	"time"

	"clawbench/internal/model"
)

// SyncResult reports the outcome of one sync pass.
type SyncResult struct {
	// Errors maps a repo slug to its failure reason. Repos that succeeded are
	// absent.
	Errors map[string]string
	// SkillCount is the number of skills discovered after the sync.
	SkillCount int
}

// persistSyncStateFn records a sync outcome in config. It is injected by
// main.go (handler.PersistSkillSyncState) because this package must not import
// the handler package. nil means "do not persist" (tests, or a build without
// the handler wired).
var persistSyncStateFn func(SyncResult) error

// SetPersistSyncStateFn wires the config persistence callback for the sync
// worker. Called from main.go during startup.
func SetPersistSyncStateFn(fn func(SyncResult) error) {
	persistSyncStateFn = fn
}

var (
	syncMu      sync.Mutex
	syncCancel  chan struct{}
	syncDone    chan struct{}
	syncRunning bool
	syncResult  SyncResult
)

// StartGitSyncWorker starts the background skill sync loop. It performs an
// immediate scan, then a startup pull, then refreshes every
// Skills.RefreshHours. A non-positive interval disables the periodic part (the
// startup pull still runs). Idempotent.
func StartGitSyncWorker() {
	syncMu.Lock()
	if syncRunning {
		syncMu.Unlock()
		return
	}
	syncRunning = true
	cancel := make(chan struct{})
	syncCancel = cancel
	syncMu.Unlock()

	go func() {
		// Initial scan makes the current directories visible immediately,
		// before any network work.
		Global().ScanAll()
		syncNow()

		for {
			hours := model.ConfigInstance.Skills.RefreshHours
			if hours <= 0 {
				<-cancel
				return
			}
			select {
			case <-cancel:
				return
			case <-time.After(time.Duration(hours) * time.Hour):
				syncNow()
			}
		}
	}()
}

// StopGitSyncWorker stops the background loop. Safe to call when not running.
func StopGitSyncWorker() {
	syncMu.Lock()
	defer syncMu.Unlock()
	if !syncRunning {
		return
	}
	close(syncCancel)
	syncCancel = nil
	syncRunning = false
}

// GitSyncRunning reports whether the worker is active. For tests.
func GitSyncRunning() bool {
	syncMu.Lock()
	defer syncMu.Unlock()
	return syncRunning
}

// TriggerGitSync runs a sync immediately and blocks until it finishes. When a
// sync is already in flight it waits for that one rather than starting a second
// concurrent pass (concurrent clones of the same repo would race on the
// checkout directory).
func TriggerGitSync() SyncResult {
	syncMu.Lock()
	if syncDone != nil {
		done := syncDone
		syncMu.Unlock()
		<-done
		syncMu.Lock()
		res := syncResult
		syncMu.Unlock()
		return res
	}
	syncMu.Unlock()
	return syncNow()
}

// RescanFilesystem invalidates the injection cache and rescans every source
// WITHOUT any network IO — no git clone/pull. It is the "rescan now" action:
// a skill the user just dropped into a local directory appears immediately,
// without waiting on (or requiring reachability of) any git remote.
//
// It does not touch sync state (LastSyncAt/LastError) or per-repo errors:
// nothing was fetched, so the previous sync outcome is still the truth.
// Returns the number of skills discovered after the rescan.
func RescanFilesystem() int {
	Global().Invalidate()
	Global().ScanAll()
	return len(Global().All())
}

// syncNow performs one sync pass and returns its result.
func syncNow() SyncResult {
	syncMu.Lock()
	if syncDone != nil {
		// Another pass is running; join it.
		done := syncDone
		syncMu.Unlock()
		<-done
		syncMu.Lock()
		res := syncResult
		syncMu.Unlock()
		return res
	}
	done := make(chan struct{})
	syncDone = done
	syncMu.Unlock()

	result := doSync()

	syncMu.Lock()
	syncResult = result
	syncDone = nil
	close(done)
	syncMu.Unlock()

	return result
}

// doSync clones or pulls every configured repo, then rescans all sources.
func doSync() SyncResult {
	res := SyncResult{Errors: make(map[string]string)}

	repos := model.ConfigInstance.Skills.Repos
	for i := range repos {
		repo := repos[i]
		if repo.URL == "" {
			continue
		}
		if _, err := EnsureRepo(repo); err != nil {
			slog.Warn("skill sync: repo failed", "url", repo.URL, "slug", repo.Slug, "error", err)
			res.Errors[repo.Slug] = err.Error()
		}
	}

	Global().ScanAll()
	res.SkillCount = len(Global().All())
	recordSyncResult(res)
	return res
}

// recordSyncResult persists the sync outcome so the settings page can show
// "last synced" / "last error" across restarts. The in-memory config is the
// source of truth; persistence is best-effort (a read-only config.yaml must not
// break syncing).
func recordSyncResult(res SyncResult) {
	model.ConfigInstance.Skills.LastSyncAt = time.Now().Unix()
	model.ConfigInstance.Skills.LastError = firstError(res.Errors)
	// Per-repo errors are cleared each pass so a stale message cannot outlive
	// its fix.
	for i := range model.ConfigInstance.Skills.Repos {
		model.ConfigInstance.Skills.Repos[i].LastError = ""
		if msg, ok := res.Errors[model.ConfigInstance.Skills.Repos[i].Slug]; ok {
			model.ConfigInstance.Skills.Repos[i].LastError = msg
		}
	}
	if persistSyncStateFn != nil {
		if err := persistSyncStateFn(res); err != nil {
			slog.Warn("skill sync: persisting sync state failed", "error", err)
		}
	}
}

// firstError returns any one error message, or "" when the map is empty. Map
// iteration is unordered, so the result is only used for a coarse "did anything
// fail" display.
func firstError(errs map[string]string) string {
	for _, msg := range errs {
		return msg
	}
	return ""
}

// ResetSyncForTest clears worker state. For testing only.
func ResetSyncForTest() {
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncCancel != nil {
		close(syncCancel)
	}
	syncCancel = nil
	syncDone = nil
	syncRunning = false
	syncResult = SyncResult{}
}
