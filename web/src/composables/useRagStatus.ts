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
  embedded_messages: number
  index_speed: number   // messages/sec (instantaneous)
  embed_speed: number   // messages/sec (instantaneous)
}

const POLL_INTERVAL = 5_000

const status = ref<RagStatus>({
  available: false,
  mode: 'none',
  has_fts_data: false,
  has_vec_data: false,
  embedder_healthy: false,
  total_messages: 0,
  indexed_messages: 0,
  embedded_messages: 0,
  index_speed: 0,
  embed_speed: 0,
})

let pollTimer: ReturnType<typeof setInterval> | null = null
let visibilityHandler: (() => void) | null = null

// Speed tracking: previous values + timestamp for instantaneous rate
let prevIndexed = 0
let prevEmbedded = 0
let prevFetchTime = 0
// Persist last known speed so label doesn't flicker on zero-delta polls
let lastIndexSpeed = 0
let lastEmbedSpeed = 0

async function fetchStatus(): Promise<void> {
  try {
    const data = await apiGet<RagStatus>('/api/rag/status')

    // Compute instantaneous speed from delta since last fetch
    const now = Date.now()
    if (prevFetchTime > 0 && now > prevFetchTime) {
      const elapsedSec = (now - prevFetchTime) / 1000
      const iSpeed = Math.max(0, (data.indexed_messages - prevIndexed) / elapsedSec)
      const eSpeed = Math.max(0, (data.embedded_messages - prevEmbedded) / elapsedSec)
      // Update persisted speed only on positive delta; keep last known speed otherwise
      if (iSpeed > 0) lastIndexSpeed = iSpeed
      if (eSpeed > 0) lastEmbedSpeed = eSpeed
      // Clear speed when indexing is complete
      if (data.indexed_messages >= data.total_messages) lastIndexSpeed = 0
      if (data.embedded_messages >= data.total_messages) lastEmbedSpeed = 0
    }

    data.index_speed = lastIndexSpeed
    data.embed_speed = lastEmbedSpeed

    prevIndexed = data.indexed_messages
    prevEmbedded = data.embedded_messages
    prevFetchTime = now

    status.value = data
  } catch (err) {
    appLog.w('RagStatus', 'Failed to fetch RAG status', err)
  }
}

function startPolling(): void {
  if (pollTimer) return // already polling
  // Reset speed tracking on fresh start
  prevIndexed = 0
  prevEmbedded = 0
  prevFetchTime = 0
  lastIndexSpeed = 0
  lastEmbedSpeed = 0
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
      } else if (!pollTimer) {
        // Reset speed tracking after visibility change to avoid stale deltas
        prevIndexed = 0
        prevEmbedded = 0
        prevFetchTime = 0
        lastIndexSpeed = 0
        lastEmbedSpeed = 0
        fetchStatus()
        pollTimer = setInterval(fetchStatus, POLL_INTERVAL)
      }
    }
    document.addEventListener('visibilitychange', visibilityHandler)
  }
}

function stopPolling(): void {
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
