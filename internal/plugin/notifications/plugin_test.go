package notifications

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

	p := plugins.Lookup("notifications")
	require.NotNil(t, p, "Register did not add the plugin")
	assert.Equal(t, "notifications", p.ID)
	assert.NotEmpty(t, p.APIPrefixes, "every plugin owns at least one prefix")
}
