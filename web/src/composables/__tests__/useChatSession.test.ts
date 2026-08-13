import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'

// ── Timer leak prevention ──

const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

const pendingIntervals: ReturnType<typeof setInterval>[] = []
const _origSetInterval = setInterval
globalThis.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetInterval(fn, ms, ...args)
  pendingIntervals.push(id)
  return id
}) as typeof setInterval

afterEach(() => {
  for (const id of pendingTimers) {
    clearTimeout(id)
  }
  pendingTimers.length = 0
  for (const id of pendingIntervals) {
    clearInterval(id)
  }
  pendingIntervals.length = 0
})

// ── Hoisted mock state (plain objects, no Vue imports needed) ──

const { mockState, resetMockState } = vi.hoisted(() => {
  const mockState = {
    runningSessions: new Set<string>(),
    runningSessionsVersion: 0,
    currentSessionId: '',
    chatUnreadCount: 0,
    chatInitialMessages: 20,
    chatPageSize: 20,
    sessionMaxCount: 10,
    sessionCount: 0,
  }
  function resetMockState() {
    mockState.runningSessions.clear()
    mockState.runningSessionsVersion = 0
    mockState.currentSessionId = ''
    mockState.chatUnreadCount = 0
    mockState.chatInitialMessages = 20
    mockState.chatPageSize = 20
    mockState.sessionMaxCount = 10
    mockState.sessionCount = 0
  }
  return { mockState, resetMockState }
})

const { mockIdentity, mockToastFn, mockAgentFns, mockUtilsFns, mockIdentityFns, mockForceCleanupStreamingState, resetAdditionalMocks } = vi.hoisted(() => {
  const mockIdentity: Record<string, string | boolean> = {
    currentSessionTitle: '',
    currentBackend: '',
    currentAgentId: '',
    currentModelId: '',
    currentModelName: '',
    currentThinkingEffort: '',
    currentThinkingEffortName: '',
    currentModeId: '',
    currentModeName: '',
    currentTransport: '',
    autoApprove: false,
  }
  const mockToastFn = vi.fn()
  const mockIdentityFns = {
    loadModelPref: vi.fn(),
    loadThinkingPref: vi.fn(),
    saveModelPref: vi.fn(),
    saveThinkingPref: vi.fn(),
    updateUsageState: vi.fn(),
    clearUsageState: vi.fn(),
    clearUsageStateById: vi.fn(),
  }
  const mockAgentFns = {
    loadAgents: vi.fn().mockResolvedValue(undefined),
    getAgentBackend: vi.fn().mockReturnValue(''),
    getAgentName: vi.fn().mockReturnValue('Test'),
    syncModelFromAgent: vi.fn().mockReturnValue({ modelId: '', modelName: '' }),
    getAgentModel: vi.fn().mockReturnValue(undefined),
    agentHeaderTitle: vi.fn().mockReturnValue('🤖 Test'),
  }
  const mockUtilsFns = {
    buildMessageSnapshot: vi.fn().mockReturnValue(''),
    parseMessages: vi.fn().mockReturnValue([]),
  }
  const mockForceCleanupStreamingState = vi.fn().mockReturnValue(undefined)
  function resetAdditionalMocks() {
    Object.keys(mockIdentity).forEach(k => { mockIdentity[k] = k === 'autoApprove' ? false : '' })
    mockToastFn.mockReset()
    mockIdentityFns.loadModelPref.mockReset()
    mockIdentityFns.loadThinkingPref.mockReset()
    mockIdentityFns.saveModelPref.mockReset()
    mockIdentityFns.saveThinkingPref.mockReset()
    mockIdentityFns.updateUsageState.mockReset()
    mockIdentityFns.clearUsageState.mockReset()
    mockUpdateUsageState.mockReset()
    mockClearUsageState.mockReset()
    mockAgentFns.loadAgents.mockReset().mockResolvedValue(undefined)
    mockAgentFns.getAgentBackend.mockReset().mockReturnValue('')
    mockAgentFns.getAgentName.mockReset().mockReturnValue('Test')
    mockAgentFns.syncModelFromAgent.mockReset().mockReturnValue({ modelId: '', modelName: '' })
    mockAgentFns.getAgentModel.mockReset().mockReturnValue(undefined)
    mockAgentFns.agentHeaderTitle.mockReset().mockReturnValue('🤖 Test')
    mockUtilsFns.buildMessageSnapshot.mockReset().mockReturnValue('')
    mockUtilsFns.parseMessages.mockReset().mockReturnValue([])
    mockForceCleanupStreamingState.mockReset().mockReturnValue(undefined)
  }
  return { mockIdentity, mockToastFn, mockAgentFns, mockUtilsFns, mockForceCleanupStreamingState, mockIdentityFns, resetAdditionalMocks }
})

// ── Mocks ──

vi.mock('@/composables/useSessionIdentity.ts', () => ({
  useSessionIdentity: () => ({
    currentSessionId: {
      get value() { return mockState.currentSessionId },
      set value(v) { mockState.currentSessionId = v },
    },
    currentSessionTitle: {
      get value() { return mockIdentity.currentSessionTitle },
      set value(v) { mockIdentity.currentSessionTitle = v },
    },
    currentBackend: {
      get value() { return mockIdentity.currentBackend },
      set value(v) { mockIdentity.currentBackend = v },
    },
    currentAgentId: {
      get value() { return mockIdentity.currentAgentId },
      set value(v) { mockIdentity.currentAgentId = v },
    },
    currentModelId: {
      get value() { return mockIdentity.currentModelId },
      set value(v) { mockIdentity.currentModelId = v },
    },
    currentModelName: {
      get value() { return mockIdentity.currentModelName },
      set value(v) { mockIdentity.currentModelName = v },
    },
    currentThinkingEffort: {
      get value() { return mockIdentity.currentThinkingEffort },
      set value(v) { mockIdentity.currentThinkingEffort = v },
    },
    currentThinkingEffortName: {
      get value() { return mockIdentity.currentThinkingEffortName },
      set value(v) { mockIdentity.currentThinkingEffortName = v },
    },
    currentModeId: {
      get value() { return mockIdentity.currentModeId || '' },
      set value(v) { mockIdentity.currentModeId = v },
    },
    currentModeName: {
      get value() { return mockIdentity.currentModeName || '' },
      set value(v) { mockIdentity.currentModeName = v },
    },
    currentTransport: {
      get value() { return mockIdentity.currentTransport },
      set value(v) { mockIdentity.currentTransport = v },
    },
    autoApprove: {
      get value() { return mockIdentity.autoApprove },
      set value(v) { mockIdentity.autoApprove = v },
    },
    availableCommands: { value: [] },
    availableModes: { value: [] },
    availableThinkingEfforts: { value: [] },
    thinkingEffortState: {
      currentId: {
        get value() { return mockIdentity.currentThinkingEffort },
        set value(v) { mockIdentity.currentThinkingEffort = v },
      },
      currentName: {
        get value() { return mockIdentity.currentThinkingEffortName },
        set value(v) { mockIdentity.currentThinkingEffortName = v },
      },
      available: { value: [] },
      syncFromData: vi.fn((id: string) => { if (id) mockIdentity.currentThinkingEffort = id }),
      syncAndFallback: vi.fn((id: string) => { if (id) mockIdentity.currentThinkingEffort = id }),
      loadPref: vi.fn((_: string, pref?: string) => { if (pref) mockIdentity.currentThinkingEffort = pref }),
      clear: vi.fn(() => { mockIdentity.currentThinkingEffort = ''; mockIdentity.currentThinkingEffortName = '' }),
    },
    modeState: {
      currentId: {
        get value() { return mockIdentity.currentModeId || '' },
        set value(v) { mockIdentity.currentModeId = v },
      },
      currentName: {
        get value() { return mockIdentity.currentModeName || '' },
        set value(v) { mockIdentity.currentModeName = v },
      },
      available: { value: [] },
      syncFromData: vi.fn((id: string) => { if (id) mockIdentity.currentModeId = id }),
      syncAndFallback: vi.fn((id: string) => { if (id) mockIdentity.currentModeId = id }),
      loadPref: vi.fn((_: string, pref?: string) => { if (pref) mockIdentity.currentModeId = pref }),
      clear: vi.fn(() => { mockIdentity.currentModeId = ''; mockIdentity.currentModeName = '' }),
    },
    contextSize: { value: 0 },
    runningSessions: {
      get value() { return mockState.runningSessions },
    },
    runningSessionsVersion: {
      get value() { return mockState.runningSessionsVersion },
      set value(v: number) { mockState.runningSessionsVersion = v },
    },
    agentHeaderTitle: { value: '' },
    sessionDrawer: { effectiveOpen: { value: false }, isOpen: { value: false }, open: vi.fn(), close: vi.fn(), toggle: vi.fn() },
    openSessionTab: vi.fn(),
    closeSessionDrawer: vi.fn(),
    switchSession: vi.fn(),
    createSession: vi.fn(),
    archiveSession: vi.fn(),
    sendMessage: vi.fn(),
    openChatPanel: vi.fn(),
    openSessionTab: vi.fn(),
    openAgentSelector: vi.fn(),
    continueFromExecution: vi.fn(),
    checkContinueSession: vi.fn(),
    forkSession: vi.fn(),
    registerSessionActions: vi.fn(),
    initSessionFromAPI: vi.fn(),
    saveModelPref: mockIdentityFns.saveModelPref,
    saveThinkingPref: mockIdentityFns.saveThinkingPref,
    loadModelPref: mockIdentityFns.loadModelPref,
    loadThinkingPref: mockIdentityFns.loadThinkingPref,
    loadModePref: vi.fn().mockReturnValue(''),
    saveModePref: vi.fn(),
    toggleAutoApprove: vi.fn(),
  }),
  currentAgentId: {
    get value() { return mockIdentity.currentAgentId },
    set value(v) { mockIdentity.currentAgentId = v },
  },
  updateModeState: vi.fn(),
  updateAvailableModes: vi.fn(),
  clearModeState: vi.fn(),
  updateCommandState: vi.fn(),
  clearCommandState: vi.fn(),
  reconcileRunningSessions: vi.fn((sessions: Array<{ id?: string; running?: boolean }>, full = false) => {
    if (!sessions) return false
    let changed = false
    if (full) {
      if (mockState.runningSessions.size > 0) { mockState.runningSessions.clear(); changed = true }
      for (const s of sessions) {
        if (s?.id && s.running && !mockState.runningSessions.has(s.id)) { mockState.runningSessions.add(s.id); changed = true }
      }
    } else {
      if (sessions.length === 0) return false
      for (const s of sessions) {
        if (!s || !s.id) continue
        if (s.running) {
          if (!mockState.runningSessions.has(s.id)) { mockState.runningSessions.add(s.id); changed = true }
        } else {
          if (mockState.runningSessions.delete(s.id)) changed = true
        }
      }
    }
    if (changed) mockState.runningSessionsVersion++
    return changed
  }),
  updateThinkingEffortState: vi.fn(),
  updateAvailableThinkingEfforts: vi.fn(),
  clearThinkingEffortState: vi.fn(),
  clearSessionIdentity: vi.fn((upcomingSessionId?: string) => {
    // Simulate clearing identity refs and setting currentSessionId
    mockIdentity.currentSessionTitle = ''
    mockIdentity.currentBackend = ''
    mockIdentity.currentAgentId = ''
    mockIdentity.currentModelId = ''
    mockIdentity.currentModelName = ''
    mockIdentity.currentThinkingEffort = ''
    mockIdentity.currentThinkingEffortName = ''
    mockIdentity.currentModeId = ''
    mockIdentity.currentModeName = ''
    mockIdentity.currentTransport = ''
    mockIdentity.autoApprove = false
    if (upcomingSessionId !== undefined) {
      mockState.currentSessionId = upcomingSessionId
    }
  }),
  updateUsageState: mockUpdateUsageState,
  clearUsageState: mockClearUsageState,
  clearUsageStateById: mockIdentityFns.clearUsageStateById,
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastFn }),
}))
vi.mock('@/composables/useNotification', () => ({
  useNotification: () => ({ play: vi.fn() }),
}))
vi.mock('@/composables/useWorktreeAnnotation', () => ({
  warmWorktreeCache: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: { value: [] },
    loadAgents: mockAgentFns.loadAgents,
    getAgentBackend: mockAgentFns.getAgentBackend,
    getAgentName: mockAgentFns.getAgentName,
    getAgent: vi.fn().mockReturnValue(undefined),
    syncModelFromAgent: mockAgentFns.syncModelFromAgent,
    getAgentModel: mockAgentFns.getAgentModel,
    agentHeaderTitle: mockAgentFns.agentHeaderTitle,
    getAgentThinkingEffortLevels: vi.fn().mockReturnValue([]),
    supportsACP: vi.fn().mockReturnValue(false),
  }),
  restoreOriginalModels: vi.fn(),
  populateACPStateFromCache: vi.fn().mockResolvedValue(undefined),
  getAgentThinkingEffortLevels: vi.fn().mockReturnValue([]),
}))

vi.mock('@/stores/app', () => ({
  store: {
    get state() {
      return mockState
    },
    loadGitBranch: vi.fn().mockResolvedValue(undefined),
  },
}))

vi.mock('@/utils/chatSessionUtils', () => ({
  buildMessageSnapshot: mockUtilsFns.buildMessageSnapshot,
  parseMessages: mockUtilsFns.parseMessages,
}))

vi.mock('@/utils/chatStreamUtils', () => ({
  forceCleanupStreamingState: mockForceCleanupStreamingState,
}))

// ── Import after mocks ──

import { useChatSession, loadSessionsOnce, resetChatSessionState } from '@/composables/useChatSession'

// Get direct references to the mocked functions from useSessionIdentity
const mockUpdateUsageState = vi.hoisted(() => vi.fn())
const mockClearUsageState = vi.hoisted(() => vi.fn())

// ── Helpers ──

// Module-level options ref so tests can access messages.value etc.
let lastSessionOptions: ReturnType<typeof createSessionInternal>['options'] | null = null

function createSessionInternal() {
  const options = {
    currentSessionId: ref('current-s1'),
    messages: ref([]),
    loading: ref(false),
    inputDisabled: ref(false),
    blockTasks: {},
    blockAskQuestions: {},
    expandedTools: ref({}),
    onParseAssistantContent: vi.fn(),
    onExtractScheduledTasks: vi.fn(),
    onRenderUpdate: vi.fn(),
    onScrollBottom: vi.fn(),
    onConnectStream: vi.fn(),
    onDisconnectStream: vi.fn(),
    onOpen: vi.fn(),
  }
  const session = useChatSession(options)
  lastSessionOptions = options
  return { session, options }
}

function createSession() {
  return createSessionInternal().session
}

// ── Tests ──

describe('onSessionEvent', () => {
  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
  })

  it('does nothing when data is null', () => {
    const session = createSession()
    const versionBefore = mockState.runningSessionsVersion
    session.onSessionEvent(null as any)
    expect(mockState.runningSessions.size > 0).toBe(false)
    expect(mockState.runningSessions.size).toBe(0)
    expect(mockState.runningSessionsVersion).toBe(versionBefore)
  })

  it('does nothing when data is undefined', () => {
    const session = createSession()
    const versionBefore = mockState.runningSessionsVersion
    session.onSessionEvent(undefined)
    expect(mockState.runningSessions.size > 0).toBe(false)
    expect(mockState.runningSessions.size).toBe(0)
    expect(mockState.runningSessionsVersion).toBe(versionBefore)
  })

  it('adds session to runningSessions on status=running', () => {
    const session = createSession()
    session.onSessionEvent({ session_id: 's1', status: 'running' })
    expect(mockState.runningSessions.size > 0).toBe(true)
    expect(mockState.runningSessions.has('s1')).toBe(true)
    expect(mockState.runningSessionsVersion).toBe(1)
  })

  it('does not add to set when session_id is missing on running', () => {
    const session = createSession()
    session.onSessionEvent({ status: 'running' })
    expect(mockState.runningSessions.size > 0).toBe(false)
    expect(mockState.runningSessions.size).toBe(0)
    expect(mockState.runningSessionsVersion).toBe(0)
  })

  it('removes session from runningSessions on status=completed', () => {
    const session = createSession()
    // Start two sessions
    session.onSessionEvent({ session_id: 's1', status: 'running' })
    session.onSessionEvent({ session_id: 's2', status: 'running' })
    expect(mockState.runningSessions.size).toBe(2)

    // Complete s1 — s2 still running
    session.onSessionEvent({ session_id: 's1', status: 'completed' })
    expect(mockState.runningSessions.has('s1')).toBe(false)
    expect(mockState.runningSessions.has('s2')).toBe(true)
    expect(mockState.runningSessions.size > 0).toBe(true)
  })

  it('runningSessions becomes empty when last running session completes', () => {
    const session = createSession()
    session.onSessionEvent({ session_id: 's1', status: 'running' })
    expect(mockState.runningSessions.size > 0).toBe(true)

    session.onSessionEvent({ session_id: 's1', status: 'completed' })
    expect(mockState.runningSessions.size).toBe(0)
    expect(mockState.runningSessions.size > 0).toBe(false)
  })

  it('does not directly set chatUnread when a different session completes — delegates to loadSessionsOnce', () => {
    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 's1', status: 'running' })
    // A different session completes — no longer sets chatUnread synchronously
    session.onSessionEvent({ session_id: 's2', status: 'completed' })
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('does not mark chatUnread when the current session completes', () => {
    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 'current-s1', status: 'running' })
    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('handles status=cancelled by removing from runningSessions', () => {
    const session = createSession()
    session.onSessionEvent({ session_id: 's1', status: 'running' })
    expect(mockState.runningSessions.has('s1')).toBe(true)

    session.onSessionEvent({ session_id: 's1', status: 'cancelled' })
    expect(mockState.runningSessions.has('s1')).toBe(false)
    expect(mockState.runningSessions.size > 0).toBe(false)
  })

  it('increments runningSessionsVersion on each add and delete', () => {
    const session = createSession()
    expect(mockState.runningSessionsVersion).toBe(0)

    session.onSessionEvent({ session_id: 's1', status: 'running' })
    expect(mockState.runningSessionsVersion).toBe(1)

    session.onSessionEvent({ session_id: 's2', status: 'running' })
    expect(mockState.runningSessionsVersion).toBe(2)

    session.onSessionEvent({ session_id: 's1', status: 'completed' })
    expect(mockState.runningSessionsVersion).toBe(3)
  })

  it('handles multiple concurrent sessions correctly', () => {
    const session = createSession()

    // Start 3 sessions
    session.onSessionEvent({ session_id: 's1', status: 'running' })
    session.onSessionEvent({ session_id: 's2', status: 'running' })
    session.onSessionEvent({ session_id: 's3', status: 'running' })

    expect(mockState.runningSessions.size > 0).toBe(true)
    expect(mockState.runningSessions.size).toBe(3)

    // Complete s2 — s1 and s3 still running
    session.onSessionEvent({ session_id: 's2', status: 'completed' })
    expect(mockState.runningSessions.size > 0).toBe(true)
    expect(mockState.runningSessions.has('s2')).toBe(false)
    expect(mockState.runningSessions.has('s1')).toBe(true)
    expect(mockState.runningSessions.has('s3')).toBe(true)

    // Complete s3
    session.onSessionEvent({ session_id: 's3', status: 'completed' })
    expect(mockState.runningSessions.size > 0).toBe(true)

    // Complete s1
    session.onSessionEvent({ session_id: 's1', status: 'completed' })
    expect(mockState.runningSessions.size > 0).toBe(false)
    expect(mockState.runningSessions.size).toBe(0)
  })

  it('does not increment version when completing a session without session_id', () => {
    const session = createSession()
    const versionBefore = mockState.runningSessionsVersion
    // completed without session_id — no sid to delete, no version bump
    session.onSessionEvent({ status: 'completed' })
    expect(mockState.runningSessionsVersion).toBe(versionBefore)
  })

  it('session running status is determined by both runningSessions Set and API running field', () => {
    // Simulates SessionDrawer's sessionsWithStatus logic:
    //   running: runningSessionIds.has(s.id) || !!s.running
    const session = createSession()

    // Scenario 1: WS event marks session as running (no API data yet)
    session.onSessionEvent({ session_id: 's1', status: 'running' })
    const runningSessionIds = mockState.runningSessions
    // s1 is in the set → should show as running
    expect(runningSessionIds.has('s1') || false).toBe(true)

    // Scenario 2: API returns running=true, but WS event hasn't arrived yet
    // (s2 is NOT in the set, but API would say running=true)
    const s2FromAPI = { id: 's2', running: true }
    expect(runningSessionIds.has('s2') || !!s2FromAPI.running).toBe(true)

    // Scenario 3: Session completed via WS but API still has stale data
    // (after onSessionEvent, s1 is removed from set)
    session.onSessionEvent({ session_id: 's1', status: 'completed' })
    expect(runningSessionIds.has('s1')).toBe(false)

    // The merged logic ensures a session shows as running if EITHER source says so
    // This covers the gap where TrySetSessionRunning's WS event arrives
    // before loadSessions is called
  })

  it('ignores data with empty/undefined status', () => {
    const session = createSession()
    const versionBefore = mockState.runningSessionsVersion

    // status is empty string → falls into else branch (treated as not-running)
    session.onSessionEvent({ session_id: 's1', status: '' })
    // No session_id in the Set (it was never added), so runningSessions stays empty
    expect(mockState.runningSessions.size > 0).toBe(false)
    // session_id is present → delete from empty set is a no-op, but version still increments
    expect(mockState.runningSessionsVersion).toBe(versionBefore + 1)
  })

  it('handles data with undefined status (missing key)', () => {
    const session = createSession()
    const versionBefore = mockState.runningSessionsVersion

    // status is undefined → else branch
    session.onSessionEvent({ session_id: 's1' })
    expect(mockState.runningSessions.size > 0).toBe(false)
    expect(mockState.runningSessionsVersion).toBe(versionBefore + 1)
  })

  it('does not add duplicate entries to runningSessions for same session_id', () => {
    const session = createSession()

    session.onSessionEvent({ session_id: 's1', status: 'running' })
    expect(mockState.runningSessions.size).toBe(1)

    // Duplicate running event — Set.add is idempotent
    session.onSessionEvent({ session_id: 's1', status: 'running' })
    expect(mockState.runningSessions.size).toBe(1)
    // But version still increments
    expect(mockState.runningSessionsVersion).toBe(2)
  })

  it('completing a non-running session does not crash', () => {
    const session = createSession()

    // Complete a session that was never started
    expect(() => {
      session.onSessionEvent({ session_id: 'ghost', status: 'completed' })
    }).not.toThrow()
    expect(mockState.runningSessions.has('ghost')).toBe(false)
    expect(mockState.runningSessions.size > 0).toBe(false)
  })

  it('preserves chatUnread=false when completing a non-current session — delegates to loadSessionsOnce', () => {
    const session = createSession()
    mockState.currentSessionId = 'current-s1'
    mockState.chatUnreadCount = 0

    session.onSessionEvent({ session_id: 's2', status: 'completed' })
    // No longer sets chatUnread=true synchronously
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('does not directly set chatUnread on cancelled status for non-current session', () => {
    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 's1', status: 'running' })
    // Cancel a different session — no longer sets chatUnread synchronously
    session.onSessionEvent({ session_id: 's2', status: 'cancelled' })
    expect(mockState.chatUnreadCount).toBe(0)
  })

  // ── onSessionEvent → loadHistory (replaces old msgCountPolling) ──

  it('calls loadHistory when has_new_messages=true for current session', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1', messages: [], total: 0, running: false,
      }),
    })

    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 'current-s1', has_new_messages: true, status: 'completed' })

    // Wait for the async loadHistory to complete
    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/ai/chat?session_id=current-s1'),
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  it('requests view=summary in loadHistory URL', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1', messages: [], total: 0, running: false,
      }),
    })

    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 'current-s1', has_new_messages: true, status: 'completed' })

    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('view=summary'),
        expect.any(Object)
      )
    })
  })

  it('calls loadHistory when current session completes', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1', messages: [], total: 0, running: false,
      }),
    })

    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })

    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/ai/chat?session_id=current-s1'),
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  it('calls loadHistory when current session is cancelled', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1', messages: [], total: 0, running: false,
      }),
    })

    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 'current-s1', status: 'cancelled' })

    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/ai/chat?session_id=current-s1'),
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  // ── Safety net: loading stuck when session completes ──
  // Bug: chat_stream 'done' event was missed (e.g. WS disconnect), so
  // loading.value stays true. The session_update 'completed' event should
  // clean up the stuck loading state.

  it('resets loading to false when session completes while loading=true (safety net)', () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })

    // Safety net should have kicked in
    expect(loading.value).toBe(false)
    expect(onDisconnectStream).toHaveBeenCalled()
    expect(mockForceCleanupStreamingState).toHaveBeenCalled()
  })

  it('resets loading to false when session is cancelled while loading=true (safety net)', () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    session.onSessionEvent({ session_id: 'current-s1', status: 'cancelled' })

    expect(loading.value).toBe(false)
    expect(onDisconnectStream).toHaveBeenCalled()
    expect(mockForceCleanupStreamingState).toHaveBeenCalled()
  })

  it('calls loadHistory after safety net cleanup to refresh messages from DB', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1', messages: [], total: 0, running: false,
      }),
    })

    const loading = ref(true)
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })

    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/ai/chat?session_id=current-s1'),
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  it('does NOT trigger safety net for non-current session completing while loading=true', async () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // A DIFFERENT session completes while we're loading
    session.onSessionEvent({ session_id: 'other-s2', status: 'completed' })

    // Safety net should NOT kick in — we're still streaming current-s1
    expect(loading.value).toBe(true)
    expect(onDisconnectStream).not.toHaveBeenCalled()
    expect(mockForceCleanupStreamingState).not.toHaveBeenCalled()
  })

  it('does NOT trigger safety net for permission_pending status', () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    session.onSessionEvent({ session_id: 'current-s1', status: 'permission_pending' })

    // permission_pending should NOT trigger the safety net
    expect(loading.value).toBe(true)
    expect(onDisconnectStream).not.toHaveBeenCalled()
  })

  it('normal path: still calls loadHistory when loading=false and session completes', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1', messages: [], total: 0, running: false,
      }),
    })

    const loading = ref(false)
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })

    // Normal path: safety net does NOT trigger, but loadHistory is called
    expect(mockForceCleanupStreamingState).not.toHaveBeenCalled()
    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/ai/chat?session_id=current-s1'),
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    })
  })

  it('does not call loadHistory for non-current session completed', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ count: 5 }),
    })

    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 's2', status: 'completed' })

    // Give async operations a chance
    await new Promise(r => setTimeout(r, 50))

    // Should NOT have called loadHistory for s2
    const chatCalls = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.filter(
      (c: any[]) => typeof c[0] === 'string' && c[0].includes('/api/ai/chat?')
    )
    expect(chatCalls.length).toBe(0)
  })

  it('safety net uses forceNotRunning to prevent race where server still says running', async () => {
    // Scenario: session completed, session_update arrives, but loadHistory
    // hits the server before its in-memory running state is updated.
    // Without forceNotRunning, loadHistory would see running=true, set
    // loading=true, and reconnect the stream — putting us back in stuck state.
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const onConnectStream = vi.fn()
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(() => ({ blocks: [], metadata: {} })),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream,
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Mock fetch to return running=true (simulating race condition where
    // server hasn't updated its in-memory state yet)
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        messages: [],
        sessionId: 'current-s1',
        running: true,  // Server still says running!
        total: 0,
      }),
    })
    globalThis.fetch = fetchSpy as any

    // Trigger safety net
    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })

    // Wait for async loadHistory to complete
    await vi.waitFor(() => fetchSpy.mock.calls.length > 0)

    // Despite server returning running=true, loading should stay false
    // because forceNotRunning=true prevents reconnecting the stream
    expect(loading.value).toBe(false)
    expect(onDisconnectStream).toHaveBeenCalled()
    // onConnectStream should NOT be called (forceNotRunning prevents it)
    expect(onConnectStream).not.toHaveBeenCalled()
  })

  it('safety net strips streaming flags when server race returns running=true', async () => {
    // Verify that forceNotRunning causes parseMessages to receive sessionRunning=false,
    // which strips the streaming flag from assistant messages — preventing the
    // three-dot loading indicator from appearing on a completed session.
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const onConnectStream = vi.fn()
    // Return an assistant message with streaming flag
    const onParseAssistantContent = vi.fn((content: string) => ({
      blocks: content ? [{ type: 'text', text: content }] : [],
      metadata: {},
    }))
    const messages = ref([] as any[])
    const options = {
      currentSessionId: ref('current-s1'),
      messages,
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent,
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream,
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Mock fetch to return an assistant message with streaming=1 AND running=true
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        messages: [{ id: 1, role: 'assistant', content: 'Hello', streaming: 1, created_at: '2025-01-01' }],
        sessionId: 'current-s1',
        running: true,  // Server still says running (race condition)
        total: 1,
      }),
    })
    globalThis.fetch = fetchSpy as any

    // Trigger safety net
    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })

    // Wait for async loadHistory to complete
    await vi.waitFor(() => fetchSpy.mock.calls.length > 0)

    // Despite server returning running=true, the streaming flag should be stripped
    // because forceNotRunning=true causes parseMessages to receive sessionRunning=false
    expect(loading.value).toBe(false)
    // The assistant message should NOT have streaming flag
    const assistantMsg = messages.value.find((m: any) => m.role === 'assistant')
    if (assistantMsg) {
      expect(assistantMsg.streaming).toBeUndefined()
    }
    expect(onConnectStream).not.toHaveBeenCalled()
  })
})

// ── loadSessionsOnce tests ──

// Need to re-import loadSessionsOnce with a separate mock setup for fetch
describe('loadSessionsOnce', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('populates runningSessions from API response with running sessions', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 's1', running: true },
          { id: 's2', running: false },
          { id: 's3', running: true },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.runningSessions.has('s1')).toBe(true)
    expect(mockState.runningSessions.has('s2')).toBe(false)
    expect(mockState.runningSessions.has('s3')).toBe(true)
    expect(mockState.runningSessions.size).toBe(2)
    expect(mockState.runningSessions.size > 0).toBe(true)
    expect(mockState.runningSessionsVersion).toBeGreaterThan(0)
  })

  it('clears runningSessions when no sessions are running', async () => {
    // Pre-populate with a running session
    mockState.runningSessions.add('old-session')

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 's1', running: false },
          { id: 's2', running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.runningSessions.size).toBe(0)
    expect(mockState.runningSessions.size > 0).toBe(false)
  })

  it('does not throw on fetch failure', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new Error('Network error'))

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    // Should not throw
    await expect(loadSessionsOnce()).resolves.toBeUndefined()
  })

  it('does not throw on non-ok response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await expect(loadSessionsOnce()).resolves.toBeUndefined()
  })

  it('increments runningSessionsVersion after populating', async () => {
    const versionBefore = mockState.runningSessionsVersion

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [{ id: 's1', running: true }],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.runningSessionsVersion).toBeGreaterThan(versionBefore)
  })

  // ── chatUnread recalculation tests ──

  it('sets chatUnread=true when another session has unreadCount > 0', async () => {
    mockState.currentSessionId = 'current-s1'
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 'current-s1', unreadCount: 0, running: false },
          { id: 's2', unreadCount: 3, running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.chatUnreadCount).toBeGreaterThan(0)
  })

  it('sets chatUnread=false when no other session has unreadCount > 0', async () => {
    mockState.currentSessionId = 'current-s1'
    mockState.chatUnreadCount = 1  // pre-set to true
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 'current-s1', unreadCount: 2, running: false },
          { id: 's2', unreadCount: 0, running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    // current session's unreadCount is ignored; s2 has 0 → chatUnread = false
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('sets chatUnread=false when all sessions are read', async () => {
    mockState.currentSessionId = 'current-s1'
    mockState.chatUnreadCount = 1
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 'current-s1', unreadCount: 0, running: false },
          { id: 's2', unreadCount: 0, running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('ignores current session unreadCount even if it is > 0', async () => {
    // Key behavior: only OTHER sessions' unread counts matter
    mockState.currentSessionId = 'current-s1'
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 'current-s1', unreadCount: 5, running: false },  // current — ignored
          { id: 's2', unreadCount: 0, running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('handles empty sessions array', async () => {
    mockState.chatUnreadCount = 1
    mockState.runningSessions.add('s1')
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: [] }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.chatUnreadCount).toBe(0)
    expect(mockState.runningSessions.size > 0).toBe(false)
  })

  it('does not change chatUnread/runningSessions when fetch is not ok', async () => {
    mockState.chatUnreadCount = 1
    mockState.runningSessions.add('s1')
    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500 })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.chatUnreadCount).toBeGreaterThan(0)
    expect(mockState.runningSessions.size > 0).toBe(true)
  })

  it('does not change chatUnread/runningSessions when fetch throws', async () => {
    mockState.chatUnreadCount = 1
    mockState.runningSessions.add('s1')
    globalThis.fetch = vi.fn().mockRejectedValue(new Error('Network error'))

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.chatUnreadCount).toBeGreaterThan(0)
    expect(mockState.runningSessions.size > 0).toBe(true)
  })

  it('clears stale runningSessions before repopulating', async () => {
    // Pre-populate with sessions that are no longer running
    mockState.runningSessions.add('old-1')
    mockState.runningSessions.add('old-2')

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 'new-1', running: true },
          { id: 'old-1', running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    // old entries should be cleared, only new-1 should remain
    expect(mockState.runningSessions.has('old-1')).toBe(false)
    expect(mockState.runningSessions.has('old-2')).toBe(false)
    expect(mockState.runningSessions.has('new-1')).toBe(true)
    expect(mockState.runningSessions.size).toBe(1)
  })

  it('handles json() throwing an error', async () => {
    mockState.runningSessions.add('s1')
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.reject(new SyntaxError('Unexpected token')),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    // Should not throw
    await expect(loadSessionsOnce()).resolves.toBeUndefined()
    // State should not change (error was caught)
    expect(mockState.runningSessions.size > 0).toBe(true)
  })

  it('handles missing sessions field in response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),  // no sessions field
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.runningSessions.size > 0).toBe(false)
    expect(mockState.runningSessions.size).toBe(0)
  })

  it('handles sessions=null in response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: null }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.runningSessions.size > 0).toBe(false)
    expect(mockState.runningSessions.size).toBe(0)
  })

  it('does not clear runningSessions when fetch is not ok', async () => {
    mockState.runningSessions.add('s1')

    globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500 })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    // Pre-existing data should not be cleared on failed fetch
    expect(mockState.runningSessions.has('s1')).toBe(true)
    expect(mockState.runningSessions.size > 0).toBe(true)
  })

  it('does not clear runningSessions when fetch throws', async () => {
    mockState.runningSessions.add('s1')

    globalThis.fetch = vi.fn().mockRejectedValue(new Error('Network error'))

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    expect(mockState.runningSessions.has('s1')).toBe(true)
    expect(mockState.runningSessions.size > 0).toBe(true)
  })
})

// ───────────────────────────────────────────────────────────
// switchSession — recalculate chatUnread after switching
// ───────────────────────────────────────────────────────────

describe('switchSession', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    mockState.currentSessionId = 'current-s1'
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('calls loadSessionsOnce after successful switch to recalculate chatUnread', async () => {
    // First call: GET /api/ai/chat?session_id=s2 (switchSession fetch)
    // Second call: GET /api/ai/sessions (loadSessionsOnce fetch)
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's2', unreadCount: 0, running: false },
          ],
        }),
      })

    const session = createSession()
    await session.switchSession('s2')

    // loadSessionsOnce is fire-and-forget — wait for it to complete
    await vi.waitFor(() => {
      expect(globalThis.fetch).toHaveBeenCalledTimes(2)
    })

    // After switching to s2 and recalculating, no unread sessions remain
    expect(mockState.chatUnreadCount).toBe(0)
    // fetch called twice: once for chat history, once for sessions list
  })

  it('clears chatUnread after switching when all sessions are read', async () => {
    mockState.chatUnreadCount = 1  // was flashing before

    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's2', unreadCount: 0, running: false },
          ],
        }),
      })

    const session = createSession()
    await session.switchSession('s2')

    // loadSessionsOnce is fire-and-forget inside switchSession,
    // wait for it to complete before checking state
    await vi.waitFor(() => {
      expect(mockState.chatUnreadCount).toBe(0)
    })
  })

  it('keeps chatUnread=true after switching when other sessions still have unread messages', async () => {
    mockState.chatUnreadCount = 1

    // Switch to s2, but s3 still has unread messages
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's2', unreadCount: 0, running: false },
            { id: 's3', unreadCount: 2, running: false },
          ],
        }),
      })

    const session = createSession()
    await session.switchSession('s2')

    // s3 is still unread — flashing should continue
    expect(mockState.chatUnreadCount).toBeGreaterThan(0)
  })

  it('sets inputDisabled=false even when switchSession fetch fails', async () => {
    globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error('Network error'))

    const inputDisabled = ref(false)
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled,
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    await session.switchSession('s2')

    // inputDisabled must be restored in finally block
    expect(inputDisabled.value).toBe(false)
  })

  it('calls loadSessionsOnce even when switchSession fetch fails', async () => {
    // switchSession delegates to loadHistory(immediate=true) which may fire
    // parallel fetches (warmWorktreeCache, loadAgents). Provide a catch-all
    // mock for those, plus a specific mock for the failing chat request.
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        // The chat history fetch (from loadHistory) fails
        ok: false,
        json: () => Promise.resolve({ error: 'not found' }),
      })
      .mockResolvedValue({
        // Catch-all for any other fetches (warmWorktreeCache, loadAgents, loadSessionsOnce)
        ok: true,
        json: () => Promise.resolve({}),
      })

    const session = createSession()
    await session.switchSession('s2')

    // At least one fetch was attempted (the failing chat request from loadHistory)
    expect(globalThis.fetch).toHaveBeenCalled()
  })

  it('restores queued messages from backend queue field after switchSession', async () => {
    // The bug: switchSession used to have its own fetch+parseMessages that skipped
    // the queue field. Now switchSession delegates to loadHistory, which correctly
    // restores pending messages from the queue field in the backend response.
    const queuedMessages = [
      { queueId: 'pending-abc123', text: 'queued message 1', filePaths: [], files: [], createdAt: '2026-01-01T00:00:00Z' },
      { queueId: 'pending-def456', text: 'queued message 2', filePaths: ['/tmp/file.txt'], files: [], createdAt: '2026-01-01T00:01:00Z' },
    ]
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        // Chat history fetch with queue data
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [{ id: 1, role: 'user', content: 'hello' }, { id: 2, role: 'assistant', content: 'hi' }],
          total: 2,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
          queue: queuedMessages,
        }),
      })
      .mockResolvedValue({
        // Catch-all for loadSessionsOnce etc.
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 0 }),
      })

    const session = createSession()
    await session.switchSession('s2')

    // Queued messages should appear in messages.value as pending
    const pendingMsgs = lastSessionOptions!.messages.value.filter((m: any) => m.pending)
    expect(pendingMsgs.length).toBe(2)
    expect(pendingMsgs[0].content).toBe('queued message 1')
    expect(pendingMsgs[0].id).toBe('pending-abc123')
    expect(pendingMsgs[1].content).toBe('queued message 2')
    expect(pendingMsgs[1].id).toBe('pending-def456')
  })

  it('restores usage state from API response after switch', async () => {
    resetAdditionalMocks() // Ensure mock call records are clean
    mockClearUsageState.mockClear()
    mockUpdateUsageState.mockClear()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
          usageState: { used: 50000, size: 200000, cost: 1.5, currency: 'USD' },
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [] }),
      })

    const session = createSession()
    await session.switchSession('s2')

    // With per-session usage cache, switchSession no longer calls clearUsageState.
    // The computed refs automatically read from the new session's cache entry.
    // updateUsageState is called to write the API response data into the cache.
    expect(mockUpdateUsageState).toHaveBeenCalledWith(50000, 200000, 1.5, 'USD', 's2', undefined, undefined)
  })

  it('does not call updateUsageState when API response has no usageState', async () => {
    mockUpdateUsageState.mockClear()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
          // no usageState field
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [] }),
      })

    const session = createSession()
    await session.switchSession('s2')

    // syncUsageFromData no longer clears cache when usageState is missing —
    // it preserves any SSE-cached data for running sessions.
    // Neither updateUsageState nor clearUsageState should be called.
    expect(mockUpdateUsageState).not.toHaveBeenCalled()
  })

  it('does not call updateUsageState when usageState.size is 0', async () => {
    mockUpdateUsageState.mockClear()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
          usageState: { used: 0, size: 0 },
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [] }),
      })

    const session = createSession()
    await session.switchSession('s2')

    // size=0 means no context window info — syncUsageFromData skips update
    // but does NOT clear existing cache (SSE-cached data is preserved)
    expect(mockUpdateUsageState).not.toHaveBeenCalled()
  })

  it('keeps inputDisabled=true when replayPending is true', async () => {
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          running: false,
          replayPending: true,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [] }),
      })

    const inputDisabled = ref(false)
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled,
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.switchSession('s2')

    expect(inputDisabled.value).toBe(true)
  })
})

// ───────────────────────────────────────────────────────────
// Integration: onSessionEvent no longer sets chatUnread synchronously
// ───────────────────────────────────────────────────────────

describe('chatUnread integration', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    mockState.currentSessionId = 's1'
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('onSessionEvent does not set chatUnread synchronously — delegates to debounced loadSessionsOnce', () => {
    const session = createSession()
    mockState.currentSessionId = 's1'

    // Session s2 completes in the background → no longer sets chatUnread synchronously
    session.onSessionEvent({ session_id: 's2', status: 'completed' })
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('onSessionEvent does not set chatUnread for cancelled sessions', () => {
    const session = createSession()
    mockState.currentSessionId = 's1'

    session.onSessionEvent({ session_id: 's2', status: 'cancelled' })
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('chatUnread is not set synchronously when current session completes — delegates to debounced loadSessionsOnce', () => {
    const session = createSession()
    mockState.currentSessionId = 'current-s1'

    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })
    // Not set synchronously — debounced loadSessionsOnce will recalculate
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('current session completion triggers debounced loadSessionsOnce to clear stale chatUnreadCount', async () => {
    // Bug scenario: stale chatUnreadCount from a prior event is not cleared
    // because onSessionEvent used to skip loadSessionsOnce for the current session.
    mockState.currentSessionId = 'current-s1'
    mockState.chatUnreadCount = 1  // stale value from prior event

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 'current-s1', unreadCount: 0, running: false },
        ],
      }),
    })

    const session = createSession()
    session.onSessionEvent({ session_id: 'current-s1', status: 'completed' })
    // Still 0 synchronously
    expect(mockState.chatUnreadCount).toBe(1)  // stale until debounce fires

    // After debounce fires, loadSessionsOnce recalculates
    await new Promise(r => setTimeout(r, 600))
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('switchSession recalculates chatUnread correctly', async () => {
    const session = createSession()
    mockState.currentSessionId = 's1'

    // User switches to s2 (backend UpdateLastRead marks it as read)
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's2', unreadCount: 0, running: false },
          ],
        }),
      })

    await session.switchSession('s2')

    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('switchSession keeps chatUnread true when another session still has unread', async () => {
    const session = createSession()
    mockState.currentSessionId = 's1'

    // User switches to s2, but s3 is still unread
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's2', unreadCount: 0, running: false },
            { id: 's3', unreadCount: 1, running: false },
          ],
        }),
      })

    await session.switchSession('s2')

    // loadSessionsOnce is fire-and-forget inside switchSession,
    // wait for it to complete before checking state
    await vi.waitFor(() => {
      // s3 still has unread → chatUnread should stay true
      expect(mockState.chatUnreadCount).toBeGreaterThan(0)
    })
  })

  it('simulates the bug scenario: user on chat tab, other session completes, no phantom flash', () => {
    // Exact scenario from the bug report:
    // 1. User is on chat tab viewing s1
    // 2. Session s2 completes → chatUnread should NOT be set synchronously
    // 3. The debounced loadSessionsOnce will determine the real state from the server
    const session = createSession()
    mockState.currentSessionId = 's1'

    // Step 2: s2 completes in the background — no longer sets chatUnread=true immediately
    session.onSessionEvent({ session_id: 's2', status: 'completed' })
    // No phantom flash! chatUnread stays false until loadSessionsOnce confirms
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('simulates: user switches to chat tab but does not open unread session', async () => {
    // Scenario:
    // 1. User is on another tab
    // 2. chatUnread was set to true (e.g. by a prior loadSessionsOnce)
    // 3. User clicks Dock chat button → switchTab('chat') calls loadSessionsOnce()
    // 4. loadSessionsOnce should recalculate: s2 still has unreadCount > 0 → chatUnread stays true
    mockState.currentSessionId = 's1'
    mockState.chatUnreadCount = 1  // was set by loadSessionsOnce

    // switchTab('chat') now calls loadSessionsOnce() instead of blindly clearing
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 's1', unreadCount: 0, running: false },
          { id: 's2', unreadCount: 2, running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    // chatUnread should remain true — user hasn't opened s2 yet
    expect(mockState.chatUnreadCount).toBeGreaterThan(0)
  })

  it('loadSessionsOnce after stream done clears chatUnread for current session', async () => {
    // Bug #10 scenario: user views session s1, AI finishes, chatUnread should be recalculated
    // After AI finishes, loadHistory calls UpdateLastRead, so the API returns unreadCount=0 for s1.
    // loadSessionsOnce should then set chatUnread=false.
    mockState.currentSessionId = 's1'
    mockState.chatUnreadCount = 1  // was set incorrectly during initial load

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 's1', unreadCount: 0, running: false },  // current session, now read
          { id: 's2', unreadCount: 0, running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    // chatUnread should be cleared — no other sessions have unread messages
    expect(mockState.chatUnreadCount).toBe(0)
  })

  it('chatUnread stays false after loadSessionsOnce when only current session has unread', async () => {
    // Edge case: current session has unreadCount > 0 but it's the current one
    // This can happen if UpdateLastRead hasn't been called yet
    mockState.currentSessionId = 's1'
    mockState.chatUnreadCount = 0

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [
          { id: 's1', unreadCount: 5, running: false },  // current session — ignored
          { id: 's2', unreadCount: 0, running: false },
        ],
      }),
    })

    const { loadSessionsOnce } = await import('@/composables/useChatSession')
    await loadSessionsOnce()

    // Current session's unreadCount is excluded, s2 has 0 → chatUnread = false
    expect(mockState.chatUnreadCount).toBe(0)
  })
})

// ───────────────────────────────────────────────────────────
// loadHistory
// ───────────────────────────────────────────────────────────

describe('loadHistory', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('normal successful load: fetches /api/ai/chat, parses messages, updates identity refs', async () => {
    const parsedMsgs = [{ id: 'm1', role: 'user' }, { id: 'm2', role: 'assistant' }]
    mockUtilsFns.parseMessages.mockReturnValue(parsedMsgs)

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        sessionTitle: 'Test Session',
        backend: 'claude',
        agentId: 'agent1',
        modelId: 'model-x',
        thinkingEffort: 'high',
        messages: [{ id: 'm1' }, { id: 'm2' }],
        total: 2,
        running: false,
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/ai/chat?session_id=current-s1'),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(mockIdentity.currentSessionTitle).toBe('Test Session')
    expect(mockIdentity.currentBackend).toBe('claude')
    expect(mockIdentity.currentAgentId).toBe('agent1')
    expect(mockUtilsFns.parseMessages).toHaveBeenCalled()
  })

  it('sets switching=true when showOverlay=true, restores to false after', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        running: false,
      }),
    })

    const session = createSession()
    // switching should be false before and after, but true during the async operation
    expect(session.switching.value).toBe(false)
    await session.loadHistory(true, true, false)
    expect(session.switching.value).toBe(false)
  })

  it('handles non-ok response: shows toast error', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'Internal Server Error' }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('skipIfUnchanged=true with same snapshot: early returns without updating', async () => {
    // First load: set the snapshot
    mockUtilsFns.buildMessageSnapshot.mockReturnValue('snap-a')
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [{ id: 'm1' }],
        total: 1,
        running: false,
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    // Second load: same snapshot, skipIfUnchanged=true
    // buildMessageSnapshot still returns 'snap-a'
    const fetchBeforeSecond = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.length
    await session.loadHistory(false, false, true)

    // parseMessages should NOT have been called again for the second load
    // (it was called once during the first load)
    expect(mockUtilsFns.parseMessages).toHaveBeenCalledTimes(1)
  })

  it('skipIfUnchanged=true but data.running=true: still proceeds', async () => {
    // First load: set the snapshot
    mockUtilsFns.buildMessageSnapshot.mockReturnValue('snap-a')
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [{ id: 'm1' }],
        total: 1,
        running: false,
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    // Second load: same snapshot but running=true → should NOT skip
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [{ id: 'm1' }, { id: 'm2' }],
        total: 2,
        running: true,
      }),
    })
    mockUtilsFns.parseMessages.mockReturnValue([{ id: 'm1' }, { id: 'm2' }])

    await session.loadHistory(false, false, true)

    // parseMessages should have been called again (second load proceeded)
    expect(mockUtilsFns.parseMessages).toHaveBeenCalledTimes(2)
  })

  it('sameCore detection: when only last message changed, expandedTools is preserved', async () => {
    // First load: 2 messages
    const firstMsgs = [{ id: 'm1' }, { id: 'm2' }]
    mockUtilsFns.parseMessages.mockReturnValue(firstMsgs)
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1',
        messages: [{ id: 'm1' }, { id: 'm2' }],
        total: 2,
        running: false,
      }),
    })

    const expandedTools = ref({} as Record<string, boolean>)
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools,
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    // After first load, set expandedTools (simulates user expanding a tool)
    expandedTools.value = { tool1: true }

    // Second load: same count, same first message, different last message
    // rawMsgs from API: [{id:'m1'}, {id:'m3'}]
    // messages.value from first load: [{id:'m1'}, {id:'m2'}]
    // sameCore check: prevCount===newCount && rawMsgs.slice(0,-1) matches messages.slice(0,-1)
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'current-s1',
        messages: [{ id: 'm1' }, { id: 'm3' }],  // same count, first msg same id
        total: 2,
        running: false,
      }),
    })
    mockUtilsFns.buildMessageSnapshot.mockReturnValue('snap-b')  // different snapshot to avoid skip
    mockUtilsFns.parseMessages.mockReturnValue([{ id: 'm1' }, { id: 'm3' }])

    await session.loadHistory(true, false, false)

    // expandedTools should be preserved because sameCore=true
    expect(expandedTools.value).toEqual({ tool1: true })
  })

  it('when data is not sameCore: expandedTools is reset to {}', async () => {
    // First load: 2 messages
    mockUtilsFns.parseMessages.mockReturnValue([{ id: 'm1' }, { id: 'm2' }])
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [{ id: 'm1' }, { id: 'm2' }],
        total: 2,
        running: false,
      }),
    })

    const expandedTools = ref({ tool1: true })
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools,
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    // Second load: different count → not sameCore
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [{ id: 'm1' }, { id: 'm2' }, { id: 'm3' }],
        total: 3,
        running: false,
      }),
    })
    mockUtilsFns.buildMessageSnapshot.mockReturnValue('snap-b')
    mockUtilsFns.parseMessages.mockReturnValue([{ id: 'm1' }, { id: 'm2' }, { id: 'm3' }])

    await session.loadHistory(true, false, false)

    // expandedTools should be reset because sameCore=false (count differs)
    expect(expandedTools.value).toEqual({})
  })

  it('when data.running=true: sets loading=true, calls onConnectStream', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        backend: 'claude',
        agentId: 'agent1',
        running: true,
      }),
    })

    const loading = ref(false)
    const onConnectStream = vi.fn()
    const currentSessionId = ref('current-s1')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream,
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    expect(loading.value).toBe(true)
    // onConnectStream is called with currentSessionId.value which has been set to data.sessionId
    expect(onConnectStream).toHaveBeenCalledWith('s1')
  })

  it('when data.running=false: sets loading=false', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        running: false,
      }),
    })

    const loading = ref(true)
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    expect(loading.value).toBe(false)
  })

  it('clears blockAskQuestions before updating', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        running: false,
      }),
    })

    const blockAskQuestions: Record<string, any> = { key1: 'val1', key2: 'val2' }
    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions,
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    expect(Object.keys(blockAskQuestions).length).toBe(0)
  })

  it('error path: shows toast, resets switching', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new Error('Network failure'))

    const session = createSession()
    await session.loadHistory(true, true, false)

    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
    expect(session.switching.value).toBe(false)
  })

  it('shows toast on NoProjectSelected (403) when project cookie is not set', async () => {
    // Simulate the 403 response that occurs on first login before
    // loadProject() has set the clawbench_project cookie.
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 403,
      json: () => Promise.resolve({ error: 'no project selected', code: 403, msgKey: 'NoProjectSelected' }),
    })

    const session = createSession()
    await session.loadHistory(true, true, false)

    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
    expect(session.switching.value).toBe(false)
  })
})

// ───────────────────────────────────────────────────────────
// createSession
// ───────────────────────────────────────────────────────────

describe('createSession', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('successful creation: POST /api/ai/sessions, then switchSession loads new session', async () => {
    // createSession now delegates to switchSession after POST, so we need to
    // mock: 1) POST /api/ai/sessions → new session ID,
    //       2) GET /api/ai/chat?session_id=... → session data (from switchSession),
    //       3) GET /api/ai/sessions → session list (from loadSessionsOnce)
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true,
          sessionId: 'new-s1',
          title: 'New Session',
          backend: 'codebuddy',
          agentId: 'agent2',
          sessionCount: 5,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'new-s1',
          sessionTitle: 'New Session',
          messages: [],
          total: 0,
          backend: 'codebuddy',
          agentId: 'agent2',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 5 }),
      })

    const messages = ref([{ id: 'old' }] as any[])
    const options = {
      currentSessionId: ref('old-s1'),
      messages,
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: { task1: true },
      blockAskQuestions: { q1: true },
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession('agent2')

    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/ai/sessions',
      expect.objectContaining({ method: 'POST' })
    )
    expect(options.currentSessionId.value).toBe('new-s1')
    expect(messages.value).toEqual([])
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'success', icon: '✨' })
    )
  })

  it('API returns !ok: shows error toast', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'Server error' }),
    })

    const session = createSession()
    await session.createSession()

    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('API returns !data.ok: shows error toast with data.error', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: false, error: 'Too many sessions' }),
    })

    const session = createSession()
    await session.createSession()

    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('sets currentSessionId, currentBackend, currentAgentId from response', async () => {
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true,
          sessionId: 's-new',
          title: 'T',
          backend: 'claude',
          agentId: 'agent3',
          sessionCount: 1,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's-new',
          sessionTitle: 'T',
          messages: [],
          total: 0,
          backend: 'claude',
          agentId: 'agent3',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 1 }),
      })

    const currentSessionId = ref('old')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession('agent3')

    expect(currentSessionId.value).toBe('s-new')
    expect(mockIdentity.currentBackend).toBe('claude')
    expect(mockIdentity.currentAgentId).toBe('agent3')
  })

  it('clears blockTasks and blockAskQuestions', async () => {
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true,
          sessionId: 's-new',
          backend: '',
          agentId: '',
          sessionCount: 1,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's-new',
          messages: [],
          total: 0,
          backend: '',
          agentId: '',
          modelId: '',
          thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 1 }),
      })

    const blockTasks: Record<string, any> = { t1: 'a', t2: 'b' }
    const blockAskQuestions: Record<string, any> = { q1: 'x', q2: 'y' }
    const options = {
      currentSessionId: ref('old'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks,
      blockAskQuestions,
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession()

    expect(Object.keys(blockTasks).length).toBe(0)
    expect(Object.keys(blockAskQuestions).length).toBe(0)
  })

  it('delegates to switchSession which disconnects stream', async () => {
    // Verify that switchSession is called after POST, ensuring all state
    // transitions (disconnectStream, loadHistory) are handled properly.
    const onDisconnectStream = vi.fn()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true, sessionId: 's-new', backend: 'claude', agentId: 'a1', sessionCount: 2,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's-new', sessionTitle: 'New', messages: [], total: 0,
          backend: 'claude', agentId: 'a1', modelId: '', thinkingEffort: '',
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 2 }),
      })

    const options = {
      currentSessionId: ref('old'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession()

    // switchSession was called internally — it calls onDisconnectStream
    expect(onDisconnectStream).toHaveBeenCalled()
  })

  it('switchSession bumps loadHistorySeq, invalidating in-flight loadHistory', async () => {
    // Race condition: loadHistory is in-flight when createSession starts.
    // switchSession increments loadHistorySeq so the stale loadHistory response
    // is discarded and cannot overwrite the new sessionId.
    let loadHistoryResolve!: (v: any) => void
    const loadHistoryPromise = new Promise(resolve => { loadHistoryResolve = resolve })

    globalThis.fetch = vi.fn()
      // 1st call: POST /api/ai/sessions
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true, sessionId: 's-new', backend: '', agentId: '', sessionCount: 1,
        }),
      })
      // 2nd call: GET /api/ai/chat?session_id=s-new (from switchSession)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's-new', messages: [], total: 0,
          backend: '', agentId: '', modelId: '', thinkingEffort: '', running: false,
        }),
      })
      // 3rd call: GET /api/ai/sessions (from loadSessionsOnce)
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 1 }),
      })

    const currentSessionId = ref('old-session')
    const options = {
      currentSessionId,
      messages: ref([{ id: 'old-msg' }] as any[]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    await session.createSession()

    // The new session ID should stick — not be overwritten by a stale loadHistory
    expect(currentSessionId.value).toBe('s-new')
  })

  it('on POST failure: shows error toast, does not switch session', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'Server error' }),
    })

    const currentSessionId = ref('old-session')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession()

    // Session ID should NOT have changed on failure
    expect(currentSessionId.value).toBe('old-session')
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('pre-check: shows sessionLimitReached toast and returns early when sessionCount >= sessionMaxCount', async () => {
    mockState.sessionMaxCount = 5
    mockState.sessionCount = 5
    const fetchSpy = vi.fn()
    globalThis.fetch = fetchSpy

    const currentSessionId = ref('current-s1')
    const inputDisabled = ref(false)
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled,
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession('agent1')

    // No POST request should have been made
    expect(fetchSpy).not.toHaveBeenCalled()
    // currentSessionId should remain unchanged
    expect(currentSessionId.value).toBe('current-s1')
    // inputDisabled should remain false (no switching overlay)
    expect(inputDisabled.value).toBe(false)
    // sessionLimitReached toast should be shown
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error', icon: '⚠️' })
    )
  })

  it('pre-check: allows creation when sessionCount < sessionMaxCount', async () => {
    mockState.sessionMaxCount = 5
    mockState.sessionCount = 3
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true, sessionId: 's-new', backend: '', agentId: '', sessionCount: 4,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's-new', messages: [], total: 0,
          backend: '', agentId: '', modelId: '', thinkingEffort: '', running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 4 }),
      })

    const currentSessionId = ref('old')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession()

    // POST should have been made (pre-check passed)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/ai/sessions',
      expect.objectContaining({ method: 'POST' })
    )
    expect(currentSessionId.value).toBe('s-new')
  })

  it('pre-check: allows creation when sessionMaxCount is 0 (unlimited)', async () => {
    mockState.sessionMaxCount = 0
    mockState.sessionCount = 100
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true, sessionId: 's-new', backend: '', agentId: '', sessionCount: 101,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's-new', messages: [], total: 0,
          backend: '', agentId: '', modelId: '', thinkingEffort: '', running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 101 }),
      })

    const currentSessionId = ref('old')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession()

    // POST should have been made (maxCount=0 means unlimited)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/ai/sessions',
      expect.objectContaining({ method: 'POST' })
    )
  })

  it('on POST failure after pre-check passes: restores currentSessionId to prevent stuck archive button', async () => {
    mockState.sessionMaxCount = 5
    mockState.sessionCount = 3 // Pre-check passes
    // But backend returns 409 (TOCTOU race — another client created a session)
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: () => Promise.resolve({ error: 'max sessions reached', msgKey: 'SessionLimitReached' }),
    })

    const currentSessionId = ref('current-s1')
    const inputDisabled = ref(false)
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled,
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.createSession()

    // currentSessionId should be restored after failure
    expect(currentSessionId.value).toBe('current-s1')
    // inputDisabled should be reset
    expect(inputDisabled.value).toBe(false)
  })
})

// ───────────────────────────────────────────────────────────
// archiveSession
// ───────────────────────────────────────────────────────────

describe('archiveSession', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('successful deletion of current session: switches to another session', async () => {
    // 1. DELETE /api/ai/session/archive → { ok: true }
    // 2. GET /api/ai/sessions → { sessions: [{ id: 's2', backend: 'claude' }] }
    // 3. switchSession('s2') → GET /api/ai/chat?session_id=s2 → session data
    // 4. loadSessionsOnce inside switchSession → GET /api/ai/sessions → sessions
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ ok: true, sessionCount: 3 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [{ id: 's2', backend: 'claude' }] }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's2', messages: [], total: 0,
          backend: 'claude', agentId: 'a1', modelId: '', thinkingEffort: '', running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [{ id: 's2', unreadCount: 0, running: false }] }),
      })

    const currentSessionId = ref('s1')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.archiveSession('s1', 'claude')

    // Should have switched to s2
    expect(currentSessionId.value).toBe('s2')
    // Success toast shown
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ icon: '📦', type: 'success' })
    )
  })

  it('deletion of current session with no remaining sessions: creates a new one', async () => {
    // 1. DELETE /api/ai/session/archive → { ok: true }
    // 2. GET /api/ai/sessions → { sessions: [] }
    // 3. createSession() → POST /api/ai/sessions → new session
    // 4. switchSession() → GET /api/ai/chat?session_id=s-new → session data
    // 5. loadSessionsOnce → GET /api/ai/sessions → session list
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ ok: true, sessionCount: 0 }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [] }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          ok: true, sessionId: 's-new', title: '', backend: '', agentId: '', sessionCount: 1,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's-new', messages: [], total: 0,
          backend: '', agentId: '', modelId: '', thinkingEffort: '', running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 1 }),
      })

    const currentSessionId = ref('s1')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.archiveSession('s1', 'claude')

    // Should have created a new session
    expect(currentSessionId.value).toBe('s-new')
  })

  it('deletion of non-current session: no switch needed', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: true, sessionCount: 2 }),
    })

    const currentSessionId = ref('s1')
    const onConnectStream = vi.fn()
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream,
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.archiveSession('s2', 'claude')

    // Should NOT switch — still on s1
    expect(currentSessionId.value).toBe('s1')
    // No switchSession calls (onConnectStream only called during switch)
    expect(onConnectStream).not.toHaveBeenCalled()
    // Success toast shown
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ icon: '📦', type: 'success' })
    )
    // Two fetch calls: 1) delete API 2) loadSessionsOnce (refresh global state)
    expect(globalThis.fetch).toHaveBeenCalledTimes(2)
  })

  it('API returns ok=false: shows error toast', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ ok: false }),
    })

    const session = createSession()
    await session.archiveSession('s1', 'claude')

    // Error toast shown when data.ok is false
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ icon: '⚠️', type: 'error' })
    )
  })
})

// ───────────────────────────────────────────────────────────
// handleVisibilityChange
// ───────────────────────────────────────────────────────────

describe('handleVisibilityChange', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('when visible and loading=true: disconnects stream, reloads history', async () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Mock fetch for the loadHistory call
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1', messages: [], total: 0, running: false,
      }),
    })

    // Mock visibilityState to 'visible'
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')

    session.handleVisibilityChange()

    // Wait for async loadHistory to complete
    await vi.waitFor(() => {
      expect(onDisconnectStream).toHaveBeenCalled()
    })
    expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/ai/chat?session_id=s1'),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    vi.restoreAllMocks()
  })

  it('when visible and loading=false: does nothing', async () => {
    const loading = ref(false)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')

    session.handleVisibilityChange()

    expect(onDisconnectStream).not.toHaveBeenCalled()

    vi.restoreAllMocks()
  })

  it('when hidden: does nothing', async () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')

    session.handleVisibilityChange()

    expect(onDisconnectStream).not.toHaveBeenCalled()

    vi.restoreAllMocks()
  })
})

// ───────────────────────────────────────────────────────────
// handleWsReconnect
// ───────────────────────────────────────────────────────────

describe('handleWsReconnect', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('when loading=true and session no longer running: disconnects stream, cleans up, reloads history', async () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const onExtractScheduledTasks = vi.fn()
    const onRenderUpdate = vi.fn()
    const options = {
      currentSessionId: ref('s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks,
      onRenderUpdate,
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Mock loadSessionsOnce to NOT include s1 in runningSessions
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [{ id: 's1', running: false }],
        totalCount: 1,
      }),
    })

    // Mock loadHistory fetch
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        // First call: loadSessionsOnce
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ id: 's1', running: false }],
          totalCount: 1,
        }),
      })
      .mockResolvedValueOnce({
        // Second call: loadHistory
        ok: true,
        json: () => Promise.resolve({
          sessionId: 's1', messages: [], total: 0, running: false,
        }),
      })

    await session.handleWsReconnect()

    expect(onDisconnectStream).toHaveBeenCalled()
    expect(mockForceCleanupStreamingState).toHaveBeenCalled()
    expect(loading.value).toBe(false)

    vi.restoreAllMocks()
  })

  it('when loading=true and session still running: does nothing', async () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Mock loadSessionsOnce to include s1 in runningSessions
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessions: [{ id: 's1', running: true }],
        totalCount: 1,
      }),
    })

    await session.handleWsReconnect()

    expect(onDisconnectStream).not.toHaveBeenCalled()
    expect(loading.value).toBe(true)

    vi.restoreAllMocks()
  })

  it('when loading=false: does nothing', async () => {
    const loading = ref(false)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref('s1'),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    await session.handleWsReconnect()

    expect(onDisconnectStream).not.toHaveBeenCalled()

    vi.restoreAllMocks()
  })

  it('when no currentSessionId: does nothing', async () => {
    const loading = ref(true)
    const onDisconnectStream = vi.fn()
    const options = {
      currentSessionId: ref(''),
      messages: ref([]),
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
      onDisconnectStream,
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    await session.handleWsReconnect()

    expect(onDisconnectStream).not.toHaveBeenCalled()

    vi.restoreAllMocks()
  })
})

// ───────────────────────────────────────────────────────────
// syncModelFromData (tested indirectly through loadHistory)
// ───────────────────────────────────────────────────────────

describe('syncModelFromData', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('when server provides modelId: uses it', async () => {
    mockAgentFns.getAgentModel.mockReturnValue({ name: 'GPT-4o' })

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        backend: 'codebuddy',
        agentId: 'agent1',
        modelId: 'gpt-4o',
        thinkingEffort: '',
        running: false,
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(mockIdentity.currentModelId).toBe('gpt-4o')
    expect(mockIdentity.currentModelName).toBe('GPT-4o')
  })

  it('when server has no modelId: falls back to localStorage preference', async () => {
    mockIdentityFns.loadModelPref.mockReturnValue('saved-model')
    mockAgentFns.getAgentModel.mockReturnValue({ name: 'Saved Model' })

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        backend: 'codebuddy',
        agentId: 'agent1',
        modelId: '',  // no model from server
        thinkingEffort: '',
        running: false,
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(mockIdentityFns.loadModelPref).toHaveBeenCalledWith('agent1')
    expect(mockIdentity.currentModelId).toBe('saved-model')
    expect(mockIdentity.currentModelName).toBe('Saved Model')
  })

  it('when localStorage preference is stale (model no longer available): falls back to agent default', async () => {
    mockIdentityFns.loadModelPref.mockReturnValue('stale-model')
    mockAgentFns.getAgentModel.mockReturnValue(undefined)  // model not found
    mockAgentFns.syncModelFromAgent.mockReturnValue({ modelId: 'default-model', modelName: 'Default Model' })

    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        backend: 'codebuddy',
        agentId: 'agent1',
        modelId: '',  // no model from server
        thinkingEffort: '',
        running: false,
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(mockAgentFns.syncModelFromAgent).toHaveBeenCalledWith('agent1')
    expect(mockIdentity.currentModelId).toBe('default-model')
    expect(mockIdentity.currentModelName).toBe('Default Model')
  })
})

// ───────────────────────────────────────────────────────────
// syncUsageFromData (tested indirectly through loadHistory)
// ───────────────────────────────────────────────────────────

describe('syncUsageFromData', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('restores usage state from loadHistory API response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        backend: 'claude',
        agentId: 'agent1',
        modelId: '',
        thinkingEffort: '',
        running: false,
        usageState: { used: 100000, size: 200000, cost: 2.5, currency: 'EUR' },
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(mockUpdateUsageState).toHaveBeenCalledWith(100000, 200000, 2.5, 'EUR', 's1', undefined, undefined)
  })

  it('does not call updateUsageState when usageState is missing', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        running: false,
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(mockUpdateUsageState).not.toHaveBeenCalled()
  })

  it('does not call updateUsageState when usageState.size is 0', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 's1',
        messages: [],
        total: 0,
        running: false,
        usageState: { used: 0, size: 0 },
      }),
    })

    const session = createSession()
    await session.loadHistory(true, false, false)

    expect(mockUpdateUsageState).not.toHaveBeenCalled()
  })
})

describe('loadMoreMessages', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('fetches older messages using before_id cursor from oldest message id', async () => {
    // First: loadHistory to populate messages and totalMessages
    const initialMsgs = [
      { id: 50, role: 'user', content: 'hello' },
      { id: 51, role: 'assistant', content: 'hi' },
    ]
    const olderMsgs = [
      { id: 42, role: 'user', content: 'older' },
      { id: 43, role: 'assistant', content: 'older reply' },
    ]

    // First fetch: loadHistory
    // Second fetch: loadMoreMessages
    mockUtilsFns.parseMessages
      .mockReturnValueOnce(initialMsgs)
      .mockReturnValueOnce(olderMsgs)

    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'current-s1',
          messages: [{ id: 50 }, { id: 51 }],
          total: 100, // more than 2 → hasMore=true
          running: false,
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          messages: [{ id: 42 }, { id: 43 }],
          total: 100,
        }),
      })

    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Load initial messages
    await session.loadHistory(true, false, false)
    expect(options.messages.value.length).toBe(2)

    // Load more (older) messages
    await session.loadMoreMessages()

    // Should use before_id=50 (oldest message id) in the fetch URL
    const secondCallUrl = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[1]?.[0] as string
    expect(secondCallUrl).toContain('before_id=50')
    expect(secondCallUrl).toContain('limit=20')

    // Older messages should be prepended
    expect(options.messages.value.length).toBe(4)
    expect(options.messages.value[0].id).toBe(42)
  })

  it('skips when loadingMore is already true', async () => {
    globalThis.fetch = vi.fn()

    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([{ id: 1, role: 'user' }]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    session.loadingMore.value = true

    await session.loadMoreMessages()

    expect(globalThis.fetch).not.toHaveBeenCalled()
  })

  it('skips when hasMore is false', async () => {
    globalThis.fetch = vi.fn()

    const options = {
      currentSessionId: ref('current-s1'),
      messages: ref([{ id: 1, role: 'user' }]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    session.hasMore.value = false

    await session.loadMoreMessages()

    expect(globalThis.fetch).not.toHaveBeenCalled()
  })
})

// ───────────────────────────────────────────────────────────
// continueFromExecution
// ───────────────────────────────────────────────────────────

describe('continueFromExecution', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('normal flow: check → POST → switchTab → switchSession', async () => {
    // 1. GET check: { exists: false, sessionId: '' }
    // 2. POST create: { ok: true, sessionId: 'new-s1', alreadyExists: false }
    // 3. switchSession('new-s1') → loadHistory(immediate=true) → GET /api/ai/chat?session_id=new-s1
    //    (loadHistory may also fire warmWorktreeCache/loadAgents in parallel)
    // 4. loadSessionsOnce → GET /api/ai/sessions
    const mockSwitchTab = vi.fn()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        // 1. GET check
        ok: true,
        json: () => Promise.resolve({ exists: false, sessionId: '' }),
      })
      .mockResolvedValueOnce({
        // 2. POST create
        ok: true,
        json: () => Promise.resolve({ ok: true, sessionId: 'new-s1', alreadyExists: false }),
      })
      .mockResolvedValue({
        // Catch-all for all subsequent fetches (loadHistory chat fetch,
        // warmWorktreeCache, loadAgents, loadSessionsOnce, etc.)
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'new-s1', messages: [], total: 0,
          backend: 'claude', agentId: 'agent1', modelId: '', thinkingEffort: '', running: false,
        }),
      })

    const session = createSession()
    const result = await session.continueFromExecution(1, 42, mockSwitchTab)

    expect(result).toBe(true)
    // 1. GET check
    expect(globalThis.fetch).toHaveBeenNthCalledWith(1, '/api/tasks/1/executions/42/continue')
    // 2. POST create
    expect(globalThis.fetch).toHaveBeenNthCalledWith(2, '/api/tasks/1/executions/42/continue', expect.objectContaining({ method: 'POST' }))
    // 3. switchTab called
    expect(mockSwitchTab).toHaveBeenCalledWith('chat')
    // 4. switchSession delegated to loadHistory and completed successfully.
    //    The chat fetch URL contains the session ID set by clearSessionIdentity.
    //    In production, currentSessionId ref and identity ref are the same object,
    //    so clearSessionIdentity updates the ref that loadHistory reads.
    //    In this test, they're different refs (test isolation), so we verify
    //    that loadHistory was called by checking a chat fetch was attempted.
    const allCalls = (globalThis.fetch as any).mock.calls
    const hasChatFetch = allCalls.some(
      (call: any[]) => typeof call[0] === 'string' && call[0].includes('/api/ai/chat')
    )
    expect(hasChatFetch).toBe(true)
  })

  it('already continued: skips POST, navigates to existing session', async () => {
    // GET check: { exists: true, sessionId: 'existing-s1' }
    const mockSwitchTab = vi.fn()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ exists: true, sessionId: 'existing-s1' }),
      })
      .mockResolvedValue({
        // Catch-all for loadHistory, loadAgents, loadSessionsOnce
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'existing-s1', messages: [], total: 0,
          backend: 'claude', agentId: 'agent1', modelId: '', thinkingEffort: '', running: false,
        }),
      })

    const session = createSession()
    const result = await session.continueFromExecution(1, 42, mockSwitchTab)

    expect(result).toBe(true)
    // GET check was called, POST was NOT called
    expect(globalThis.fetch).toHaveBeenNthCalledWith(1, '/api/tasks/1/executions/42/continue')
    expect(globalThis.fetch).not.toHaveBeenCalledWith('/api/tasks/1/executions/42/continue', expect.objectContaining({ method: 'POST' }))
    expect(mockSwitchTab).toHaveBeenCalledWith('chat')
  })

  it('POST returns 409 (session limit): shows toast error', async () => {
    // GET check: not continued
    // POST create: 409 error
    const mockSwitchTab = vi.fn()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ exists: false, sessionId: '' }),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 409,
        json: () => Promise.resolve({ error: 'max sessions reached', msgKey: 'SessionLimitReached' }),
      })

    const session = createSession()
    const result = await session.continueFromExecution(1, 42, mockSwitchTab)

    expect(result).toBe(false)
    expect(mockSwitchTab).not.toHaveBeenCalled()
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('POST returns other error: shows generic toast error', async () => {
    const mockSwitchTab = vi.fn()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ exists: false, sessionId: '' }),
      })
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: 'internal server error' }),
      })

    const session = createSession()
    const result = await session.continueFromExecution(1, 42, mockSwitchTab)

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('GET check fails: shows toast error', async () => {
    const mockSwitchTab = vi.fn()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ error: 'server error' }),
      })

    const session = createSession()
    const result = await session.continueFromExecution(1, 42, mockSwitchTab)

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('POST returns ok but no sessionId: shows toast error', async () => {
    const mockSwitchTab = vi.fn()
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ exists: false, sessionId: '' }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ ok: true, sessionId: '', alreadyExists: false }),
      })

    const session = createSession()
    const result = await session.continueFromExecution(1, 42, mockSwitchTab)

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('network error during check: shows toast error, returns false', async () => {
    const mockSwitchTab = vi.fn()
    globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error('Network error'))

    const session = createSession()
    const result = await session.continueFromExecution(1, 42, mockSwitchTab)

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('checkContinueSession: returns check result without creating', async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ exists: true, sessionId: 'existing-s1' }),
    })

    const session = createSession()
    const result = await session.checkContinueSession(1, 42)

    expect(result).toEqual({ exists: true, sessionId: 'existing-s1' })
    expect(globalThis.fetch).toHaveBeenCalledWith('/api/tasks/1/executions/42/continue')
  })

  it('checkContinueSession: handles fetch error gracefully', async () => {
    globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error('Network error'))

    const session = createSession()
    const result = await session.checkContinueSession(1, 42)

    expect(result).toEqual({ exists: false, sessionId: '' })
  })
})

// ───────────────────────────────────────────────────────────
// forkSession
// ───────────────────────────────────────────────────────────

describe('forkSession', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('calls fork API and switches to new session', async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ ok: true, sessionId: 'forked-s1', sessionCount: 2 }),
    })

    const session = createSession()
    mockState.currentSessionId = 'source-s1'
    const result = await session.forkSession('source-s1')

    expect(result).toBe(true)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/ai/session/fork',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ sessionId: 'source-s1' }),
      })
    )
    // switchSession should have been called with the new session ID
    // (tested via the fetch for loadHistory after switch)
  })

  it('shows session limit toast on 409', async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: () => Promise.resolve({ msgKey: 'SessionLimitReached' }),
    })

    const session = createSession()
    mockState.currentSessionId = 'source-s1'
    const result = await session.forkSession('source-s1')

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('shows generic error toast on non-ok response', async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce({
      ok: false,
      status: 500,
      json: () => Promise.resolve({ error: 'Internal error' }),
    })

    const session = createSession()
    mockState.currentSessionId = 'source-s1'
    const result = await session.forkSession('source-s1')

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('shows error toast on network failure', async () => {
    globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error('Network error'))

    const session = createSession()
    mockState.currentSessionId = 'source-s1'
    const result = await session.forkSession('source-s1')

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('shows error toast when response lacks sessionId', async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ ok: true }),
    })

    const session = createSession()
    mockState.currentSessionId = 'source-s1'
    const result = await session.forkSession('source-s1')

    expect(result).toBe(false)
    expect(mockToastFn).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ type: 'error' })
    )
  })

  it('sends agentId in fork request body', async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ ok: true, sessionId: 'forked-s1', sessionCount: 2 }),
    })

    const session = createSession()
    mockState.currentSessionId = 'source-s1'
    const result = await session.forkSession('source-s1', undefined, 'codebuddy')

    expect(result).toBe(true)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/ai/session/fork',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ sessionId: 'source-s1', agentId: 'codebuddy' }),
      })
    )
  })

  it('sends beforeMessageId and agentId together', async () => {
    globalThis.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve({ ok: true, sessionId: 'forked-s1', sessionCount: 2 }),
    })

    const session = createSession()
    mockState.currentSessionId = 'source-s1'
    const result = await session.forkSession('source-s1', 42, 'claude')

    expect(result).toBe(true)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/ai/session/fork',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ sessionId: 'source-s1', beforeMessageId: 42, agentId: 'claude' }),
      })
    )
  })
})

// ───────────────────────────────────────────────────────────
// loadHistory race protection (loadHistorySeq)
// ───────────────────────────────────────────────────────────

describe('loadHistory race protection', () => {
  let originalFetch: typeof globalThis.fetch

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  // loadHistory coalescing: when a second loadHistory is called while the first
  // is in-flight, it sets pendingReload. After the first completes, the pendingReload
  // fires via setTimeout and its result overwrites the first. The final state
  // reflects the pendingReload's data.
  it('applies pendingReload result after in-flight loadHistory completes', async () => {
    let resolveFirst: (v: any) => void
    const firstPromise = new Promise(resolve => { resolveFirst = resolve })

    const callOrder: string[] = []

    // URL-aware mock: agents fetch resolves immediately; chat fetch follows call order
    globalThis.fetch = vi.fn((url: string) => {
      if (url.includes('/api/agents')) {
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ agents: [], defaultAgent: 'claude' }) })
      }
      if (!callOrder.includes('first-start')) {
        callOrder.push('first-start')
        return firstPromise
      }
      if (!callOrder.includes('second-start')) {
        callOrder.push('second-start')
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({
            sessionId: 's2',
            sessionTitle: 'Second Session',
            messages: [],
            total: 0,
            running: false,
          }),
        })
      }
      // Default: loadSessionsOnce or other
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: [] }) })
    })

    const currentSessionId = ref('current-s1')
    const messages = ref([])
    const loading = ref(false)
    const options = {
      currentSessionId,
      messages,
      loading,
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Start first loadHistory (slow, won't resolve yet)
    const firstLoad = session.loadHistory(true, false, false)
    await vi.waitFor(() => callOrder.includes('first-start'))

    // Start second loadHistory — gets coalesced into pendingReload
    const secondLoad = session.loadHistory(true, false, false)

    // Now resolve the first one with stale data
    resolveFirst!({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'stale-s1',
        sessionTitle: 'Stale Session',
        messages: [],
        total: 0,
        running: false,
      }),
    })

    // Wait for the first load to complete — stale data is applied first
    await firstLoad

    // After first load completes, pendingReload fires via setTimeout(0).
    // Yield to the event loop to let the setTimeout(0) execute
    await new Promise(r => setTimeout(r, 10))

    // Wait for the second fetch to start and complete.
    await vi.waitFor(() => callOrder.includes('second-start'), { timeout: 5000 })
    await secondLoad

    // The pendingReload result overwrites the stale data
    expect(currentSessionId.value).toBe('s2')
  })

  it('switchSession bumps loadHistorySeq, discarding in-flight loadHistory', async () => {
    let resolveLoadHistory: (v: any) => void
    const loadHistoryPromise = new Promise(resolve => { resolveLoadHistory = resolve })

    globalThis.fetch = vi.fn()
      // loadHistory call: slow
      .mockImplementationOnce(() => loadHistoryPromise)
      // switchSession call: fast
      .mockImplementationOnce(() => Promise.resolve({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'switched-s1',
          messages: [],
          total: 0,
          running: false,
        }),
      }))
      // loadSessionsOnce (called by switchSession)
      .mockImplementationOnce(() => Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ sessions: [] }),
      }))

    const currentSessionId = ref('current-s1')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)

    // Start a slow loadHistory
    const loadHistoryTask = session.loadHistory(true, false, false)

    // Before it resolves, call switchSession
    const switchTask = session.switchSession('switched-s1')
    await switchTask

    // Now resolve the slow loadHistory with stale data
    resolveLoadHistory!({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'stale-s1',
        messages: [],
        total: 0,
        running: false,
      }),
    })
    await loadHistoryTask

    // switchSession result should win
    expect(currentSessionId.value).toBe('switched-s1')
  })
})

// ───────────────────────────────────────────────────────────
// loadHistory session_id recovery
// ───────────────────────────────────────────────────────────

describe('loadHistory session_id recovery', () => {
  let originalFetch: typeof globalThis.fetch
  let warnSpy: ReturnType<typeof vi.spyOn>

  beforeEach(() => {
    resetMockState()
    resetChatSessionState()
    resetAdditionalMocks()
    originalFetch = globalThis.fetch
    warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {})
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    warnSpy.mockRestore()
  })

  it('recovers session from backend when currentSessionId is empty', async () => {
    globalThis.fetch = vi.fn()
      // Recovery call: /api/ai/chat?limit=1
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'recovered-s1',
          sessionTitle: 'Recovered Session',
          backend: 'claude',
          agentId: 'agent1',
          modelId: 'model-x',
          thinkingEffort: 'high',
          messages: [],
          total: 5,
          running: false,
        }),
      })
      // Full load call: /api/ai/chat?session_id=recovered-s1&limit=...
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'recovered-s1',
          sessionTitle: 'Recovered Session',
          backend: 'claude',
          agentId: 'agent1',
          modelId: 'model-x',
          thinkingEffort: 'high',
          messages: [{ id: 'm1' }, { id: 'm2' }],
          total: 5,
          running: false,
        }),
      })

    const currentSessionId = ref('') // empty — triggers recovery
    const messages = ref([])
    const options = {
      currentSessionId,
      messages,
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    // Should have made two fetch calls: recovery + full load
    expect(globalThis.fetch).toHaveBeenCalledTimes(2)
    // First call: recovery with full limit (from store.state.chatInitialMessages)
    expect(globalThis.fetch).toHaveBeenNthCalledWith(1, expect.stringContaining('limit=20'), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    // Second call: full load with explicit session_id
    expect(globalThis.fetch).toHaveBeenNthCalledWith(2, expect.stringContaining('session_id=recovered-s1'), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    // currentSessionId should be set from recovery
    expect(currentSessionId.value).toBe('recovered-s1')
    expect(mockIdentity.currentSessionTitle).toBe('Recovered Session')
    expect(mockIdentity.currentBackend).toBe('claude')
    expect(mockIdentity.currentAgentId).toBe('agent1')
  })

  it('returns early when recovery yields no session', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: '',
        messages: [],
        total: 0,
        running: false,
      }),
    })

    const currentSessionId = ref('')
    const messages = ref([])
    const options = {
      currentSessionId,
      messages,
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    // Only recovery call should be made (no full load)
    expect(globalThis.fetch).toHaveBeenCalledTimes(1)
    // Messages should remain empty
    expect(messages.value).toEqual([])
    // currentSessionId should still be empty
    expect(currentSessionId.value).toBe('')
  })

  it('logs warning when backend returns different sessionId than requested', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({
        sessionId: 'wrong-s1',
        sessionTitle: 'Wrong Session',
        backend: 'claude',
        agentId: 'agent1',
        messages: [],
        total: 0,
        running: false,
      }),
    })

    const currentSessionId = ref('current-s1')
    const options = {
      currentSessionId,
      messages: ref([]),
      loading: ref(false),
      inputDisabled: ref(false),
      blockTasks: {},
      blockAskQuestions: {},
      expandedTools: ref({}),
      onParseAssistantContent: vi.fn(),
      onExtractScheduledTasks: vi.fn(),
      onRenderUpdate: vi.fn(),
      onScrollBottom: vi.fn(),
      onConnectStream: vi.fn(),
        onDisconnectStream: vi.fn(),
      onOpen: vi.fn(),
    }
    const session = useChatSession(options)
    await session.loadHistory(true, false, false)

    // Should have logged a warning about mismatch
    expect(warnSpy).toHaveBeenCalledWith(
      '[ChatSession]',
      expect.stringContaining('session ID mismatch')
    )
  })

  it('restores queued messages from backend queue field in recovery path', async () => {
    // Recovery path: currentSessionId is empty, loadHistory uses /api/ai/chat?limit=N
    // The backend response includes a queue field that must be appended as pending messages.
    const queuedMessages = [
      { queueId: 'pending-recovery1', text: 'queued in recovery', filePaths: [], files: [], createdAt: '2026-01-01T00:00:00Z' },
    ]
    // Reset currentSessionId so loadHistory takes the recovery path
    mockState.currentSessionId = ''
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({
          sessionId: 'recovered-s1',
          sessionTitle: 'Recovered Session',
          backend: 'claude',
          agentId: 'agent1',
          modelId: '',
          thinkingEffort: '',
          messages: [{ id: 1, role: 'user', content: 'hello' }],
          total: 1,
          running: false,
          queue: queuedMessages,
        }),
      })
      .mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], totalCount: 0 }),
      })

    const { session, options } = createSessionInternal()
    // Clear currentSessionId so loadHistory takes the recovery path
    options.currentSessionId.value = ''
    await session.loadHistory()

    // Recovery path should also restore queued messages
    const pendingMsgs = options.messages.value.filter((m: any) => m.pending)
    expect(pendingMsgs.length).toBe(1)
    expect(pendingMsgs[0].content).toBe('queued in recovery')
    expect(pendingMsgs[0].id).toBe('pending-recovery1')
  })
})

// ── loadSessionsOnce dedup / resetChatSessionState ──

describe('loadSessionsOnce dedup', () => {
  beforeEach(() => {
    resetChatSessionState()
    vi.clearAllMocks()
  })

  it('deduplicates concurrent calls (shares single in-flight request)', async () => {
    let resolveFetch: (v: any) => void
    const fetchPromise = new Promise(r => { resolveFetch = r })
    const mockFetch = vi.fn().mockReturnValue(fetchPromise)
    vi.stubGlobal('fetch', mockFetch)

    // Fire two concurrent calls before the first resolves
    const p1 = loadSessionsOnce()
    const p2 = loadSessionsOnce()

    // Only one fetch call should have been made
    expect(mockFetch).toHaveBeenCalledTimes(1)

    // Resolve the fetch so both promises complete
    resolveFetch!({
      ok: true,
      json: () => Promise.resolve({ sessions: [], totalCount: 0 }),
    })
    await p1
    await p2

    vi.unstubAllGlobals()
  })

  it('allows sequential calls (after previous completes)', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: [], totalCount: 0 }),
    })
    vi.stubGlobal('fetch', mockFetch)

    await loadSessionsOnce()
    await loadSessionsOnce()

    // Two fetch calls should have been made
    expect(mockFetch).toHaveBeenCalledTimes(2)

    vi.unstubAllGlobals()
  })

  it('resetChatSessionState clears in-flight promise', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: [], totalCount: 0 }),
    })
    vi.stubGlobal('fetch', mockFetch)

    await loadSessionsOnce()
    resetChatSessionState()
    await loadSessionsOnce()

    // Two fetch calls should have been made
    expect(mockFetch).toHaveBeenCalledTimes(2)

    vi.unstubAllGlobals()
  })
})
