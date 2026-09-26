import { ref, reactive } from 'vue'
import { appLog } from '@/utils/appLog'
import { getShareThinking } from '@/share/shareMode'

const TAG = 'ThinkingContent'

const thinkingTextCache = reactive(new Map<string, string>())
const inFlight = new Map<string, Promise<string>>()
// Bumped by clearThinkingCache. A fetch started before a clear must not write
// its result afterwards: content_reset deletes the message's chat_thinking rows
// (the failed Prompt's reasoning must not survive the retry), and a request that
// was already in flight would otherwise repopulate the cache with exactly the
// text that was just discarded.
let cacheGeneration = 0

// think_ids whose cached text was fetched WHILE THE BLOCK WAS STILL STREAMING.
//
// The backend appends reasoning to chat_thinking incrementally, so a fetch
// taken mid-stream returns only whatever had been flushed at that instant — a
// provisional prefix, not the finished reasoning. The streaming render needs it
// (otherwise the block shows only the deltas that arrived after the switch), but
// once the block finishes that snapshot is stale. Without this marker nothing
// ever refetched it: the block stayed frozen at the partial prefix forever,
// showing "thinking that stops mid-sentence and never continues".
const provisionalIds = new Set<string>()

export function clearThinkingCache() {
  cacheGeneration++
  thinkingTextCache.clear()
  provisionalIds.clear()
}

export function useThinkingContent() {
  const loading = ref<Record<string, boolean>>({})
  const errors = ref<Record<string, string>>({})

  function cachedText(thinkId: string): string | undefined {
    return thinkingTextCache.get(thinkId)
  }

  /** Whether the cached text for this id is a mid-stream snapshot that must be
   *  replaced by the final reasoning once the block finishes. */
  function isProvisional(thinkId: string): boolean {
    return provisionalIds.has(thinkId)
  }

  /**
   * Load a thinking block's text.
   *
   * `provisional` marks the request as one taken while the block is still
   * streaming: its result is cached (so the block can render immediately) but
   * flagged so that a later non-provisional request refetches the final text
   * instead of returning the stale snapshot.
   */
  async function loadThinking(
    thinkId: string,
    msgId: string | number,
    sessionId?: string,
    provisional = false,
  ): Promise<string> {
    const cached = thinkingTextCache.get(thinkId)
    // A final request is never satisfied by a provisional snapshot.
    if (cached !== undefined && (provisional || !provisionalIds.has(thinkId))) return cached

    const pending = inFlight.get(thinkId)
    if (pending) {
      if (provisional) return pending
      // A final request must not adopt an in-flight PROVISIONAL fetch's result
      // (it would resolve to the partial prefix again). Wait it out, then
      // refetch; the recursion terminates because the pending entry is cleared
      // by the other call's finally before this continuation runs.
      await pending.catch(() => { /* the refetch below reports the real error */ })
      return loadThinking(thinkId, msgId, sessionId, false)
    }

    if (provisional) provisionalIds.add(thinkId)
    loading.value[thinkId] = true
    delete errors.value[thinkId]
    const p = doFetch(thinkId, msgId, sessionId)
    inFlight.set(thinkId, p)
    try {
      const text = await p
      if (!provisional) provisionalIds.delete(thinkId)
      return text
    } catch (e) {
      errors.value[thinkId] = e instanceof Error ? e.message : String(e)
      throw e
    } finally {
      loading.value[thinkId] = false
      inFlight.delete(thinkId)
    }
  }

  return { loading, errors, cachedText, loadThinking, clearThinkingCache, isProvisional }
}

async function doFetch(thinkId: string, msgId: string | number, sessionId?: string): Promise<string> {
  const gen = cacheGeneration
  // Session-share mode inlines the thinking text into the snapshot, so the
  // authenticated detail endpoint is never reachable (and never needed).
  const shared = getShareThinking(msgId, thinkId)
  if (shared !== undefined) {
    if (gen === cacheGeneration) thinkingTextCache.set(thinkId, shared)
    return shared
  }

  let url = `/api/ai/chat/thinking?think_id=${encodeURIComponent(thinkId)}&message_id=${encodeURIComponent(msgId)}`
  if (sessionId) url += `&session_id=${encodeURIComponent(sessionId)}`
  let resp: Response
  try {
    resp = await fetch(url)
  } catch (e) {
    appLog.w(TAG, 'thinking fetch failed:', e)
    throw e
  }
  if (!resp.ok) {
    appLog.w(TAG, 'thinking fetch failed:', resp.status)
    throw new Error(`thinking fetch failed: ${resp.status}`)
  }
  const data = await resp.json()
  if (!data.text) {
    appLog.w(TAG, 'thinking text empty for', thinkId)
    throw new Error('thinking text empty')
  }
  // A clear during the request means the rows this text came from are gone
  // (content_reset) — do not resurrect them. The caller still gets the value.
  if (gen === cacheGeneration) thinkingTextCache.set(thinkId, data.text)
  return data.text
}
