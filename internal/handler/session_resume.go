package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeSessionResume handles POST /api/ai/session/resume — restores a soft-deleted
// session and returns the session ID. Validates project ownership and session count limits.
func ServeSessionResume(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	projectPath := middleware.GetProjectFromCookie(r)
	if projectPath == "" {
		writeLocalizedError(w, r, model.Forbidden(nil, "NoProjectSelected"))
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SessionID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "SessionIdRequired")
		return
	}

	// Check session exists and belongs to project
	var sessionProjectPath string
	var deleted int
	err := service.DBRead.QueryRowContext(
		r.Context(),
		"SELECT project_path, deleted FROM chat_sessions WHERE id = ?",
		req.SessionID,
	).Scan(&sessionProjectPath, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "SessionNotFound")
		return
	}
	if err != nil {
		model.WriteError(w, model.Internal(err))
		return
	}

	// Project isolation
	if sessionProjectPath != projectPath {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	// If soft-deleted, check session count limit before restoring
	if deleted == 1 {
		if model.SessionMaxCount > 0 {
			var count int
			err = service.DBRead.QueryRowContext(
				r.Context(),
				"SELECT COUNT(*) FROM chat_sessions WHERE project_path = ? AND deleted = 0 AND session_type = 'chat'",
				sessionProjectPath,
			).Scan(&count)
			if err != nil {
				model.WriteError(w, model.Internal(err))
				return
			}
			// Restoring a soft-deleted session would increase active count by 1
			if count+1 > model.SessionMaxCount {
				writeLocalizedErrorf(w, r, http.StatusConflict, "SessionLimitReached", map[string]any{
					"Count": count,
					"Limit": model.SessionMaxCount,
				})
				return
			}
		}

		// Restore the session
		_, err = service.DB.ExecContext(
			r.Context(),
			"UPDATE chat_sessions SET deleted = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
			req.SessionID,
		)
		if err != nil {
			model.WriteError(w, model.Internal(fmt.Errorf("failed to restore session %s: %w", req.SessionID, err)))
			return
		}
		slog.Info("session restored from soft-delete",
			slog.String("session", req.SessionID),
			slog.String("project", sessionProjectPath))
	} else {
		slog.Info("session resume requested (already active)",
			slog.String("session", req.SessionID),
			slog.String("project", sessionProjectPath))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"session_id": req.SessionID,
	})
}
