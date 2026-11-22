package service

import (
	"log/slog"

	"clawbench/internal/store"
)

// MigrateTransportToSinglePath rewrites the retired per-agent transport column
// to the only two values that exist now: "acp-stdio" for every agent with an
// ACP command, "in-process" for the built-in agent, and "" for an agent that
// has neither (nothing can start it).
//
// The column used to be a user choice ("cli" vs "acp-stdio") and drove the
// backend factory. CLI support is gone, so the factory decides from the command
// alone; the column is a read-only derived value for clients. Doing the rewrite
// here — once — is what lets the runtime stop carrying "what if the row still
// says cli" branches.
func MigrateTransportToSinglePath() bool {
	res, err := store.WriteExec(`
		UPDATE agents
		   SET transport = CASE
		         WHEN COALESCE(acp_command, '') != '' THEN 'acp-stdio'
		         WHEN backend = 'builtin' THEN 'in-process'
		         ELSE ''
		       END`)
	if err != nil {
		slog.Warn("migration: normalize agent transport failed", "err", err)
		return false
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		slog.Info("migration: normalized agent transport to the single path", "rows", n)
	}
	return true
}

// MigrateSessionTransportColumns retires the per-session transport column.
//
// The column used to pin one session to CLI. Nothing reads it any more (the
// agent's command decides), but a stale value made clients keep rendering the
// CLI-facing gates for that session. Clearing it leaves every row on the single
// meaning of "derived from the agent".
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
