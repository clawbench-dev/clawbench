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

export function clearThinkingCache() {
  cacheGeneration++
  thinkingTextCache.clear()
}

export function useThinkingContent() {
  const loading = ref<Record<string, boolean>>({})
  const errors = ref<Record<string, string>>({})

  function cachedText(thinkId: string): string | undefined {
    return thinkingTextCache.get(thinkId)
  }

  async function loadThinking(thinkId: string, msgId: string | number, sessionId?: string): Promise<string> {
    const cached = thinkingTextCache.get(thinkId)
    if (cached !== undefined) return cached
    const pending = inFlight.get(thinkId)
    if (pending) return pending

    loading.value[thinkId] = true
    delete errors.value[thinkId]
    const p = doFetch(thinkId, msgId, sessionId)
    inFlight.set(thinkId, p)
    try {
      const text = await p
      return text
    } catch (e) {
      errors.value[thinkId] = e instanceof Error ? e.message : String(e)
      throw e
    } finally {
      loading.value[thinkId] = false
      inFlight.delete(thinkId)
    }
  }

  return { loading, errors, cachedText, loadThinking, clearThinkingCache }
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
