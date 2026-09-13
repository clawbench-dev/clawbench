package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/forge/github"
	"clawbench/internal/forge/gitlab"
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

// forgeVerifyRequest is the body for verifying a forge token.
type forgeVerifyRequest struct {
	// Host is the forge host to authenticate against.
	Host string `json:"host"`
	// Token is the credential to check. When empty, the stored credential for
	// Host is verified instead — so a user can re-check a saved token without
	// retyping it.
	Token string `json:"token"`
}

// ServeForgeVerifyToken checks that a token authenticates against a forge host.
//
// Verification is deliberately decoupled from saving: a caller may verify a
// token it is about to save, or re-verify one already stored, and a failed
// check never mutates stored credentials.
//
//	POST /api/forge/verify-token  {host, token?}
//
// Responds 200 with {ok:true, identity} on success. On failure it still
// responds 200 with {ok:false, code, error} so the frontend can distinguish
// "token rejected" (auth) from "host unreachable" (network) — an unreachable
// host is not proof the token is bad.
func ServeForgeVerifyToken(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req forgeVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
		return
	}
	host := strings.ToLower(strings.TrimSpace(req.Host))
	if host == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
		return
	}
	// Same SSRF guard as storing a credential: we must not be tricked into
	// sending a token (or an unauthenticated probe) to an internal address.
	if err := checkForgeHostAllowed(host); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: err.Error()})
		return
	}

	// An explicit token verifies what the user just typed; an empty one verifies
	// the stored credential.
	token := req.Token
	if token == "" {
		token = model.ConfigInstance.ForgeToken(host)
		if token == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":        false,
				strReqError: "no token stored for this host",
				jsonCode:    "ForgeNoCredential",
			})
			return
		}
	}

	author, err := verifyForgeToken(r, host, token)
	if err != nil {
		code := "ForgeError"
		var fe *forge.Error
		if errorsAsForge(err, &fe) {
			code = string(fe.Kind)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			strReqError: err.Error(),
			jsonCode:    code,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"identity": author.Login,
		"name":     author.Name,
		jsonHost:   host,
	})
}

// verifyForgeToken builds a host-scoped client and probes the platform's user
// endpoint. The platform is derived from the host the same way bindings are.
func verifyForgeToken(r *http.Request, host, token string) (forge.Author, error) {
	return verifyForgeTokenContext(forgeContext(r), host, token)
}

// verifyForgeTokenContext is the context-explicit form of verifyForgeToken, so
// callers without an *http.Request (the identity cache) share one implementation
// and therefore one credential/TLS policy.
func verifyForgeTokenContext(ctx context.Context, host, token string) (forge.Author, error) {
	httpClient := forgeHTTPClient()

	if forge.PlatformForHost(host) == forge.PlatformGitHub {
		baseURL := ""
		if host != forge.GitHubHost {
			baseURL = fmt.Sprintf("https://%s/api/v3", host)
		}
		return github.VerifyToken(ctx, github.Config{
			Token:      token,
			BaseURL:    baseURL,
			HTTPClient: httpClient,
		})
	}
	return gitlab.VerifyToken(ctx, gitlab.Config{
		Token:      token,
		Host:       host,
		HTTPClient: httpClient,
	})
}

// forgeHTTPClient applies the configured TLS policy to a fresh client. Self-
// signed certificates are common on self-hosted instances; InsecureTLS is an
// explicit opt-in (see model.ForgeConfig).
func forgeHTTPClient() *http.Client {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if model.ConfigInstance.Forge.InsecureTLS {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // opt-in by explicit config (see InsecureTLS)
		}
	}
	return httpClient
}

// forgeHostGuard rejects hosts that must never receive a credential (loopback,
// private, link-local, metadata addresses). It delegates to the forge package's
// SSRF guard so the binding flow and the credential flow share one rule.
//
// It is a var so tests can point the verifier at a local httptest server, which
// the real guard deliberately blocks.
var forgeHostGuard = func(host string) error {
	return forge.CheckHostSafety(host, nil)
}
