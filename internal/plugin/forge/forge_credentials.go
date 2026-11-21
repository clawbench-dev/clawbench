package forge

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clawbench/internal/cfgwrite"
	forgecore "clawbench/internal/forge"
	"clawbench/internal/forge/gitea"
	"clawbench/internal/forge/github"
	"clawbench/internal/forge/gitlab"
	"clawbench/internal/httpx"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// forgeCredentialsRequest is the body for setting or clearing a forge token.
type forgeCredentialsRequest struct {
	// Host is the forge host the token applies to (e.g. "github.com" or
	// "git.acme.internal:8443").
	Host string `json:"host"`
	// Token is the credential. An empty token clears the stored credential.
	Token string `json:"token"`
	// Platform names the platform this host runs ("github", "gitlab", "gitea";
	// "forgejo" is accepted as an alias). Optional: omitting it keeps whatever
	// hint the host already has, which is what re-saving a token without
	// restating the platform should do. It exists because a self-hosted host
	// cannot always be classified from its name — a Gitea instance and a
	// self-hosted GitLab are indistinguishable.
	Platform string `json:"platform"`
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
			httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		scheme, host := model.SplitForgeHostScheme(req.Host)
		if host == "" {
			httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		// Validate before storing: an unrecognized value would be persisted and
		// then ignored by every resolver, which looks like the choice was saved
		// and silently did nothing.
		platform, ok := forgecore.ParsePlatform(req.Platform)
		if req.Platform != "" && !ok {
			httpx.WriteJSON(w, http.StatusBadRequest, map[string]any{
				strReqError: "unsupported platform",
				jsonCode:    codeForgeInvalidPlatform,
			})
			return
		}
		// Any host is accepted, including self-hosted instances on private
		// networks and unresolvable internal names. The UI warns before binding
		// a non-official host; the server does not gate it.
		if err := setForgeToken(host, req.Token, scheme, string(platform)); err != nil {
			httpx.WriteLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError", nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			jsonHost: host,
			// The RESOLVED scheme, not the raw hint: this is what requests to
			// that host will actually use, which is what the caller needs to
			// display. Reporting the raw hint would show "" for a bare host even
			// though requests go out over https.
			jsonScheme:   forgecore.ResolveScheme("", model.ConfigInstance.ForgeScheme(host)),
			jsonPlatform: string(resolveForgePlatform(host, req.Platform)),
			"has_token":  req.Token != "",
		})
	case http.MethodDelete:
		host := model.NormalizeForgeHost(r.URL.Query().Get("host"))
		if host == "" {
			httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
			return
		}
		if err := setForgeToken(host, "", "", ""); err != nil {
			httpx.WriteLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError", nil)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		httpx.WriteLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed", nil)
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
//
// `platform` follows the same three rules for the platform hint: empty keeps
// whatever the host already had (so re-saving a token does not reset it), a
// value replaces it, and clearing the credential clears it too.
func setForgeToken(host, token, scheme, platform string) error {
	cfgwrite.Mu.Lock()
	defer cfgwrite.Mu.Unlock()

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
	validScheme := forgecore.NormalizeScheme(scheme)
	switch {
	case token == "":
		// Clearing the credential clears its hint too.
		delete(nextSchemes, host)
	case validScheme != "":
		nextSchemes[host] = validScheme
		// Nothing usable was named with a token present: leave any existing
		// hint untouched, since a bare host says nothing about the scheme.
	}

	prevPlatforms := model.ConfigInstance.Forge.Platforms
	nextPlatforms := make(map[string]string, len(prevPlatforms)+1)
	for k, v := range prevPlatforms {
		nextPlatforms[k] = v
	}
	switch {
	case token == "":
		delete(nextPlatforms, host)
	case platform != "":
		nextPlatforms[host] = platform
	}

	model.ConfigInstance.Forge.Credentials = next
	model.ConfigInstance.Forge.Schemes = nextSchemes
	model.ConfigInstance.Forge.Platforms = nextPlatforms

	// Persist every map: writeConfigYAML patches the YAML map generically, and a
	// per-host delete must be reflected on disk too. Writing them together keeps
	// a token and its hints from diverging on disk.
	patch := map[string]any{
		pluginID: map[string]any{
			"credentials": next,
			"schemes":     nextSchemes,
			"platforms":   nextPlatforms,
		},
	}
	if err := cfgwrite.Patch(patch); err != nil {
		model.ConfigInstance = snapshot
		return fmt.Errorf("persist forge credential: %w", err)
	}
	return nil
}

// setForgePlatformHint records which platform a host runs, without touching its
// credential or scheme.
//
// It exists for the binding path: binding a repository is the moment the user
// states what the host is ("this is my Gitea"), and token verification reads the
// same hint later. Keeping one host-level answer means the binding and the
// credential form can never disagree about what they are talking to.
//
// `platform` must already be validated; the caller owns the vocabulary.
func setForgePlatformHint(host, platform string) error {
	host = model.NormalizeForgeHost(host)
	if host == "" || platform == "" {
		return nil
	}
	cfgwrite.Mu.Lock()
	defer cfgwrite.Mu.Unlock()

	if model.ConfigInstance.ForgePlatform(host) == platform {
		return nil
	}
	snapshot := model.ConfigInstance

	prev := model.ConfigInstance.Forge.Platforms
	next := make(map[string]string, len(prev)+1)
	for k, v := range prev {
		next[k] = v
	}
	next[host] = platform
	model.ConfigInstance.Forge.Platforms = next

	if err := cfgwrite.Patch(map[string]any{pluginID: map[string]any{"platforms": next}}); err != nil {
		model.ConfigInstance = snapshot
		return fmt.Errorf("persist forge platform: %w", err)
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
	// Platform names what the host runs, when the caller knows better than the
	// host name does. Empty falls back to the stored hint for the host, then to
	// the host-name inference.
	Platform string `json:"platform"`
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
	if !httpx.RequireMethod(w, r, http.MethodPost) {
		return
	}
	var req forgeVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
		return
	}
	// A scheme in the host field applies to this check. When the user typed a
	// bare host, fall back to the stored hint so re-verifying a saved credential
	// reaches an http-only instance the same way a request would.
	scheme, host := model.SplitForgeHostScheme(req.Host)
	if host == "" {
		httpx.WriteLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequest", nil)
		return
	}
	// An explicit platform must be one we can actually speak to; silently
	// falling back would verify the token against the wrong API.
	if req.Platform != "" {
		if _, ok := forgecore.ParsePlatform(req.Platform); !ok {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"ok":        false,
				strReqError: "unsupported platform",
				jsonCode:    codeForgeInvalidPlatform,
			})
			return
		}
	}
	// An explicit token verifies what the user just typed; an empty one verifies
	// the stored credential.
	token := req.Token
	if token == "" {
		token = model.ConfigInstance.ForgeToken(host)
		if token == "" {
			httpx.WriteJSON(w, http.StatusOK, map[string]any{
				"ok":        false,
				strReqError: "no token stored for this host",
				jsonCode:    "ForgeNoCredential",
			})
			return
		}
	}

	author, err := verifyForgeToken(r, host, token, scheme, req.Platform)
	if err != nil {
		code := "ForgeError"
		var fe *forgecore.Error
		if errorsAsForge(err, &fe) {
			code = string(fe.Kind)
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			strReqError: err.Error(),
			jsonCode:    code,
		})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"identity":   author.Login,
		jsonName:     author.Name,
		jsonHost:     host,
		jsonScheme:   forgecore.ResolveScheme(scheme, model.ConfigInstance.ForgeScheme(host)),
		jsonPlatform: string(resolveForgePlatform(host, req.Platform)),
	})
}

// verifyForgeToken builds a host-scoped client and probes the platform's user
// endpoint.
//
// `scheme` is the scheme the caller named in the host field, or "" when it named
// none; the instance hint fills the gap. `platform` is the platform the caller
// named, or "" to use the host's recorded hint and then the host-name inference.
func verifyForgeToken(r *http.Request, host, token, scheme, platform string) (forgecore.Author, error) {
	return verifyForgeTokenContext(forgeContext(r), host, token, scheme, platform)
}

// verifyForgeTokenContext is the context-explicit form of verifyForgeToken, so
// callers without an *http.Request (the identity cache) share one implementation
// and therefore one credential/TLS/scheme/platform policy.
func verifyForgeTokenContext(ctx context.Context, host, token, scheme, platform string) (forgecore.Author, error) {
	httpClient := forgeHTTPClient()
	resolved := forgecore.ResolveScheme(scheme, model.ConfigInstance.ForgeScheme(host))

	switch resolveForgePlatform(host, platform) {
	case forgecore.PlatformGitHub:
		baseURL := ""
		if host != forgecore.GitHubHost {
			baseURL = fmt.Sprintf("%s://%s/api/v3", resolved, host)
		}
		return github.VerifyToken(ctx, github.Config{
			Token:      token,
			BaseURL:    baseURL,
			HTTPClient: httpClient,
		})
	case forgecore.PlatformGitea:
		return gitea.VerifyToken(ctx, gitea.Config{
			Token:      token,
			Host:       host,
			Scheme:     resolved,
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

// resolveForgePlatform decides which platform a host is, in precedence order:
// what the caller named, the hint recorded for that host, the platform a project
// binding on that host records, then the host-name inference (github.com →
// GitHub, everything else → GitLab).
//
// The hints sit above the inference because a self-hosted Gitea instance is
// indistinguishable from a self-hosted GitLab by name, and a recorded choice is
// the only thing that knows the difference. The binding sits below the explicit
// hint but above the inference so a binding made before the hint existed (or by
// an older build) still decides how its host is probed — the user's binding says
// what the host is, and verification must not contradict it.
func resolveForgePlatform(host, explicit string) forgecore.Platform {
	if p, ok := forgecore.ParsePlatform(explicit); ok {
		return p
	}
	if p, ok := forgecore.ParsePlatform(model.ConfigInstance.ForgePlatform(host)); ok {
		return p
	}
	if p, ok := forgecore.ParsePlatform(forgePlatformFromBindings(host)); ok {
		return p
	}
	return forgecore.PlatformForHost(host)
}

// forgePlatformFromBindings reports the platform any project's binding records
// for host, or "" when nothing is bound to it.
//
// A platform is a property of the host, not of one project, so any binding that
// names it answers for all of them. Newest binding wins (ListProjectForges is
// ordered by updated_at DESC), which is also the one most recently confirmed by
// the user.
func forgePlatformFromBindings(host string) string {
	bindings, err := service.ListProjectForges()
	if err != nil {
		// Best-effort: an unreadable table must not block verification, which
		// can still fall back to the host-name inference.
		return ""
	}
	for _, pf := range bindings {
		if strings.EqualFold(pf.Host, host) {
			return pf.Platform
		}
	}
	return ""
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
