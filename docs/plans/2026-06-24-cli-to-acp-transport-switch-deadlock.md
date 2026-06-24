# CLI→ACP Transport Switch Deadlock Fix & Integration Test

**Goal:** Fix the deadlock when switching Pi (and all ACP-capable backends) from CLI to ACP transport mid-session, and add integration tests for CLI→ACP and ACP→CLI transport switching.

**Architecture:** The root cause is that `external_session_id` (a CLI-specific session ID used for `--session` resume) leaks into the ACP connection pool's `GetOrCreateConn`, where it's incorrectly interpreted as an ACP session ID for `ResumeSession`. The fix: clear `external_session_id` when switching transport to `acp-stdio`, and add a guard in `GetOrCreateConn` to ignore `external_session_id` for ACP-transport sessions.

**Tech Stack:** Go, ACP protocol (JSON-RPC over stdio), ClawBench session management (SQLite)

---

## Root Cause Analysis

1. **CLI phase:** `CLIBackend.ExecuteStream` captures CLI session ID via `session_capture`, stores in `external_session_id` (e.g. `"pi-sess-abc123"`)
2. **Transport switch:** `PATCH /api/ai/session` with `transport: "acp-stdio"`. Calls `UpdateSessionTransport` but does NOT clear `external_session_id`.
3. **Next message with ACP:** `GetOrCreateConn` runs: `conn.acpSID = getExternalSessionID(...)` — sets CLI session ID as ACP session ID!
4. **Deadlock:** `ensureAliveWithSession` tries `ResumeSession` with CLI-format ID. Pi ACP bridge can't recognize it → hangs 60s before fallback to `NewSession`.
