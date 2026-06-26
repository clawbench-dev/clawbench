/**
 * Pure functions and constants extracted from useTerminalSession for testability.
 * These handle the message routing and close-code logic that would otherwise
 * require a live WebSocket to exercise.
 */

// Error codes that should NOT trigger automatic reconnection
export const NO_RECONNECT_CODES = new Set([
  'terminal_disabled',
  'shell_start_failed',
  'session_limit',
  'platform_unsupported',
])

// Custom WebSocket close code: server kicked this client because a new one connected
export const WS_CLOSE_REPLACED = 4001

export interface TerminalMessage {
  type: string
  sessionId?: string
  data?: string
  cwd?: string
  running?: boolean
  code?: number
  message?: string
  errcode?: string
}

export interface TerminalSessionState {
  sessionId: string
  currentCwd: string
  errorMessage: string
  errorCode: string
  connectionState: 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'error'
  fatalError: boolean
}

export interface TerminalCallbacks {
  onOutput?: ((data: string) => void) | null
  onReplay?: ((data: string) => void) | null
  onStatus?: ((status: { running: boolean; cwd: string }) => void) | null
  onExit?: ((code: number) => void) | null
  onError?: ((message: string, code: string) => void) | null
}

/**
 * Pure message handler for terminal WebSocket messages.
 * Returns the new state (partial) instead of mutating refs directly.
 */
export function processTerminalMessage(
  msg: TerminalMessage,
  _state: TerminalSessionState,
  callbacks: TerminalCallbacks = {}
): Partial<TerminalSessionState> {
  switch (msg.type) {
    case 'output':
      callbacks.onOutput?.(msg.data ?? '')
      return {}
    case 'replay':
      callbacks.onReplay?.(msg.data ?? '')
      return {}
    case 'status': {
      const updates: Partial<TerminalSessionState> = {
        currentCwd: msg.cwd ?? '',
      }
      if (msg.sessionId) {
        updates.sessionId = msg.sessionId
      }
      callbacks.onStatus?.({ running: msg.running ?? true, cwd: msg.cwd ?? '' })
      return updates
    }
    case 'exit':
      callbacks.onExit?.(msg.code ?? 0)
      return {
        sessionId: '',
        connectionState: 'disconnected',
      }
    case 'error': {
      const isFatal = !!(msg.errcode && NO_RECONNECT_CODES.has(msg.errcode))
      callbacks.onError?.(msg.message ?? '', msg.errcode ?? '')
      return {
        errorMessage: msg.message ?? '',
        errorCode: msg.errcode ?? '',
        connectionState: 'error',
        fatalError: isFatal,
      }
    }
    default:
      return {}
  }
}

/**
 * Pure function to determine the result of a WebSocket close event.
 * Returns state updates and whether reconnection should be attempted.
 */
export function processWSClose(
  code: number,
  currentState: TerminalSessionState['connectionState'],
  canReconnect: boolean
): { state: Partial<TerminalSessionState>; shouldReconnect: boolean; shouldDisable: boolean } {
  // If already in error state, don't override
  if (currentState === 'error') {
    return { state: {}, shouldReconnect: false, shouldDisable: false }
  }

  // Replaced by a new client — don't reconnect
  if (code === WS_CLOSE_REPLACED) {
    return {
      state: { connectionState: 'disconnected' },
      shouldReconnect: false,
      shouldDisable: true,
    }
  }

  // Unexpected disconnect after successful connection
  if (currentState === 'connected') {
    return {
      state: { connectionState: 'disconnected' },
      shouldReconnect: true,
      shouldDisable: false,
    }
  }

  // During connecting/reconnecting phase
  if (!canReconnect) {
    return {
      state: { connectionState: 'disconnected' },
      shouldReconnect: false,
      shouldDisable: false,
    }
  }

  // Code 1006 = abnormal closure (server rejected upgrade)
  if (code === 1006) {
    return {
      state: { connectionState: 'error', errorCode: 'shell_start_failed', fatalError: true },
      shouldReconnect: false,
      shouldDisable: false,
    }
  }

  return {
    state: { connectionState: 'disconnected' },
    shouldReconnect: false,
    shouldDisable: false,
  }
}

/**
 * Build the WebSocket URL with optional session ID for reconnection.
 */
export function buildWsUrl(baseUrl: string, sessionId: string): string {
  if (!sessionId) return baseUrl
  const sep = baseUrl.includes('?') ? '&' : '?'
  return `${baseUrl}${sep}session=${encodeURIComponent(sessionId)}`
}

/**
 * Strip DEC mode 2026 (Synchronized Output) escape sequences from terminal data.
 * These sequences (\x1b[?2026h and \x1b[?2026l) cause xterm.js to buffer
 * rendering updates, only flushing on mode-off or a 1-second safety timeout.
 * In a remote terminal over WebSocket, the combination of:
 * - TUI apps (vim, OpenCode/Bubble Tea) sending \x1b[?2026h before each frame
 * - xterm.js WriteBuffer's 12ms time-slicing
 * - The 1-second safety timeout re-enabling sync mode before the renderer fires
 * causes the alternate screen buffer to appear frozen — key input reaches the
 * PTY but vim's response is buffered by xterm.js and never rendered.
 * Stripping is safe: local xterm.js rendering has no perceptible benefit from
 * batched updates when streaming over a WebSocket — the network is already
 * the bottleneck.
 */
export function stripSyncOutput(data: string): string {
  if (!data.includes('\x1b[?2026')) return data
  // eslint-disable-next-line no-control-regex
  return data.replace(/\x1b\[\?2026[hl]/g, '')
}
