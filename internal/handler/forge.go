package handler

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/forge/github"
	"clawbench/internal/forge/gitlab"
	"clawbench/internal/model"
	"clawbench/internal/service"
)

// newForgeProvider builds a read-only Provider for a binding, using the token
// scoped to that host and applying the configured TLS policy.
//
// This is the single place where a provider is constructed, so the credential
// scope (per host) and the TLS policy (optional insecure mode) are applied
// consistently for every endpoint.
func newForgeProvider(pf *service.ProjectForge) (forge.Provider, error) {
	if pf == nil {
		return nil, fmt.Errorf("no repository binding")
	}
	token := model.ConfigInstance.ForgeToken(pf.Host)

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}
	if model.ConfigInstance.Forge.InsecureTLS {
		// Self-signed certificates are common on self-hosted instances. This is
		// opt-in and logged when enabled (see applyHotReloadGlobals).
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in by explicit config
		}
	}

	switch forge.Platform(pf.Platform) {
	case forge.PlatformGitHub:
		baseURL := ""
		if pf.Host != forge.GitHubHost {
			baseURL = fmt.Sprintf("https://%s/api/v3", pf.Host)
		}
		return github.New(github.Config{
			Token:      token,
			BaseURL:    baseURL,
			HTTPClient: httpClient,
		}, pf.Owner, pf.Repo)
	case forge.PlatformGitLab:
		return gitlab.New(gitlab.Config{
			Token:      token,
			Host:       pf.Host,
			HTTPClient: httpClient,
		}, pf.Owner, pf.Repo)
	default:
		return nil, fmt.Errorf("unsupported platform %q", pf.Platform)
	}
}

// forgeProviderForProject resolves the project's binding and returns a provider.
// The bool is false when the caller should stop (an error response was written).
func forgeProviderForProject(w http.ResponseWriter, r *http.Request, projectPath string) (forge.Provider, bool) {
	pf, err := service.GetProjectForge(projectPath)
	if err != nil {
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return nil, false
	}
	if pf == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "no repository bound to this project",
			"code":  "NoForgeBinding",
		})
		return nil, false
	}
	provider, err := newForgeProvider(pf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return nil, false
	}
	return provider, true
}

// writeForgeError maps a classified forge error to an HTTP response, preserving
// the error kind so the frontend can react (auth → settings, rate limit → retry
// later, network → check connectivity).
func writeForgeError(w http.ResponseWriter, r *http.Request, err error) {
	var fe *forge.Error
	status := http.StatusBadGateway
	code := "ForgeError"
	message := err.Error()
	retryAfter := 0
	if errors.As(err, &fe) {
		message = fe.Message
		retryAfter = fe.RetryAfterSeconds
		switch fe.Kind {
		case forge.ErrKindAuth:
			status = http.StatusUnauthorized
			code = "ForgeAuthFailed"
		case forge.ErrKindRateLimit:
			status = http.StatusTooManyRequests
			code = "ForgeRateLimited"
		case forge.ErrKindNotFound:
			status = http.StatusNotFound
			code = "ForgeNotFound"
		case forge.ErrKindNetwork:
			status = http.StatusBadGateway
			code = "ForgeNetworkError"
		case forge.ErrKindServer:
			status = http.StatusBadGateway
			code = "ForgeServerError"
		default:
			status = http.StatusBadGateway
		}
	}
	body := map[string]any{"error": message, "code": code}
	if retryAfter > 0 {
		body["retryAfterSeconds"] = retryAfter
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
	writeJSON(w, status, body)
}

// forgeContext returns the request context, defaulting to Background when nil
// (defensive: net/http always provides one).
func forgeContext(r *http.Request) context.Context {
	if r.Context() != nil {
		return r.Context()
	}
	return context.Background()
}

// errorsAsForge is a tiny alias so callers in this package can classify forge
// errors without importing errors at each site.
func errorsAsForge(err error, target **forge.Error) bool {
	return errors.As(err, target)
}
