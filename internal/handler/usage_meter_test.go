package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// A disabled rule must answer with EXACTLY the six keys the client can use, and
// none of the quota-derived ones. The OpenAPI description used to promise
// "unconfigured still reports spend over a 30-day window" — the opposite of what
// the handler returns — so the field set is pinned here rather than left to a
// prose document nothing validates.
func TestServeUsageMeter_DisabledRuleReturns200(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	setMeterRuleForTest(t, model.UsageMeterConfig{Quota: 0})

	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter", nil))
	require.Equal(t, http.StatusOK, w.Code, "a disabled rule is a state, not an error")

	// The body is read once and decoded twice (key set + values): a second
	// decodeRespJSON on the same reader would hit EOF.
	rawBody, err := io.ReadAll(w.Body)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rawBody, &raw))

	// Exact set, both directions: a missing key would break the client's
	// zero-initialised readout, and an extra one (metric / periodDays /
	// cycleStart / series) would tell it there is a window to render.
	got := make([]string, 0, len(raw))
	for k := range raw {
		got = append(got, k)
	}
	sort.Strings(got)
	assert.Equal(t, []string{
		"actual", "dayInCycle", "enabled", "expected", "projectedExhaustDay", "remainingDays",
	}, got, "a disabled rule must report no metric, no window and no series")

	var body struct {
		Enabled             bool    `json:"enabled"`
		DayInCycle          int     `json:"dayInCycle"`
		Actual              float64 `json:"actual"`
		Expected            float64 `json:"expected"`
		RemainingDays       int     `json:"remainingDays"`
		ProjectedExhaustDay int     `json:"projectedExhaustDay"`
	}
	require.NoError(t, json.Unmarshal(rawBody, &body))
	assert.False(t, body.Enabled)
	assert.Zero(t, body.DayInCycle)
	assert.Zero(t, body.Actual)
	assert.Zero(t, body.Expected)
	assert.Zero(t, body.RemainingDays)
	assert.Zero(t, body.ProjectedExhaustDay)
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

// The settings page lets the user pin the cycle start to a time of day. The
// PATCH validator must accept exactly the shapes the service can compute with,
// and reject everything else — a value it accepts but the meter cannot parse
// would silently disable the meter after a successful-looking save.
func TestValidateUsageMeterRule_AnchorAcceptsOptionalTime(t *testing.T) {
	accepted := []string{
		"2026-10-05",
		"2026-10-05T14:30:00",
		"2026-10-05T14:30",
		"2026-10-05T00:00:00",
		"2026-10-05T23:59:59",
	}
	for _, anchor := range accepted {
		t.Run("accepts "+anchor, func(t *testing.T) {
			err := validateUsageMeterRule(model.UsageMeterConfig{
				Quota: 10, AnchorDate: anchor, PeriodDays: 30, Metric: model.UsageMeterMetricCost,
			})
			require.NoError(t, err, "%q must be accepted", anchor)
		})
	}

	// A zone suffix is rejected on purpose: the anchor is a wall-clock reading in
	// the server's timezone. Accepting an offset would make the cycle boundary
	// depend on the host's TZ, and two visually identical rules would roll over
	// at different instants.
	rejected := []string{
		"2026-13-45",
		"10/05/2026",
		"2026-10-05 14:30:00",
		"2026-10-05T14:30:00Z",
		"2026-10-05T14:30:00+08:00",
		"2026-10-05T25:00:00",
		"2026-10-05T14:70:00",
		"2026-10-05T",
	}
	for _, anchor := range rejected {
		t.Run("rejects "+anchor, func(t *testing.T) {
			err := validateUsageMeterRule(model.UsageMeterConfig{
				Quota: 10, AnchorDate: anchor, PeriodDays: 30, Metric: model.UsageMeterMetricCost,
			})
			require.Error(t, err, "%q must be rejected", anchor)

			// Still a field-scoped, localizable rejection — not a bare error the
			// panel cannot explain.
			var valueErr *configValueError
			require.True(t, errors.As(err, &valueErr))
			assert.Equal(t, "UsageMeterAnchorInvalid", valueErr.MsgKey)
			assert.Equal(t, "usage_meter.anchor_date", valueErr.Field)
		})
	}
}

// Every rejection must carry a message key AND the field path, and the keys must
// be distinct per rule. Asserting only "err != nil" (as the table above does)
// lets a wrong key through: swapping two MsgKeys, or pointing them all at one
// shared "invalid" key, would still pass while the panel again tells the user
// nothing about what to fix.
func TestValidateUsageMeterRule_EachFailureNamesItsField(t *testing.T) {
	cases := []struct {
		name      string
		rule      model.UsageMeterConfig
		wantKey   string
		wantField string
	}{
		{
			name:      "negative quota",
			rule:      model.UsageMeterConfig{Quota: -1},
			wantKey:   "UsageMeterQuotaNegative",
			wantField: "usage_meter.quota",
		},
		{
			name:      "quota without anchor",
			rule:      model.UsageMeterConfig{Quota: 10, PeriodDays: 30, Metric: model.UsageMeterMetricCost},
			wantKey:   "UsageMeterAnchorRequired",
			wantField: "usage_meter.anchor_date",
		},
		{
			name:      "malformed anchor",
			rule:      model.UsageMeterConfig{Quota: 10, AnchorDate: "10/05/2026", PeriodDays: 30, Metric: model.UsageMeterMetricCost},
			wantKey:   "UsageMeterAnchorInvalid",
			wantField: "usage_meter.anchor_date",
		},
		{
			name:      "zero period",
			rule:      model.UsageMeterConfig{Quota: 10, AnchorDate: "2026-10-05", PeriodDays: 0, Metric: model.UsageMeterMetricCost},
			wantKey:   "UsageMeterPeriodRange",
			wantField: "usage_meter.period_days",
		},
		{
			name:      "period over the cap",
			rule:      model.UsageMeterConfig{Quota: 10, AnchorDate: "2026-10-05", PeriodDays: model.MaxUsageMeterPeriodDays + 1, Metric: model.UsageMeterMetricCost},
			wantKey:   "UsageMeterPeriodRange",
			wantField: "usage_meter.period_days",
		},
		{
			name:      "unknown metric",
			rule:      model.UsageMeterConfig{Quota: 10, AnchorDate: "2026-10-05", PeriodDays: 30, Metric: "usd"},
			wantKey:   "UsageMeterMetricInvalid",
			wantField: "usage_meter.metric",
		},
	}

	keyToField := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUsageMeterRule(tc.rule)
			require.Error(t, err)

			var valueErr *configValueError
			require.True(t, errors.As(err, &valueErr),
				"a rule rejection must be a configValueError so serveConfigPatch can localize it; got %T", err)
			assert.Equal(t, tc.wantKey, valueErr.MsgKey)
			assert.Equal(t, tc.wantField, valueErr.Field)
			assert.Equal(t, tc.wantField, valueErr.templateData()["field"],
				"the field path must reach the response's detail")

			// A message key must never be reused across DIFFERENT fields: the
			// text names one value ("the cycle length…"), so a shared key would
			// point the user at the wrong row. Two rules on the SAME field
			// legitimately share a key (an out-of-range period is one message
			// whether it is 0 or 999).
			if prevField, ok := keyToField[tc.wantKey]; ok && prevField != tc.wantField {
				t.Errorf("key %q is used for both %q and %q", tc.wantKey, prevField, tc.wantField)
			}
			keyToField[tc.wantKey] = tc.wantField
		})
	}
}

// templateData must copy, not alias, the caller's map: the same error value can
// be rendered twice (retry, logging), and writing `field` into a shared map
// would leak between them.
func TestConfigValueError_TemplateDataDoesNotMutateSource(t *testing.T) {
	src := map[string]any{"Max": 366}
	err := &configValueError{MsgKey: "UsageMeterPeriodRange", Field: "usage_meter.period_days", Data: src}

	data := err.templateData()
	assert.Equal(t, 366, data["Max"])
	assert.Equal(t, "usage_meter.period_days", data["field"])

	_, leaked := src["field"]
	assert.False(t, leaked, "the source map must be left untouched")

	// A nil Data map must still yield the field path rather than panicking.
	bare := &configValueError{MsgKey: "UsageMeterQuotaNegative", Field: "usage_meter.quota"}
	assert.Equal(t, map[string]any{"field": "usage_meter.quota"}, bare.templateData())
}

// The localization path is new for this endpoint; the rest of the config
// validators still return plain errors and must keep answering the old
// `invalid_value` shape. Without this the generic branch could be deleted by
// accident and every unrelated validation failure would start rendering a
// message key that has no locale entry (the raw key as the error text).
func TestServeConfig_Patch_GenericValidationKeepsInvalidValueShape(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	model.ConfigInstance = model.Config{}

	req := httptest.NewRequest(http.MethodPatch, "/api/config",
		strings.NewReader(`{"tts":{"speed":0.1}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Locale", "zh")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "invalid_value", resp["error"])
	assert.Empty(t, resp["msgKey"], "an unlocalized validator must not claim a message key")
	assert.Contains(t, resp["message"], "tts.speed must be between",
		"the raw validation message is still the only detail available here")
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
