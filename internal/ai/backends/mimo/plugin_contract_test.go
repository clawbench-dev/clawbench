package mimo

import (
	"testing"

	"clawbench/internal/ai/backends"
)

// MiMo-Code is an OpenCode fork: the ACP wire shapes, the tool-name map and the
// plan bridge are shared code, not a copy. These assertions pin that sharing,
// because a fork that silently stopped sharing would look identical until the
// first tool call rendered under the wrong name.

// TestMimoPlugin_BehavesLikeOpenCode pins the three pieces of the ACP behavior:
// a tool-call parser, an update parser, and the plan bridge. A missing plan
// bridge in particular means the todo checklist never renders for MiMo.
func TestMimoPlugin_BehavesLikeOpenCode(t *testing.T) {
	plugin := backends.Lookup("mimo")
	if plugin == nil {
		t.Fatal("mimo backend is not registered")
	}
	if plugin.ACP != nil && plugin.ACP.InputRemaps != nil {
		t.Fatal("MiMo must not carry its own remap copy: OpenCode's fields are the shared generic set")
	}
	// OpenCode sends camelCase fields; the canonical form the frontend and the
	// tool cards read is snake_case, so the effective table must map both.
	remaps := backends.LookupACPRemaps("mimo")
	if remaps["filePath"] != "file_path" || remaps["oldString"] != "old_string" {
		t.Fatalf("the effective remap table must map OpenCode's camelCase field names, got %v", remaps)
	}
	if plugin.Behavior == nil {
		t.Fatal("MiMo must declare an ACP behavior")
	}
	if plugin.Behavior.ParseToolCall == nil || plugin.Behavior.ParseToolCallUpdate == nil {
		t.Fatal("MiMo must reuse OpenCode's tool mappers")
	}
	if plugin.Behavior.OnToolCallUpdate == nil {
		t.Fatal("MiMo must bridge its todowrite snapshots into plan_update")
	}
}

// TestMimoPlugin_DeclaresItsOwnCommandAndSkills pins the spec facts the UI and
// the injection path read: the binary is `mimo` (not `opencode`), and the agent
// loads its own skills so ClawBench must not inject them again.
func TestMimoPlugin_DeclaresItsOwnCommandAndSkills(t *testing.T) {
	spec := backends.Lookup("mimo").Spec
	if spec.DefaultCmd != "mimo" || spec.AcpCommand != "mimo acp" {
		t.Fatalf("MiMo must spawn its own binary, got cmd=%q acp=%q", spec.DefaultCmd, spec.AcpCommand)
	}
	if !spec.AutoLoadsNativeSkills {
		t.Fatal("MiMo loads its own skills; injecting them again would duplicate the table")
	}
	if len(spec.NativeSkillsDirs) == 0 {
		t.Fatal("the skill directories must be declared so detection can find them")
	}
}
