package handler

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"

	_ "modernc.org/sqlite"
)

// testEnv holds test environment state for setup/teardown.
type testEnv struct {
	ProjectDir      string
	WatchDir        string
	OrigToken       string
	OrigCookieToken string
	OrigRootPaths   []string
	OrigDB          *sql.DB
}

// setupTestEnv creates a temporary project directory, initializes an in-memory DB,
// and saves/restores global state. Returns env and teardown function.
func setupTestEnv(t *testing.T) (*testEnv, func()) {
	t.Helper()

	// Create temp directories — project must be under WatchDir to match production
	watchDir := t.TempDir()
	// Note: We intentionally do NOT resolve symlinks here.
	// On macOS, /var/folders → /private/var/folders, but we want model.RootPaths
	// to match what production code uses (ListRootPaths returns "/" on Unix,
	// which doesn't need resolution). The isPathUnderAnyRoot function handles
	// symlink resolution internally, so RootPaths and path arguments can use
	// either resolved or unresolved forms.
	projectDir := filepath.Join(watchDir, "project")
	_ = os.MkdirAll(projectDir, 0o755)

	// Save original globals
	origToken := model.SessionToken
	origCookieToken := model.CookieToken
	origRootPaths := model.RootPaths
	origDB := service.DB
	origDBRead := service.DBRead
	origAgents := model.Agents
	origAgentList := model.AgentList

	// Set test globals
	model.SessionToken = ""
	model.CookieToken = ""
	model.RootPaths = []string{watchDir}

	// Init in-memory SQLite
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS chat_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			files TEXT,
			session_id TEXT,
			backend TEXT NOT NULL DEFAULT 'claude',
			streaming INTEGER NOT NULL DEFAULT 0,
			indexed INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_sessions (
			id TEXT PRIMARY KEY,
			project_path TEXT NOT NULL,
			backend TEXT NOT NULL,
			title TEXT NOT NULL,
			agent_id TEXT DEFAULT '',
			agent_source TEXT DEFAULT 'default',
			model TEXT DEFAULT '',
			session_type TEXT NOT NULL DEFAULT 'chat',
			external_session_id TEXT DEFAULT '',
			source_session_id TEXT DEFAULT NULL,
			transport TEXT DEFAULT '',
			auto_approve INTEGER NOT NULL DEFAULT 0,
			deleted INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_read_at DATETIME,
			UNIQUE(project_path, backend, id)
		);
		CREATE TABLE IF NOT EXISTS recent_projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT UNIQUE NOT NULL,
			accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			is_default INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_path TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			cron_expr TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			prompt TEXT NOT NULL,
			session_id TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			repeat_mode TEXT NOT NULL DEFAULT 'unlimited',
			max_runs INTEGER DEFAULT 0,
			last_run_at DATETIME,
			next_run_at DATETIME,
			run_count INTEGER DEFAULT 0,
			last_read_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id INTEGER NOT NULL,
			session_id TEXT NOT NULL,
			trigger_type TEXT NOT NULL DEFAULT 'auto',
			status TEXT NOT NULL DEFAULT 'completed',
			read_at DATETIME,
			summary TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_executions_task ON task_executions(task_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_history_session ON chat_history(project_path, backend, session_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_sessions_project_backend ON chat_sessions(project_path, backend);
		CREATE INDEX IF NOT EXISTS idx_sessions_source_session ON chat_sessions(source_session_id) WHERE source_session_id IS NOT NULL;
		CREATE INDEX IF NOT EXISTS idx_executions_session ON task_executions(session_id);
		CREATE TABLE IF NOT EXISTS summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			target_type TEXT NOT NULL,
			target_id   INTEGER NOT NULL,
			summary     TEXT NOT NULL,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(target_type, target_id)
		);
		CREATE TABLE IF NOT EXISTS ai_raw_responses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			message_id INTEGER NOT NULL,
			backend TEXT NOT NULL DEFAULT '',
			raw_output TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS tts_summaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id   INTEGER NOT NULL,
			tts_summary  TEXT NOT NULL,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(message_id)
		);
		CREATE TABLE IF NOT EXISTS terminal_quick_commands (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT NOT NULL,
			command TEXT NOT NULL,
			hidden INTEGER NOT NULL DEFAULT 0,
			auto_execute INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS chat_quick_send (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT NOT NULL,
			command TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS terminal_key_config (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			key_id TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(type, key_id)
		);
		CREATE TABLE IF NOT EXISTS chat_metadata (
			message_id INTEGER PRIMARY KEY,
			mode TEXT DEFAULT '',
			thinking_effort TEXT DEFAULT '',
			transport TEXT DEFAULT '',
			model TEXT DEFAULT '',
			input_tokens INTEGER DEFAULT 0,
			output_tokens INTEGER DEFAULT 0,
			duration_ms INTEGER DEFAULT 0,
			wall_ms INTEGER DEFAULT 0,
			cost_usd REAL DEFAULT 0,
			stop_reason TEXT DEFAULT '',
			is_error INTEGER DEFAULT 0,
			error_message TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (message_id) REFERENCES chat_history(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_model ON chat_metadata(model);
		CREATE INDEX IF NOT EXISTS idx_chat_metadata_created ON chat_metadata(created_at);
		CREATE TABLE IF NOT EXISTS chat_tool_calls (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL REFERENCES chat_history(id) ON DELETE CASCADE,
			session_id TEXT NOT NULL,
			tool_id TEXT NOT NULL,
			name TEXT NOT NULL,
			input TEXT NOT NULL DEFAULT '{}',
			output TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			done INTEGER NOT NULL DEFAULT 0,
			summary TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(tool_id, message_id)
		);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_message ON chat_tool_calls(message_id);
		CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON chat_tool_calls(session_id, created_at DESC);
	`)
	if err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}

	// Create agent store tables (agents + agent_api_keys)
	if _, err := db.Exec(service.AgentDDL); err != nil {
		t.Fatalf("failed to create agent tables: %v", err)
	}

	service.DB = db
	service.DBRead = db // Same instance for :memory: SQLite — data is shared

	// Register mock agents so GetDefaultAgentID() works
	model.Agents = map[string]*model.Agent{
		"codebuddy": {ID: "codebuddy", Name: "Test", Backend: "codebuddy", Models: []model.AgentModel{{ID: "glm-5.1", Name: "GLM 5.1", Default: true}}},
		"claude":    {ID: "claude", Name: "Claude", Backend: "claude", Models: []model.AgentModel{{ID: "claude-sonnet-4-6", Name: "Claude Sonnet", Default: true}}},
	}
	model.AgentList = []*model.Agent{model.Agents["codebuddy"], model.Agents["claude"]}

	env := &testEnv{
		ProjectDir:      projectDir,
		WatchDir:        watchDir,
		OrigToken:       origToken,
		OrigCookieToken: origCookieToken,
		OrigRootPaths:   origRootPaths,
		OrigDB:          origDB,
	}

	teardown := func() {
		model.SessionToken = origToken
		model.CookieToken = origCookieToken
		model.RootPaths = origRootPaths
		model.Agents = origAgents
		model.AgentList = origAgentList
		service.DB = origDB
		service.DBRead = origDBRead
		_ = db.Close()
	}

	return env, teardown
}

// newRequest creates an HTTP request with optional body.
func newRequest(t *testing.T, method, path string, body interface{}) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// withProjectCookie adds the clawbench_project cookie to the request.
func withProjectCookie(req *http.Request, projectPath string) *http.Request {
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("clawbench_project"),
		Value: url.QueryEscape(projectPath),
	})
	return req
}

// withAuthCookie adds the clawbench_session cookie to the request.
func withAuthCookie(req *http.Request, token string) *http.Request { //nolint:unparam // test helper: return value unused but signature supports chaining
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName(model.SessionCookie),
		Value: token,
	})
	return req
}

// withSessionCookie adds the chat_session_id cookie to the request.
func withSessionCookie(req *http.Request, sessionID string) *http.Request {
	req.AddCookie(&http.Cookie{
		Name:  model.ScopedCookieName("chat_session_id"),
		Value: sessionID,
	})
	return req
}

// hashPassword computes the SHA-256 hash with salt, matching the auth logic.
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password + "clawbench-salt"))
	return hex.EncodeToString(hash[:])
}

// decodeRespJSON decodes the response body into target.
func decodeRespJSON(t *testing.T, body io.Reader, target interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
}

// callHandler calls a handler function with the given request and returns the response recorder.
func callHandler(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// callHandlerWithAuth calls a handler wrapped with Auth middleware.
func callHandlerWithAuth(handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	middleware.Auth(handler)(w, req)
	return w
}

// createTestFile creates a file with the given content in the project directory.
func createTestFile(t *testing.T, projectDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(projectDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

// assertOK asserts that the response status code is 200.
func assertOK(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// assertStatus asserts the response status code.
func assertStatus(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if w.Code != expected {
		t.Errorf("expected status %d, got %d; body: %s", expected, w.Code, w.Body.String())
	}
}

// assertJSONField asserts a specific field value in the JSON response.
func assertJSONField(t *testing.T, w *httptest.ResponseRecorder, field string, expected interface{}) {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v; body: %s", err, w.Body.String())
	}
	val, ok := result[field]
	if !ok {
		t.Errorf("field %q not found in response; body: %s", field, w.Body.String())
		return
	}
	if fmt.Sprintf("%v", val) != fmt.Sprintf("%v", expected) {
		t.Errorf("field %q: expected %v, got %v", field, expected, val)
	}
}
