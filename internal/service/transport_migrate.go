package service

import (
	"log/slog"

	"clawbench/internal/store"
)

// migAgentTransportFromCommand records the transport column's new single source
// of truth: every agent with an ACP command runs over ACP.
//
// The column USED to be a user choice ("cli" vs "acp-stdio") and drove the
// backend factory. CLI support is gone, so the factory now decides from the
// command alone, and the column is only what clients read. Rows still say "cli"
// for agents that have an ACP command (either because the user picked CLI, or
// because they were written before the command was added), which makes the
// client show a non-ACP agent for one that in fact runs over ACP.
//
// The PATCH handler refuses the field now, so the in-memory model no longer
// derives it on write. This migration is what stops an upgraded install from
// keeping the stale value forever.
func MigrateAgentTransportFromCommand() bool {
	res, err := store.WriteExec(`
		UPDATE agents
		   SET transport = 'acp-stdio'
		 WHERE COALESCE(acp_command, '') != ''
		   AND COALESCE(transport, '') != 'acp-stdio'`)
	if err != nil {
		slog.Warn("migration: normalize agent transport failed", "err", err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		slog.Info("migration: normalized agent transport from acp_command", "rows", n)
	}
	return true
}

// MigrateSessionTransportColumns retires the per-session transport column.
//
// The column used to pin one session to CLI. Nothing reads it any more (the
// factory decides from the agent's command), but a stale "cli" value made
// clients keep rendering the CLI-facing gates for that session. Clearing it
// leaves every row on the single meaning of "derived from the agent".
func MigrateSessionTransportColumns() bool {
	res, err := store.WriteExec(`
		UPDATE chat_sessions
		   SET transport = ''
		 WHERE COALESCE(transport, '') != ''`)
	if err != nil {
		slog.Warn("migration: clear session transport failed", "err", err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		slog.Info("migration: cleared retired per-session transport", "rows", n)
	}
	return true
}
