import { computed, reactive, type ComputedRef } from 'vue'

// A single session's recommendation state.
export interface ChatRecommendationEntry {
  text: string
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
  /** Fetches the persisted recommendation text for a session (trimmed, or ''). */
  fetchRemote: (sessionId: string) => Promise<string>
}

/**
 * Session-bound conversation recommendation state.
 *
 * Recommendation state is keyed by session id and the *displayed* value is a
 * derived read of the currently active session's slot. Writes from live WS
 * events and async fetches always target their own session's slot, so a
 * recommendation from one session can never leak into another session's view.
 *
 * A per-session generation counter guards async fetch races: if a session is
 * invalidated (e.g. a new message starts streaming) while a fetch is in flight,
 * the stale result is discarded instead of overwriting newer state.
 */
export function useChatRecommendation(opts: UseChatRecommendationOptions) {
  const cache = reactive(new Map<string, ChatRecommendationEntry>())
  const generation = reactive(new Map<string, number>())

  /** Text of the currently active session's recommendation ('' when none). */
  const current: ComputedRef<string> = computed(() => {
    const id = opts.activeSessionId()
    if (!id) return ''
    return cache.get(id)?.text ?? ''
  })

  /** Whether the recommendation banner should be visible for the active session. */
  const show: ComputedRef<boolean> = computed(() => {
    const id = opts.activeSessionId()
    if (!id) return false
    if (opts.loading()) return false
    if (!opts.isLastMessageAssistant()) return false
    const e = cache.get(id)
    return !!e && !!e.text && !e.dismissed
  })

  /** Record a recommendation for a session (from a live WS event). */
  function upsert(sessionId: string, text: string) {
    const t = (text || '').trim()
    if (!sessionId || !t) return
    cache.set(sessionId, { text: t, dismissed: false })
  }

  /** Fetch the persisted recommendation for a session into its slot, once. */
  async function ensureFetched(sessionId: string) {
    if (!sessionId) return
    if (cache.has(sessionId)) return
    const gen = generation.get(sessionId) || 0
    let text: string
    try {
      text = await opts.fetchRemote(sessionId)
    } catch {
      return
    }
    // Discard if the slot was invalidated while the fetch was in flight.
    if ((generation.get(sessionId) || 0) !== gen) return
    // Don't park a stale value for a session that is actively streaming.
    if (opts.activeSessionId() === sessionId && opts.loading()) return
    const t = (text || '').trim()
    if (!t) return
    cache.set(sessionId, { text: t, dismissed: false })
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
    if (e && e.text && !e.dismissed) {
      e.dismissed = true
      return e.text
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
