package terminal

import (
	"strings"
	"testing"

	"clawbench/internal/handlertest"
	"clawbench/internal/model"
	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	Register(0)

	p := plugins.Lookup("terminal")
	require.NotNil(t, p, "Register did not add the plugin")
	assert.Equal(t, "terminal", p.ID)
	assert.NotEmpty(t, p.APIPrefixes, "every plugin owns at least one prefix")
	assert.NotEmpty(t, p.ConfigFields, "config ownership must be declared")
	assert.NotNil(t, p.MountRoutes, "declared a prefix but never mounts routes")
}

// TestManagerLifecycle pins the three hooks: Start is idempotent, Reconfigure
// only touches an existing manager, Stop clears it and is idempotent too.
func TestManagerLifecycle(t *testing.T) {
	orig := GetTerminalManager()
	t.Cleanup(func() { SetTerminalManager(orig) })
	SetTerminalManager(nil)

	cfg := model.Config{Terminal: model.TerminalConfig{
		Enabled:     true,
		BufferLines: 100,
	}}

	startManager(cfg)
	require.NotNil(t, GetTerminalManager(), "Start must install a manager")
	first := GetTerminalManager()

	startManager(cfg)
	assert.Same(t, first, GetTerminalManager(), "Start must be idempotent")

	reconfigureManager(cfg)
	assert.Same(t, first, GetTerminalManager(), "Reconfigure must keep the same manager")

	stopManager()
	assert.Nil(t, GetTerminalManager(), "Stop must clear the manager")

	stopManager()
	assert.Nil(t, GetTerminalManager(), "Stop must be idempotent")
}

func TestMountRoutes(t *testing.T) {
	rec := &handlertest.MountRecorder{}
	MountTerminalRoutes(rec.Mount())

	// 每条路由都必须以 / 开头且只注册一次：重复 pattern 会让 http.ServeMux
	// 在注册时 panic，而那条 panic 只会在真实启动时才出现。
	seen := map[string]bool{}
	for _, p := range rec.All() {
		assert.True(t, strings.HasPrefix(p, "/"), "pattern %q must be absolute", p)
		assert.Falsef(t, seen[p], "pattern %q registered twice", p)
		seen[p] = true
	}
	assert.NotEmpty(t, rec.Auth, "the feature has authenticated endpoints")
	assert.Contains(t, rec.Auth, "/api/terminal/ws")
	assert.Contains(t, rec.Auth, "/api/terminal/history")
}
