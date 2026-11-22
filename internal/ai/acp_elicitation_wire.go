package ai

import (
	"bytes"
	"encoding/json"
	"log/slog"
)

// normalizeElicitationSchema rewrites an agent's `elicitation/create` request so
// the typed SDK can decode it.
//
// JSON Schema makes `type` optional and lets an agent imply it from `enum`,
// `oneOf`/`anyOf` or `items`. The SDK's property union requires it: one
// property without `type` (or a `null` property) fails the whole request with
// -32602, so the question card never appears. Infer the unambiguous cases, and
// drop the properties the card cannot render anyway instead of failing the
// request.
//
// This is a wire-level compatibility shim next to fixStringNumericID: it runs
// before the SDK sees the line, only for `elicitation/create`.
func normalizeElicitationSchema(line []byte) []byte {
	if !bytes.Contains(line, []byte(`"elicitation/create"`)) ||
		!bytes.Contains(line, []byte(`"requestedSchema"`)) {
		return line
	}
	env, ok := decodeElicitationEnvelope(line)
	if !ok {
		return line
	}
	if !normalizeElicitationProperties(env.properties) {
		return line
	}
	if rawRequired, ok := env.schema["required"]; ok {
		if fixed, ok := dropMissingRequired(rawRequired, env.properties); ok {
			env.schema["required"] = fixed
		}
	}
	out, err := rewriteElicitationEnvelope(env)
	if err != nil {
		return line
	}
	return out
}

// elicitationEnvelope is the decoded chain a request has to walk before the
// property map can be normalized: envelope → params → requestedSchema →
// properties. Each level is kept raw so unknown fields survive the rewrite.
type elicitationEnvelope struct {
	envelope   map[string]json.RawMessage
	params     map[string]json.RawMessage
	schema     map[string]json.RawMessage
	properties map[string]json.RawMessage
}

func decodeElicitationEnvelope(line []byte) (elicitationEnvelope, bool) {
	var env elicitationEnvelope
	if json.Unmarshal(line, &env.envelope) != nil {
		return env, false
	}
	if jsonString(env.envelope["method"]) != "elicitation/create" {
		return env, false
	}
	if json.Unmarshal(env.envelope["params"], &env.params) != nil {
		return env, false
	}
	if json.Unmarshal(env.params["requestedSchema"], &env.schema) != nil {
		return env, false
	}
	if json.Unmarshal(env.schema["properties"], &env.properties) != nil ||
		env.properties == nil {
		return env, false
	}
	return env, true
}

// normalizeElicitationProperties rewrites each property in place and reports
// whether anything changed.
func normalizeElicitationProperties(properties map[string]json.RawMessage) bool {
	changed := false
	for name, raw := range properties {
		fixed, ok := normalizeElicitationProperty(raw)
		if !ok {
			slog.Debug("acp: dropping elicitation property the card cannot render",
				"property", name)
			delete(properties, name)
			changed = true
			continue
		}
		if !bytes.Equal(fixed, raw) {
			properties[name] = fixed
			changed = true
		}
	}
	return changed
}

func rewriteElicitationEnvelope(env elicitationEnvelope) ([]byte, error) {
	propertiesJSON, err := json.Marshal(env.properties)
	if err != nil {
		return nil, err
	}
	env.schema["properties"] = propertiesJSON
	schemaJSON, err := json.Marshal(env.schema)
	if err != nil {
		return nil, err
	}
	env.params["requestedSchema"] = schemaJSON
	paramsJSON, err := json.Marshal(env.params)
	if err != nil {
		return nil, err
	}
	env.envelope["params"] = paramsJSON
	return json.Marshal(env.envelope)
}

// normalizeElicitationProperty returns the property as the SDK can decode it,
// or false when the card cannot render it and the property should be dropped.
func normalizeElicitationProperty(raw json.RawMessage) (json.RawMessage, bool) {
	var prop map[string]json.RawMessage
	if err := json.Unmarshal(raw, &prop); err != nil || prop == nil {
		return nil, false
	}
	switch typ := jsonString(prop["type"]); typ {
	case "string":
		return normalizeStringProperty(raw, prop)
	case "array":
		return normalizeArrayProperty(raw, prop)
	case "":
		return inferElicitationProperty(prop)
	default:
		return raw, true
	}
}

// normalizeStringProperty keeps a string property's options in the fields the
// SDK models. `anyOf` is not modeled there; the card reads either field, so
// moving it to `oneOf` preserves the options across the typed round trip.
func normalizeStringProperty(raw json.RawMessage, prop map[string]json.RawMessage) (json.RawMessage, bool) {
	if enum, ok := prop["enum"]; ok && !jsonAllStrings(enum) {
		return nil, false
	}
	if oneOf, ok := prop["oneOf"]; ok {
		if !enumOptionsAllStrings(oneOf) {
			return nil, false
		}
		return raw, true
	}
	if anyOf, ok := prop["anyOf"]; ok {
		if !enumOptionsAllStrings(anyOf) {
			return nil, false
		}
		prop["oneOf"] = anyOf
		delete(prop, "anyOf")
		return marshalJSON(prop)
	}
	return raw, true
}

// normalizeArrayProperty normalizes the `items` union the SDK models. An array
// without items cannot be decoded; a titled multi-select must use `anyOf`.
func normalizeArrayProperty(raw json.RawMessage, prop map[string]json.RawMessage) (json.RawMessage, bool) {
	items, ok := prop["items"]
	if !ok {
		return nil, false
	}
	fixed, ok := normalizeArrayItems(items)
	if !ok {
		return nil, false
	}
	if bytes.Equal(fixed, items) {
		return raw, true
	}
	prop["items"] = fixed
	return marshalJSON(prop)
}

func normalizeArrayItems(raw json.RawMessage) (json.RawMessage, bool) {
	var items map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, false
	}
	switch typ := jsonString(items["type"]); typ {
	case "string":
		// MultiSelectItemsString models enum, not oneOf/anyOf. Titled options
		// are modeled by the type-less anyOf variant, so drop `type` when
		// converting.
		if oneOf, ok := items["oneOf"]; ok {
			if !enumOptionsAllStrings(oneOf) {
				return nil, false
			}
			items["anyOf"] = oneOf
			delete(items, "oneOf")
			delete(items, "type")
			return marshalJSON(items)
		}
		if enum, ok := items["enum"]; ok && !jsonAllStrings(enum) {
			return nil, false
		}
		return marshalJSON(items)
	case "":
		if anyOf, ok := items["anyOf"]; ok {
			if !enumOptionsAllStrings(anyOf) {
				return nil, false
			}
			return marshalJSON(items)
		}
		if oneOf, ok := items["oneOf"]; ok {
			if !enumOptionsAllStrings(oneOf) {
				return nil, false
			}
			items["anyOf"] = oneOf
			delete(items, "oneOf")
			return marshalJSON(items)
		}
		if enum, ok := items["enum"]; ok {
			if !jsonAllStrings(enum) {
				return nil, false
			}
			items["type"] = json.RawMessage(`"string"`)
			return marshalJSON(items)
		}
		return nil, false
	default:
		// The card only has a string multi-select. A numeric/boolean/custom
		// item type would decode but render an unanswerable question, so drop
		// it with the other unrenderable properties.
		return nil, false
	}
}

// inferElicitationProperty fills in a missing `type` from the property's own
// shape. Anything ambiguous is dropped: guessing would render the wrong
// control and send an answer the agent rejects.
func inferElicitationProperty(prop map[string]json.RawMessage) (json.RawMessage, bool) {
	if items, ok := prop["items"]; ok {
		fixed, ok := normalizeArrayItems(items)
		if !ok {
			return nil, false
		}
		prop["items"] = fixed
		prop["type"] = json.RawMessage(`"array"`)
		return marshalJSON(prop)
	}
	if enum, ok := prop["enum"]; ok {
		typ, ok := jsonScalarType(enum)
		if !ok {
			return nil, false
		}
		prop["type"] = json.RawMessage(`"` + typ + `"`)
		return marshalJSON(prop)
	}
	if oneOf, ok := prop["oneOf"]; ok {
		if !enumOptionsAllStrings(oneOf) {
			return nil, false
		}
		prop["type"] = json.RawMessage(`"string"`)
		return marshalJSON(prop)
	}
	if anyOf, ok := prop["anyOf"]; ok {
		if !enumOptionsAllStrings(anyOf) {
			return nil, false
		}
		prop["type"] = json.RawMessage(`"string"`)
		prop["oneOf"] = anyOf
		delete(prop, "anyOf")
		return marshalJSON(prop)
	}
	if def, ok := prop["default"]; ok {
		typ, ok := jsonScalarType(def)
		if !ok {
			return nil, false
		}
		prop["type"] = json.RawMessage(`"` + typ + `"`)
		return marshalJSON(prop)
	}
	return nil, false
}

// dropMissingRequired removes required entries whose property was dropped.
func dropMissingRequired(raw json.RawMessage, properties map[string]json.RawMessage) (json.RawMessage, bool) {
	var required []string
	if err := json.Unmarshal(raw, &required); err != nil {
		return nil, false
	}
	kept := required[:0]
	for _, name := range required {
		if _, ok := properties[name]; ok {
			kept = append(kept, name)
		}
	}
	if len(kept) == len(required) {
		return nil, false
	}
	fixed, err := json.Marshal(kept)
	if err != nil {
		return nil, false
	}
	return fixed, true
}

// jsonString reads a JSON string, returning "" for absent, null or non-string.
func jsonString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// jsonAllStrings reports whether raw is a non-empty array of strings.
func jsonAllStrings(raw json.RawMessage) bool {
	var values []any
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return false
	}
	for _, v := range values {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// enumOptionsAllStrings reports whether raw is a non-empty array of
// {const: string, ...} entries.
func enumOptionsAllStrings(raw json.RawMessage) bool {
	var entries []map[string]json.RawMessage
	if json.Unmarshal(raw, &entries) != nil || len(entries) == 0 {
		return false
	}
	for _, entry := range entries {
		if jsonString(entry["const"]) == "" {
			return false
		}
	}
	return true
}

// jsonScalarType infers the JSON Schema type of a scalar or a homogeneous
// array of scalars.
func jsonScalarType(raw json.RawMessage) (string, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	if values, ok := v.([]any); ok {
		if len(values) == 0 {
			return "", false
		}
		typ := ""
		for _, item := range values {
			itemType, ok := scalarType(item)
			if !ok || (typ != "" && typ != itemType) {
				return "", false
			}
			typ = itemType
		}
		return typ, true
	}
	return scalarType(v)
}

func scalarType(v any) (string, bool) {
	switch v.(type) {
	case string:
		return "string", true
	case float64:
		return "number", true
	case bool:
		return "boolean", true
	default:
		return "", false
	}
}

// marshalJSON is json.Marshal in the (value, ok) shape the normalizers use.
func marshalJSON(v any) (json.RawMessage, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return b, true
}
