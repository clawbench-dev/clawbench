package all

import (
	"testing"

	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
)

// TestRegisterAll pins that the aggregation list is not empty and that every
// entry actually reached the registry. A missing line here means a feature is
// silently absent at runtime (no routes, no worker, no /api/features entry)
// while everything still compiles.
func TestRegisterAll(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)

	RegisterAll(0)

	all := plugins.All()
	assert.NotEmpty(t, all)
	for _, id := range []string{
		"forge", "kb", "mail", "share", "tasks",
		"terminal", "live", "notifications", "stats",
	} {
		assert.NotNilf(t, plugins.Lookup(id), "plugin %q was not registered by RegisterAll", id)
	}
}
