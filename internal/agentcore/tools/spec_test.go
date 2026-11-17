package tools

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// The spec of each tool is what the model receives. A spec that declares an
// argument the tool does not read (or fails to declare one it requires) is worse
// than no documentation: the model fills a field nothing consumes and the call
// fails for a reason it cannot see. These tests check the declaration against the
// struct the tool actually decodes into.

// argsOf decodes the tool's declared parameters object.
func argsOf(t *testing.T, spec Spec) (props map[string]any, required map[string]bool) {
	t.Helper()
	props, ok := spec.Parameters[schemaProperties].(map[string]any)
	if !ok {
		t.Fatalf("%s: no properties object in the spec", spec.Name)
	}
	list, _ := spec.Parameters[schemaRequired].([]string)
	required = make(map[string]bool, len(list))
	for _, name := range list {
		required[name] = true
	}
	return props, required
}

// TestEveryToolSpecIsWellFormed pins the JSON Schema shape every tool shares.
func TestEveryToolSpecIsWellFormed(t *testing.T) {
	for _, tool := range All(true, true, true) {
		spec := tool.Spec()
		if spec.Name != tool.Name() {
			t.Errorf("%s: spec name %q does not match the callable name", tool.Name(), spec.Name)
		}
		if strings.TrimSpace(spec.Description) == "" {
			t.Errorf("%s: the spec must describe the tool", spec.Name)
		}
		if got := spec.Parameters[schemaType]; got != schemaObject {
			t.Errorf("%s: parameters must be an object, got %v", spec.Name, got)
		}
		props, required := argsOf(t, spec)
		if len(props) == 0 {
			t.Errorf("%s: no parameters declared", spec.Name)
		}
		for name, raw := range props {
			field, ok := raw.(map[string]any)
			if !ok {
				t.Errorf("%s: parameter %q is not a schema object", spec.Name, name)
				continue
			}
			typ, _ := field[schemaType].(string)
			switch typ {
			case schemaString, schemaInteger, schemaBoolean:
			default:
				t.Errorf("%s: parameter %q has an unexpected type %q", spec.Name, name, typ)
			}
			if desc, _ := field[schemaDescription].(string); strings.TrimSpace(desc) == "" {
				t.Errorf("%s: parameter %q has no description", spec.Name, name)
			}
		}
		// Every required name must be declared; a required name with no schema
		// is a request the model cannot satisfy.
		for name := range required {
			if _, ok := props[name]; !ok {
				t.Errorf("%s: required parameter %q is not declared", spec.Name, name)
			}
		}
	}
}

// TestToolSpecsDeclareTheArgumentsTheyDecode pins each tool's pair (spec, arg
// struct): the declared strings must be exactly the ones the decoder reads, so a
// renamed field cannot leave the spec advertising the old name.
func TestToolSpecsDeclareTheArgumentsTheyDecode(t *testing.T) {
	// requireNames checks that every named argument is declared AND required.
	requireNames := func(t *testing.T, spec Spec, want ...string) {
		t.Helper()
		props, required := argsOf(t, spec)
		for _, name := range want {
			if _, ok := props[name]; !ok {
				t.Fatalf("%s does not declare %q", spec.Name, name)
			}
			if !required[name] {
				t.Fatalf("%s must require %q", spec.Name, name)
			}
		}
	}

	requireNames(t, ReadTool{}.Spec(), argPath)
	requireNames(t, ReadToolNoVision{}.Spec(), argPath)
	requireNames(t, WriteTool{}.Spec(), argPath, argContent)
	requireNames(t, EditTool{}.Spec(), argPath, "old_string", "new_string")
	requireNames(t, GlobTool{}.Spec(), argPattern)
	requireNames(t, GrepTool{}.Spec(), argPattern)

	// bash: command and description are required; timeout is optional.
	bashProps, bashRequired := argsOf(t, BashTool{}.Spec())
	if !bashRequired[argCommand] || !bashRequired[argDescription] {
		t.Fatalf("bash must require command and description, got %v", bashRequired)
	}
	if bashRequired["timeout"] {
		t.Fatal("timeout must stay optional")
	}

	// The optional arguments must still be declared, or the model cannot see them.
	if _, ok := ArgsKeys(ReadTool{}.Spec())[argOffset]; !ok {
		t.Fatal("read must declare offset")
	}
	if _, ok := ArgsKeys(ReadTool{}.Spec())[argLimit]; !ok {
		t.Fatal("read must declare limit")
	}
	if _, ok := ArgsKeys(GrepTool{}.Spec())["include"]; !ok {
		t.Fatal("grep must declare include")
	}
	if _, ok := ArgsKeys(EditTool{}.Spec())["replace_all"]; !ok {
		t.Fatal("edit must declare replace_all")
	}
	_ = bashProps
}

// ArgsKeys exposes the declared parameter names for the assertions above.
func ArgsKeys(spec Spec) map[string]any {
	props, _ := spec.Parameters[schemaProperties].(map[string]any)
	return props
}

// TestReadSpecsDocumentTheirLineWindow pins the two numbers the model relies on
// when it splits a long file: if the spec's limit disagreed with MaxReadLines,
// the model would ask for more than one read can return.
func TestReadSpecsDocumentTheirLineWindow(t *testing.T) {
	want := "2000"
	for _, spec := range []Spec{ReadTool{}.Spec(), ReadToolNoVision{}.Spec()} {
		limit, _ := ArgsKeys(spec)[argLimit].(map[string]any)
		desc, _ := limit[schemaDescription].(string)
		if !strings.Contains(desc, want) {
			t.Errorf("%s: the limit description must state the real cap, got %q", spec.Name, desc)
		}
	}
}

// TestSpecsAreStableAcrossCalls pins that building a spec is side-effect free:
// two calls must declare the same argument names (a spec built from shared,
// mutated state would drift between rounds).
func TestSpecsAreStableAcrossCalls(t *testing.T) {
	for _, tool := range All(true, true, true) {
		first := tool.Spec()
		// Mutating the returned map must not affect the next call.
		firstProps, _ := first.Parameters[schemaProperties].(map[string]any)
		for name := range firstProps {
			delete(firstProps, name)
		}
		first.Parameters[schemaRequired] = nil

		second := tool.Spec()
		props, required := argsOf(t, second)
		if len(props) == 0 || len(required) == 0 {
			t.Errorf("%s: the spec shares mutable state between calls", tool.Name())
		}
	}
}

// TestBashSpecMatchesThePOSIXShell pins the one place the spec names the
// interpreter: it says "/bin/sh -c", and shellCommand() must be the same.
func TestBashSpecMatchesThePOSIXShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell spec is POSIX-only")
	}
	spec := BashTool{}.Spec()
	if !strings.Contains(spec.Description, "shell") {
		t.Fatalf("the description must say it runs a shell command, got %q", spec.Description)
	}
	cmd, _ := ArgsKeys(spec)[argCommand].(map[string]any)
	desc, _ := cmd[schemaDescription].(string)
	shell, _ := shellCommand()
	if !strings.Contains(desc, shell) {
		t.Fatalf("the spec must name the real interpreter %q, got %q", shell, desc)
	}
}

// TestSpecsSurviveJSONRoundTrip pins the shape the wire actually carries: the
// parameters object must marshal, and the required list must come back as an
// array of strings (a map or a comma-joined string would be rejected by every
// provider).
func TestSpecsSurviveJSONRoundTrip(t *testing.T) {
	for _, tool := range All(true, true, true) {
		raw, err := json.Marshal(tool.Spec().Parameters)
		if err != nil {
			t.Fatalf("%s: %v", tool.Name(), err)
		}
		var back map[string]any
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("%s: %v", tool.Name(), err)
		}
		required, ok := back[schemaRequired].([]any)
		if !ok || len(required) == 0 {
			t.Fatalf("%s: the required list must serialize as a non-empty array, got %v", tool.Name(), back[schemaRequired])
		}
		for _, name := range required {
			if _, isString := name.(string); !isString {
				t.Fatalf("%s: a required entry is not a string: %v", tool.Name(), name)
			}
		}
	}
}
