package stats

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
//
// A configured rule is switched ON unless the test says otherwise, mirroring
// ApplyDefaults' treatment of a config.yaml written before the enable flag
// existed. Tests of the off state use setMeterRuleRawForTest.
func setMeterRuleForTest(t *testing.T, rule model.UsageMeterConfig) {
	t.Helper()
	if model.UsageMeterConfigured(rule) {
		rule.Enabled = true
	}
	setMeterRuleRawForTest(t, rule)
}

// setMeterRuleRawForTest installs a rule verbatim, including Enabled.
func setMeterRuleRawForTest(t *testing.T, rule model.UsageMeterConfig) {
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
// A disabled rule must answer with EXACTLY the six keys the client can use, and
// none of the quota-derived ones. The OpenAPI description used to promise
// "unconfigured still reports spend over a 30-day window" — the opposite of what
// the handler returns — so the field set is pinned here rather than left to a
// prose document nothing validates.
func TestServeUsageMeter_DisabledRuleReturns200(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	setMeterRuleForTest(t, model.UsageMeterConfig{Quota: 0})

	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter"))
	require.Equal(t, http.StatusOK, w.Code, "a disabled rule is a state, not an error")

	// The body is read once and decoded twice (key set + values): a second
	// decodeRespJSON on the same reader would hit EOF.
	rawBody, err := io.ReadAll(w.Body)
	require.NoError(t, err)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rawBody, &raw))

	// Exact set, both directions: a missing key would break the client's
	// zero-initialized readout, and an extra one (metric / periodDays /
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

	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter"))
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

// The agent scope is part of the RULE, not a query parameter: setting it on the
// rule must narrow the measured amount while the quota stays the global one.

// The agent scope is part of the RULE, not a query parameter: setting it on the
// rule must narrow the measured amount while the quota stays the global one.
func TestServeUsageMeter_AgentScopeFromRule(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	now := time.Now()
	anchor := now.AddDate(0, 0, -5).Format("2006-01-02")
	stamp := now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05")

	db := store.UnsafeDBForTest()
	projectID := store.ProjectIDForTest(t, env.ProjectDir)
	for _, seed := range []struct {
		session string
		agent   string
		cost    float64
	}{
		{"s-scope-a", "agent-a", 1.5},
		{"s-scope-b", "agent-b", 2.5},
	} {
		_, err := db.Exec(
			"INSERT INTO chat_sessions (id, project_id, backend, title, agent_id) VALUES (?, ?, 'claude', 't', ?)",
			seed.session, projectID, seed.agent,
		)
		require.NoError(t, err)
		res, err := db.Exec(
			"INSERT INTO chat_history (project_id, backend, session_id, role, content, streaming, created_at) VALUES (?, 'claude', ?, 'assistant', '{}', 0, ?)",
			projectID, seed.session, stamp,
		)
		require.NoError(t, err)
		msgID, _ := res.LastInsertId()
		_, err = db.Exec(
			`INSERT INTO chat_metadata (message_id, model, cost_usd, created_at, project_id, backend, agent_id, clawbench_session_id)
			 VALUES (?, 'm', ?, ?, ?, 'claude', ?, ?)`,
			msgID, seed.cost, stamp, projectID, seed.agent, seed.session,
		)
		require.NoError(t, err)
	}

	readActual := func(t *testing.T) (actual, quota float64) {
		t.Helper()
		w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter"))
		require.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Actual float64 `json:"actual"`
			Quota  float64 `json:"quota"`
		}
		decodeRespJSON(t, w.Body, &body)
		return body.Actual, body.Quota
	}

	setMeterRuleForTest(t, model.UsageMeterConfig{
		AnchorDate: anchor, PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
	})
	actual, _ := readActual(t)
	assert.InDelta(t, 4.0, actual, 1e-9, "no scope = every agent")

	setMeterRuleForTest(t, model.UsageMeterConfig{
		AnchorDate: anchor, PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
		AgentIDs: []string{"agent-a"},
	})
	actual, quota := readActual(t)
	assert.InDelta(t, 1.5, actual, 1e-9, "only the scoped agent counts")
	assert.InDelta(t, 30.0, quota, 1e-9, "the quota stays the shared global one")

	setMeterRuleForTest(t, model.UsageMeterConfig{
		AnchorDate: anchor, PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 30,
		AgentIDs: []string{"agent-a", "agent-b"},
	})
	actual, _ = readActual(t)
	assert.InDelta(t, 4.0, actual, 1e-9)
}

// The meter is instance-wide, so a project cookie must not narrow it: the
// endpoint has no scope parameter at all.

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

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/usage/meter"), env.ProjectDir)
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
	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodPost, "/api/usage/meter"))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ── Rule validation (PATCH path) ─────────────────────────────────────────────

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

	w := callHandler(ServeUsageMeter, newRequest(t, http.MethodGet, "/api/usage/meter"))
	assert.Equal(t, http.StatusForbidden, w.Code,
		"an unauthenticated request must not read instance-wide spend")

	authed := withAuthCookie(newRequest(t, http.MethodGet, "/api/usage/meter"), "secret-token")
	w = callHandler(ServeUsageMeter, authed)
	assert.Equal(t, http.StatusOK, w.Code, "an authenticated request must still work")
}

// PersistUsageMeterState writes ONLY the two bookkeeping fields, from what is
// currently in memory. A PATCH may have changed the quota while the aggregate
// query was running; writing a captured snapshot back would silently undo the
// user's edit.

// PersistUsageMeterState writes ONLY the two bookkeeping fields, from what is
// currently in memory. A PATCH may have changed the quota while the aggregate
// query was running; writing a captured snapshot back would silently undo the
// user's edit.
func TestPersistUsageMeterState_WritesOnlyBookkeeping(t *testing.T) {
	teardown := setupPersistTestEnv(t)
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

	req := withProjectCookie(newRequest(t, http.MethodGet, "/api/usage/meter"), t.TempDir())
	w := callHandler(ServeUsageMeter, req)
	assert.NotEqual(t, http.StatusOK, w.Code,
		"a failed aggregate must not be reported as a healthy zero total")
}

// A persistence failure must be reported AND must roll the in-memory rule back:
// leaving the cleared state in memory while the file still holds the old stage
// makes the next process start suppress a notification the user never saw.

// A persistence failure must be reported AND must roll the in-memory rule back:
// leaving the cleared state in memory while the file still holds the old stage
// makes the next process start suppress a notification the user never saw.
func TestPersistUsageMeterState_RollsBackOnWriteFailure(t *testing.T) {
	teardown := setupPersistTestEnv(t)
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

// ── Enable flag ──────────────────────────────────────────────────────────────

// The switch is part of the wire contract in both directions: GET must report
// it (or the panel renders an off switch for an enabled rule) and PATCH must
// accept it.
