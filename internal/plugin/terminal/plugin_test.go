package terminal

import (
	"testing"

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
