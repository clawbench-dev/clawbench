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
