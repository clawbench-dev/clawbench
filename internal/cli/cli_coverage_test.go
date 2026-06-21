package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// helper: start a test server and configure model.ConfigInstance.Port to match
func setupTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server { //nolint:unparam // test helper: server used by caller indirectly
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Extract port from server URL (http://127.0.0.1:PORT)
	parts := strings.Split(server.URL, ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: port}

	return server
}

// captureStdout captures stdout during fn execution
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	return strings.TrimSpace(string(buf[:n]))
}

// ---------- FindConfigPath ----------

func TestFindConfigPath_BinDirConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(configDir, 0o755)
	_ = os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("port: 12345"), 0o644)

	path := FindConfigPath(tmpDir)
	assert.Equal(t, filepath.Join(tmpDir, "config", "config.yaml"), path)
}

func TestFindConfigPath_FallbackToCWD(t *testing.T) {
	tmpDir := t.TempDir()
	// No config dir under tmpDir — should fall back to CWD-relative path
	path := FindConfigPath(tmpDir)
	assert.Equal(t, filepath.Join("config", "config.yaml"), path)
}

// ---------- checkHTTPResponse ----------

func TestCheckHTTPResponse_OK(t *testing.T) {
	err := checkHTTPResponse(map[string]any{}, http.StatusOK, "test")
	assert.NoError(t, err)
}

func TestCheckHTTPResponse_NonOK_WithErrorMessage(t *testing.T) {
	result := map[string]any{"error": "not found"}
	err := checkHTTPResponse(result, http.StatusNotFound, "get task")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Contains(t, err.Error(), "get task")
}

func TestCheckHTTPResponse_NonOK_NoErrorMessage(t *testing.T) {
	err := checkHTTPResponse(map[string]any{}, http.StatusInternalServerError, "list tasks")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
	assert.Contains(t, err.Error(), "list tasks")
}

// ---------- httpDo with real test server ----------

func TestHTTPDo_SuccessWithServer(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/test", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "value": 42})
	})

	result, status, err := httpDo(http.MethodGet, "/api/test", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, float64(42), result["value"])
}

func TestHTTPDo_PostWithBody(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "hello", body["key"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	_, status, err := httpDo(http.MethodPost, "/api/test", map[string]any{"key": "hello"})
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
}

func TestHTTPDo_NonJSONResp(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})

	_, _, err := httpDo(http.MethodGet, "/api/test", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse response")
}

func TestHTTPDo_UnreachableServer(t *testing.T) {
	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: 59999} // nothing listening here

	_, _, err := httpDo(http.MethodGet, "/api/test", nil)
	assert.Error(t, err)
}

// ---------- httpDoWithProject with real test server ----------

func TestHTTPDoWithProject_SetsCookieAndReturnsData(t *testing.T) {
	var receivedProject string
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		for _, c := range r.Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				receivedProject = c.Value
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "tasks": []any{}})
	})

	_, status, err := httpDoWithProject(http.MethodGet, "/api/tasks", nil, "/my/project")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.NotEmpty(t, receivedProject, "clawbench_project cookie should be set")
}

// ---------- runList ----------

func TestRunList_MissingProject(t *testing.T) {
	code := runList([]string{})
	assert.Equal(t, 1, code)
}

func TestRunList_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/tasks", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
	})

	output := captureStdout(t, func() {
		code := runList([]string{"--project", "/test"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, output, "tasks")
}

func TestRunList_ServerError(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal"})
	})

	code := runList([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

// ---------- runGet ----------

func TestRunGet_MissingTaskID(t *testing.T) {
	code := runGet([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunGet_MissingProject(t *testing.T) {
	code := runGet([]string{"1"})
	assert.Equal(t, 1, code)
}

func TestRunGet_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/tasks/42")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "42", "name": "test"})
	})

	output := captureStdout(t, func() {
		code := runGet([]string{"42", "--project", "/test"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, output, "42")
}

// ---------- runListExec ----------

func TestRunListExec_MissingTaskID(t *testing.T) {
	code := runListExec([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunListExec_MissingProject(t *testing.T) {
	code := runListExec([]string{"1"})
	assert.Equal(t, 1, code)
}

func TestRunListExec_InvalidLimit(t *testing.T) {
	code := runListExec([]string{"1", "--project", "/test", "--limit", "0"})
	assert.Equal(t, 1, code)
}

func TestRunListExec_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/tasks/1/executions")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"executions": []any{}})
	})

	output := captureStdout(t, func() {
		code := runListExec([]string{"1", "--project", "/test", "--limit", "5"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, output, "executions")
}

// ---------- runDeleteExec ----------

func TestRunDeleteExec_MissingIDs(t *testing.T) {
	code := runDeleteExec([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunDeleteExec_MissingExecID(t *testing.T) {
	code := runDeleteExec([]string{"1", "--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunDeleteExec_MissingProject(t *testing.T) {
	code := runDeleteExec([]string{"1", "2"})
	assert.Equal(t, 1, code)
}

func TestRunDeleteExec_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	output := captureStdout(t, func() {
		code := runDeleteExec([]string{"1", "2", "--project", "/test"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, output, "ok")
}

// ---------- runListAgents ----------

func TestRunListAgents_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/agents", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"agents": []any{
				map[string]any{"id": "codebuddy", "name": "CodeBuddy"},
			},
		})
	})

	output := captureStdout(t, func() {
		code := runListAgents([]string{})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, output, "codebuddy")
}

func TestRunListAgents_ServerError(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "fail"})
	})

	code := runListAgents([]string{})
	assert.Equal(t, 1, code)
}

func TestRunListAgents_Unreachable(t *testing.T) {
	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: 59999}

	code := runListAgents([]string{})
	assert.Equal(t, 1, code)
}

// ---------- runPause / runResume / runTrigger ----------

func TestRunPause_MissingTaskID(t *testing.T) {
	code := runPause([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunResume_MissingTaskID(t *testing.T) {
	code := runResume([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunTrigger_MissingTaskID(t *testing.T) {
	code := runTrigger([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunPause_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	code := runPause([]string{"1", "--project", "/test"})
	assert.Equal(t, 0, code)
}

func TestRunResume_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	code := runResume([]string{"1", "--project", "/test"})
	assert.Equal(t, 0, code)
}

func TestRunTrigger_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	code := runTrigger([]string{"1", "--project", "/test"})
	assert.Equal(t, 0, code)
}

// ---------- runDelete ----------

func TestRunDelete_MissingTaskID(t *testing.T) {
	code := runDelete([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunDelete_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	code := runDelete([]string{"1", "--project", "/test"})
	assert.Equal(t, 0, code)
}

// ---------- runUpdate ----------

func TestRunUpdate_ScheduledBlock(t *testing.T) {
	_ = os.Setenv("CLAWBENCH_SCHEDULED", "1")
	defer os.Unsetenv("CLAWBENCH_SCHEDULED")

	code := runUpdate([]string{"1", "--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunUpdate_MissingTaskID(t *testing.T) {
	code := runUpdate([]string{"--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunUpdate_MissingProject(t *testing.T) {
	code := runUpdate([]string{"1"})
	assert.Equal(t, 1, code)
}

// ---------- runCreate (more coverage) ----------

func TestRunCreate_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1})
	})

	code := runCreate([]string{
		"--name", "Test",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "hello",
		"--project", "/test",
	})
	assert.Equal(t, 0, code)
}

func TestRunCreate_ServerError(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "bad request"})
	})

	code := runCreate([]string{
		"--name", "Test",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "hello",
		"--project", "/test",
	})
	assert.Equal(t, 1, code)
}

// ---------- runCreate with repeat limited + max-runs ----------

func TestRunCreate_LimitedRepeatWithMaxRuns(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1})
	})

	code := runCreate([]string{
		"--name", "Test",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "hello",
		"--project", "/test",
		"--repeat", "limited",
		"--max-runs", "5",
	})
	assert.Equal(t, 0, code)
}

// ---------- runCreate with unlimited repeat ----------

func TestRunCreate_UnlimitedRepeat(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1})
	})

	code := runCreate([]string{
		"--name", "Test",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "hello",
		"--project", "/test",
		"--repeat", "unlimited",
	})
	assert.Equal(t, 0, code)
}

// ---------- runCreate once (default) ----------

func TestRunCreate_OnceRepeat(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": 1})
	})

	code := runCreate([]string{
		"--name", "Test",
		"--cron", "0 9 * * *",
		"--agent", "codebuddy",
		"--prompt", "hello",
		"--project", "/test",
		"--repeat", "once",
	})
	assert.Equal(t, 0, code)
}

// ---------- fmt.Sprintf used ----------

func TestCLIHelpers_FmtUsed(t *testing.T) {
	// Ensure fmt import is used
	_ = fmt.Sprintf("test %d", 1)
}

// ---------- runUpdate with full field coverage ----------

func TestRunUpdate_ServerSuccess(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "update", body["action"])
		assert.Equal(t, "NewName", body["name"])
		assert.Equal(t, "0 * * * *", body["cron_expr"])
		assert.Equal(t, "claude", body["agent_id"])
		assert.Equal(t, "new prompt", body["prompt"])
		assert.Equal(t, "limited", body["repeat_mode"])
		assert.Equal(t, float64(10), body["max_runs"])

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	code := runUpdate([]string{
		"1",
		"--name", "NewName",
		"--cron", "0 * * * *",
		"--agent", "claude",
		"--prompt", "new prompt",
		"--repeat", "limited",
		"--max-runs", "10",
		"--project", "/test",
	})
	assert.Equal(t, 0, code)
}

func TestRunUpdate_InvalidRepeat(t *testing.T) {
	code := runUpdate([]string{"1", "--repeat", "bad", "--project", "/test"})
	assert.Equal(t, 1, code)
}

func TestRunUpdate_ServerError(t *testing.T) {
	setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "fail"})
	})

	code := runUpdate([]string{"1", "--name", "X", "--project", "/test"})
	assert.Equal(t, 1, code)
}

// ---------- loadConfig with config file ----------

func TestLoadConfig_FromFile(t *testing.T) {
	origCfg := model.ConfigInstance
	origBinDir := model.BinDir
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.BinDir = origBinDir
	})

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(configDir, 0o755)
	_ = os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("port: 12345\nwatch_dir: /tmp"), 0o644)

	// loadConfig() reads os.Args[0] to compute BinDir, then calls FindConfigPath.
	// Since we can't easily control os.Args[0] in tests, just test that
	// the idempotent path works and FindConfigPath works with BinDir.
	// The full loadConfig path is tested indirectly by integration tests.
	// Here, just verify FindConfigPath resolves correctly with a valid config dir.
	path := FindConfigPath(tmpDir)
	assert.Equal(t, filepath.Join(tmpDir, "config", "config.yaml"), path)

	// Verify that a YAML file at that path is parseable
	data, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "port: 12345")
}

// ---------- runCreate without cron (immediate trigger) ----------

func TestRunCreate_NoCron(t *testing.T) {
	code := runCreate([]string{
		"--name", "Test",
		"--agent", "codebuddy",
		"--prompt", "hello",
		"--project", "/test",
	})
	assert.Equal(t, 1, code) // missing --cron
}
