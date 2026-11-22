package service

import (
	"testing"
)

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
