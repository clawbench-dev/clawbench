package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ws"
)

// A completed turn must reach the header capsule on its own. Before the tick
// event the numbers only moved when a threshold was crossed (usage_meter_update)
// or the user opened the popover, so the capsule showed a stale figure for up to
// five minutes after every task.
func TestUsageMeter_TickReachesConnectedClient(t *testing.T) {
	db := setupDB(t)
	ensureNotificationTables(t, db)
	ensureAgentsTable(t, db)

	prev := model.ConfigInstance.UsageMeter
	t.Cleanup(func() { model.ConfigInstance.UsageMeter = prev })

	mgr := ws.NewManagerForTest()
	prevMgr := ws.GetManager()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(prevMgr) })

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ai/events/ws", ws.EventsHandler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, "ws"+srv.URL[4:]+"/api/ai/events/ws?client_id=tick-e2e", nil)
	require.NoError(t, err)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	time.Sleep(200 * time.Millisecond) // let the handler register

	now := time.Now()
	setUsageMeterRule(t, model.UsageMeterConfig{
		AnchorDate: anchorOn(now, 5), PeriodDays: 30,
		Metric: model.UsageMeterMetricCost, Quota: 20,
	})
	insertUsageSeed(t, db, usageSeed{
		project: "/a", sessionID: "s-tick", agentID: "cb", agentName: "CB", backend: "codebuddy",
		model: "m", createdAt: now.Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04:05"), costUSD: 5,
	})

	// A check runs on every completed turn; this is that path. Day 6 of 30 with
	// a 20 quota means 4.00 is on plan, so 5.00 also crosses the baseline and
	// the check legitimately returns "over" — irrelevant here, the point is the
	// tick frame, which is broadcast regardless of the threshold outcome.
	service.SetUsageMeterNowForTest(nil)
	_ = service.CheckUsageMeter(context.Background())

	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer readCancel()
	_, data, err := conn.Read(readCtx)
	require.NoError(t, err, "the client should receive a frame without opening the popover")

	var msg map[string]any
	require.NoError(t, json.Unmarshal(data, &msg))
	assert.Equal(t, "usage_meter_tick", msg["event"])

	body, ok := msg["data"].(map[string]any)
	require.True(t, ok)
	assert.InDelta(t, 5.0, body["actual"], 1e-9)
	assert.InDelta(t, 20.0, body["quota"], 1e-9)
}
