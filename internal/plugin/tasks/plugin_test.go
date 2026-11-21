package tasks

import (
	"net/http"
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
	require.NotNil(t, p, "Register did not add the tasks plugin")
	assert.NotNil(t, p.Enabled)
	assert.True(t, p.Enabled(), "the master switch reads the global")
	assert.NotNil(t, p.SwitchEffect, "the tasks switch is bi-directional")
	assert.NotNil(t, p.MountRoutes, "the routes are mounted by the plugin")
}

func TestMountTasksRoutes(t *testing.T) {
	var auth []string
	MountTasksRoutes(plugins.Mount{
		Auth:   func(pattern string, _ http.HandlerFunc) { auth = append(auth, pattern) },
		Public: func(pattern string, _ http.HandlerFunc) { t.Fatalf("tasks has no public route: %s", pattern) },
	})
	assert.Equal(t, []string{"/api/tasks", "/api/tasks/"}, auth)
}

func TestTasksSwitchEffect(t *testing.T) {
	// A nil scheduler is the "not started yet" case; SetEnabled handles it.
	assert.Empty(t, TasksSwitchEffect(false))
	assert.Empty(t, TasksSwitchEffect(true))
}
