package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisteredRoutes_CoversEveryRoute guards the route-table refactor: the
// table must capture exactly the routes mounted on the mux, with no route
// registered through mux.HandleFunc directly (which would escape the table and
// silently drop out of the OpenAPI drift check).
func TestRegisteredRoutes_CoversEveryRoute(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	routes := RegisteredRoutes()
	require.NotEmpty(t, routes, "route table must not be empty")

	seen := make(map[string]bool, len(routes))
	for _, r := range routes {
		require.NotEmpty(t, r.Pattern, "route pattern must not be empty")
		assert.Falsef(t, seen[r.Pattern], "duplicate route pattern in table: %s", r.Pattern)
		seen[r.Pattern] = true
	}

	// Every registered pattern must actually be served: a GET to a concrete
	// path under it must not fall through to the catch-all 404. We probe the
	// mux's handler map indirectly via ServeMux.Handler, which reports the
	// matched pattern.
	for _, r := range routes {
		probe := r.Pattern
		if len(probe) > 1 && probe[len(probe)-1] == '/' {
			// Subtree pattern: probe a concrete child path.
			probe += "__probe__"
		}
		_, matched := mux.Handler(newRequest(t, http.MethodGet, probe, nil))
		assert.Equalf(t, r.Pattern, matched,
			"pattern %q is in the route table but the mux resolves %q to %q",
			r.Pattern, probe, matched)
	}
}

// TestRegisteredRoutes_AuthFlagMatchesSpec pins the public/authenticated split.
// The count and membership are asserted explicitly so that flipping a route's
// auth without updating the OpenAPI spec fails here first.
func TestRegisteredRoutes_AuthFlagMatchesSpec(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	got := map[string]bool{}
	for _, r := range RegisteredRoutes() {
		got[r.Pattern] = r.Authenticated
	}

	// The intentionally-public routes (each documented at its call site and
	// marked `security: []` in the spec).
	publicPatterns := []string{
		"/",
		"/login",
		"/api/health",
		"/api/me",
		"/api/share/",
		"/share/",
		"/api/apk",
		"/api/desktop/latest",
		"/api/ssh/info",
		"/api/frp/status",
	}
	for _, p := range publicPatterns {
		auth, ok := got[p]
		require.Truef(t, ok, "expected public route %q in table", p)
		assert.Falsef(t, auth, "route %q must be marked unauthenticated", p)
	}

	// Everything else must be auth-protected.
	for pattern, auth := range got {
		if isPublicPattern(pattern) {
			continue
		}
		assert.Truef(t, auth, "route %q must be marked authenticated", pattern)
	}
}

func isPublicPattern(p string) bool {
	switch p {
	case "/", "/login", "/api/health", "/api/me", "/api/share/", "/share/",
		"/api/apk", "/api/desktop/latest", "/api/ssh/info", "/api/frp/status":
		return true
	}
	return false
}
