package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestSpecRoutes_ReturnsSortedUniquePairs covers the accessor the drift guard
// depends on: every path+method pair in the embedded spec, sorted, with the
// security declaration surfaced so a public endpoint can be told apart.
func TestSpecRoutes_ReturnsSortedUniquePairs(t *testing.T) {
	routes, err := SpecRoutes()
	require.NoError(t, err, "the embedded spec must parse")
	require.NotEmpty(t, routes)

	seen := map[string]bool{}
	for _, r := range routes {
		require.NotEmpty(t, r.Path)
		require.NotEmpty(t, r.Method)
		require.Equal(t, r.Method, upper(r.Method), "methods are upper-cased: %s", r.Method)
		key := r.Method + " " + r.Path
		assert.False(t, seen[key], "duplicate route %s", key)
		seen[key] = true
	}

	// Sorted by path, then method, so callers get a stable order.
	for i := 1; i < len(routes); i++ {
		prev, cur := routes[i-1], routes[i]
		if prev.Path == cur.Path {
			assert.LessOrEqual(t, prev.Method, cur.Method, "same path must sort by method")
		} else {
			assert.Less(t, prev.Path, cur.Path, "paths must be sorted")
		}
	}

	// The spec must declare at least one public (security: []) endpoint, or the
	// Public flag would be dead and the auth drift check vacuous.
	var public int
	for _, r := range routes {
		if r.Public {
			public++
		}
	}
	assert.NotZero(t, public, "at least one endpoint is declared public")
}

// upper is a tiny ASCII helper so the test does not import strings for one call.
func upper(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'a' && out[i] <= 'z' {
			out[i] -= 'a' - 'A'
		}
	}
	return string(out)
}

// TestResolve_DereferencesComponentParameter covers $ref resolution: a parameter
// declared as a reference is replaced by its component definition, and a
// non-reference (or a dangling ref) is returned untouched.
func TestResolve_DereferencesComponentParameter(t *testing.T) {
	var s spec
	require.NoError(t, yaml.Unmarshal([]byte(`
components:
  parameters:
    SessionId:
      name: session_id
      in: query
      required: true
      description: the session
paths: {}
`), &s))

	got := s.resolve(parameter{Ref: "#/components/parameters/SessionId"})
	assert.Equal(t, "session_id", got.Name)
	assert.Equal(t, "query", got.In)
	assert.True(t, got.Required)

	// A plain parameter is returned as-is.
	plain := parameter{Name: "page", In: "query"}
	assert.Equal(t, plain, s.resolve(plain))

	// A ref to a missing component is not silently replaced by an empty value.
	dangling := parameter{Ref: "#/components/parameters/Nope"}
	assert.Equal(t, dangling, s.resolve(dangling))
}

// TestHasAnyTag covers the tag-intersection helper used to keep an operation
// selection tied to the spec's own grouping.
func TestHasAnyTag(t *testing.T) {
	want := map[string]bool{"rag": true, "task": true}
	assert.True(t, hasAnyTag([]string{"session", "rag"}, want))
	assert.False(t, hasAnyTag([]string{"session", "agent"}, want))
	assert.False(t, hasAnyTag(nil, want), "no tags never intersects")
	assert.False(t, hasAnyTag([]string{"rag"}, map[string]bool{}), "an empty filter matches nothing")
}

// TestPropertyNames covers the ordered field extraction and the required set.
func TestPropertyNames(t *testing.T) {
	var sc schema
	require.NoError(t, yaml.Unmarshal([]byte(`
type: object
required: [b]
properties:
  a:
    type: string
  b:
    type: integer
`), &sc))

	names, required := sc.propertyNames()
	assert.Equal(t, []string{"a", "b"}, names, "field order must follow the document")
	assert.True(t, required["b"])
	assert.False(t, required["a"])
}

// TestEmbeddedSpec_StructuralIntegrity guards the two mistakes that parse fine
// but produce a broken document: a schema block mis-indented under `paths`
// (parses as a bogus path item, and its $ref no longer resolves), and a $ref
// that points at a component that does not exist.
//
// Neither is caught elsewhere: SpecRoutes iterates paths by method, so a
// schemaless bogus path item is silently dropped, and the handler drift guard
// only compares path+method pairs.
func TestEmbeddedSpec_StructuralIntegrity(t *testing.T) {
	var doc struct {
		Paths      map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(specYAML, &doc))

	for key := range doc.Paths {
		assert.True(t, strings.HasPrefix(key, "/"),
			"paths must only contain path items (keys starting with /); %q is a stray block "+
				"mis-indented into `paths` instead of `components.schemas`", key)
	}

	// Every local schema $ref must resolve. Collect them by walking the raw
	// document so a ref nested inside a component is checked too.
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(specYAML, &root))
	var dangling []string
	var walk func(*yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "$ref" {
					ref := n.Content[i+1].Value
					const prefix = "#/components/schemas/"
					if strings.HasPrefix(ref, prefix) {
						if _, ok := doc.Components.Schemas[strings.TrimPrefix(ref, prefix)]; !ok {
							dangling = append(dangling, ref)
						}
					}
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&root)
	assert.Empty(t, dangling, "every $ref must point at a defined component schema")
}
