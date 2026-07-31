import { ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { useGlobalEvents } from '@/composables/useGlobalEvents'

export interface MessageCluster {
  id: number
  representative: string
  variants: string[]
  total_count: number
  representative_count: number
}

export interface ClusterProgress {
  status: string       // "idle" | "computing" | "done" | "error"
  phase: string        // "extracting" | "clustering" | "saving"
  msg_count: number
  cluster_count: number
  elapsed_ms: number
  mode: string
  error?: string
}

interface MessageClustersResponse {
  clusters: MessageCluster[]
  total: number
  mode: string
  progress: string
  updated_at: string
}

export function useMessageClusters() {
  const clusters = ref<MessageCluster[]>([])
  const loaded = ref(false)
  const loading = ref(false)
  const computing = ref(false)
  const progress = ref<ClusterProgress>({
    status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: ''
  })
  const mode = ref<string>('')
  const updatedAt = ref<string>('')

  // ── WebSocket cluster_progress listener ──
  const { onEvent } = useGlobalEvents()
  const unsubscribeWs = onEvent((event: string, data: unknown) => {
    if (event !== 'cluster_progress') return
    const d = data as ClusterProgress
    if (!d?.status) return
    progress.value = d
    if (d.status === 'done') {
      computing.value = false
      stopPolling()
      fetchClusters()
    } else if (d.status === 'error') {
      computing.value = false
      stopPolling()
    } else if (d.status === 'computing') {
      computing.value = true
    }
  })

  // Read cached results (instant)
  async function fetchClusters() {
    loading.value = true
    try {
      const resp = await fetch('/api/chat/message-clusters')
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
      const data: MessageClustersResponse = await resp.json()
      clusters.value = data.clusters
      mode.value = data.mode
      updatedAt.value = data.updated_at
      progress.value.status = data.progress
      loaded.value = true
    } catch (e) {
      appLog.e('MsgCluster', `Failed to fetch clusters: ${e}`)
    } finally {
      loading.value = false
    }
  }

  // Trigger on-demand computation
  async function startCompute() {
    try {
      const resp = await fetch('/api/chat/message-clusters/compute', { method: 'POST' })
      if (resp.status === 409) {
        appLog.i('MsgCluster', 'Computation already running')
        return
      }
      if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
      computing.value = true
      progress.value.status = 'computing'
      pollProgress()
    } catch (e) {
      appLog.e('MsgCluster', `Failed to start computation: ${e}`)
    }
  }

  // Poll progress every 2 seconds
  let pollTimer: ReturnType<typeof setInterval> | null = null
  function pollProgress() {
    pollTimer = setInterval(async () => {
      try {
        const resp = await fetch('/api/chat/message-clusters/compute/status')
        if (!resp.ok) return
        const data: ClusterProgress = await resp.json()
        progress.value = data
        if (data.status === 'done' || data.status === 'error') {
          stopPolling()
          computing.value = false
          if (data.status === 'done') {
            await fetchClusters()
          }
        }
      } catch (e) {
        appLog.e('MsgCluster', `Progress poll error: ${e}`)
      }
    }, 2000)
  }

  function stopPolling() {
    if (pollTimer !== null) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  return { clusters, loaded, loading, computing, progress, mode, updatedAt, fetchClusters, startCompute, stopPolling, unsubscribeWs }
}
