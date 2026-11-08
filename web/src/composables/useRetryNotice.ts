import { ref } from 'vue'

/**
 * Transient "the agent is retrying upstream" notice.
 *
 * Why it exists: a CLI agent that retries a rate-limited request emits NOTHING
 * while it does so — opencode 2.2.6 retried a persistent 429 eleven times over
 * ~85 seconds before reporting the terminal error. Without this the user stares
 * at a spinner for over a minute with no way to tell "working" from "hung".
 *
 * It is deliberately NOT part of the transcript: a retry that recovers must
 * leave no trace (see RetryNoticeData in internal/ai/interface.go). The backend
 * sends it as a `retry_notice` stream event, never as a block, and the message
 * list is never touched.
 *
 * Module-level singleton keyed by session id: only the foreground session can
 * stream, and a notice from a background session must not appear over another
 * conversation. `clearRetryNotice(sessionId)` ignores a notice belonging to a
 * different session so a late event cannot wipe the current one.
 */
export interface RetryNotice {
  sessionId: string
  /** Retry number the agent reported (0 when it does not report one). */
  attempt: number
  /** Upstream error text, shown as a detail/tooltip. */
  message: string
  /** Upstream HTTP status (429/503/…), 0 when unknown. */
  httpStatus: number
}

const notice = ref<RetryNotice | null>(null)

/** Record (or refresh) the notice for a session. */
export function showRetryNotice(
  sessionId: string,
  detail: { attempt?: number; message?: string; httpStatus?: number } = {},
): void {
  if (!sessionId) return
  notice.value = {
    sessionId,
    attempt: detail.attempt ?? 0,
    message: detail.message ?? '',
    httpStatus: detail.httpStatus ?? 0,
  }
}

/**
 * Drop the notice. With a sessionId, only that session's notice is dropped —
 * a delayed event from a previous turn must not clear the current one.
 */
export function clearRetryNotice(sessionId?: string): void {
  if (!notice.value) return
  if (sessionId && notice.value.sessionId !== sessionId) return
  notice.value = null
}

/** Read-only accessor for components. */
export function useRetryNotice() {
  return { notice }
}
