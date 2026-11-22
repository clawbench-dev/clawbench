package opencode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/ai"
)

// The OPENCODE_PERMISSION override is what stops subagent permission asks from
// hanging the session (opencode's ACP layer silently drops them). These tests
// pin the payload and its scope: only opencode gets it, and it must not
// blanket-allow.

func TestSpawnEnv_InjectsOpenCodePermission(t *testing.T) {
	env := spawnEnv(ai.ACPSpawnContext{})
	require.Len(t, env, 1)

	const prefix = "OPENCODE_PERMISSION="
	require.Contains(t, env[0], prefix)
	raw := env[0][len(prefix):]
	require.NotEmpty(t, raw)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &parsed),
		"OPENCODE_PERMISSION must be valid JSON: %s", raw)
}

func TestSpawnEnv_PreservesModeRestrictions(t *testing.T) {
	// The override must ONLY auto-allow the permissions that default to "ask"
	// and hang subagents (external_directory, .env reads, doom_loop). It must
	// NOT use {"*":"allow"}, which would override per-agent permission rules
	// (e.g. plan mode's edit deny) and break mode enforcement.
	raw := spawnEnv(ai.ACPSpawnContext{})[0][len("OPENCODE_PERMISSION="):]

	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &parsed))

	wantKeys := map[string]bool{"external_directory": true, "read": true, "doom_loop": true}
	for key := range parsed {
		if _, ok := wantKeys[key]; !ok {
			t.Errorf("unexpected permission key %q in override (expected only external_directory/read/doom_loop)", key)
		}
		delete(wantKeys, key)
	}
	for key := range wantKeys {
		t.Errorf("missing permission key %q in override", key)
	}

	// "read" must whitelist .env files, not blanket-allow.
	var read map[string]string
	require.NoError(t, json.Unmarshal(parsed["read"], &read))
	assert.Equal(t, "allow", read["*.env"])
	assert.Equal(t, "allow", read["*.env.*"])
}

// The hook must be registered, or the shared lifecycle never asks for it.
func TestSpawnHooks_Shape(t *testing.T) {
	hooks := spawnHooks()
	require.NotNil(t, hooks)
	assert.NotNil(t, hooks.Env)
	assert.Empty(t, hooks.Args, "opencode needs no extra argv")
}
