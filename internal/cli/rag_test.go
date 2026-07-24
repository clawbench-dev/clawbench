package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRAGCommand_NoArgs(t *testing.T) {
	// No args now prints help and returns 0
	exitCode := RunRAGCommand([]string{})
	assert.Equal(t, 0, exitCode)
}

func TestRunRAGCommand_HelpFlag(t *testing.T) {
	exitCode := RunRAGCommand([]string{"--help"})
	assert.Equal(t, 0, exitCode)
}

func TestRunRAGCommand_ShortHelpFlag(t *testing.T) {
	exitCode := RunRAGCommand([]string{"-h"})
	assert.Equal(t, 0, exitCode)
}

func TestRunRAGCommand_UnknownSubcommand(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{Port: 30000}

	exitCode := RunRAGCommand([]string{"foo"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSearch_MissingQuery(t *testing.T) {
	exitCode := RunRAGCommand([]string{"search"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGMessage_MissingID(t *testing.T) {
	exitCode := RunRAGCommand([]string{"message"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGMessage_InvalidID(t *testing.T) {
	exitCode := RunRAGCommand([]string{"message", "--id", "abc"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSession_MissingID(t *testing.T) {
	exitCode := RunRAGCommand([]string{"session"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSearch_ServerNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{
		Port: 59999,
	}

	exitCode := RunRAGCommand([]string{"search", "-q", "test query"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGMessage_ServerNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{
		Port: 59999,
	}

	exitCode := RunRAGCommand([]string{"message", "--id", "42"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSession_ServerNotReachable(t *testing.T) {
	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{
		Port: 59999,
	}

	exitCode := RunRAGCommand([]string{"session", "--id", "test-session-id"})
	assert.Equal(t, 1, exitCode)
}

// ---------- runRAGMessage / runRAGSession with working server ----------

func TestRAGMessage_ServerSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/rag/message")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"message":  "test message content",
			"id":       42,
			"role":     "user",
		})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"message", "--id", "42"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGMessage_PositionalID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"message": "positional id test",
		})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"message", "42"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGMessage_WithProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify project cookie is set
		var hasProjectCookie bool
		for _, c := range r.Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				hasProjectCookie = true
			}
		}
		assert.True(t, hasProjectCookie, "project cookie should be set")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "with project"})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"message", "--id", "42", "--project", "/my/project"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGMessage_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{"error": "not found"})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"message", "--id", "999"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSession_ServerSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/api/rag/session")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"session_id": "test-session-123",
			"messages":  []any{},
			"total":     0,
		})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"session", "--id", "test-session-123"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGSession_PositionalID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"session_id": "positional-session",
			"messages":  []any{},
		})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"session", "positional-session"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGSession_WithProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var hasProjectCookie bool
		for _, c := range r.Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				hasProjectCookie = true
			}
		}
		assert.True(t, hasProjectCookie, "project cookie should be set")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"session_id": "session-with-project",
		})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"session", "--id", "session-with-project", "--project", "/my/project"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGSession_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "internal error"})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"session", "--id", "bad-session"})
	assert.Equal(t, 1, exitCode)
}

func TestRAGSearch_ServerSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/api/rag/search")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"results": []any{},
			"total":  0,
		})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"search", "-q", "test query"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGSearch_WithProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var hasProjectCookie bool
		for _, c := range r.Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				hasProjectCookie = true
			}
		}
		assert.True(t, hasProjectCookie, "project cookie should be set for search with project")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": []any{}})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"search", "-q", "test", "--project", "/my/project"})
	assert.Equal(t, 0, exitCode)
}

func TestRAGSearch_WithFilters(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "results": []any{}})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{
		"search", "-q", "test",
		"--limit", "5",
		"--backend", "claude",
		"--role", "user",
		"--session-id", "sess-123",
		"--exclude-session-id", "sess-456",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
	})
	require.Equal(t, 0, exitCode)
	assert.Equal(t, "test", receivedBody["q"])
	assert.Equal(t, float64(5), receivedBody["limit"])
	assert.Equal(t, "claude", receivedBody["backend"])
	assert.Equal(t, "user", receivedBody["role"])
	assert.Equal(t, "sess-123", receivedBody["session_id"])
	assert.Equal(t, "sess-456", receivedBody["exclude_session_id"])
	assert.Equal(t, "2024-01-01", receivedBody["from"])
	assert.Equal(t, "2024-12-31", receivedBody["to"])
}

func TestRAGSearch_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "search failed"})
	}))
	defer server.Close()

	setupRAGTestConfig(t, server)

	exitCode := RunRAGCommand([]string{"search", "-q", "test"})
	assert.Equal(t, 1, exitCode)
}

// setupRAGTestConfig configures model state so RAG commands connect to the test server.
func setupRAGTestConfig(t *testing.T, server *httptest.Server) {
	t.Helper()
	origCfg := model.ConfigInstance
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.BinDir = origBinDir
		model.DataDir = origDataDir
	})

	// Extract port from server URL
	var port int
	_, err := parsePortFromURL(server.URL, &port)
	require.NoError(t, err)

	tmpDir := t.TempDir()
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	model.ConfigInstance = model.Config{Port: port}
}

func parsePortFromURL(u string, port *int) (bool, error) {
	// Simple port extraction from http://127.0.0.1:PORT
	for i := len(u) - 1; i >= 0; i-- {
		if u[i] == ':' {
			*port = 0
			for j := i + 1; j < len(u); j++ {
				*port = *port*10 + int(u[j]-'0')
			}
			return true, nil
		}
	}
	return false, nil
}
