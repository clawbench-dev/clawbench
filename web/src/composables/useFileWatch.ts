import { watch, onUnmounted, type Ref } from 'vue'
import { store } from '@/stores/app.ts'
import { refreshCurrentFile, wasRecentlySaved } from '@/composables/useFileRefresh.ts'
import { appLog } from '@/utils/appLog'
import { useReconnect } from './useReconnect'
import {
  bumpMediaVersion,
  ensureMediaObserver,
  mediaPaths,
  setMediaProjectRoot,
} from './useMediaWatch.ts'
import { sameFilePath } from '@/utils/path.ts'

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
 * Beyond the open file, it also reports every locally-served image currently
 * rendered (useMediaWatch) so a background rewrite of a diagram or screenshot
 * refreshes the preview in place — the markdown source does not change in that
 * case, so nothing else would ever trigger a re-render.
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
  /** Last media-path list sent, so navigation-only updates don't re-send it. */
  let lastSentMedia: string[] = []

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
    const paths = mediaPaths.value
    lastSentMedia = paths
    send({
      type: 'watch',
      dir: currentDir.value || '',
      file: currentFile.value?.path || '',
      files: paths,
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
        const changedPath = msg.path || ''
        // A media file changing must repaint the preview even when it is NOT
        // the file open in the viewer (an image referenced by a markdown file).
        // This patches the live <img> elements directly, so v-html surfaces that
        // never re-render still pick up the new bytes.
        if (changedPath) bumpMediaVersion(changedPath)

        const path = currentFile.value?.path
        if (!path) return
        // Only refresh the open file's CONTENT when the event is about it.
        // Before media paths were watched, every file_change implied the open
        // file; now it can be a sibling image, and refreshing would flash the
        // viewer for an unrelated change.
        //
        // The server reports ABSOLUTE paths while currentFile.path is
        // project-relative, so the comparison has to normalize both sides
        // (sameFilePath relativizes under the project root). When the root is
        // not known yet the two cannot be related reliably — refresh rather
        // than risk missing the change, since a needless refresh only flashes
        // while a missed one is the bug this whole path exists to fix.
        if (changedPath && store.state.projectRoot
            && !sameFilePath(changedPath, path, store.state.projectRoot)) return
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
    // Media watching is useful whenever any image may be on screen, not only
    // while the file panel is open — a markdown preview in the chat column and
    // the file-manager grid both render local images.
    return fileManagerOpen.value || currentFile.value !== null || mediaPaths.value.length > 0
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

  // Re-target when the set of on-screen images changes. The media list is
  // derived from the live DOM, so this fires as previews open/close and as
  // streamed markdown gains or loses images.
  watch(mediaPaths, (paths) => {
    if (!ws || !connected) return
    // Avoid a redundant round-trip when only the open file/dir moved.
    if (paths.length === lastSentMedia.length && paths.every((p, i) => p === lastSentMedia[i])) return
    sendWatch()
  })

  // Discover images already on screen and track later DOM changes.
  ensureMediaObserver()

  // Media paths are stored project-relative, so the root has to be current —
  // it changes on project switch without remounting the watcher.
  watch(() => store.state.projectRoot, (root) => {
    setMediaProjectRoot(root || '')
  }, { immediate: true })

  onUnmounted(() => {
    disconnect()
  })

  return { connect, disconnect }
}
