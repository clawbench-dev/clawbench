package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"clawbench/internal/frontend"
	i18npkg "clawbench/internal/i18n"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/platform"
	"clawbench/internal/proxy"
	"clawbench/internal/ws"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// jsonKeyStatus is the JSON key "status" used across handler responses (goconst).
const jsonKeyStatus = "status"

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
		slog.Warn("handler: requireProject — project cookie is empty",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path))
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

// decodeOptionalJSON decodes the request body into v, treating an EMPTY body as
// "no fields supplied" rather than an error. A non-empty but malformed body is
// still a 400.
//
// This is for endpoints where the body carries only optional refinements, so the
// same route serves "act on everything" and "act on one thing".
func decodeOptionalJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(v)
	if errors.Is(err, io.EOF) {
		return nil // empty body: keep v's zero values
	}
	return err
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
func isPathUnderBase(absPath, basePath string) bool {
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

// Route describes one registered HTTP route. The route table is built once by
// RegisterRoutes and exposed via RegisteredRoutes so tests can assert that the
// OpenAPI spec (internal/api/openapi.yaml) and the live mux stay in sync.
//
// Pattern keeps the http.ServeMux form: an exact path ("/api/tasks") or a
// subtree prefix ending in "/" ("/api/tasks/"). Authenticated records whether
// the handler is wrapped in middleware.Auth, which mirrors the spec's
// `security` declaration.
type Route struct {
	Pattern       string
	Authenticated bool
}

// routeTable accumulates every route registered during RegisterRoutes. It is
// package-level (not returned by value) so the ~141 register calls below do not
// each need to thread a slice through, keeping the diff to the call site
// minimal. RegisterRoutes clears it on entry, so repeated calls in tests do not
// accumulate duplicates.
var routeTable []Route

// RegisteredRoutes returns the routes registered by the most recent
// RegisterRoutes call. Used by drift-guard tests; not for runtime routing.
func RegisteredRoutes() []Route {
	out := make([]Route, len(routeTable))
	copy(out, routeTable)
	return out
}

// registerRoute wraps a handler in the global middleware chain and mounts it.
// Kept separate from the register closures so both the authenticated and the
// public path share one definition of the chain.
func registerRoute(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	wrapped := middleware.Chain(
		middleware.RecoverPanic,
		middleware.WithRequestID,
		middleware.RequestLogger,
		middleware.WithLocalizer,
		middleware.NoCache,
	)(handler)
	mux.HandleFunc(pattern, wrapped)
}

// RegisterRoutes registers all HTTP routes with the given mux
func RegisterRoutes(mux *http.ServeMux) {
	routeTable = routeTable[:0]

	// register records an auth-protected route. Authentication is the default,
	// mirroring the OpenAPI spec (every /api/ operation requires the session
	// cookie unless it explicitly declares `security: []`).
	register := func(pattern string, handler http.HandlerFunc) {
		registerRoute(mux, pattern, middleware.Auth(handler))
		routeTable = append(routeTable, Route{Pattern: pattern, Authenticated: true})
	}

	// registerPublic records one of the handful of intentionally-unauthenticated
	// routes. Each has a documented reason at its call site and a matching
	// `security: []` in the spec.
	registerPublic := func(pattern string, handler http.HandlerFunc) {
		registerRoute(mux, pattern, handler)
		routeTable = append(routeTable, Route{Pattern: pattern, Authenticated: false})
	}

	registerPublic("/", ServeIndex)
	registerPublic("/login", ServeLogin)
	// Health probe — intentionally public. Android calls it before the WebView
	// loads, for two things it cannot do later: confirming the target really is
	// a ClawBench server (the "app" field), and reading "version" to drive the
	// blocking version-mismatch dialog. Neither can wait for login, since login
	// itself requires the WebView. It exposes only {app, version}, and the
	// version is already inferable from the publicly downloadable APK.
	registerPublic("/api/health", ServeHealth)
	registerPublic("/api/me", ServeAuthCheck)
	register("/api/system/resources", ServeSystemResources)
	register("/api/ws/delivery-stats", ServeWSDeliveryStats)
	register("/api/roots", ServeRoots)
	register("/api/config", ServeConfig)
	register("/api/config/test", ServeConfigTest)
	register("/api/config/restart", ServeConfigRestart)
	register("/api/config/password", ServeConfigPassword)
	register("/api/fonts/list", ServeFontsList)
	register("/api/fonts/file", ServeFontFile)
	register("/api/theme/local/upload", ServeThemeLocalUpload)
	register("/api/theme/local/item", ServeThemeLocalUpload)
	register("/api/theme/local/select", ServeThemeLocalSelect)
	register("/api/theme/wallpaper", ServeThemeWallpaperMode)
	register("/api/theme/bing/sync", ServeThemeBingSync)
	register("/api/theme/bing/status", ServeThemeBingStatus)
	register("/api/file/theme-wallpaper", ServeThemeWallpaperGet)
	register("/api/projects", ServeProjects)
	register("/api/project", ServeProjectSet)
	register("/api/ai/chat", AIChat)
	register("/api/ai/chat/cancel", CancelChat)
	register("/api/ai/chat/read", MarkChatRead)
	register("/api/ai/queue", QueueHandler)
	register("/api/ai/queue/inject", QueueInjectHandler)
	register("/api/ai/queue/interrupt", QueueInterruptHandler)
	register("/api/ai/session/update", ServeAISessionUpdate)
	register("/api/ai/session/tags", ServeSessionTags)
	register("/api/ai/sessions", ServeSessions)
	register("/api/ai/sessions/overview", ServeSessionsOverview)
	register("/api/ai/session/archive", ArchiveSession)
	register("/api/ai/session/destroy", DestroySession)
	register("/api/ai/session/resume", ServeSessionResume)
	register("/api/ai/session/acp-load", ServeACPLoadSession)
	register("/api/ai/session/acp-sync", ServeACPSyncSession)
	register("/api/ai/session/fork", ServeForkSession)
	register("/api/ai/session/reset", ServeSessionReset)
	register("/api/ai/session/rewind", ServeSessionRewind)
	register("/api/ai/chat/user-messages", ServeUserMessageIndex)
	register("/api/ai/chat/tool-call", ServeToolCallDetail)
	register("/api/ai/chat/thinking", ServeThinkingDetail)
	register("/api/usage/stats", ServeUsageStats)
	register("/api/git/stats", ServeGitStats)
	register("/api/git/cloc", ServeGitCloc)
	register("/api/ai/permission/respond", ServePermissionRespond)
	register("/api/upload/file", UploadFile)
	register("/api/upload/recent", UploadRecent)
	register("/api/share-in/recent", ShareInRecent)

	// GitHub / GitLab integration (read-only issue & PR browsing).
	register("/api/forge/credentials", ServeForgeCredentials)
	register("/api/forge/verify-token", ServeForgeVerifyToken)
	register("/api/forge/items", ServeForgeItems)
	register("/api/forge/item", ServeForgeItem)
	register("/api/forge/comments", ServeForgeComments)
	register("/api/forge/pipelines", ServeForgePipelines)
	register("/api/forge/pipeline", ServeForgePipeline)
	register("/api/forge/item-pipelines", ServeForgeItemPipelines)
	register("/api/forge/binding", ServeForgeBinding)
	register("/api/forge/remotes", ServeForgeRemotes)
	register("/api/forge/test", ServeForgeTest)
	register("/api/forge/unread", ServeForgeUnread)
	register("/api/forge/unread-items", ServeForgeUnreadItems)
	register("/api/forge/read", ServeForgeMarkRead)

	// Public file-share links. Management endpoints are auth-protected; the
	// public data endpoints (/api/share/{token}/...) and the share SPA page
	// (/share/{token}) are intentionally unauthenticated — the capability token
	// in the URL is the sole credential, and no token means a 404 (zero
	// exposure when the feature is unused).
	register("/api/share", ServeShareManage)
	register("/api/share/list", ServeShareList)
	registerPublic("/api/share/", ServeSharePublic)
	registerPublic("/share/", ServeSharePage)
	register("/api/dir", ListDir)
	register("/api/file/list-tree", ServeListTree)
	register("/api/file/thumb", FileThumb)
	register("/api/file/", GetFile)
	register("/api/git/branch", ServeGitBranch)
	register("/api/git/branches", ServeGitBranches)
	register("/api/git/project-history", ServeGitProjectHistory)
	register("/api/git/file-diff", ServeGitFileDiff)
	register("/api/git/commit-files", ServeGitCommitFiles)
	register("/api/git/history", ServeGitHistory)
	register("/api/git/diff", ServeGitDiff)
	register("/api/git/working-tree", ServeGitWorkingTreeFiles)
	register("/api/git/verify-commits", ServeGitVerifyCommits)
	register("/api/git/worktrees", ServeGitWorktrees)
	register("/api/git/checkout", ServeGitCheckout)
	register("/api/git/tags", ServeGitTags)
	register("/api/file/rename", ServeFileRename)
	register("/api/file/write", ServeFileWrite)
	register("/api/file/delete", ServeFileDelete)
	register("/api/file/batch-exists", ServeFileBatchExists)
	register("/api/file/batch-base64", ServeFileBatchBase64)
	register("/api/file/create", ServeFileCreate)
	register("/api/file/copy", ServeFileCopy)
	register("/api/dir/create", ServeDirCreate)
	register("/api/file/move", ServeFileMove)
	register("/api/file/archive", ServeFileArchive)
	register("/api/file/symbols", ServeFileSymbols)
	register("/api/recent-projects", ServeRecentProjects)
	register("/api/local-file/", ServeLocalFile)
	register("/api/agents", ServeAgents)
	register("/api/agents/", ServeAgentSubRoutes)
	register("/api/backends", ServeBackends)
	register("/api/tts/generate", TTSGenerate)
	register("/api/tts/stream/", TTSStream)
	register("/api/tts/audio/ws", TTSAudioWS)
	register("/api/stt/transcribe", STTTranscribe)
	register("/api/stt/transcribe/ws", STTTranscribeWS)
	register("/api/tasks", ServeTasks)
	register("/api/tasks/", ServeTaskByID)
	register("/api/rag/search", ServeRAGSearch)
	register("/api/rag/message", ServeRAGMessage)
	register("/api/rag/message/summarize", ServeMessageSummarize)
	register("/api/rag/message-index-status", ServeRAGMessageIndexStatus)
	register("/api/rag/session", ServeRAGSession)
	register("/api/rag/status", ServeRAGStatus)
	register("/api/rag/reset-vector", ServeRAGResetVector)
	register("/api/rag/rebuild-fts", ServeRAGRebuildFTS)
	register("/api/rag/rebuild-fts/status", ServeRAGRebuildFTSStatus)
	register("/api/rag/session-search", ServeRAGSessionSearch)
	register("/api/rag/session-first-message", ServeRAGSessionFirstMessage)

	// Client log collection — auth-protected.
	// This is an append-only write primitive into a file on the server, so an
	// unauthenticated caller could forge log lines or rotate the log file to
	// destroy the previous generation. Both clients have a session by the time
	// they upload: the JS relay is gated on the "Debug Log Capture" setting and
	// only armed from the authenticated app (web/src/utils/appLog.ts), and
	// Android's AppLog.startCapture is invoked from the WebView bridge after
	// login, with the WebView cookie jar available in the same process.
	// Android native and JS frontend both land in the unified
	// {LogDir}/logs/client.log ([js]/[android] markers).
	register("/api/client-log", ServeClientLog)

	// Android APK download — intentionally unauthenticated:
	// APK is a public resource; users need to download it before they can even log in.
	registerPublic("/api/apk", ServeAPK)

	// File watch SSE (auto-refresh on file changes)
	register("/api/file/watch", FileWatchSSE)
	register("/api/file/watch/update", FileWatchUpdate)

	// Directory search SSE (recursive fuzzy file search)
	register("/api/dir/search", DirSearch)

	// Port forwarding (registration & detection only; actual forwarding uses SSH tunnels)
	register("/api/proxy/ports", ServeProxyPortAction)
	register("/api/proxy/ports/enabled", ServeProxySetPortEnabled)
	register("/api/proxy/detect", ServeProxyDetect)
	// CORS proxy for Swagger UI "Try it out" — forwards API requests to avoid CORS issues
	register("/api/openapi-proxy", proxy.ServeCORSProxy)

	// SSH tunnel info — split by audience, mirroring the frp pair below.
	// 1. Android BackgroundService.fetchSSHPort() calls the public one from
	//    native Java (no WebView cookies available) to discover the SSH port
	//    before connecting. Without it, fetchSSHPort gets 401, falls back to
	//    httpPort+1 (wrong port), and the tunnel silently fails.
	//    It therefore exposes ONLY {enabled, port}.
	// 2. The full payload (host, username, host key fingerprint, the generated
	//    `ssh -L` command, connection stats) is consumed exclusively by the
	//    authenticated web UI, so it lives behind auth. The command enumerates
	//    every forwarded port and its internal target host — that is a map of
	//    the operator's internal network.
	registerPublic("/api/ssh/info", ServeSSHInfo)
	register("/api/ssh/info/full", ServeSSHInfoFull)

	// FRP tunnel status
	register("/api/frp/info", ServeFRPInfo)           // Full status, requires auth (exposes public IP)
	registerPublic("/api/frp/status", ServeFRPStatus) // Minimal status, no auth (only enabled+running)

	// Terminal (interactive web terminal with PTY + WebSocket + xterm.js)
	register("/api/terminal/ws", TerminalWebSocket)
	register("/api/terminal/status", TerminalStatus)
	register("/api/terminal/close", TerminalClose)
	register("/api/terminal/quick-commands", ServeQuickCommands)
	register("/api/terminal/quick-commands/", ServeQuickCommandByID)
	register("/api/terminal/key-config", ServeKeyConfig)

	// Global event WebSocket (replaces polling for session/task status)
	register("/api/ai/events/ws", func(w http.ResponseWriter, r *http.Request) {
		ws.EventsHandler(w, r)
	})

	// Pending events (missed notifications for offline clients)
	register("/api/ai/events/pending", ServePendingEvents)

	// Chat quick-send (CRUD for quick-send presets stored in database)
	register("/api/chat/quick-send", ServeChatQuickSend)
	register("/api/chat/quick-send/", ServeChatQuickSendByID)

	// Message clusters (cached cluster suggestions + on-demand computation)
	register("/api/chat/message-clusters", ServeMessageClusters)
	register("/api/chat/message-clusters/compute", ServeMessageClustersCompute)
	register("/api/chat/message-clusters/compute/cancel", ServeMessageClustersComputeCancel)
	register("/api/chat/message-clusters/compute/status", ServeMessageClustersComputeStatus)

	// Conversation recommendation (latest next-step suggestion for a session)
	register("/api/chat/recommendation", ServeChatRecommendation)

	// Self-upgrade
	register("/api/upgrade/check", ServeUpgradeCheck)
	register("/api/upgrade/start", ServeUpgradeStart)
	register("/api/upgrade/status", ServeUpgradeStatus)

	// Serve static assets from frontend filesystem (disk public/ > embed fallback)
	// http.FileServerFS internally cleans paths before Open(), preventing traversal.
	// For embed.FS, Open() additionally rejects ".." paths. No explicit ISS-055 guard needed.
	// NOTE: all other static paths (/index-*.js, /material-icons/*, etc.) are
	// handled by ServeIndex, which reads directly from the frontend FS.
	fsys := frontend.GetFS()
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServerFS(fsys)))
	if !frontend.DiskPublicExists() {
		// Dev mode fallbacks: Vite dev server needs these routes
		mux.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(filepath.Join("web", "css")))))
		mux.Handle("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir(filepath.Join("web", "js")))))
	}
}
