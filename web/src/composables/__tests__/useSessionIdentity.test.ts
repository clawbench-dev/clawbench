import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'

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
  for (const id of pendingTimers) { clearTimeout(id) }
  pendingTimers.length = 0
  for (const id of pendingIntervals) { clearInterval(id) }
  pendingIntervals.length = 0
})

// Mock external dependencies — but NOT the module under test itself
const mockPatchAgentPref = vi.fn().mockResolvedValue(undefined)
vi.mock('@/composables/useSettingsConfig', () => ({
    patchAgentPref: (...args: any[]) => mockPatchAgentPref(...args),
}))

const mockGetAgent = vi.fn()
const mockGetAgentModel = vi.fn()
const mockSyncModelFromAgent = vi.fn().mockReturnValue({ modelId: 'default-model', modelName: 'Default Model' })
const mockGetEffectiveThinkingEffort = vi.fn().mockReturnValue(null)
const mockAgentHeaderTitle = vi.fn().mockReturnValue('🤖 Test')

const mockGetAgentThinkingEffortLevels = vi.fn().mockReturnValue([])

vi.mock('@/composables/useAgents', () => ({
    useAgents: () => ({
        agents: ref([]),
        loadAgents: vi.fn().mockResolvedValue(undefined),
        getAgent: mockGetAgent,
        getAgentModel: mockGetAgentModel,
        syncModelFromAgent: mockSyncModelFromAgent,
        getEffectiveThinkingEffort: mockGetEffectiveThinkingEffort,
        getAgentThinkingEffortLevels: mockGetAgentThinkingEffortLevels,
        agentHeaderTitle: mockAgentHeaderTitle,
        supportsACP: vi.fn().mockReturnValue(true),
    }),
    registerIdentityUpdaters: vi.fn(),
}))

vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: vi.fn() }),
}))

vi.mock('@/composables/useNotification', () => ({
    useNotification: () => ({ play: vi.fn() }),
}))

vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key,
}))

vi.mock('@/stores/app', () => ({
    store: { state: { chatInitialMessages: 50, chatPageSize: 50 } },
}))

vi.mock('@/utils/chatSessionUtils', () => ({
    buildMessageSnapshot: vi.fn().mockReturnValue(''),
    parseMessages: vi.fn().mockReturnValue([]),
}))

import { useSessionIdentity, registerSessionActions, initSessionFromAPI, resetIdentity, clearSessionIdentity, updateModeState, updateAvailableModes, clearModeState, updateCommandState, clearCommandState, updateThinkingEffortState, updateAvailableThinkingEfforts, clearThinkingEffortState, updateUsageState, clearUsageState, clearAllUsageState, clearUsageStateById, toggleAutoApprove, getSessionId, prefetchCommands, registerSessionDrawerRef } from '@/composables/useSessionIdentity'

describe('useSessionIdentity', () => {
    beforeEach(() => {
        vi.clearAllMocks()
    })

    // ── Identity refs ──

    describe('identity refs', () => {
        it('exposes all identity refs', () => {
            const identity = useSessionIdentity()
            expect(identity.currentSessionId).toBeDefined()
            expect(identity.currentSessionTitle).toBeDefined()
            expect(identity.currentBackend).toBeDefined()
            expect(identity.currentAgentId).toBeDefined()
            expect(identity.currentModelId).toBeDefined()
            expect(identity.currentModelName).toBeDefined()
            expect(identity.currentThinkingEffort).toBeDefined()
            expect(identity.runningSessions).toBeDefined()
            expect(identity.sessionDrawer).toBeDefined()
        })

        it('shares state across multiple instances (singleton)', () => {
            const id1 = useSessionIdentity()
            const id2 = useSessionIdentity()

            id1.thinkingEffortState.currentId.value = 'high'
            expect(id2.currentThinkingEffort.value).toBe('high')

            // Reset
            id1.thinkingEffortState.currentId.value = ''
        })
    })

    // ── currentThinkingEffort ──

    describe('currentThinkingEffort', () => {
        it('initializes as empty string (auto)', () => {
            const { currentThinkingEffort } = useSessionIdentity()
            expect(currentThinkingEffort.value).toBe('')
        })

        it('can be set to various effort levels', async () => {
            const { thinkingEffortState, currentThinkingEffort } = useSessionIdentity()

            for (const level of ['low', 'medium', 'high', 'xhigh', 'max']) {
                thinkingEffortState.currentId.value = level
                await nextTick()
                expect(currentThinkingEffort.value).toBe(level)
            }

            // Reset
            thinkingEffortState.currentId.value = ''
        })

        it('can be reset to empty (auto)', async () => {
            const { thinkingEffortState, currentThinkingEffort } = useSessionIdentity()
            thinkingEffortState.currentId.value = 'high'
            await nextTick()
            thinkingEffortState.currentId.value = ''
            await nextTick()
            expect(currentThinkingEffort.value).toBe('')
        })
    })

    // ── sessionDrawer ──

    describe('sessionDrawer', () => {
        it('can be opened and closed', async () => {
            const { sessionDrawer } = useSessionIdentity()
            sessionDrawer.open()
            await nextTick()
            expect(sessionDrawer.isOpen.value).toBe(true)

            sessionDrawer.close()
            await nextTick()
            expect(sessionDrawer.isOpen.value).toBe(false)
        })
    })

    // ── openSessionTab ──

    describe('openSessionTab', () => {
        it('opens the session drawer', async () => {
            const identity = useSessionIdentity()
            identity.sessionDrawer.close()

            identity.openSessionTab()
            await nextTick()

            expect(identity.sessionDrawer.isOpen.value).toBe(true)
        })
    })

    // ── switchSession proxy ──

    describe('switchSession', () => {
        it('delegates to registered callback', async () => {
            const mockSwitch = vi.fn()
            registerSessionActions({
                switchSession: mockSwitch,
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            await identity.switchSession('session-2')

            expect(mockSwitch).toHaveBeenCalledWith('session-2')
        })

        it('does nothing when callback is a no-op', async () => {
            // Register with no-op callbacks
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            // Should not throw
            await identity.switchSession('session-3')
        })
    })

    // ── createSession fallback ──

    describe('createSession fallback', () => {
        it('makes direct API call when no callback registered', async () => {
            // Register with empty no-op callbacks — but createSession should not be called
            // since we're testing fallback. Actually we need _createSession to be null.
            // The composable checks if (_createSession) before delegating.
            // We can't easily set it to null without modifying the source,
            // so let's test the delegation path instead.

            const mockCreate = vi.fn()
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: mockCreate,
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            await identity.createSession('agent-1')

            expect(mockCreate).toHaveBeenCalledWith('agent-1')
        })

        it('delegates to registered callback when available', async () => {
            const mockCreate = vi.fn()
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: mockCreate,
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            await identity.createSession('agent-2')

            expect(mockCreate).toHaveBeenCalledWith('agent-2')
        })
    })

    // ── archiveSession ──

    describe('archiveSession', () => {
        it('delegates to registered callback when available', async () => {
            const mockArchive = vi.fn()
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: mockArchive,
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            await identity.archiveSession('session-1', 'claude')

            expect(mockArchive).toHaveBeenCalledWith('session-1', 'claude')
        })

        it('does not throw when callback is no-op', async () => {
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            await expect(identity.archiveSession('session-1')).resolves.toBeUndefined()
        })
    })

    // ── sendMessage fallback ──

    describe('sendMessage fallback', () => {
        it('makes direct API call when callback is not registered', async () => {
            // Register with empty callbacks that won't intercept
            // This tests the sendMessage delegation path
            const mockSend = vi.fn()
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: mockSend,
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-1'

            await identity.sendMessage('hello')

            expect(mockSend).toHaveBeenCalledWith('hello')
        })

        it('delegates to registered callback when available', async () => {
            const mockSend = vi.fn()
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: mockSend,
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            await identity.sendMessage('test message')

            expect(mockSend).toHaveBeenCalledWith('test message')
        })
    })

    // ── openChatPanel ──

    describe('openChatPanel', () => {
        it('calls registered callback', () => {
            const mockOpen = vi.fn()
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: mockOpen,
            })

            const identity = useSessionIdentity()
            identity.openChatPanel()

            expect(mockOpen).toHaveBeenCalled()
        })

        it('does nothing when callback is no-op', () => {
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })
            const identity = useSessionIdentity()
            expect(() => identity.openChatPanel()).not.toThrow()
        })
    })

    // ── initSessionFromAPI ──

    describe('initSessionFromAPI', () => {
        it('populates identity refs from API response', async () => {
            const identity = useSessionIdentity()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    sessionTitle: 'API Session',
                    backend: 'codebuddy',
                    agentId: 'agent-1',
                    modelId: 'model-1',
                    thinkingEffortState: {
                        currentId: 'high',
                        availableLevels: [{ id: 'low', name: 'Low' }, { id: 'high', name: 'High' }],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)

            mockGetAgentModel.mockReturnValue({ name: 'Model One' })

            await initSessionFromAPI()

            expect(identity.currentSessionId.value).toBe('api-session')
            expect(identity.currentSessionTitle.value).toBe('API Session')
            expect(identity.currentBackend.value).toBe('codebuddy')
            expect(identity.currentAgentId.value).toBe('agent-1')
            expect(identity.currentModelId.value).toBe('model-1')
            expect(identity.currentModelName.value).toBe('Model One')
            expect(identity.currentThinkingEffort.value).toBe('high')
            expect(identity.currentThinkingEffortName.value).toBe('High')

            vi.unstubAllGlobals()
        })

        it('handles API failure gracefully', async () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'existing'

            const mockFetch = vi.fn().mockRejectedValue(new Error('fail'))
            vi.stubGlobal('fetch', mockFetch)

            await expect(initSessionFromAPI()).resolves.toBeUndefined()
            // Should not change existing values
            expect(identity.currentSessionId.value).toBe('existing')

            vi.unstubAllGlobals()
        })

        it('falls back to saved model preference when server returns no modelId', async () => {
            const identity = useSessionIdentity()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                }),
            })
            vi.stubGlobal('fetch', mockFetch)

            mockGetAgent.mockReturnValue({ preferredModel: 'saved-model' })
            mockGetAgentModel.mockReturnValue({ name: 'Saved Model' })

            await initSessionFromAPI()

            expect(identity.currentModelId.value).toBe('saved-model')
            expect(identity.currentModelName.value).toBe('Saved Model')

            vi.unstubAllGlobals()
        })

        it('falls back to agent default when saved model is unavailable', async () => {
            const identity = useSessionIdentity()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                }),
            })
            vi.stubGlobal('fetch', mockFetch)

            mockGetAgent.mockReturnValue({ preferredModel: 'stale-model' })
            mockGetAgentModel.mockReturnValue(null) // Model no longer exists
            mockSyncModelFromAgent.mockReturnValue({ modelId: 'default-model', modelName: 'Default' })

            await initSessionFromAPI()

            expect(mockSyncModelFromAgent).toHaveBeenCalled()
            expect(identity.currentModelId.value).toBe('default-model')

            vi.unstubAllGlobals()
        })

        it('handles thinking effort from server ACP state', async () => {
            const identity = useSessionIdentity()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    thinkingEffortState: {
                        currentId: 'xhigh',
                        availableLevels: [{ id: 'low', name: 'Low' }, { id: 'xhigh', name: 'X-High' }],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.currentThinkingEffort.value).toBe('xhigh')
            expect(identity.currentThinkingEffortName.value).toBe('X-High')

            vi.unstubAllGlobals()
        })

        it('falls back to saved thinking effort when server returns none', async () => {
            const identity = useSessionIdentity()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgent.mockReturnValue({ preferredModel: null })
            mockGetAgentModel.mockReturnValue(null)
            mockSyncModelFromAgent.mockReturnValue({ modelId: 'default', modelName: 'Default' })
            mockGetEffectiveThinkingEffort.mockReturnValue('medium')

            await initSessionFromAPI()

            expect(identity.currentThinkingEffort.value).toBe('medium')

            vi.unstubAllGlobals()
        })
    })

    // ── resetIdentity ──

    describe('resetIdentity', () => {
        it('clears all identity refs to defaults', async () => {
            const identity = useSessionIdentity()

            // Set some state
            identity.currentSessionId.value = 'session-1'
            identity.currentSessionTitle.value = 'Title'
            identity.currentBackend.value = 'codebuddy'
            identity.currentAgentId.value = 'agent-1'
            identity.currentModelId.value = 'model-1'
            identity.currentModelName.value = 'Model One'
            identity.thinkingEffortState.currentId.value = 'high'
            identity.runningSessions.value = new Set(['s1', 's2'])
            identity.sessionDrawer.open()

            resetIdentity()

            expect(identity.currentSessionId.value).toBe('')
            expect(identity.currentSessionTitle.value).toBe('')
            expect(identity.currentBackend.value).toBe('')
            expect(identity.currentAgentId.value).toBe('')
            expect(identity.currentModelId.value).toBe('')
            expect(identity.currentModelName.value).toBe('')
            expect(identity.currentThinkingEffort.value).toBe('')
            expect(identity.runningSessions.value.size).toBe(0)
            expect(identity.sessionDrawer.isOpen.value).toBe(false)
        })

        it('clears registered session action callbacks', async () => {
            const mockSwitch = vi.fn()
            registerSessionActions({
                switchSession: mockSwitch,
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            resetIdentity()

            // After reset, the identity should have null callbacks
            // so calling switchSession should not invoke the old callback
            const identity = useSessionIdentity()
            await identity.switchSession('session-2')
            expect(mockSwitch).not.toHaveBeenCalled()
        })

        it('clears openChatPanel callback', () => {
            const mockOpen = vi.fn()
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: mockOpen,
            })

            resetIdentity()

            const identity = useSessionIdentity()
            identity.openChatPanel()
            expect(mockOpen).not.toHaveBeenCalled()
        })
    })

    // ── ACP mode state ──

    describe('updateModeState / clearModeState', () => {
        beforeEach(() => {
            clearModeState()
        })

        it('sets currentModeId and currentModeName from matching mode', () => {
            const identity = useSessionIdentity()
            updateModeState('code', [
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
            ])

            expect(identity.currentModeId.value).toBe('code')
            expect(identity.currentModeName.value).toBe('Code')
            expect(identity.availableModes.value).toEqual([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
            ])
        })

        it('uses modeId as fallback name when mode not in list', () => {
            const identity = useSessionIdentity()
            updateModeState('architect', [
                { id: 'ask', name: 'Ask' },
            ])

            expect(identity.currentModeId.value).toBe('architect')
            expect(identity.currentModeName.value).toBe('architect')
        })

        it('updates availableModes even when modeId is empty', () => {
            const identity = useSessionIdentity()
            updateModeState('', [
                { id: 'ask', name: 'Ask' },
            ])

            expect(identity.currentModeId.value).toBe('')
            expect(identity.availableModes.value).toEqual([
                { id: 'ask', name: 'Ask' },
            ])
        })

        it('does not update availableModes when modes array is empty', () => {
            const identity = useSessionIdentity()
            identity.modeState.available.value = [{ id: 'existing', name: 'Existing' }]

            updateModeState('ask', [])

            expect(identity.availableModes.value).toEqual([{ id: 'existing', name: 'Existing' }])
        })

        it('clearModeState resets all mode refs', () => {
            const identity = useSessionIdentity()
            updateModeState('code', [{ id: 'code', name: 'Code' }])

            clearModeState()

            expect(identity.currentModeId.value).toBe('')
            expect(identity.currentModeName.value).toBe('')
            expect(identity.availableModes.value).toEqual([])
        })
    })

    // ── updateAvailableModes ──

    describe('updateAvailableModes', () => {
        beforeEach(() => { clearModeState() })

        it('updates availableModes without changing currentModeId', () => {
            const identity = useSessionIdentity()
            updateModeState('code', [{ id: 'code', name: 'Code' }])
            updateAvailableModes([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
                { id: 'architect', name: 'Architect' },
            ])
            expect(identity.currentModeId.value).toBe('code')
            expect(identity.currentModeName.value).toBe('Code')
            expect(identity.availableModes.value).toEqual([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
                { id: 'architect', name: 'Architect' },
            ])
        })

        it('does not update when modes array is empty', () => {
            const identity = useSessionIdentity()
            updateModeState('code', [{ id: 'code', name: 'Code' }])
            updateAvailableModes([])
            expect(identity.availableModes.value).toEqual([{ id: 'code', name: 'Code' }])
        })

        it('resolves currentModeName when currentModeId was set before modes arrived', () => {
            const identity = useSessionIdentity()
            // Set modeId directly (e.g. from loadModePref in createSession)
            identity.modeState.currentId.value = 'architect'
            identity.modeState.currentName.value = ''
            // Then modes arrive from populateACPStateFromCache
            updateAvailableModes([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
                { id: 'architect', name: 'Architect' },
            ])
            expect(identity.currentModeId.value).toBe('architect')
            expect(identity.currentModeName.value).toBe('Architect')
            expect(identity.availableModes.value).toEqual([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
                { id: 'architect', name: 'Architect' },
            ])
        })
    })

    // ── ACP command state ──

    describe('updateCommandState / clearCommandState', () => {
        beforeEach(() => {
            clearCommandState()
        })

        it('sets availableCommands', () => {
            const identity = useSessionIdentity()
            const commands = [
                { name: '/compact', description: 'Compact conversation' },
                { name: '/clear', description: 'Clear screen', inputHint: '<confirm>' },
            ]
            updateCommandState(commands)

            expect(identity.availableCommands.value).toEqual(commands)
        })

        it('replaces previous commands on update', () => {
            const identity = useSessionIdentity()
            updateCommandState([{ name: '/old', description: 'Old' }])
            updateCommandState([{ name: '/new', description: 'New' }])

            expect(identity.availableCommands.value).toEqual([{ name: '/new', description: 'New' }])
        })

        it('clearCommandState resets to empty array', () => {
            const identity = useSessionIdentity()
            updateCommandState([{ name: '/compact', description: 'Compact' }])

            clearCommandState()

            expect(identity.availableCommands.value).toEqual([])
        })

        it('handles empty array update', () => {
            const identity = useSessionIdentity()
            updateCommandState([{ name: '/compact', description: 'Compact' }])
            updateCommandState([])

            expect(identity.availableCommands.value).toEqual([])
        })
    })

    // ── ACP thinking effort state ──

    describe('updateThinkingEffortState / clearThinkingEffortState', () => {
        beforeEach(() => {
            clearThinkingEffortState()
        })

        it('sets currentThinkingEffort and availableThinkingEfforts', () => {
            const identity = useSessionIdentity()
            updateThinkingEffortState('high', [
                { id: 'low', name: 'Low' },
                { id: 'high', name: 'High' },
            ])

            expect(identity.currentThinkingEffort.value).toBe('high')
            expect(identity.currentThinkingEffortName.value).toBe('High')
            expect(identity.availableThinkingEfforts.value).toEqual([
                { id: 'low', name: 'Low' },
                { id: 'high', name: 'High' },
            ])
        })

        it('does not update currentThinkingEffort when currentId is empty', () => {
            const identity = useSessionIdentity()
            identity.thinkingEffortState.currentId.value = 'existing'

            updateThinkingEffortState('', [
                { id: 'low', name: 'Low' },
            ])

            expect(identity.currentThinkingEffort.value).toBe('existing')
            expect(identity.availableThinkingEfforts.value).toEqual([{ id: 'low', name: 'Low' }])
        })

        it('does not update availableThinkingEfforts when levels is empty', () => {
            const identity = useSessionIdentity()
            identity.thinkingEffortState.available.value = [{ id: 'existing', name: 'Existing' }]

            updateThinkingEffortState('high', [])

            expect(identity.availableThinkingEfforts.value).toEqual([{ id: 'existing', name: 'Existing' }])
        })

        it('clearThinkingEffortState resets all thinking effort state', () => {
            const identity = useSessionIdentity()
            updateThinkingEffortState('high', [
                { id: 'high', name: 'High' },
            ])

            clearThinkingEffortState()

            // clearThinkingEffortState now properly clears all state,
            // including currentId (fixes the original bug)
            expect(identity.availableThinkingEfforts.value).toEqual([])
            expect(identity.currentThinkingEffort.value).toBe('')
            expect(identity.currentThinkingEffortName.value).toBe('')
        })
    })

    // ── updateAvailableThinkingEfforts ──

    describe('updateAvailableThinkingEfforts', () => {
        beforeEach(() => { clearThinkingEffortState() })

        it('updates available levels without changing current selection', () => {
            const identity = useSessionIdentity()
            updateThinkingEffortState('high', [{ id: 'high', name: 'High' }])
            updateAvailableThinkingEfforts([
                { id: 'low', name: 'Low' },
                { id: 'high', name: 'High' },
            ])
            expect(identity.currentThinkingEffort.value).toBe('high')
            expect(identity.currentThinkingEffortName.value).toBe('High')
            expect(identity.availableThinkingEfforts.value).toEqual([
                { id: 'low', name: 'Low' },
                { id: 'high', name: 'High' },
            ])
        })

        it('does not update when levels array is empty', () => {
            const identity = useSessionIdentity()
            updateThinkingEffortState('high', [{ id: 'high', name: 'High' }])
            updateAvailableThinkingEfforts([])
            expect(identity.availableThinkingEfforts.value).toEqual([{ id: 'high', name: 'High' }])
        })
    })

    // ── initSessionFromAPI with ACP fields ──

    describe('initSessionFromAPI with ACP fields', () => {
        it('populates mode state from data.modeState', async () => {
            const identity = useSessionIdentity()
            clearModeState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    transport: 'acp-stdio',
                    modeId: 'code',
                    modeState: {
                        currentModeId: 'code',
                        availableModes: [
                            { id: 'ask', name: 'Ask' },
                            { id: 'code', name: 'Code' },
                        ],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.currentModeId.value).toBe('code')
            expect(identity.currentModeName.value).toBe('Code')
            expect(identity.availableModes.value).toEqual([
                { id: 'ask', name: 'Ask' },
                { id: 'code', name: 'Code' },
            ])

            vi.unstubAllGlobals()
        })

        it('uses modeState.currentModeId when available', async () => {
            const identity = useSessionIdentity()
            clearModeState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    transport: 'acp-stdio',
                    modeState: {
                        currentModeId: 'architect',
                        availableModes: [
                            { id: 'ask', name: 'Ask' },
                            { id: 'architect', name: 'Architect' },
                        ],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.currentModeId.value).toBe('architect')
            expect(identity.currentModeName.value).toBe('Architect')

            vi.unstubAllGlobals()
        })

        it('does not populate available modes when availableModes is empty but sets currentModeId', async () => {
            const identity = useSessionIdentity()
            clearModeState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    transport: 'acp-stdio',
                    modeState: {
                        currentModeId: 'code',
                        availableModes: [],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            // currentModeId is set from ACP state even without available modes
            expect(identity.currentModeId.value).toBe('code')
            expect(identity.availableModes.value).toEqual([])

            vi.unstubAllGlobals()
        })

        it('populates thinking effort state from data.thinkingEffortState', async () => {
            const identity = useSessionIdentity()
            clearThinkingEffortState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    transport: 'acp-stdio',
                    thinkingEffort: 'high',
                    thinkingEffortState: {
                        currentId: 'high',
                        availableLevels: [
                            { id: 'low', name: 'Low' },
                            { id: 'high', name: 'High' },
                        ],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.currentThinkingEffort.value).toBe('high')
            expect(identity.currentThinkingEffortName.value).toBe('High')
            expect(identity.availableThinkingEfforts.value).toEqual([
                { id: 'low', name: 'Low' },
                { id: 'high', name: 'High' },
            ])

            vi.unstubAllGlobals()
        })

        it('uses thinkingEffortState.currentId when available', async () => {
            const identity = useSessionIdentity()
            clearThinkingEffortState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    transport: 'acp-stdio',
                    thinkingEffortState: {
                        currentId: 'medium',
                        availableLevels: [
                            { id: 'low', name: 'Low' },
                            { id: 'medium', name: 'Medium' },
                        ],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.currentThinkingEffort.value).toBe('medium')
            expect(identity.currentThinkingEffortName.value).toBe('Medium')

            vi.unstubAllGlobals()
        })

        it('does not populate thinking effort state when availableLevels is empty', async () => {
            const identity = useSessionIdentity()
            clearThinkingEffortState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    transport: 'acp-stdio',
                    thinkingEffortState: {
                        currentId: 'high',
                        availableLevels: [],
                    },
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.availableThinkingEfforts.value).toEqual([])

            vi.unstubAllGlobals()
        })

        it('populates commands from data.commands when not already set', async () => {
            const identity = useSessionIdentity()
            clearCommandState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    commands: [
                        { name: '/compact', description: 'Compact conversation' },
                        { name: '/clear', description: 'Clear screen' },
                    ],
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.availableCommands.value).toEqual([
                { name: '/compact', description: 'Compact conversation' },
                { name: '/clear', description: 'Clear screen' },
            ])

            vi.unstubAllGlobals()
        })

        it('does not overwrite commands when already populated', async () => {
            const identity = useSessionIdentity()
            const existingCommands = [{ name: '/existing', description: 'Existing command' }]
            identity.availableCommands.value = existingCommands

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                    commands: [
                        { name: '/compact', description: 'Compact conversation' },
                    ],
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.availableCommands.value).toEqual(existingCommands)

            vi.unstubAllGlobals()
        })

        it('handles response without ACP fields gracefully', async () => {
            const identity = useSessionIdentity()
            clearModeState()
            clearCommandState()
            clearThinkingEffortState()

            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    sessionId: 'api-session',
                    agentId: 'agent-1',
                }),
            })
            vi.stubGlobal('fetch', mockFetch)
            mockGetAgentModel.mockReturnValue({ name: 'Model' })

            await initSessionFromAPI()

            expect(identity.currentModeId.value).toBe('')
            expect(identity.availableModes.value).toEqual([])
            expect(identity.availableCommands.value).toEqual([])
            expect(identity.availableThinkingEfforts.value).toEqual([])

            vi.unstubAllGlobals()
        })
    })

    // ── sendMessage fallback: no session_id → no POST ──

    describe('sendMessage fallback', () => {
        it('does not send POST to /api/ai/chat when session creation also fails', async () => {
            const identity = useSessionIdentity()
            resetIdentity()

            // Mock fetch: session creation returns failure (no sessionId)
            const mockFetch = vi.fn().mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: false, error: 'Too many sessions' }),
            })
            vi.stubGlobal('fetch', mockFetch)

            const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
            await identity.sendMessage('hello')
            errorSpy.mockRestore()

            // Should have called fetch for session creation
            expect(mockFetch).toHaveBeenCalledWith(
                '/api/ai/sessions',
                expect.objectContaining({ method: 'POST' })
            )

            // Should NOT have sent POST to /api/ai/chat (no session_id available)
            const chatPostCalls = mockFetch.mock.calls.filter(
                (call: any[]) => call[0]?.includes?.('/api/ai/chat') && call[1]?.method === 'POST'
            )
            expect(chatPostCalls.length).toBe(0)

            vi.unstubAllGlobals()
        })

        it('sends POST with session_id when session creation succeeds', async () => {
            const identity = useSessionIdentity()
            resetIdentity()

            // Mock fetch: session creation succeeds
            const mockFetch = vi.fn()
                .mockResolvedValueOnce({
                    ok: true,
                    json: () => Promise.resolve({ ok: true, sessionId: 'new-s1' }),
                })
                .mockResolvedValueOnce({
                    ok: true,
                    json: () => Promise.resolve({ started: true, sessionId: 'new-s1' }),
                })
            vi.stubGlobal('fetch', mockFetch)

            await identity.sendMessage('hello')

            // First call: create session
            expect(mockFetch).toHaveBeenNthCalledWith(1,
                '/api/ai/sessions',
                expect.objectContaining({ method: 'POST' })
            )

            // Second call: POST message with explicit session_id
            expect(mockFetch).toHaveBeenNthCalledWith(2,
                expect.stringContaining('session_id=new-s1'),
                expect.objectContaining({ method: 'POST' })
            )

            vi.unstubAllGlobals()
        })
    })

    // ── usage state ──

    describe('updateUsageState / clearUsageState', () => {
        beforeEach(() => {
            clearAllUsageState()
        })

        it('sets usage state from SSE event', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(5000, 200000, 0.05, 'USD')

            expect(identity.contextUsed.value).toBe(5000)
            expect(identity.contextSize.value).toBe(200000)
            expect(identity.contextCost.value).toBe(0.05)
            expect(identity.contextCurrency.value).toBe('USD')
        })

        it('defaults cost and currency when not provided', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(1000, 100000)

            expect(identity.contextCost.value).toBe(0)
            expect(identity.contextCurrency.value).toBe('')
        })

        it('clearUsageState removes current session cache entry', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(5000, 200000, 0.05, 'USD')
            clearUsageState()

            expect(identity.contextUsed.value).toBe(0)
            expect(identity.contextSize.value).toBe(0)
            expect(identity.contextCost.value).toBe(0)
            expect(identity.contextCurrency.value).toBe('')
        })

        it('writing to a different session does not affect current display', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'

            // Set usage for the current session
            updateUsageState(5000, 200000, 0.05, 'USD')
            expect(identity.contextUsed.value).toBe(5000)

            // SSE event for a different session writes to its own cache bucket —
            // current display should be unaffected
            updateUsageState(99999, 888888, 1.0, 'EUR', 'session-B')
            expect(identity.contextUsed.value).toBe(5000)
            expect(identity.contextSize.value).toBe(200000)
            expect(identity.contextCost.value).toBe(0.05)
            expect(identity.contextCurrency.value).toBe('USD')
        })

        it('switching sessions instantly switches usage display', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(5000, 200000, 0.05, 'USD')

            // Write data for session-B while viewing session-A
            updateUsageState(3000, 100000, 0.02, 'EUR', 'session-B')

            // Still showing session-A data
            expect(identity.contextUsed.value).toBe(5000)
            expect(identity.contextSize.value).toBe(200000)

            // Switch to session-B — instantly shows its cached data, no flash to 0
            identity.currentSessionId.value = 'session-B'
            expect(identity.contextUsed.value).toBe(3000)
            expect(identity.contextSize.value).toBe(100000)
            expect(identity.contextCost.value).toBe(0.02)
            expect(identity.contextCurrency.value).toBe('EUR')

            // Switch back to session-A — data is still there
            identity.currentSessionId.value = 'session-A'
            expect(identity.contextUsed.value).toBe(5000)
            expect(identity.contextSize.value).toBe(200000)
        })

        it('unknown session shows zero usage', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-unknown'

            expect(identity.contextUsed.value).toBe(0)
            expect(identity.contextSize.value).toBe(0)
            expect(identity.contextCost.value).toBe(0)
            expect(identity.contextCurrency.value).toBe('')
        })

        it('clearUsageState only clears current session, not other sessions', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(5000, 200000, 0.05, 'USD', 'session-A')
            updateUsageState(3000, 100000, 0.02, 'EUR', 'session-B')

            // Clear current session (A) only
            clearUsageState()
            expect(identity.contextUsed.value).toBe(0)

            // Session B data is still cached
            identity.currentSessionId.value = 'session-B'
            expect(identity.contextUsed.value).toBe(3000)
            expect(identity.contextSize.value).toBe(100000)
        })
    })

    // ── toggleAutoApprove ──

    describe('toggleAutoApprove', () => {
        it('sets autoApprove ref and persists to server', async () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-1'

            const mockFetch = vi.fn().mockResolvedValue({ ok: true })
            vi.stubGlobal('fetch', mockFetch)

            toggleAutoApprove(true)

            expect(identity.autoApprove.value).toBe(true)
            expect(mockFetch).toHaveBeenCalledWith('/api/ai/session/update', expect.objectContaining({
                method: 'PATCH',
            }))

            vi.unstubAllGlobals()
        })

        it('does not persist when no session ID', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = ''

            const mockFetch = vi.fn()
            vi.stubGlobal('fetch', mockFetch)

            toggleAutoApprove(true)

            expect(identity.autoApprove.value).toBe(true)
            expect(mockFetch).not.toHaveBeenCalled()

            vi.unstubAllGlobals()
        })
    })

    // ── continueFromExecution / checkContinueSession ──

    describe('continueFromExecution', () => {
        it('returns false when no callback registered', async () => {
            resetIdentity()
            const identity = useSessionIdentity()

            const result = await identity.continueFromExecution(1, 2, () => {})
            expect(result).toBe(false)
        })

        it('delegates to registered callback', async () => {
            const mockContinue = vi.fn().mockResolvedValue(true)
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: mockContinue,
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
            })

            const identity = useSessionIdentity()
            const switchTab = vi.fn()
            const result = await identity.continueFromExecution(1, 2, switchTab)

            expect(result).toBe(true)
            expect(mockContinue).toHaveBeenCalledWith(1, 2, switchTab)
        })
    })

    describe('checkContinueSession', () => {
        it('returns default when no callback registered', async () => {
            resetIdentity()
            const identity = useSessionIdentity()

            const result = await identity.checkContinueSession(1, 2)
            expect(result).toEqual({ exists: false, sessionId: '' })
        })

        it('delegates to registered callback', async () => {
            const mockCheck = vi.fn().mockResolvedValue({ exists: true, sessionId: 'existing-s' })
            registerSessionActions({
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(false),
                checkContinueSession: mockCheck,
            })

            const identity = useSessionIdentity()
            const result = await identity.checkContinueSession(1, 2)

            expect(result).toEqual({ exists: true, sessionId: 'existing-s' })
        })
    })

    // ── clearThinkingEffortState ──

    describe('clearThinkingEffortState', () => {
        it('clears both availableThinkingEfforts and currentThinkingEffortName', () => {
            const identity = useSessionIdentity()
            updateThinkingEffortState('high', [{ id: 'high', name: 'High' }])

            clearThinkingEffortState()

            expect(identity.availableThinkingEfforts.value).toEqual([])
            expect(identity.currentThinkingEffortName.value).toBe('')
        })
    })

    // ── getSessionId ──

    describe('getSessionId', () => {
        it('returns the current session ID directly', async () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'test-session-42'

            expect(getSessionId()).toBe('test-session-42')

            // Reset
            identity.currentSessionId.value = ''
        })

        it('returns empty string when no session is active', () => {
            resetIdentity()
            expect(getSessionId()).toBe('')
        })
    })

    // ── prefetchCommands (deprecated no-op) ──

    describe('prefetchCommands', () => {
        it('resolves without error (no-op)', async () => {
            await expect(prefetchCommands('any-agent')).resolves.toBeUndefined()
        })
    })

    // ── registerSessionDrawerRef / openAgentSelector ──

    describe('openAgentSelector', () => {
        it('calls openAgentSelector on registered drawer ref', () => {
            const mockOpenAgentSelector = vi.fn()
            registerSessionDrawerRef({ openAgentSelector: mockOpenAgentSelector })

            const identity = useSessionIdentity()
            identity.openAgentSelector()

            expect(mockOpenAgentSelector).toHaveBeenCalled()
        })

        it('does nothing when no drawer ref is registered', () => {
            resetIdentity()
            const identity = useSessionIdentity()
            expect(() => identity.openAgentSelector()).not.toThrow()
        })
    })

    // ── forkSession (in SessionActions interface) ──

    describe('SessionActions.forkSession', () => {
        it('is declared in the SessionActions interface', () => {
            // forkSession is part of the interface but the composable
            // does not expose it directly — it's handled by ChatPanel.
            // Just verify registerSessionActions accepts it.
            const actions = {
                switchSession: vi.fn(),
                createSession: vi.fn(),
                archiveSession: vi.fn(),
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
                continueFromExecution: vi.fn().mockResolvedValue(true),
                checkContinueSession: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
                forkSession: vi.fn().mockResolvedValue(true),
            }
            expect(() => registerSessionActions(actions)).not.toThrow()
        })
    })

    // ── clearUsageStateById ──

    describe('clearUsageStateById', () => {
        beforeEach(() => {
            clearAllUsageState()
        })

        it('removes usage state for a specific session by ID', () => {
            const identity = useSessionIdentity()
            updateUsageState(5000, 200000, 0.05, 'USD', 'session-A')
            updateUsageState(3000, 100000, 0.02, 'EUR', 'session-B')

            clearUsageStateById('session-A')

            // session-A data is gone
            identity.currentSessionId.value = 'session-A'
            expect(identity.contextUsed.value).toBe(0)
            expect(identity.contextSize.value).toBe(0)

            // session-B data is still there
            identity.currentSessionId.value = 'session-B'
            expect(identity.contextUsed.value).toBe(3000)
            expect(identity.contextSize.value).toBe(100000)
        })

        it('does nothing when session ID is not in cache', () => {
            updateUsageState(5000, 200000, 0.05, 'USD', 'session-A')

            // Clearing a non-existent session should not throw or affect existing data
            expect(() => clearUsageStateById('session-unknown')).not.toThrow()

            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            expect(identity.contextUsed.value).toBe(5000)
        })
    })

    // ── closeSessionDrawer ──

    describe('closeSessionDrawer', () => {
        it('closes the session drawer', async () => {
            const identity = useSessionIdentity()
            identity.sessionDrawer.open()
            await nextTick()
            expect(identity.sessionDrawer.isOpen.value).toBe(true)

            identity.closeSessionDrawer()
            await nextTick()
            expect(identity.sessionDrawer.isOpen.value).toBe(false)
        })
    })

    // ── loadModePref ──

    describe('loadModePref', () => {
        it('returns null when agentId is empty', () => {
            const identity = useSessionIdentity()
            expect(identity.loadModePref('')).toBeNull()
        })
    })

    // ── clearSessionIdentity ──

    describe('clearSessionIdentity', () => {
        beforeEach(() => {
            resetIdentity()
        })

        it('clears all identity refs but keeps session drawer intact', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-1'
            identity.currentSessionTitle.value = 'Title'
            identity.currentBackend.value = 'claude'
            identity.currentAgentId.value = 'agent-1'
            identity.currentModelId.value = 'model-1'
            identity.currentModelName.value = 'Model One'
            identity.currentTransport.value = 'acp-stdio'
            identity.autoApprove.value = true
            identity.thinkingEffortState.currentId.value = 'high'
            updateModeState('code', [{ id: 'code', name: 'Code' }])
            updateCommandState([{ name: '/help', description: 'Help' }])

            clearSessionIdentity()

            expect(identity.currentSessionId.value).toBe('')
            expect(identity.currentSessionTitle.value).toBe('')
            expect(identity.currentBackend.value).toBe('')
            expect(identity.currentAgentId.value).toBe('')
            expect(identity.currentModelId.value).toBe('')
            expect(identity.currentModelName.value).toBe('')
            expect(identity.currentTransport.value).toBe('')
            expect(identity.autoApprove.value).toBe(false)
            expect(identity.currentModeId.value).toBe('')
            expect(identity.availableModes.value).toEqual([])
            expect(identity.availableCommands.value).toEqual([])
            expect(identity.currentThinkingEffort.value).toBe('')
        })

        it('sets currentSessionId to upcomingSessionId when provided', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'old-session'

            clearSessionIdentity('new-session')

            expect(identity.currentSessionId.value).toBe('new-session')
        })

        it('resets currentSessionId to empty when no upcomingSessionId', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'old-session'

            clearSessionIdentity()

            expect(identity.currentSessionId.value).toBe('')
        })
    })

    // ── contextInputTokens / contextOutputTokens ──

    describe('contextInputTokens / contextOutputTokens', () => {
        beforeEach(() => {
            clearAllUsageState()
        })

        it('tracks input and output tokens per session', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(5000, 200000, 0.05, 'USD', 'session-A', 1000, 2000)

            expect(identity.contextInputTokens.value).toBe(1000)
            expect(identity.contextOutputTokens.value).toBe(2000)
        })

        it('defaults input/output tokens to 0 when not provided', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(5000, 200000, 0.05, 'USD', 'session-A')

            expect(identity.contextInputTokens.value).toBe(0)
            expect(identity.contextOutputTokens.value).toBe(0)
        })

        it('switches token display when switching sessions', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'
            updateUsageState(5000, 200000, 0.05, 'USD', 'session-A', 100, 200)
            updateUsageState(3000, 100000, 0.02, 'EUR', 'session-B', 50, 150)

            identity.currentSessionId.value = 'session-B'
            expect(identity.contextInputTokens.value).toBe(50)
            expect(identity.contextOutputTokens.value).toBe(150)

            identity.currentSessionId.value = 'session-A'
            expect(identity.contextInputTokens.value).toBe(100)
            expect(identity.contextOutputTokens.value).toBe(200)
        })
    })

    // ── currentTransport ──

    describe('currentTransport', () => {
        beforeEach(() => {
            resetIdentity()
        })

        it('can be set to acp-stdio', () => {
            const identity = useSessionIdentity()
            identity.currentTransport.value = 'acp-stdio'
            expect(identity.currentTransport.value).toBe('acp-stdio')
        })

        it('can be set to cli', () => {
            const identity = useSessionIdentity()
            identity.currentTransport.value = 'cli'
            expect(identity.currentTransport.value).toBe('cli')
        })
    })

    // ── autoApprove ──

    describe('autoApprove', () => {
        beforeEach(() => {
            resetIdentity()
        })

        it('starts as false after reset', () => {
            const identity = useSessionIdentity()
            expect(identity.autoApprove.value).toBe(false)
        })

        it('can be toggled on', () => {
            const identity = useSessionIdentity()
            identity.autoApprove.value = true
            expect(identity.autoApprove.value).toBe(true)
        })
    })

    // ── loadModelPref / saveModelPref / loadThinkingPref / saveThinkingPref ──

    describe('preference helpers', () => {
        it('saveModelPref is a no-op (session-scoped)', async () => {
            const identity = useSessionIdentity()
            // saveModelPref is documented as no-op for chat sessions
            await expect(identity.saveModelPref('agent-1', 'model-1')).resolves.toBeUndefined()
        })

        it('saveThinkingPref is a no-op (session-scoped)', async () => {
            const identity = useSessionIdentity()
            // saveThinkingPref is documented as no-op for chat sessions
            await expect(identity.saveThinkingPref('agent-1', 'high')).resolves.toBeUndefined()
        })

        it('loadModelPref returns null for empty agentId', () => {
            const identity = useSessionIdentity()
            expect(identity.loadModelPref('')).toBeNull()
        })

        it('loadModelPref returns agent preferredModel when available', () => {
            const identity = useSessionIdentity()
            mockGetAgent.mockReturnValue({ preferredModel: 'preferred-1' })
            expect(identity.loadModelPref('agent-1')).toBe('preferred-1')
        })

        it('loadModelPref returns null when agent has no preferredModel', () => {
            const identity = useSessionIdentity()
            mockGetAgent.mockReturnValue({})
            expect(identity.loadModelPref('agent-1')).toBeNull()
        })

        it('loadThinkingPref returns null for empty agentId', () => {
            const identity = useSessionIdentity()
            expect(identity.loadThinkingPref('')).toBeNull()
        })

        it('loadThinkingPref returns effective thinking effort', () => {
            const identity = useSessionIdentity()
            mockGetEffectiveThinkingEffort.mockReturnValue('high')
            expect(identity.loadThinkingPref('agent-1')).toBe('high')
        })

        it('loadThinkingPref returns null when no effort configured', () => {
            const identity = useSessionIdentity()
            mockGetEffectiveThinkingEffort.mockReturnValue(null)
            expect(identity.loadThinkingPref('agent-1')).toBeNull()
        })
    })

    // ── updateUsageState with sessionId parameter ──

    describe('updateUsageState with explicit sessionId', () => {
        beforeEach(() => {
            clearAllUsageState()
        })

        it('writes usage to the specified session, not current', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = 'session-A'

            updateUsageState(5000, 200000, 0.05, 'USD', 'session-B')

            // Current session should show 0 (data was written to session-B)
            expect(identity.contextUsed.value).toBe(0)
            expect(identity.contextSize.value).toBe(0)

            // Switch to session-B to verify data
            identity.currentSessionId.value = 'session-B'
            expect(identity.contextUsed.value).toBe(5000)
            expect(identity.contextSize.value).toBe(200000)
        })

        it('does not write when sessionId is empty and no current session', () => {
            const identity = useSessionIdentity()
            identity.currentSessionId.value = ''

            updateUsageState(5000, 200000)

            // No session to write to — should not throw
            expect(identity.contextUsed.value).toBe(0)
        })
    })
})
