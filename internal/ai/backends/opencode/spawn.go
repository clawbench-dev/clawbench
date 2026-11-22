package opencode

import (
	"clawbench/internal/ai"
)

// permissionEnvValue is injected into opencode ACP processes via the
// OPENCODE_PERMISSION env var (opencode merges it into its `permission` config
// and it is inherited by subagent sessions).
//
// Workaround for the opencode ACP subagent permission-ask hang: opencode's ACP
// layer (packages/opencode/src/acp/permission.ts) silently drops permission
// requests from subagent (task-tool) sessions — the subagent's session isn't in
// the ACP session registry, so the handler hits `if (!session) return` and never
// replies. The subagent's tool call then blocks forever waiting for the
// approval, hanging the whole session.
//
// Injecting OPENCODE_PERMISSION makes the three permissions that default to
// "ask" resolve to "allow" client-side, so subagents never trigger an ask.
// We deliberately DON'T use {"*":"allow"}: that would be merged last into
// every agent's permission rules and override per-mode enforcement (e.g.
// plan mode's edit deny, explore's read-only boundary). Only these three
// ask-type gates are lifted, and mode protections stay intact.
const permissionEnvValue = `{"external_directory":"allow","read":{"*.env":"allow","*.env.*":"allow"},"doom_loop":"allow"}`

// spawnHooks returns opencode's spawn-time environment customization.
func spawnHooks() *ai.ACPSpawnHooks {
	return &ai.ACPSpawnHooks{Env: spawnEnv}
}

func spawnEnv(ai.ACPSpawnContext) []string {
	return []string{"OPENCODE_PERMISSION=" + permissionEnvValue}
}
