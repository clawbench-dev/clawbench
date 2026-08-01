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
  status: string       // "idle" | "computing" | "done" | "error" | "cancelled"
  phase: string        // "extracting" | "clustering" | "saving"
  msg_count: number
  cluster_count: number
  elapsed_ms: number
  mode: string
  progress_pct: number // 0-100 fine-grained progress within phase
  error?: string
}

interface MessageClustersResponse {
  clusters: MessageCluster[]
  total: number
  mode: string
  progress: string
  updated_at: string
}

// ── Module-level singleton state ──
// Computing/progress must persist across drawer open/close cycles.

const clusters = ref<MessageCluster[]>([])
const loaded = ref(false)
const loading = ref(false)
const computing = ref(false)
const progress = ref<ClusterProgress>({
  status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0
})
const mode = ref<string>('')
const updatedAt = ref<string>('')
let fetchingGuard = false

// ── Cancel guard ──
// After user cancels, stale goroutine may still send WS events with status="computing".
// This flag blocks those stale events from re-setting computing=true.
// It is cleared when a terminal event (done/error/cancelled) arrives or
// when the user explicitly starts a new computation.
let cancelledGuard = false

// ── WebSocket cluster_progress listener (module-level, never unregistered) ──
// WS is the sole authoritative data source during computing.
// No polling — drawer open does a single /compute/status query to sync initial state,
// then WS drives all subsequent progress updates.
const { onEvent } = useGlobalEvents()
onEvent((event: string, data: unknown) => {
  if (event !== 'cluster_progress') return
  const d = data as ClusterProgress
  if (!d?.status) return

  // After cancel, block stale "computing" events from the dying goroutine.
  // Only terminal events (done/error/cancelled) can clear the guard.
  if (cancelledGuard && d.status === 'computing') {
    appLog.d('MsgCluster', 'Blocked stale computing WS event after cancel')
    return
  }

  progress.value = d

  if (d.status === 'done') {
    computing.value = false
    cancelledGuard = false
    fetchClusters()
  } else if (d.status === 'error') {
    computing.value = false
    cancelledGuard = false
  } else if (d.status === 'cancelled') {
    // Cancelled → return to idle (user can re-trigger)
    computing.value = false
    cancelledGuard = false
    progress.value = { status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
  } else if (d.status === 'computing') {
    computing.value = true
  }
})

// ── Read cached results (instant) ──
async function fetchClusters() {
  if (fetchingGuard) return
  fetchingGuard = true
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
    fetchingGuard = false
  }
}

// ── Single query: sync progress state from server ──
// Called once when drawer opens to detect ongoing computation.
// After this, WS events drive all further updates.
async function syncProgressOnce() {
  try {
    const resp = await fetch('/api/chat/message-clusters/compute/status')
    if (!resp.ok) return
    const data: ClusterProgress = await resp.json()
    // Only sync coarse fields; phase and progress_pct are WS-only.
    // If computation is done, error, or cancelled, update fully.
    if (data.status === 'done' || data.status === 'error') {
      progress.value = data
      computing.value = false
      cancelledGuard = false
      if (data.status === 'done') {
        await fetchClusters()
      }
    } else if (data.status === 'cancelled') {
      // Cancelled → return to idle
      progress.value = { status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
      computing.value = false
      cancelledGuard = false
    } else if (data.status === 'computing') {
      computing.value = true
      // Update coarse fields only; WS will fill in phase and progress_pct
      progress.value.status = data.status
      progress.value.msg_count = data.msg_count
      progress.value.elapsed_ms = data.elapsed_ms
    }
  } catch (e) {
    appLog.e('MsgCluster', `Progress sync error: ${e}`)
  }
}

// ── Trigger on-demand computation ──
async function startCompute() {
  try {
    const resp = await fetch('/api/chat/message-clusters/compute', { method: 'POST' })
    if (resp.status === 409) {
      appLog.i('MsgCluster', 'Computation already running')
      return 'already_running'
    }
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
    computing.value = true
    cancelledGuard = false // new computation clears cancel guard
    progress.value = {
      ...progress.value,
      status: 'computing',
      phase: 'extracting',
      elapsed_ms: 0,
      progress_pct: 0,
    }
    return 'started'
  } catch (e) {
    appLog.e('MsgCluster', `Failed to start computation: ${e}`)
    return 'error'
  }
}

// ── Cancel in-progress computation ──
async function cancelCompute() {
  cancelledGuard = true
  computing.value = false
  // Immediately return to idle state
  progress.value = { status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
  try {
    const resp = await fetch('/api/chat/message-clusters/compute/cancel', { method: 'POST' })
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`)
  } catch (e) {
    appLog.e('MsgCluster', `Failed to cancel computation: ${e}`)
  }
}

// ── Composable returns module-level refs (singleton pattern) ──
export function useMessageClusters() {
  return { clusters, loaded, loading, computing, progress, mode, updatedAt, fetchClusters, startCompute, cancelCompute, syncProgressOnce }
}

// ── Reset for tests only ──
export function resetMessageClustersState() {
  clusters.value = []
  loaded.value = false
  loading.value = false
  computing.value = false
  cancelledGuard = false
  progress.value = { status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
  mode.value = ''
  updatedAt.value = ''
  fetchingGuard = false
}
