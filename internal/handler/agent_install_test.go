package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/platform/source"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubInstallForTest replaces the service's command runner for one test.
func timeoutAfter(t *testing.T) <-chan struct{} {
	t.Helper()
	ch := make(chan struct{})
	timer := time.AfterFunc(2*time.Second, func() { close(ch) })
	t.Cleanup(func() { timer.Stop() })
	return ch
}

func TestServeBackendInstall_UnknownBackendIs404(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/backends/does-not-exist/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	// A 400/500 here would claim the route works for a name it does not know.
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestServeBackendInstall_RejectsWrongPath(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/backends/claude/something-else", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeBackendInstall_MethodNotAllowed(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/backends/claude/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// The command comes from the registry, never from the request body. This is the
// property that keeps the endpoint from being arbitrary remote execution, so it
// is pinned directly: a body carrying its own command must be ignored.
func TestServeBackendInstall_IgnoresClientSuppliedCommand(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	var ranCommand string
	prevRun := service.SetAgentInstallRunForTest(func(_ context.Context, command string) (string, error) {
		ranCommand = command
		return "ok", nil
	})
	prevLook := service.SetAgentInstallLookPathForTest(func(string) (string, error) { return "/usr/bin/npm", nil })
	defer func() { prevRun(); prevLook() }()

	withInstallSources(t, []source.Candidate{{Name: "region-mirror", Base: source.NpmMirrorBase}})
	stubNpmProbe(t, func(context.Context, string, string) error { return nil })

	req := newRequest(t, http.MethodPost, "/api/backends/claude/install", map[string]any{
		"command": "rm -rf /",
	})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	spec := model.FindSpecByBackend("claude")
	require.NotNil(t, spec)
	assert.Equal(t, displayInstallCmd(spec.InstallCmd), ranCommand)
	assert.NotContains(t, ranCommand, "rm -rf")
}

func TestServeBackendInstall_MissingNodeIsActionable(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	prevLook := service.SetAgentInstallLookPathForTest(func(string) (string, error) {
		return "", errors.New("not found")
	})
	prevNode := service.SetAgentInstallNodeRuntimeForTest(func() (string, error) {
		return "", errors.New("dial tcp: connection refused")
	})
	defer func() { prevLook(); prevNode() }()

	req := newRequest(t, http.MethodPost, "/api/backends/claude/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var resp struct {
		Error  string         `json:"error"`
		MsgKey string         `json:"msgKey"`
		Detail map[string]any `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// A distinct key: "install failed" would not tell the user the fix is to
	// install Node.js.
	assert.Equal(t, "NodeRuntimeMissing", resp.MsgKey)
	assert.Equal(t, "npm", resp.Detail["Tool"])
	// The automatic install already ran, so WHY it failed is the actionable
	// part — without it the user cannot tell a blocked network from a broken
	// machine. It must also survive templating into the message, not just the
	// detail map.
	assert.Contains(t, resp.Detail["Error"], "connection refused")
	assert.Contains(t, resp.Error, "connection refused")
	assert.NotContains(t, resp.Error, "<no value>")
}

// A machine without Node.js installs one and then finishes the job: the endpoint
// must not stop at "npm was not found".
func TestServeBackendInstall_InstallsNodeRuntimeThenAgent(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	lookups := 0
	prevLook := service.SetAgentInstallLookPathForTest(func(string) (string, error) {
		lookups++
		if lookups == 1 {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/npm", nil
	})
	prevNode := service.SetAgentInstallNodeRuntimeForTest(func() (string, error) {
		return "ClawBench installed Node.js v22.11.0 (private copy: /x)", nil
	})
	prevRun := service.SetAgentInstallRunForTest(func(context.Context, string) (string, error) {
		return "added 1 package", nil
	})
	defer func() { prevLook(); prevNode(); prevRun() }()

	req := newRequest(t, http.MethodPost, "/api/backends/claude/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		Output string `json:"output"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp.Output, "Node.js v22.11.0")
	assert.Contains(t, resp.Output, "added 1 package")
}

func TestServeBackendInstall_CommandFailureShowsOutput(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	prevRun := service.SetAgentInstallRunForTest(func(context.Context, string) (string, error) {
		return "npm ERR! 404 Not Found", errors.New("exit status 1")
	})
	prevLook := service.SetAgentInstallLookPathForTest(func(string) (string, error) { return "/usr/bin/npm", nil })
	defer func() { prevRun(); prevLook() }()

	req := newRequest(t, http.MethodPost, "/api/backends/claude/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "404 Not Found",
		"the installer's own output is the only thing that says WHY")
}

func TestServeBackendInstall_SilentFailureHasItsOwnMessage(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	prevRun := service.SetAgentInstallRunForTest(func(context.Context, string) (string, error) {
		return "   ", errors.New("exit status 3")
	})
	prevLook := service.SetAgentInstallLookPathForTest(func(string) (string, error) { return "/usr/bin/npm", nil })
	defer func() { prevRun(); prevLook() }()

	req := newRequest(t, http.MethodPost, "/api/backends/claude/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	var resp struct {
		MsgKey string `json:"msgKey"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// "install failed: " with nothing after the colon reads like a UI bug.
	assert.Equal(t, "AgentInstallFailedNoOutput", resp.MsgKey)
}

func TestServeBackendInstall_SuccessRescansAndReturnsAgents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	prevRun := service.SetAgentInstallRunForTest(func(context.Context, string) (string, error) {
		return "added 1 package", nil
	})
	prevLook := service.SetAgentInstallLookPathForTest(func(string) (string, error) { return "/usr/bin/npm", nil })
	defer func() { prevRun(); prevLook() }()

	req := newRequest(t, http.MethodPost, "/api/backends/claude/install", nil)
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeBackendInstall, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		OK      bool   `json:"ok"`
		Backend string `json:"backend"`
		Output  string `json:"output"`
		// Auto-rescan is the difference between "installed" and "installed and
		// usable without pressing Rescan".
		Refreshed bool  `json:"refreshed"`
		Agents    []any `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.Equal(t, "claude", resp.Backend)
	assert.Equal(t, "added 1 package", resp.Output)
	assert.NotNil(t, resp.Agents, "the fresh agent list must ride along so the caller can render it")
}

// The settings page's install button needs the same command the welcome panel
// shows, without a second request — and the two must not diverge.
func TestServeAgentsGet_CarriesBackendInstallCmd(t *testing.T) {
	defer setupAgentTestEnv(t)()
	withInstallSources(t, []source.Candidate{{Name: "region-mirror", Base: source.NpmMirrorBase}})

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Agents []struct {
			ID         string `json:"id"`
			Backend    string `json:"backend"`
			InstallCmd string `json:"installCmd"`
		} `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	var found bool
	for _, a := range resp.Agents {
		if a.Backend != "claude" {
			continue
		}
		found = true
		spec := model.FindSpecByBackend("claude")
		require.NotNil(t, spec)
		assert.Equal(t, displayInstallCmd(spec.InstallCmd), a.InstallCmd)
	}
	assert.True(t, found, "the claude agent must be in the fixture")
}

// ── first-session auto probe ────────────────────────────────────────────────

// The whole point: a session created for an agent that has never reported its
// capabilities starts the probe, so the pickers are not empty.
func TestCreateSession_AutoProbesAgentWithNoCapabilities(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID:         "probe-me",
		Name:       "Probe Me",
		Backend:    "claude",
		AcpCommand: "claude --acp",
		Transport:  "acp-stdio",
	}
	model.Agents[agent.ID] = agent
	model.AgentList = append(model.AgentList, agent)
	reg := ai.GetAgentCapabilityRegistry()
	t.Cleanup(func() {
		reg.UpdateModels(agent.ID, nil)
		reg.UpdateModes(agent.ID, nil)
		reg.UpdateThinkingEfforts(agent.ID, nil)
		reg.UpdateCommands(agent.ID, nil)
	})

	probed := make(chan string, 1)
	restore := ai.SetACPStateProbeForTest(func(_ context.Context, a *model.Agent, cwd string) (*ai.ACPStateProbeResult, error) {
		probed <- a.ID
		return &ai.ACPStateProbeResult{Models: []model.AgentModel{{ID: "m1", Name: "M1"}}}, nil
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{"agentId": agent.ID})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	select {
	case got := <-probed:
		assert.Equal(t, agent.ID, got)
	case <-timeoutAfter(t):
		t.Fatal("the probe never ran; a fresh ACP agent would keep empty pickers until the first message")
	}
}

// An agent that HAS reported capabilities must not be probed again: that would
// spawn a process on every new session for no reason.
func TestCreateSession_SkipsProbeWhenCapabilitiesExist(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID:         "already-known",
		Name:       "Known",
		Backend:    "claude",
		AcpCommand: "claude --acp",
		Transport:  "acp-stdio",
	}
	model.Agents[agent.ID] = agent
	model.AgentList = append(model.AgentList, agent)

	reg := ai.GetAgentCapabilityRegistry()
	reg.UpdateModels(agent.ID, []model.AgentModel{{ID: "m1", Name: "M1"}})
	t.Cleanup(func() { reg.UpdateModels(agent.ID, nil) })

	probed := make(chan struct{}, 1)
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		probed <- struct{}{}
		return &ai.ACPStateProbeResult{}, nil
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{"agentId": agent.ID})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	select {
	case <-probed:
		t.Fatal("an agent that already reported its capabilities must not be probed")
	case <-timeoutAfter(t):
	}
}

// A CLI agent has no capabilities to report — discovery already produced its
// model list, and probing would fail with "needs an agent with an acp command".
func TestCreateSession_DoesNotProbeCLIAgents(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	agent := &model.Agent{
		ID:      "cli-only",
		Name:    "CLI Only",
		Backend: "claude",
		// No AcpCommand: agentUsesACP is false.
		Models: []model.AgentModel{{ID: "m1", Name: "M1"}},
	}
	model.Agents[agent.ID] = agent
	model.AgentList = append(model.AgentList, agent)

	probed := make(chan struct{}, 1)
	restore := ai.SetACPStateProbeForTest(func(context.Context, *model.Agent, string) (*ai.ACPStateProbeResult, error) {
		probed <- struct{}{}
		return &ai.ACPStateProbeResult{}, nil
	})
	defer restore()

	req := newRequest(t, http.MethodPost, "/api/ai/sessions", map[string]any{"agentId": agent.ID})
	withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeSessions, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	select {
	case <-probed:
		t.Fatal("a CLI agent must not be probed over ACP")
	case <-timeoutAfter(t):
	}
}

// The install rescan must probe ONLY the backend that was just installed.
//
// Probing every backend is not merely slow (~18s serial, since each probe forks
// a vendor CLI and antigravity hangs out its timeout when logged out): it ran
// while holding configMutex, so every concurrent PATCH /api/config, agent edit
// and project switch queued behind a one-click install to learn nothing — the
// other backends' model lists cannot have changed.
func TestRescanAfterInstall_ProbesOnlyInstalledBackend(t *testing.T) {
	defer setupAgentTestEnv(t)()

	prevDiscoverModels := model.DiscoverModels
	prevWithDetail := model.DiscoverWithDetail
	t.Cleanup(func() {
		model.DiscoverModels = prevDiscoverModels
		model.DiscoverWithDetail = prevWithDetail
	})

	var mu sync.Mutex
	var probed []string
	model.DiscoverWithDetail = func(backend string) ([]model.AgentModel, string) {
		mu.Lock()
		probed = append(probed, backend)
		mu.Unlock()
		return []model.AgentModel{{ID: backend + "-model", Name: backend + "-model"}}, ""
	}
	model.DiscoverModels = func(backend string) []model.AgentModel {
		models, _ := model.DiscoverWithDetail(backend)
		return models
	}

	require.True(t, rescanAfterInstall("claude"), "the rescan must succeed for a registered backend")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"claude"}, probed,
		"only the installed backend may be probed; probing the rest is the 18s under configMutex")
}

// A backend with no discovery source still needs the rescan to insert its agent
// row — that is the whole reason the install endpoint runs the refresh at all.
// Narrowing the probe list must not turn into "skip the refresh".
func TestRescanAfterInstall_NarrowProbeStillRefreshesAgents(t *testing.T) {
	defer setupAgentTestEnv(t)()

	prevDiscoverModels := model.DiscoverModels
	prevWithDetail := model.DiscoverWithDetail
	t.Cleanup(func() {
		model.DiscoverModels = prevDiscoverModels
		model.DiscoverWithDetail = prevWithDetail
	})
	probed := 0
	model.DiscoverWithDetail = func(string) ([]model.AgentModel, string) {
		probed++
		return nil, ""
	}
	model.DiscoverModels = func(string) []model.AgentModel { return nil }

	require.True(t, rescanAfterInstall("claude"))
	assert.LessOrEqual(t, probed, 1, "at most the installed backend may be probed")
}
