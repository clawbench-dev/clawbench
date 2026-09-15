package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestApplyDefaults_ForgeNotifyDefaultsEnabled is the regression guard for the
// Go bool zero-value trap: without explicit defaulting, every notification
// toggle would silently default to off, contradicting the documented default.
func TestApplyDefaults_ForgeNotifyDefaultsEnabled(t *testing.T) {
	var cfg Config
	ApplyDefaults(&cfg, nil)

	assert.True(t, cfg.Forge.Notify.Opened, "opened must default on")
	assert.True(t, cfg.Forge.Notify.Closed, "closed must default on")
	assert.True(t, cfg.Forge.Notify.Merged, "merged must default on")
	assert.True(t, cfg.Forge.Notify.Reopened, "reopened must default on")
	assert.True(t, cfg.Forge.Notify.Commented, "commented must default on")
	assert.True(t, cfg.Forge.Notify.Pipeline, "pipeline must default on")
}

// TestApplyDefaults_ForgeNotifyRespectsExplicitFalse verifies that a user who
// explicitly disables a toggle keeps it disabled — presence must win over the
// default.
func TestApplyDefaults_ForgeNotifyRespectsExplicitFalse(t *testing.T) {
	var cfg Config
	ApplyDefaults(&cfg, map[string]bool{
		"forge.notify.commented": true,
	})

	assert.True(t, cfg.Forge.Notify.Opened, "absent toggles still default on")
	assert.False(t, cfg.Forge.Notify.Commented, "an explicitly-present toggle is respected")
}

// TestApplyDefaults_ForgeInsecureTLSOffByDefault ensures the TLS-verification
// escape hatch is never enabled implicitly.
func TestApplyDefaults_ForgeInsecureTLSOffByDefault(t *testing.T) {
	var cfg Config
	ApplyDefaults(&cfg, nil)
	assert.False(t, cfg.Forge.InsecureTLS)
}

func TestForgeToken_ScopedByHost(t *testing.T) {
	var cfg Config
	cfg.SetForgeToken("GitHub.com", "gh-token")
	cfg.SetForgeToken("git.acme.internal:8443", "gl-token")

	// Lookup is case-insensitive.
	assert.Equal(t, "gh-token", cfg.ForgeToken("github.com"))
	assert.Equal(t, "gh-token", cfg.ForgeToken("GitHub.com"))
	assert.Equal(t, "gl-token", cfg.ForgeToken("git.acme.internal:8443"))
	// A host with no credential returns empty — tokens do not leak across hosts.
	assert.Equal(t, "", cfg.ForgeToken("evil.com"))
}

// TestForgeToken_NormalizesHostSpelling pins the fix for a private-repository
// 404: the credential key is the HOST, so any other spelling of the same
// instance ("https://gitlab.com", "gitlab.com/", "GitLab.com") must resolve to
// the same token. Storing those as distinct keys left the real lookup empty and
// sent every request out anonymously.
func TestForgeToken_NormalizesHostSpelling(t *testing.T) {
	equivalents := []string{
		"gitlab.com",
		"GitLab.com",
		"  gitlab.com  ",
		"https://gitlab.com",
		"https://gitlab.com/",
		"http://gitlab.com",
		"gitlab.com/",
		"git@gitlab.com:group/repo.git",
	}
	for _, spelling := range equivalents {
		var cfg Config
		cfg.SetForgeToken(spelling, "glpat-secret")
		assert.Equal(t, "glpat-secret", cfg.ForgeToken("gitlab.com"),
			"token stored via %q must be found under the canonical host", spelling)
		assert.True(t, cfg.ForgeHasToken("gitlab.com"))
		// The stored key itself is canonical, so one instance never occupies
		// two scopes.
		assert.Len(t, cfg.Forge.Credentials, 1, "%q must collapse to one key", spelling)
	}

	// A port is part of the instance identity and must survive normalization.
	var cfg Config
	cfg.SetForgeToken("https://git.acme.internal:8443/group/repo.git", "self-token")
	assert.Equal(t, "self-token", cfg.ForgeToken("git.acme.internal:8443"))
	// A different port is a different instance.
	assert.Equal(t, "", cfg.ForgeToken("git.acme.internal:9999"))
}

// TestNormalizeForgeHost covers the normalizer directly, including the shapes
// that must NOT be collapsed.
func TestNormalizeForgeHost(t *testing.T) {
	cases := map[string]string{
		"gitlab.com":                         "gitlab.com",
		"GitLab.com":                         "gitlab.com",
		"  gitlab.com  ":                     "gitlab.com",
		"https://gitlab.com":                 "gitlab.com",
		"https://gitlab.com/":                "gitlab.com",
		"https://gitlab.com/group/repo":      "gitlab.com",
		"gitlab.com/":                        "gitlab.com",
		"git@gitlab.com:group/repo.git":      "gitlab.com",
		"git.acme.internal:8443":             "git.acme.internal:8443",
		"https://git.acme.internal:8443/g/r": "git.acme.internal:8443",
		// A plain-http instance is the same instance, so the scheme must not
		// survive into the key.
		"http://gitlab.internal:8080": "gitlab.internal:8080",
		"":                            "",
		"   ":                         "",
	}
	for in, want := range cases {
		assert.Equal(t, want, NormalizeForgeHost(in), "NormalizeForgeHost(%q)", in)
	}
}

// TestNormalizeForgeHost_IPv6 pins the bracketed-literal case.
//
// An IPv6 host is full of colons, so the "strip a path tail after ':'" rule
// would truncate "[::1]:8080" to "[" — collapsing every IPv6 instance onto one
// credential scope and producing a host that no longer matches the binding.
// ParseRemoteURL legitimately yields such hosts, so this is reachable from a
// real remote URL rather than a hypothetical.
func TestNormalizeForgeHost_IPv6(t *testing.T) {
	cases := map[string]string{
		"[::1]:8080":                    "[::1]:8080",
		"[2001:db8::1]:8443":            "[2001:db8::1]:8443",
		"[::1]":                         "[::1]",
		"https://[::1]:8080/group/repo": "[::1]:8080",
		"git@[::1]:8080/group/repo.git": "[::1]:8080",
		// An unterminated bracket is not a usable host; do not invent one.
		"[unterminated": "",
	}
	for in, want := range cases {
		assert.Equal(t, want, NormalizeForgeHost(in), "NormalizeForgeHost(%q)", in)
	}

	// Two distinct IPv6 instances must not collide onto one credential.
	assert.NotEqual(t,
		NormalizeForgeHost("[::1]:8080"),
		NormalizeForgeHost("[::1]:9090"),
		"different ports are different instances",
	)
}

// TestSplitForgeHostScheme pins the split that NormalizeForgeHost throws away.
// The distinction that matters is "" versus "https": a bare host must not
// overwrite a scheme the user set earlier by typing a URL.
func TestSplitForgeHostScheme(t *testing.T) {
	tests := []struct {
		in         string
		wantScheme string
		wantHost   string
	}{
		{in: "gitlab.internal", wantHost: "gitlab.internal"},
		{in: "gitlab.internal:8080", wantHost: "gitlab.internal:8080"},
		{in: "http://gitlab.internal", wantScheme: "http", wantHost: "gitlab.internal"},
		{in: "https://gitlab.internal", wantScheme: "https", wantHost: "gitlab.internal"},
		{in: "HTTPS://GitLab.Internal", wantScheme: "https", wantHost: "gitlab.internal"},
		{in: "  https://gitlab.internal/  ", wantScheme: "https", wantHost: "gitlab.internal"},
		{in: "http://10.0.0.5:8080/team/repo", wantScheme: "http", wantHost: "10.0.0.5:8080"},
		{in: "https://gitlab.internal/group/repo.git", wantScheme: "https", wantHost: "gitlab.internal"},
		{in: "", wantScheme: "", wantHost: ""},
		{in: "   ", wantScheme: "", wantHost: ""},
	}
	for _, tt := range tests {
		scheme, host := SplitForgeHostScheme(tt.in)
		assert.Equal(t, tt.wantScheme, scheme, "scheme for %q", tt.in)
		assert.Equal(t, tt.wantHost, host, "host for %q", tt.in)
	}
}

// TestApplyDefaults_NormalizesLegacyCredentialKeys is the upgrade-path guard for
// the URL-in-host-field bug.
//
// Older builds stored the host exactly as typed (only lowercased and trimmed),
// so a user who entered "https://gitlab.internal" — the input this feature
// exists to support — persisted a row keyed by the URL. Lookups normalize their
// argument, so such a row could never be found; DELETE normalizes too, so it
// could never be removed. The credential was permanently unreachable while the
// settings page still reported it as configured.
func TestApplyDefaults_NormalizesLegacyCredentialKeys(t *testing.T) {
	cfg := Config{
		Forge: ForgeConfig{
			Credentials: map[string]string{
				"https://gitlab.internal":       "glpat-url",
				"HTTP://GitLab.Internal:8080/":  "glpat-port",
				"git@github.com:group/repo.git": "gh-token",
				"github.com":                    "gh-existing",
			},
			Schemes: map[string]string{
				"https://gitlab.internal": "https",
			},
		},
	}
	ApplyDefaults(&cfg, nil)

	// Every spelling must now resolve through the normal lookup.
	assert.Equal(t, "glpat-url", cfg.ForgeToken("gitlab.internal"),
		"a URL-keyed credential must become reachable")
	assert.Equal(t, "glpat-port", cfg.ForgeToken("gitlab.internal:8080"))
	// The scp-like key collapses to its host and collides with the existing
	// github.com entry. Two different tokens for one host is a genuine data
	// conflict with no right answer; what matters is that it resolves the same
	// way every load, which sorted order guarantees.
	assert.Equal(t, "gh-token", cfg.ForgeToken("github.com"))

	// The canonical keys are what is actually stored.
	for k := range cfg.Forge.Credentials {
		assert.Equal(t, k, NormalizeForgeHost(k), "stored key %q must already be canonical", k)
	}
	assert.Equal(t, "https", cfg.ForgeScheme("gitlab.internal"))
}

// TestApplyDefaults_NormalizationCollapsesDuplicateHosts pins the collision
// rule: two spellings of one host must become ONE entry, and an empty value must
// not clobber a real one.
func TestApplyDefaults_NormalizationCollapsesDuplicateHosts(t *testing.T) {
	cfg := Config{
		Forge: ForgeConfig{
			Credentials: map[string]string{
				"https://gitlab.internal": "from-url",
				"gitlab.internal":         "from-bare",
			},
			// The empty value is the absence of information, so it must not win.
			Schemes: map[string]string{
				"https://gitlab.internal": "http",
				"gitlab.internal":         "",
			},
		},
	}
	ApplyDefaults(&cfg, nil)

	assert.Len(t, cfg.Forge.Credentials, 1, "two spellings of one host must collapse to one entry")
	assert.Len(t, cfg.Forge.Schemes, 1)
	assert.Equal(t, "http", cfg.ForgeScheme("gitlab.internal"),
		"an empty scheme must not overwrite a real one")
}

// TestApplyDefaults_NormalizationDropsUnusableHosts guards the degenerate input:
// a key that normalizes to nothing has no host to scope a credential to, so
// keeping it would leave exactly the unreachable row this migration removes.
func TestApplyDefaults_NormalizationDropsUnusableHosts(t *testing.T) {
	cfg := Config{
		Forge: ForgeConfig{
			Credentials: map[string]string{"": "orphan", "   ": "blank"},
		},
	}
	ApplyDefaults(&cfg, nil)
	assert.Empty(t, cfg.Forge.Credentials, "a key with no usable host must be dropped")
}

// TestApplyDefaults_NormalizationIsIdempotent ensures a second load is a no-op,
// so the migration cannot churn the config file on every start.
func TestApplyDefaults_NormalizationIsIdempotent(t *testing.T) {
	cfg := Config{
		Forge: ForgeConfig{
			Credentials: map[string]string{"https://gitlab.internal": "tok"},
		},
	}
	ApplyDefaults(&cfg, nil)
	first := cfg.Forge.Credentials

	ApplyDefaults(&cfg, nil)
	assert.Equal(t, first, cfg.Forge.Credentials)
	assert.Equal(t, "tok", cfg.ForgeToken("gitlab.internal"))
}

func TestForgeScheme_HintIsSeparateFromToken(t *testing.T) {
	var cfg Config
	cfg.SetForgeToken("gitlab.internal", "tok")
	cfg.SetForgeScheme("gitlab.internal", "http")

	assert.Equal(t, "http", cfg.ForgeScheme("gitlab.internal"))
	// The hint is looked up through the same normalization as the token, so any
	// spelling of the instance resolves.
	assert.Equal(t, "http", cfg.ForgeScheme("http://gitlab.internal"))
	assert.Equal(t, "tok", cfg.ForgeToken("https://gitlab.internal"))

	// A host with no hint reports empty, NOT https: the resolver owns the
	// fallback and must be able to tell "unspecified" from a real choice.
	assert.Equal(t, "", cfg.ForgeScheme("other.internal"))

	// Clearing the hint leaves the token alone.
	cfg.SetForgeScheme("gitlab.internal", "")
	assert.Equal(t, "", cfg.ForgeScheme("gitlab.internal"))
	assert.Equal(t, "tok", cfg.ForgeToken("gitlab.internal"))
}

// TestSetForgeScheme_ClearingOnEmptyMapIsSafe guards the nil-map delete: a
// config with no hints at all must not panic when one is cleared.
func TestSetForgeScheme_ClearingOnEmptyMapIsSafe(t *testing.T) {
	var cfg Config
	assert.NotPanics(t, func() { cfg.SetForgeScheme("gitlab.internal", "") })
	assert.Equal(t, "", cfg.ForgeScheme("gitlab.internal"))
}

// TestSetForgeScheme_EmptyHostIsNoOp mirrors the token setter: there is no
// instance to scope a hint to.
func TestSetForgeScheme_EmptyHostIsNoOp(t *testing.T) {
	var cfg Config
	cfg.SetForgeScheme("", "http")
	cfg.SetForgeScheme("   ", "http")
	assert.Empty(t, cfg.Forge.Schemes, "an empty host must not create an entry")
}

func TestForgeToken_ClearOnEmpty(t *testing.T) {
	var cfg Config
	cfg.SetForgeToken("github.com", "gh-token")
	assert.True(t, cfg.ForgeHasToken("github.com"))

	cfg.SetForgeToken("github.com", "")
	assert.False(t, cfg.ForgeHasToken("github.com"))
	assert.Equal(t, "", cfg.ForgeToken("github.com"))
}

func TestForgeToken_EmptyHostIsNoOp(t *testing.T) {
	var cfg Config
	cfg.SetForgeToken("", "token")
	assert.Empty(t, cfg.Forge.Credentials, "an empty host must not create an entry")
	cfg.SetForgeToken("   ", "token")
	assert.Empty(t, cfg.Forge.Credentials)
}

func TestForgeHasToken_DoesNotRevealToken(t *testing.T) {
	var cfg Config
	cfg.SetForgeToken("github.com", "super-secret")
	// HasToken is the only accessor exposed to the API; it must be boolean.
	assert.True(t, cfg.ForgeHasToken("github.com"))
	assert.False(t, cfg.ForgeHasToken("other.com"))
}
