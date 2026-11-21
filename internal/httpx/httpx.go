// Package httpx 是 HTTP 处理器的公共设施：写 JSON、解析请求体、本地化错误、
// 项目路径校验。
//
// 它从 internal/handler 抽出来，是为了让功能插件（internal/plugin/<id>）
// 能自己实现接口而不必 import handler——handler 是「所有功能的大杂烩」，
// 插件反过来依赖它会让依赖方向变成双向，也无法做到「删掉一个功能就删一个
// 目录」。
//
// 这些函数只依赖 middleware / model / platform / i18n，都是下层包，不构成环。
package httpx

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

	"github.com/nicksnyder/go-i18n/v2/i18n"

	i18npkg "clawbench/internal/i18n"
	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/platform"
)

// Loc returns the Localizer for the current request.
func Loc(r *http.Request) *i18n.Localizer {
	return middleware.GetLocalizer(r)
}

// T is a shorthand for translating a message key in the handler layer.
func T(r *http.Request, msgKey string, templateData ...map[string]any) string {
	return i18npkg.T(Loc(r), msgKey, templateData...)
}

// WriteLocalizedErrorf writes a localized error response with i18n message key.
func WriteLocalizedErrorf(w http.ResponseWriter, r *http.Request, status int, msgKey string, templateData ...map[string]any) {
	localizedMsg := T(r, msgKey, templateData...)
	var detail map[string]any
	if len(templateData) > 0 {
		detail = templateData[0]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(model.ErrorResponse{Error: localizedMsg, Code: status, MsgKey: msgKey, Detail: detail})
}

// WriteLocalizedError writes a localized AppError response.
func WriteLocalizedError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *model.AppError
	if err == nil {
		WriteLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	if ok := errors.As(err, &appErr); ok {
		localizedMsg := T(r, appErr.Message)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(appErr.Code)
		_ = json.NewEncoder(w).Encode(model.ErrorResponse{Error: localizedMsg, Code: appErr.Code, MsgKey: appErr.Message})
		return
	}
	WriteLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
}

// RequireProject extracts the project path from cookie and writes error if not set.
// Returns the project path and true on success, or empty string and false on failure.
func RequireProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		slog.Warn("handler: requireProject — project cookie is empty",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path))
		WriteLocalizedError(w, r, model.Forbidden(model.ErrProjectNotSet, "NoProjectSelected"))
		return "", false
	}
	return projectPath, true
}

// RequireMethod checks that the request method is one of the allowed methods.
// Writes 405 on mismatch. Returns true if allowed.
func RequireMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	if slices.Contains(methods, r.Method) {
		return true
	}
	WriteLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	return false
}

// WriteJSON sets Content-Type and encodes v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON decodes the request body into v. Writes 400 on failure.
// Returns true on success.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return false
	}
	return true
}

// DecodeOptionalJSON decodes the request body into v, treating an EMPTY body as
// "no fields supplied" rather than an error. A non-empty but malformed body is
// still a 400.
//
// This is for endpoints where the body carries only optional refinements, so the
// same route serves "act on everything" and "act on one thing".
func DecodeOptionalJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return nil
	}
	err := json.NewDecoder(r.Body).Decode(v)
	if errors.Is(err, io.EOF) {
		return nil // empty body: keep v's zero values
	}
	return err
}

// ValidateAndResolvePath validates a relative path and returns the absolute path.
// Writes 403 on failure. Returns (absPath, true) on success.
func ValidateAndResolvePath(w http.ResponseWriter, r *http.Request, basePath, relPath string) (string, bool) {
	absPath, ok := model.ValidatePath(basePath, relPath)
	if !ok {
		WriteLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return "", false
	}
	return absPath, true
}

// ResolveAbsPath resolves a path string to an absolute path under any root path.
// Absolute paths are validated directly; relative paths are resolved against
// the project path from cookie then validated. Writes error on failure.
// Returns (absPath, true) on success.
func ResolveAbsPath(w http.ResponseWriter, r *http.Request, pathStr string) (string, bool) {
	if filepath.IsAbs(pathStr) {
		// Absolute path — validate it's under any root path
		absPath, err := filepath.Abs(pathStr)
		if err != nil {
			WriteLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return "", false
		}
		if !IsPathUnderAnyRoot(absPath) {
			WriteLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
			return "", false
		}
		return absPath, true
	}

	// Relative path — resolve against projectPath from cookie
	projectPath, ok := RequireProject(w, r)
	if !ok {
		return "", false
	}
	baseAbs, err := filepath.Abs(projectPath)
	if err != nil {
		model.WriteError(w, model.Internal(fmt.Errorf("failed to resolve project path: %w", err)))
		return "", false
	}
	absPath, ok := ValidateAndResolvePath(w, r, baseAbs, pathStr)
	if !ok {
		return "", false
	}
	return absPath, true
}

// IsPathUnderAnyRoot checks that absPath is under at least one of the
// configured root paths. Uses the platform's IsPathUnderAnyRoot for
// symlink-safe validation.
func IsPathUnderAnyRoot(absPath string) bool {
	return platform.IsPathUnderAnyRoot(absPath, model.RootPaths)
}

// IsPathUnderBase checks that absPath is under basePath by resolving symlinks
// on both sides before comparing. This prevents symlink traversal attacks.
// Both paths must be absolute.
func IsPathUnderBase(absPath, basePath string) bool {
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
	return model.IsPathUnderBase(evalPath, evalBase)
}
