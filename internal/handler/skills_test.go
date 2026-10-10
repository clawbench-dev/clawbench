package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/skill"
)

// isolateSkillGlobals saves the process-global state the skill endpoints read
// (data dir, config, backend registry, agent roster) and restores it on
// cleanup.
//
// This is not optional hygiene: model.DataDir is process-global, and several
// tests below point it at a t.TempDir(). Without a restore the last such test
// leaves it dangling after Go removes the directory, and a LATER test in the
// same package that resolves the data dir — notably the disk sampler in
// system_resources_test.go — then samples a path that no longer exists and
// reports disk.total = 0. Tests run in file order, so skills_test.go runs
// before system_resources_test.go and the leak is deterministic, not flaky.
func isolateSkillGlobals(t *testing.T) {
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
}

// setSkillTestHome isolates the home directory the skill scanner resolves
// against. Both variables are set because os.UserHomeDir reads $HOME on POSIX
// but $USERPROFILE on Windows; setting only HOME leaves the real profile in
// play on Windows and the shared-directory tests fail there.
func setSkillTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

// setupSkillsEnv isolates the skill globals and gives one agent a native dir
// with a skill, so the endpoints have something to report.
func setupSkillsEnv(t *testing.T) {
	t.Helper()

	isolateSkillGlobals(t)

	root := t.TempDir()
	model.DataDir = root
	// Isolate the home directory: the registry always scans the shared
	// ~/.agents/skills, so without this the test would see the developer's own
	// installed skills.
	setSkillTestHome(t, t.TempDir())
	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{Enabled: true, Dirs: []string{filepath.Join(root, "user")}, RefreshHours: 6},
	}

	native := filepath.Join(root, "native")
	dir := filepath.Join(native, "demo")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: demo\ndescription: A demo skill\n---\n"), 0o644))

	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "a1", Backend: "a1", NativeSkillsDirs: []string{native}},
	}
	agent := &model.Agent{ID: "a1", Backend: "a1"}
	model.ReplaceAgents(map[string]*model.Agent{"a1": agent}, []*model.Agent{agent})

	skill.ResetForTest()
	skill.Global().ScanAll()
}

func TestServeSkills_List(t *testing.T) {
	setupSkillsEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkills, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Enabled)
	assert.Equal(t, 6, resp.RefreshHours)
	require.Len(t, resp.Skills, 1)
	assert.Equal(t, "demo", resp.Skills[0].Name)
	// The listing has no agent in scope, so a native directory is reported as
	// "other" (relative to whom is what InjectedFor decides).
	assert.Equal(t, "other", resp.Skills[0].SourceKind)
	assert.Equal(t, "a1", resp.Skills[0].AgentID)
	// The UI shows the AI backend, not the agent id. Here the agent's backend
	// is also "a1", so it must be reported as such.
	assert.Equal(t, []string{"a1"}, resp.Skills[0].Backends)
	assert.Contains(t, resp.Skills[0].Path, "SKILL.md")
}

// A custom agent's ID can differ from its backend. The listing must report the
// BACKEND (what the UI renders as an icon+name), not the agent id.
func TestServeSkills_BackendsResolveThroughAgent(t *testing.T) {
	isolateSkillGlobals(t)

	root := t.TempDir()
	model.DataDir = root
	setSkillTestHome(t, t.TempDir())
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true}}

	native := filepath.Join(root, "native")
	writeSkillMD(t, filepath.Join(native, "demo"), "demo")

	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "my-custom-agent", Backend: "claude", NativeSkillsDirs: []string{native}},
	}
	agent := &model.Agent{ID: "my-custom-agent", Backend: "claude"}
	model.ReplaceAgents(map[string]*model.Agent{"my-custom-agent": agent}, []*model.Agent{agent})
	skill.ResetForTest()
	skill.Global().ScanAll()

	req := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkills, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Skills, 1)
	assert.Equal(t, "my-custom-agent", resp.Skills[0].AgentID)
	assert.Equal(t, []string{"claude"}, resp.Skills[0].Backends,
		"the backend (not the agent id) is what the UI renders")
}

// A shared (.agents/skills) skill belongs to no single backend, so it must
// carry neither an agent id nor a backend roster — the UI labels it "generic".
func TestServeSkills_SharedHasNoBackends(t *testing.T) {
	isolateSkillGlobals(t)

	root := t.TempDir()
	model.DataDir = root
	home := t.TempDir()
	setSkillTestHome(t, home)
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true}}

	shared := filepath.Join(home, skill.SharedSkillsDir)
	writeSkillMD(t, filepath.Join(shared, "generic"), "generic")

	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "claude", Backend: "claude", NativeSkillsDirs: []string{skill.SharedSkillsDir}},
		{ID: "codex", Backend: "codex", NativeSkillsDirs: []string{skill.SharedSkillsDir}},
	}
	model.ReplaceAgents(map[string]*model.Agent{
		"claude": {ID: "claude", Backend: "claude"},
		"codex":  {ID: "codex", Backend: "codex"},
	}, []*model.Agent{{ID: "claude", Backend: "claude"}, {ID: "codex", Backend: "codex"}})
	skill.ResetForTest()
	skill.Global().ScanAll()

	req := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkills, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Skills, 1)
	assert.True(t, resp.Skills[0].Shared)
	assert.Empty(t, resp.Skills[0].AgentID)
	assert.Empty(t, resp.Skills[0].Backends,
		"a shared skill must not carry the roster of backends that read the directory")
}

// A project skill (<project>/.agents/skills) is listed only for the requesting
// project: the cookie selects it, and another project's skills never appear.
func TestServeSkills_ListsCurrentProjectSkills(t *testing.T) {
	isolateSkillGlobals(t)
	model.DataDir = t.TempDir()
	setSkillTestHome(t, t.TempDir())
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true}}
	model.GetBackendRegistry()
	model.BackendRegistry = nil
	model.ReplaceAgents(nil, nil)
	skill.ResetForTest()
	skill.Global().ScanAll()

	projA := t.TempDir()
	projB := t.TempDir()
	writeSkillMD(t, filepath.Join(projA, ".agents", "skills", "a-skill"), "a-skill")
	writeSkillMD(t, filepath.Join(projB, ".agents", "skills", "b-skill"), "b-skill")

	req := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	withProjectCookie(req, projA)
	w := callHandler(ServeSkills, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	byName := map[string]skillInfoJSON{}
	for _, s := range resp.Skills {
		byName[s.Name] = s
	}
	require.Contains(t, byName, "a-skill", "the current project's skill must be listed")
	assert.Equal(t, "project", byName["a-skill"].SourceKind)
	assert.Empty(t, byName["a-skill"].AgentID)
	assert.Empty(t, byName["a-skill"].Backends)
	assert.NotContains(t, byName, "b-skill", "another project's skill must not be listed")
}

// Without a project cookie the listing is the global view: no project skills.
func TestServeSkills_NoProjectCookieOmitsProjectSkills(t *testing.T) {
	isolateSkillGlobals(t)
	model.DataDir = t.TempDir()
	setSkillTestHome(t, t.TempDir())
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true}}
	model.GetBackendRegistry()
	model.BackendRegistry = nil
	model.ReplaceAgents(nil, nil)
	skill.ResetForTest()
	skill.Global().ScanAll()

	proj := t.TempDir()
	writeSkillMD(t, filepath.Join(proj, ".agents", "skills", "p-skill"), "p-skill")

	req := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkills, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	for _, s := range resp.Skills {
		assert.NotEqual(t, "p-skill", s.Name, "no project cookie means no project skills")
	}
}

func TestServeSkills_MethodNotAllowed(t *testing.T) {
	setupSkillsEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkills, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestServeSkillsRefresh_PerRepoFailureIsNot5xx pins the contract that one
// unreachable repository does not fail the whole request: the other repos still
// synced, and the caller needs the per-repo reason inline.
func TestServeSkillsRefresh_PerRepoFailureIsNot5xx(t *testing.T) {
	setupSkillsEnv(t)

	// A slug pointing at a path that cannot be a git remote makes the sync fail
	// without needing network access.
	model.ConfigInstance.Skills.Repos = []model.SkillRepo{
		{URL: "file:///nonexistent/repo.git", Slug: "broken-repo"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/skills/refresh", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkillsRefresh, req)

	require.Equal(t, http.StatusOK, w.Code, "a single repo failure must not be a server error")

	var resp skillsRefreshResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.OK, "ok must reflect that something failed")
	assert.Contains(t, resp.Errors, "broken-repo")
	// The previously discovered skill is still reported: a failed sync must not
	// drop what was already found.
	assert.GreaterOrEqual(t, resp.SkillCount, 1)
}

func TestServeSkillsRefresh_NoReposSucceeds(t *testing.T) {
	setupSkillsEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/skills/refresh", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkillsRefresh, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp skillsRefreshResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.Empty(t, resp.Errors)
}

func TestServeSkillsRefresh_MethodNotAllowed(t *testing.T) {
	setupSkillsEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/skills/refresh", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkillsRefresh, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestServeSkillsRescan_RescansWithoutNetwork pins the pure-local rescan: a
// skill dropped into a directory by hand must be discovered immediately, with
// no git IO. The configured repo points at a path that cannot be a remote, so
// if rescan tried to clone/pull it would record an error — it must not.
func TestServeSkillsRescan_RescansWithoutNetwork(t *testing.T) {
	setupSkillsEnv(t)

	// A repo whose sync would fail; rescan must leave its state untouched.
	model.ConfigInstance.Skills.Repos = []model.SkillRepo{
		{URL: "file:///nonexistent/repo.git", Slug: "broken-repo"},
	}

	// Drop a new skill into the user directory AFTER the initial scan.
	userDir := model.ConfigInstance.Skills.Dirs[0]
	writeSkillMD(t, filepath.Join(userDir, "fresh"), "fresh")

	req := httptest.NewRequest(http.MethodPost, "/api/skills/rescan", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkillsRescan, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsRescanResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.OK)
	assert.GreaterOrEqual(t, resp.SkillCount, 2, "the newly added skill must be discovered")

	// No network IO happened, so no sync state may have been written.
	assert.Zero(t, model.ConfigInstance.Skills.LastSyncAt,
		"rescan must not stamp a sync time")
	assert.Empty(t, model.ConfigInstance.Skills.LastError,
		"rescan must not record a sync error")
	assert.Empty(t, model.ConfigInstance.Skills.Repos[0].LastError)
}

func TestServeSkillsRescan_MethodNotAllowed(t *testing.T) {
	setupSkillsEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/skills/rescan", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkillsRescan, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// TestServeSkills_ListSortsByNameThenPath pins the deterministic ordering the
// UI relies on: primary by name, tie-broken by path so two skills sharing a
// name (one per source) do not shuffle between requests.
func TestServeSkills_ListSortsByNameThenPath(t *testing.T) {
	root := t.TempDir()
	isolateSkillGlobals(t)
	model.DataDir = root
	setSkillTestHome(t, t.TempDir())
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true}}

	// Two sources, each with a "zeta" skill; plus one "alpha". Different dirs
	// give the same-name entries distinct paths.
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	writeSkillMD(t, filepath.Join(dirA, "zeta"), "zeta")
	writeSkillMD(t, filepath.Join(dirB, "zeta"), "zeta")
	writeSkillMD(t, filepath.Join(dirA, "alpha"), "alpha")

	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		{ID: "a1", Backend: "a1", NativeSkillsDirs: []string{dirA, dirB}},
	}
	agent := &model.Agent{ID: "a1", Backend: "a1"}
	model.ReplaceAgents(map[string]*model.Agent{"a1": agent}, []*model.Agent{agent})
	skill.ResetForTest()
	skill.Global().ScanAll()

	req := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkills, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Skills, 3)

	// alpha first; the two zetas are adjacent and ordered by path.
	assert.Equal(t, "alpha", resp.Skills[0].Name)
	assert.Equal(t, "zeta", resp.Skills[1].Name)
	assert.Equal(t, "zeta", resp.Skills[2].Name)
	assert.Less(t, resp.Skills[1].Path, resp.Skills[2].Path,
		"same-name skills must be ordered by path so the list is stable")
}

// --- PersistSkillSyncState ---

// TestPersistSkillSyncState_WritesOutcome pins the happy path: the worker's
// result is mirrored into config.yaml (last_error, last_sync_at and the
// per-repo last_error), so the settings page shows it across restarts.
func TestPersistSkillSyncState_WritesOutcome(t *testing.T) {
	isolateSkillGlobals(t)

	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{
			Enabled: true,
			Repos: []model.SkillRepo{
				{URL: "https://example.com/a.git", Slug: "a", LastError: "old"},
				{URL: "https://example.com/b.git", Slug: "b"},
			},
		},
	}

	// recordSyncResult populates the in-memory config before persisting, so
	// emulate that by setting the fields the writer reads.
	model.ConfigInstance.Skills.LastError = "boom"
	model.ConfigInstance.Skills.LastSyncAt = 1234
	model.ConfigInstance.Skills.Repos[0].LastError = "boom"

	require.NoError(t, PersistSkillSyncState(skill.SyncResult{SkillCount: 1}))

	// The YAML on disk must carry the sync state.
	data, err := os.ReadFile(filepath.Join(model.DataDir, "config", "config.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "last_sync_at")
	assert.Contains(t, string(data), "boom")
}

// TestPersistSkillSyncState_RestoresConfigOnWriteFailure pins the rollback
// branch: when the YAML write fails, the function returns the error AND leaves
// the in-memory config exactly as it was at entry (the snapshot restore), so a
// failed persist never leaves a half-updated view behind.
func TestPersistSkillSyncState_RestoresConfigOnWriteFailure(t *testing.T) {
	isolateSkillGlobals(t)

	// Point DataDir at a path that cannot be created (a file where a directory
	// is expected) so writeConfigYAML fails.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	model.DataDir = blocker

	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{
			Enabled:    true,
			LastError:  "at-entry",
			LastSyncAt: 7,
		},
	}

	err := PersistSkillSyncState(skill.SyncResult{})
	require.Error(t, err, "a failed write must surface")
	// The snapshot is taken at entry, so the config must be unchanged.
	assert.Equal(t, "at-entry", model.ConfigInstance.Skills.LastError)
	assert.Equal(t, int64(7), model.ConfigInstance.Skills.LastSyncAt)
}

// --- PATCH /api/config validation ---

func TestServeConfig_Patch_SkillsApplied(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	isolateSkillGlobals(t)

	model.ConfigInstance = model.Config{}
	model.DataDir = t.TempDir()

	// The PATCH validator requires ABSOLUTE dirs, and a POSIX literal like
	// "/tmp/skills" is not absolute on Windows. Build the fixture with the host
	// separator so the request is valid everywhere.
	dirA := filepath.Join(t.TempDir(), "skills")
	dirB := filepath.Join(t.TempDir(), "more-skills")
	dirsJSON, err := json.Marshal([]string{dirA, dirB})
	require.NoError(t, err)
	body := fmt.Sprintf(
		`{"skills":{"enabled":true,"dirs":%s,"refresh_hours":3,"repos":[{"url":"https://github.com/org/skills.git"}]}}`,
		dirsJSON)
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)

	require.Equal(t, http.StatusOK, w.Code)

	assert.True(t, model.ConfigInstance.Skills.Enabled)
	assert.Equal(t, []string{dirA, dirB}, model.ConfigInstance.Skills.Dirs)
	assert.Equal(t, 3, model.ConfigInstance.Skills.RefreshHours)
	require.Len(t, model.ConfigInstance.Skills.Repos, 1)
	assert.Equal(t, "https://github.com/org/skills.git", model.ConfigInstance.Skills.Repos[0].URL)
	// A slug is derived so the checkout directory is stable.
	assert.NotEmpty(t, model.ConfigInstance.Skills.Repos[0].Slug)

	// All four fields are hot-reloadable — no restart dialog.
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["needs_restart"].(bool))
}

// Adding a local skill directory must make its skills visible immediately.
//
// The PATCH handler invalidates and rescans the registry (settings.go, the
// skills branch) precisely so a newly configured directory shows up without a
// restart or a manual sync. Without that rescan the config is persisted, the
// next GET /api/skills still lists the old sources, and the user sees an empty
// "已发现的技能" card until the server is restarted.
//
// This asserts the end-to-end contract rather than calling ScanAll by hand:
// PATCH the config, then GET the listing and require the new skill to be there.
func TestServeConfig_Patch_SkillsDirsTriggersScan(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	isolateSkillGlobals(t)

	root := t.TempDir()
	model.DataDir = root
	// Keep the scan deterministic: no native dirs, no shared ~/.agents/skills.
	setSkillTestHome(t, t.TempDir())
	model.BackendRegistry = []model.BackendSpec{}
	model.ReplaceAgents(nil, nil)
	model.ConfigInstance = model.Config{Skills: model.SkillsConfig{Enabled: true}}
	skill.ResetForTest()

	// A directory that does not exist yet at PATCH time, so the discovery
	// cannot have happened before it.
	newDir := filepath.Join(root, "added")
	writeSkillMD(t, filepath.Join(newDir, "fresh"), "fresh")

	dirsJSON, err := json.Marshal([]string{newDir})
	require.NoError(t, err)
	body := fmt.Sprintf(`{"skills":{"dirs":%s}}`, dirsJSON)
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeConfig, req).Code)

	// No manual ScanAll: the PATCH must have rescanned.
	listReq := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(listReq, model.SessionToken)
	w := callHandler(ServeSkills, listReq)
	require.Equal(t, http.StatusOK, w.Code)

	var resp skillsListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	names := make([]string, 0, len(resp.Skills))
	for _, s := range resp.Skills {
		names = append(names, s.Name)
	}
	assert.Contains(t, names, "fresh",
		"a directory added via PATCH must be scanned before the response returns")
}

func TestServeConfig_Patch_SkillsRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"relative dir", `{"skills":{"dirs":["relative/path"]}}`},
		{"empty dir entry", `{"skills":{"dirs":[""]}}`},
		{"empty repo url", `{"skills":{"repos":[{"url":""}]}}`},
		{"malformed repo url", `{"skills":{"repos":[{"url":"::not a url::"}]}}`},
		{"negative refresh hours", `{"skills":{"refresh_hours":-1}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, teardown := setupTestEnv(t)
			defer teardown()
			isolateSkillGlobals(t)

			model.ConfigInstance = model.Config{}
			model.DataDir = t.TempDir()

			req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			withAuthCookie(req, model.SessionToken)
			w := callHandler(ServeConfig, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", tc.body)
		})
	}
}

// TestServeConfig_Patch_RepoTokenIsWriteOnly pins the write-only contract: the
// client never receives a token, so an entry without one must keep the stored
// token rather than silently clearing the credential.
func TestServeConfig_Patch_RepoTokenIsWriteOnly(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	isolateSkillGlobals(t)

	model.ConfigInstance = model.Config{}
	model.DataDir = t.TempDir()

	// First PATCH stores a token.
	body := `{"skills":{"repos":[{"url":"https://github.com/org/skills.git","token":"s3cret"}]}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeConfig, req).Code)
	require.Len(t, model.ConfigInstance.Skills.Repos, 1)
	require.Equal(t, "s3cret", model.ConfigInstance.Skills.Repos[0].Token)

	// A later PATCH of the same URL without a token must preserve it.
	body = `{"skills":{"repos":[{"url":"https://github.com/org/skills.git"}]}}`
	req = httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	require.Equal(t, http.StatusOK, callHandler(ServeConfig, req).Code)

	require.Len(t, model.ConfigInstance.Skills.Repos, 1)
	assert.Equal(t, "s3cret", model.ConfigInstance.Skills.Repos[0].Token,
		"an entry without a token must inherit the stored one, not clear it")
}

// TestServeConfig_Get_SkillsNeverLeaksToken pins that GET /api/config reports
// only whether a token exists.
func TestServeConfig_Get_SkillsNeverLeaksToken(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	isolateSkillGlobals(t)

	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{
			Enabled: true,
			Dirs:    []string{"/tmp/skills"},
			Repos:   []model.SkillRepo{{URL: "https://github.com/a/b.git", Slug: "b-1234", Token: "top-secret"}},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	require.Equal(t, http.StatusOK, w.Code)

	assert.NotContains(t, w.Body.String(), "top-secret")

	var resp configResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Skills.Repos, 1)
	assert.True(t, resp.Skills.Repos[0].HasToken)
	assert.Equal(t, "b-1234", resp.Skills.Repos[0].Slug)
}

// TestServeSkills_SharedOmitsAgentID pins that a shared (.agents/skills) skill
// reports no agent id.
//
// The directory is read by several backends, so the id would be the roster of
// whoever declares the path — not the owner. Emitting it is what let the UI
// render shared skills as "Agent codex,copilot,dsh,…".
func TestServeSkills_SharedOmitsAgentID(t *testing.T) {
	isolateSkillGlobals(t)

	root := t.TempDir()
	model.DataDir = root
	// The shared directory resolves against $HOME, so isolate it and create the
	// real .agents/skills layout.
	home := t.TempDir()
	setSkillTestHome(t, home)
	model.ConfigInstance = model.Config{
		Skills: model.SkillsConfig{Enabled: true, Dirs: []string{filepath.Join(root, "user")}},
	}

	sharedRoot := filepath.Join(home, skill.SharedSkillsDir)
	writeSkillMD(t, filepath.Join(sharedRoot, "shared-skill"), "shared-skill")
	// A genuine other-agent directory, which MUST still report its agent.
	otherDir := filepath.Join(root, "other-native")
	writeSkillMD(t, filepath.Join(otherDir, "other-skill"), "other-skill")

	model.GetBackendRegistry()
	model.BackendRegistry = []model.BackendSpec{
		// a1 reads the shared dir; a2 reads it too plus its own other-agent dir.
		{ID: "a1", Backend: "a1", NativeSkillsDirs: []string{skill.SharedSkillsDir}},
		{ID: "a2", Backend: "a2", NativeSkillsDirs: []string{skill.SharedSkillsDir, otherDir}},
	}
	model.ReplaceAgents(map[string]*model.Agent{
		"a1": {ID: "a1", Backend: "a1"},
		"a2": {ID: "a2", Backend: "a2"},
	}, []*model.Agent{{ID: "a1", Backend: "a1"}, {ID: "a2", Backend: "a2"}})
	skill.ResetForTest()
	// ServeSkills reads the registry; it does not scan. Trigger the scan the way
	// the startup path does.
	skill.Global().ScanAll()

	req := httptest.NewRequest(http.MethodGet, "/api/skills", http.NoBody)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSkills, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Skills []struct {
			Name    string `json:"name"`
			Shared  bool   `json:"shared"`
			AgentID string `json:"agent_id"`
		} `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	type row struct {
		shared  bool
		agentID string
	}
	byName := map[string]row{}
	for _, s := range resp.Skills {
		byName[s.Name] = row{s.Shared, s.AgentID}
	}

	require.Contains(t, byName, "shared-skill")
	assert.True(t, byName["shared-skill"].shared)
	assert.Empty(t, byName["shared-skill"].agentID,
		"a shared skill must not name the backends that read the directory")

	// The other agent's own directory still identifies its agent.
	require.Contains(t, byName, "other-skill")
	assert.False(t, byName["other-skill"].shared)
	assert.Equal(t, "a2", byName["other-skill"].agentID)
}

// writeSkillMD creates dir/SKILL.md with the given skill name.
func writeSkillMD(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: "+name+"\ndescription: d\n---\n"), 0o644))
}
