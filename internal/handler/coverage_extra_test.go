package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
	"clawbench/internal/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Emptying a provider's model list breaks every config-directory agent bound to
// it (the file no longer declares any model the CLI can resolve), so the edit is
// refused with a 409 that NAMES those agents: without the names the user cannot
// know which agent to rebind.
func TestProvidersAPI_EmptyingModelsNamesTheBoundAgents(t *testing.T) {
	setupProvidersHandlerTest(t)
	clearSummaryBinding(t)

	p, err := service.CreateProvider(model.Provider{
		Name: "WithModels", Protocol: model.ProviderProtocolOpenAI,
		BaseURL: "https://api.example.com/v1", APIKey: "k", Models: []string{"m1"},
	})
	require.NoError(t, err)
	require.NoError(t, service.SaveAgent(store.WriteDB(), &model.Agent{
		ID: "pi-agent", Name: "Pi Agent", Backend: "pi", ProviderID: &p.ID,
	}))
	t.Cleanup(func() { model.ReplaceAgents(map[string]*model.Agent{}, nil) })

	rec := doProviderRequest(t, ServeProviderSubRoutes, http.MethodPatch,
		fmt.Sprintf("/api/providers/%d", p.ID), map[string]any{"models": []string{}})
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ProviderModelsRequired")
	assert.Contains(t, rec.Body.String(), "Pi Agent")

	stored, err := service.GetProvider(p.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"m1"}, stored.Models, "the refused edit must not be stored")
}

// ── terminal: the history-capture sink ─────────────────────────────────────

// Recording a command from the terminal is what makes the history panel useful,
// and the sink is installed by SetTerminalManager (a second creation site would
// otherwise forget it). This is the whole reason the sink exists: the service
// package cannot be imported from the terminal one, so the injection point is
// the only place the wiring can be checked.
//
// The recorder is unexported and has no test hook, and a real submission needs a
// foreground shell on a PTY (already covered by the terminal package's own
// tests). What is pinned here is the wiring: a manager created the way main.go
// creates it — through SetTerminalManager — must come out with a sink, or every
// command a user types silently never reaches the history panel.

// A nil agent or an empty project has nothing to probe: the probe needs a cwd
// for session/new, and a nil agent is the "unknown id" case of the caller.
func TestMaybeAutoProbeAgentState_NeedsAnAgentAndAProject(t *testing.T) {
	resetAutoProbeState(t)
	called := make(chan struct{}, 1)
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		called <- struct{}{}
		return &ai.ACPStateProbeResult{}, nil
	})
	defer restore()

	agent := &model.Agent{ID: "probe-guard", Backend: "claude", AcpCommand: "claude --acp", Transport: "acp-stdio"}
	maybeAutoProbeAgentState(nil, t.TempDir())
	maybeAutoProbeAgentState(agent, "")

	select {
	case <-called:
		t.Fatal("the probe must not start without an agent and a project")
	case <-time.After(200 * time.Millisecond):
		// A positive wait is the only way to assert "nothing happened"; 200ms is
		// orders of magnitude above the goroutine start the guard would have
		// reached, and the signal is the probe itself (not a timer race).
	}
	autoProbeMu.Lock()
	defer autoProbeMu.Unlock()
	assert.False(t, autoProbeInFlight[agent.ID], "a refused probe must not mark the agent in flight")
}

// While a probe is running, a second request for the same agent is dropped: three
// session creations in a row must not spawn three agent processes. The slot is
// released when the probe finishes, so the NEXT attempt does run.

// While a probe is running, a second request for the same agent is dropped: three
// session creations in a row must not spawn three agent processes. The slot is
// released when the probe finishes, so the NEXT attempt does run.
func TestMaybeAutoProbeAgentState_DedupesWhileInFlightAndReleasesTheSlot(t *testing.T) {
	resetAutoProbeState(t)
	agent := &model.Agent{ID: "probe-dedupe", Backend: "claude", AcpCommand: "claude --acp", Transport: "acp-stdio"}
	reg := ai.GetAgentCapabilityRegistry()
	t.Cleanup(func() { reg.UpdateModels(agent.ID, nil) })

	release := make(chan struct{})
	started := make(chan struct{}, 8)
	finished := make(chan struct{}, 8)
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		started <- struct{}{}
		<-release
		finished <- struct{}{}
		return &ai.ACPStateProbeResult{Models: []model.AgentModel{{ID: "m1", Name: "M1"}}}, nil
	})
	defer restore()

	maybeAutoProbeAgentState(agent, t.TempDir())
	select {
	case <-started:
	case <-timeoutAfter(t):
		t.Fatal("the first probe never started")
	}

	// In flight: the second and third calls return without a probe.
	maybeAutoProbeAgentState(agent, t.TempDir())
	maybeAutoProbeAgentState(agent, t.TempDir())
	select {
	case <-started:
		t.Fatal("a probe was started for an agent that already has one in flight")
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-finished:
	case <-timeoutAfter(t):
		t.Fatal("the probe never finished")
	}

	// The in-flight slot is released after completion (otherwise the second
	// session ever created for this agent would never be probed again).
	require.Eventually(t, func() bool {
		autoProbeMu.Lock()
		defer autoProbeMu.Unlock()
		return !autoProbeInFlight[agent.ID]
	}, 2*time.Second, 10*time.Millisecond, "the in-flight slot must be released when the probe ends")

	// A failure must not leave the slot taken either.
	failRelease := make(chan struct{})
	restore2 := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		<-failRelease
		return nil, errors.New("agent is not signed in")
	})
	defer restore2()
	maybeAutoProbeAgentState(agent, t.TempDir())
	close(failRelease)
	require.Eventually(t, func() bool {
		autoProbeMu.Lock()
		defer autoProbeMu.Unlock()
		return !autoProbeInFlight[agent.ID]
	}, 2*time.Second, 10*time.Millisecond, "a failed probe must release its slot too")
}

// A successful probe announces "agents_updated" so every open picker refills
// without a reload. The manager is process-global, so the test installs one and
// reads what the probe broadcast.

// A successful probe announces "agents_updated" so every open picker refills
// without a reload. The manager is process-global, so the test installs one and
// reads what the probe broadcast.
func TestMaybeAutoProbeAgentState_BroadcastsAgentsUpdatedOnSuccess(t *testing.T) {
	resetAutoProbeState(t)
	agent := &model.Agent{ID: "probe-broadcast", Backend: "claude", AcpCommand: "claude --acp", Transport: "acp-stdio"}
	reg := ai.GetAgentCapabilityRegistry()
	t.Cleanup(func() { reg.UpdateModels(agent.ID, nil) })

	prevMgr := ws.GetManager()
	mgr := ws.NewManagerForTest()
	ws.SetManagerForTest(mgr)
	t.Cleanup(func() { ws.SetManagerForTest(prevMgr) })

	// A disconnected subscriber is where BroadcastEvent leaves its trace: every
	// event is buffered for replay, so the buffer IS the observable outcome.
	var writeMu sync.Mutex
	sub := mgr.Subscribe(nil, &writeMu, "probe-client", "")
	require.NotNil(t, sub)
	mgr.DisconnectClient("probe-client")

	done := make(chan struct{})
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		close(done)
		return &ai.ACPStateProbeResult{
			Models:   []model.AgentModel{{ID: "m1", Name: "M1"}},
			Modes:    nil,
			Efforts:  nil,
			Commands: nil,
		}, nil
	})
	defer restore()

	maybeAutoProbeAgentState(agent, t.TempDir())
	select {
	case <-done:
	case <-timeoutAfter(t):
		t.Fatal("the probe never ran")
	}

	// The broadcast happens after the probe returns; poll the manager's own event
	// log rather than sleeping a fixed time.
	require.Eventually(t, func() bool {
		for _, msg := range sub.GetBufferedEvents() {
			if msg.Event == "agents_updated" {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "a successful probe must tell clients the agent list changed")
}

// ── agent install: remaining refusals ──────────────────────────────────────

// A registry backend without an install command (the built-in agent) is a 400
// with its own key: a 404 would say "unknown backend", which it is not.

// A registry backend without an install command (the built-in agent) is a 400
// with its own key: a 404 would say "unknown backend", which it is not.
func TestServeBackendInstall_BackendWithoutAnInstallCommand(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	// builtin has no CLI to install; it is in the registry with InstallCmd "".
	spec := model.FindSpecByBackend("builtin")
	require.NotNil(t, spec)
	require.Empty(t, strings.TrimSpace(spec.InstallCmd), "fixture assumption: builtin has no install command")

	req := newRequest(t, http.MethodPost, "/api/backends/"+spec.ID+"/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "InstallCommandUnavailable")
}

// The rescan is best-effort: a refresh failure must not turn a successful install
// into an error, but it must be REPORTED as refreshed:false so the UI can ask for
// a manual rescan.

// The rescan is best-effort: a refresh failure must not turn a successful install
// into an error, but it must be REPORTED as refreshed:false so the UI can ask for
// a manual rescan.
func TestRescanAfterInstall_ReportsFailureWithoutFailingTheInstall(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// RefreshAgents reloads the agent set from the database; hiding that table
	// is the failure a rescan can actually hit (a half-migrated store), and it is
	// the one this handler has to survive without failing the install.
	_, err := store.WriteExec("ALTER TABLE agents RENAME TO agents_renamed")
	require.NoError(t, err)
	assert.False(t, rescanAfterInstall("claude"), "a failed rescan must be reported, not swallowed")
}

// The service's sentinel errors each have their own status; a busy install is a
// 409 (retry later) while an empty command is a 400 (nothing to run).

// The service's sentinel errors each have their own status; a busy install is a
// 409 (retry later) while an empty command is a 400 (nothing to run).
func TestWriteAgentInstallError_MapsSentinels(t *testing.T) {
	cases := []struct {
		err    error
		status int
		key    string
	}{
		{service.ErrAgentInstallBusy, http.StatusConflict, "AgentInstallBusy"},
		{service.ErrAgentInstallEmptyCommand, http.StatusBadRequest, "InstallCommandUnavailable"},
		{errors.New("something unexpected"), http.StatusInternalServerError, "InternalError"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeAgentInstallError(rec, httptest.NewRequest(http.MethodPost, "/api/backends/x/install", http.NoBody), "x", tc.err)
		assert.Equal(t, tc.status, rec.Code, "%v: %s", tc.err, rec.Body.String())
		assert.Contains(t, rec.Body.String(), tc.key)
	}
}

// A Node.js failure with no underlying cause still names the tool and says the
// automatic install failed, rather than printing an empty reason after a colon.

// A Node.js failure with no underlying cause still names the tool and says the
// automatic install failed, rather than printing an empty reason after a colon.
func TestWriteAgentInstallError_NodeFailureWithoutCause(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAgentInstallError(rec, httptest.NewRequest(http.MethodPost, "/api/backends/x/install", http.NoBody), "x",
		&service.AgentInstallNodeMissingError{Tool: "npm"})
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "NodeRuntimeMissing")
	assert.Contains(t, rec.Body.String(), "npm")
	assert.Contains(t, rec.Body.String(), "the automatic install failed")
}

// ── live notes: clear-all per-row failures ────────────────────────────────

// A row whose artifact-job lookup fails is skipped, not fatal: refusing the whole
// clear because one row's query broke would leave the other ninety-nine behind.

// ── auto probe of an ACP agent ─────────────────────────────────────────────

func resetAutoProbeState(t *testing.T) {
	t.Helper()
	autoProbeMu.Lock()
	prev := autoProbeInFlight
	autoProbeInFlight = map[string]bool{}
	autoProbeMu.Unlock()
	t.Cleanup(func() {
		autoProbeMu.Lock()
		autoProbeInFlight = prev
		autoProbeMu.Unlock()
	})
}
