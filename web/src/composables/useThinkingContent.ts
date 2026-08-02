import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'ThinkingContent'

const thinkingTextCache = new Map<string, string>()
const inFlight = new Map<string, Promise<string>>()

function clearThinkingCache() {
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
  thinkingTextCache.set(thinkId, data.text)
  return data.text
}
