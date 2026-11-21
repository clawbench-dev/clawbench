package live

import (
	"strings"
	"testing"

	"clawbench/internal/handlertest"
	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	Register()

	p := plugins.Lookup("live")
	require.NotNil(t, p, "Register did not add the plugin")
	assert.Equal(t, "live", p.ID)
	assert.NotEmpty(t, p.APIPrefixes, "every plugin owns at least one prefix")
	assert.NotNil(t, p.MountRoutes, "declared a prefix but never mounts routes")
}

func TestMountRoutes(t *testing.T) {
	rec := &handlertest.MountRecorder{}
	MountLiveRoutes(rec.Mount())

	// 每条路由都必须以 / 开头且只注册一次：重复 pattern 会让 http.ServeMux
	// 在注册时 panic，而那条 panic 只会在真实启动时才出现。
	seen := map[string]bool{}
	for _, p := range rec.All() {
		assert.True(t, strings.HasPrefix(p, "/"), "pattern %q must be absolute", p)
		assert.Falsef(t, seen[p], "pattern %q registered twice", p)
		seen[p] = true
	}
	assert.NotEmpty(t, rec.Auth, "the feature has authenticated endpoints")
	assert.Contains(t, rec.Auth, "/api/live-share")
	assert.Contains(t, rec.Auth, "/api/live-share/notes")
	assert.ElementsMatch(t, []string{"/livekit/"}, rec.Public)
}
