package handler

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"clawbench/internal/api"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenAPISpecMatchesRegisteredRoutes is the drift guard between the OpenAPI
// document (internal/api/openapi.yaml) and the live mux. It compares in both
// directions:
//
//   - a spec path with no registered route would document an endpoint that
//     returns 404 (the AI would be told to call something that does not exist);
//   - a registered /api/ route missing from the spec is undocumented surface
//     that the AI cannot discover and reviewers cannot see.
//
// Subtree patterns (a ServeMux pattern ending in "/") are matched against spec
// path templates by prefix, since ServeMux subtree registration is how this
// codebase serves parameterised paths.
func TestOpenAPISpecMatchesRegisteredRoutes(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	specRoutes, err := api.SpecRoutes()
	require.NoError(t, err, "embedded OpenAPI spec must parse")
	require.NotEmpty(t, specRoutes)

	registered := RegisteredRoutes()

	// Index the route table for prefix lookups. The "/" catch-all (ServeIndex)
	// is excluded: it is the SPA fallback, not an API route, and as a subtree
	// pattern it would "cover" every path and make this check vacuous.
	exact := map[string]bool{}
	var subtrees []string
	for _, r := range registered {
		if r.Pattern == "/" {
			continue
		}
		if strings.HasSuffix(r.Pattern, "/") {
			subtrees = append(subtrees, r.Pattern)
			continue
		}
		exact[r.Pattern] = true
	}
	sort.Strings(subtrees)

	// Spec path templates such as /api/tasks/{id} must resolve to a route.
	// Non-/api/ paths (the share SPA page, the login page) are matched exactly.
	var missing []string
	for _, sr := range specRoutes {
		if exact[sr.Path] {
			continue
		}
		// Strip the template suffix down to its static prefix and look for a
		// subtree registration covering it.
		if coveredBySubtree(sr.Path, subtrees) {
			continue
		}
		missing = append(missing, sr.Method+" "+sr.Path+" ("+sr.OperationID+")")
	}
	assert.Emptyf(t, missing,
		"spec declares routes that are not registered (they would 404):\n  %s",
		strings.Join(missing, "\n  "))

	// Registered /api/ routes must be documented. The /assets/, /css/ and /js/
	// static mounts bypass the route table entirely, so they are not compared.
	var undocumented []string
	for _, r := range registered {
		if !strings.HasPrefix(r.Pattern, "/api/") {
			continue
		}
		if exactCoveredBySpec(r.Pattern, specRoutes) {
			continue
		}
		undocumented = append(undocumented, r.Pattern)
	}
	assert.Emptyf(t, undocumented,
		"registered /api/ routes are missing from the OpenAPI spec:\n  %s",
		strings.Join(undocumented, "\n  "))
}

// coveredBySubtree reports whether any ServeMux subtree pattern covers path.
// "/api/tasks/{id}" is covered by the "/api/tasks/" subtree.
func coveredBySubtree(path string, subtrees []string) bool {
	static := path
	if i := strings.IndexByte(path, '{'); i >= 0 {
		static = path[:i]
	}
	for _, sub := range subtrees {
		// A subtree pattern matches everything below its prefix.
		if strings.HasPrefix(static, sub) {
			return true
		}
	}
	return false
}

// exactCoveredBySpec reports whether a registered pattern is represented in the
// spec, either literally or by a path template sharing its static prefix.
func exactCoveredBySpec(pattern string, specRoutes []api.SpecRoute) bool {
	for _, sr := range specRoutes {
		if sr.Path == pattern {
			return true
		}
		if strings.HasSuffix(pattern, "/") {
			// Subtree registration covers every template below it.
			if strings.HasPrefix(sr.Path, pattern) {
				return true
			}
		}
	}
	return false
}

// TestOpenAPISpecAuthMatchesRoutes pins the public/authenticated split against
// the spec's `security: []` declarations. A route flipped to public without a
// spec update (or vice versa) fails here.
func TestOpenAPISpecAuthMatchesRoutes(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)

	specRoutes, err := api.SpecRoutes()
	require.NoError(t, err)

	routes := RegisteredRoutes()
	for _, sr := range specRoutes {
		if !strings.HasPrefix(sr.Path, "/api/") {
			continue // SPA pages are outside the API auth contract
		}
		pattern, authenticated, ok := routeForSpecPath(sr.Path, routes)
		if !ok {
			// Undocumented/unsupported paths are reported by the sibling test.
			continue
		}
		assert.Equalf(t, sr.Public, !authenticated,
			"auth mismatch for %s %s: spec public=%v but route %q authenticated=%v",
			sr.Method, sr.Path, sr.Public, pattern, authenticated)
	}
}

// routeForSpecPath finds the registered route that serves a spec path template,
// applying exact ServeMux semantics:
//
//   - an exact pattern matches only the identical path;
//   - a subtree pattern (trailing "/") matches anything below its prefix.
//
// A plain prefix match would be wrong: the authenticated "/api/share" must not
// be considered the route for the public "/api/share/{token}/..." template —
// that template is served by the "/api/share/" subtree. The longest match wins
// so a subtree never shadows a more specific sibling.
func routeForSpecPath(specPath string, routes []Route) (pattern string, authenticated, ok bool) {
	for _, r := range routes {
		if r.Pattern == "/" {
			continue // SPA catch-all, not an API route
		}
		var hit bool
		if strings.HasSuffix(r.Pattern, "/") {
			hit = strings.HasPrefix(specPath, r.Pattern)
		} else {
			hit = specPath == r.Pattern
		}
		if hit && len(r.Pattern) > len(pattern) {
			pattern, authenticated, ok = r.Pattern, r.Authenticated, true
		}
	}
	return pattern, authenticated, ok
}
