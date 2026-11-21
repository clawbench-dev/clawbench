package handler

import (
	"net/http"
	"testing"

	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
)

// TestMountBuiltinRoutes_UsesRegistryWhenPopulated pins the production path:
// routes come from each plugin's own MountRoutes.
//
// This is the only path. Without the test, a regression here would only show up
// as "every feature endpoint 404s" on a real server.
func TestMountBuiltinRoutes_UsesRegistryWhenPopulated(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	var mounted []string
	plugins.Register(&plugins.Plugin{
		ID:          "probe",
		APIPrefixes: []string{"/api/probe/"},
		MountRoutes: func(m plugins.Mount) {
			m.Auth("/api/probe/x", func(http.ResponseWriter, *http.Request) {})
		},
	})

	m := plugins.Mount{
		Auth:   func(p string, _ http.HandlerFunc) { mounted = append(mounted, p) },
		Public: func(p string, _ http.HandlerFunc) { mounted = append(mounted, p) },
	}
	mountBuiltinRoutes(m)

	assert.Equal(t, []string{"/api/probe/x"}, mounted,
		"routes must come from the registered plugin's MountRoutes")
}

// TestMountBuiltinRoutes_EmptyRegistryMountsNothing pins the deliberate
// no-fallback rule.
//
// handler's own tests run with an empty registry and therefore see only the core
// routes. A fallback list would have to import the plugin packages, which would
// make "delete internal/plugin/<id>" break handler's compilation — the exact
// opposite of the goal.
func TestMountBuiltinRoutes_EmptyRegistryMountsNothing(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	var mounted []string
	m := plugins.Mount{
		Auth:   func(p string, _ http.HandlerFunc) { mounted = append(mounted, p) },
		Public: func(p string, _ http.HandlerFunc) { mounted = append(mounted, p) },
	}
	mountBuiltinRoutes(m)

	assert.Empty(t, mounted, "an empty registry must mount no plugin routes")
}
