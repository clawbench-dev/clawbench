package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
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
		scheme, host := model.SplitForgeHostScheme(req.Host)
		if host == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		// Any host is accepted, including self-hosted instances on private
		// networks and unresolvable internal names. The UI warns before binding
		// a non-official host; the server does not gate it.
		if err := setForgeToken(host, req.Token, scheme); err != nil {
			writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError", nil)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			jsonHost: host,
			// The RESOLVED scheme, not the raw hint: this is what requests to
			// that host will actually use, which is what the caller needs to
			// display. Reporting the raw hint would show "" for a bare host even
			// though requests go out over https.
			jsonScheme:  forge.ResolveScheme("", model.ConfigInstance.ForgeScheme(host)),
			"has_token": req.Token != "",
		})
	case http.MethodDelete:
		host := model.NormalizeForgeHost(r.URL.Query().Get("host"))
		if host == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		if err := setForgeToken(host, "", ""); err != nil {
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
//
// `scheme` is the API scheme the user named in the host field ("http://h"), or
// "" when they typed a bare host. The two cases are NOT the same, and an empty
// token disambiguates them:
//
//   - token != "" and scheme == "": the user replaced the token by typing a bare
//     host. That says nothing about the scheme, so any recorded hint is kept —
//     overwriting it would silently switch an http-only instance to https.
//   - token == "": the credential is being cleared, so its hint goes with it.
//     A scheme for a host with no credential is state the UI cannot act on.
func setForgeToken(host, token, scheme string) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	snapshot := model.ConfigInstance
	// Copy the maps so a failed write can be rolled back cleanly.
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

	// The hint is copied rather than mutated in place, so the rollback above
	// covers both maps.
	prevSchemes := model.ConfigInstance.Forge.Schemes
	nextSchemes := make(map[string]string, len(prevSchemes)+1)
	for k, v := range prevSchemes {
		nextSchemes[k] = v
	}
	// Clamp before storing: the splitter returns whatever preceded "://", so a
	// typo like "ftp://host" would otherwise be persisted and echoed back to the
	// settings UI as a real scheme. Requests would still work (the resolver
	// falls back to https for anything unrecognized), but the UI would advertise
	// a scheme that is not in use — the opposite of what the hint is for.
	validScheme := forge.NormalizeScheme(scheme)
	switch {
	case token == "":
		// Clearing the credential clears its hint too.
		delete(nextSchemes, host)
	case validScheme != "":
		nextSchemes[host] = validScheme
		// Nothing usable was named with a token present: leave any existing
		// hint untouched, since a bare host says nothing about the scheme.
	}

	model.ConfigInstance.Forge.Credentials = next
	model.ConfigInstance.Forge.Schemes = nextSchemes

	// Persist both maps: writeConfigYAML patches the YAML map generically, and a
	// per-host delete must be reflected on disk too. Writing them together keeps
	// a token and its scheme hint from diverging on disk.
	patch := map[string]any{
		"forge": map[string]any{
			"credentials": next,
			"schemes":     nextSchemes,
		},
	}
	if err := writeConfigYAML(patch); err != nil {
		model.ConfigInstance = snapshot
		return fmt.Errorf("persist forge credential: %w", err)
	}
	return nil
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
	// A scheme in the host field applies to this check. When the user typed a
	// bare host, fall back to the stored hint so re-verifying a saved credential
	// reaches an http-only instance the same way a request would.
	scheme, host := model.SplitForgeHostScheme(req.Host)
	if host == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
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

	author, err := verifyForgeToken(r, host, token, scheme)
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
		jsonScheme: forge.ResolveScheme(scheme, model.ConfigInstance.ForgeScheme(host)),
	})
}

// verifyForgeToken builds a host-scoped client and probes the platform's user
// endpoint. The platform is derived from the host the same way bindings are.
//
// `scheme` is the scheme the caller named in the host field, or "" when it named
// none; the instance hint fills the gap.
func verifyForgeToken(r *http.Request, host, token, scheme string) (forge.Author, error) {
	return verifyForgeTokenContext(forgeContext(r), host, token, scheme)
}

// verifyForgeTokenContext is the context-explicit form of verifyForgeToken, so
// callers without an *http.Request (the identity cache) share one implementation
// and therefore one credential/TLS/scheme policy.
func verifyForgeTokenContext(ctx context.Context, host, token, scheme string) (forge.Author, error) {
	httpClient := forgeHTTPClient()
	resolved := forge.ResolveScheme(scheme, model.ConfigInstance.ForgeScheme(host))

	if forge.PlatformForHost(host) == forge.PlatformGitHub {
		baseURL := ""
		if host != forge.GitHubHost {
			baseURL = fmt.Sprintf("%s://%s/api/v3", resolved, host)
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
		Scheme:     resolved,
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
