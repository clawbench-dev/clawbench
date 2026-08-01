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
}

const status = ref<RagStatus>({
  available: false,
  mode: 'none',
  has_fts_data: false,
  has_vec_data: false,
  embedder_healthy: false,
  total_messages: 0,
  indexed_messages: 0,
  embedded_messages: 0,
})

async function refresh(): Promise<void> {
  try {
    const data = await apiGet<RagStatus>('/api/rag/status')
    status.value = data
  } catch (err) {
    appLog.w('RagStatus', 'Failed to fetch RAG status', err)
  }
}

/** @internal Reset all state — for tests only */
export function _resetForTesting() {
  status.value = {
    available: false,
    mode: 'none',
    has_fts_data: false,
    has_vec_data: false,
    embedder_healthy: false,
    total_messages: 0,
    indexed_messages: 0,
    embedded_messages: 0,
  }
}

export function useRagStatus() {
  return {
    status: readonly(status),
    refresh,
  }
}
