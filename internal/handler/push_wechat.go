package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"clawbench/internal/model"
	"clawbench/internal/service"
)

// WeChatLoginView is the JSON snapshot of one QR login session.
//
// It mirrors wechat.LoginSnapshot field for field. The handler package declares
// its own type on purpose: internal/handler must not import internal/push/*
// (the push backends call into service, and handler sits on top of service).
// main.go adapts the real service to this interface.
type WeChatLoginView struct {
	LoginID    string `json:"login_id"`
	State      string `json:"state"`
	QRURL      string `json:"qr_url"`
	QRSeq      int    `json:"qr_seq"`
	Refresh    int    `json:"refresh"`
	RefreshMax int    `json:"refresh_max"`
	BotID      string `json:"bot_id"`
	Message    string `json:"message"`
	Error      string `json:"error"`
	ExpiresAt  int64  `json:"expires_at"` // unix milliseconds
}

// WeChatCredentials is what a completed scan yields, in handler-local terms.
type WeChatCredentials struct {
	BotToken string
	BotID    string
	BaseURL  string
	UserID   string
}

// WeChatLoginService is the QR login engine, injected from main.go.
type WeChatLoginService interface {
	// Start begins a login or returns the one already in flight.
	Start(ctx context.Context) WeChatLoginView
	// Poll returns the current snapshot, or false for an unknown session.
	Poll(id string) (WeChatLoginView, bool)
	// Cancel aborts a login. Idempotent.
	Cancel(id string)
}

var wechatLoginSvc WeChatLoginService

// SetWeChatLoginService registers the QR login engine (called from main.go).
func SetWeChatLoginService(s WeChatLoginService) { wechatLoginSvc = s }

// ServeWeChatLogin handles /api/push/wechat/login.
//
//	POST   → start (or resume) a login, 202 + snapshot
//	GET    → poll by ?id=, 200 + snapshot, 404 when the session is gone
//	DELETE → cancel by ?id=, 204 (idempotent)
func ServeWeChatLogin(w http.ResponseWriter, r *http.Request) {
	if wechatLoginSvc == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "InternalError", nil)
		return
	}

	switch r.Method {
	case http.MethodPost:
		snap := wechatLoginSvc.Start(r.Context())
		writeJSON(w, http.StatusAccepted, snap)

	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		snap, ok := wechatLoginSvc.Poll(id)
		if !ok {
			// The session was reaped (or the server restarted). The client
			// should start a new one rather than retry this id forever.
			writeLocalizedErrorf(w, r, http.StatusNotFound, "NotFound", nil)
			return
		}
		writeJSON(w, http.StatusOK, snap)

	case http.MethodDelete:
		wechatLoginSvc.Cancel(r.URL.Query().Get("id"))
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed", nil)
	}
}

// WeChat connection states reported by GET /api/push/wechat/status.
const (
	wechatStateStopped = "stopped"
	wechatStateRunning = "running"
)

// WeChatConnState is the live connection state of the push manager.
//
// Reported separately from the config block because it changes without a config
// write: a `-14` expiry leaves the token in place but stops the channel, and the
// panel has to say so or the user sits on a green "connected" card that never
// delivers anything.
type WeChatConnState struct {
	State     string `json:"state"` // "stopped" | "running" | "expired"
	LastError string `json:"last_error"`
}

// WeChatStatusProvider is the live manager state, injected from main.go.
type WeChatStatusProvider func() WeChatConnState

var wechatStatusProvider WeChatStatusProvider

// SetWeChatStatusProvider registers the live state reader (called from main.go).
func SetWeChatStatusProvider(p WeChatStatusProvider) { wechatStatusProvider = p }

// ServeWeChatStatus handles GET /api/push/wechat/status.
//
// The connection state is reported here rather than through GET /api/config
// because the token is write-only (see PersistWeChatLogin) and the state can
// change without a config write.
func ServeWeChatStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	configMutex.RLock()
	cfg := model.ConfigInstance
	configMutex.RUnlock()

	conn := WeChatConnState{State: wechatStateStopped}
	if cfg.PushMode == "wechat" && cfg.WeChat.BotToken != "" {
		conn.State = wechatStateRunning
	}
	if wechatStatusProvider != nil {
		conn = wechatStatusProvider()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"push_mode":  cfg.PushMode,
		"has_token":  cfg.WeChat.BotToken != "",
		"bot_id":     cfg.WeChat.BotID,
		"user_id":    cfg.WeChat.UserID,
		"base_url":   cfg.WeChat.BaseURL,
		"state":      conn.State,
		"last_error": conn.LastError,
	})
}

// ServeWeChatLogout handles POST /api/push/wechat/logout.
//
// Clears the credentials, every stored context token and the long-poll cursor,
// then lets hot-reload stop the manager. Subscriber rows are kept: their sticky
// last_session_id is a user preference that should survive a re-login of the
// same bot.
func ServeWeChatLogout(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	configMutex.RLock()
	botID := model.ConfigInstance.WeChat.BotID
	configMutex.RUnlock()

	if err := persistWeChatCredentials(WeChatCredentials{}); err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError", nil)
		return
	}
	// Context tokens are scoped to the bot account that received them, so they
	// are useless (and misleading) once a different account logs in. Both
	// failures are non-fatal: the credentials are already gone, so the tokens
	// and the cursor are unusable anyway — but they still need a log line, or a
	// broken DB would look like a clean logout.
	if err := service.ClearWeChatContextTokens(); err != nil {
		slog.Warn("wechat: clear context tokens on logout failed", "error", err)
	}
	if wechatOnLogout != nil {
		wechatOnLogout(botID)
	}

	TriggerReconfigure()
	w.WriteHeader(http.StatusNoContent)
}

// wechatOnLogout releases per-bot state that lives outside the config (the
// long-poll cursor). Injected from main.go, which owns the push package.
var wechatOnLogout func(botID string)

// SetWeChatLogoutHook registers the per-bot cleanup (called from main.go).
func SetWeChatLogoutHook(f func(botID string)) { wechatOnLogout = f }

// PersistWeChatLogin stores the credentials produced by a QR scan.
//
// Mirrors setForgeToken: mutate the in-memory config, write the YAML, and roll
// the in-memory snapshot back if the write fails so the two can never diverge.
// Called from the push package's login goroutine, so it must not assume an HTTP
// request in scope.
func PersistWeChatLogin(creds WeChatCredentials) error {
	return persistWeChatCredentials(creds)
}

// persistWeChatCredentials is the shared write path for login and logout.
func persistWeChatCredentials(creds WeChatCredentials) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	snapshot := model.ConfigInstance

	model.ConfigInstance.WeChat.BotToken = creds.BotToken
	model.ConfigInstance.WeChat.BotID = creds.BotID
	model.ConfigInstance.WeChat.BaseURL = creds.BaseURL
	model.ConfigInstance.WeChat.UserID = creds.UserID

	patch := map[string]any{
		"wechat": map[string]any{
			"bot_token": creds.BotToken,
			"bot_id":    creds.BotID,
			"base_url":  creds.BaseURL,
			"user_id":   creds.UserID,
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		return fmt.Errorf("persist wechat credentials: %w", err)
	}
	return nil
}
