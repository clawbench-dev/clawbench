package service

import (
	"log/slog"

	"clawbench/internal/model"
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

// MigrateRemoveRetiredBackends deletes the agents of backends that no longer
// exist: vecli only ever spoke its own CLI stream, and copilot declared an ACP
// command that was never verified, so it only ever ran over CLI. Both were
// removed with the CLI transport.
//
// An upgraded install can still hold rows for them. Such an agent has no ACP
// command and no in-process implementation, so every turn fails — leaving the
// row puts a permanently broken entry in every list and picker. The delete
// reuses the same takeover as DELETE /api/agents: conversations move to a
// surviving agent (history kept, one system notice written), never left
// dangling.
//
// Returns false when the rows cannot be deleted yet (no surviving agent to hand
// them to, or a write error) so the ledger retries on the next start instead of
// stranding the conversations.
func MigrateRemoveRetiredBackends() bool {
	type agentRow struct {
		id, name, backend, acpCommand string
	}
	query := func(q string, args ...any) ([]agentRow, error) {
		rows, err := store.ReadDB().Query(q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []agentRow
		for rows.Next() {
			var r agentRow
			if err := rows.Scan(&r.id, &r.name, &r.backend, &r.acpCommand); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	toAgent := func(r agentRow) *model.Agent {
		return &model.Agent{ID: r.id, Name: r.name, Backend: r.backend, AcpCommand: r.acpCommand}
	}

	retired, err := query(
		`SELECT id, name, backend, acp_command FROM agents
		  WHERE backend IN ('vecli', 'copilot')
		  ORDER BY sort_order, id`)
	if err != nil {
		slog.Warn("migration: listing retired-backend agents failed", "err", err)
		return false
	}
	if len(retired) == 0 {
		return true
	}

	// Prefer a global, enabled agent so the handed-over sessions stay visible in
	// every project. Fall back to a scoped one, then to a disabled one: a broken
	// agent id is worse than a hidden target.
	//
	// Every candidate must also be able to RUN a turn. An agent with neither an
	// ACP command nor the in-process implementation cannot start one (see
	// model.Agent.DeriveTransport), so handing the conversations to it would
	// replace one permanently broken agent with another — exactly what this
	// migration exists to avoid.
	var replacement *model.Agent
	for _, q := range []string{
		`SELECT id, name, backend, acp_command FROM agents
		  WHERE backend NOT IN ('vecli', 'copilot')
		    AND (COALESCE(acp_command, '') != '' OR backend = 'builtin')
		    AND disabled = 0 AND project_id IS NULL
		  ORDER BY sort_order, id LIMIT 1`,
		`SELECT id, name, backend, acp_command FROM agents
		  WHERE backend NOT IN ('vecli', 'copilot')
		    AND (COALESCE(acp_command, '') != '' OR backend = 'builtin')
		    AND disabled = 0
		  ORDER BY sort_order, id LIMIT 1`,
		`SELECT id, name, backend, acp_command FROM agents
		  WHERE backend NOT IN ('vecli', 'copilot')
		    AND (COALESCE(acp_command, '') != '' OR backend = 'builtin')
		  ORDER BY sort_order, id LIMIT 1`,
	} {
		rows, err := query(q)
		if err != nil {
			slog.Warn("migration: resolving a takeover target failed", "err", err)
			return false
		}
		if len(rows) > 0 {
			replacement = toAgent(rows[0])
			break
		}
	}
	if replacement == nil {
		slog.Warn("migration: retired-backend agents left in place — no agent to hand their conversations to",
			"agents", len(retired))
		return false
	}

	removed := 0
	for _, r := range retired {
		if _, err := DeleteAgentWithTakeover(toAgent(r), replacement); err != nil {
			slog.Warn("migration: deleting a retired-backend agent failed", "agent", r.id, "err", err)
			continue
		}
		removed++
	}
	slog.Info("migration: removed retired-backend agents",
		"removed", removed, "replacement", replacement.ID)
	return removed == len(retired)
}
