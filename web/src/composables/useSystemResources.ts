import { ref, onUnmounted } from 'vue'
import { appLog } from '@/utils/appLog'

export interface CPUInfo {
  percent: number
  core_count: number
}

export interface MemoryInfo {
  used: number
  total: number
  percent: number
}

export interface DiskInfo {
  used: number
  total: number
  percent: number
}

export interface NetworkInfo {
  upload_rate: number
  download_rate: number
}

export interface SystemResources {
  cpu: CPUInfo
  memory: MemoryInfo
  disk: DiskInfo
  network: NetworkInfo
  errors?: string[]
}

const POLL_INTERVAL = 1000 // 1s — fastest rate needed (CPU, network)

let activeCount = 0
let timer: ReturnType<typeof setInterval> | null = null
let initTimeout: ReturnType<typeof setTimeout> | null = null

const resources = ref<SystemResources>({
  cpu: { percent: 0, core_count: 0 },
  memory: { used: 0, total: 0, percent: 0 },
  disk: { used: 0, total: 0, percent: 0 },
  network: { upload_rate: 0, download_rate: 0 },
})

const loading = ref(false)

async function fetchResources() {
  try {
    loading.value = true
    const resp = await fetch('/api/system/resources')
    if (!resp.ok) return
    const data: SystemResources = await resp.json()
    resources.value = data
  } catch (e) {
    appLog.w('SystemResources', 'fetch failed', e)
  } finally {
    loading.value = false
  }
}

function startPolling() {
  activeCount++
  if (activeCount > 1) return // already polling

  // Initial fetch — two calls: first initializes CPU/network sampler,
  // second returns actual calculated rates
  fetchResources().then(() => {
    // Short delay before second fetch to allow CPU/network interval sampling
    initTimeout = setTimeout(() => fetchResources(), 200)
  })

  timer = setInterval(fetchResources, POLL_INTERVAL)
}

function stopPolling() {
  activeCount = Math.max(0, activeCount - 1)
  if (activeCount > 0) return // still in use

  if (timer) {
    clearInterval(timer)
    timer = null
  }
  if (initTimeout) {
    clearTimeout(initTimeout)
    initTimeout = null
  }
}

export function useSystemResources() {
  onUnmounted(() => {
    stopPolling()
  })

  return {
    resources,
    loading,
    startPolling,
    stopPolling,
  }
}
