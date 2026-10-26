//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

// ServeConversationProjects handles GET /api/conversation-projects — every
// project that has conversation history, newest activity first.
//
// Unlike /api/recent-projects (a user-driven MRU list that drops directories
// which no longer exist), this endpoint is derived from recorded conversations
// and deliberately keeps projects whose directory has been deleted: the whole
// point is to let the built-in slash commands find and search old history. The
// response carries an `exists` flag so callers can tell a live project from a
// deleted one without another round trip.
func ServeConversationProjects(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projects, err := service.GetConversationProjects()
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to load conversation projects")))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// ServeRecentProjects handles GET (list) and POST (add) for recent projects.
//
// GET returns the projects grouped by git repository: paths sharing one git
// common dir (main worktree, linked worktrees, subdirectories opened as their
// own project) arrive in one group, so the client can show which entries belong
// to the same project. A path outside any repository forms a single-item group.
// Ordering — groups by their most recent member, items by access time — is
// resolved here; the client only renders.
func ServeRecentProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		groups, err := service.GetRecentProjectGroups()
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to load recent projects")))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(groups)

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

// ServeProjectRegistryList handles GET /api/projects/list — every registered
// project (the `projects` registry), with DB-derived statistics, most recently
// active first.
//
// Distinct from /api/conversation-projects (only projects with history) and
// /api/recent-projects (an MRU window that drops vanished directories): this
// lists the full registry, including projects whose directory was deleted and
// projects that were never used, so the settings panel can show them all.
func ServeProjectRegistryList(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	projects, err := service.ListAllProjects()
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to list projects")))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

// ServeProjectRegistryDetail handles GET (detail), DELETE (remove project and
// all its data) and PATCH (repoint the project at a moved directory) for a
// single registry project, identified by `?id=`.
//
// DELETE refuses the global-scope sentinel, an unknown id, the caller's CURRENT
// project, and any project with a running session. PATCH refuses the sentinel,
// an unknown id, a missing/non-directory target, a path another project already
// owns, and a project with a running session. Neither touches the directory on
// disk.
func ServeProjectRegistryDetail(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id, ok := parseProjectIDQuery(w, r)
		if !ok {
			return
		}
		detail, err := service.GetProjectDetail(id)
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to load project detail")))
			return
		}
		if detail == nil {
			writeLocalizedErrorf(w, r, http.StatusNotFound, "ProjectNotFound")
			return
		}
		writeJSON(w, http.StatusOK, detail)

	case http.MethodDelete:
		deleteProjectRegistryEntry(w, r)

	case http.MethodPatch:
		renameProjectRegistryEntry(w, r)

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// renameProjectRegistryEntry handles PATCH /api/projects/detail?id= with
// `{"path": "..."}`: repoint an existing project at a directory that was moved
// or renamed on disk.
//
// The directory is only validated, never created or moved — this is a registry
// update. The caller's project cookie is rewritten when the renamed project is
// the one it currently has open, otherwise the next request would resolve the
// old path again.
func renameProjectRegistryEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectIDQuery(w, r)
	if !ok {
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	// Reject the sentinel before path validation: it is not a real project, and
	// reporting 403 for an arbitrary target path would mask the intended 400.
	if id == store.GlobalScopeProjectID {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProjectRenameForbidden")
		return
	}

	raw := strings.TrimSpace(req.Path)
	if raw == "" || !filepath.IsAbs(raw) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProjectPathInvalid")
		return
	}
	newPath := model.NormalizeProjectPath(raw)
	if !isPathUnderAnyRoot(newPath) {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	oldPath, err := service.RenameProjectData(id, newPath)
	switch {
	case err == nil:
	case errors.Is(err, service.ErrProjectNotFound):
		writeLocalizedErrorf(w, r, http.StatusNotFound, "ProjectNotFound")
		return
	case errors.Is(err, service.ErrProjectRenameForbidden):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProjectRenameForbidden")
		return
	case errors.Is(err, service.ErrProjectPathInvalid):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProjectPathInvalid")
		return
	case errors.Is(err, service.ErrProjectPathExists):
		writeLocalizedErrorf(w, r, http.StatusConflict, "ProjectPathExists")
		return
	case errors.Is(err, service.ErrProjectHasRunningSessions):
		writeLocalizedErrorf(w, r, http.StatusConflict, "ProjectRenameHasRunningSessions")
		return
	default:
		model.WriteError(w, model.Internal(fmt.Errorf("failed to rename project")))
		return
	}

	// Follow the open project. Normalize both sides because the cookie can carry
	// a non-canonical spelling from older sessions.
	if cur := middleware.GetProjectFromCookie(r); cur != "" &&
		store.NormalizeProjectPath(cur) == store.NormalizeProjectPath(oldPath) {
		http.SetCookie(w, &http.Cookie{
			Name:     model.ScopedCookieName("clawbench_project"),
			Value:    url.QueryEscape(newPath),
			Path:     "/",
			MaxAge:   7 * 24 * 3600,
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteLaxMode,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"id":       id,
		"path":     newPath,
		"old_path": oldPath,
	})
}

// deleteProjectRegistryEntry handles DELETE /api/projects/detail?id=.
//
// Split from ServeProjectRegistryDetail so the GET branch and the method
// dispatch do not share its cyclomatic budget.
func deleteProjectRegistryEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectIDQuery(w, r)
	if !ok {
		return
	}

	// The global-scope sentinel is not a real project, but GetProjectDetail
	// below reports it as "not found" — which would mask the intended 400.
	// Reject it explicitly so the caller gets the documented status.
	if id == store.GlobalScopeProjectID {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProjectDeleteForbidden")
		return
	}

	// Refuse deleting the project the caller currently has open: its sessions
	// and the SPA's live state reference it, and the registry row disappearing
	// underneath would leave both dangling.
	detail, err := service.GetProjectDetail(id)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to load project detail")))
		return
	}
	if detail == nil {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "ProjectNotFound")
		return
	}
	if cur := middleware.GetProjectFromCookie(r); cur != "" &&
		store.NormalizeProjectPath(cur) == store.NormalizeProjectPath(detail.Path) {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "CannotDeleteCurrentProject")
		return
	}

	switch err := service.DeleteProjectData(id); {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case errors.Is(err, service.ErrProjectNotFound):
		writeLocalizedErrorf(w, r, http.StatusNotFound, "ProjectNotFound")
	case errors.Is(err, service.ErrProjectDeleteForbidden):
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProjectDeleteForbidden")
	case errors.Is(err, service.ErrProjectHasRunningSessions):
		writeLocalizedErrorf(w, r, http.StatusConflict, "ProjectHasRunningSessions")
	default:
		model.WriteError(w, model.Internal(fmt.Errorf("failed to delete project")))
	}
}

// parseProjectIDQuery reads the required numeric `id` query parameter, writing
// a 400 and returning false when it is missing or malformed.
func parseProjectIDQuery(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("id")
	if raw == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return 0, false
	}
	return id, true
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

		// Canonicalize ONCE, before anything consumes the path.
		//
		// The client adopts the echoed path as the active project
		// (`store.setProject`) and compares it against paths that come from the
		// `projects` table — which stores model.NormalizeProjectPath output.
		// Echoing the REQUEST string verbatim made those two forms differ for
		// any path the user pasted with a trailing slash or a "./" segment, and
		// an exact comparison then reported the user's OWN project as a
		// different one. Normalizing here also keeps the cookie, the DB row and
		// the response in agreement.
		absPath = model.NormalizeProjectPath(absPath)

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

		// Canonical form already established above (see the note there) — the
		// cookie must carry it too, since it is the per-session source of truth
		// read back by the GET branch and by every project-scoped handler.
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
		"groupMaxMembers":        service.MaxGroupMembers,
		"recentProjectsMaxCount": model.RecentProjectsMaxCount,
	})
}
