// Package acptest builds ACP wire values for tests.
//
// The v0.20 SDK keeps free-form values as raw JSON and wraps every optional
// update field in acp.Patch, which makes hand-written fixtures noisy. These
// helpers keep the fixtures readable without changing what they mean: a nil
// map or value builds the unset form, and everything else is encoded as the
// SDK would encode it.
package acptest

import (
	"encoding/json"

	acp "github.com/coder/acp-go-sdk"
)

// Meta builds the _meta of a start frame. A nil map builds a nil Meta.
func Meta(values map[string]any) acp.Meta {
	if values == nil {
		return nil
	}
	if len(values) == 0 {
		// An empty object on the wire decodes to a non-nil empty Meta, which is
		// how a frame with a bag but no keys reaches the client.
		return acp.Meta{}
	}
	m, err := acp.NewMeta(values)
	if err != nil {
		panic(err)
	}
	return m
}

// PatchMeta builds the _meta of an update frame.
func PatchMeta(values map[string]any) acp.Patch[acp.Meta] {
	if values == nil {
		return acp.Patch[acp.Meta]{}
	}
	return acp.PatchSet(Meta(values))
}

// Raw encodes v as JSON for rawInput/rawOutput on a start frame.
func Raw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// PatchRaw encodes v as JSON for rawInput/rawOutput on an update frame.
func PatchRaw(v any) acp.Patch[json.RawMessage] {
	if v == nil {
		return acp.Patch[json.RawMessage]{}
	}
	return acp.PatchSet(Raw(v))
}

// MsgID returns a pointer to an acp.MessageId, for the message-id fields.
func MsgID(s string) *acp.MessageId {
	id := acp.MessageId(s)
	return &id
}

// Patch replaces a stored value, for the optional fields of an update frame.
func Patch[T any](v T) acp.Patch[T] {
	return acp.PatchSet(v)
}
