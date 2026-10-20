package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setMeterRuleForTest installs a rule and restores it afterwards. The rule is
// process-wide state, so a leak would affect every later test in this package.
func setMeterRuleForTest(t *testing.T, rule model.UsageMeterConfig) {
	t.Helper()
	prev := model.ConfigInstance.UsageMeter
	model.ConfigInstance.UsageMeter = rule
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })
}

func TestServeUsageMeter_DisabledRuleReturns200(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	setMeterRuleForTest(t, model.UsageMeterConfig{Quota: 0})

	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter", nil))
	require.Equal(t, http.StatusOK, w.Code, "a disabled rule is a state, not an error")

	var body struct {
		Enabled bool `json:"enabled"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.False(t, body.Enabled)
}

func TestServeUsageMeter_EnabledRuleReportsCycleAndTotals(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// The anchor must be relative to now: a fixed date would roll the cycle past
	// the seeded rows and the totals would be legitimately zero.
	now := time.Now()
	setMeterRuleForTest(t, model.UsageMeterConfig{
		AnchorDate: now.AddDate(0, 0, -5).Format("2006-01-02"),
		PeriodDays: 30,
		Metric:     model.UsageMeterMetricCost,
		Quota:      30,
	})
	stamp := now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05")

	// One cost-bearing row inside the cycle.
	db := store.UnsafeDBForTest()
	projectID := store.ProjectIDForTest(t, env.ProjectDir)
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id) VALUES ('s-meter', ?, 'codebuddy', 't', 'codebuddy')",
		projectID,
	)
	require.NoError(t, err)
	res, err := db.Exec(
		"INSERT INTO chat_history (project_id, backend, session_id, role, content, streaming, created_at) VALUES (?, 'codebuddy', 's-meter', 'assistant', '{}', 0, ?)",
		projectID, stamp,
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, cost_usd, created_at, project_id, backend, agent_id, clawbench_session_id)
		 VALUES (?, 'm', 4.5, ?, ?, 'codebuddy', 'codebuddy', 's-meter')`,
		msgID, stamp, projectID,
	)
	require.NoError(t, err)

	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Enabled    bool             `json:"enabled"`
		Metric     string           `json:"metric"`
		PeriodDays int              `json:"periodDays"`
		Quota      float64          `json:"quota"`
		Actual     float64          `json:"actual"`
		Expected   float64          `json:"expected"`
		DayInCycle int              `json:"dayInCycle"`
		Series     []map[string]any `json:"series"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.True(t, body.Enabled)
	assert.Equal(t, model.UsageMeterMetricCost, body.Metric)
	assert.Equal(t, 30, body.PeriodDays)
	assert.InDelta(t, 30.0, body.Quota, 1e-9)
	assert.InDelta(t, 4.5, body.Actual, 1e-9)
	assert.Greater(t, body.DayInCycle, 0)
	assert.NotEmpty(t, body.Series)
}

// The meter is instance-wide, so a project cookie must not narrow it: the
// endpoint has no scope parameter at all.
func TestServeUsageMeter_IgnoresProjectCookie(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	now := time.Now()
	setMeterRuleForTest(t, model.UsageMeterConfig{
		AnchorDate: now.AddDate(0, 0, -5).Format("2006-01-02"), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})
	stamp := now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05")

	// Seed usage under a DIFFERENT project than the cookie names.
	otherDir := filepath.Join(env.WatchDir, "other-project")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	otherID := store.ProjectIDForTest(t, otherDir)
	db := store.UnsafeDBForTest()
	_, err := db.Exec(
		"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id) VALUES ('s-other', ?, 'codebuddy', 't', 'codebuddy')",
		otherID,
	)
	require.NoError(t, err)
	res, err := db.Exec(
		"INSERT INTO chat_history (project_id, backend, session_id, role, content, streaming, created_at) VALUES (?, 'codebuddy', 's-other', 'assistant', '{}', 0, ?)",
		otherID, stamp,
	)
	require.NoError(t, err)
	msgID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO chat_metadata (message_id, model, cost_usd, created_at, project_id, backend, agent_id, clawbench_session_id)
		 VALUES (?, 'm', 7.25, ?, ?, 'codebuddy', 'codebuddy', 's-other')`,
		msgID, stamp, otherID,
	)
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/usage/meter", nil), env.ProjectDir)
	w := callHandler(ServeUsageMeter, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Actual float64 `json:"actual"`
	}
	decodeRespJSON(t, w.Body, &body)
	assert.InDelta(t, 7.25, body.Actual, 1e-9,
		"usage from a project other than the cookie's must still count")
}

func TestServeUsageMeter_MethodNotAllowed(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodPost, "/api/usage/meter", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── Rule validation (PATCH path) ─────────────────────────────────────────────

func TestValidateUsageMeterRule(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rule    model.UsageMeterConfig
		wantErr bool
	}{
		{"disabled with nothing else set", model.UsageMeterConfig{Quota: 0}, false},
		{"negative quota", model.UsageMeterConfig{Quota: -1}, true},
		{"quota without anchor", model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: model.UsageMeterMetricCost}, true},
		{"malformed anchor", model.UsageMeterConfig{Quota: 10, AnchorDate: "10/05/2026", PeriodDays: 30, Metric: model.UsageMeterMetricCost}, true},
		{"zero period", model.UsageMeterConfig{Quota: 10, AnchorDate: "2026-10-05", PeriodDays: 0, Metric: model.UsageMeterMetricCost}, true},
		{"period over the cap", model.UsageMeterConfig{Quota: 10, AnchorDate: "2026-10-05", PeriodDays: model.MaxUsageMeterPeriodDays + 1, Metric: model.UsageMeterMetricCost}, true},
		{"unknown metric", model.UsageMeterConfig{Quota: 10, AnchorDate: "2026-10-05", PeriodDays: 30, Metric: "usd"}, true},
		{"valid cost rule", model.UsageMeterConfig{Quota: 20, AnchorDate: "2026-10-05", PeriodDays: 30, Metric: model.UsageMeterMetricCost}, false},
		{"valid token rule", model.UsageMeterConfig{Quota: 5_000_000, AnchorDate: "2026-10-05", PeriodDays: 7, Metric: model.UsageMeterMetricTokens}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUsageMeterRule(tc.rule)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// The internal bookkeeping fields must NOT be patchable: a client that could
// write them could suppress or duplicate the notification.
func TestPatchableConfigPaths_UsageMeterStateIsNotWritable(t *testing.T) {
	for _, forbidden := range []string{
		"usage_meter.notified_cycle_start",
		"usage_meter.notified_stage",
	} {
		assert.False(t, PatchableConfigPaths[forbidden],
			"%s is service-owned bookkeeping and must not be PATCHable", forbidden)
	}
	for _, allowed := range []string{
		"usage_meter.anchor_date",
		"usage_meter.period_days",
		"usage_meter.metric",
		"usage_meter.quota",
	} {
		assert.True(t, PatchableConfigPaths[allowed], "%s must be PATCHable", allowed)
	}
}

// A rule change must clear the "already notified" state so the new budget gets
// its own warning (the documented behavior when a user raises a quota).
func TestApplyConfigPatch_UsageMeterChangeClearsNotificationState(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric:             model.UsageMeterMetricCost,
		Quota:              20,
		NotifiedCycleStart: "2026-10-05T00:00:00+08:00",
		NotifiedStage:      service.UsageMeterStageOver,
	}

	applyConfigPatch(map[string]any{
		"usage_meter": map[string]any{"quota": float64(30)},
	})

	assert.InDelta(t, 30.0, model.ConfigInstance.UsageMeter.Quota, 1e-9)
	assert.Empty(t, model.ConfigInstance.UsageMeter.NotifiedStage,
		"changing the rule must re-arm notification")
	assert.Empty(t, model.ConfigInstance.UsageMeter.NotifiedCycleStart)
}

// A partial patch must be validated against the MERGED rule, not against empty
// strings: sending only `quota` to an already-configured rule is legal.
func TestValidatePatchValues_UsageMeterPartialPatch(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 20,
	}

	assert.NoError(t, validatePatchValues(map[string]any{
		"usage_meter": map[string]any{"quota": float64(25)},
	}), "a quota-only patch inherits the stored anchor and metric")

	assert.Error(t, validatePatchValues(map[string]any{
		"usage_meter": map[string]any{"metric": "bogus"},
	}), "an unknown metric is rejected")
}

// A patch that leaves the rule identical must NOT re-arm notification. The
// settings panel submits the whole panel on every save, so re-arming on any
// usage_meter patch would re-notify the user each time they pressed save
// without changing anything.
func TestApplyConfigPatch_UsageMeterUnchangedRuleKeepsNotificationState(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric:             model.UsageMeterMetricCost,
		Quota:              20,
		NotifiedCycleStart: "2026-10-05T00:00:00+08:00",
		NotifiedStage:      service.UsageMeterStageOver,
	}

	// Same values as the stored rule.
	applyConfigPatch(map[string]any{
		"usage_meter": map[string]any{
			"anchor_date": "2026-10-05",
			"period_days": float64(30),
			"metric":      "cost",
			"quota":       float64(20),
		},
	})

	assert.Equal(t, service.UsageMeterStageOver, model.ConfigInstance.UsageMeter.NotifiedStage,
		"an unchanged rule must not re-arm notification")
	assert.Equal(t, "2026-10-05T00:00:00+08:00", model.ConfigInstance.UsageMeter.NotifiedCycleStart)
}

// Changing only one field is still a change: the state must be cleared.
func TestApplyConfigPatch_UsageMeterSingleFieldChangeRearms(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric:             model.UsageMeterMetricCost,
		Quota:              20,
		NotifiedCycleStart: "2026-10-05T00:00:00+08:00",
		NotifiedStage:      service.UsageMeterStageFull,
	}

	applyConfigPatch(map[string]any{
		"usage_meter": map[string]any{"period_days": float64(7)},
	})

	assert.Empty(t, model.ConfigInstance.UsageMeter.NotifiedStage,
		"changing the cycle length must re-arm notification")
}

// A rule change clears the notification state in memory; that clearing must
// reach DISK in the same atomic write. Otherwise a restart before the new rule
// notifies reads the old stage back and suppresses the notification for the
// rest of the cycle (the cycle start is unchanged when only the quota changes,
// so the stale stage looks valid).
func TestMergeUsageMeterStateIntoPatch_CarriesClearedState(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	// State already cleared, as applyConfigPatch leaves it after a rule change.
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
		NotifiedCycleStart: "", NotifiedStage: "",
	}

	out := mergeUsageMeterStateIntoPatch(map[string]any{
		"usage_meter": map[string]any{"quota": float64(30)},
	})

	um, ok := out["usage_meter"].(map[string]any)
	require.True(t, ok)
	stage, present := um["notified_stage"]
	require.True(t, present, "the state field must be written even when empty")
	assert.Equal(t, "", stage, "an empty stage must overwrite the stale on-disk value")
	cycleStart, present := um["notified_cycle_start"]
	require.True(t, present)
	assert.Equal(t, "", cycleStart)

	// The rule fields survive.
	assert.InDelta(t, 30.0, um["quota"], 1e-9)
}

// The original patch must not be mutated: the caller still uses it for the
// response's changed-field list.
func TestMergeUsageMeterStateIntoPatch_DoesNotMutateInput(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{NotifiedStage: "over"}

	patch := map[string]any{
		"usage_meter": map[string]any{"quota": float64(10)},
	}
	_ = mergeUsageMeterStateIntoPatch(patch)

	um := patch["usage_meter"].(map[string]any)
	_, leaked := um["notified_stage"]
	assert.False(t, leaked, "the input patch must be left untouched")
}

// Wrong-typed numeric fields must be rejected rather than skipped: skipping
// leaves the bad value in the patch, which is written verbatim to config.yaml,
// and the next startup fails to unmarshal and exits.
func TestValidatePatchValues_UsageMeterRejectsWrongTypes(t *testing.T) {
	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })
	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 20,
	}

	for _, tc := range []struct {
		name  string
		patch map[string]any
	}{
		{"quota as string", map[string]any{"quota": "5000000"}},
		{"period_days as string", map[string]any{"period_days": "30"}},
		{"metric as number", map[string]any{"metric": float64(1)}},
		{"anchor_date as number", map[string]any{"anchor_date": float64(20261005)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePatchValues(map[string]any{"usage_meter": tc.patch})
			assert.Error(t, err, "a present-but-wrong-typed field must be rejected")
		})
	}
}

// ── Endpoint auth + persistence ──────────────────────────────────────────────

// The meter reports instance-wide spend, so an unauthenticated caller must not
// read it — the same gate the stats panel's scope=all uses.
//
// setupTestEnv deliberately runs with no password configured (open access), so
// the token has to be installed here for the check to have anything to compare.
func TestServeUsageMeter_RequiresAuth(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	setMeterRuleForTest(t, model.UsageMeterConfig{Quota: 20})

	origToken, origCookie := model.SessionToken, model.CookieToken
	model.SessionToken = "secret-token"
	model.CookieToken = "secret-token"
	t.Cleanup(func() {
		model.SessionToken = origToken
		model.CookieToken = origCookie
	})

	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter", nil))
	assert.Equal(t, http.StatusForbidden, w.Code,
		"an unauthenticated request must not read instance-wide spend")

	authed := withAuthCookie(newRequest(t, http.MethodGet, "/api/usage/meter", nil), "secret-token")
	w = callHandler(ServeUsageMeter, authed)
	assert.Equal(t, http.StatusOK, w.Code, "an authenticated request must still work")
}

// PersistUsageMeterState writes ONLY the two bookkeeping fields, from what is
// currently in memory. A PATCH may have changed the quota while the aggregate
// query was running; writing a captured snapshot back would silently undo the
// user's edit.
func TestPersistUsageMeterState_WritesOnlyBookkeeping(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 42,
	}

	require.NoError(t, PersistUsageMeterState("2026-10-05T00:00:00+08:00", service.UsageMeterStageOver))

	assert.Equal(t, "2026-10-05T00:00:00+08:00", model.ConfigInstance.UsageMeter.NotifiedCycleStart)
	assert.Equal(t, service.UsageMeterStageOver, model.ConfigInstance.UsageMeter.NotifiedStage)
	assert.InDelta(t, 42.0, model.ConfigInstance.UsageMeter.Quota, 1e-9,
		"the rule itself must not be touched by a state write")
}

// A failed aggregate must surface as an error response, not as a zeroed
// "everything is fine" body — a silent 200 would show an empty capsule and hide
// the fact that the numbers are unknown.
func TestServeUsageMeter_ComputeErrorIsReported(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	now := time.Now()
	setMeterRuleForTest(t, model.UsageMeterConfig{
		AnchorDate: now.AddDate(0, 0, -5).Format("2006-01-02"), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})

	// Drop the ledger table the aggregate reads: the query fails, and the
	// handler must translate that into a non-2xx rather than a zero total.
	_, err := store.UnsafeDBForTest().Exec(`DROP TABLE chat_metadata`)
	require.NoError(t, err)

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/usage/meter", nil), t.TempDir())
	w := callHandler(ServeUsageMeter, req)
	assert.NotEqual(t, http.StatusOK, w.Code,
		"a failed aggregate must not be reported as a healthy zero total")
}

// A persistence failure must be reported AND must roll the in-memory rule back:
// leaving the cleared state in memory while the file still holds the old stage
// makes the next process start suppress a notification the user never saw.
func TestPersistUsageMeterState_RollsBackOnWriteFailure(t *testing.T) {
	_, teardown := setupPersistTestEnv(t)
	defer teardown()

	model.ConfigInstance.UsageMeter = model.UsageMeterConfig{
		AnchorDate: "2026-10-05", PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 42,
		NotifiedCycleStart: "2026-09-05T00:00:00+08:00",
		NotifiedStage:      service.UsageMeterStageOver,
	}

	// Make the write fail deterministically (permissions are not enforced when
	// the tests run as root): point DataDir at a regular FILE, so the MkdirAll
	// of <data-dir>/config cannot succeed.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	origDataDir := model.DataDir
	model.DataDir = blocker
	t.Cleanup(func() { model.DataDir = origDataDir })

	err := PersistUsageMeterState("2026-10-05T00:00:00+08:00", service.UsageMeterStageFull)
	require.Error(t, err)
	assert.Equal(t, service.UsageMeterStageOver, model.ConfigInstance.UsageMeter.NotifiedStage,
		"the in-memory state must be rolled back when the write fails")
}
