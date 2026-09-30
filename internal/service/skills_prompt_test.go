package service

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

// setupSkillInjection points the skill registry at a temp native dir for one
// fake agent, and restores every global it touched.
func setupSkillInjection(t *testing.T) (agentID string) {
	t.Helper()

	origCfg := model.ConfigInstance
	origDataDir := model.DataDir
	origSpecs := model.GetBackendRegistry()
	origAgents := model.GetAgentList()
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.DataDir = origDataDir
		model.BackendRegistry = origSpecs
		model.ReplaceAgents(nil, origAgents)
		skill.ResetForTest()
	})

	root := t.TempDir()
	model.DataDir = root
	// Isolate the home directory: the registry always scans the shared
	// ~/.agents/skills, so without this the test would see the developer's own
	// installed skills.
	t.Setenv("HOME", t.TempDir())
	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{Enabled: true, Dirs: []string{filepath.Join(root, "user")}},
	}

	ownDir := filepath.Join(root, "native")
	require.NoError(t, os.MkdirAll(ownDir, 0o755))
	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "testagent", Backend: "testagent", NativeSkillsDirs: []string{ownDir}},
	}
	agent := &model.Agent{ID: "testagent", Backend: "testagent"}
	model.ReplaceAgents(map[string]*model.Agent{"testagent": agent}, []*model.Agent{agent})

	skillDir := filepath.Join(ownDir, "demo")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: demo-skill\ndescription: A demo\n---\n"), 0o644))

	skill.ResetForTest()
	skill.Global().ScanAll()
	return "testagent"
}

func TestAppendSkillsSection(t *testing.T) {
	agentID := setupSkillInjection(t)

	t.Run("empty prompt becomes the section alone", func(t *testing.T) {
		got := AppendSkillsSection("", agentID)
		assert.Contains(t, got, "demo-skill")
		assert.Contains(t, got, "## Available Skills")
	})

	t.Run("existing prompt is preserved and the section appended", func(t *testing.T) {
		got := AppendSkillsSection("BASE PROMPT", agentID)
		assert.True(t, strings.HasPrefix(got, "BASE PROMPT"))
		assert.Contains(t, got, "demo-skill")
		assert.Contains(t, got, "BASE PROMPT\n\n## Available Skills")
	})

	t.Run("unknown agent leaves the prompt untouched", func(t *testing.T) {
		assert.Equal(t, "BASE PROMPT", AppendSkillsSection("BASE PROMPT", "nobody"))
		assert.Equal(t, "", AppendSkillsSection("", ""))
	})
}

func TestAppendSkillsSection_NoSkillsLeavesPromptUnchanged(t *testing.T) {
	origCfg := model.ConfigInstance
	origSpecs := model.GetBackendRegistry()
	origAgents := model.GetAgentList()
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.BackendRegistry = origSpecs
		model.ReplaceAgents(nil, origAgents)
		skill.ResetForTest()
	})

	model.DataDir = t.TempDir()
	// Isolate the home directory: the registry always scans the shared
	// ~/.agents/skills, so without this the test would see the developer's own
	// installed skills and the prompt would not stay unchanged.
	t.Setenv("HOME", t.TempDir())
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true, Dirs: []string{filepath.Join(model.DataDir, "user")}}}
	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "bare", Backend: "bare", NativeSkillsDirs: []string{filepath.Join(model.DataDir, "nothing")}},
	}
	agent := &model.Agent{ID: "bare", Backend: "bare"}
	model.ReplaceAgents(map[string]*model.Agent{"bare": agent}, []*model.Agent{agent})
	skill.ResetForTest()
	skill.Global().ScanAll()

	assert.Equal(t, "BASE", AppendSkillsSection("BASE", "bare"))
}

// TestSkillsSectionWiredIntoBothProducers is a source guard: there are two
// producers of ai.ChatRequest.SystemPrompt and BOTH must append the skill
// table. A behavior test cannot catch a missing call in one of them (the
// scheduled-task path is not reachable from a unit test without a full
// scheduler), so the call sites are asserted directly.
func TestSkillsSectionWiredIntoBothProducers(t *testing.T) {
	cases := []struct {
		file string
		why  string
	}{
		{"chat_request.go", "direct sends, queue drain and push all build the request here"},
		{"scheduler.go", "scheduled tasks build the request directly and would otherwise miss the skills"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			src, err := os.ReadFile(tc.file)
			require.NoError(t, err)
			assert.Contains(t, string(src), "AppendSkillsSection(",
				"%s must call AppendSkillsSection (%s)", tc.file, tc.why)
		})
	}
}
