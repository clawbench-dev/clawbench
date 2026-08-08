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

export interface DiskIOInfo {
  read_rate: number
  write_rate: number
}

export interface LoadInfo {
  load1: number
  load5: number
  load15: number
}

export interface SystemResources {
  cpu: CPUInfo
  memory: MemoryInfo
  disk: DiskInfo
  disk_io: DiskIOInfo
  network: NetworkInfo
  load: LoadInfo
  errors?: string[]
}

const POLL_INTERVAL = 1000 // 1s — fastest rate needed (CPU, network)
const BACKGROUND_POLL_INTERVAL = 5000 // 5s — background polling when menu is closed

let activeCount = 0
let backgroundCount = 0
let currentInterval = 0
let timer: ReturnType<typeof setInterval> | null = null
let initTimeout: ReturnType<typeof setTimeout> | null = null

const resources = ref<SystemResources>({
  cpu: { percent: 0, core_count: 0 },
  memory: { used: 0, total: 0, percent: 0 },
  disk: { used: 0, total: 0, percent: 0 },
  disk_io: { read_rate: 0, write_rate: 0 },
  network: { upload_rate: 0, download_rate: 0 },
  load: { load1: 0, load5: 0, load15: 0 },
})

async function fetchResources() {
  try {
    const resp = await fetch('/api/system/resources')
    if (!resp.ok) return
    const data: SystemResources = await resp.json()
    resources.value = data
  } catch (e) {
    appLog.w('SystemResources', 'fetch failed', e)
  }
}

function getCurrentInterval() {
  // If any foreground consumer is active, use fast interval; otherwise background
  return activeCount > 0 ? POLL_INTERVAL : BACKGROUND_POLL_INTERVAL
}

function startPolling() {
  activeCount++
  // If this is the first consumer overall, start the timer
  if (activeCount + backgroundCount > 1) {
    // Already running — but interval may need to change (background → foreground)
    if (timer) {
      const desiredInterval = getCurrentInterval()
      if (desiredInterval !== currentInterval) {
        clearInterval(timer)
        timer = setInterval(fetchResources, desiredInterval)
        currentInterval = desiredInterval
      }
    }
    return
  }

  // Register visibility handler on first consumer
  document.addEventListener('visibilitychange', onVisibilityChange)

  // Initial fetch — two calls: first initializes CPU/network sampler,
  // second returns actual calculated rates
  fetchResources().then(() => {
    // Short delay before second fetch to allow CPU/network interval sampling
    initTimeout = setTimeout(() => fetchResources(), 200)
  })

  currentInterval = POLL_INTERVAL
  timer = setInterval(fetchResources, POLL_INTERVAL)
}

function stopPolling() {
  activeCount = Math.max(0, activeCount - 1)
  // If still has consumers, maybe adjust interval
  if (activeCount + backgroundCount > 0) {
    if (timer) {
      const desiredInterval = getCurrentInterval()
      if (desiredInterval !== currentInterval) {
        clearInterval(timer)
        timer = setInterval(fetchResources, desiredInterval)
        currentInterval = desiredInterval
      }
    }
    return
  }

  // No consumers left — stop completely
  if (timer) {
    clearInterval(timer)
    timer = null
    currentInterval = 0
  }
  if (initTimeout) {
    clearTimeout(initTimeout)
    initTimeout = null
  }
  document.removeEventListener('visibilitychange', onVisibilityChange)
}

function startBackgroundPolling() {
  backgroundCount++
  // If this is the first consumer overall, start the timer
  if (activeCount + backgroundCount > 1) {
    // Already running — background polling uses same or slower interval
    return
  }

  document.addEventListener('visibilitychange', onVisibilityChange)

  fetchResources().then(() => {
    initTimeout = setTimeout(() => fetchResources(), 200)
  })

  currentInterval = BACKGROUND_POLL_INTERVAL
  timer = setInterval(fetchResources, BACKGROUND_POLL_INTERVAL)
}

function stopBackgroundPolling() {
  backgroundCount = Math.max(0, backgroundCount - 1)
  if (activeCount + backgroundCount > 0) {
    // Adjust interval if needed (e.g. foreground stopped, only background remains)
    if (timer) {
      const desiredInterval = getCurrentInterval()
      if (desiredInterval !== currentInterval) {
        clearInterval(timer)
        timer = setInterval(fetchResources, desiredInterval)
        currentInterval = desiredInterval
      }
    }
    return
  }

  if (timer) {
    clearInterval(timer)
    timer = null
    currentInterval = 0
  }
  if (initTimeout) {
    clearTimeout(initTimeout)
    initTimeout = null
  }
  document.removeEventListener('visibilitychange', onVisibilityChange)
}

// Pause polling when tab is hidden, resume when visible
function onVisibilityChange() {
  if (document.hidden) {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
    if (initTimeout) {
      clearTimeout(initTimeout)
      initTimeout = null
    }
  } else if (activeCount + backgroundCount > 0 && !timer) {
    fetchResources()
    timer = setInterval(fetchResources, getCurrentInterval())
  }
}

export function useSystemResources() {
  let isForeground = false
  let isBackground = false

  onUnmounted(() => {
    if (isForeground) stopPolling()
    if (isBackground) stopBackgroundPolling()
  })

  const wrappedStartPolling = () => {
    isForeground = true
    startPolling()
  }
  const wrappedStopPolling = () => {
    isForeground = false
    stopPolling()
  }
  const wrappedStartBackgroundPolling = () => {
    isBackground = true
    startBackgroundPolling()
  }
  const wrappedStopBackgroundPolling = () => {
    isBackground = false
    stopBackgroundPolling()
  }

  return {
    resources,
    startPolling: wrappedStartPolling,
    stopPolling: wrappedStopPolling,
    startBackgroundPolling: wrappedStartBackgroundPolling,
    stopBackgroundPolling: wrappedStopBackgroundPolling,
  }
}
