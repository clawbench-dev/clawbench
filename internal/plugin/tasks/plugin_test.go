package tasks

import (
	"testing"

	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegister(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	Register()

	p := plugins.Lookup("tasks")
	require.NotNil(t, p, "Register did not add the plugin")
	assert.Equal(t, "tasks", p.ID)
	assert.NotEmpty(t, p.APIPrefixes, "every plugin owns at least one prefix")
	assert.NotEmpty(t, p.ConfigFields, "config ownership must be declared")
	assert.NotNil(t, p.SwitchEffect, "the tasks switch is bi-directional")
}
