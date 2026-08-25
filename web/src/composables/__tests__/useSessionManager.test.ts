import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

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

function createMockOptions() {
    const messages = ref<any[]>([])
    const loading = ref(false)
    const switchSessionCore = vi.fn()
    const createSessionCore = vi.fn()
    const archiveSessionCore = vi.fn()
    const destroySessionCore = vi.fn()
    const disconnectStream = vi.fn()
    const updateRenderedContents = vi.fn()
    const clearInputState = vi.fn()
    const scrollBottom = vi.fn()
    return {
        messages, loading,
        switchSessionCore, createSessionCore, archiveSessionCore, destroySessionCore,
        continueFromExecutionCore: vi.fn().mockResolvedValue(true),
        forkSessionCore: vi.fn().mockResolvedValue(true),
        checkContinueSessionCore: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
        disconnectStream,
        updateRenderedContents, clearInputState, scrollBottom,
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
        })

        it('disconnects stream when loading', () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const mgr = useSessionManager(opts)

            mgr.cleanupActiveStream()

            expect(opts.disconnectStream).toHaveBeenCalled()
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

        it('does not explicitly clear pending messages — loadHistory replaces entire messages array', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'queued in old session', blocks: [],
                files: [], createdAt: '', pending: true,
            })
            const mgr = useSessionManager(opts)

            await mgr.switchSession('session-2')

            // Pending messages are not explicitly cleared by switchSession —
            // loadHistory's parseMessages + queueAppend replaces the entire
            // messages array, so old pending messages are naturally removed.
            // The test just verifies switchSession delegates correctly.
            expect(opts.switchSessionCore).toHaveBeenCalledWith('session-2')
        })

        it('calls switchSessionCore to load history including queue data', async () => {
            // loadHistory now includes queue data, so switchSession just
            // delegates to switchSessionCore which handles everything.
            const opts = createMockOptions()
            opts.switchSessionCore = vi.fn().mockImplementation(async () => {
                mockCurrentSessionId.value = 'session-2'
                opts.messages.value = [
                    { role: 'user', content: 'hello', id: 1 },
                    { role: 'assistant', content: 'hi', id: 2 },
                ]
            })
            const mgr = useSessionManager(opts)

            await mgr.switchSession('session-2')

            expect(opts.switchSessionCore).toHaveBeenCalledWith('session-2')
        })
    })

    // ── createSession ──

    describe('createSession', () => {
        it('clears pending messages from messages.value before creating', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'old', blocks: [], files: [], createdAt: '', pending: true,
            })
            const mgr = useSessionManager(opts)

            await mgr.createSession('agent-1')

            // Pending messages should be removed from messages.value
            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)
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

    // ── archiveSession ──

    describe('archiveSession', () => {
        it('calls cleanup then clears queue then deletes', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.archiveSession('session-2', 'claude')

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('/api/ai/queue?session_id=session-2'),
                { method: 'DELETE' },
            )
            expect(opts.archiveSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })

        it('continues with delete even if queue clear fails', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.archiveSession('session-2')

            expect(opts.archiveSessionCore).toHaveBeenCalledWith('session-2', undefined)

            fetchSpy.mockRestore()
        })

        it('cancels running session before deleting', async () => {
            const opts = createMockOptions()
            mockRunningSessions.value = new Set(['session-2'])
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.archiveSession('session-2', 'claude')

            expect(mockCancelChat).toHaveBeenCalledWith('session-2')
            expect(opts.archiveSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })

        it('does not cancel non-running session before deleting', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.archiveSession('session-2', 'claude')

            expect(mockCancelChat).not.toHaveBeenCalled()
            expect(opts.archiveSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })

        it('continues with delete even if cancel fails', async () => {
            const opts = createMockOptions()
            mockRunningSessions.value = new Set(['session-2'])
            mockCancelChat.mockRejectedValue(new Error('cancel fail'))
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.archiveSession('session-2', 'claude')

            expect(opts.archiveSessionCore).toHaveBeenCalledWith('session-2', 'claude')

            fetchSpy.mockRestore()
        })
    })

    // ── archiveCurrentSession ──

    describe('archiveCurrentSession', () => {
        it('returns early if no current session', async () => {
            const opts = createMockOptions()
            mockCurrentSessionId.value = ''
            const mgr = useSessionManager(opts)

            const deleteDraft = vi.fn()
            await mgr.archiveCurrentSession(deleteDraft)

            expect(opts.archiveSessionCore).not.toHaveBeenCalled()
            expect(deleteDraft).not.toHaveBeenCalled()
        })

        it('clears pending messages from messages.value, deletes session and draft', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'pending', blocks: [], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)
            const deleteDraft = vi.fn()

            await mgr.archiveCurrentSession(deleteDraft)

            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)
            expect(opts.archiveSessionCore).toHaveBeenCalledWith('session-1', 'claude')
            expect(deleteDraft).toHaveBeenCalledWith('session-1')

            fetchSpy.mockRestore()
        })

        it('cancels running current session before deleting', async () => {
            const opts = createMockOptions()
            mockRunningSessions.value = new Set(['session-1'])
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)
            const deleteDraft = vi.fn()

            await mgr.archiveCurrentSession(deleteDraft)

            expect(mockCancelChat).toHaveBeenCalledWith('session-1')
            expect(opts.archiveSessionCore).toHaveBeenCalledWith('session-1', 'claude')

            fetchSpy.mockRestore()
        })
    })

    // ── enqueueMessage ──

    describe('enqueueMessage', () => {
        it('posts message to backend queue API with queueId', async () => {
            const opts = createMockOptions()
            const queue = [{ text: 'enqueued' }]
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: true, queue }),
            } as Response)
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('session-1', 'hello', [{ path: 'attached', isDir: false }], ['pending'], 'pending-123')

            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('/api/ai/queue?session_id=session-1'),
                expect.objectContaining({ method: 'POST' }),
            )
            const body = JSON.parse((fetchSpy.mock.calls[0] as any[])[1].body)
            expect(body.message).toBe('hello')
            expect(body.queueId).toBe('pending-123')
            expect(body.filePaths).toEqual(['attached'])
            expect(body.files).toEqual([{ path: 'pending', isDir: false }, { path: 'attached', isDir: false }])

            fetchSpy.mockRestore()
        })

        it('sends clientId so the backend user_message broadcast can skip self-echo', async () => {
            // Regression: without clientId the backend broadcasts user_message
            // with an empty senderClientId, the frontend cannot skip its own
            // echo, and the queued message is rendered twice — once as the
            // pending bubble and once as a remote duplicate above it.
            localStorage.setItem('clawbench_client_id', 'device-test-1')
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: true, started: false }),
            } as Response)
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('session-1', 'hello', [], [], 'pending-123')

            const body = JSON.parse((fetchSpy.mock.calls[0] as any[])[1].body)
            expect(body.clientId).toBe('device-test-1')

            fetchSpy.mockRestore()
        })

        it('removes stale pending message on fetch error', async () => {
            // When enqueueMessage fails, the locally-pushed pending message
            // should be removed from messages.value so the user doesn't see a ghost entry.
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('session-1', 'hello')

            // The pending message should have been removed on error
            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)

            fetchSpy.mockRestore()
        })

        it('keeps other pending messages when removing failed one on error', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'earlier', blocks: [{ type: 'text', text: 'earlier' }], files: [], createdAt: '', pending: true,
            })
            opts.messages.value.push({
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('session-1', 'hello')

            // Only the failed 'hello' message is removed; 'earlier' stays
            const pendingMsgs = opts.messages.value.filter((m: any) => m.pending)
            expect(pendingMsgs).toHaveLength(1)
            expect(pendingMsgs[0].content).toBe('earlier')

            fetchSpy.mockRestore()
        })

        it('shows toast on fetch error', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.enqueueMessage('session-1', 'hello')

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

            await mgr.enqueueMessage('session-1', 'hello')

            expect(opts.scrollBottom).toHaveBeenCalledWith(true)

            fetchSpy.mockRestore()
        })
    })

    // ── handleRemovePending ──

    describe('handleRemovePending', () => {
        it('sends DELETE with queueId to backend', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'a', blocks: [], files: [], createdAt: '', pending: true, id: 'pending-1',
            })
            opts.messages.value.push({
                role: 'user', content: 'b', blocks: [], files: [], createdAt: '', pending: true, id: 'pending-2',
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [] }),
            } as Response)
            const mgr = useSessionManager(opts)

            await mgr.handleRemovePending('pending-2')

            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('queueId=pending-2'),
                expect.objectContaining({ method: 'DELETE' }),
            )

            fetchSpy.mockRestore()
        })

        it('removes pending message from messages.value by queueId', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'a', blocks: [], files: [], createdAt: '', pending: true, id: 'pending-1',
            })
            opts.messages.value.push({
                role: 'user', content: 'b', blocks: [], files: [], createdAt: '', pending: true, id: 'pending-2',
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [] }),
            } as Response)
            const mgr = useSessionManager(opts)

            await mgr.handleRemovePending('pending-2')

            // Only pending-1 should remain
            const pendingMsgs = opts.messages.value.filter((m: any) => m.pending)
            expect(pendingMsgs).toHaveLength(1)
            expect(pendingMsgs[0].id).toBe('pending-1')

            fetchSpy.mockRestore()
        })

        it('returns early for empty queueId', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'a', blocks: [], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch')
            const mgr = useSessionManager(opts)

            await mgr.handleRemovePending('')

            expect(fetchSpy).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('shows toast on error', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'pending-msg', blocks: [], files: [], createdAt: '', pending: true, id: 'pending-1',
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.handleRemovePending('pending-1')

            expect(mockToastShow).toHaveBeenCalledWith(
                'session.removeFailed',
                expect.objectContaining({ type: 'error' }),
            )

            fetchSpy.mockRestore()
        })
    })

    // ── registerIdentityActions ──

    describe('registerIdentityActions', () => {
        it('registers session actions with identity', async () => {
            const opts = createMockOptions()
            const mgr = useSessionManager(opts)

            expect(typeof mgr.registerIdentityActions).toBe('function')

            const mockExtra = {
                sendMessage: vi.fn(),
                openChatPanel: vi.fn(),
            }
            expect(() => mgr.registerIdentityActions(mockExtra)).not.toThrow()
        })
    })

    // ── forkSession ──

    describe('forkSession', () => {
        it('calls cleanup, clears input and pending messages, then delegates to forkSessionCore', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            opts.messages.value.push({
                role: 'user', content: 'pending', blocks: [], files: [], createdAt: '', pending: true,
            })
            const mgr = useSessionManager(opts)

            const result = await mgr.forkSession('session-1', 42, 'agent-2')

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(opts.clearInputState).toHaveBeenCalled()
            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)
            expect(opts.forkSessionCore).toHaveBeenCalledWith('session-1', 42, 'agent-2')
            expect(result).toBe(true)
        })

        it('calls forkSessionCore without optional args', async () => {
            const opts = createMockOptions()
            const mgr = useSessionManager(opts)

            await mgr.forkSession('session-1')

            expect(opts.forkSessionCore).toHaveBeenCalledWith('session-1', undefined, undefined)
        })
    })

    // ── destroySession ──

    describe('destroySession', () => {
        it('calls cleanup, cancels running session, clears queue, then destroys', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            mockRunningSessions.value = new Set(['session-2'])
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)

            await mgr.destroySession('session-2')

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(mockCancelChat).toHaveBeenCalledWith('session-2')
            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('/api/ai/queue?session_id=session-2'),
                { method: 'DELETE' },
            )
            expect(opts.destroySessionCore).toHaveBeenCalledWith('session-2')

            fetchSpy.mockRestore()
        })
    })

    // ── destroyCurrentSession ──

    describe('destroyCurrentSession', () => {
        it('returns early if no current session', async () => {
            const opts = createMockOptions()
            mockCurrentSessionId.value = ''
            const mgr = useSessionManager(opts)

            const deleteDraft = vi.fn()
            await mgr.destroyCurrentSession(deleteDraft)

            expect(opts.destroySessionCore).not.toHaveBeenCalled()
            expect(deleteDraft).not.toHaveBeenCalled()
        })

        it('destroys current session and calls deleteDraft', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)
            const deleteDraft = vi.fn()

            await mgr.destroyCurrentSession(deleteDraft)

            expect(opts.destroySessionCore).toHaveBeenCalledWith('session-1')
            expect(deleteDraft).toHaveBeenCalledWith('session-1')

            fetchSpy.mockRestore()
        })
    })

    // ── continueFromExecution ──

    describe('continueFromExecution', () => {
        it('calls cleanup then delegates to continueFromExecutionCore', async () => {
            const opts = createMockOptions()
            opts.loading.value = true
            const switchTabFn = vi.fn()
            const mgr = useSessionManager(opts)

            const result = await mgr.continueFromExecution(1, 2, switchTabFn)

            expect(opts.disconnectStream).toHaveBeenCalled()
            expect(opts.continueFromExecutionCore).toHaveBeenCalledWith(1, 2, switchTabFn)
            expect(result).toBe(true)
        })
    })

    // ── checkContinueSession ──

    describe('checkContinueSession', () => {
        it('delegates to checkContinueSessionCore', async () => {
            const opts = createMockOptions()
            const mgr = useSessionManager(opts)

            const result = await mgr.checkContinueSession(1, 2)

            expect(opts.checkContinueSessionCore).toHaveBeenCalledWith(1, 2)
            expect(result).toEqual({ exists: false, sessionId: '' })
        })
    })
})
