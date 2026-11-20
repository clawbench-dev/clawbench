package plugins

import (
	"net/http"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetRegistry isolates each test: the registry is package-global, so a
// leftover plugin would make ForRoute answer for routes the next test never
// declared.
func resetRegistry(t *testing.T) {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
}

func TestRegisterPanicsOnProgrammingErrors(t *testing.T) {
	resetRegistry(t)

	cases := []struct {
		name   string
		plugin *Plugin
	}{
		{"nil plugin", nil},
		{"empty id", &Plugin{}},
		{"prefix without leading slash", &Plugin{ID: "x", APIPrefixes: []string{"api/x/"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Panics(t, func() { Register(tc.plugin) })
		})
	}

	Register(&Plugin{ID: "mail", APIPrefixes: []string{"/api/mail/"}})
	assert.Panics(t, func() {
		Register(&Plugin{ID: "mail", APIPrefixes: []string{"/api/mail2/"}})
	}, "duplicate ID must panic rather than silently replacing the first plugin")
}

func TestAllIsSortedByID(t *testing.T) {
	resetRegistry(t)

	// Register out of order on purpose: the order must come from the ID, not
	// from the call sequence (worker start order and /api/features both depend
	// on it being stable).
	Register(&Plugin{ID: "tasks"})
	Register(&Plugin{ID: "forge"})
	Register(&Plugin{ID: "kb"})

	ids := make([]string, 0, 3)
	for _, p := range All() {
		ids = append(ids, p.ID)
	}
	assert.Equal(t, []string{"forge", "kb", "tasks"}, ids)

	// All must hand back a copy: mutating it must not corrupt the registry.
	all := All()
	all[0] = &Plugin{ID: "hacked"}
	assert.Equal(t, "forge", All()[0].ID)
}

func TestLookup(t *testing.T) {
	resetRegistry(t)

	mail := &Plugin{ID: "mail"}
	Register(mail)

	assert.Same(t, mail, Lookup("mail"))
	assert.Nil(t, Lookup("nope"))
}

func TestForRoute(t *testing.T) {
	resetRegistry(t)

	mail := &Plugin{ID: "mail", APIPrefixes: []string{"/api/mail/"}}
	tasks := &Plugin{ID: "tasks", APIPrefixes: []string{"/api/tasks"}}
	stats := &Plugin{ID: "stats", APIPrefixes: []string{"/api/usage/", "/api/git/stats"}}
	git := &Plugin{ID: "git", APIPrefixes: []string{"/api/git/"}}
	Register(mail)
	Register(tasks)
	Register(stats)
	Register(git)

	cases := []struct {
		name string
		path string
		want string // plugin ID, "" = no match
	}{
		{"subtree prefix", "/api/mail/send", "mail"},
		{"exact collection endpoint under subtree prefix", "/api/mail", "mail"},
		{"trailing slash on the request path", "/api/mail/", "mail"},
		{"segment boundary rejects a longer sibling", "/api/mailbox", ""},
		{"exact declaration matches itself", "/api/tasks", "tasks"},
		{"exact declaration matches children", "/api/tasks/42", "tasks"},
		{"longest prefix wins", "/api/git/stats", "stats"},
		{"fallback to the shorter prefix", "/api/git/branch", "git"},
		{"unrelated path", "/api/ai/chat", ""},
		{"root", "/", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ""
			if p := ForRoute(tc.path); p != nil {
				got = p.ID
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDisabledKeyAndStatus(t *testing.T) {
	assert.Equal(t, "ForgeDisabled", (&Plugin{ID: "forge"}).DisabledKey())
	assert.Equal(t, "KbDisabled", (&Plugin{ID: "kb"}).DisabledKey(),
		"derivation only capitalises the first letter; the key must match the locale file")
	assert.Equal(t, "Custom", (&Plugin{ID: "forge", DisabledMsgKey: "Custom"}).DisabledKey())

	assert.Equal(t, http.StatusConflict, (&Plugin{ID: "forge"}).DisabledHTTPStatus())
	assert.Equal(t, http.StatusServiceUnavailable,
		(&Plugin{ID: "terminal", DisabledStatus: http.StatusServiceUnavailable}).DisabledHTTPStatus())
}

func TestIsEnabledAndAvailableDefaultToTrue(t *testing.T) {
	p := &Plugin{ID: "stats"}
	assert.True(t, p.IsEnabled(), "no Enabled func means always on")
	assert.True(t, p.IsAvailable(), "no Available func means always available")

	off := &Plugin{ID: "mail", Enabled: func() bool { return false }}
	assert.False(t, off.IsEnabled())

	// enabled and available are independent axes.
	capped := &Plugin{ID: "terminal", Enabled: func() bool { return true }, Available: func() bool { return false }}
	assert.True(t, capped.IsEnabled())
	assert.False(t, capped.IsAvailable())
}

func TestIsGateExempt(t *testing.T) {
	p := &Plugin{
		ID:         "forge",
		GateExempt: []string{"GET /api/forge/binding", "/api/forge/read/"},
	}
	assert.True(t, p.IsGateExempt(http.MethodGet, "/api/forge/binding"))
	assert.False(t, p.IsGateExempt(http.MethodPost, "/api/forge/binding"),
		"a method-scoped exemption must not let writes through")
	assert.False(t, p.IsGateExempt(http.MethodGet, "/api/forge/binding-extra"),
		"an exact exemption must not act as a prefix")
	assert.False(t, p.IsGateExempt(http.MethodGet, "/api/forge/items"))

	assert.True(t, p.IsGateExempt(http.MethodGet, "/api/forge/read/42"),
		"a trailing slash declares a subtree")
	assert.False(t, p.IsGateExempt(http.MethodGet, "/api/forge/read"),
		"a subtree exemption must not cover the prefix itself")
	assert.True(t, p.IsGateExempt(http.MethodDelete, "/api/forge/read/42"),
		"an entry without a method exempts every method")
}

func TestIsFeatureSwitchPath(t *testing.T) {
	resetRegistry(t)
	assert.False(t, IsFeatureSwitchPath("mail.enabled"), "empty registry: nothing is a feature switch")

	Register(&Plugin{ID: "mail", Section: "mail"})
	Register(&Plugin{ID: "stats"}) // no section: a pure-preference plugin

	assert.True(t, IsFeatureSwitchPath("mail.enabled"))
	assert.False(t, IsFeatureSwitchPath("stats.enabled"),
		"a plugin without a Section has no config switch")
	assert.False(t, IsFeatureSwitchPath("forge.enabled"), "section not registered")
	assert.False(t, IsFeatureSwitchPath("mail.sync_interval_minutes"),
		"only the master switch is derived, not every field of the section")
	assert.False(t, IsFeatureSwitchPath("mail.enabled.extra"))
	assert.False(t, IsFeatureSwitchPath(".enabled"))
	assert.False(t, IsFeatureSwitchPath("enabled"))
	assert.False(t, IsFeatureSwitchPath(""))
}

func TestStartStopAreOptional(t *testing.T) {
	// Nothing calls them here; this pins the shape the worker lifecycle in
	// cmd/server relies on (nil hooks are legal and mean "no background work").
	p := &Plugin{ID: "stats"}
	require.Nil(t, p.Start)
	require.Nil(t, p.Stop)
	require.Nil(t, p.Reconfigure)
	require.Nil(t, p.OnDisable)

	started := false
	withHooks := &Plugin{
		ID:          "mail",
		Start:       func(model.Config) { started = true },
		Stop:        func() {},
		Reconfigure: func(model.Config) {},
		OnDisable:   func() []string { return nil },
	}
	withHooks.Start(model.Config{})
	assert.True(t, started)
}

// TestRegisterPanicsOnConflictingConfigPaths pins the check that moved from
// ConfigPaths() (called on every PATCH) to Register (called once at startup).
//
// Two plugins claiming the same config path means nobody can say which feature
// owns it, so the gate has no honest answer. Failing at startup names both
// plugins; failing inside ConfigPaths() would surface as "saving settings
// randomly 500s" — much harder to trace back to a registration mistake.
func TestRegisterPanicsOnConflictingConfigPaths(t *testing.T) {
	resetRegistry(t)

	Register(&Plugin{ID: "a", ConfigFields: map[string]bool{"shared.field": true}})
	assert.Panics(t, func() {
		Register(&Plugin{ID: "b", ConfigFields: map[string]bool{"shared.field": true}})
	}, "two plugins claiming one config path must panic at Register")

	// A non-conflicting field on a third plugin must still be accepted, and
	// Reset must clear the reverse index (otherwise the next test would inherit
	// these claims).
	Register(&Plugin{ID: "c", ConfigFields: map[string]bool{"other.field": true}})
	Reset()
	Register(&Plugin{ID: "d", ConfigFields: map[string]bool{"shared.field": true}})
}

// TestConfigPaths pins the reverse index built in Register.
//
// ConfigPaths is on the PATCH hot path (handler.IsPatchablePath), and it used to
// rebuild the map from byID on every call — including a duplicate check that
// panicked there instead of at startup. The index now lives in configPathOwner,
// so this test also guards that Register and Reset keep it in sync.
func TestConfigPaths(t *testing.T) {
	resetRegistry(t)

	Register(&Plugin{ID: "a", ConfigFields: map[string]bool{"a.x": true, "a.y": true}})
	Register(&Plugin{ID: "b", ConfigFields: map[string]bool{"b.z": true}})

	got := ConfigPaths()
	require.Len(t, got, 3)
	assert.Equal(t, "a", got["a.x"].ID)
	assert.Equal(t, "a", got["a.y"].ID)
	assert.Equal(t, "b", got["b.z"].ID)
}
