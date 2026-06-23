import { ref, type Ref } from 'vue'
import { gt } from '@/composables/useLocale'
import { localConfig } from '@/composables/useSettingsConfig'

export interface UseSwipeSessionOptions {
  currentSessionId: Ref<string>
  switchSession: (sessionId: string) => Promise<void>
}

export function useSwipeSession(options: UseSwipeSessionOptions) {
  const { currentSessionId, switchSession } = options

  // Cached session list
  let sessionCache: { id: string; title: string }[] = []
  let sessionCacheTime = 0
  const CACHE_TTL = 3000 // 3 seconds

  // Session switch indicator state
  const indicatorText = ref('')
  const indicatorDirection = ref<'left' | 'right' | null>(null)
  let indicatorTimer: ReturnType<typeof setTimeout> | null = null

  // Position indicator state
  const sessionIndex = ref(-1)
  const sessionTotal = ref(0)

  function updatePosition(sessions: { id: string; title: string }[]) {
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
      sessionCache = (data.sessions || []).map((s: any) => ({
        id: s.id,
        title: s.title || gt('session.unnamed'),
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

  async function swipeToNext() {
    const sessions = await fetchSessions()
    if (sessions.length <= 1) return
    const idx = sessions.findIndex(s => s.id === currentSessionId.value)
    // Next = index - 1 because sessions are ordered by updated_at DESC
    // swiping left (next) goes to a more recent session
    const nextIdx = idx > 0 ? idx - 1 : sessions.length - 1
    if (nextIdx === idx) return
    showIndicator(sessions[nextIdx].title, 'left')
    await switchSession(sessions[nextIdx].id)
    sessionIndex.value = nextIdx
  }

  async function swipeToPrev() {
    const sessions = await fetchSessions()
    if (sessions.length <= 1) return
    const idx = sessions.findIndex(s => s.id === currentSessionId.value)
    // Prev = index + 1 because sessions are ordered by updated_at DESC
    // swiping right (prev) goes to an older session
    const prevIdx = idx < sessions.length - 1 ? idx + 1 : 0
    if (prevIdx === idx) return
    showIndicator(sessions[prevIdx].title, 'right')
    await switchSession(sessions[prevIdx].id)
    sessionIndex.value = prevIdx
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
      // Swipe left → next session
      swipeToNext()
    } else {
      // Swipe right → previous session
      swipeToPrev()
    }
  }

  return {
    swipeToNext,
    swipeToPrev,
    onTouchStart,
    onTouchEnd,
    indicatorText,
    indicatorDirection,
    sessionIndex,
    sessionTotal,
  }
}
