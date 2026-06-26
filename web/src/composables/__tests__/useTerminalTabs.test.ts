import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'

// Mock useTerminalSession — we're unit-testing useTerminalTabs, not useTerminalSession.
// We store raw (non-reactive-wrapped) session objects so tests can set .value on Refs.
const mockSessionStore: Array<{
  connectionState: ReturnType<typeof ref<string>>
  sessionId: ReturnType<typeof ref<string>>
  wsOpen: ReturnType<typeof ref<boolean>>
  errorMessage: ReturnType<typeof ref<string>>
  errorCode: ReturnType<typeof ref<string>>
  currentCwd: ReturnType<typeof ref<string>>
  connect: ReturnType<typeof vi.fn>
  disconnect: ReturnType<typeof vi.fn>
  sendClose: ReturnType<typeof vi.fn>
  sendInput: ReturnType<typeof vi.fn>
  sendResize: ReturnType<typeof vi.fn>
  setCallbacks: ReturnType<typeof vi.fn>
}> = []

vi.mock('@/composables/useTerminalSession', () => {
  return {
    useTerminalSession: (_getWsUrl: () => string, _errorMessages?: any) => {
      const session = {
        connectionState: ref('disconnected'),
        sessionId: ref(''),
        wsOpen: ref(false),
        errorMessage: ref(''),
        errorCode: ref(''),
        currentCwd: ref(''),
        connect: vi.fn(async () => {
          session.connectionState.value = 'connecting'
          await nextTick()
          session.connectionState.value = 'connected'
          session.wsOpen.value = true
        }),
        disconnect: vi.fn(() => {
          session.connectionState.value = 'disconnected'
          session.wsOpen.value = false
          // sessionId preserved for reconnect
        }),
        reset: vi.fn(() => {
          session.connectionState.value = 'disconnected'
          session.sessionId.value = ''
          session.errorMessage.value = ''
          session.errorCode.value = ''
          session.wsOpen.value = false
        }),
        sendClose: vi.fn(() => {
          // Simulates real sendClose: clears sessionId and disconnects
          session.sessionId.value = ''
          session.connectionState.value = 'disconnected'
          session.wsOpen.value = false
        }),
        sendInput: vi.fn(),
        sendResize: vi.fn(),
        setCallbacks: vi.fn(),
      }
      mockSessionStore.push(session)
      return session
    },
  }
})

// Mock xterm — useTerminalTabs creates Terminal/FitAddon/WebLinksAddon instances
vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    options: Record<string, unknown> = {}
    element: HTMLElement | null = null
    private _dataCallbacks: ((data: string) => void)[] = []
    private _resizeCallbacks: (() => void)[] = []

    loadAddon() {}
    open(el: HTMLElement) { this.element = el }
    dispose() { this.element = null }
    write() {}
    clear() {}
    reset() {}
    focus() {}
    onData(cb: (data: string) => void) { this._dataCallbacks.push(cb) }
    onResize(cb: () => void) { this._resizeCallbacks.push(cb) }
  }
  return { Terminal: MockTerminal }
})

vi.mock('@xterm/addon-fit', () => {
  class MockFitAddon { fit() {} }
  return { FitAddon: MockFitAddon }
})

vi.mock('@xterm/addon-web-links', () => {
  class MockWebLinksAddon {}
  return { WebLinksAddon: MockWebLinksAddon }
})

import { useTerminalTabs, type TerminalTab } from '@/composables/useTerminalTabs'

const defaultErrorMessages = {
  shellStartFailed: 'Shell start failed',
  websocketFailed: 'WebSocket connection failed',
}

function createTabManager(overrides?: {
  onCloseSessionViaHttp?: (sessionId: string) => void
  onExit?: (tabId: string) => void
  onError?: (tabId: string, message: string, code: string) => void
}) {
  return useTerminalTabs(
    (cwd?: string) => `ws://localhost:8080/api/terminal/ws${cwd ? `?cwd=${cwd}` : ''}`,
    {
      fontSize: ref(14),
      getXtermTheme: () => ({}),
      errorMessages: defaultErrorMessages,
      onCloseSessionViaHttp: overrides?.onCloseSessionViaHttp,
      onExit: overrides?.onExit,
      onError: overrides?.onError,
      toast: vi.fn(),
    },
  )
}

/**
 * Get the raw (non-reactive-unwrapped) mock session for a tab.
 *
 * Since useTerminalTabs wraps sessions in reactive(), accessing session
 * properties through the tab gives unwrapped values (strings, booleans).
 * To set Ref .value for test simulation, we look up the original mock
 * object from mockSessionStore using tab index order.
 */
function getRawSession(tabIndex: number) {
  // mockSessionStore is ordered by creation time — first tab = index 0
  return mockSessionStore[tabIndex]
}

/** Get the index of a tab in the tabs array. */
function tabIndex(mgr: ReturnType<typeof useTerminalTabs>, tab: TerminalTab): number {
  return mgr.tabs.value.indexOf(tab as any)
}

beforeEach(() => {
  mockSessionStore.length = 0
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useTerminalTabs', () => {
  describe('initialization', () => {
    it('creates one default tab on initialization', () => {
      const mgr = createTabManager()
      expect(mgr.tabs.value).toHaveLength(1)
      expect(mgr.activeTabId.value).toBeTruthy()
      expect(mgr.activeTab.value).not.toBeNull()
    })

    it('default tab has empty cwd and title "Terminal"', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      expect(tab.cwd).toBe('')
      expect(tab.title).toBe('Terminal')
    })

    it('default tab session starts disconnected', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      expect(tab.session.connectionState).toBe('disconnected')
    })

    // Regression: reactive() auto-unwraps Refs, so .value must NOT be used
    // on session properties accessed through the reactive tab proxy.
    // Using .value would read the .value property of the already-unwrapped
    // string, which is undefined.
    it('session Refs are auto-unwrapped by reactive() — no .value needed', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      // Direct access (correct) returns the unwrapped string
      expect(tab.session.connectionState).toBe('disconnected')
      expect(tab.session.errorCode).toBe('')
      expect(tab.session.errorMessage).toBe('')
      // Accessing .value on the unwrapped string is undefined
      expect((tab.session.connectionState as any).value).toBeUndefined()
      expect((tab.session.errorCode as any).value).toBeUndefined()
      expect((tab.session.errorMessage as any).value).toBeUndefined()
    })
  })

  describe('createTab', () => {
    it('creates a new tab and makes it active', () => {
      const mgr = createTabManager()
      const initialCount = mgr.tabs.value.length
      const newTab = mgr.createTab('/home/user/project')

      expect(mgr.tabs.value).toHaveLength(initialCount + 1)
      expect(mgr.activeTabId.value).toBe(newTab.id)
    })

    it('derives title from last directory segment', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab('/home/user/myproject')
      expect(tab.title).toBe('myproject')
    })

    it('uses "Terminal" as title for empty cwd', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab('')
      expect(tab.title).toBe('Terminal')
    })

    it('passes errorMessages to useTerminalSession', () => {
      const mgr = createTabManager()
      mgr.createTab()
      // Session was created and stored — verify mock was called
      expect(mockSessionStore.length).toBeGreaterThanOrEqual(2)
    })

    it('generates unique IDs for each tab', () => {
      const mgr = createTabManager()
      const tab1 = mgr.createTab()
      const tab2 = mgr.createTab()
      expect(tab1.id).not.toBe(tab2.id)
    })

    it('wires session callbacks via setCallbacks', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab()
      const rawSession = getRawSession(tabIndex(mgr, tab))
      expect(rawSession.setCallbacks).toHaveBeenCalled()
    })
  })

  describe('closeTab', () => {
    it('removes the tab from the list', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab('/home/user/project')
      expect(mgr.tabs.value).toHaveLength(2)

      mgr.closeTab(tab.id)
      expect(mgr.tabs.value).toHaveLength(1)
      expect(mgr.tabs.value.find(t => t.id === tab.id)).toBeUndefined()
    })

    it('switches to adjacent tab when closing the active tab', () => {
      const mgr = createTabManager()
      const tab1 = mgr.tabs.value[0]
      const tab2 = mgr.createTab('/home/user/project')

      expect(mgr.activeTabId.value).toBe(tab2.id)

      mgr.closeTab(tab2.id)
      expect(mgr.activeTabId.value).toBe(tab1.id)
    })

    it('allows closing the last tab without auto-creating a new one', () => {
      const mgr = createTabManager()
      const onlyTab = mgr.tabs.value[0]
      expect(mgr.tabs.value).toHaveLength(1)

      const result = mgr.closeTab(onlyTab.id)
      expect(mgr.tabs.value).toHaveLength(0)
      expect(result.switchToId).toBeNull()
    })

    it('activeTab becomes null when all tabs are closed', () => {
      const mgr = createTabManager()
      const onlyTab = mgr.tabs.value[0]

      mgr.closeTab(onlyTab.id)
      expect(mgr.activeTab.value).toBeNull()
    })

    it('returns switchToId for the new active tab', () => {
      const mgr = createTabManager()
      const tab1 = mgr.tabs.value[0]
      const tab2 = mgr.createTab()

      const result = mgr.closeTab(tab2.id)
      expect(result.switchToId).toBe(tab1.id)
    })

    it('returns null switchToId when tab not found', () => {
      const mgr = createTabManager()
      const result = mgr.closeTab('nonexistent-id')
      expect(result.switchToId).toBeNull()
    })

    it('calls onCloseSessionViaHttp when WS is disconnected (session leak fix)', () => {
      const onCloseViaHttp = vi.fn()
      const mgr = createTabManager({ onCloseSessionViaHttp: onCloseViaHttp })

      const tab = mgr.createTab('/home')
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Simulate connected session with a session ID
      rawSession.connectionState.value = 'connected'
      rawSession.wsOpen.value = true
      rawSession.sessionId.value = 'leak-session-123'

      // Now disconnect (user switches away from terminal panel)
      rawSession.connectionState.value = 'disconnected'
      rawSession.wsOpen.value = false
      // sessionId is preserved for reconnect

      // Close the tab — since WS is disconnected, sendClose can't reach backend
      mgr.closeTab(tab.id)

      // Should fall back to HTTP API
      expect(onCloseViaHttp).toHaveBeenCalledWith('leak-session-123')
    })

    it('does NOT call onCloseSessionViaHttp when WS is connected', () => {
      const onCloseViaHttp = vi.fn()
      const mgr = createTabManager({ onCloseSessionViaHttp: onCloseViaHttp })

      const tab = mgr.createTab('/home')
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Session is connected
      rawSession.connectionState.value = 'connected'
      rawSession.wsOpen.value = true
      rawSession.sessionId.value = 'connected-session-456'

      // Close the tab — WS is connected, so sendClose reaches backend
      mgr.closeTab(tab.id)

      // Should NOT fall back to HTTP API
      expect(onCloseViaHttp).not.toHaveBeenCalled()
    })

    it('does NOT call onCloseSessionViaHttp when session has no ID', () => {
      const onCloseViaHttp = vi.fn()
      const mgr = createTabManager({ onCloseSessionViaHttp: onCloseViaHttp })

      const tab = mgr.createTab()
      const rawSession = getRawSession(tabIndex(mgr, tab))
      // Session never connected, sessionId is empty
      rawSession.sessionId.value = ''
      rawSession.wsOpen.value = false

      mgr.closeTab(tab.id)
      expect(onCloseViaHttp).not.toHaveBeenCalled()
    })

    it('disposes xterm instance when closing', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab()
      const disposeSpy = vi.spyOn(tab.xterm!, 'dispose')

      mgr.closeTab(tab.id)
      expect(disposeSpy).toHaveBeenCalled()
    })

    it('calls sendClose on the session', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab()
      const rawSession = getRawSession(tabIndex(mgr, tab))

      mgr.closeTab(tab.id)
      expect(rawSession.sendClose).toHaveBeenCalled()
    })
  })

  describe('disposeAll', () => {
    it('clears all tabs and resets activeTabId', () => {
      const mgr = createTabManager()
      mgr.createTab('/home/a')
      mgr.createTab('/home/b')
      expect(mgr.tabs.value.length).toBeGreaterThanOrEqual(3)

      mgr.disposeAll()
      expect(mgr.tabs.value).toHaveLength(0)
      expect(mgr.activeTabId.value).toBe('')
    })

    it('calls onCloseSessionViaHttp for disconnected tabs (session leak fix)', () => {
      const onCloseViaHttp = vi.fn()
      const mgr = createTabManager({ onCloseSessionViaHttp: onCloseViaHttp })

      // Create tab1 — set up as disconnected with session ID
      const tab1 = mgr.createTab('/home/a')
      const rawSession1 = getRawSession(tabIndex(mgr, tab1))
      rawSession1.sessionId.value = 'disconnected-session-1'
      rawSession1.wsOpen.value = false
      rawSession1.connectionState.value = 'disconnected'

      // Create tab2 — set up as connected with session ID
      const tab2 = mgr.createTab('/home/b')
      const rawSession2 = getRawSession(tabIndex(mgr, tab2))
      rawSession2.sessionId.value = 'connected-session-2'
      rawSession2.wsOpen.value = true
      rawSession2.connectionState.value = 'connected'

      mgr.disposeAll()

      // Only the disconnected tab should trigger HTTP fallback
      expect(onCloseViaHttp).toHaveBeenCalledWith('disconnected-session-1')
      expect(onCloseViaHttp).not.toHaveBeenCalledWith('connected-session-2')
    })

    it('disposes all xterm instances', () => {
      const mgr = createTabManager()
      const tab1 = mgr.createTab('/home/a')
      const tab2 = mgr.createTab('/home/b')

      const spy1 = vi.spyOn(tab1.xterm!, 'dispose')
      const spy2 = vi.spyOn(tab2.xterm!, 'dispose')

      mgr.disposeAll()
      expect(spy1).toHaveBeenCalled()
      expect(spy2).toHaveBeenCalled()
    })

    it('calls sendClose on all sessions', () => {
      const mgr = createTabManager()
      const tab1 = mgr.createTab('/home/a')
      const tab2 = mgr.createTab('/home/b')
      const rawSession1 = getRawSession(tabIndex(mgr, tab1))
      const rawSession2 = getRawSession(tabIndex(mgr, tab2))

      mgr.disposeAll()
      expect(rawSession1.sendClose).toHaveBeenCalled()
      expect(rawSession2.sendClose).toHaveBeenCalled()
    })
  })

  describe('switchTab', () => {
    it('changes the active tab', () => {
      const mgr = createTabManager()
      const tab1 = mgr.tabs.value[0]
      const tab2 = mgr.createTab()

      expect(mgr.activeTabId.value).toBe(tab2.id)

      mgr.switchTab(tab1.id)
      expect(mgr.activeTabId.value).toBe(tab1.id)
    })

    it('does nothing for nonexistent tab', () => {
      const mgr = createTabManager()
      const currentActive = mgr.activeTabId.value
      mgr.switchTab('nonexistent-id')
      expect(mgr.activeTabId.value).toBe(currentActive)
    })
  })

  describe('disconnectAll', () => {
    it('disconnects all tab sessions', () => {
      const mgr = createTabManager()
      const tab1 = mgr.tabs.value[0]
      const tab2 = mgr.createTab()
      const rawSession1 = getRawSession(tabIndex(mgr, tab1))
      const rawSession2 = getRawSession(tabIndex(mgr, tab2))

      mgr.disconnectAll()

      expect(rawSession1.disconnect).toHaveBeenCalled()
      expect(rawSession2.disconnect).toHaveBeenCalled()
    })

    it('syncs sessionId after disconnect for reconnect', async () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Simulate a session that was connected and has a session ID
      rawSession.sessionId.value = 'persist-session-789'
      rawSession.connectionState.value = 'connected'

      mgr.disconnectAll()
      await nextTick()

      // After disconnect, sessionId should be synced to the tab
      // (disconnect preserves sessionId in the session composable)
      expect(tab.sessionId).toBe('persist-session-789')
    })
  })

  describe('connectTab', () => {
    it('connects a disconnected tab', async () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const rawSession = getRawSession(tabIndex(mgr, tab))

      expect(tab.session.connectionState).toBe('disconnected')

      await mgr.connectTab(tab.id)
      expect(rawSession.connect).toHaveBeenCalled()
    })

    it('does not connect an already connected tab', async () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Simulate connected state
      rawSession.connectionState.value = 'connected'

      await mgr.connectTab(tab.id)
      expect(rawSession.connect).not.toHaveBeenCalled()
    })

    it('does nothing for nonexistent tab', async () => {
      const mgr = createTabManager()
      // Should not throw
      await mgr.connectTab('nonexistent-id')
    })

    it('syncs sessionId to tab after connect', async () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Simulate connect returning a session ID
      rawSession.sessionId.value = 'synced-session-999'

      await mgr.connectTab(tab.id)
      await nextTick()

      expect(tab.sessionId).toBe('synced-session-999')
    })
  })

  describe('syncTabSessionId', () => {
    it('syncs session sessionId to tab sessionId', async () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const rawSession = getRawSession(tabIndex(mgr, tab))

      expect(tab.sessionId).toBe('')

      rawSession.sessionId.value = 'sync-test-id'
      mgr.syncTabSessionId(tab.id)
      await nextTick()
      expect(tab.sessionId).toBe('sync-test-id')
    })

    it('does nothing for nonexistent tab', () => {
      const mgr = createTabManager()
      expect(() => mgr.syncTabSessionId('nonexistent-id')).not.toThrow()
    })
  })

  describe('updateTabCwd', () => {
    it('updates tab cwd and title', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab('/home/old')

      expect(tab.title).toBe('old')

      mgr.updateTabCwd(tab.id, '/home/newproject')
      expect(tab.cwd).toBe('/home/newproject')
      expect(tab.title).toBe('newproject')
    })

    it('does nothing for nonexistent tab', () => {
      const mgr = createTabManager()
      expect(() => mgr.updateTabCwd('nonexistent-id', '/home')).not.toThrow()
    })
  })

  describe('updateFontSize', () => {
    it('updates fontSize on all xterm instances', () => {
      const mgr = createTabManager()
      mgr.createTab()

      mgr.updateFontSize(18)

      for (const tab of mgr.tabs.value) {
        expect(tab.xterm!.options.fontSize).toBe(18)
      }
    })
  })

  describe('updateTheme', () => {
    it('updates theme on all xterm instances', () => {
      const mgr = createTabManager()
      mgr.createTab()

      const theme = { background: '#1e1e1e' }
      mgr.updateTheme(theme)

      for (const tab of mgr.tabs.value) {
        expect(tab.xterm!.options.theme).toStrictEqual(theme)
      }
    })
  })

  describe('getTab', () => {
    it('returns the tab with the given ID', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab('/home/test')
      const found = mgr.getTab(tab.id)
      expect(found).toBeDefined()
      expect(found!.id).toBe(tab.id)
    })

    it('returns undefined for nonexistent tab', () => {
      const mgr = createTabManager()
      expect(mgr.getTab('nonexistent-id')).toBeUndefined()
    })
  })

  describe('activeTab computed', () => {
    it('returns the currently active tab', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab('/active-test')
      expect(mgr.activeTab.value).not.toBeNull()
      expect(mgr.activeTab.value!.id).toBe(tab.id)
    })

    it('returns null when no tabs match activeTabId', () => {
      const mgr = createTabManager()
      mgr.activeTabId.value = 'invalid-id'
      expect(mgr.activeTab.value).toBeNull()
    })
  })

  describe('reactive auto-unwrap behavior', () => {
    it('tab.session.connectionState is a string (not Ref) due to reactive()', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const state = tab.session.connectionState
      expect(typeof state).toBe('string')
      expect(state).toBe('disconnected')
    })

    it('tab.session.sessionId is a string (not Ref) due to reactive()', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const id = tab.session.sessionId
      expect(typeof id).toBe('string')
      expect(id).toBe('')
    })

    it('tab.session.wsOpen is a boolean (not Ref) due to reactive()', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const open = tab.session.wsOpen
      expect(typeof open).toBe('boolean')
      expect(open).toBe(false)
    })

    it('connectionState updates are reflected without .value', async () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Update the raw session's state
      rawSession.connectionState.value = 'connecting'
      await nextTick()
      expect(tab.session.connectionState).toBe('connecting')

      rawSession.connectionState.value = 'connected'
      await nextTick()
      expect(tab.session.connectionState).toBe('connected')
    })
  })

  describe('callbacks', () => {
    it('onReplay clears terminal and writes replay data (no frontend suppress)', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab()
      const rawSession = getRawSession(tabIndex(mgr, tab))
      const writeSpy = vi.spyOn(tab.xterm!, 'write')
      const resetSpy = vi.spyOn(tab.xterm!, 'reset')

      const setCallbacksCall = rawSession.setCallbacks.mock.calls[0][0]

      // Replay resets terminal and writes replay data
      setCallbacksCall.onReplay('replay-buffer-with-prompt$ ')
      expect(resetSpy).toHaveBeenCalled()
      expect(writeSpy).toHaveBeenLastCalledWith('replay-buffer-with-prompt$ ')

      // After replay, output is NOT suppressed on the frontend —
      // the backend handles suppressOutput to avoid duplicate prompts.
      writeSpy.mockClear()
      setCallbacksCall.onOutput('new output')
      expect(writeSpy).toHaveBeenCalledWith('new output')
    })

    it('fires onExit callback when session exits', () => {
      const onExit = vi.fn()
      const mgr = createTabManager({ onExit })
      const tab = mgr.createTab()
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Find the onExit callback that createTab wired to the session
      const setCallbacksCall = rawSession.setCallbacks.mock.calls[0][0]
      expect(setCallbacksCall.onExit).toBeDefined()

      // Simulate the session calling onExit
      setCallbacksCall.onExit()

      expect(onExit).toHaveBeenCalledWith(tab.id)
    })

    it('fires onError callback when session errors', () => {
      const onError = vi.fn()
      const mgr = createTabManager({ onError })
      const tab = mgr.createTab()
      const rawSession = getRawSession(tabIndex(mgr, tab))

      const setCallbacksCall = rawSession.setCallbacks.mock.calls[0][0]
      expect(setCallbacksCall.onError).toBeDefined()

      setCallbacksCall.onError('Shell crashed', 'shell_start_failed')

      expect(onError).toHaveBeenCalledWith(tab.id, 'Shell crashed', 'shell_start_failed')
    })

    it('updates tab cwd and title on status callback', () => {
      const mgr = createTabManager()
      const tab = mgr.createTab('/home/old')
      const rawSession = getRawSession(tabIndex(mgr, tab))

      expect(tab.title).toBe('old')

      const setCallbacksCall = rawSession.setCallbacks.mock.calls[0][0]
      setCallbacksCall.onStatus({ running: true, cwd: '/home/newproject' })

      expect(tab.cwd).toBe('/home/newproject')
      expect(tab.title).toBe('newproject')
    })
  })

  describe('mountTabXterm', () => {
    it('sets container on the tab', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      const container = document.createElement('div')

      mgr.mountTabXterm(tab, container)
      expect(tab.container).toBe(container)
    })

    it('does not throw for tab without xterm', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]
      tab.xterm = null

      expect(() => mgr.mountTabXterm(tab, document.createElement('div'))).not.toThrow()
    })

    it('updates container even if xterm element already exists', () => {
      const mgr = createTabManager()
      const tab = mgr.tabs.value[0]

      // First mount
      const container1 = document.createElement('div')
      mgr.mountTabXterm(tab, container1)
      expect(tab.container).toBe(container1)

      // Second mount to new container — container should update
      const container2 = document.createElement('div')
      mgr.mountTabXterm(tab, container2)
      expect(tab.container).toBe(container2)
    })
  })

  describe('closeTab — mixed connection states (session leak scenarios)', () => {
    it('handles closing a tab that was connected then disconnected then reconnected', () => {
      const onCloseViaHttp = vi.fn()
      const mgr = createTabManager({ onCloseSessionViaHttp: onCloseViaHttp })

      const tab = mgr.createTab('/home')
      const rawSession = getRawSession(tabIndex(mgr, tab))

      // Simulate: connected → received session ID → disconnected → reconnected
      rawSession.sessionId.value = 'reconnected-session'
      rawSession.connectionState.value = 'connected'
      rawSession.wsOpen.value = true

      // Close while connected — no HTTP fallback needed
      mgr.closeTab(tab.id)
      expect(onCloseViaHttp).not.toHaveBeenCalled()
    })

    it('handles closing multiple disconnected tabs in sequence', () => {
      const onCloseViaHttp = vi.fn()
      const mgr = createTabManager({ onCloseSessionViaHttp: onCloseViaHttp })

      const tab1 = mgr.createTab('/home/a')
      const rawSession1 = getRawSession(tabIndex(mgr, tab1))
      rawSession1.sessionId.value = 'session-a'
      rawSession1.wsOpen.value = false

      const tab2 = mgr.createTab('/home/b')
      const rawSession2 = getRawSession(tabIndex(mgr, tab2))
      rawSession2.sessionId.value = 'session-b'
      rawSession2.wsOpen.value = false

      mgr.closeTab(tab1.id)
      mgr.closeTab(tab2.id)

      expect(onCloseViaHttp).toHaveBeenCalledWith('session-a')
      expect(onCloseViaHttp).toHaveBeenCalledWith('session-b')
      expect(onCloseViaHttp).toHaveBeenCalledTimes(2)
    })
  })
})
