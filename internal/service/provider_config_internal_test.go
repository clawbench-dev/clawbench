package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The "is this our value" test decides whether the key may be restored or
// dropped at all, so every shape a shared file can produce has to be answered
// correctly: a false positive deletes another tool's provider, a false negative
// leaves the key pointing at a table the strip already removed.
func TestTomlValueIsManagedProvider(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`"clawbench"`, true},
		{`'clawbench'`, true},
		{`clawbench`, true},
		{`  "clawbench"  `, true},
		{`"clawbench" # note`, true},
		{`"clawbench"# note`, true},
		{`"clawbenchX"`, false},
		{`"clawbench "`, false},
		{`"Clawbench"`, false},
		{`"clawbench#1"`, false},
		{`""`, false},
		{``, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, tomlValueIsManagedProvider(c.raw), "raw=%q", c.raw)
	}
}

// --- userConfigDir mapping guard ---

// An unmapped backend must report known=false. ensureProviderConfigDir turns
// that into an error: otherwise a backend whose spec declares a relocatable
// config dir would silently get an isolated directory with no symlinks, and the
// CLI would see a login-less config.
func TestUserConfigDir_UnknownBackendIsReported(t *testing.T) {
	if _, known := userConfigDir("not-a-backend"); known {
		t.Fatal("an unmapped backend must report known=false")
	}
}

// Every backend the service can write a config dir for must also have a
// user-directory mapping, or the two switches have drifted apart.
func TestUserConfigDir_KnownBackendsAreMapped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, backend := range []string{"kimi", "pi", "codebuddy", "claude"} {
		dir, known := userConfigDir(backend)
		if !known {
			t.Errorf("%s must be mapped (ensureProviderConfigDir would now fail loud)", backend)
			continue
		}
		if dir == "" {
			t.Errorf("%s must resolve to a directory", backend)
		}
	}
}
