package ai

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Branch-level cases for the elicitation schema shim. The end-to-end cases in
// acp_elicitation_wire_test.go cover the shapes agents actually send; these pin
// the reject paths, because every one of them means "leave the line alone" or
// "drop the property" — a wrong answer there either kills the request with
// -32602 or renders a control the answer cannot be typed into.

func normalizeProp(t *testing.T, in string) (map[string]any, bool) {
	t.Helper()
	raw, ok := normalizeElicitationProperty(json.RawMessage(in))
	if !ok {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("normalized property is not JSON: %s (%v)", raw, err)
	}
	return out, true
}

func TestJSONString(t *testing.T) {
	cases := map[string]string{
		`"x"`:  "x",
		`""`:   "",
		`1`:    "",
		`null`: "",
		``:     "",
	}
	for in, want := range cases {
		if got := jsonString(json.RawMessage(in)); got != want {
			t.Fatalf("jsonString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJSONAllStrings(t *testing.T) {
	cases := map[string]bool{
		`["a","b"]`: true,
		`[]`:        false,
		`["a",1]`:   false,
		`"a"`:       false,
		`bad`:       false,
	}
	for in, want := range cases {
		if got := jsonAllStrings(json.RawMessage(in)); got != want {
			t.Fatalf("jsonAllStrings(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEnumOptionsAllStrings(t *testing.T) {
	cases := map[string]bool{
		`[{"const":"a","title":"A"}]`: true,
		`[]`:                          false,
		`[{"const":""}]`:              false,
		`[{"title":"no const"}]`:      false,
		`bad`:                         false,
	}
	for in, want := range cases {
		if got := enumOptionsAllStrings(json.RawMessage(in)); got != want {
			t.Fatalf("enumOptionsAllStrings(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestScalarType(t *testing.T) {
	cases := []struct {
		in   any
		want string
		ok   bool
	}{
		{"s", "string", true},
		{1.5, "number", true},
		{true, "boolean", true},
		{nil, "", false},
		{map[string]any{}, "", false},
	}
	for _, c := range cases {
		got, ok := scalarType(c.in)
		if got != c.want || ok != c.ok {
			t.Fatalf("scalarType(%v) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestJSONScalarType(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{`"x"`, "string", true},
		{`1`, "number", true},
		{`false`, "boolean", true},
		{`[1,2]`, "number", true},
		{`[1,"a"]`, "", false},
		{`[]`, "", false},
		{`{"a":1}`, "", false},
		{`bad`, "", false},
	}
	for _, c := range cases {
		got, ok := jsonScalarType(json.RawMessage(c.in))
		if got != c.want || ok != c.ok {
			t.Fatalf("jsonScalarType(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestDropMissingRequired(t *testing.T) {
	props := map[string]json.RawMessage{"kept": json.RawMessage(`{}`)}

	if _, ok := dropMissingRequired(json.RawMessage(`"not an array"`), props); ok {
		t.Fatal("a non-array required must be reported as unchanged")
	}
	if _, ok := dropMissingRequired(json.RawMessage(`["kept"]`), props); ok {
		t.Fatal("nothing was dropped, so nothing must change")
	}
	got, ok := dropMissingRequired(json.RawMessage(`["kept","gone"]`), props)
	if !ok {
		t.Fatal("a dropped property must be pruned from required")
	}
	if string(got) != `["kept"]` {
		t.Fatalf("unexpected required: %s", got)
	}
}

func TestNormalizeElicitationProperty_RejectPaths(t *testing.T) {
	rejected := []string{
		`"not an object"`,
		`null`,
		`{}`,                           // no type, nothing to infer from
		`{"type":["null","object"]}`,   // no renderable type in the array
		`{"type":"string","enum":[1]}`, // enum values must be strings
		`{"type":"string","oneOf":[{"title":"x"}]}`, // options need a const
		`{"type":"string","anyOf":[{"title":"x"}]}`,
		`{"type":"array"}`,                           // array without items cannot decode
		`{"type":"array","items":"not an object"}`,   // items must be an object
		`{"type":"array","items":{"type":"number"}}`, // only a string multi-select renders
		`{"type":"array","items":{"type":["null"]}}`, // unrenderable item type array
		`{"type":"array","items":{"type":"string","enum":[1]}}`,
		`{"type":"array","items":{"type":"string","oneOf":[{"title":"x"}]}}`,
		`{"type":"array","items":{"anyOf":[{"title":"x"}]}}`,
		`{"type":"array","items":{"oneOf":[{"title":"x"}]}}`,
		`{"type":"array","items":{"enum":[1]}}`,
		`{"type":"array","items":{}}`,
		`{"items":{"type":["null"]}}`,
		`{"enum":[]}`,
		`{"oneOf":[{"title":"x"}]}`,
		`{"anyOf":[{"title":"x"}]}`,
		`{"default":{"nested":true}}`,
	}
	for _, in := range rejected {
		if _, ok := normalizeElicitationProperty(json.RawMessage(in)); ok {
			t.Fatalf("%s must be dropped, not rendered", in)
		}
	}
}

func TestNormalizeElicitationProperty_AcceptedShapes(t *testing.T) {
	cases := []struct {
		in       string
		wantType string
		check    func(t *testing.T, got map[string]any)
	}{
		{`{"type":"object"}`, "object", nil}, // passed through for the SDK to judge
		{`{"type":["number","integer"]}`, "number", nil},
		{`{"type":"string","oneOf":[{"const":"a"}]}`, "string", nil},
		{`{"type":"string","anyOf":[{"const":"a"}]}`, "string", func(t *testing.T, got map[string]any) {
			if _, ok := got["anyOf"]; ok {
				t.Fatal("anyOf must move to oneOf, which is what the SDK models")
			}
			if _, ok := got["oneOf"]; !ok {
				t.Fatal("the options must survive as oneOf")
			}
		}},
		{`{"type":"array","items":{"type":"string","enum":["a"]}}`, "array", nil},
		{`{"type":"array","items":{"type":"string","oneOf":[{"const":"a"}]}}`, "array", func(t *testing.T, got map[string]any) {
			items := got["items"].(map[string]any)
			if _, ok := items["oneOf"]; ok {
				t.Fatal("a titled multi-select lives on anyOf")
			}
			if _, ok := items["anyOf"]; !ok {
				t.Fatal("the options must survive as anyOf")
			}
			if _, ok := items["type"]; ok {
				t.Fatal("the typed variant cannot carry the type-less anyOf form")
			}
		}},
		{`{"type":"array","items":{"anyOf":[{"const":"a"}]}}`, "array", nil},
		{`{"type":"array","items":{"oneOf":[{"const":"a"}]}}`, "array", func(t *testing.T, got map[string]any) {
			items := got["items"].(map[string]any)
			if _, ok := items["oneOf"]; ok {
				t.Fatal("an untyped item's oneOf must become anyOf")
			}
			if _, ok := items["anyOf"]; !ok {
				t.Fatal("the options must survive as anyOf")
			}
		}},
		{`{"type":"array","items":{"enum":["a"]}}`, "array", func(t *testing.T, got map[string]any) {
			items := got["items"].(map[string]any)
			if items["type"] != "string" {
				t.Fatalf("a string enum implies a string item, got %v", items["type"])
			}
		}},
		{`{"items":{"type":["string"]}}`, "array", nil},
		{`{"enum":["a"]}`, "string", nil},
		{`{"enum":[1]}`, "number", nil},
		{`{"oneOf":[{"const":"a"}]}`, "string", nil},
		{`{"anyOf":[{"const":"a"}]}`, "string", func(t *testing.T, got map[string]any) {
			if _, ok := got["anyOf"]; ok {
				t.Fatal("anyOf must move to oneOf")
			}
		}},
		{`{"default":"x"}`, "string", nil},
		{`{"default":true}`, "boolean", nil},
	}
	for _, c := range cases {
		got, ok := normalizeProp(t, c.in)
		if !ok {
			t.Fatalf("%s must be accepted", c.in)
		}
		if got["type"] != c.wantType {
			t.Fatalf("%s: type = %v, want %s", c.in, got["type"], c.wantType)
		}
		if c.check != nil {
			c.check(t, got)
		}
	}
}

func TestDecodeElicitationEnvelope_RejectPaths(t *testing.T) {
	rejected := []string{
		`not json`,
		`{"method":"other/create","params":{"requestedSchema":{"properties":{}}}}`,
		`{"method":"elicitation/create","params":"not an object"}`,
		`{"method":"elicitation/create","params":{"requestedSchema":"not an object"}}`,
		`{"method":"elicitation/create","params":{"requestedSchema":{}}}`,
		`{"method":"elicitation/create","params":{"requestedSchema":{"properties":null}}}`,
	}
	for _, in := range rejected {
		if _, ok := decodeElicitationEnvelope([]byte(in)); ok {
			t.Fatalf("%s must be rejected", in)
		}
	}
}

// A dropped property must not stay listed in `required`: the SDK validates the
// two against each other and rejects the whole request otherwise.
func TestNormalizeElicitationSchema_PrunesRequiredWithDroppedProperty(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":1,"method":"elicitation/create","params":{` +
		`"message":"pick","requestedSchema":{"type":"object",` +
		`"properties":{"ok":{"type":"string"},"weird":{"enum":[]}},` +
		`"required":["ok","weird"]}}}`)

	out := normalizeElicitationSchema(line)

	var env struct {
		Params struct {
			RequestedSchema struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"requestedSchema"`
		} `json:"params"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("rewritten line is not JSON: %v", err)
	}
	if _, ok := env.Params.RequestedSchema.Properties["weird"]; ok {
		t.Fatal("the unrenderable property must be dropped")
	}
	if len(env.Params.RequestedSchema.Required) != 1 || env.Params.RequestedSchema.Required[0] != "ok" {
		t.Fatalf("required must lose the dropped property, got %v", env.Params.RequestedSchema.Required)
	}
}

func TestNormalizeElicitationSchema_RequiredNotAnArrayIsLeftAlone(t *testing.T) {
	line := []byte(`{"method":"elicitation/create","params":{"requestedSchema":{` +
		`"properties":{"weird":{"type":"object"}},"required":"oops"}}}`)

	out := normalizeElicitationSchema(line)

	var env struct {
		Params struct {
			RequestedSchema struct {
				Required any `json:"required"`
			} `json:"requestedSchema"`
		} `json:"params"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("rewritten line is not JSON: %v", err)
	}
	if env.Params.RequestedSchema.Required != "oops" {
		t.Fatalf("an unparsable required must be left untouched, got %v", env.Params.RequestedSchema.Required)
	}
}

func TestNormalizeElicitationSchema_UnrelatedLinesPassThrough(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"method":"session/update"}`),
		[]byte(`{"method":"elicitation/create"}`),                             // no requestedSchema marker
		[]byte(`{"method":"elicitation/create","params":{"requestedSchema":`), // truncated
	}
	for _, in := range cases {
		out := normalizeElicitationSchema(in)
		if !bytes.Equal(out, in) {
			t.Fatalf("%s must pass through unchanged, got %s", in, out)
		}
	}
}

func TestNormalizeElicitationSchema_UnchangedPropertiesKeepTheLine(t *testing.T) {
	// Every property already decodes, so rewriting would only reorder keys.
	line := []byte(`{"method":"elicitation/create","params":{"requestedSchema":{` +
		`"properties":{"a":{"type":"string"}}}}}`)

	out := normalizeElicitationSchema(line)
	if !bytes.Equal(out, line) {
		t.Fatalf("a line that needs no change must not be rewritten:\n in: %s\nout: %s", line, out)
	}
}
