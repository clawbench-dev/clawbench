package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/skill"
)

// --- skillRefCandidates tests ---

func TestSkillRefCandidates(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want []string
	}{
		// The reference may sit anywhere, as long as it starts a token.
		{"leading reference", "#pdf summarize this", []string{"pdf"}},
		{"bare reference", "#pdf", []string{"pdf"}},
		{"leading whitespace", "   #pdf do it", []string{"pdf"}},
		{"leading newline", "\n#pdf do it", []string{"pdf"}},
		{"leading tab", "\t#pdf do it", []string{"pdf"}},
		{"leading vertical tab", "\v#pdf do it", []string{"pdf"}},
		{"leading form feed", "\f#pdf do it", []string{"pdf"}},
		{"leading nbsp", "\u00a0#pdf do it", []string{"pdf"}},

		// Mid-message references — the whole point of the change.
		{"mid-sentence reference", "summarize this #pdf", []string{"pdf"}},
		{"reference after other text", "hello #pdf", []string{"pdf"}},
		{"reference on a later line", "first line\n#pdf do it", []string{"pdf"}},
		{"two references", "use #pdf and #web-search", []string{"pdf", "web-search"}},
		{"duplicate references", "#pdf then #pdf again", []string{"pdf", "pdf"}},

		// Trailing punctuation terminates the name, not part of it.
		{"trailing comma", "use #pdf, then summarize", []string{"pdf"}},
		{"trailing period", "use #pdf.", []string{"pdf"}},
		{"trailing cjk punctuation", "用 #pdf。", []string{"pdf"}},
		{"trailing fullwidth comma", "用 #pdf，然后总结", []string{"pdf"}},
		{"trailing colon", "skill: #pdf:", []string{"pdf"}},
		{"punctuation then more text", "#pdf,then", []string{"pdf"}},
		{"inner punctuation kept", "#user/skill", []string{"user/skill"}},

		// Not references.
		{"no hash at all", "just a message", nil},
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"lone hash", "#", nil},
		{"lone hash with space", "# something", nil},
		{"double hash no name", "##", nil},
		{"name containing hash", "#pdf#other", nil},
		// Mid-word hashes: the "#" does not start the token.
		{"mid-word hash", "C#", nil},
		{"mid-word hash after text", "use#pdf", nil},
		{"url fragment", "see https://x.dev/a#section", nil},
		// "issue #12" IS a candidate token ("12"), but it resolves to no skill;
		// filtering happens in processSkillCommand.
		{"issue number is a candidate", "issue #12 is fixed", []string{"12"}},

		// Name characters.
		{"hyphenated name", "#web-search find it", []string{"web-search"}},
		{"name with slash", "#user/skill", []string{"user/skill"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, skillRefCandidates(tc.msg), "candidates for %q", tc.msg)
		})
	}
}

// --- processSkillCommand tests ---

// setupSkillForHandler installs skills in an isolated user dir and points the
// global registry at it.
func setupSkillForHandler(t *testing.T, names ...string) {
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

	for _, name := range names {
		writeHandlerSkill(t, dir, name)
	}

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

// TestProcessSkillCommand_MidMessageReference is the core of issue #548: a
// reference typed after the problem description must still inject.
func TestProcessSkillCommand_MidMessageReference(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	result := processSkillCommand("please summarize the attached file #pdf", "/project")
	assert.Contains(t, result, filepath.Join("pdf", "SKILL.md"),
		"a mid-message reference must inject like a leading one")
	assert.NotContains(t, result, "please summarize the attached file #pdf")
}

// TestProcessSkillCommand_MultipleReferences pins that every resolvable
// reference is injected (previously a second "#" token made the whole message
// fail closed, which would break the natural "use #a and #b" phrasing).
func TestProcessSkillCommand_MultipleReferences(t *testing.T) {
	setupSkillForHandler(t, "pdf", "web-search")

	result := processSkillCommand("use #pdf and #web-search on this", "/project")
	assert.Contains(t, result, filepath.Join("pdf", "SKILL.md"))
	assert.Contains(t, result, filepath.Join("web-search", "SKILL.md"))
	assert.Equal(t, 2, strings.Count(result, "explicitly selected the skill"),
		"one template block per distinct reference")
}

// TestProcessSkillCommand_DuplicateReferenceInjectedOnce: repeating a skill
// adds no instruction, only prompt weight.
func TestProcessSkillCommand_DuplicateReferenceInjectedOnce(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	result := processSkillCommand("#pdf do it, and also #pdf again", "/project")
	assert.Equal(t, 1, strings.Count(result, "explicitly selected the skill"))
}

// TestProcessSkillCommand_MixedKnownAndUnknown: an unknown hash is ordinary
// text and must not suppress the known reference (the old fail-closed rule
// rejected the whole message when a second "#" token appeared).
func TestProcessSkillCommand_MixedKnownAndUnknown(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	result := processSkillCommand("#pdf fix issue #12", "/project")
	assert.Contains(t, result, filepath.Join("pdf", "SKILL.md"))
	assert.Equal(t, 1, strings.Count(result, "explicitly selected the skill"))
}

// TestProcessSkillCommand_TrailingPunctuation pins that a reference followed
// by punctuation still resolves — the frontend strips the same characters
// before matching, so the menu and the server must agree.
func TestProcessSkillCommand_TrailingPunctuation(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	for _, msg := range []string{"use #pdf, thanks", "use #pdf.", "用 #pdf。", "use #pdf!"} {
		result := processSkillCommand(msg, "/project")
		assert.Contains(t, result, filepath.Join("pdf", "SKILL.md"), "msg %q", msg)
	}
}

func TestProcessSkillCommand_UnknownNameUnchanged(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	msg := "#nope do something"
	assert.Equal(t, msg, processSkillCommand(msg, "/project"),
		"an unknown skill name must fall through to a normal message")
}

func TestProcessSkillCommand_ProseHashUnchanged(t *testing.T) {
	setupSkillForHandler(t, "pdf")

	for _, msg := range []string{
		"issue #12 is fixed",
		"see https://x.dev/a#section",
		"C# is a language",
		"use#pdf",
	} {
		assert.Equal(t, msg, processSkillCommand(msg, "/project"),
			"prose hash must not inject: %q", msg)
	}
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

	// A mid-message reference renders too.
	got, err = processCommandInjection("do it #pdf", "/project", "sess-1")
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
