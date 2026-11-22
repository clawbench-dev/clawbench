package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// acpSpawnHooks resolves a backend's spawn customization through the function
// variable wired by internal/ai/backends. These tests pin the two "no hooks"
// cases (unwired, empty id) because both must degrade to nil rather than panic.

func TestACPSpawnHooks_UnwiredOrEmptyIDReturnsNil(t *testing.T) {
	prev := LookupACPSpawnHooksFn
	LookupACPSpawnHooksFn = nil
	t.Cleanup(func() { LookupACPSpawnHooksFn = prev })

	assert.Nil(t, acpSpawnHooks("codebuddy"), "unwired lookup must not invent hooks")
	assert.Nil(t, acpSpawnHooks(""), "an empty backend id must not resolve")
}

func TestACPSpawnHooks_ResolvesByBackendID(t *testing.T) {
	prev := LookupACPSpawnHooksFn
	t.Cleanup(func() { LookupACPSpawnHooksFn = prev })

	hooks := &ACPSpawnHooks{Args: func(ACPSpawnContext) []string { return []string{"--x"} }}
	LookupACPSpawnHooksFn = func(id string) *ACPSpawnHooks {
		if id == "with-hooks" {
			return hooks
		}
		return nil
	}

	assert.Same(t, hooks, acpSpawnHooks("with-hooks"))
	assert.Nil(t, acpSpawnHooks("without-hooks"))
}
