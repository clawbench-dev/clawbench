package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/handler"
	"clawbench/internal/model"
	"clawbench/internal/plugins"
	"clawbench/internal/terminal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFeatureSwitches snapshots the feature switches, hands them to fn, and
// restores them afterwards.
//
// ConfigInstance is a package-global that other tests in this package read, and
// the master-switch globals live outside it — so both have to be saved, not
// just the config.
func withFeatureSwitches(t *testing.T, mutate func()) {
	t.Helper()
	origCfg := model.ConfigInstance
	origTasks, origShare := model.TasksEnabled, model.ShareEnabled
	origToken, origCookieToken := model.SessionToken, model.CookieToken

	// No password: middleware.Auth then accepts every request, so these tests
	// exercise the feature gate rather than the auth gate.
	model.SessionToken, model.CookieToken = "", ""

	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.TasksEnabled, model.ShareEnabled = origTasks, origShare
		model.SessionToken, model.CookieToken = origToken, origCookieToken
	})

	mutate()
}

// newGatedMux builds the real route table with the real plugin registry.
func newGatedMux(t *testing.T) *http.ServeMux {
	t.Helper()
	plugins.Reset()
	t.Cleanup(plugins.Reset)
	registerPlugins(0) // the port only matters to terminal.Start, which tests never call

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux
}

func callRoute(mux *http.ServeMux, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// switchOff flips one plugin's master switch. The default case fails loudly so a
// newly registered feature cannot silently skip the table below.
func switchOff(t *testing.T, id string) {
	t.Helper()
	switch id {
	case "forge":
		model.ConfigInstance.Forge.Enabled = false
	case "kb":
		model.ConfigInstance.KB.Enabled = false
	case "mail":
		model.ConfigInstance.Mail.Enabled = false
	case "share":
		model.ShareEnabled = false
	case "tasks":
		model.TasksEnabled = false
	case "terminal":
		model.ConfigInstance.Terminal.Enabled = false
	default:
		t.Fatalf("plugin %q has an Enabled func but no switch here — add it to switchOff", id)
	}
}

// TestMailPrefixRejectedWhenDisabled is the Phase-0 regression: turning mail off
// used to stop only the background sync worker, while /api/mail/send kept
// sending. Every route under the prefix must now be refused.
func TestMailPrefixRejectedWhenDisabled(t *testing.T) {
	withFeatureSwitches(t, func() { model.ConfigInstance.Mail.Enabled = false })
	mux := newGatedMux(t)

	paths := []struct{ method, path string }{
		{http.MethodPost, "/api/mail/send"},
		{http.MethodGet, "/api/mail/presets"},
		{http.MethodGet, "/api/mail/accounts"},
		{http.MethodGet, "/api/mail/folders"},
		{http.MethodGet, "/api/mail/messages"},
		{http.MethodGet, "/api/mail/unread"},
		{http.MethodGet, "/api/mail/attachments"},
		{http.MethodGet, "/api/mail/drafts"},
		// Subtree routes: the gate must cover parameterised paths too.
		{http.MethodGet, "/api/mail/accounts/abc"},
		{http.MethodGet, "/api/mail/messages/42"},
	}
	for _, tc := range paths {
		w := callRoute(mux, tc.method, tc.path)
		assert.Equalf(t, http.StatusConflict, w.Code,
			"%s %s must be refused while mail is off", tc.method, tc.path)
		assert.Containsf(t, w.Body.String(), "MailDisabled",
			"%s %s must name the mail switch in its msgKey", tc.method, tc.path)
	}
}

func TestMailPrefixAllowedWhenEnabled(t *testing.T) {
	withFeatureSwitches(t, func() { model.ConfigInstance.Mail.Enabled = true })
	mux := newGatedMux(t)

	// /api/mail/presets is a static list: it must reach the handler rather than
	// being refused by the gate.
	w := callRoute(mux, http.MethodGet, "/api/mail/presets")
	require.NotEqual(t, http.StatusConflict, w.Code, "enabled mail must not be gated")
	assert.NotContains(t, w.Body.String(), "MailDisabled")
}

// TestGate_EveryDisabledPluginRejectsItsRoutes is the table that makes "关闭即
// 拒绝" a property of the registry rather than of each handler.
//
// It walks the REAL route table and the REAL registry: for every switchable
// plugin, every authenticated route the registry attributes to it must answer
// with the plugin's status and msgKey once the switch is off. A new endpoint
// under an existing prefix is therefore covered the moment it is registered.
func TestGate_EveryDisabledPluginRejectsItsRoutes(t *testing.T) {
	all := registeredPluginsForTest(t)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	routes := handler.RegisteredRoutes()
	require.NotEmpty(t, routes)

	checkedPlugins := 0
	for _, p := range all {
		if p.Enabled == nil {
			continue // pure-preference plugin: nothing can ever be refused
		}
		checkedPlugins++
		t.Run(p.ID, func(t *testing.T) {
			withFeatureSwitches(t, func() { switchOff(t, p.ID) })

			checkedRoutes := 0
			for _, route := range routes {
				// Public routes are deliberately outside the gate: /api/share/{token}
				// must 404 on its own terms, not 409.
				if !route.Authenticated {
					continue
				}
				if plugins.ForRoute(route.Pattern) != p {
					continue
				}
				method, ok := gatedMethodFor(p, route.Pattern)
				if !ok {
					continue // every method on this route is exempt
				}
				w := callRoute(mux, method, route.Pattern)
				assert.Equalf(t, p.DisabledHTTPStatus(), w.Code,
					"%s %s must be refused while %q is off", method, route.Pattern, p.ID)
				assert.Containsf(t, w.Body.String(), p.DisabledKey(),
					"%s %s must carry the %q msgKey", method, route.Pattern, p.DisabledKey())
				checkedRoutes++
			}
			assert.NotZerof(t, checkedRoutes,
				"no gated route found for %q: its APIPrefixes match nothing (or every method is exempt)", p.ID)
		})
	}
	assert.NotZero(t, checkedPlugins, "no plugin has an Enabled func — the table would be vacuous")
}

// TestGate_ExemptionsNameRealRoutes keeps GateExempt honest: an exemption for a
// path that does not exist (typo, renamed route) would silently leave a route
// ungated the day someone adds it, and an exemption pointing at another plugin's
// prefix would punch a hole in that plugin's gate.
func TestGate_ExemptionsNameRealRoutes(t *testing.T) {
	all := registeredPluginsForTest(t)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	routes := handler.RegisteredRoutes()

	for _, p := range all {
		for _, entry := range p.GateExempt {
			_, target, _ := strings.Cut(entry, " ")
			target = strings.TrimSpace(target)
			if target == "" {
				target = strings.TrimSpace(entry)
			}
			assert.Truef(t, routeCovers(routes, target),
				"plugin %q exempts %q, which matches no registered route", p.ID, entry)
			assert.Samef(t, p, plugins.ForRoute(target),
				"plugin %q exempts %q, which belongs to another plugin", p.ID, entry)
		}
	}
}

// TestGate_PublicShareRoutesStayOut pins the "old links 404, not 409" rule: the
// public share endpoints are registered outside middleware.Auth and outside the
// gate, so a disabled share switch must not turn them into 409s.
func TestGate_PublicShareRoutesStayOut(t *testing.T) {
	withFeatureSwitches(t, func() { model.ShareEnabled = false })
	mux := newGatedMux(t)

	for _, path := range []string{"/api/share/abc123", "/share/abc123"} {
		w := callRoute(mux, http.MethodGet, path)
		assert.NotEqualf(t, http.StatusConflict, w.Code,
			"%s must not be gated (public routes answer 404 on their own)", path)
		assert.NotContains(t, w.Body.String(), "ShareDisabled")
	}
}

// TestGate_ForgeBindingGetIsExempt: the frontend reads the binding to decide
// "not bound", so GET must keep answering while forge is off — only writes are
// refused. The exact body is not asserted here (that needs a project + DB);
// what matters is that the gate did not fire.
func TestGate_ForgeBindingGetIsExempt(t *testing.T) {
	withFeatureSwitches(t, func() { model.ConfigInstance.Forge.Enabled = false })
	mux := newGatedMux(t)

	w := callRoute(mux, http.MethodGet, "/api/forge/binding")
	assert.NotEqual(t, http.StatusConflict, w.Code, "GET /api/forge/binding must stay reachable")
	assert.NotContains(t, w.Body.String(), "ForgeDisabled")

	w = callRoute(mux, http.MethodPost, "/api/forge/binding")
	assert.Equal(t, http.StatusConflict, w.Code, "writing a binding must be refused")
	assert.Contains(t, w.Body.String(), "ForgeDisabled")
}

// TestGate_TerminalStatusIsExempt pins the one terminal route that must stay
// reachable while the feature is off.
//
// The status endpoint is a read-only capability probe: the settings page uses
// `platform_supported` to decide "this OS cannot run a terminal" (grey the
// switch out) versus "just switched off". Gating it returned 503, the frontend's
// catch set platformSupported = false as well, and re-enabling the terminal left
// the switch stuck as "on but unsupported" with no dock tab until a reload.
//
// The second half is the counterweight: every OTHER route under the prefix must
// still be refused, so the exemption cannot quietly widen.
func TestGate_TerminalStatusIsExempt(t *testing.T) {
	withFeatureSwitches(t, func() { model.ConfigInstance.Terminal.Enabled = false })
	mux := newGatedMux(t)

	p := plugins.Lookup("terminal")
	require.NotNil(t, p)
	assert.True(t, p.IsGateExempt(http.MethodGet, "/api/terminal/status"),
		"the probe must be declared exempt, not merely skipped by the test")

	w := callRoute(mux, http.MethodGet, "/api/terminal/status")
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var body struct {
		Enabled           bool `json:"enabled"`
		PlatformSupported bool `json:"platform_supported"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.False(t, body.Enabled, "the feature is off, so the runtime is not enabled")
	assert.Equal(t, terminal.PlatformSupported(), body.PlatformSupported,
		"platform_supported must describe the platform, not the switch")

	for _, path := range []string{
		"/api/terminal/close",
		"/api/terminal/history",
		"/api/terminal/key-config",
		"/api/terminal/ws",
	} {
		w := callRoute(mux, http.MethodGet, path)
		assert.Equalf(t, http.StatusServiceUnavailable, w.Code, "%s must still be refused", path)
		assert.Containsf(t, w.Body.String(), "TerminalDisabled", "%s must name the switch", path)
	}
}

// TestGate_TasksKeepsReadAndCleanupRoutes is the reason tasks is not a plain
// prefix gate: while tasks are off the user must still be able to inspect the
// list and cancel/delete what the switch left behind.
func TestGate_TasksKeepsReadAndCleanupRoutes(t *testing.T) {
	withFeatureSwitches(t, func() { model.TasksEnabled = false })
	mux := newGatedMux(t)

	allowed := []struct{ method, path string }{
		{http.MethodGet, "/api/tasks"},
		{http.MethodGet, "/api/tasks/7"},
		{http.MethodDelete, "/api/tasks/7"},
		{http.MethodPost, "/api/tasks/7"}, // action endpoint; resume/trigger decide in the handler
	}
	for _, tc := range allowed {
		w := callRoute(mux, tc.method, tc.path)
		assert.NotEqualf(t, http.StatusConflict, w.Code,
			"%s %s must stay reachable while tasks are off", tc.method, tc.path)
	}

	refused := []struct{ method, path string }{
		{http.MethodPost, "/api/tasks"},  // create
		{http.MethodPut, "/api/tasks/7"}, // edit / re-arm
	}
	for _, tc := range refused {
		w := callRoute(mux, tc.method, tc.path)
		assert.Equalf(t, http.StatusConflict, w.Code,
			"%s %s must be refused while tasks are off", tc.method, tc.path)
		assert.Contains(t, w.Body.String(), "TasksDisabled")
	}
}

// gatedMethodFor returns a method the gate does NOT exempt for this path, i.e.
// one that must be refused while the feature is off.
func gatedMethodFor(p *plugins.Plugin, path string) (string, bool) {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
	} {
		if !p.IsGateExempt(method, path) {
			return method, true
		}
	}
	return "", false
}

// routeCovers reports whether any registered route serves target, applying the
// same exact-or-subtree semantics ServeMux uses.
func routeCovers(routes []handler.Route, target string) bool {
	clean := strings.TrimSuffix(target, "/")
	for _, route := range routes {
		pattern := strings.TrimSuffix(route.Pattern, "/")
		if pattern == clean || strings.HasPrefix(clean, pattern+"/") {
			return true
		}
	}
	return false
}

// TestPluginLifecycle_FlipsStartStopElseReconfigure pins the driver that turns a
// config PATCH into Start / Stop / Reconfigure.
//
// The distinction it must get right: a switch FLIP starts or stops the resource,
// a config change with the switch untouched only reconfigures. Getting it wrong
// either leaks a goroutine per PATCH or leaves a re-enabled feature dead.
func TestPluginLifecycle_FlipsStartStopElseReconfigure(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)
	pluginRunning = map[string]bool{}

	enabled := true
	starts, stops, reconfigures := 0, 0, 0
	plugins.Register(&plugins.Plugin{
		ID:          "fake",
		Enabled:     func() bool { return enabled },
		Start:       func(model.Config) { starts++ },
		Stop:        func() { stops++ },
		Reconfigure: func(model.Config) { reconfigures++ },
	})

	startPlugins(model.Config{})
	assert.Equal(t, 1, starts, "boot starts an enabled plugin")
	assert.True(t, pluginIsRunning("fake"))

	syncPluginLifecycle(model.Config{})
	assert.Equal(t, 1, starts, "no flip: Start must not run again")
	assert.Equal(t, 1, reconfigures, "no flip: Reconfigure runs instead")

	enabled = false
	syncPluginLifecycle(model.Config{})
	assert.Equal(t, 1, stops, "flip to off stops the plugin")
	assert.False(t, pluginIsRunning("fake"))

	syncPluginLifecycle(model.Config{})
	assert.Equal(t, 1, stops, "already stopped: Stop must not run again")
	assert.Equal(t, 2, reconfigures)

	enabled = true
	syncPluginLifecycle(model.Config{})
	assert.Equal(t, 2, starts, "flip to on starts it again")
	assert.True(t, pluginIsRunning("fake"))
}

func TestPluginLifecycle_DoesNotStartDisabledPlugin(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)
	pluginRunning = map[string]bool{}

	started := false
	plugins.Register(&plugins.Plugin{
		ID:      "off",
		Enabled: func() bool { return false },
		Start:   func(model.Config) { started = true },
	})

	startPlugins(model.Config{})
	assert.False(t, started, "a feature that is off at boot must not get a goroutine")
	assert.False(t, pluginIsRunning("off"))
}

func TestPluginLifecycle_StopsInReverseOrder(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)
	pluginRunning = map[string]bool{}

	var order []string
	for _, id := range []string{"a", "b", "c"} {
		plugins.Register(&plugins.Plugin{
			ID:    id,
			Start: func(model.Config) {},
			Stop:  func() { order = append(order, id) },
		})
	}

	startPlugins(model.Config{})
	stopPlugins()

	assert.Equal(t, []string{"c", "b", "a"}, order,
		"plugins must stop in reverse start order so consumers go down before their dependencies")
}

// TestStopPluginsStopsEverythingThatStarted is the shutdown half of the
// lifecycle driver: every plugin whose Start ran must have Stop called, in
// reverse start order, so consumers go down before their dependencies.
//
// Stop is called on never-started plugins too (it is required to be idempotent —
// see TestTerminalPluginLifecycleIsIdempotent). At shutdown that is the safe
// direction: a Start that failed halfway still gets its cleanup.
func TestStopPluginsStopsEverythingThatStarted(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)
	pluginRunning = map[string]bool{}

	var started, stopped []string
	for _, id := range []string{"a", "b", "c"} {
		plugins.Register(&plugins.Plugin{
			ID: id,
			// "b" is switched off at boot: it must not be started.
			Enabled: func() bool { return id != "b" },
			Start:   func(model.Config) { started = append(started, id) },
			Stop:    func() { stopped = append(stopped, id) },
		})
	}

	startPlugins(model.Config{})
	assert.Equal(t, []string{"a", "c"}, started, "a disabled plugin must not get a goroutine")

	stopPlugins()
	assert.ElementsMatch(t, started, []string{"a", "c"}, "every started plugin must have been started")
	assert.Equal(t, []string{"c", "b", "a"}, stopped,
		"plugins must stop in reverse start order (b included: Stop is idempotent)")
}

// TestTerminalPluginLifecycleIsIdempotent pins the contract the driver relies
// on: Start and Stop may be called repeatedly without stacking managers or
// panicking on an already-closed one. Terminal is the plugin that really owns a
// resource here (a Manager holding PTY sessions), so it is the meaningful case;
// mail/forge guard themselves with a global-pointer check inside the service
// package.
func TestTerminalPluginLifecycleIsIdempotent(t *testing.T) {
	registeredPluginsForTest(t)

	p := plugins.Lookup("terminal")
	require.NotNil(t, p)

	orig := handler.GetTerminalManager()
	t.Cleanup(func() { handler.SetTerminalManager(orig) })
	handler.SetTerminalManager(nil)

	p.Start(model.Config{})
	first := handler.GetTerminalManager()
	require.NotNil(t, first, "Start must create the manager")

	p.Start(model.Config{})
	assert.Same(t, first, handler.GetTerminalManager(), "a second Start must not stack a second manager")

	p.Stop()
	assert.Nil(t, handler.GetTerminalManager(), "Stop must release the manager")

	p.Stop() // must not panic or resurrect anything
	assert.Nil(t, handler.GetTerminalManager())
}

// TestOnDisableRunsOnlyAfterPersist pins the red line from the design: the
// destructive half of a disable must never run for a change that failed to
// persist. Doing it first would destroy the user's data while the switch stayed
// on — the worst of both states.
func TestOnDisableRunsOnlyAfterPersist(t *testing.T) {
	origDataDir := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = origDataDir })

	plugins.Reset()
	t.Cleanup(plugins.Reset)

	calls := 0
	plugins.Register(&plugins.Plugin{
		ID:        "mail",
		Section:   "mail",
		Enabled:   func() bool { return model.ConfigInstance.Mail.Enabled },
		OnDisable: func() []string { calls++; return nil },
	})

	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeConfig(w, req)
		return w
	}

	withFeatureSwitches(t, func() { model.ConfigInstance.Mail.Enabled = true })

	// 1. Successful write: the hook runs.
	w := patch(`{"mail":{"enabled":false}}`)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, 1, calls, "OnDisable must run once the config was persisted")

	// 2. Failed write: the hook must NOT run.
	//
	// A fresh data dir whose "config" entry is a regular FILE: MkdirAll then
	// fails and writeConfigYAML returns before touching anything.
	model.ConfigInstance.Mail.Enabled = true
	model.DataDir = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(model.DataDir, "config"), []byte("not a directory"), 0o644))

	w = patch(`{"mail":{"enabled":false}}`)
	require.Equal(t, http.StatusInternalServerError, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, 1, calls, "OnDisable must not run when the config write failed")
	assert.True(t, model.ConfigInstance.Mail.Enabled, "the in-memory config must roll back too")
}
