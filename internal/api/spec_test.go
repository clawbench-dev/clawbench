package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestPropertySchema_DecodesScalarFacets covers the accessor that feeds body
// field rendering: the caller needs the field's type, enum and description, and
// previously had no way to get the last two at all.
func TestPropertySchema_DecodesScalarFacets(t *testing.T) {
	const doc = `
type: object
properties:
  count:
    type: integer
    description: how many
  mode:
    type: string
    enum: [a, b]
  nested:
    type: object
    properties:
      inner:
        type: string
  typeless:
    properties:
      inner:
        type: string
`
	var sc schema
	require.NoError(t, yaml.Unmarshal([]byte(doc), &sc))

	count := sc.propertySchema("count")
	require.NotNil(t, count)
	assert.Equal(t, "integer", count.Type)
	assert.Equal(t, "how many", count.Description)

	mode := sc.propertySchema("mode")
	require.NotNil(t, mode)
	assert.Equal(t, []string{"a", "b"}, mode.enumValues())

	// A nested object may declare its type explicitly...
	nested := sc.propertySchema("nested")
	require.NotNil(t, nested)
	assert.Equal(t, "object", nested.Type)

	// ...or omit it, leaving only its own properties to identify it. The
	// renderer labels that case "object" itself.
	typeless := sc.propertySchema("typeless")
	require.NotNil(t, typeless)
	assert.Equal(t, "", typeless.Type)
	assert.Equal(t, yaml.MappingNode, typeless.Properties.Kind)

	// An absent property yields nil rather than an empty schema, so the caller
	// can tell "not declared" from "declared with no facets".
	assert.Nil(t, sc.propertySchema("missing"))
}

// TestPropertySchema_WithoutProperties guards the lookup on a schema that
// declares no properties (e.g. a scalar body). It must return nil, not panic.
func TestPropertySchema_WithoutProperties(t *testing.T) {
	sc := &schema{Type: "string"}
	assert.Nil(t, sc.propertySchema("anything"))

	names, required := sc.propertyNames()
	assert.Empty(t, names)
	assert.Empty(t, required)
}

// TestDisplayType covers the label shown next to a parameter or body field.
// A nested object frequently omits its type, and previously still rendered as
// "object" because the old accessor synthesised it; that behaviour has to
// survive the refactor or the field list silently loses the annotation.
func TestDisplayType(t *testing.T) {
	assert.Equal(t, "", (*schema)(nil).displayType(), "nil must not panic")

	assert.Equal(t, "integer", (&schema{Type: "integer"}).displayType())
	assert.Equal(t, "string", (&schema{Type: "string"}).displayType())

	// A schema with neither a type nor properties has nothing to show, so the
	// label stays bare rather than inventing one.
	assert.Equal(t, "", (&schema{}).displayType())

	var withProps schema
	require.NoError(t, yaml.Unmarshal([]byte("properties:\n  inner:\n    type: string\n"), &withProps))
	assert.Equal(t, "object", withProps.displayType(),
		"a typeless nested object must still be labelled")

	// An explicit type wins over the properties-derived label.
	typed := &schema{Type: "array", Properties: withProps.Properties}
	assert.Equal(t, "array", typed.displayType())
}
