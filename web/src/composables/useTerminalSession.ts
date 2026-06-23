import { ref, computed, type Ref } from 'vue'
import { useReconnect } from './useReconnect'
import {
  processTerminalMessage,
  processWSClose,
  buildWsUrl,
  type TerminalCallbacks,
  type TerminalMessage,
} from '@/utils/terminalSessionUtils'
import { appLog } from '@/utils/appLog'

const TAG = 'TerminalSession'

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'error'

export interface TerminalStatus {
  running: boolean
  cwd: string
}

/** Error messages used by useTerminalSession — caller provides localized strings. */
export interface TerminalErrorMessages {
  shellStartFailed: string
  websocketFailed: string
  platformUnsupported: string
}

export function useTerminalSession(
  getWsUrl: () => string,
  errorMessages?: TerminalErrorMessages,
) {
  const connectionState: Ref<ConnectionState> = ref('disconnected')
  const errorMessage = ref('')
  const errorCode = ref('')
  const currentCwd = ref('')
  const sessionId = ref('')
  const ws: Ref<WebSocket | null> = ref(null)
  // Track whether the current error is fatal (should not auto-reconnect)
  let fatalError = false

  const reconnect = useReconnect({
    onReconnect: () => connect().catch(() => { /* onclose handles retry logic */ }),
    getFatalError: () => fatalError ? true : null,
  })

  function connect(): Promise<void> {
    return new Promise((resolve, reject) => {
      if (ws.value && ws.value.readyState === WebSocket.OPEN) {
        resolve()
        return
      }

      connectionState.value = 'connecting'
      errorMessage.value = ''
      errorCode.value = ''
      fatalError = false

      // Build URL with session ID for reconnect (to reattach to existing PTY)
      const url = buildWsUrl(getWsUrl(), sessionId.value)
      const socket = new WebSocket(url)

      socket.onopen = () => {
        connectionState.value = 'connected'
        reconnect.reset()
        ws.value = socket
        resolve()
      }

      socket.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data)
          handleMessage(msg)
        } catch {
          appLog.w(TAG, 'invalid message', event.data)
        }
      }

      socket.onclose = (event) => {
        ws.value = null

        const result = processWSClose(event.code, connectionState.value, reconnect.shouldReconnect())

        if (result.shouldDisable) {
          reconnect.disable()
        }

        // Apply state updates
        if (result.state.connectionState) {
          connectionState.value = result.state.connectionState
        }
        if (result.state.errorCode) {
          errorCode.value = result.state.errorCode
          if (result.state.errorCode === 'platform_unsupported') {
            errorMessage.value = errorMessages?.platformUnsupported || 'Platform not supported'
          } else {
            errorMessage.value = errorMessages?.shellStartFailed || 'Shell start failed'
          }
        }
        if (result.state.fatalError) {
          fatalError = true
        }

        if (result.shouldReconnect) {
          tryReconnect()
        }
      }

      socket.onerror = () => {
        ws.value = null
        // Don't set error state here — onclose will fire next and handle it
        // Just remember this was a connection failure
        if (connectionState.value !== 'error') {
          errorMessage.value = errorMessages?.websocketFailed || 'WebSocket connection failed'
          connectionState.value = 'error'
        }
        reject(new Error(errorMessage.value))
      }
    })
  }

  function disconnect() {
    reconnect.reset()
    fatalError = false
    // sessionId intentionally NOT cleared — allows reattach to existing PTY on next connect()
    if (ws.value) {
      ws.value.close()
      ws.value = null
    }
    connectionState.value = 'disconnected'
  }

  // reset clears all state for a fresh connect (used by rebuild session)
  function reset() {
    reconnect.reset()
    fatalError = false
    errorMessage.value = ''
    errorCode.value = ''
    sessionId.value = '' // rebuild = new session
    if (ws.value) {
      ws.value.close()
      ws.value = null
    }
    connectionState.value = 'disconnected'
  }

  function tryReconnect() {
    if (!reconnect.shouldReconnect()) {
      connectionState.value = 'error'
      errorMessage.value = errorMessage.value || errorMessages?.websocketFailed || 'WebSocket connection failed'
      return
    }
    connectionState.value = 'reconnecting'
    reconnect.scheduleReconnect()
  }

  // Message handler callbacks — set by TerminalPanel
  let onOutput: TerminalCallbacks['onOutput'] = null
  let onReplay: TerminalCallbacks['onReplay'] = null
  let onStatus: TerminalCallbacks['onStatus'] = null
  let onExit: TerminalCallbacks['onExit'] = null
  let onError: TerminalCallbacks['onError'] = null

  function setCallbacks(callbacks: TerminalCallbacks) {
    onOutput = callbacks.onOutput ?? null
    onReplay = callbacks.onReplay ?? null
    onStatus = callbacks.onStatus ?? null
    onExit = callbacks.onExit ?? null
    onError = callbacks.onError ?? null
  }

  function handleMessage(msg: TerminalMessage) {
    const updates = processTerminalMessage(msg, {
      sessionId: sessionId.value,
      currentCwd: currentCwd.value,
      errorMessage: errorMessage.value,
      errorCode: errorCode.value,
      connectionState: connectionState.value,
      fatalError,
    }, { onOutput, onReplay, onStatus, onExit, onError })

    if (updates.sessionId !== undefined) sessionId.value = updates.sessionId
    if (updates.currentCwd !== undefined) currentCwd.value = updates.currentCwd
    if (updates.errorMessage !== undefined) errorMessage.value = updates.errorMessage
    if (updates.errorCode !== undefined) errorCode.value = updates.errorCode
    if (updates.connectionState !== undefined) connectionState.value = updates.connectionState
    if (updates.fatalError !== undefined) fatalError = updates.fatalError
  }

  function sendInput(data: string) {
    if (ws.value?.readyState === WebSocket.OPEN) {
      ws.value.send(JSON.stringify({ type: 'input', data }))
    }
  }

  function sendResize(cols: number, rows: number) {
    if (ws.value?.readyState === WebSocket.OPEN) {
      ws.value.send(JSON.stringify({ type: 'resize', cols, rows }))
    }
  }

  function sendClose() {
    if (ws.value?.readyState === WebSocket.OPEN) {
      ws.value.send(JSON.stringify({ type: 'close' }))
    }
    disconnect()
    // Explicit close kills the PTY — clear sessionId so next connect creates a new session
    sessionId.value = ''
  }

  /** Whether the WebSocket is currently open. */
  const wsOpen = computed(() => ws.value?.readyState === WebSocket.OPEN)

  return {
    connectionState,
    errorMessage,
    errorCode,
    currentCwd,
    sessionId,
    wsOpen,
    connect,
    disconnect,
    reset,
    setCallbacks,
    sendInput,
    sendResize,
    sendClose,
  }
}
