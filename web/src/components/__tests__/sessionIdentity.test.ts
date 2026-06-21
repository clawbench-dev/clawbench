import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  registerSessionActions,
  registerSessionDrawerRef,
  useSessionIdentity,
  updateModeState,
  updateAvailableModes,
  clearModeState,
  updateCommandState,
  clearCommandState,
  updateThinkingEffortState,
  updateAvailableThinkingEfforts,
  clearThinkingEffortState,
  updateUsageState,
  clearUsageState,
  toggleAutoApprove,
  resetIdentity,
  prefetchCommands,
  currentAgentId,
} from '@/composables/useSessionIdentity.ts'

// Reset module-level callbacks between tests by re-registering with nulls
beforeEach(() => {
  registerSessionActions({
    switchSession: vi.fn(),
    createSession: vi.fn(),
    deleteSession: vi.fn(),
    sendMessage: vi.fn(),
    openChatPanel: vi.fn(),
    continueFromExecution: vi.fn().mockResolvedValue(true),
    checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
  })
})

describe('registerSessionActions', () => {
  it('registers action callbacks that are callable through the composable', async () => {
    const mockSwitch = vi.fn().mockResolvedValue(undefined)
    const mockCreate = vi.fn().mockResolvedValue(undefined)
    const mockDelete = vi.fn().mockResolvedValue(undefined)
    const mockSend = vi.fn().mockResolvedValue(undefined)
    const mockOpen = vi.fn()

    registerSessionActions({
      switchSession: mockSwitch,
      createSession: mockCreate,
      deleteSession: mockDelete,
      sendMessage: mockSend,
      openChatPanel: mockOpen,
    })

    // Verify delegation works by calling through the composable
    const { switchSession, createSession, deleteSession, sendMessage, openChatPanel } = useSessionIdentity()

    await switchSession('s1')
    expect(mockSwitch).toHaveBeenCalledWith('s1')

    await createSession('agent-1')
    expect(mockCreate).toHaveBeenCalledWith('agent-1')

    await deleteSession('s2', 'claude')
    expect(mockDelete).toHaveBeenCalledWith('s2', 'claude')

    await sendMessage('hello', ['/file.ts'])
    expect(mockSend).toHaveBeenCalledWith('hello', ['/file.ts'])

    openChatPanel()
    expect(mockOpen).toHaveBeenCalled()
  })

  it('replaces previous callbacks on re-registration', async () => {
    const firstSwitch = vi.fn()
    const secondSwitch = vi.fn()

    registerSessionActions({
      switchSession: firstSwitch,
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    registerSessionActions({
      switchSession: secondSwitch,
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    const { switchSession } = useSessionIdentity()
    await switchSession('session-123')
    expect(secondSwitch).toHaveBeenCalledWith('session-123')
    expect(firstSwitch).not.toHaveBeenCalled()
  })
})

describe('action delegation', () => {
  it('delegates switchSession to registered callback', async () => {
    const mockSwitch = vi.fn()
    registerSessionActions({
      switchSession: mockSwitch,
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    const { switchSession } = useSessionIdentity()
    await switchSession('session-123')
    expect(mockSwitch).toHaveBeenCalledWith('session-123')
  })

  it('does nothing when switchSession has no callback', async () => {
    // Register with nulls — switchSession will be a no-op
    registerSessionActions({
      switchSession: async () => {},
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })
    const { switchSession } = useSessionIdentity()
    // Should not throw
    await expect(switchSession('session-123')).resolves.toBeUndefined()
  })

  it('delegates createSession to registered callback', async () => {
    const mockCreate = vi.fn()
    registerSessionActions({
      switchSession: vi.fn(),
      createSession: mockCreate,
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    const { createSession } = useSessionIdentity()
    await createSession('agent-1')
    expect(mockCreate).toHaveBeenCalledWith('agent-1')
  })

  it('delegates deleteSession with backend', async () => {
    const mockDelete = vi.fn()
    registerSessionActions({
      switchSession: vi.fn(),
      createSession: vi.fn(),
      deleteSession: mockDelete,
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    const { deleteSession } = useSessionIdentity()
    await deleteSession('session-1', 'claude')
    expect(mockDelete).toHaveBeenCalledWith('session-1', 'claude')
  })

  it('delegates sendMessage with filePaths', async () => {
    const mockSend = vi.fn()
    registerSessionActions({
      switchSession: vi.fn(),
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: mockSend,
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    const { sendMessage } = useSessionIdentity()
    await sendMessage('hello', ['/tmp/file.go'])
    expect(mockSend).toHaveBeenCalledWith('hello', ['/tmp/file.go'])
  })

  it('delegates openChatPanel to registered callback', () => {
    const mockOpen = vi.fn()
    registerSessionActions({
      switchSession: vi.fn(),
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: mockOpen,
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    const { openChatPanel } = useSessionIdentity()
    openChatPanel()
    expect(mockOpen).toHaveBeenCalled()
  })

  it('delegates continueFromExecution to registered callback', async () => {
    const mockContinue = vi.fn().mockResolvedValue(true)
    const mockSwitchTab = vi.fn()
    registerSessionActions({
      switchSession: vi.fn(),
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: mockContinue,
      checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
    })

    const { continueFromExecution } = useSessionIdentity()
    const result = await continueFromExecution(1, 42, mockSwitchTab)
    expect(result).toBe(true)
    expect(mockContinue).toHaveBeenCalledWith(1, 42, mockSwitchTab)
  })

  it('delegates checkContinueSession to registered callback', async () => {
    const mockCheck = vi.fn().mockResolvedValue({ exists: true, sessionId: 'existing-s1' })
    registerSessionActions({
      switchSession: vi.fn(),
      createSession: vi.fn(),
      deleteSession: vi.fn(),
      sendMessage: vi.fn(),
      openChatPanel: vi.fn(),
      continueFromExecution: vi.fn().mockResolvedValue(true),
      checkContinueSession: mockCheck,
    })

    const { checkContinueSession } = useSessionIdentity()
    const result = await checkContinueSession(1, 42)
    expect(result).toEqual({ exists: true, sessionId: 'existing-s1' })
    expect(mockCheck).toHaveBeenCalledWith(1, 42)
  })
})

describe('identity refs', () => {
  it('returns reactive refs from the singleton with correct initial values', () => {
    const { currentSessionId, currentSessionTitle, currentThinkingEffort, currentBackend, runningSessions, currentAgentId, currentModelId, currentModelName } = useSessionIdentity()
    // Initial values should be empty strings/sets
    expect(currentSessionId.value).toBe('')
    expect(currentSessionTitle.value).toBe('')
    expect(currentThinkingEffort.value).toBe('')
    expect(currentBackend.value).toBe('')
    expect(currentAgentId.value).toBe('')
    expect(currentModelId.value).toBe('')
    expect(currentModelName.value).toBe('')
    expect(runningSessions.value).toBeInstanceOf(Set)
    expect(runningSessions.value.size).toBe(0)
  })

  it('runningSessions reflects session state changes via the ref', () => {
    const { runningSessions } = useSessionIdentity()
    // Simulate a session starting
    runningSessions.value.add('session-1')
    expect(runningSessions.value.has('session-1')).toBe(true)

    // Simulate the session completing — remove it
    runningSessions.value.delete('session-1')
    expect(runningSessions.value.has('session-1')).toBe(false)

    // Clean up
    runningSessions.value.clear()
  })

  it('runningSessionsVersion is exposed and can be incremented', () => {
    const { runningSessionsVersion } = useSessionIdentity()
    const initial = runningSessionsVersion.value
    runningSessionsVersion.value = initial + 1
    expect(runningSessionsVersion.value).toBe(initial + 1)
    // Clean up
    runningSessionsVersion.value = initial
  })

  it('runningSessionsVersion is shared across instances', () => {
    const instance1 = useSessionIdentity()
    const instance2 = useSessionIdentity()
    const initial = instance1.runningSessionsVersion.value
    instance1.runningSessionsVersion.value = initial + 5
    expect(instance2.runningSessionsVersion.value).toBe(initial + 5)
    // Clean up
    instance1.runningSessionsVersion.value = initial
  })

  it('currentSessionId is writable and shared across instances', () => {
    const instance1 = useSessionIdentity()
    const instance2 = useSessionIdentity()

    instance1.currentSessionId.value = 'test-session-123'
    expect(instance2.currentSessionId.value).toBe('test-session-123')

    // Clean up
    instance1.currentSessionId.value = ''
  })

  it('sessionDrawerOpen is exposed and defaults to false', () => {
    const { sessionDrawerOpen } = useSessionIdentity()
    expect(sessionDrawerOpen.value).toBe(false)
  })

  it('openSessionTab sets sessionDrawerOpen to true', () => {
    const { sessionDrawerOpen, openSessionTab } = useSessionIdentity()
    expect(sessionDrawerOpen.value).toBe(false)
    openSessionTab()
    expect(sessionDrawerOpen.value).toBe(true)
    // Clean up
    sessionDrawerOpen.value = false
  })

  it('openAgentSelector delegates to registered drawer ref', () => {
    const mockOpenAgentSelector = vi.fn()
    registerSessionDrawerRef({ openAgentSelector: mockOpenAgentSelector })
    const { openAgentSelector } = useSessionIdentity()
    openAgentSelector()
    expect(mockOpenAgentSelector).toHaveBeenCalled()
  })

  it('openAgentSelector does nothing when no drawer ref registered', () => {
    registerSessionDrawerRef(null)
    const { openAgentSelector } = useSessionIdentity()
    // Should not throw
    expect(() => openAgentSelector()).not.toThrow()
  })

  it('registerSessionDrawerRef can be updated', () => {
    const firstRef = { openAgentSelector: vi.fn() }
    const secondRef = { openAgentSelector: vi.fn() }
    registerSessionDrawerRef(firstRef)
    const { openAgentSelector } = useSessionIdentity()
    openAgentSelector()
    expect(firstRef.openAgentSelector).toHaveBeenCalled()
    registerSessionDrawerRef(secondRef)
    openAgentSelector()
    expect(secondRef.openAgentSelector).toHaveBeenCalled()
  })
})

// ── Mode state helpers ──

describe('updateModeState', () => {
  it('sets currentModeId and currentModeName from matching mode', () => {
    const { currentModeId, currentModeName } = useSessionIdentity()
    updateModeState('code', [
      { id: 'ask', name: 'Ask' },
      { id: 'code', name: 'Code' },
    ])
    expect(currentModeId.value).toBe('code')
    expect(currentModeName.value).toBe('Code')
    // Clean up
    clearModeState()
  })

  it('sets modeName to modeId when no matching mode found', () => {
    const { currentModeId, currentModeName } = useSessionIdentity()
    updateModeState('unknown', [])
    expect(currentModeId.value).toBe('unknown')
    expect(currentModeName.value).toBe('unknown')
    // Clean up
    clearModeState()
  })

  it('updates availableModes when modes array is non-empty', () => {
    const { availableModes } = useSessionIdentity()
    const modes = [{ id: 'ask', name: 'Ask' }, { id: 'code', name: 'Code' }]
    updateModeState('ask', modes)
    expect(availableModes.value).toEqual(modes)
    // Clean up
    clearModeState()
  })

  it('does not update availableModes when modes array is empty', () => {
    const { availableModes } = useSessionIdentity()
    const preModes = [{ id: 'ask', name: 'Ask' }]
    updateModeState('ask', preModes)
    updateModeState('code', [])
    // availableModes should still have the previous modes
    expect(availableModes.value).toEqual(preModes)
    // Clean up
    clearModeState()
  })

  it('does not set modeId when modeId is empty', () => {
    const { currentModeId } = useSessionIdentity()
    const prevValue = currentModeId.value
    updateModeState('', [{ id: 'ask', name: 'Ask' }])
    // modeId should not change
    expect(currentModeId.value).toBe(prevValue)
    // Clean up
    clearModeState()
  })
})

describe('updateAvailableModes', () => {
  it('updates available modes without changing current selection', () => {
    const { availableModes } = useSessionIdentity()
    updateAvailableModes([{ id: 'ask', name: 'Ask' }])
    expect(availableModes.value).toEqual([{ id: 'ask', name: 'Ask' }])
    // Clean up
    clearModeState()
  })

  it('does nothing when modes array is empty', () => {
    const { availableModes } = useSessionIdentity()
    updateAvailableModes([{ id: 'ask', name: 'Ask' }])
    updateAvailableModes([])
    expect(availableModes.value).toEqual([{ id: 'ask', name: 'Ask' }])
    // Clean up
    clearModeState()
  })
})

describe('clearModeState', () => {
  it('clears mode state', () => {
    const { currentModeId, currentModeName, availableModes } = useSessionIdentity()
    updateModeState('code', [{ id: 'code', name: 'Code' }])
    clearModeState()
    expect(currentModeId.value).toBe('')
    expect(currentModeName.value).toBe('')
    expect(availableModes.value).toEqual([])
  })
})

// ── Command state helpers ──

describe('updateCommandState', () => {
  it('updates available commands', () => {
    const { availableCommands } = useSessionIdentity()
    const cmds = [{ name: '/commit', description: 'Create a commit' }]
    updateCommandState(cmds)
    expect(availableCommands.value).toEqual(cmds)
    // Clean up
    clearCommandState()
  })
})

describe('clearCommandState', () => {
  it('clears available commands', () => {
    const { availableCommands } = useSessionIdentity()
    updateCommandState([{ name: '/commit', description: 'Create a commit' }])
    clearCommandState()
    expect(availableCommands.value).toEqual([])
  })
})

// ── Thinking effort state helpers ──

describe('updateThinkingEffortState', () => {
  it('sets thinking effort id and name from matching level', () => {
    const { currentThinkingEffort, currentThinkingEffortName } = useSessionIdentity()
    updateThinkingEffortState('high', [
      { id: 'low', name: 'Low' },
      { id: 'high', name: 'High' },
    ])
    expect(currentThinkingEffort.value).toBe('high')
    expect(currentThinkingEffortName.value).toBe('High')
    // Clean up
    clearThinkingEffortState()
  })

  it('resolves name from available levels when id was set before levels arrived', () => {
    const { currentThinkingEffort, currentThinkingEffortName } = useSessionIdentity()
    // First set id with no levels (name stays empty)
    updateThinkingEffortState('medium', [])
    expect(currentThinkingEffort.value).toBe('medium')
    // Now update levels — should resolve name
    updateThinkingEffortState('medium', [
      { id: 'low', name: 'Low' },
      { id: 'medium', name: 'Medium' },
    ])
    expect(currentThinkingEffortName.value).toBe('Medium')
    // Clean up
    clearThinkingEffortState()
  })

  it('uses id as name when no matching level found', () => {
    const { currentThinkingEffort, currentThinkingEffortName } = useSessionIdentity()
    updateThinkingEffortState('custom', [])
    expect(currentThinkingEffortName.value).toBe('custom')
    // Clean up
    clearThinkingEffortState()
  })
})

describe('updateAvailableThinkingEfforts', () => {
  it('updates available levels and resolves name if id was set', () => {
    const { currentThinkingEffort, currentThinkingEffortName } = useSessionIdentity()
    // Set effort id first without levels
    updateThinkingEffortState('high', [])
    // Now provide levels
    updateAvailableThinkingEfforts([
      { id: 'low', name: 'Low' },
      { id: 'high', name: 'High' },
    ])
    expect(currentThinkingEffortName.value).toBe('High')
    // Clean up
    clearThinkingEffortState()
  })

  it('does nothing when levels array is empty', () => {
    const { availableThinkingEfforts } = useSessionIdentity()
    updateAvailableThinkingEfforts([{ id: 'low', name: 'Low' }])
    updateAvailableThinkingEfforts([])
    expect(availableThinkingEfforts.value).toEqual([{ id: 'low', name: 'Low' }])
    // Clean up
    clearThinkingEffortState()
  })
})

describe('clearThinkingEffortState', () => {
  it('clears thinking effort state', () => {
    const { currentThinkingEffortName, availableThinkingEfforts } = useSessionIdentity()
    updateThinkingEffortState('high', [{ id: 'high', name: 'High' }])
    clearThinkingEffortState()
    expect(currentThinkingEffortName.value).toBe('')
    expect(availableThinkingEfforts.value).toEqual([])
  })
})

// ── Usage state helpers ──

describe('updateUsageState', () => {
  it('updates usage state', () => {
    const { contextUsed, contextSize, contextCost, contextCurrency } = useSessionIdentity()
    updateUsageState(5000, 200000, 0.05, 'USD')
    expect(contextUsed.value).toBe(5000)
    expect(contextSize.value).toBe(200000)
    expect(contextCost.value).toBe(0.05)
    expect(contextCurrency.value).toBe('USD')
    // Clean up
    clearUsageState()
  })

  it('defaults cost and currency when not provided', () => {
    const { contextCost, contextCurrency } = useSessionIdentity()
    updateUsageState(1000, 50000)
    expect(contextCost.value).toBe(0)
    expect(contextCurrency.value).toBe('')
    // Clean up
    clearUsageState()
  })
})

describe('clearUsageState', () => {
  it('clears usage state', () => {
    const { contextUsed, contextSize, contextCost, contextCurrency } = useSessionIdentity()
    updateUsageState(5000, 200000, 0.05, 'USD')
    clearUsageState()
    expect(contextUsed.value).toBe(0)
    expect(contextSize.value).toBe(0)
    expect(contextCost.value).toBe(0)
    expect(contextCurrency.value).toBe('')
  })
})

// ── toggleAutoApprove ──

describe('toggleAutoApprove', () => {
  it('sets autoApprove ref', () => {
    const { autoApprove } = useSessionIdentity()
    toggleAutoApprove(true)
    expect(autoApprove.value).toBe(true)
    toggleAutoApprove(false)
    expect(autoApprove.value).toBe(false)
  })

  it('sends PATCH request when sessionId is set', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const { autoApprove, currentSessionId } = useSessionIdentity()
    currentSessionId.value = 'test-session-1'

    toggleAutoApprove(true)

    expect(fetchMock).toHaveBeenCalledWith('/api/ai/session/update', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessionId: 'test-session-1', autoApprove: true }),
    })

    // Clean up
    currentSessionId.value = ''
    autoApprove.value = false
    vi.unstubAllGlobals()
  })

  it('does not send PATCH request when sessionId is empty', () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true })
    vi.stubGlobal('fetch', fetchMock)

    const { autoApprove, currentSessionId } = useSessionIdentity()
    currentSessionId.value = ''

    toggleAutoApprove(true)

    expect(fetchMock).not.toHaveBeenCalled()

    // Clean up
    autoApprove.value = false
    vi.unstubAllGlobals()
  })
})

// ── resetIdentity ──

describe('resetIdentity', () => {
  it('resets all singleton refs to initial values', () => {
    const identity = useSessionIdentity()

    // Set some values
    identity.currentSessionId.value = 's1'
    identity.currentSessionTitle.value = 'Title'
    identity.currentBackend.value = 'claude'
    currentAgentId.value = 'agent-1'
    identity.currentModelId.value = 'model-1'
    identity.currentModelName.value = 'GPT-4'
    identity.currentThinkingEffort.value = 'high'
    identity.currentThinkingEffortName.value = 'High'
    identity.currentModeId.value = 'code'
    identity.currentModeName.value = 'Code'
    identity.currentTransport.value = 'acp-stdio'
    identity.autoApprove.value = true
    identity.availableModes.value = [{ id: 'code', name: 'Code' }]
    identity.availableCommands.value = [{ name: '/commit', description: 'Commit' }]
    identity.availableThinkingEfforts.value = [{ id: 'high', name: 'High' }]
    identity.contextUsed.value = 5000
    identity.contextSize.value = 200000
    identity.contextCost.value = 0.05
    identity.contextCurrency.value = 'USD'
    identity.runningSessions.value = new Set(['s1'])
    identity.runningSessionsVersion.value = 5
    identity.sessionDrawerOpen.value = true

    resetIdentity()

    expect(identity.currentSessionId.value).toBe('')
    expect(identity.currentSessionTitle.value).toBe('')
    expect(identity.currentBackend.value).toBe('')
    expect(currentAgentId.value).toBe('')
    expect(identity.currentModelId.value).toBe('')
    expect(identity.currentModelName.value).toBe('')
    expect(identity.currentThinkingEffort.value).toBe('')
    expect(identity.currentThinkingEffortName.value).toBe('')
    expect(identity.currentModeId.value).toBe('')
    expect(identity.currentModeName.value).toBe('')
    expect(identity.currentTransport.value).toBe('')
    expect(identity.autoApprove.value).toBe(false)
    expect(identity.availableModes.value).toEqual([])
    expect(identity.availableCommands.value).toEqual([])
    expect(identity.availableThinkingEfforts.value).toEqual([])
    expect(identity.contextUsed.value).toBe(0)
    expect(identity.contextSize.value).toBe(0)
    expect(identity.contextCost.value).toBe(0)
    expect(identity.contextCurrency.value).toBe('')
    expect(identity.runningSessions.value.size).toBe(0)
    expect(identity.runningSessionsVersion.value).toBe(0)
    expect(identity.sessionDrawerOpen.value).toBe(false)
  })

  it('cleans up E2E test bridge on window', () => {
    ;(window as any).__clawbench = {
      createSession: vi.fn(),
      switchSession: vi.fn(),
      deleteSession: vi.fn(),
    }

    resetIdentity()

    expect((window as any).__clawbench.createSession).toBeNull()
    expect((window as any).__clawbench.switchSession).toBeNull()
    expect((window as any).__clawbench.deleteSession).toBeNull()

    delete (window as any).__clawbench
  })
})

// ── prefetchCommands (deprecated no-op) ──

describe('prefetchCommands', () => {
  it('is a no-op and does not throw', async () => {
    await expect(prefetchCommands('agent-1')).resolves.toBeUndefined()
  })
})
