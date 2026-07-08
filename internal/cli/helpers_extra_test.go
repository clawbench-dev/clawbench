package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- httpDoWithProject comprehensive tests ---

func TestHTTPDoWithProject_SuccessWithServer(t *testing.T) {
	var receivedCookie *http.Cookie
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range r.Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				receivedCookie = c
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": "response"})
	}))
	defer server.Close()

	// Extract port from server URL
	parts := strings.Split(server.URL, ":")
	var port int
	_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &port)

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: port}

	result, status, err := httpDoWithProject(http.MethodGet, "/api/test", nil, "/my/project")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "response", result["data"])
	assert.NotNil(t, receivedCookie)
}

func TestHTTPDoWithProject_PostWithBody(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	parts := strings.Split(server.URL, ":")
	var port int
	_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &port)

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: port}

	_, status, err := httpDoWithProject(http.MethodPost, "/api/test", map[string]any{"key": "value"}, "/my/project")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "value", receivedBody["key"])
}

func TestHTTPDoWithProject_EmptyProjectPath(t *testing.T) {
	var cookieFound bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range r.Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				cookieFound = true
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	parts := strings.Split(server.URL, ":")
	var port int
	_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &port)

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: port}

	// Empty project path should NOT set the cookie
	_, _, err := httpDoWithProject(http.MethodGet, "/api/test", nil, "")
	require.NoError(t, err)
	assert.False(t, cookieFound, "no project cookie should be set when projectPath is empty")
}

func TestHTTPDoWithProject_NonJSONResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	parts := strings.Split(server.URL, ":")
	var port int
	_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &port)

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: port}

	_, _, err := httpDoWithProject(http.MethodGet, "/api/test", nil, "/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse response")
}

func TestHTTPDoWithProject_UnreachableServer(t *testing.T) {
	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: 59999}

	_, _, err := httpDoWithProject(http.MethodGet, "/api/test", nil, "/test")
	assert.Error(t, err)
}

// --- resolveDataDir ---

func TestResolveDataDir_AlreadySet(t *testing.T) {
	origDataDir := model.DataDir
	t.Cleanup(func() { model.DataDir = origDataDir })

	model.DataDir = "/custom/data"
	dir, err := resolveDataDir()
	assert.NoError(t, err)
	assert.Equal(t, "/custom/data", dir)
}

func TestResolveDataDir_DefaultFromHome(t *testing.T) {
	origDataDir := model.DataDir
	t.Cleanup(func() { model.DataDir = origDataDir })

	model.DataDir = ""
	dir, err := resolveDataDir()
	assert.NoError(t, err)
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		assert.Equal(t, filepath.Join(homeDir, ".clawbench"), dir)
	}
}

func TestResolveDataDir_EmptyHome(t *testing.T) {
	origDataDir := model.DataDir
	t.Cleanup(func() { model.DataDir = origDataDir })

	model.DataDir = ""
	// Note: on Windows, platform.UserHomeDir() may still resolve via
	// Go's os.UserHomeDir() even if HOME/USERPROFILE are unset (e.g.
	// via Windows API). This test only verifies the error path on
	// platforms where the home dir truly cannot be determined.
	// We skip if the home dir is still resolvable after unsetting env vars.
	origHome := os.Getenv("HOME")
	origUserProfile := os.Getenv("USERPROFILE")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", origHome)
		_ = os.Setenv("USERPROFILE", origUserProfile)
	})

	_ = os.Unsetenv("HOME")
	_ = os.Unsetenv("USERPROFILE")

	dir, err := resolveDataDir()
	if err != nil {
		assert.Contains(t, err.Error(), "cannot determine home directory")
		assert.Empty(t, dir)
	} else {
		// Home dir was still resolvable (e.g. Windows API) — not an error
		assert.NotEmpty(t, dir)
	}
}

// --- loadConfig additional coverage ---

func TestLoadConfig_HomeDirFallback(t *testing.T) {
	// Verify that loadConfig sets DataDir to ~/.clawbench using platform.UserHomeDir()
	origCfg := model.ConfigInstance
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	origServerPort := model.ServerPort
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.BinDir = origBinDir
		model.DataDir = origDataDir
		model.ServerPort = origServerPort
	})

	model.ConfigInstance = model.Config{}
	model.DataDir = "" // empty to trigger homeDir fallback

	// We can't easily test the os.Exit(1) path (homeDir == ""),
	// but we can verify the normal path sets DataDir to ~/.clawbench.
	loadConfig()
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		expected := filepath.Join(homeDir, ".clawbench")
		assert.Equal(t, expected, model.DataDir, "DataDir should default to ~/.clawbench")
	}
}

func TestLoadConfig_CheckLegacyLayoutCalled(t *testing.T) {
	// Verify that loadConfig calls CheckLegacyLayout by checking it doesn't panic
	// when a legacy BinDir layout exists. CheckLegacyLayout just prints a warning.
	origCfg := model.ConfigInstance
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	origServerPort := model.ServerPort
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.BinDir = origBinDir
		model.DataDir = origDataDir
		model.ServerPort = origServerPort
	})

	tmpDir := t.TempDir()
	// Create a legacy .clawbench dir next to a fake binary dir
	legacyDataDir := filepath.Join(tmpDir, ".clawbench")
	assert.NoError(t, os.MkdirAll(legacyDataDir, 0o755))

	model.ConfigInstance = model.Config{}
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, "newdata")

	// loadConfig should not panic even with legacy layout
	loadConfig()
}

func TestLoadConfig_SetsServerPort(t *testing.T) {
	origCfg := model.ConfigInstance
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	origServerPort := model.ServerPort
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.BinDir = origBinDir
		model.DataDir = origDataDir
		model.ServerPort = origServerPort
	})

	// Reset ConfigInstance so loadConfig actually runs
	model.ConfigInstance = model.Config{}

	// loadConfig() calls FindConfigPath(DataDir).
	// We create a config file relative to DataDir.
	dataDir := t.TempDir()
	model.DataDir = dataDir
	configDir := filepath.Join(dataDir, "config")
	_ = os.MkdirAll(configDir, 0o755)
	configPath := filepath.Join(configDir, "config.yaml")

	// Write a config with a non-default port
	_ = os.WriteFile(configPath, []byte("port: 9999\n"), 0o644)

	loadConfig()

	// Verify ServerPort was set from config
	assert.Equal(t, 9999, model.ServerPort, "loadConfig should set ServerPort from cfg.Port")
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	// loadConfig is idempotent and reads os.Args[0] — just test FindConfigPath
	// with a config file that exists but has invalid YAML.
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	_ = os.MkdirAll(configDir, 0o755)
	_ = os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("port: [invalid yaml\n"), 0o644)

	path := FindConfigPath(tmpDir)
	assert.Equal(t, filepath.Join(tmpDir, "config", "config.yaml"), path)
}

func TestLoadConfig_FullPathWithConfigFile(t *testing.T) {
	// Test the full loadConfig path with Port=0 and a valid config file
	// placed relative to DataDir (since loadConfig calls FindConfigPath(DataDir))
	origCfg := model.ConfigInstance
	origServerPort := model.ServerPort
	origDataDir := model.DataDir
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.ServerPort = origServerPort
		model.DataDir = origDataDir
	})

	dataDir := t.TempDir()
	model.DataDir = dataDir
	configDir := filepath.Join(dataDir, "config")
	_ = os.MkdirAll(configDir, 0o755)
	configPath := filepath.Join(configDir, "config.yaml")
	_ = os.WriteFile(configPath, []byte("port: 12345\n"), 0o644)

	model.ConfigInstance = model.Config{}

	loadConfig()

	assert.Equal(t, 12345, model.ConfigInstance.Port, "port should be loaded from config")
	assert.Equal(t, 12345, model.ServerPort, "ServerPort should be set from config")
}

func TestLoadConfig_NoConfigFile(t *testing.T) {
	// Test loadConfig when no config file exists — should apply defaults
	origCfg := model.ConfigInstance
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	origServerPort := model.ServerPort
	t.Cleanup(func() {
		model.ConfigInstance = origCfg
		model.BinDir = origBinDir
		model.DataDir = origDataDir
		model.ServerPort = origServerPort
	})

	tmpDir := t.TempDir()
	model.ConfigInstance = model.Config{}
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")

	loadConfig()

	// Defaults should be applied (port 20000 from ApplyDefaults)
	assert.Equal(t, 20000, model.ConfigInstance.Port, "default port should be 20000")
}

// --- httpDo additional coverage ---

func TestHTTPDo_MarshalError(t *testing.T) {
	// httpDo with a body that can't be marshaled
	_, _, err := httpDo(http.MethodPost, "/api/test", make(chan int))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal request")
}

func TestHTTPDoWithProject_MarshalError(t *testing.T) {
	// httpDoWithProject with a body that can't be marshaled
	_, _, err := httpDoWithProject(http.MethodPost, "/api/test", make(chan int), "/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "marshal request")
}

func TestHTTPDo_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal"})
	}))
	defer server.Close()

	parts := strings.Split(server.URL, ":")
	var port int
	_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &port)

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: port}

	result, status, err := httpDo(http.MethodGet, "/api/test", nil)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, "internal", result["error"])
}

// --- cookie encoding test ---

func TestHTTPDoWithProject_ProjectPathURLEncoded(t *testing.T) {
	var cookieValue string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range r.Cookies() {
			if c.Name == model.ScopedCookieName("clawbench_project") {
				cookieValue = c.Value
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	parts := strings.Split(server.URL, ":")
	var port int
	_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &port)

	origCfg := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = origCfg })
	model.ConfigInstance = model.Config{Port: port}

	projectPath := "/path/with spaces/and&special=chars"
	_, _, err := httpDoWithProject(http.MethodGet, "/api/test", nil, projectPath)
	require.NoError(t, err)

	// Cookie value should be URL-encoded
	decoded, _ := url.QueryUnescape(cookieValue)
	assert.Equal(t, projectPath, decoded)
}
