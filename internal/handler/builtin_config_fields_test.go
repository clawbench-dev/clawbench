package handler

import (
	"testing"

	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
)

// TestAllPatchablePaths pins the union that guards use for "does anything under
// this prefix have a settable field". It must include all three sources:
// ownerless fields, feature fields, and plugin-declared fields. Missing one makes
// forge.notify-style intermediate objects look like orphans.
//
// The plugin source only contributes when the registry is populated, which it
// never is during handler tests — so this test registers a probe explicitly.
func TestAllPatchablePaths(t *testing.T) {
	plugins.Reset()
	t.Cleanup(plugins.Reset)
	plugins.Register(&plugins.Plugin{
		ID:           "probe",
		ConfigFields: map[string]bool{"probe.field": true},
	})

	got := AllPatchablePaths()

	assert.True(t, got["chat.page_size"], "ownerless field missing")
	assert.True(t, got["forge.enabled"], "feature field missing")
	assert.True(t, got["probe.field"], "plugin-declared field missing")
}
