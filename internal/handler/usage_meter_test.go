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
