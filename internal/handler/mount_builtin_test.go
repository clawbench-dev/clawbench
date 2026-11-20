package handler

import (
	"net/http"
	"testing"

	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
)

// TestMountBuiltinRoutes_UsesRegistryWhenPopulated pins the production branch.
//
// handler's own tests run with an empty registry and exercise the compile-time
// fallback list. Without this test the branch that actually runs in production
// (iterate plugins.All(), call each MountRoutes) would never execute, so a
// regression there would only show up as "the feature's endpoints all 404" on a
// real server.
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
		"with a populated registry, routes must come from MountRoutes — not from the fallback list")
}
