//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"clawbench/internal/middleware"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// ServeChatQuickSend handles GET (list) and POST (create) for chat quick-send items,
// and PUT /reorder for batch reordering.
func ServeChatQuickSend(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo,gocognit // multi-method quick-send handler
	switch r.Method {
	case http.MethodGet:
		projectPath := middleware.GetProjectFromCookie(r)
		items, err := service.GetChatQuickSend(projectPath)
		if err != nil {
			slog.Error("failed to get chat quick-send items", slog.String("error", err.Error()))
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		if items == nil {
			items = []service.ChatQuickSendItem{}
		}
		writeJSON(w, http.StatusOK, items)

	case http.MethodPost:
		var req struct {
			Label       string `json:"label"`
			Command     string `json:"command"`
			ProjectOnly bool   `json:"project_only"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		req.Label = strings.TrimSpace(req.Label)
		req.Command = strings.TrimSpace(req.Command)
		if req.Label == "" || req.Command == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if len(req.Label) > 100 || len(req.Command) > 4096 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		projectPath := ""
		if req.ProjectOnly {
			projectPath = middleware.GetProjectFromCookie(r)
			if projectPath == "" {
				writeLocalizedError(w, r, model.Forbidden(nil, "NoProjectSelected"))
				return
			}
		}
		id, err := service.AddChatQuickSend(req.Label, req.Command, projectPath)
		if err != nil {
			slog.Error("failed to add chat quick-send item", slog.String("error", err.Error()))
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": id, "label": req.Label, "command": req.Command, "project_only": req.ProjectOnly,
		})

	case http.MethodPut:
		// PUT /api/chat/quick-send/reorder
		path := strings.TrimPrefix(r.URL.Path, "/api/chat/quick-send")
		if strings.TrimPrefix(path, "/") == "reorder" {
			var req struct {
				IDs []int64 `json:"ids"`
			}
			if !decodeJSON(w, r, &req) {
				return
			}
			if len(req.IDs) == 0 {
				writeJSON(w, http.StatusOK, map[string]any{"success": true})
				return
			}
			if err := service.ReorderChatQuickSend(req.IDs); err != nil {
				slog.Error("failed to reorder chat quick-send items", slog.String("error", err.Error()))
				writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
			return
		}
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}

// ServeChatQuickSendByID handles PUT (update) and DELETE for a single chat quick-send item.
func ServeChatQuickSendByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/chat/quick-send/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/chat/quick-send/")
	idStr := strings.TrimSuffix(path, "/")
	// Handle sub-paths like "reorder" — those should go to ServeChatQuickSend
	if idStr == "" || idStr == "reorder" {
		ServeChatQuickSend(w, r)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Label       string `json:"label"`
			Command     string `json:"command"`
			ProjectOnly bool   `json:"project_only"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		req.Label = strings.TrimSpace(req.Label)
		req.Command = strings.TrimSpace(req.Command)
		if req.Label == "" || req.Command == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		if len(req.Label) > 100 || len(req.Command) > 4096 {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		projectPath := ""
		if req.ProjectOnly {
			projectPath = middleware.GetProjectFromCookie(r)
			if projectPath == "" {
				writeLocalizedError(w, r, model.Forbidden(nil, "NoProjectSelected"))
				return
			}
		}
		if err := service.UpdateChatQuickSend(id, req.Label, req.Command, projectPath); err != nil {
			slog.Error("failed to update chat quick-send item", slog.String("error", err.Error()))
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})

	case http.MethodDelete:
		if err := service.DeleteChatQuickSend(id); err != nil {
			slog.Error("failed to delete chat quick-send item", slog.String("error", err.Error()))
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})

	default:
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
	}
}
