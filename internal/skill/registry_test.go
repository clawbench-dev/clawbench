package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// setTestHome isolates the home directory the skill scanner resolves against.
//
// Both variables are set: os.UserHomeDir reads $HOME on POSIX but $USERPROFILE
// on Windows, so setting only HOME leaves the real profile in play on Windows
// and the shared-directory assertions fail there (and worse, a developer's own
// ~/.agents/skills leaks into the scan).
func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// setupRegistry installs a fresh registry plus two agents whose native dirs
// point at temp directories, and restores the globals afterwards.
//
// The dedup logic depends on model.ConfigInstance, model.DataDir and the agent
// list, so all three are stubbed here rather than relying on the real machine.
func setupRegistry(t *testing.T) (ownDir, otherDir string, reg *Registry) {
	t.Helper()

	origCfg := model.ConfigInstance
	origDataDir := model.DataDir
	origAgents := model.GetAgentList()
	// GetBackendRegistry() is filled once per process via sync.Once, so an
	// override of LoadBackendSpecs is ignored after the first call. Assign the
	// registry variable directly instead (same approach as
	// internal/model/discovery_db_test.go).
	origSpecs := model.GetBackendRegistry()
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.DataDir = origDataDir
		model.BackendRegistry = origSpecs
		model.ReplaceAgents(nil, origAgents)
		ResetForTest()
	})

	root := t.TempDir()
	model.DataDir = root
	// Isolate the home directory: scanSources always scans the shared
	// ~/.agents/skills, so without this the tests would pick up whatever the
	// developer happens to have installed there.
	setTestHome(t, t.TempDir())
	// Enabled must be set explicitly: a zero-value SkillsConfig has
	// Enabled=false (the field's default is applied by ApplyDefaults, which
	// tests do not run).
	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{Enabled: true, Dirs: []string{filepath.Join(root, "user")}},
	}

	ownDir = filepath.Join(root, "own")
	otherDir = filepath.Join(root, "other")
	require.NoError(t, os.MkdirAll(ownDir, 0o755))
	require.NoError(t, os.MkdirAll(otherDir, 0o755))

	// Give each agent its own backend spec so they have distinct native dirs.
	setSpecs(ownDir, otherDir, false)

	model.ReplaceAgents(map[string]*model.Agent{
		"me":   {ID: "me", Backend: "me"},
		"them": {ID: "them", Backend: "them"},
	}, []*model.Agent{
		{ID: "me", Backend: "me"},
		{ID: "them", Backend: "them"},
	})

	ResetForTest()
	reg = Global()
	return ownDir, otherDir, reg
}

// setSpecs installs the two fake backend specs used by these tests.
func setSpecs(ownDir, otherDir string, ownAutoLoads bool) {
	// Force the lazy once to fire first: assigning BackendRegistry before the
	// first GetBackendRegistry() call would be overwritten by the real specs.
	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "me", Backend: "me", NativeSkillsDirs: []string{ownDir}, AutoLoadsNativeSkills: ownAutoLoads},
		{ID: "them", Backend: "them", NativeSkillsDirs: []string{otherDir}},
	}
}

func TestInjectedFor_OwnNativeWins(t *testing.T) {
	ownDir, otherDir, reg := setupRegistry(t)

	writeSkill(t, ownDir, "shared", "---\nname: shared\ndescription: own version\n---\n")
	writeSkill(t, otherDir, "shared", "---\nname: shared\ndescription: other version\n---\n")

	reg.ScanAll()
	got := reg.InjectedFor("me")

	require.Len(t, got, 1, "same-named skills must collapse to one entry")
	assert.Equal(t, "own version", got[0].Description)
	assert.Equal(t, SourceOwnNative, got[0].Source.Kind)
}

func TestInjectedFor_OwnNativeAutoLoadIsSkipped(t *testing.T) {
	ownDir, otherDir, reg := setupRegistry(t)

	// "me" auto-loads its own directory.
	setSpecs(ownDir, otherDir, true)

	writeSkill(t, ownDir, "mine", "---\nname: mine\ndescription: own\n---\n")
	writeSkill(t, otherDir, "theirs", "---\nname: theirs\ndescription: other\n---\n")

	reg.ScanAll()
	got := reg.InjectedFor("me")

	// Own skill is skipped (the agent loads it), but another agent's skill is
	// still injected.
	require.Len(t, got, 1)
	assert.Equal(t, "theirs", got[0].Name)
}

func TestInjectedFor_OwnNativeNotAutoLoadedIsInjected(t *testing.T) {
	ownDir, _, reg := setupRegistry(t)

	// The default spec has AutoLoadsNativeSkills == false, which is what keeps
	// CodeBuddy's skills injected after the generic framework replaced the
	// hardcoded CodeBuddy-only path.
	writeSkill(t, ownDir, "mine", "---\nname: mine\ndescription: own\n---\n")
	reg.ScanAll()

	got := reg.InjectedFor("me")
	require.Len(t, got, 1)
	assert.Equal(t, "mine", got[0].Name)
	assert.Equal(t, SourceOwnNative, got[0].Source.Kind)
}

func TestInjectedFor_OwnNativeShadowsEvenWhenSkipped(t *testing.T) {
	ownDir, otherDir, reg := setupRegistry(t)
	setSpecs(ownDir, otherDir, true)

	writeSkill(t, ownDir, "shared", "---\nname: shared\ndescription: own\n---\n")
	writeSkill(t, otherDir, "shared", "---\nname: shared\ndescription: other\n---\n")

	reg.ScanAll()
	// The own skill wins the dedup and is then skipped, so the other agent's
	// same-named skill must NOT sneak back in.
	assert.Empty(t, reg.InjectedFor("me"))
}

func TestInjectedFor_UserDirBeatsOtherNative(t *testing.T) {
	_, otherDir, reg := setupRegistry(t)

	userDir := filepath.Join(model.DataDir, "user")
	require.NoError(t, os.MkdirAll(userDir, 0o755))
	model.ConfigInstance.Skills = model.SkillsConfig{Enabled: true, Dirs: []string{userDir}}

	writeSkill(t, userDir, "shared", "---\nname: shared\ndescription: user\n---\n")
	writeSkill(t, otherDir, "shared", "---\nname: shared\ndescription: other\n---\n")

	reg.ScanAll()
	got := reg.InjectedFor("me")

	require.Len(t, got, 1)
	assert.Equal(t, "user", got[0].Description)
	assert.Equal(t, SourceUserDir, got[0].Source.Kind)
}

func TestInjectedFor_GitBeatsOtherNativeButLosesToUserDir(t *testing.T) {
	_, otherDir, reg := setupRegistry(t)

	repoSlug := "team-skills-abc"
	gitDir := filepath.Join(model.DataDir, "skills", repoSlug)
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	userDir := filepath.Join(model.DataDir, "user")
	require.NoError(t, os.MkdirAll(userDir, 0o755))

	model.ConfigInstance.Skills = model.SkillsConfig{
		Enabled: true,
		Dirs:    []string{userDir},
		Repos:   []model.SkillRepo{{URL: "https://example.com/team/skills.git", Slug: repoSlug}},
	}

	writeSkill(t, gitDir, "shared", "---\nname: shared\ndescription: git\n---\n")
	writeSkill(t, otherDir, "shared", "---\nname: shared\ndescription: other\n---\n")
	writeSkill(t, otherDir, "git-only", "---\nname: git-only\ndescription: only git has this\n---\n")
	writeSkill(t, gitDir, "git-only", "---\nname: git-only\ndescription: git version\n---\n")

	reg.ScanAll()
	got := reg.InjectedFor("me")

	byName := map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	require.Contains(t, byName, "shared")
	assert.Equal(t, "git", byName["shared"].Description)
	assert.Equal(t, SourceGit, byName["shared"].Source.Kind)

	require.Contains(t, byName, "git-only")
	assert.Equal(t, "git version", byName["git-only"].Description)

	// Now add the same name to the user dir: it must take over from git.
	writeSkill(t, userDir, "shared", "---\nname: shared\ndescription: user\n---\n")
	reg.ScanAll()
	got = reg.InjectedFor("me")
	byName = map[string]Skill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	assert.Equal(t, "user", byName["shared"].Description)
	assert.Equal(t, SourceUserDir, byName["shared"].Source.Kind)
}

func TestInjectedFor_DisabledInjectsNothing(t *testing.T) {
	ownDir, _, reg := setupRegistry(t)
	model.ConfigInstance.Skills = model.SkillsConfig{Enabled: false}

	writeSkill(t, ownDir, "mine", "---\nname: mine\ndescription: own\n---\n")
	reg.ScanAll()

	assert.Empty(t, reg.InjectedFor("me"))
}

func TestInjectedFor_DeterministicOrder(t *testing.T) {
	ownDir, _, reg := setupRegistry(t)
	writeSkill(t, ownDir, "zeta", "---\nname: zeta\ndescription: z\n---\n")
	writeSkill(t, ownDir, "alpha", "---\nname: alpha\ndescription: a\n---\n")
	writeSkill(t, ownDir, "mid", "---\nname: mid\ndescription: m\n---\n")

	reg.ScanAll()
	got := reg.InjectedFor("me")
	require.Len(t, got, 3)
	assert.Equal(t, []string{"alpha", "mid", "zeta"}, []string{got[0].Name, got[1].Name, got[2].Name})
}

func TestInjectedFor_UnknownAgent(t *testing.T) {
	_, _, reg := setupRegistry(t)
	reg.ScanAll()
	assert.Nil(t, reg.InjectedFor("nobody"))
	assert.Nil(t, reg.InjectedFor(""))
}

func TestInvalidateDropsCache(t *testing.T) {
	ownDir, _, reg := setupRegistry(t)
	reg.ScanAll()
	assert.Empty(t, reg.InjectedFor("me"))

	writeSkill(t, ownDir, "new", "---\nname: new\ndescription: added later\n---\n")
	// Without a rescan the cached (empty) result is still returned...
	assert.Empty(t, reg.InjectedFor("me"))

	reg.ScanAll()
	require.Len(t, reg.InjectedFor("me"), 1)
}

func TestOwnNative(t *testing.T) {
	ownDir, otherDir, reg := setupRegistry(t)
	writeSkill(t, ownDir, "mine", "---\nname: mine\ndescription: own\n---\n")
	writeSkill(t, otherDir, "theirs", "---\nname: theirs\ndescription: other\n---\n")
	reg.ScanAll()

	own := reg.OwnNative("me")
	require.Len(t, own, 1)
	assert.Equal(t, "mine", own[0].Name)

	assert.Nil(t, reg.OwnNative("nobody"))
}

func TestAll(t *testing.T) {
	ownDir, otherDir, reg := setupRegistry(t)
	writeSkill(t, ownDir, "mine", "---\nname: mine\ndescription: own\n---\n")
	writeSkill(t, otherDir, "theirs", "---\nname: theirs\ndescription: other\n---\n")
	reg.ScanAll()

	all := reg.All()
	require.Len(t, all, 2)
	assert.Equal(t, "mine", all[0].Name)
	assert.Equal(t, "theirs", all[1].Name)
}

func TestRepoDirRejectsEscapingSlug(t *testing.T) {
	origDataDir := model.DataDir
	t.Cleanup(func() { model.DataDir = origDataDir })
	model.DataDir = t.TempDir()

	assert.Equal(t, filepath.Join(model.DataDir, "skills", "ok"), RepoDir("ok"))
	assert.Equal(t, "", RepoDir("../escape"))
	assert.Equal(t, "", RepoDir(""))
	assert.Equal(t, "", RepoDir("a/b"))
}

// TestInjectedFor_MultipleUserDirs pins that every configured user directory is
// scanned, not just the first.
func TestInjectedFor_MultipleUserDirs(t *testing.T) {
	_, _, reg := setupRegistry(t)

	dirA := filepath.Join(model.DataDir, "user-a")
	dirB := filepath.Join(model.DataDir, "user-b")
	require.NoError(t, os.MkdirAll(dirA, 0o755))
	require.NoError(t, os.MkdirAll(dirB, 0o755))
	model.ConfigInstance.Skills = model.SkillsConfig{Enabled: true, Dirs: []string{dirA, dirB}}

	writeSkill(t, dirA, "from-a", "---\nname: from-a\ndescription: a\n---\n")
	writeSkill(t, dirB, "from-b", "---\nname: from-b\ndescription: b\n---\n")

	reg.ScanAll()
	got := reg.InjectedFor("me")

	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	assert.True(t, names["from-a"], "the first user dir must be scanned")
	assert.True(t, names["from-b"], "the second user dir must be scanned")
	assert.Len(t, got, 2)
}

// TestInjectedFor_MultipleUserDirsDedupe pins that a name present in two user
// directories collapses to one entry, deterministically (all user dirs share
// SourceUserDir priority, so the directory path breaks the tie).
func TestInjectedFor_MultipleUserDirsDedupe(t *testing.T) {
	_, _, reg := setupRegistry(t)

	dirA := filepath.Join(model.DataDir, "user-a")
	dirB := filepath.Join(model.DataDir, "user-b")
	require.NoError(t, os.MkdirAll(dirA, 0o755))
	require.NoError(t, os.MkdirAll(dirB, 0o755))

	writeSkill(t, dirA, "shared", "---\nname: shared\ndescription: from-a\n---\n")
	writeSkill(t, dirB, "shared", "---\nname: shared\ndescription: from-b\n---\n")

	// Both orders must produce the same winner: the tie-break is the directory
	// path, not the list order.
	for _, dirs := range [][]string{{dirA, dirB}, {dirB, dirA}} {
		model.ConfigInstance.Skills = model.SkillsConfig{Enabled: true, Dirs: dirs}
		reg.ScanAll()
		got := reg.InjectedFor("me")
		require.Len(t, got, 1, "same-named skills in two user dirs must collapse")
		// dirA < dirB lexically, so "from-a" always wins.
		assert.Equal(t, "from-a", got[0].Description)
		assert.Equal(t, SourceUserDir, got[0].Source.Kind)
	}
}

// TestResolveSkillsDirs covers the default fallback and blank filtering.
func TestResolveSkillsDirs(t *testing.T) {
	origDataDir := model.DataDir
	t.Cleanup(func() { model.DataDir = origDataDir })
	model.DataDir = t.TempDir()

	t.Run("empty list falls back to the default dir", func(t *testing.T) {
		cfg := model.Config{}
		assert.Equal(t, []string{model.DefaultSkillsDir()}, cfg.ResolveSkillsDirs())
	})

	t.Run("configured dirs are returned in order", func(t *testing.T) {
		cfg := model.Config{Skills: model.SkillsConfig{Dirs: []string{"/a", "/b"}}}
		assert.Equal(t, []string{"/a", "/b"}, cfg.ResolveSkillsDirs())
	})

	t.Run("blank entries are dropped", func(t *testing.T) {
		cfg := model.Config{Skills: model.SkillsConfig{Dirs: []string{"/a", "", "/b"}}}
		assert.Equal(t, []string{"/a", "/b"}, cfg.ResolveSkillsDirs())
	})

	t.Run("an all-blank list falls back to the default", func(t *testing.T) {
		cfg := model.Config{Skills: model.SkillsConfig{Dirs: []string{"", ""}}}
		assert.Equal(t, []string{model.DefaultSkillsDir()}, cfg.ResolveSkillsDirs())
	})
}

// TestInjectedFor_SharedSkillsDirAlwaysScanned pins that ~/.agents/skills is
// discovered even when no installed agent declares it — it is where
// `npx skills` installs, so a Claude-only setup must still see those skills.
func TestInjectedFor_SharedSkillsDirAlwaysScanned(t *testing.T) {
	_, _, reg := setupRegistry(t)

	home := os.Getenv("HOME")
	shared := filepath.Join(home, SharedSkillsDir)
	writeSkill(t, shared, "shared-skill", "---\nname: shared-skill\ndescription: from npx skills\n---\n")

	// Neither fake backend declares .agents/skills.
	reg.ScanAll()
	got := reg.InjectedFor("me")

	require.Len(t, got, 1)
	assert.Equal(t, "shared-skill", got[0].Name)
	assert.Contains(t, got[0].Path, SharedSkillsDir)
}

// TestInjectedFor_SharedDirScannedOnceForManyAgents pins that a directory
// declared by several agents is scanned once and stored once — otherwise its
// skills would be listed once per agent in the UI and re-read on every scan.
func TestInjectedFor_SharedDirScannedOnceForManyAgents(t *testing.T) {
	ownDir, otherDir, reg := setupRegistry(t)

	home := os.Getenv("HOME")
	shared := filepath.Join(home, SharedSkillsDir)
	writeSkill(t, shared, "everywhere", "---\nname: everywhere\ndescription: shared\n---\n")
	// Give each agent's own dir a skill too, so all three directories have
	// content and therefore a registry entry.
	writeSkill(t, ownDir, "only-me", "---\nname: only-me\ndescription: own\n---\n")
	writeSkill(t, otherDir, "only-them", "---\nname: only-them\ndescription: other\n---\n")

	// Both agents declare the shared directory.
	setSpecsShared(ownDir, otherDir, shared)
	reg.ScanAll()

	// One registry entry per distinct directory: own + other + shared.
	reg.mu.RLock()
	entries := len(reg.bySource)
	// The shared source must record BOTH agents. This is what pins the grouping
	// in scanSources: scanning per agent instead would still collapse to one
	// entry (the key is the directory), so only the recorded agent list
	// distinguishes "scanned once, declared by two" from "scanned twice".
	var sharedAgents string
	for _, skills := range reg.bySource {
		for _, s := range skills {
			if s.Name == "everywhere" {
				sharedAgents = s.Source.AgentID
			}
		}
	}
	reg.mu.RUnlock()
	assert.Equal(t, 3, entries, "own + other + shared = 3 distinct directories")
	assert.Equal(t, "me,them", sharedAgents,
		"the shared directory must be recorded as declared by both agents")

	// And the UI listing shows it once, not once per agent.
	var n int
	for _, s := range reg.All() {
		if s.Name == "everywhere" {
			n++
		}
	}
	assert.Equal(t, 1, n, "a shared skill must be listed once")
}

// TestInjectedFor_NativeDirContainment pins that a skill nested below a declared
// native directory counts as that agent's own (a declared directory is a tree,
// not a single skill).
func TestInjectedFor_NativeDirContainment(t *testing.T) {
	_, _, reg := setupRegistry(t)

	ownRoot := filepath.Join(os.Getenv("HOME"), "contain")
	setSpecsNested(ownRoot)
	// Nested two levels below the declared root.
	writeSkill(t, filepath.Join(ownRoot, "group"), "deep", "---\nname: deep\ndescription: nested\n---\n")

	reg.ScanAll()
	own := reg.OwnNative("me")
	require.Len(t, own, 1, "a skill below the declared dir is still the agent's own")
	assert.Equal(t, "deep", own[0].Name)

	// With auto-load on it must be skipped from injection (the agent has it).
	setSpecsNestedAutoLoad(ownRoot)
	reg.ScanAll()
	assert.Empty(t, reg.InjectedFor("me"))
}

// setSpecsShared installs specs where both agents also read the shared dir.
func setSpecsShared(ownDir, otherDir, shared string) {
	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "me", Backend: "me", NativeSkillsDirs: []string{ownDir, shared}},
		{ID: "them", Backend: "them", NativeSkillsDirs: []string{otherDir, shared}},
	}
}

func setSpecsNested(root string) {
	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "me", Backend: "me", NativeSkillsDirs: []string{root}},
		{ID: "them", Backend: "them", NativeSkillsDirs: []string{filepath.Join(os.Getenv("HOME"), "unused")}},
	}
}

func setSpecsNestedAutoLoad(root string) {
	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "me", Backend: "me", NativeSkillsDirs: []string{root}, AutoLoadsNativeSkills: true},
		{ID: "them", Backend: "them", NativeSkillsDirs: []string{filepath.Join(os.Getenv("HOME"), "unused")}},
	}
}

// TestSharedDirIsFlagged pins that the shared directory is marked on the Source.
// The flag is what lets the UI label those skills "generic" instead of naming
// whichever backend happens to read the directory — the own/other kind is
// observer-dependent, but "shared" is not.
func TestSharedDirIsFlagged(t *testing.T) {
	_, _, reg := setupRegistry(t)

	home := os.Getenv("HOME")
	shared := filepath.Join(home, SharedSkillsDir)
	ownDir := filepath.Join(home, "own")

	writeSkill(t, shared, "shared-skill", "---\nname: shared-skill\ndescription: s\n---\n")
	writeSkill(t, ownDir, "own-skill", "---\nname: own-skill\ndescription: o\n---\n")

	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "me", Backend: "me", NativeSkillsDirs: []string{ownDir}},
	}
	model.ReplaceAgents(map[string]*model.Agent{"me": {ID: "me", Backend: "me"}}, []*model.Agent{{ID: "me", Backend: "me"}})
	ResetForTest()
	reg.ScanAll()

	byName := map[string]skill2Source{}
	for _, s := range reg.All() {
		byName[s.Name] = skill2Source{shared: s.Source.Shared, kind: s.Source.Kind}
	}

	require.Contains(t, byName, "shared-skill")
	assert.True(t, byName["shared-skill"].shared, "the shared directory must be flagged")
	assert.False(t, byName["own-skill"].shared, "an ordinary native dir must not be flagged")
}

type skill2Source struct {
	shared bool
	kind   SourceKind
}
