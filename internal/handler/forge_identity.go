package handler

import (
	"context"
	"strings"
	"sync"
	"time"

	"clawbench/internal/service"
)

// forgeIdentityCache memoizes the credential's login per (platform, host) so the
// anti-recursion check does not call the forge API on every event. A negative
// result is cached too, with a shorter TTL, so a transient failure does not
// permanently disable the check.
type forgeIdentityEntry struct {
	login     string
	fetchedAt time.Time
}

var (
	forgeIdentityMu    sync.Mutex
	forgeIdentityCache = map[string]forgeIdentityEntry{}
)

const (
	forgeIdentityTTL     = 30 * time.Minute
	forgeIdentityFailTTL = 2 * time.Minute
)

// ForgeCredentialLogin resolves the account a host's credential authenticates
// as, for the anti-recursion check. It returns "" when the identity cannot be
// determined (no credential, no binding, or an API failure); callers treat ""
// as "unknown" and do not suppress the event.
func ForgeCredentialLogin(platform, host string) string {
	key := strings.ToLower(platform) + "|" + strings.ToLower(host)

	forgeIdentityMu.Lock()
	if e, ok := forgeIdentityCache[key]; ok {
		ttl := forgeIdentityTTL
		if e.login == "" {
			ttl = forgeIdentityFailTTL
		}
		if time.Since(e.fetchedAt) < ttl {
			forgeIdentityMu.Unlock()
			return e.login
		}
	}
	forgeIdentityMu.Unlock()

	login := fetchForgeCredentialLogin(platform, host)

	forgeIdentityMu.Lock()
	forgeIdentityCache[key] = forgeIdentityEntry{login: login, fetchedAt: time.Now()}
	forgeIdentityMu.Unlock()
	return login
}

// fetchForgeCredentialLogin builds a provider for the host and asks it for the
// authenticated account. Any failure yields "".
func fetchForgeCredentialLogin(platform, host string) string {
	pf := service.ProjectForge{Platform: platform, Host: host}
	provider, err := newForgeProvider(&pf)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	author, err := provider.CurrentUser(ctx)
	if err != nil {
		return ""
	}
	return author.Login
}
