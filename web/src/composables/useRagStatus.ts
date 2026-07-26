import { ref, readonly } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'

export interface RagStatus {
  available: boolean
  mode: string
  has_fts_data: boolean
  has_vec_data: boolean
  embedder_healthy: boolean
  total_messages: number
  indexed_messages: number
  total_chunks: number
  embedded_chunks: number
}

const POLL_INTERVAL = 10_000

const status = ref<RagStatus>({
  available: false,
  mode: 'none',
  has_fts_data: false,
  has_vec_data: false,
  embedder_healthy: false,
  total_messages: 0,
  indexed_messages: 0,
  total_chunks: 0,
  embedded_chunks: 0,
})

let pollTimer: ReturnType<typeof setInterval> | null = null
let activeCount = 0
let visibilityHandler: (() => void) | null = null

async function fetchStatus(): Promise<void> {
  try {
    const data = await apiGet<RagStatus>('/api/rag/status')
    status.value = data
  } catch (err) {
    appLog.w('RagStatus', 'Failed to fetch RAG status', err)
  }
}

function startPolling(): void {
  activeCount++
  if (pollTimer) return
  fetchStatus()
  pollTimer = setInterval(fetchStatus, POLL_INTERVAL)

  // Pause polling when tab is hidden, resume when visible
  if (!visibilityHandler) {
    visibilityHandler = () => {
      if (document.hidden) {
        if (pollTimer) {
          clearInterval(pollTimer)
          pollTimer = null
        }
      } else if (activeCount > 0 && !pollTimer) {
        fetchStatus()
        pollTimer = setInterval(fetchStatus, POLL_INTERVAL)
      }
    }
    document.addEventListener('visibilitychange', visibilityHandler)
  }
}

function stopPolling(): void {
  activeCount = Math.max(0, activeCount - 1)
  if (activeCount > 0) return
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  if (visibilityHandler) {
    document.removeEventListener('visibilitychange', visibilityHandler)
    visibilityHandler = null
  }
}

export function useRagStatus() {
  return {
    status: readonly(status),
    startPolling,
    stopPolling,
    refresh: fetchStatus,
  }
}
