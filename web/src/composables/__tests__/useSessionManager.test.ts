import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref, nextTick } from 'vue'

// Mock dependencies
const mockCurrentSessionId = ref('session-1')
const mockCurrentBackend = ref('claude')
const mockRunningSessions = ref(new Set<string>())

vi.mock('@/composables/useSessionIdentity', () => ({
    useSessionIdentity: () => ({
        currentSessionId: mockCurrentSessionId,
        currentBackend: mockCurrentBackend,
        registerSessionActions: vi.fn(),
    }),
    get runningSessions() { return mockRunningSessions },
}))

const mockCancelChat = vi.fn()
vi.mock('@/utils/api', () => ({
    cancelChat: (...args: any[]) => mockCancelChat(...args),
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
    useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useLocale', () => ({
    gt: (key: string) => key,
}))

vi.mock('vue', async () => {
    const actual = await vi.importActual('vue')
    return {
        ...actual,
        onUnmounted: vi.fn(),
    }
})

import { useSessionManager } from '@/composables/useSessionManager'
import { usePendingStore } from '@/composables/usePendingStore'

function createMockOptions() {
    const messages = ref<any[]>([])
    const loading = ref(false)
    const switchSessionCore = vi.fn()
    const createSessionCore = vi.fn()
    const deleteSessionCore = vi.fn()
    const disconnectStream = vi.fn()
    const stopPolling = vi.fn()
    const updateRenderedContents = vi.fn()
    const clearInputState = vi.fn()
    const scrollBottom = vi.fn()
    const pendingStore = usePendingStore()

    return {
        messages, loading, pendingStore,
        switchSessionCore, createSessionCore, deleteSessionCore,
        continueFromExecutionCore: vi.fn().mockResolvedValue(true),
        forkSessionCore: vi.fn().mockResolvedValue(true),
        checkContinueSessionCore: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
        disconnectStream, stopPolling,
        updateRenderedContents, clearInputState, scrollBottom,
        sendMessageNow: vi.fn().mockResolvedValue(undefined),
    }
}

describe('useSessionManager', () => {
    beforeEach(() => {
        vi.clearAllMocks()
        mockCurrentSessionId.value = 'session-1'
        mockCurrentBackend.value = 'claude'
        mockRunningSessions.value = new Set()
        mockCancelChat.mockResolvedValue(undefined)
    })

    // ── cleanupActiveStream ──

    describe('cleanupActiveStream', () => {
        it('returns early when not loading', () => {
            const opts = createMockOptions()
            opts.loading.value = false
            const mgr = useSessionManager(opts)

            mgr.cleanupActiveStream()

            expect(opts.disconnectStream).not.toHaveBeenCalled()
            expect(opts.stopPolling).not.toHaveBeenCalled()
        })

        it('disconnects stream and stops polling when loading', () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const mgr = useSessionManager(opts)

            mgr.cleanupActiveStream()

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(opts.stopPolling).toHaveBeenCalled()
        })

        it('removes streaming flag from assistant messages', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const streamingMsg = { role: 'assistant', streaming: true, blocks: [] }
            opts.messages.value = [streamingMsg]
            const mgr = useSessionManager(opts)

            mgr.cleanupActiveStream()

            expect(streamingMsg.streaming).toBeUndefined()
        })

        it('marks undone tool_use blocks as done', () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const streamingMsg = {
                role: 'assistant', streaming: true,
                blocks: [
                    { type: 'text', content: 'hello' },
                    { type: 'tool_use', done: false },
                    { type: 'tool_use', done: true },
                ],
            }
            opts.messages.value = [streamingMsg]
            const mgr = useSessionManager(opts)

            mgr.cleanupActiveStream()

            expect(streamingMsg.blocks[1].done).toBe(true)
            expect(streamingMsg.blocks[2].done).toBe(true) // was already true
        })

        it('calls updateRenderedContents with forceFull=true', () => {
            const opts = createMockOptions()
            opts.loading.value = true
            opts.messages.value = [{ role: 'assistant', streaming: true, blocks: [] }]
            const mgr = useSessionManager(opts)

            mgr.cleanupActiveStream()

            expect(opts.updateRenderedContents).toHaveBeenCalledWith(true)
        })

        it('does not touch non-assistant or non-streaming messages', () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const userMsg = { role: 'user', content: 'hi' }
            const nonStreamingAssistant = { role: 'assistant', blocks: [] }
            opts.messages.value = [userMsg, nonStreamingAssistant]
            const mgr = useSessionManager(opts)

            mgr.cleanupActiveStream()

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(userMsg.role).toBe('user')
            expect((nonStreamingAssistant as any).streaming).toBeUndefined()
        })
    })

    // ── switchSession ──

    describe('switchSession', () => {
        it('calls cleanupActiveStream then switchSessionCore', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const mgr = useSessionManager(opts)

            await mgr.switchSession('session-2')

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(opts.switchSessionCore).toHaveBeenCalledWith('session-2')
        })
    })

    // ── createSession ──

    describe('createSession', () => {
        it('clears pending messages from pendingStore before creating', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'old', blocks: [], files: [], createdAt: '', pending: true,
            })
            const mgr = useSessionManager(opts)

            await mgr.createSession('agent-1')

            expect(opts.pendingStore.getPending('session-1')).toHaveLength(0)
            expect(opts.createSessionCore).toHaveBeenCalledWith('agent-1')
        })

        it('calls cleanup before creating', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const mgr = useSessionManager(opts)

            await mgr.createSession()

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(opts.createSessionCore).toHaveBeenCalled()
        })
    })

    // ── deleteSession ──

    describe('deleteSession', () => {
        it('calls cleanup then clears queue then deletes', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.deleteSession('session-2', 'claude')

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('/api/ai/queue?session_id=session-2'),
                { method: 'DELETE' },
            )
            expect(opts.deleteSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })

        it('continues with delete even if queue clear fails', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.deleteSession('session-2')

            expect(opts.deleteSessionCore).toHaveBeenCalledWith('session-2', undefined)

            fetchSpy.mockRestore()
        })

        it('cancels running session before deleting', async () => {
            const opts = createMockOptions()
            mockRunningSessions.value = new Set(['session-2'])
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.deleteSession('session-2', 'claude')

            expect(mockCancelChat).toHaveBeenCalledWith('session-2')
            expect(opts.deleteSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })

        it('does not cancel non-running session before deleting', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.deleteSession('session-2', 'claude')

            expect(mockCancelChat).not.toHaveBeenCalled()
            expect(opts.deleteSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })

        it('continues with delete even if cancel fails', async () => {
            const opts = createMockOptions()
            mockRunningSessions.value = new Set(['session-2'])
            mockCancelChat.mockRejectedValue(new Error('cancel fail'))
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.deleteSession('session-2', 'claude')

            expect(opts.deleteSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })
    })

    // ── deleteCurrentSession ──

    describe('deleteCurrentSession', () => {
        it('returns early if no current session', async () => {
            const opts = createMockOptions()
            mockCurrentSessionId.value = ''
            const mgr = useSessionManager(opts)

            const deleteDraft = vi.fn()
            await mgr.deleteCurrentSession(deleteDraft)

            expect(opts.deleteSessionCore).not.toHaveBeenCalled()
            expect(deleteDraft).not.toHaveBeenCalled()
        })

        it('clears pending messages from pendingStore, deletes session and draft', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'pending', blocks: [], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)
            const deleteDraft = vi.fn()

            await mgr.deleteCurrentSession(deleteDraft)

            expect(opts.pendingStore.getPending('session-1')).toHaveLength(0)
            expect(opts.deleteSessionCore).toHaveBeenCalledWith('session-1', 'claude')
            expect(deleteDraft).toHaveBeenCalledWith('session-1')

            fetchSpy.mockRestore()
        })

        it('cancels running current session before deleting', async () => {
            const opts = createMockOptions()
            mockRunningSessions.value = new Set(['session-1'])
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)
            const deleteDraft = vi.fn()

            await mgr.deleteCurrentSession(deleteDraft)

            expect(mockCancelChat).toHaveBeenCalledWith('session-1')
            expect(opts.deleteSessionCore).toHaveBeenCalledWith('session-1', 'claude')

            fetchSpy.mockRestore()
        })
    })

    // ── fetchQueue ──

    describe('fetchQueue', () => {
        it('returns early for empty sessionId', async () => {
            const opts = createMockOptions()
            const mgr = useSessionManager(opts)

            await mgr.fetchQueue('')

            // No fetch call
        })

        it('fetches queue and syncs pending messages via pendingStore.syncFromBackendQueue', async () => {
            const opts = createMockOptions()
            const queue = [{ text: 'hello' }]
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue }),
            } as Response)
            const syncSpy = vi.spyOn(opts.pendingStore, 'syncFromBackendQueue')
            const mgr = useSessionManager(opts)

            await mgr.fetchQueue('session-1')

            expect(syncSpy).toHaveBeenCalledWith('session-1', queue)

            fetchSpy.mockRestore()
        })

        it('handles fetch error gracefully', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const syncSpy = vi.spyOn(opts.pendingStore, 'syncFromBackendQueue')
            const mgr = useSessionManager(opts)

            await mgr.fetchQueue('session-1')

            // No crash, syncFromBackendQueue not called
            expect(syncSpy).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('handles non-ok response gracefully', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: false,
            } as Response)
            const syncSpy = vi.spyOn(opts.pendingStore, 'syncFromBackendQueue')
            const mgr = useSessionManager(opts)

            await mgr.fetchQueue('session-1')

            expect(syncSpy).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })
    })

    // ── enqueueMessage ──

    describe('enqueueMessage', () => {
        it('posts message and syncs pending messages via pendingStore.syncFromBackendQueue', async () => {
            const opts = createMockOptions()
            const queue = [{ text: 'enqueued' }]
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: true, queue }),
            } as Response)
            const syncSpy = vi.spyOn(opts.pendingStore, 'syncFromBackendQueue')
            const mgr = useSessionManager(opts)
            // Clear calls from immediate watch (fetchQueue on mount)
            fetchSpy.mockClear()
            syncSpy.mockClear()

            await mgr.enqueueMessage('hello', ['/path1'], ['attached'], ['pending'])

            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('/api/ai/queue?session_id=session-1'),
                expect.objectContaining({ method: 'POST' }),
            )
            const body = JSON.parse((fetchSpy.mock.calls[0] as any[])[1].body)
            expect(body.message).toBe('hello')
            expect(body.filePaths).toEqual(['/path1', 'attached'])
            expect(body.files).toEqual(['pending', '/path1', 'attached'])
            expect(syncSpy).toHaveBeenCalledWith('session-1', queue)

            fetchSpy.mockRestore()
        })

        it('removes stale pending message on fetch error', async () => {
            // When enqueueMessage fails, the locally-pushed pending message
            // should be removed from pendingStore so the user doesn't see a ghost entry.
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const removeSpy = vi.spyOn(opts.pendingStore, 'removePending')
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('hello')

            // The pending message should have been removed on error
            expect(removeSpy).toHaveBeenCalledWith('session-1', 'hello')
            expect(opts.pendingStore.getPending('session-1')).toHaveLength(0)

            fetchSpy.mockRestore()
        })

        it('keeps other pending messages when removing failed one on error', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'earlier', blocks: [{ type: 'text', text: 'earlier' }], files: [], createdAt: '', pending: true,
            })
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('hello')

            // Only the failed 'hello' message is removed; 'earlier' stays
            const remaining = opts.pendingStore.getPending('session-1')
            expect(remaining).toHaveLength(1)
            expect(remaining[0].content).toBe('earlier')

            fetchSpy.mockRestore()
        })

        it('shows toast on fetch error', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('hello')

            expect(mockToastShow).toHaveBeenCalledWith(
                'session.queueFailed',
                expect.objectContaining({ type: 'error' }),
            )

            fetchSpy.mockRestore()
        })

        it('calls scrollBottom after enqueue', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: true, queue: [] }),
            } as Response)
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('hello')

            expect(opts.scrollBottom).toHaveBeenCalledWith(true)

            fetchSpy.mockRestore()
        })

        it('returns needsStart=true when backend detects session not running', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    ok: true,
                    needs_start: true,
                    message: 'hello',
                    filePaths: ['/main.go'],
                    files: ['/main.go'],
                    queue: [],
                }),
            } as Response)
            const mgr = useSessionManager(opts)

            const result = await mgr.enqueueMessage('hello', [], [], [])

            expect(result.needsStart).toBe(true)
            expect(result.message).toBe('hello')
            expect(result.filePaths).toEqual(['/main.go'])
            expect(result.files).toEqual(['/main.go'])

            fetchSpy.mockRestore()
        })

        it('removes pending message from pendingStore when needsStart is true', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    ok: true,
                    needs_start: true,
                    message: 'hello',
                    filePaths: [],
                    files: [],
                    queue: [],
                }),
            } as Response)
            const removeSpy = vi.spyOn(opts.pendingStore, 'removePending')
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('hello')

            // The pending message should have been removed from pendingStore
            expect(removeSpy).toHaveBeenCalledWith('session-1', 'hello')

            fetchSpy.mockRestore()
        })

        it('returns needsStart=false on normal enqueue', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: true, queue: [{ text: 'hello' }] }),
            } as Response)
            const mgr = useSessionManager(opts)

            const result = await mgr.enqueueMessage('hello')

            expect(result.needsStart).toBe(false)

            fetchSpy.mockRestore()
        })
    })

    // ── handleRemovePending ──

    describe('handleRemovePending', () => {
        it('removes pending by pendingIndex and sends correct index to backend', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'a', blocks: [], files: [], createdAt: '', pending: true,
            })
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'b', blocks: [], files: [], createdAt: '', pending: true,
            })
            const queue = [{ text: 'remaining' }]
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue }),
            } as Response)
            const syncSpy = vi.spyOn(opts.pendingStore, 'syncFromBackendQueue')
            const mgr = useSessionManager(opts)

            // Pass pendingIndex 1 (second pending message = 'b')
            await mgr.handleRemovePending(1)

            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('index=1'),
                expect.objectContaining({ method: 'DELETE' }),
            )
            // syncFromBackendQueue called with remaining queue
            expect(syncSpy).toHaveBeenCalledWith('session-1', queue)

            fetchSpy.mockRestore()
        })

        it('returns early when pendingIndex is out of bounds', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'a', blocks: [], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch')
            const mgr = useSessionManager(opts)
            // Clear calls from immediate watch (fetchQueue on mount)
            fetchSpy.mockClear()

            // Pass index beyond pending list length
            await mgr.handleRemovePending(5)

            expect(fetchSpy).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('returns early when pendingIndex is negative', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'a', blocks: [], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch')
            const mgr = useSessionManager(opts)
            // Clear calls from immediate watch (fetchQueue on mount)
            fetchSpy.mockClear()

            await mgr.handleRemovePending(-1)

            expect(fetchSpy).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('shows toast on error', async () => {
            const opts = createMockOptions()
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'pending-msg', blocks: [], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.handleRemovePending(0)

            expect(mockToastShow).toHaveBeenCalledWith(
                'session.removeFailed',
                expect.objectContaining({ type: 'error' }),
            )

            fetchSpy.mockRestore()
        })
    })

    // ── visibility handler ──

    describe('visibility handler', () => {
        it('exposes _visibilityHandler', () => {
            const opts = createMockOptions()
            const mgr = useSessionManager(opts)

            expect(typeof mgr._visibilityHandler).toBe('function')
        })

        it('fetches queue when visible with pending messages', async () => {
            const opts = createMockOptions()
            // Put a pending message in pendingStore
            opts.pendingStore.addPending('session-1', {
                role: 'user', content: 'pending', blocks: [], files: [], createdAt: '', pending: true,
            })
            const mgr = useSessionManager(opts)

            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [] }),
            } as Response)

            // Simulate visibility change
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            mgr._visibilityHandler()

            // Wait for async fetchQueue
            await nextTick()

            expect(fetchSpy).toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('does not fetch queue when no pending messages', async () => {
            const opts = createMockOptions()
            // No pending messages in pendingStore
            const mgr = useSessionManager(opts)

            const fetchSpy = vi.spyOn(globalThis, 'fetch')

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            mgr._visibilityHandler()

            expect(fetchSpy).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })
    })

    // ── registerIdentityActions ──

    describe('registerIdentityActions', () => {
        it('registers session actions with identity', async () => {
            const opts = createMockOptions()
            const mgr = useSessionManager(opts)

            // We can't easily test the internal call to identity.registerSessionActions
            // since it's mocked, but we can verify the method exists and doesn't throw
            expect(typeof mgr.registerIdentityActions).toBe('function')

            const mockExtra = {
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
            }
            expect(() => mgr.registerIdentityActions(mockExtra)).not.toThrow()
        })
    })
})
