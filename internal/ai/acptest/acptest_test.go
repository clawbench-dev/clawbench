package acptest

import (
	"encoding/json"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

// These helpers exist so fixtures read like the wire shape they describe. The
// cases below pin the one distinction that matters: nil means "field absent",
// while an empty map means "present but empty" — the two are different frames.

func TestMeta(t *testing.T) {
	if got := Meta(nil); got != nil {
		t.Fatalf("nil map must build a nil Meta, got %#v", got)
	}

	empty := Meta(map[string]any{})
	if empty == nil {
		t.Fatal("an empty map must build a non-nil, empty Meta")
	}

	m := Meta(map[string]any{"k": "v"})
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"k":"v"}` {
		t.Fatalf("unexpected encoding: %s", raw)
	}
}

func TestPatchMeta(t *testing.T) {
	if _, ok := PatchMeta(nil).Value(); ok {
		t.Fatal("nil map must leave the patch unset")
	}
	v, ok := PatchMeta(map[string]any{"k": 1}).Value()
	if !ok || v == nil {
		t.Fatalf("a non-nil map must set the patch, got ok=%v", ok)
	}
}

func TestRaw(t *testing.T) {
	if got := Raw(nil); got != nil {
		t.Fatalf("nil must encode to nil, got %s", got)
	}
	got := Raw(map[string]any{"a": 1})
	if string(got) != `{"a":1}` {
		t.Fatalf("unexpected encoding: %s", got)
	}
}

func TestPatchRaw(t *testing.T) {
	if _, ok := PatchRaw(nil).Value(); ok {
		t.Fatal("nil must leave the patch unset")
	}
	v, ok := PatchRaw([]int{1, 2}).Value()
	if !ok || string(v) != "[1,2]" {
		t.Fatalf("unexpected patch: ok=%v value=%s", ok, v)
	}
}

func TestMsgID(t *testing.T) {
	id := MsgID("m1")
	if id == nil || *id != acp.MessageId("m1") {
		t.Fatalf("unexpected message id: %#v", id)
	}
}

func TestPatch(t *testing.T) {
	v, ok := Patch("x").Value()
	if !ok || v != "x" {
		t.Fatalf("unexpected patch: ok=%v value=%q", ok, v)
	}
}
