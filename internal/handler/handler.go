package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	i18npkg "clawbench/internal/i18n"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/ws"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// loc returns the Localizer for the current request.
func loc(r *http.Request) *i18n.Localizer {
	return middleware.GetLocalizer(r)
}

// T is a shorthand for translating a message key in the handler layer.
func T(r *http.Request, msgKey string, templateData ...map[string]any) string {
	return i18npkg.T(loc(r), msgKey, templateData...)
}

// writeLocalizedErrorf writes a localized error response with i18n message key.
func writeLocalizedErrorf(w http.ResponseWriter, r *http.Request, status int, msgKey string, templateData ...map[string]any) {
	localizedMsg := T(r, msgKey, templateData...)
	var detail map[string]any
	if len(templateData) > 0 {
		detail = templateData[0]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(model.ErrorResponse{Error: localizedMsg, Code: status, MsgKey: msgKey, Detail: detail})
}

// writeLocalizedError writes a localized AppError response.
func writeLocalizedError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *model.AppError
	if err == nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if ok := errors.As(err, &appErr); ok {
		localizedMsg := T(r, appErr.Message)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(appErr.Code)
		_ = json.NewEncoder(w).Encode(model.ErrorResponse{Error: localizedMsg, Code: appErr.Code, MsgKey: appErr.Message})
		return
	}
	writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
}

// requireProject extracts the project path from cookie and writes error if not set.
// Returns the project path and true on success, or empty string and false on failure.
func requireProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		writeLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return "", false
	}
	return projectPath, true
}

// requireMethod checks that the request method is one of the allowed methods.
// Writes 405 on mismatch. Returns true if allowed.
func requireMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	if slices.Contains(methods, r.Method) {
		return true
	}
	writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	return false
}

// writeJSON sets Content-Type and encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON decodes the request body into v. Writes 400 on failure.
// Returns true on success.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return false
	}
	return true
}

// validateAndResolvePath validates a relative path and returns the absolute path.
// Writes 403 on failure. Returns (absPath, true) on success.
func validateAndResolvePath(w http.ResponseWriter, r *http.Request, basePath, relPath string) (string, bool) {
	absPath, ok := model.ValidatePath(basePath, relPath)
	if !ok {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return "", false
	}
	return absPath, true
}

// resolveAbsPath resolves a path string to an absolute path under any root path.
// Absolute paths are validated directly; relative paths are resolved against
// the project path from cookie then validated. This unifies path handling for
// all file mutation endpoints so callers don't need to worry about base-path
// bookkeeping. Writes error on failure. Returns (absPath, true) on success.
func resolveAbsPath(w http.ResponseWriter, r *http.Request, pathStr string) (string, bool) {
	if filepath.IsAbs(pathStr) {
		// Absolute path — validate it's under any root path
		absPath, err := filepath.Abs(pathStr)
		if err != nil {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return "", false
		}
		if !isPathUnderAnyRoot(absPath) {
			writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return "", false
		}
		return absPath, true
	}

	// Relative path — resolve against projectPath from cookie
	projectPath, ok := requireProject(w, r)
	if !ok {
		return "", false
	}
	baseAbs, err := filepath.Abs(projectPath)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to resolve project path: %w", err)))
		return "", false
	}
	absPath, ok := validateAndResolvePath(w, r, baseAbs, pathStr)
	if !ok {
		return "", false
	}
	return absPath, true
}

// isPathUnderAnyRoot checks that absPath is under at least one of the
// configured root paths. Uses the platform's IsPathUnderAnyRoot for
// symlink-safe validation.
func isPathUnderAnyRoot(absPath string) bool {
	return platform.IsPathUnderAnyRoot(absPath, model.RootPaths)
}

// isPathUnderBase checks that absPath is under basePath by resolving symlinks
// on both sides before comparing. This prevents symlink traversal attacks.
// Both paths must be absolute.
func isPathUnderBase(absPath, basePath string) bool { //nolint:unused // security utility
	evalBase, err := filepath.EvalSymlinks(basePath)
	if err != nil {
		return false
	}
	evalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return false
		}
		// Target doesn't exist — resolve parent directory
		evalPath = model.ResolveExistingPath(absPath, evalBase)
		if evalPath == "" {
			return false
		}
	}
	return strings.HasPrefix(evalPath, evalBase+string(filepath.Separator)) || evalPath == evalBase
}

// resolveAgentConfig resolves agent configuration from model.Agents.
// Returns (backend, defaultModelID, systemPrompt, command, ok).
func resolveAgentConfig(agentID string) (string, string, string, string, bool) {
	if agentID == "" {
		agentID = model.GetDefaultAgentID()
	}
	if agentID == "" {
		return "", "", "", "", false
	}
	agent, found := model.Agents[agentID]
	if !found {
		return "", "", "", "", false
	}
	return agent.Backend, agent.DefaultModelID(), agent.SystemPrompt, agent.Command, true
}

// requireSessionID extracts session ID from query param or cookie.
// Writes 400 if not found. Returns (sessionID, true) on success.
func requireSessionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	sessionID := getSessionID(r)
	if sessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return "", false
	}
	return sessionID, true
}

// RegisterRoutes registers all HTTP routes with the given mux
func RegisterRoutes(mux *http.ServeMux) {
	register := func(pattern string, handler http.HandlerFunc) {
		wrapped := middleware.Chain(
			middleware.RecoverPanic,
			middleware.WithRequestID,
			middleware.RequestLogger,
			middleware.WithLocalizer,
		)(handler)
		mux.HandleFunc(pattern, wrapped)
	}

	register("/", ServeIndex)
	register("/login", ServeLogin)
	register("/dialog/project", middleware.Auth(ServeProjectDialog))
	register("/api/health", ServeHealth)
	register("/api/me", ServeAuthCheck)
	register("/api/roots", middleware.Auth(ServeRoots))
	register("/api/config", middleware.Auth(ServeConfig))
	register("/api/config/restart", middleware.Auth(ServeConfigRestart))
	register("/api/config/password", middleware.Auth(ServeConfigPassword))
	register("/api/projects", middleware.Auth(ServeProjects))
	register("/api/project", middleware.Auth(ServeProjectSet))
	register("/api/ai/chat", middleware.Auth(AIChat))
	register("/api/ai/chat/stream", middleware.Auth(AIChatStream))
	register("/api/ai/chat/cancel", middleware.Auth(CancelChat))
	register("/api/ai/queue", middleware.Auth(QueueHandler))
	register("/api/ai/history", middleware.Auth(ServeChatHistory))
	register("/api/ai/session", middleware.Auth(ServeAISession))
	register("/api/ai/session/update", middleware.Auth(ServeAISessionUpdate))
	register("/api/ai/sessions", middleware.Auth(ServeSessions))
	register("/api/ai/session/delete", middleware.Auth(DeleteSession))
	register("/api/ai/session/resume", middleware.Auth(ServeSessionResume))
	register("/api/ai/session/acp-load", middleware.Auth(ServeACPLoadSession))
	register("/api/ai/session/fork", middleware.Auth(ServeForkSession))
	register("/api/ai/commands", middleware.Auth(ServeAICommands))
	register("/api/ai/chat/count", middleware.Auth(ServeChatCount))
	register("/api/ai/chat/user-messages", middleware.Auth(ServeUserMessageIndex))
	register("/api/ai/chat/message", middleware.Auth(ServeChatMessageUpdate))
	register("/api/ai/chat/tool-call", middleware.Auth(ServeToolCallDetail))
	register("/api/ai/permission/respond", middleware.Auth(ServePermissionRespond))
	register("/api/upload/file", middleware.Auth(UploadFile))
	register("/api/dir", middleware.Auth(ListDir))
	register("/api/files", middleware.Auth(ListFiles))
	register("/api/file/thumb", middleware.Auth(FileThumb))
	register("/api/file/", middleware.Auth(GetFile))
	register("/api/git/branch", middleware.Auth(ServeGitBranch))
	register("/api/git/branches", middleware.Auth(ServeGitBranches))
	register("/api/git/project-history", middleware.Auth(ServeGitProjectHistory))
	register("/api/git/file-diff", middleware.Auth(ServeGitFileDiff))
	register("/api/git/commit-files", middleware.Auth(ServeGitCommitFiles))
	register("/api/git/history", middleware.Auth(ServeGitHistory))
	register("/api/git/diff", middleware.Auth(ServeGitDiff))
	register("/api/git/status", middleware.Auth(ServeGitStatus))
	register("/api/git/working-tree", middleware.Auth(ServeGitWorkingTreeFiles))
	register("/api/git/verify-commits", middleware.Auth(ServeGitVerifyCommits))
	register("/api/git/verify-worktrees", middleware.Auth(ServeGitVerifyWorktrees))
	register("/api/git/worktrees", middleware.Auth(ServeGitWorktrees))
	register("/api/git/checkout", middleware.Auth(ServeGitCheckout))
	register("/api/git/tags", middleware.Auth(ServeGitTags))
	register("/api/file/rename", middleware.Auth(ServeFileRename))
	register("/api/file/write", middleware.Auth(ServeFileWrite))
	register("/api/file/delete", middleware.Auth(ServeFileDelete))
	register("/api/file/batch-delete", middleware.Auth(ServeFileBatchDelete))
	register("/api/file/batch-exists", middleware.Auth(ServeFileBatchExists))
	register("/api/file/batch-base64", middleware.Auth(ServeFileBatchBase64))
	register("/api/file/create", middleware.Auth(ServeFileCreate))
	register("/api/file/copy", middleware.Auth(ServeFileCopy))
	register("/api/dir/create", middleware.Auth(ServeDirCreate))
	register("/api/file/move", middleware.Auth(ServeFileMove))
	register("/api/file/archive", middleware.Auth(ServeFileArchive))
	register("/api/file/symbols", middleware.Auth(ServeFileSymbols))
	register("/api/recent-projects", middleware.Auth(ServeRecentProjects))
	register("/api/local-file/", middleware.Auth(ServeLocalFile))
	register("/api/agents", middleware.Auth(ServeAgents))
	register("/api/agents/", middleware.Auth(ServeAgentSubRoutes))
	register("/api/backends", middleware.Auth(ServeBackends))
	register("/api/tts/generate", middleware.Auth(TTSGenerate))
	register("/api/tts/stream/", middleware.Auth(TTSStream))
	register("/api/tasks", middleware.Auth(ServeTasks))
	register("/api/tasks/", middleware.Auth(ServeTaskByID))
	register("/api/rag/search", middleware.Auth(ServeRAGSearch))
	register("/api/rag/message", middleware.Auth(ServeRAGMessage))
	register("/api/rag/session", middleware.Auth(ServeRAGSession))

	// Android log collection — intentionally unauthenticated:
	// Android AppLog sends logs via native HttpURLConnection (no WebView cookies).
	// This endpoint only accepts log entries (write-only, no read); the data is
	// non-sensitive debug logs. Auth is unnecessary and would block the feature.
	register("/api/android-log", ServeAndroidLog)

	// Android APK download — intentionally unauthenticated:
	// APK is a public resource; users need to download it before they can even log in.
	register("/api/apk", ServeAPK)

	// File watch SSE (auto-refresh on file changes)
	register("/api/file/watch", middleware.Auth(FileWatchSSE))
	register("/api/file/watch/update", middleware.Auth(FileWatchUpdate))

	// Port forwarding (registration & detection only; actual forwarding uses SSH tunnels)
	register("/api/proxy/ports", middleware.Auth(ServeProxyPortAction))
	register("/api/proxy/detect", middleware.Auth(ServeProxyDetect))

	// Push config — intentionally unauthenticated:
	// Android native layer calls this before WebView loads (no cookies)
	// to discover JPush AppKey at runtime. Only exposes enabled flag and
	// AppKey — no secrets or credentials.
	register("/api/push/config", ServePushConfig)

	// Push registration is now done via WS "register" message (see events.go).
	// No need for a separate HTTP endpoint.

	// SSH tunnel info — intentionally unauthenticated:
	// 1. Android BackgroundService.fetchSSHPort() calls this from native Java
	//    (no WebView cookies available) to discover the SSH port before connecting.
	// 2. Without this, fetchSSHPort gets 401, falls back to httpPort+1 (wrong port),
	//    and SSH tunnel silently fails with no error reported to the user.
	// 3. This endpoint only exposes: SSH port number, username ("clawbench"),
	//    host key fingerprint, and connection stats — no secrets or credentials.
	register("/api/ssh/info", ServeSSHInfo)

	// Terminal (interactive web terminal with PTY + WebSocket + xterm.js)
	register("/api/terminal/ws", middleware.Auth(TerminalWebSocket))
	register("/api/terminal/status", middleware.Auth(TerminalStatus))
	register("/api/terminal/close", middleware.Auth(TerminalClose))
	register("/api/terminal/config", middleware.Auth(TerminalConfigHandler))
	register("/api/terminal/quick-commands", middleware.Auth(ServeQuickCommands))
	register("/api/terminal/quick-commands/", middleware.Auth(ServeQuickCommandByID))
	register("/api/terminal/key-config", middleware.Auth(ServeKeyConfig))

	// Global event WebSocket (replaces polling for session/task status)
	register("/api/ai/events/ws", middleware.Auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.EventsHandler(w, r)
	})))

	// Chat quick-send (CRUD for quick-send presets stored in database)
	register("/api/chat/quick-send", middleware.Auth(ServeChatQuickSend))
	register("/api/chat/quick-send/", middleware.Auth(ServeChatQuickSendByID))

	if _, err := os.Stat("public"); err == nil {
		mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("public"))))
	} else {
		mux.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(filepath.Join("web", "css")))))
		mux.Handle("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir(filepath.Join("web", "js")))))
		mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))
	}
}
