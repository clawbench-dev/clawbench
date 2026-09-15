import { appLog } from '@/utils/appLog'

/**
 * Measures how long a user-perceived cancel takes: from the stop click to the
 * terminal event that clears the spinner.
 *
 * Why this exists as its own module rather than a ref inside useChatStream:
 * a session can be cleared by either of two independent paths — the
 * chat_stream terminal event (useChatStream) or the session_update safety net
 * (useChatSession) — and the click happens in the former while the latter may
 * win the race. A module-level timestamp is shared by both, so the measurement
 * is reported exactly once no matter which path arrives first.
 *
 * The backend stops the agent instantly; the delay is finalize/persistence
 * work before the terminal event is emitted. This timer measures that delay
 * from the client side, which is the number the user actually experiences.
 */

const TAG = 'ChatStream'

/** Click timestamp, or 0 when no cancel is pending. */
let requestedAt = 0

/**
 * Upper bound for a reported round-trip. A pending click older than this is
 * treated as stale (e.g. the terminal event never arrived and the state was
 * cleared much later by an unrelated reload) rather than reported as a real
 * multi-minute cancel.
 */
const MAX_REPORTABLE_MS = 120_000

/** Records the stop click. Called once per cancel request. */
export function markCancelRequested(): void {
  requestedAt = Date.now()
}

/**
 * Reports the round-trip if a cancel is pending, then clears it. Safe to call
 * from every terminal path: the first call reports, the rest are no-ops.
 */
export function reportCancelRoundTrip(terminal: string): void {
  if (!requestedAt) return
  const elapsed = Date.now() - requestedAt
  requestedAt = 0
  if (elapsed > MAX_REPORTABLE_MS) return
  appLog.i(TAG, `cancel round-trip: ${elapsed}ms (terminal=${terminal})`)
}

/** Clears a pending measurement without reporting (tests, session teardown). */
export function resetCancelRoundTrip(): void {
  requestedAt = 0
}
