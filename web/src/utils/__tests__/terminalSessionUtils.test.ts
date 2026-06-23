import { describe, expect, it, vi } from 'vitest'
import {
  NO_RECONNECT_CODES,
  WS_CLOSE_REPLACED,
  processTerminalMessage,
  processWSClose,
  buildWsUrl,
  type TerminalSessionState,
} from '@/utils/terminalSessionUtils'

function makeState(overrides?: Partial<TerminalSessionState>): TerminalSessionState {
  return {
    sessionId: '',
    currentCwd: '',
    errorMessage: '',
    errorCode: '',
    connectionState: 'connected',
    fatalError: false,
    ...overrides,
  }
}

describe('processTerminalMessage', () => {
  describe('output', () => {
    it('calls onOutput callback with data', () => {
      const onOutput = vi.fn()
      const result = processTerminalMessage(
        { type: 'output', data: 'hello world' },
        makeState(),
        { onOutput }
      )
      expect(onOutput).toHaveBeenCalledWith('hello world')
      expect(result).toEqual({})
    })

    it('calls onOutput with empty string when data is missing', () => {
      const onOutput = vi.fn()
      processTerminalMessage({ type: 'output' }, makeState(), { onOutput })
      expect(onOutput).toHaveBeenCalledWith('')
    })

    it('does not call onOutput when callback is null', () => {
      const result = processTerminalMessage(
        { type: 'output', data: 'test' },
        makeState(),
        { onOutput: null }
      )
      expect(result).toEqual({})
    })
  })

  describe('replay', () => {
    it('calls onReplay callback with data', () => {
      const onReplay = vi.fn()
      processTerminalMessage(
        { type: 'replay', data: 'replay content' },
        makeState(),
        { onReplay }
      )
      expect(onReplay).toHaveBeenCalledWith('replay content')
    })

    it('calls onReplay with empty string when data is missing', () => {
      const onReplay = vi.fn()
      processTerminalMessage({ type: 'replay' }, makeState(), { onReplay })
      expect(onReplay).toHaveBeenCalledWith('')
    })
  })

  describe('status', () => {
    it('stores sessionId and calls onStatus', () => {
      const onStatus = vi.fn()
      const result = processTerminalMessage(
        { type: 'status', sessionId: 'abc123', cwd: '/home/user/project', running: true },
        makeState(),
        { onStatus }
      )
      expect(result).toEqual({
        sessionId: 'abc123',
        currentCwd: '/home/user/project',
      })
      expect(onStatus).toHaveBeenCalledWith({ running: true, cwd: '/home/user/project' })
    })

    it('does not update sessionId when not provided', () => {
      const result = processTerminalMessage(
        { type: 'status', cwd: '/home', running: false },
        makeState(),
      )
      expect(result.sessionId).toBeUndefined()
      expect(result.currentCwd).toBe('/home')
    })

    it('defaults running to true when not provided', () => {
      const onStatus = vi.fn()
      processTerminalMessage(
        { type: 'status', cwd: '/home' },
        makeState(),
        { onStatus }
      )
      expect(onStatus).toHaveBeenCalledWith({ running: true, cwd: '/home' })
    })

    it('defaults cwd to empty string when not provided', () => {
      const onStatus = vi.fn()
      const result = processTerminalMessage(
        { type: 'status', running: true },
        makeState(),
        { onStatus }
      )
      expect(result.currentCwd).toBe('')
      expect(onStatus).toHaveBeenCalledWith({ running: true, cwd: '' })
    })
  })

  describe('exit', () => {
    it('clears sessionId, sets disconnected, calls onExit with code', () => {
      const onExit = vi.fn()
      const result = processTerminalMessage(
        { type: 'exit', code: 0 },
        makeState({ sessionId: 'abc' }),
        { onExit }
      )
      expect(result).toEqual({
        sessionId: '',
        connectionState: 'disconnected',
      })
      expect(onExit).toHaveBeenCalledWith(0)
    })

    it('defaults exit code to 0 when not provided', () => {
      const onExit = vi.fn()
      processTerminalMessage({ type: 'exit' }, makeState(), { onExit })
      expect(onExit).toHaveBeenCalledWith(0)
    })

    it('passes non-zero exit codes', () => {
      const onExit = vi.fn()
      processTerminalMessage({ type: 'exit', code: 137 }, makeState(), { onExit })
      expect(onExit).toHaveBeenCalledWith(137)
    })
  })

  describe('error', () => {
    it('sets error state and calls onError', () => {
      const onError = vi.fn()
      const result = processTerminalMessage(
        { type: 'error', message: 'Shell failed', errcode: 'shell_start_failed' },
        makeState(),
        { onError }
      )
      expect(result).toEqual({
        errorMessage: 'Shell failed',
        errorCode: 'shell_start_failed',
        connectionState: 'error',
        fatalError: true,
      })
      expect(onError).toHaveBeenCalledWith('Shell failed', 'shell_start_failed')
    })

    it('marks terminal_disabled as fatal', () => {
      const result = processTerminalMessage(
        { type: 'error', message: 'Disabled', errcode: 'terminal_disabled' },
        makeState()
      )
      expect(result.fatalError).toBe(true)
    })

    it('marks session_limit as fatal', () => {
      const result = processTerminalMessage(
        { type: 'error', message: 'Too many', errcode: 'session_limit' },
        makeState()
      )
      expect(result.fatalError).toBe(true)
    })

    it('does not mark unknown error codes as fatal', () => {
      const result = processTerminalMessage(
        { type: 'error', message: 'Something', errcode: 'unknown_error' },
        makeState()
      )
      expect(result.fatalError).toBe(false)
      expect(result.connectionState).toBe('error')
    })

    it('does not mark error as fatal when errcode is missing', () => {
      const result = processTerminalMessage(
        { type: 'error', message: 'Something went wrong' },
        makeState()
      )
      expect(result.fatalError).toBe(false)
    })

    it('defaults message and errcode to empty strings', () => {
      const onError = vi.fn()
      const result = processTerminalMessage(
        { type: 'error' },
        makeState(),
        { onError }
      )
      expect(result.errorMessage).toBe('')
      expect(result.errorCode).toBe('')
      expect(onError).toHaveBeenCalledWith('', '')
    })
  })

  describe('unknown message type', () => {
    it('returns empty state updates', () => {
      const result = processTerminalMessage(
        { type: 'unknown_type' },
        makeState()
      )
      expect(result).toEqual({})
    })
  })
})

describe('processWSClose', () => {
  describe('already in error state', () => {
    it('does not override error state', () => {
      const result = processWSClose(1000, 'error', true)
      expect(result).toEqual({
        state: {},
        shouldReconnect: false,
        shouldDisable: false,
      })
    })
  })

  describe('WS_CLOSE_REPLACED (4001)', () => {
    it('disconnects without reconnecting and disables', () => {
      const result = processWSClose(WS_CLOSE_REPLACED, 'connected', true)
      expect(result).toEqual({
        state: { connectionState: 'disconnected' },
        shouldReconnect: false,
        shouldDisable: true,
      })
    })
  })

  describe('unexpected disconnect after connected', () => {
    it('sets disconnected and requests reconnect', () => {
      const result = processWSClose(1000, 'connected', true)
      expect(result).toEqual({
        state: { connectionState: 'disconnected' },
        shouldReconnect: true,
        shouldDisable: false,
      })
    })
  })

  describe('during connecting phase', () => {
    it('sets disconnected when cannot reconnect', () => {
      const result = processWSClose(1000, 'connecting', false)
      expect(result).toEqual({
        state: { connectionState: 'disconnected' },
        shouldReconnect: false,
        shouldDisable: false,
      })
    })

    it('sets error state for code 1006 (abnormal closure)', () => {
      const result = processWSClose(1006, 'connecting', true)
      expect(result.state).toEqual({
        connectionState: 'error',
        errorCode: 'shell_start_failed',
        fatalError: true,
      })
      expect(result.shouldReconnect).toBe(false)
    })

    it('sets disconnected for normal close during connecting', () => {
      const result = processWSClose(1000, 'connecting', true)
      expect(result).toEqual({
        state: { connectionState: 'disconnected' },
        shouldReconnect: false,
        shouldDisable: false,
      })
    })
  })

  describe('during reconnecting phase', () => {
    it('sets error for code 1006', () => {
      const result = processWSClose(1006, 'reconnecting', true)
      expect(result.state.connectionState).toBe('error')
      expect(result.state.fatalError).toBe(true)
    })

    it('sets disconnected when cannot reconnect', () => {
      const result = processWSClose(1000, 'reconnecting', false)
      expect(result.state.connectionState).toBe('disconnected')
    })
  })

  describe('code 1006 during connected state', () => {
    it('still triggers reconnect (treated as unexpected disconnect)', () => {
      const result = processWSClose(1006, 'connected', true)
      expect(result.shouldReconnect).toBe(true)
      expect(result.state.connectionState).toBe('disconnected')
    })
  })
})

describe('buildWsUrl', () => {
  it('returns base URL when no session ID', () => {
    expect(buildWsUrl('ws://localhost:8080/api/terminal/ws', '')).toBe('ws://localhost:8080/api/terminal/ws')
  })

  it('appends session parameter with ?', () => {
    expect(buildWsUrl('ws://localhost:8080/api/terminal/ws', 'abc123'))
      .toBe('ws://localhost:8080/api/terminal/ws?session=abc123')
  })

  it('appends session parameter with & when URL already has query params', () => {
    expect(buildWsUrl('ws://localhost:8080/api/terminal/ws?cwd=/home', 'abc123'))
      .toBe('ws://localhost:8080/api/terminal/ws?cwd=/home&session=abc123')
  })

  it('encodes special characters in session ID', () => {
    expect(buildWsUrl('ws://localhost/ws', 'session/with+special'))
      .toBe('ws://localhost/ws?session=session%2Fwith%2Bspecial')
  })
})

describe('NO_RECONNECT_CODES', () => {
  it('contains terminal_disabled', () => {
    expect(NO_RECONNECT_CODES.has('terminal_disabled')).toBe(true)
  })

  it('contains shell_start_failed', () => {
    expect(NO_RECONNECT_CODES.has('shell_start_failed')).toBe(true)
  })

  it('contains session_limit', () => {
    expect(NO_RECONNECT_CODES.has('session_limit')).toBe(true)
  })

  it('contains platform_unsupported', () => {
    expect(NO_RECONNECT_CODES.has('platform_unsupported')).toBe(true)
  })

  it('does not contain unknown codes', () => {
    expect(NO_RECONNECT_CODES.has('some_other_error')).toBe(false)
  })
})

describe('WS_CLOSE_REPLACED', () => {
  it('is 4001', () => {
    expect(WS_CLOSE_REPLACED).toBe(4001)
  })
})
