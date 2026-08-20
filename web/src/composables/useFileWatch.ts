import { watch, onUnmounted, type Ref } from 'vue'
import { store } from '@/stores/app.ts'
import { refreshCurrentFile, wasRecentlySaved } from '@/composables/useFileRefresh.ts'
import { useReconnect } from './useReconnect'

interface UseFileWatchOptions {
  fileManagerOpen: Ref<boolean>
  currentDir: Ref<string>
  currentFile: Ref<{ path: string; isImage?: boolean; isAudio?: boolean; isVideo?: boolean } | null>
}

/**
 * useFileWatch connects to the backend file watch SSE endpoint,
 * listens for dir_change and file_change events, and auto-refreshes
 * the directory listing or file content accordingly.
 *
 * Only active when FileManager is open or a file is being viewed.
 */
export function useFileWatch(options: UseFileWatchOptions) {
  const { fileManagerOpen, currentDir, currentFile } = options

  let eventSource: EventSource | null = null
  let clientId: string | null = null
  let updating = false // guard against concurrent updateWatch calls

  const reconnect = useReconnect({
    baseDelay: 2000,
    maxDelay: 15000,
    onReconnect: connect,
  })

  function connect() {
    if (eventSource) return

    const params = new URLSearchParams()
    // Always pass dir (empty string = project root); backend resolves it
    params.set('dir', currentDir.value || '')
    if (currentFile.value?.path) params.set('file', currentFile.value.path)

    const url = `/api/file/watch?${params.toString()}`
    eventSource = new EventSource(url)

    eventSource.addEventListener('connected', (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data)
        clientId = data.clientId
        reconnect.reset()
        updateWatch()
      } catch { /* ignore parse error */ }
    })

    eventSource.addEventListener('dir_change', () => {
      store.loadFiles(currentDir.value || '', false, 0, true)
      store.loadGitBranch()
    })

    eventSource.addEventListener('file_change', () => {
      const path = currentFile.value?.path
      if (!path) return
      // Skip refreshes caused by our own save — saveFile already synced the
      // content in memory via markSaved, so re-fetching only causes a flash.
      if (wasRecentlySaved(path)) return
      refreshCurrentFile({ clearOnError: true, loadDir: true })
    })

    eventSource.onerror = () => {
      disconnect()
      if (reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
      }
      // Reconnects indefinitely with exponential backoff (capped at 15s)
    }
  }

  function disconnect() {
    reconnect.reset()
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
    clientId = null
  }

  async function updateWatch() {
    if (!clientId || updating) return
    updating = true
    try {
      await fetch('/api/file/watch/update', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          clientId,
          dir: currentDir.value || '',
          file: currentFile.value?.path || '',
        }),
      })
    } catch {
      // Update failed — will retry on next navigation
    } finally {
      updating = false
    }
  }

  function shouldWatch(): boolean {
    return fileManagerOpen.value || currentFile.value !== null
  }

  // Connect/disconnect based on activity
  watch(() => shouldWatch(), (active) => {
    if (active) {
      connect()
    } else {
      disconnect()
    }
  }, { immediate: true })

  // Update watched paths on navigation
  watch([currentDir, () => currentFile.value?.path], () => {
    if (eventSource && clientId) {
      updateWatch()
    }
  })

  onUnmounted(() => {
    disconnect()
  })

  return { connect, disconnect }
}
