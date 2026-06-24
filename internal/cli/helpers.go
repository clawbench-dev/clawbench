package cli

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"clawbench/internal/model"

	"gopkg.in/yaml.v3"
)

// FindConfigPath searches for config.yaml in priority order:
//  1. <BinDir>/config/config.yaml (green portable: next to binary)
//  2. config/config.yaml (CWD-relative, standard layout)
func FindConfigPath(binDir string) string {
	configPath := filepath.Join(binDir, "config", "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = filepath.Join("config", "config.yaml")
	}
	return configPath
}

// loadConfig loads the YAML config file and applies defaults.
// It is safe to call multiple times — subsequent calls are no-ops
// once model.ConfigInstance is populated.
func loadConfig() {
	if model.ConfigInstance.Port != 0 {
		return // already loaded
	}

	absBinPath, _ := filepath.Abs(os.Args[0])
	model.BinDir = filepath.Dir(absBinPath)

	var cfg model.Config
	var presence map[string]bool
	configPath := FindConfigPath(model.BinDir)

	data, err := os.ReadFile(configPath)
	if err == nil {
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse config: %v\n", err)
			return
		}
		presence = model.ParsePresenceMap(raw)
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse config: %v\n", err)
			return
		}
	}
	model.ApplyDefaults(&cfg, presence)
	model.ConfigInstance = cfg
	// Set ServerPort so ScopedCookieName works correctly in CLI context
	model.ServerPort = cfg.Port
}

// apiURL returns the base URL for the local server API.
// Uses https:// when TLS is enabled in config, otherwise http://.
func apiURL() string {
	port := model.ConfigInstance.Port
	if port == 0 {
		port = 20000
	}
	scheme := "http"
	if model.ConfigInstance.TLS.Enabled {
		scheme = "https"
	}
	return scheme + "://localhost:" + strconv.Itoa(port)
}

// httpClient returns an HTTP client that skips TLS verification.
// CLI connects to localhost — self-signed certs are expected.
// Timeout: 30s — prevents indefinite hangs when the server is unresponsive (ISS-265).
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// loadSessionCookie reads the cookie token from .clawbench/cookie-token.
// Returns empty string if the file doesn't exist (no password configured or
// server hasn't generated a token yet).
func loadSessionCookie() string {
	tokenPath := filepath.Join(model.BinDir, ".clawbench", "cookie-token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return ""
	}
	return string(bytes.TrimSpace(data))
}

// setAuthCookie attaches the session cookie to req if a cookie token file exists.
// This allows CLI tools to work when require_auth_for_localhost is enabled.
func setAuthCookie(req *http.Request) {
	if token := loadSessionCookie(); token != "" {
		req.AddCookie(&http.Cookie{
			Name:  model.ScopedCookieName(model.SessionCookie),
			Value: token,
		})
	}
}

// httpDo performs an HTTP request to the server API.
// If a cookie token file exists (require_auth_for_localhost is enabled),
// the session cookie is attached automatically.
func httpDo(method, path string, body any) (map[string]any, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, apiURL()+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthCookie(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("server not reachable at %s: %w", apiURL(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}

	return result, resp.StatusCode, nil
}

// httpDoWithProject is like httpDo but sets the clawbench_project cookie
// so the server can bind the operation to the correct project.
func httpDoWithProject(method, path string, body any, projectPath string) (map[string]any, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, apiURL()+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthCookie(req)

	// Set project cookie so server's requireProject() can extract it
	if projectPath != "" {
		req.AddCookie(&http.Cookie{
			Name:  model.ScopedCookieName("clawbench_project"),
			Value: url.QueryEscape(projectPath),
		})
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("server not reachable at %s: %w", apiURL(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parse response: %w (body: %s)", err, string(respBody))
	}

	return result, resp.StatusCode, nil
}

// outputJSON prints v as JSON to stdout.
func outputJSON(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// outputError prints a JSON error and returns exit code 1.
func outputError(msg string) int {
	outputJSON(map[string]any{"ok": false, "error": msg})
	return 1
}

// mustMarshal returns the JSON encoding of v, or "{}" on error.
func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// flagSet creates a FlagSet with output directed to io.Discard
// (custom help is handled by parseOrHelp).
func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
