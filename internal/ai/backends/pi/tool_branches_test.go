package pi

import (
	"encoding/json"
	"testing"
)

// The CLI path and the ACP path must produce byte-identical input for the same
// arguments, so the branches below are the ones that decide whether an edit
// renders as a diff at all.
func TestNormalizePiToolInput_EmptyInput(t *testing.T) {
	if got := normalizePiToolInput("read", nil, nil); got != "{}" {
		t.Fatalf("empty input must normalize to an empty object, got %q", got)
	}
}

func TestNormalizePiToolInput_RemapsPathForReadAndWrite(t *testing.T) {
	for _, tool := range []string{"read", "write"} {
		got := normalizePiToolInput(tool, json.RawMessage(`{"path":"a.txt"}`), nil)
		var out map[string]any
		if err := json.Unmarshal([]byte(got), &out); err != nil {
			t.Fatalf("%s: not JSON: %s", tool, got)
		}
		if out["file_path"] != "a.txt" {
			t.Fatalf("%s: path must be remapped, got %v", tool, out)
		}
		if _, ok := out["path"]; ok {
			t.Fatalf("%s: the old key must be gone, got %v", tool, out)
		}
	}
}

func TestNormalizePiToolInput_BashGetsNoPathRemap(t *testing.T) {
	got := normalizePiToolInput("bash", json.RawMessage(`{"command":"ls","path":"x"}`), map[string]string{"cwd": "dir"})
	var out map[string]any
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("not JSON: %s", got)
	}
	if out["path"] != "x" {
		t.Fatalf("bash must not remap path, got %v", out)
	}
	if out["dir"] != nil && out["cwd"] != nil {
		t.Fatalf("the caller's base remaps must still apply, got %v", out)
	}
}

// The caller's map is read-only for us: mutating it would corrupt the next call.
func TestNormalizePiToolInput_DoesNotMutateBaseRemaps(t *testing.T) {
	base := map[string]string{"cwd": "dir"}
	normalizePiToolInput("read", json.RawMessage(`{"path":"a"}`), base)
	if _, ok := base["path"]; ok {
		t.Fatalf("base remaps were mutated: %v", base)
	}
}

func TestNormalizePiToolInput_InvalidJSONIsPassedThrough(t *testing.T) {
	raw := json.RawMessage(`{not json`)
	if got := normalizePiToolInput("read", raw, nil); got != string(raw) {
		t.Fatalf("invalid input must be returned untouched, got %q", got)
	}
}

func TestNormalizePiEditInput_InvalidJSONIsPassedThrough(t *testing.T) {
	raw := json.RawMessage(`{not json`)
	if got := NormalizePiEditInput(raw, nil); got != string(raw) {
		t.Fatalf("invalid input must be returned untouched, got %q", got)
	}
}

func TestNormalizePiEditInput_PromotesFirstNestedEdit(t *testing.T) {
	got := NormalizePiEditInput(
		json.RawMessage(`{"path":"a.txt","edits":[{"oldText":"beta","newText":"BETA"}]}`),
		map[string]string{"path": "file_path"},
	)
	var out map[string]any
	if err := json.Unmarshal([]byte(got), &out); err != nil {
		t.Fatalf("not JSON: %s", got)
	}
	if out["old_string"] != "beta" || out["new_string"] != "BETA" {
		t.Fatalf("the first edit must be promoted to the top level, got %v", out)
	}
	if out["file_path"] != "a.txt" {
		t.Fatalf("path must be remapped, got %v", out)
	}
}

func TestPromoteFirstEditToTopLevel(t *testing.T) {
	t.Run("no edits array", func(t *testing.T) {
		in := map[string]any{"old_string": "kept"}
		promoteFirstEditToTopLevel(in)
		if in["old_string"] != "kept" {
			t.Fatalf("unrelated input was changed: %v", in)
		}
	})

	t.Run("empty edits array", func(t *testing.T) {
		in := map[string]any{"edits": []any{}}
		promoteFirstEditToTopLevel(in)
		if _, ok := in["old_string"]; ok {
			t.Fatalf("nothing to promote, got %v", in)
		}
	})

	t.Run("first edit is not an object", func(t *testing.T) {
		in := map[string]any{"edits": []any{"nope"}}
		promoteFirstEditToTopLevel(in)
		if _, ok := in["old_string"]; ok {
			t.Fatalf("nothing to promote, got %v", in)
		}
	})

	t.Run("flat fields win", func(t *testing.T) {
		in := map[string]any{
			"old_string": "flat",
			"edits":      []any{map[string]any{"old_string": "nested"}},
		}
		promoteFirstEditToTopLevel(in)
		if in["old_string"] != "flat" {
			t.Fatalf("the tool's own flat field must win, got %v", in)
		}
	})
}
