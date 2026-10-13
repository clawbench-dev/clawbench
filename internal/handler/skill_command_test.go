package handler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/skill"
)

// --- parseSkillCommand tests ---

func TestParseSkillCommand(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		wantName string
		wantOK   bool
	}{
		{"bare reference", "#pdf", "pdf", true},
		{"reference with body", "#pdf summarize this", "pdf", true},
		{"leading whitespace", "   #pdf do it", "pdf", true},
		{"leading newline", "\n#pdf do it", "pdf", true},
		{"leading tab", "\t#pdf do it", "pdf", true},
		// Non-ASCII / exotic spaces must be stripped too, matching the
		// frontend's JS .trim() (otherwise the menu opens and the server does
		// not recognize the reference).
		{"leading vertical tab", "\v#pdf do it", "pdf", true},
		{"leading form feed", "\f#pdf do it", "pdf", true},
		{"leading nbsp", "\u00a0#pdf do it", "pdf", true},
		{"hyphenated name", "#web-search find it", "web-search", true},
		{"name with slash-like chars", "#user/skill", "user/skill", true},

		// Not a reference: the "#" is not at the start.
		{"mid-sentence hash", "use #pdf please", "", false},
		{"trailing hash", "summarize this #pdf", "", false},
		{"hash after other text", "hello #pdf", "", false},
		{"no hash at all", "just a message", "", false},
		{"empty", "", "", false},
		{"whitespace only", "   ", "", false},
		{"lone hash", "#", "", false},
		{"lone hash with space", "# something", "", false},
		{"double hash no name", "##", "", false},

		// Exactly one reference is allowed; a second "#" token is fail-closed.
		{"two references", "#pdf #web-search do both", "", false},
		{"two references no body", "#a #b", "", false},
		// A "#" later in the body (not immediately after) is prose, not a
		// second reference, so the message is still a valid single reference.
		{"second hash later in body", "#pdf see #123", "pdf", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, ok := parseSkillCommand(tc.msg)
			assert.Equal(t, tc.wantOK, ok, "ok for %q", tc.msg)
			assert.Equal(t, tc.wantName, name, "name for %q", tc.msg)
		})
	}
}

// --- processSkillCommand tests ---

// setupSkillForHandler installs a skill in an isolated user dir and points the
// global registry at it.
func setupSkillForHandler(t *testing.T, name string) {
	t.Helper()

	origCfg := model.ConfigInstance
	origDataDir := model.DataDir
	origAgents := model.GetAgentList()
	origSpecs := model.GetBackendRegistry()
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.DataDir = origDataDir
		model.BackendRegistry = origSpecs
		model.ReplaceAgents(nil, origAgents)
		skill.ResetForTest()
	})

	root := t.TempDir()
	model.DataDir = root
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(root, "user")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true, Dirs: []string{dir}}}
	model.ReplaceAgents(nil, nil)

	writeHandlerSkill(t, dir, name)

	skill.ResetForTest()
	skill.Global().ScanAll()
}

func writeHandlerSkill(t *testing.T, dir, folder string) {
	t.Helper()
	p := filepath.Join(dir, folder)
	require.NoError(t, os.MkdirAll(p, 0o755))
	content := "---\nname: " + folder + "\ndescription: d\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(p, "SKILL.md"), []byte(content), 0o644))
}

func TestProcessSkillCommand_InjectsPath(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	result := processSkillCommand("#pdf summarize the doc", "/project")
	assert.Contains(t, result, "pdf")
	assert.Contains(t, result, "SKILL.md")
	assert.Contains(t, result, filepath.Join("pdf", "SKILL.md"),
		"the injection must carry the resolved SKILL.md path")
	// Returns ONLY the template; the caller prepends it to a prompt that already
	// contains the user's original message.
	assert.NotContains(t, result, "#pdf summarize the doc")
}

func TestProcessSkillCommand_UnknownNameUnchanged(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	msg := "#nope do something"
	assert.Equal(t, msg, processSkillCommand(msg, "/project"),
		"an unknown skill name must fall through to a normal message")
}

func TestProcessSkillCommand_NonReferenceUnchanged(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	msg := "use #pdf here"
	assert.Equal(t, msg, processSkillCommand(msg, "/project"),
		"a mid-sentence # is not a reference")
}

func TestProcessSkillCommand_TwoReferencesUnchanged(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	msg := "#pdf #pdf do it"
	assert.Equal(t, msg, processSkillCommand(msg, "/project"),
		"two references are fail-closed, never a guess")
}

// --- processCommandInjection tests ---

// TestProcessCommandInjection_Dispatches pins the single dispatcher: /cb-* goes
// to the built-in renderer, #skill to the skill renderer, and neither leaks
// into the other.
func TestProcessCommandInjection_Dispatches(t *testing.T) {
	withServerPort(t, 20000)
	setupSkillForHandler(t, "pdf")

	// A built-in command still renders its OpenAPI-backed template.
	got, err := processCommandInjection("/cb-task daily build", "/project", "sess-1")
	require.NoError(t, err)
	assert.Contains(t, got, "POST /api/tasks")

	// An explicit skill reference renders the skill instruction.
	got, err = processCommandInjection("#pdf do it", "/project", "sess-1")
	require.NoError(t, err)
	assert.Contains(t, got, filepath.Join("pdf", "SKILL.md"))

	// A plain message is returned unchanged (the caller reads this as no
	// injection).
	got, err = processCommandInjection("hello", "/project", "sess-1")
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

// TestProcessCommandInjection_BuiltinNotTreatedAsSkill pins that a "/cb-*"
// command is never parsed as a "#" reference and vice versa.
func TestProcessCommandInjection_BuiltinNotTreatedAsSkill(t *testing.T) {
	withServerPort(t, 20000)
	setupSkillForHandler(t, "cb-task") // a skill named like the command

	got, err := processCommandInjection("/cb-task list", "/project", "sess-1")
	require.NoError(t, err)
	assert.Contains(t, got, "POST /api/tasks", "the built-in command must win the dispatch")

	// And the "#" form resolves the skill, not the command.
	got, err = processCommandInjection("#cb-task list", "/project", "sess-1")
	require.NoError(t, err)
	assert.Contains(t, got, filepath.Join("cb-task", "SKILL.md"))
}
