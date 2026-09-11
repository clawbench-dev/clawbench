package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"clawbench/internal/forge"
	"clawbench/internal/model"
)

// forgeCredentialsRequest is the body for setting or clearing a forge token.
type forgeCredentialsRequest struct {
	// Host is the forge host the token applies to (e.g. "github.com" or
	// "git.acme.internal:8443").
	Host string `json:"host"`
	// Token is the credential. An empty token clears the stored credential.
	Token string `json:"token"`
}

// ServeForgeCredentials sets or clears the token for a forge host.
//
// Tokens are write-only: GET /api/config reports which hosts have a token
// (has_token) but never returns the token itself. This endpoint is the only way
// to set one.
//
//	POST /api/forge/credentials  {host, token}
//	DELETE /api/forge/credentials?host=...  (equivalent to POST with empty token)
func ServeForgeCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req forgeCredentialsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		host := strings.ToLower(strings.TrimSpace(req.Host))
		if host == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		// Refuse to store a credential for a host that is unsafe to contact, so
		// the token can never be pointed at loopback/private/metadata addresses.
		if err := checkForgeHostAllowed(host); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: err.Error()})
			return
		}
		if err := setForgeToken(host, req.Token); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			jsonHost:    host,
			"has_token": req.Token != "",
		})
	case http.MethodDelete:
		host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("host")))
		if host == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		if err := setForgeToken(host, ""); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError", nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed", nil)
	}
}

// setForgeToken persists a forge credential, restoring the in-memory snapshot on
// disk-write failure so config never diverges from what is stored.
func setForgeToken(host, token string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	snapshot := model.ConfigInstance
	// Copy the credentials map so a failed write can be rolled back cleanly.
	prev := model.ConfigInstance.Forge.Credentials
	next := make(map[string]string, len(prev)+1)
	for k, v := range prev {
		next[k] = v
	}
	if token == "" {
		delete(next, host)
	} else {
		next[host] = token
	}
	model.ConfigInstance.Forge.Credentials = next

	// Persist the whole credentials map: writeConfigYAML patches the YAML map
	// generically, and a per-host delete must be reflected on disk too.
	patch := map[string]any{
		"forge": map[string]any{
			"credentials": next,
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		return fmt.Errorf("persist forge credential: %w", err)
	}
	return nil
}

// checkForgeHostAllowed rejects hosts that must never receive a credential.
// It is the same guard the binding flow uses.
func checkForgeHostAllowed(host string) error {
	return forgeHostGuard(host)
}

// forgeHostGuard rejects hosts that must never receive a credential (loopback,
// private, link-local, metadata addresses). It delegates to the forge package's
// SSRF guard so the binding flow and the credential flow share one rule.
func forgeHostGuard(host string) error {
	return forge.CheckHostSafety(host, nil)
}
