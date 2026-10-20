import { ref, type Ref } from 'vue'
import { gt } from '@/composables/useLocale'
import { localConfig } from '@/composables/useSettingsConfig'

export interface UseSwipeSessionOptions {
  currentSessionId: Ref<string>
  switchSession: (sessionId: string) => Promise<void>
}

/**
 * Circular navigation over the session list in its *rendered* order
 * (pinned DESC, sort_order ASC, created_at DESC — see service.GetSessions).
 *
 * "Next" is the next row down (index + 1) and "prev" the row above
 * (index - 1), so pressing → / swiping right always walks the list the same
 * way the eye does. A current index of -1 means the active session is not in
 * the fetched list (just archived, or created after the cache was filled);
 * it is treated as "before the first row", so next lands on the first row and
 * prev on the last.
 */
export function getNextSessionIndex(currentIndex: number, total: number): number {
  if (total <= 1) return currentIndex
  return currentIndex < total - 1 ? currentIndex + 1 : 0
}

export function getPrevSessionIndex(currentIndex: number, total: number): number {
  if (total <= 1) return currentIndex
  return currentIndex > 0 ? currentIndex - 1 : total - 1
}

export function useSwipeSession(options: UseSwipeSessionOptions) {
  const { currentSessionId, switchSession } = options

  // Cached session list
  interface SessionMeta { id: string; title: string; unreadCount?: number; pendingApproval?: boolean }
  let sessionCache: SessionMeta[] = []
  let sessionCacheTime = 0
  const CACHE_TTL = 3000 // 3 seconds

  // Session switch indicator state
  const indicatorText = ref('')
  const indicatorDirection = ref<'left' | 'right' | null>(null)
  let indicatorTimer: ReturnType<typeof setTimeout> | null = null

  // Position indicator state
  const sessionIndex = ref(-1)
  const sessionTotal = ref(0)

  function updatePosition(sessions: SessionMeta[]) {
    sessionTotal.value = sessions.length
    const idx = sessions.findIndex(s => s.id === currentSessionId.value)
    sessionIndex.value = idx
  }

  async function fetchSessions() {
    const now = Date.now()
    if (sessionCache.length > 0 && now - sessionCacheTime < CACHE_TTL) {
      return sessionCache
    }
    try {
      const resp = await fetch('/api/ai/sessions')
      if (!resp.ok) return sessionCache
      const data = await resp.json()
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      sessionCache = (data.sessions || []).map((s: any) => ({
        id: s.id,
        title: s.title || gt('session.unnamed'),
        unreadCount: s.unreadCount || 0,
        pendingApproval: !!s.pendingApproval,
      }))
      sessionCacheTime = now
      updatePosition(sessionCache)
      return sessionCache
    } catch {
      return sessionCache
    }
  }

  function showIndicator(title: string, direction: 'left' | 'right') {
    if (indicatorTimer) clearTimeout(indicatorTimer)
    indicatorText.value = title
    indicatorDirection.value = direction
    indicatorTimer = setTimeout(() => {
      indicatorText.value = ''
      indicatorDirection.value = null
    }, 1500)
  }

  // "Next" always means the next row down the session list, and "prev" the row
  // above it — the same order the list itself is rendered in. Both the swipe
  // gesture and the desktop Ctrl+←/→ shortcut use this mapping, so moving one
  // step down the list is always the same physical action: swipe right / press
  // →. Before this, "next" walked *up* the list, so from the first session
  // pressing → wrapped around to the oldest one instead of the second.
  async function swipeToNext() {
    const sessions = await fetchSessions()
    if (sessions.length <= 1) return
    const idx = sessions.findIndex(s => s.id === currentSessionId.value)
    const nextIdx = getNextSessionIndex(idx, sessions.length)
    if (nextIdx === idx) return
    // The indicator animates in the direction the content travelled, so moving
    // forward (right) slides in from the left.
    showIndicator(sessions[nextIdx].title, 'right')
    await switchSession(sessions[nextIdx].id)
    sessionIndex.value = nextIdx
  }

  async function swipeToPrev() {
    const sessions = await fetchSessions()
    if (sessions.length <= 1) return
    const idx = sessions.findIndex(s => s.id === currentSessionId.value)
    const prevIdx = getPrevSessionIndex(idx, sessions.length)
    if (prevIdx === idx) return
    showIndicator(sessions[prevIdx].title, 'left')
    await switchSession(sessions[prevIdx].id)
    sessionIndex.value = prevIdx
  }

  /**
   * Jump to the next unread session after the current one (walking down the
   * list and wrapping around). Returns the target session's title, or null
   * when there are no unread sessions left. Does NOT show the swipe indicator.
   */
  async function jumpToNextUnread(): Promise<string | null> {
    const sessions = await fetchSessions()
    if (sessions.length === 0) return null
    const isUnread = (s: SessionMeta) => (s.unreadCount || 0) > 0 || s.pendingApproval
    const startIdx = sessions.findIndex(s => s.id === currentSessionId.value)
    const start = startIdx >= 0 ? startIdx : 0
    for (let offset = 1; offset <= sessions.length; offset++) {
      const idx = (start + offset) % sessions.length
      if (startIdx >= 0 && idx === start) continue // never jump to the currently active session
      if (isUnread(sessions[idx])) {
        await switchSession(sessions[idx].id)
        sessionIndex.value = idx
        return sessions[idx].title
      }
    }
    return null
  }

  // Touch state
  const SWIPE_THRESHOLD = 80 // px horizontal
  const SWIPE_MAX_DURATION = 500 // ms

  let touchStartX = 0
  let touchStartY = 0
  let touchStartTime = 0
  let touchInsideScrollable = false

  /** Walk from target up to boundary (exclusive). Return true if any ancestor
   *  has horizontal overflow with actual scrollable content. */
  function isInsideHorizontalScroll(target: EventTarget | null, boundary: EventTarget | null): boolean {
    if (!(target instanceof Element) || !(boundary instanceof Element)) return false
    let el: Element | null = target
    while (el && el !== boundary) {
      const style = getComputedStyle(el)
      const overflowX = style.overflowX
      if ((overflowX === 'auto' || overflowX === 'scroll') && el.scrollWidth > el.clientWidth) {
        return true
      }
      el = el.parentElement
    }
    return false
  }

  function onTouchStart(e: TouchEvent) {
    if (!localConfig.swipeSession) return
    const touch = e.touches[0]
    touchStartX = touch.clientX
    touchStartY = touch.clientY
    touchStartTime = Date.now()
    touchInsideScrollable = isInsideHorizontalScroll(e.target, e.currentTarget)
  }

  function onTouchEnd(e: TouchEvent) {
    if (!localConfig.swipeSession) return
    if (touchInsideScrollable) return

    const touch = e.changedTouches[0]
    const deltaX = touch.clientX - touchStartX
    const deltaY = touch.clientY - touchStartY
    const duration = Date.now() - touchStartTime

    // Must be fast enough
    if (duration > SWIPE_MAX_DURATION) return
    // Must be more horizontal than vertical
    if (Math.abs(deltaY) > Math.abs(deltaX) * 0.75) return
    // Must exceed threshold
    if (Math.abs(deltaX) < SWIPE_THRESHOLD) return

    if (deltaX < 0) {
      // Swipe left → previous session (up the list)
      swipeToPrev()
    } else {
      // Swipe right → next session (down the list)
      swipeToNext()
    }
  }

  return {
    swipeToNext,
    swipeToPrev,
    jumpToNextUnread,
    onTouchStart,
    onTouchEnd,
    indicatorText,
    indicatorDirection,
    sessionIndex,
    sessionTotal,
  }
}
