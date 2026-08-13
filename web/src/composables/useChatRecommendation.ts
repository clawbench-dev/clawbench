import { computed, reactive, type ComputedRef } from 'vue'

// A single session's recommendation state.
export interface ChatRecommendationEntry {
  /** Concise next-step suggestion text. */
  text: string
  /** Assistant message id this recommendation was generated for. */
  messageId: number | string
  dismissed: boolean
}

// Injected dependencies so the composable stays decoupled and unit-testable.
export interface UseChatRecommendationOptions {
  /** Returns the currently active session id (or undefined when none). */
  activeSessionId: () => string | undefined
  /** Whether the active session is currently streaming an assistant reply. */
  loading: () => boolean
  /** Whether the active session's last message is an assistant reply. */
  isLastMessageAssistant: () => boolean
  /** Id of the active session's last assistant message (or undefined). */
  lastAssistantMessageId: () => number | string | undefined
  /** Fetches the persisted recommendation for a session+message (trimmed, or ''). */
  fetchRemote: (sessionId: string, messageId: number | string) => Promise<string>
}

/**
 * Session-bound conversation recommendation state.
 *
 * Recommendation state is keyed by session id and the *displayed* value is a
 * derived read of the currently active session's slot. Writes from live WS
 * events and async fetches always target their own session's slot, so a
 * recommendation from one session can never leak into another session's view.
 *
 * Each recommendation is additionally bound to the assistant message it was
 * generated for. The displayed value is gated on that message id matching the
 * session's current last assistant message, so a stale recommendation from an
 * earlier reply is never shown (previously the UI briefly flashed the previous
 * reply's recommendation before the current one was generated).
 *
 * A per-session generation counter guards async fetch races: if a session is
 * invalidated (e.g. a new message starts streaming) while a fetch is in flight,
 * the stale result is discarded instead of overwriting newer state.
 */
export function useChatRecommendation(opts: UseChatRecommendationOptions) {
  const cache = reactive(new Map<string, ChatRecommendationEntry>())
  const generation = reactive(new Map<string, number>())

  /** Whether a stored recommendation belongs to the session's current last assistant message. */
  function matchesCurrent(entry: ChatRecommendationEntry | undefined): boolean {
    if (!entry || !entry.text || entry.dismissed) return false
    const lastMsgId = opts.lastAssistantMessageId()
    if (lastMsgId === undefined) return false
    return String(entry.messageId) === String(lastMsgId)
  }

  /** Text of the currently active session's recommendation ('' when none or stale). */
  const current: ComputedRef<string> = computed(() => {
    const id = opts.activeSessionId()
    if (!id) return ''
    const entry = cache.get(id)
    return matchesCurrent(entry) ? entry!.text : ''
  })

  /** Whether the recommendation banner should be visible for the active session. */
  const show: ComputedRef<boolean> = computed(() => {
    const id = opts.activeSessionId()
    if (!id) return false
    if (opts.loading()) return false
    if (!opts.isLastMessageAssistant()) return false
    const entry = cache.get(id)
    return matchesCurrent(entry)
  })

  /** Record a recommendation for a session+message (from a live WS event). */
  function upsert(sessionId: string, text: string, messageId: number | string) {
    const t = (text || '').trim()
    if (!sessionId || !t) return
    cache.set(sessionId, { text: t, messageId, dismissed: false })
  }

  /** Fetch the persisted recommendation for a session+message into its slot, once. */
  async function ensureFetched(sessionId: string, messageId: number | string) {
    if (!sessionId) return
    if (cache.has(sessionId)) return
    const gen = generation.get(sessionId) || 0
    let text: string
    try {
      text = await opts.fetchRemote(sessionId, messageId)
    } catch {
      return
    }
    // Discard if the slot was invalidated while the fetch was in flight.
    if ((generation.get(sessionId) || 0) !== gen) return
    // Don't park a stale value for a session that is actively streaming.
    if (opts.activeSessionId() === sessionId && opts.loading()) return
    const t = (text || '').trim()
    if (!t) return
    cache.set(sessionId, { text: t, messageId, dismissed: false })
  }

  /** Invalidate a session's recommendation (e.g. a new message starts streaming). */
  function invalidate(sessionId: string) {
    if (!sessionId) return
    generation.set(sessionId, (generation.get(sessionId) || 0) + 1)
    cache.delete(sessionId)
  }

  /** Dismiss the active session's recommendation (persists per session). */
  function dismiss() {
    const id = opts.activeSessionId()
    const e = id ? cache.get(id) : undefined
    if (e) e.dismissed = true
  }

  /** Consume the active session's recommendation, returning its text to fill in. */
  function accept(): string {
    const id = opts.activeSessionId()
    const e = id ? cache.get(id) : undefined
    if (matchesCurrent(e)) {
      e!.dismissed = true
      return e!.text
    }
    return ''
  }

  /** Reset all recommendation state (e.g. on logout/teardown). */
  function clear() {
    cache.clear()
    generation.clear()
  }

  return { current, show, upsert, ensureFetched, invalidate, dismiss, accept, clear }
}
