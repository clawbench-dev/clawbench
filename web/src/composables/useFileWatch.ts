import { watch, onUnmounted, type Ref } from 'vue'
import { store } from '@/stores/app.ts'
import { refreshCurrentFile, wasRecentlySaved } from '@/composables/useFileRefresh.ts'
import { appLog } from '@/utils/appLog'
import { useReconnect } from './useReconnect'

interface UseFileWatchOptions {
  fileManagerOpen: Ref<boolean>
  currentDir: Ref<string>
  currentFile: Ref<{ path: string; isImage?: boolean; isAudio?: boolean; isVideo?: boolean } | null>
}

/**
 * No message (ping or event) for this long means the socket is silently dead —
 * the TCP connection can stay open while nothing flows, which Android WebView
 * does routinely. Mirrors EVENT_STALE_MS in useGlobalEvents.
 */
const EVENT_STALE_MS = 60000

/** Staleness check cadence. */
const STALE_CHECK_MS = 15000

function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}${path}`
}

/**
 * useFileWatch connects to the backend file watch WebSocket, listens for
 * dir_change and file_change events, and auto-refreshes the directory listing
 * or file content accordingly.
 *
 * Only active when FileManager is open or a file is being viewed.
 *
 * This uses a WebSocket rather than the EventSource it replaced: a resident
 * EventSource permanently consumes one of the browser's 6 HTTP/1.1 connections
 * per origin, which starves parallel REST requests on plain-HTTP deployments. A
 * WebSocket leaves that pool once upgraded. The trade-off is that raw
 * WebSocket does NOT auto-reconnect — `onclose` below is the only reconnect
 * trigger, so it must never be suppressed outside disconnect().
 */
export function useFileWatch(options: UseFileWatchOptions) {
  const { fileManagerOpen, currentDir, currentFile } = options

  let ws: WebSocket | null = null
  let connected = false
  let lastMessageAt = 0
  let staleTimer: ReturnType<typeof setInterval> | null = null

  const reconnect = useReconnect({
    baseDelay: 2000,
    maxDelay: 15000,
    onReconnect: connect,
  })

  function send(msg: object) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(msg))
    }
  }

  /** Re-target the server-side watch. Idempotent, so no de-dup guard is needed. */
  function sendWatch() {
    send({
      type: 'watch',
      dir: currentDir.value || '',
      file: currentFile.value?.path || '',
    })
  }

  function handleMessage(e: MessageEvent) {
    lastMessageAt = Date.now()

    let msg: { type?: string; path?: string; clientId?: string; code?: string }
    try {
      msg = JSON.parse(e.data)
    } catch {
      appLog.w('FileWatch', 'failed to parse WS message')
      return
    }

    switch (msg.type) {
      case 'connected':
        connected = true
        reconnect.reset()
        sendWatch()
        break

      case 'dir_change':
        store.loadFiles(currentDir.value || '', false, 0, true)
        store.loadGitBranch()
        break

      case 'file_change': {
        const path = currentFile.value?.path
        if (!path) return
        // Skip refreshes caused by our own save — saveFile already synced the
        // content in memory via markSaved, so re-fetching only causes a flash.
        if (wasRecentlySaved(path)) return
        refreshCurrentFile({ clearOnError: true, loadDir: true })
        break
      }

      case 'ping':
        send({ type: 'pong' })
        break

      case 'error':
        // A rejected control message (e.g. a path outside the project root).
        // Not fatal — the channel stays open.
        appLog.w('FileWatch', `server rejected control message: ${msg.code ?? 'unknown'}`)
        break

      default:
        break
    }
  }

  function startStaleCheck() {
    stopStaleCheck()
    staleTimer = setInterval(() => {
      if (!ws || ws.readyState !== WebSocket.OPEN) return
      if (Date.now() - lastMessageAt > EVENT_STALE_MS) {
        appLog.w('FileWatch', `no WS message for ${EVENT_STALE_MS}ms — connection silently dead, forcing reconnect`)
        ws.close() // fires onclose → reconnect
      }
    }, STALE_CHECK_MS)
  }

  function stopStaleCheck() {
    if (staleTimer !== null) {
      clearInterval(staleTimer)
      staleTimer = null
    }
  }

  function connect() {
    if (ws) return

    const params = new URLSearchParams()
    // Always pass dir (empty string = project root); backend resolves it
    params.set('dir', currentDir.value || '')
    if (currentFile.value?.path) params.set('file', currentFile.value.path)

    ws = new WebSocket(wsUrl(`/api/file/watch/ws?${params.toString()}`))

    ws.onmessage = handleMessage

    ws.onclose = () => {
      connected = false
      ws = null
      stopStaleCheck()
      // Do NOT reset the reconnect state here — resetting would zero the
      // attempt counter and clear the pending timer, pinning the backoff to
      // its base delay forever.
      if (reconnect.shouldReconnect()) {
        reconnect.scheduleReconnect()
      }
    }

    ws.onerror = () => {
      // onclose always follows; reconnect is handled there.
    }

    lastMessageAt = Date.now()
    startStaleCheck()
  }

  function disconnect() {
    reconnect.reset()
    stopStaleCheck()
    if (ws) {
      // Suppress the close handler so an intentional teardown does not schedule
      // a reconnect.
      ws.onclose = null
      ws.onerror = null
      ws.close()
      ws = null
    }
    connected = false
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
    if (ws && connected) {
      sendWatch()
    }
  })

  onUnmounted(() => {
    disconnect()
  })

  return { connect, disconnect }
}
