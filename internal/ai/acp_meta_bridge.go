package ai

import (
	"encoding/json"

	"github.com/coder/acp-go-sdk"
)

// The v0.20 SDK keeps free-form values as raw JSON: acp.Meta is
// map[string]json.RawMessage, so a number keeps every digit across a round
// trip. The rest of this package works with decoded values, so decode at the
// boundary — a JSON number still reads as float64, exactly as the pre-0.20
// SDK's map[string]any did, and every helper keeps its existing shape.
func MetaAny(m acp.Meta) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, raw := range m {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		out[k] = v
	}
	return out
}

// patchMetaAny is metaAny for update variants, whose _meta is a Patch. ACP v1
// reads a null in an update field as absent, so unset and null both mean "no
// metadata" here.
func PatchMetaAny(p acp.Patch[acp.Meta]) map[string]any {
	m, ok := p.Value()
	if !ok {
		return nil
	}
	return MetaAny(m)
}

// rawJSONValue decodes a raw JSON value into the untyped form the rest of this
// package works with: the pre-0.20 SDK decoded every free-form value before
// handing it over, and the helpers that consume them still expect that shape.
func rawJSONValue(raw json.RawMessage) any {
	var v any
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

// metaStringOf reads a string value from an acp.Meta, returning "" when the
// key is missing or holds something that is not a string.
func metaStringOf(m acp.Meta, key string) string {
	var s string
	if err := m.Get(key, &s); err != nil {
		return ""
	}
	return s
}
