//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/service"
)

// ServeRecentProjects handles GET (list) and POST (add) for recent projects.
func ServeRecentProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		paths, err := service.GetRecentProjects()
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to load recent projects")))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(paths)

	case http.MethodPost:
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if err := service.AddRecentProject(req.Path); err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to save recent project")))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})

	case http.MethodDelete:
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if err := service.RemoveRecentProject(req.Path); err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to remove recent project")))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// ServeProjectSet handles GET (current project) and POST (set project).
func ServeProjectSet(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo,gocognit // multi-method project handler
	switch r.Method {
	case http.MethodGet:
		// Prefer the existing cookie over the DB default — the cookie is set
		// by POST /api/project (user-initiated switch) and is the per-session
		// source of truth.  Only fall back to the DB default when no cookie
		// exists (first visit or cookie expired).
		projectPath := middleware.GetProjectFromCookie(r)
		if projectPath == "" {
			projectPath, _ = service.GetDefaultProject()
		}
		if projectPath == "" {
			if homeDir := platform.UserHomeDir(); homeDir != "" {
				projectPath = homeDir
			} else if len(model.RootPaths) > 0 {
				projectPath = model.RootPaths[0]
			}
		}
		// Refresh the cookie to extend its lifetime, using the resolved path.
		http.SetCookie(w, &http.Cookie{
			Name:     model.ScopedCookieName("clawbench_project"),
			Value:    url.QueryEscape(projectPath),
			Path:     "/",
			MaxAge:   7 * 24 * 3600,
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"path": projectPath, "homeDir": platform.UserHomeDir()})

	case http.MethodPost:
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}

		// Resolve path and validate against root paths
		rawPath := req.Path
		var absPath string
		if rawPath == "" || rawPath == "/" {
			if len(model.RootPaths) > 0 {
				absPath = model.RootPaths[0]
			}
		} else if filepath.IsAbs(rawPath) {
			absPath = rawPath
			if !isPathUnderAnyRoot(absPath) {
				writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
				return
			}
		} else {
			// Relative path — resolve from first root
			if len(model.RootPaths) > 0 {
				relPath := strings.TrimPrefix(rawPath, "/")
				absPath, _ = filepath.Abs(filepath.Join(model.RootPaths[0], relPath))
			}
			if !isPathUnderAnyRoot(absPath) {
				writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
				return
			}
		}

		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "NotADirectory")
			return
		}

		// Persist as default project in DB (user-initiated switch)
		if err := service.SetDefaultProject(absPath); err != nil {
			slog.Warn("failed to set default project", "path", absPath, "err", err)
		}

		// Clear chat session cookie when switching project
		http.SetCookie(w, &http.Cookie{
			Name:     model.ScopedCookieName("chat_session_id"),
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		})

		http.SetCookie(w, &http.Cookie{
			Name:     model.ScopedCookieName("clawbench_project"),
			Value:    url.QueryEscape(absPath),
			Path:     "/",
			MaxAge:   7 * 24 * 3600,
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":                     "true",
			"path":                   absPath,
			"homeDir":                platform.UserHomeDir(),
			"roots":                  rootPaths(),
			"uploadMaxSizeMB":        model.UploadMaxSizeMB,
			"uploadMaxFiles":         model.UploadMaxFiles,
			"chatInitialMessages":    model.ChatInitialMessages,
			"chatPageSize":           model.ChatPageSize,
			"chatSessionPageSize":    model.ChatSessionPageSize,
			"sessionMaxCount":        model.SessionMaxCount,
			"recentProjectsMaxCount": model.RecentProjectsMaxCount,
		})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// rootPaths returns the configured filesystem root paths.
// On Linux/macOS, roots is ["/"]. On Windows, roots is the list of available drives.
func rootPaths() []string {
	roots := model.RootPaths
	if len(roots) == 0 {
		slog.Warn("no root paths configured")
		roots = []string{platform.UserHomeDir()}
	}
	return roots
}

// ServeRoots returns the filesystem root paths and configuration limits as JSON.
func ServeRoots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"roots":                  rootPaths(),
		"uploadMaxSizeMB":        model.UploadMaxSizeMB,
		"uploadMaxFiles":         model.UploadMaxFiles,
		"chatInitialMessages":    model.ChatInitialMessages,
		"chatPageSize":           model.ChatPageSize,
		"chatSessionPageSize":    model.ChatSessionPageSize,
		"sessionMaxCount":        model.SessionMaxCount,
		"recentProjectsMaxCount": model.RecentProjectsMaxCount,
	})
}
