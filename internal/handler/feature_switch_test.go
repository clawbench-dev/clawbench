package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveFeatureSwitchState restores the process-wide switches and config after a
// test. They are globals, so a leaked "false" would break every later test in
// this package.
func saveFeatureSwitchState(t *testing.T) {
	t.Helper()
	origCfg := model.ConfigInstance
	origTasks := model.TasksEnabled
	origShare := model.ShareEnabled
	origPolicy := model.FeaturePolicy
	origDataDir := model.DataDir
	origScheduler := service.GlobalScheduler

	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.TasksEnabled = origTasks
		model.ShareEnabled = origShare
		model.FeaturePolicy = origPolicy
		model.DataDir = origDataDir
		service.GlobalScheduler = origScheduler
	})
}

// GET /api/config must expose both new switches; the frontend drives the whole
// plugin manager from these fields.
func TestServeConfig_GetExposesTasksAndShareSwitches(t *testing.T) {
	saveFeatureSwitchState(t)

	model.ConfigInstance = model.Config{
		Tasks: model.TasksConfig{Enabled: true},
		Share: model.ShareConfig{Enabled: false},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", http.NoBody)
	w := callHandler(ServeConfig, req)
	assertOK(t, w)

	var resp struct {
		Tasks struct {
			Enabled bool `json:"enabled"`
		} `json:"tasks"`
		Share struct {
			Enabled bool `json:"enabled"`
		} `json:"share"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Tasks.Enabled)
	assert.False(t, resp.Share.Enabled)
}

// Turning tasks off must stop the scheduler AND cancel what is already running,
// then turning it back on must re-enable execution without touching anything
// that was cancelled.
func TestServeConfig_PatchTasksSwitchCancelsAndRestarts(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{Tasks: model.TasksConfig{Enabled: true}}

	s := service.NewScheduler()
	t.Cleanup(s.Stop)
	service.GlobalScheduler = s

	cancelled := make(chan struct{})
	var once sync.Once
	s.AddRunningExecution(&service.RunningExecution{
		ID:         "exec-switch",
		TaskID:     7,
		CancelFunc: func() { once.Do(func() { close(cancelled) }) },
		Phase:      service.RunningPhaseAI,
	})

	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		withAuthCookie(req, model.SessionToken)
		return callHandler(ServeConfig, req)
	}

	w := patch(`{"tasks":{"enabled":false}}`)
	assertOK(t, w)
	assert.False(t, model.TasksEnabled, "the global must follow the saved value")
	assert.True(t, s.TasksDisabled(), "the scheduler must stop firing")
	select {
	case <-cancelled:
	default:
		t.Fatal("running executions must be cancelled when tasks are turned off")
	}

	w = patch(`{"tasks":{"enabled":true}}`)
	assertOK(t, w)
	assert.True(t, model.TasksEnabled)
	assert.False(t, s.TasksDisabled(), "the scheduler must be usable again")
}

// Turning share off must revoke every kind of link, and turning it back on must
// NOT resurrect them.
func TestServeConfig_PatchShareSwitchRevokesLinks(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{Share: model.ShareConfig{Enabled: true}}

	_, err := store.WriteExec(
		`INSERT INTO file_shares (token, path, name, root, project_id) VALUES ('ftok', '/p/a.md', 'a.md', '/p', 0)`)
	require.NoError(t, err)
	_, err = store.WriteExec(
		`INSERT INTO session_shares (token, session_id, message_ids) VALUES ('stok', 'sess-x', '[1]')`)
	require.NoError(t, err)
	_, err = store.WriteExec(
		`INSERT INTO live_notes (token, project_id, title, status, share_enabled) VALUES ('mtok', 1, 'M', 'ready', 1)`)
	require.NoError(t, err)

	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		withAuthCookie(req, model.SessionToken)
		return callHandler(ServeConfig, req)
	}

	w := patch(`{"share":{"enabled":false}}`)
	assertOK(t, w)
	assert.False(t, model.ShareEnabled)

	var count int
	require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM file_shares`).Scan(&count))
	assert.Zero(t, count, "file shares must be revoked")
	require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM session_shares`).Scan(&count))
	assert.Zero(t, count, "session shares must be revoked")
	require.NoError(t, store.ReadDB().QueryRow(`SELECT share_enabled FROM live_notes WHERE token = 'mtok'`).Scan(&count))
	assert.Zero(t, count, "meeting publication must be cleared")

	// Re-enabling is not a restore.
	w = patch(`{"share":{"enabled":true}}`)
	assertOK(t, w)
	assert.True(t, model.ShareEnabled)
	require.NoError(t, store.ReadDB().QueryRow(`SELECT COUNT(*) FROM file_shares`).Scan(&count))
	assert.Zero(t, count, "re-enabling must not bring revoked links back")
}

// GET /api/config must expose the tuning fields the four new settings pages
// render. Missing fields read as empty rows in the UI.
func TestServeConfig_GetExposesSubSettings(t *testing.T) {
	saveFeatureSwitchState(t)

	model.ConfigInstance = model.Config{
		Tasks: model.TasksConfig{
			Enabled: true, RetentionEnabled: true, RetentionDays: 7,
			NotifyOnSuccess: false, NotifyOnFailure: true, Timezone: "Asia/Shanghai",
		},
		Share: model.ShareConfig{
			Enabled: true, AllowFiles: false, AllowSessions: true, AllowMeetings: false,
			AllowDownload: false, LinkTTLDays: 14, ShowProjectName: false,
		},
		KB: model.KBConfig{
			Enabled: true, AutoInject: false, InjectTopN: 3, InjectMaxChars: 500,
			InjectMinSimilarity: 0.4, MaxFileMB: 10, MaxTextMB: 5,
			PollInterval: "3s", EmbedBatchSize: 8,
		},
		GitHistory: model.GitHistoryConfig{PageSize: 50},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", http.NoBody)
	w := callHandler(ServeConfig, req)
	assertOK(t, w)

	var resp struct {
		Tasks struct {
			Enabled          bool   `json:"enabled"`
			RetentionEnabled bool   `json:"retention_enabled"`
			RetentionDays    int    `json:"retention_days"`
			NotifyOnSuccess  bool   `json:"notify_on_success"`
			NotifyOnFailure  bool   `json:"notify_on_failure"`
			Timezone         string `json:"timezone"`
		} `json:"tasks"`
		Share struct {
			AllowFiles      bool `json:"allow_files"`
			AllowSessions   bool `json:"allow_sessions"`
			AllowMeetings   bool `json:"allow_meetings"`
			AllowDownload   bool `json:"allow_download"`
			LinkTTLDays     int  `json:"link_ttl_days"`
			ShowProjectName bool `json:"show_project_name"`
		} `json:"share"`
		KB struct {
			Enabled        bool    `json:"enabled"`
			InjectTopN     int     `json:"inject_top_n"`
			PollInterval   string  `json:"poll_interval"`
			EmbedBatchSize int     `json:"embed_batch_size"`
			MinSimilarity  float64 `json:"inject_min_similarity"`
		} `json:"kb"`
		GitHistory struct {
			PageSize int `json:"page_size"`
		} `json:"git_history"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Tasks.RetentionEnabled)
	assert.Equal(t, 7, resp.Tasks.RetentionDays)
	assert.False(t, resp.Tasks.NotifyOnSuccess)
	assert.Equal(t, "Asia/Shanghai", resp.Tasks.Timezone)
	assert.False(t, resp.Share.AllowFiles)
	assert.Equal(t, 14, resp.Share.LinkTTLDays)
	assert.False(t, resp.Share.ShowProjectName)
	assert.True(t, resp.KB.Enabled)
	assert.Equal(t, 3, resp.KB.InjectTopN)
	assert.Equal(t, "3s", resp.KB.PollInterval)
	assert.Equal(t, 8, resp.KB.EmbedBatchSize)
	assert.InDelta(t, 0.4, resp.KB.MinSimilarity, 0.0001)
	assert.Equal(t, 50, resp.GitHistory.PageSize)
}

// Every new key must survive a PATCH round trip (whitelisted AND applied).
func TestServeConfig_PatchSubSettingsRoundTrip(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{Tasks: model.TasksConfig{Enabled: true}, Share: model.ShareConfig{Enabled: true}}

	body := `{
		"tasks":{"retention_enabled":true,"retention_days":9,"notify_on_success":false,"notify_on_failure":false,"timezone":"Europe/Berlin"},
		"share":{"allow_files":false,"allow_sessions":false,"allow_meetings":false,"allow_download":false,"link_ttl_days":3,"show_project_name":false},
		"git_history":{"page_size":12}
	}`
	req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeConfig, req)
	assertOK(t, w)

	assert.True(t, model.ConfigInstance.Tasks.RetentionEnabled)
	assert.Equal(t, 9, model.ConfigInstance.Tasks.RetentionDays)
	assert.Equal(t, "Europe/Berlin", model.ConfigInstance.Tasks.Timezone)
	assert.False(t, model.FeaturePolicy.TasksNotifyOnSuccess)
	assert.False(t, model.FeaturePolicy.TasksNotifyOnFailure)
	assert.False(t, model.FeaturePolicy.ShareAllowFiles)
	assert.False(t, model.FeaturePolicy.ShareAllowSessions)
	assert.False(t, model.FeaturePolicy.ShareAllowMeetings)
	assert.False(t, model.FeaturePolicy.ShareAllowDownload)
	assert.Equal(t, 3, model.ConfigInstance.Share.LinkTTLDays)
	assert.False(t, model.FeaturePolicy.ShareShowProjectName)
	assert.Equal(t, 12, model.ConfigInstance.GitHistory.PageSize)
}

func TestServeConfig_PatchRejectsInvalidSubSettings(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	model.DataDir = t.TempDir()
	model.ConfigInstance = model.Config{Tasks: model.TasksConfig{Enabled: true}, Share: model.ShareConfig{Enabled: true}}

	patch := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		withAuthCookie(req, model.SessionToken)
		return callHandler(ServeConfig, req)
	}

	assertStatus(t, patch(`{"tasks":{"timezone":"Not/AZone"}}`), http.StatusBadRequest)
	assertStatus(t, patch(`{"tasks":{"retention_days":0}}`), http.StatusBadRequest)
	assertStatus(t, patch(`{"share":{"link_ttl_days":-1}}`), http.StatusBadRequest)
	assertStatus(t, patch(`{"git_history":{"page_size":0}}`), http.StatusBadRequest)
}

// The per-kind switches are real creation gates, not just UI hints.
func TestShareAllowSwitchesGateCreation(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	model.FeaturePolicy.ShareAllowFiles = false
	req := newRequest(t, http.MethodPost, "/api/share", map[string]any{"path": "a.md"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)
	assertStatus(t, w, http.StatusConflict)
	assert.Contains(t, w.Body.String(), "ShareFilesDisabled")

	model.FeaturePolicy.ShareAllowSessions = false
	req = newRequest(t, http.MethodPost, "/api/share/session", map[string]any{"sessionId": "sess-x"})
	req = withProjectCookie(req, env.ProjectDir)
	w = callHandler(ServeSessionShareManage, req)
	assertStatus(t, w, http.StatusConflict)
	assert.Contains(t, w.Body.String(), "ShareSessionsDisabled")
}

// Downloads can be switched off without breaking previews.
func TestShareDownloadSwitchForbidsDownload(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	createTestFile(t, env.ProjectDir, "notes.md", "# hi\n")
	absPath := filepath.Join(env.ProjectDir, "notes.md")
	token, _, err := service.UpsertFileShare(absPath, "notes.md", env.ProjectDir, env.ProjectDir)
	require.NoError(t, err)

	model.FeaturePolicy.ShareAllowDownload = false

	req := httptest.NewRequest(http.MethodGet, "/api/share/"+token+"/download", http.NoBody)
	w := callHandler(ServeSharePublic, req)
	assertStatus(t, w, http.StatusForbidden)
	assert.Contains(t, w.Body.String(), "ShareDownloadDisabled")
}

func TestServeTasks_CreateRejectedWhenTasksDisabled(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	model.TasksEnabled = false

	req := newRequest(t, http.MethodPost, "/api/tasks", map[string]any{
		"name": "blocked", "agent_id": "codebuddy", "prompt": "p",
	})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTasks, req)

	assertStatus(t, w, http.StatusConflict)
	assert.Contains(t, w.Body.String(), "TasksDisabled")
}

func TestServeTaskByID_TriggerRejectedWhenTasksDisabled(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	s := service.NewScheduler()
	t.Cleanup(s.Stop)
	service.GlobalScheduler = s

	task := &model.ScheduledTask{
		ProjectPath: env.ProjectDir,
		Name:        "blocked-trigger",
		CronExpr:    "0 * * * *",
		AgentID:     "codebuddy",
		Prompt:      "p",
		RepeatMode:  "unlimited",
	}
	require.NoError(t, s.AddTask(task))

	model.TasksEnabled = false

	req := newRequest(t, http.MethodPut, fmt.Sprintf("/api/tasks/%d", task.ID), map[string]any{"action": "trigger"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeTaskByID, req)

	assertStatus(t, w, http.StatusConflict)
	assert.Contains(t, w.Body.String(), "TasksDisabled")
}

func TestServeShareCreate_RejectedWhenShareDisabled(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()
	saveFeatureSwitchState(t)

	model.ShareEnabled = false

	req := newRequest(t, http.MethodPost, "/api/share", map[string]any{"path": "a.md"})
	req = withProjectCookie(req, env.ProjectDir)
	w := callHandler(ServeShareManage, req)

	assertStatus(t, w, http.StatusConflict)
	assert.Contains(t, w.Body.String(), "ShareDisabled")
}

// An old link must 404 — not render the SPA shell with a "not found" state,
// which would leave the URL looking alive.
func TestServeSharePage_NotFoundWhenShareDisabled(t *testing.T) {
	saveFeatureSwitchState(t)

	model.ShareEnabled = false

	req := httptest.NewRequest(http.MethodGet, "/share/"+strings.Repeat("a", 32), http.NoBody)
	w := callHandler(ServeSharePage, req)

	assertStatus(t, w, http.StatusNotFound)
}
