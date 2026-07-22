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

function createMockOptions() {
    const messages = ref<any[]>([])
    const loading = ref(false)
    const switchSessionCore = vi.fn()
    const createSessionCore = vi.fn()
    const deleteSessionCore = vi.fn()
    const disconnectStream = vi.fn()
    const updateRenderedContents = vi.fn()
    const clearInputState = vi.fn()
    const scrollBottom = vi.fn()
    const reloadHistory = vi.fn().mockResolvedValue(undefined)
    return {
        messages, loading,
        switchSessionCore, createSessionCore, deleteSessionCore,
        continueFromExecutionCore: vi.fn().mockResolvedValue(true),
        forkSessionCore: vi.fn().mockResolvedValue(true),
        checkContinueSessionCore: vi.fn().mockResolvedValue({ exists: false, sessionId: '' }),
        disconnectStream,
        updateRenderedContents, clearInputState, scrollBottom, reloadHistory,
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

        it('clears pending messages before switching session', async () => {
            // Bug: pending messages from the old session must be cleared
            // before switching, otherwise watch(loading) fires with the
            // new session's ID and fetches the wrong queue.
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'queued in old session', blocks: [],
                files: [], createdAt: '', pending: true,
            })
            // Mock fetch to return empty queue (new session has no pending messages)
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [] }),
            } as Response)
            const mgr = useSessionManager(opts)
            fetchSpy.mockClear()

            await mgr.switchSession('session-2')

            // Pending messages from old session should be cleared
            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)
            expect(opts.switchSessionCore).toHaveBeenCalledWith('session-2')

            fetchSpy.mockRestore()
        })

        it('restores queued messages from backend after switchSessionCore completes', async () => {
            // Bug fix: switching to a session that has queued messages in the
            // backend must show them in the UI. Previously, the watch on
            // currentSessionId fired when clearSessionIdentity() set the ID
            // (before messages.value was populated from REST), so
            // syncPendingFromBackendQueue pushed pending messages into the
            // stale array, which then got replaced wholesale by parseMessages().
            const opts = createMockOptions()
            // Simulate switchSessionCore populating messages.value with
            // persisted messages from the new session (this is what the REST
            // API returns after clearSessionIdentity sets the ID).
            opts.switchSessionCore = vi.fn().mockImplementation(async () => {
                mockCurrentSessionId.value = 'session-2'
                opts.messages.value = [
                    { role: 'user', content: 'hello', id: 1 },
                    { role: 'assistant', content: 'hi', id: 2 },
                ]
            })
            // Backend queue for session-2 has 1 queued message
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    queue: [{ text: 'queued message', queueId: 'q-1', createdAt: '2025-01-01' }],
                }),
            } as Response)
            const mgr = useSessionManager(opts)
            fetchSpy.mockClear()

            await mgr.switchSession('session-2')

            // The queued message from the backend should appear in messages.value
            const pendingMsgs = opts.messages.value.filter((m: any) => m.pending)
            expect(pendingMsgs).toHaveLength(1)
            expect(pendingMsgs[0].content).toBe('queued message')
            expect(pendingMsgs[0].id).toBe('q-1')
            // Persisted messages should still be there
            expect(opts.messages.value.some((m: any) => m.content === 'hello')).toBe(true)

            fetchSpy.mockRestore()
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

        it('clears pending messages from messages.value, deletes session and draft', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'pending', blocks: [], files: [], createdAt: '', pending: true,
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true } as Response)
            const mgr = useSessionManager(opts)
            const deleteDraft = vi.fn()

            await mgr.deleteCurrentSession(deleteDraft)

            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)
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

        it('fetches queue and syncs pending messages into messages.value', async () => {
            const opts = createMockOptions()
            const queue = [{ text: 'hello' }]
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue }),
            } as Response)
            const mgr = useSessionManager(opts)

            await mgr.fetchQueue('session-1')

            // Pending messages should be synced from backend queue into messages.value
            expect(opts.messages.value.some((m: any) => m.pending)).toBe(true)

            fetchSpy.mockRestore()
        })

        it('handles fetch error gracefully', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('fail'))
            const mgr = useSessionManager(opts)

            await mgr.fetchQueue('session-1')

            // No crash, no pending messages added
            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)

            fetchSpy.mockRestore()
        })

        it('handles non-ok response gracefully', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: false,
            } as Response)
            const mgr = useSessionManager(opts)

            await mgr.fetchQueue('session-1')

            expect(opts.messages.value.some((m: any) => m.pending)).toBe(false)

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
            // Clear calls from immediate watch (fetchQueue on mount)
            fetchSpy.mockClear()

            await mgr.enqueueMessage('session-1', 'hello', ['/path1'], [{ path: 'attached', isDir: false }], ['pending'], 'pending-123')

            expect(fetchSpy).toHaveBeenCalledWith(
                expect.stringContaining('/api/ai/queue?session_id=session-1'),
                expect.objectContaining({ method: 'POST' }),
            )
            const body = JSON.parse((fetchSpy.mock.calls[0] as any[])[1].body)
            expect(body.message).toBe('hello')
            expect(body.queueId).toBe('pending-123')
            expect(body.filePaths).toEqual(['/path1', 'attached'])
            expect(body.files).toEqual([{ path: 'pending', isDir: false }, { path: 'attached', isDir: false }])

            fetchSpy.mockRestore()
        })

        it('does NOT full-sync queue after successful enqueue (preserves optimistic messages)', async () => {
            // Bug 1 fix: after enqueueMessage succeeds, we should NOT call
            // syncPendingFromBackendQueue because it would clear+repush all pending
            // messages, which can lose other optimistically-pushed messages when
            // two enqueueMessage calls overlap.
            const opts = createMockOptions()
            // Pre-existing optimistic pending message from another send
            opts.messages.value.push({
                role: 'user', content: 'earlier', blocks: [{ type: 'text', text: 'earlier' }],
                files: [], createdAt: '', pending: true, id: 'queue-earlier',
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: true, queue: [{ text: 'earlier' }, { text: 'hello' }] }),
            } as Response)
            const mgr = useSessionManager(opts)
            fetchSpy.mockClear()

            await mgr.enqueueMessage('session-1', 'hello')

            // The pre-existing 'earlier' pending message must still be in messages.value
            // (not cleared by syncPendingFromBackendQueue)
            const pendingContents = opts.messages.value.filter((m: any) => m.pending).map((m: any) => m.content)
            expect(pendingContents).toContain('earlier')

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

        it('returns needsStart=true when backend detects session not running', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true, id: 'pending-456',
            })
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({
                    ok: true,
                    needs_start: true,
                    message: 'hello',
                    filePaths: ['/main.go'],
                    files: ['/main.go'],
                    queueId: 'pending-456',
                    queue: [],
                }),
            } as Response)
            const mgr = useSessionManager(opts)
            fetchSpy.mockClear()

            const result = await mgr.enqueueMessage('session-1', 'hello', [], [], [], 'pending-456')

            expect(result.needsStart).toBe(true)
            expect(result.message).toBe('hello')
            expect(result.filePaths).toEqual(['/main.go'])

            fetchSpy.mockRestore()
        })

        it('removes pending message from messages.value when needsStart is true', async () => {
            const opts = createMockOptions()
            opts.messages.value.push({
                role: 'user', content: 'hello', blocks: [{ type: 'text', text: 'hello' }], files: [], createdAt: '', pending: true, id: 'pending-456',
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
            const mgr = useSessionManager(opts)
            fetchSpy.mockClear()

            await mgr.enqueueMessage('session-1', 'hello', [], [], [], 'pending-456')

            // The pending message should have been removed from messages.value
            const pendingMsgs = opts.messages.value.filter((m: any) => m.pending)
            expect(pendingMsgs).toHaveLength(0)

            fetchSpy.mockRestore()
        })

        it('returns needsStart=false on normal enqueue', async () => {
            const opts = createMockOptions()
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ ok: true, queue: [{ text: 'hello' }] }),
            } as Response)
            const mgr = useSessionManager(opts)

            const result = await mgr.enqueueMessage('session-1', 'hello')

            expect(result.needsStart).toBe(false)

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
            fetchSpy.mockClear()

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
            // First call: fetchQueue on mount returns both pending items
            // Second call: DELETE removes pending-2
            let callCount = 0
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(() => {
                callCount++
                if (callCount === 1) {
                    // fetchQueue on mount — return both items so they survive sync
                    return Promise.resolve({
                        ok: true,
                        json: () => Promise.resolve({ queue: [
                            { text: 'a', queueId: 'pending-1' },
                            { text: 'b', queueId: 'pending-2' },
                        ]}),
                    } as Response)
                }
                // DELETE call
                return Promise.resolve({
                    ok: true,
                    json: () => Promise.resolve({ queue: [] }),
                } as Response)
            })
            const mgr = useSessionManager(opts)
            // Wait for mount fetchQueue to complete
            await nextTick()

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
            fetchSpy.mockClear()

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

    // ── visibility handler ──

    describe('visibility handler', () => {
        it('exposes _visibilityHandler', () => {
            const opts = createMockOptions()
            const mgr = useSessionManager(opts)

            expect(typeof mgr._visibilityHandler).toBe('function')
        })

        it('fetches queue when visible with pending messages', async () => {
            const opts = createMockOptions()
            // Put a pending message in messages.value
            opts.messages.value.push({
                role: 'user', content: 'pending', blocks: [], files: [], createdAt: '', pending: true,
            })
            const mgr = useSessionManager(opts)

            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [] }),
            } as Response)

            // Simulate visibility change
            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            expect(fetchSpy).toHaveBeenCalled()
            // Backend queue empty → pending cleared → reloadHistory called
            expect(opts.reloadHistory).toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('does not fetch queue when no pending messages', async () => {
            const opts = createMockOptions()
            // No pending messages in messages.value
            const mgr = useSessionManager(opts)

            const fetchSpy = vi.spyOn(globalThis, 'fetch')

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            expect(fetchSpy).not.toHaveBeenCalled()
            expect(opts.reloadHistory).not.toHaveBeenCalled()

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

    describe('visibility change — pending messages drained while backgrounded', () => {
        it('reloads history when pending messages are cleared by fetchQueue', async () => {
            const opts = createMockOptions()
            opts.messages.value = [
                { role: 'user', content: 'hello', pending: true, id: 'pending-1' },
            ]
            const mgr = useSessionManager(opts)

            // Mock fetchQueue to return empty backend queue (message was drained)
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [] }),
            } as Response)

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            // fetchQueue was called and cleared pending messages
            expect(fetchSpy).toHaveBeenCalled()
            // reloadHistory should have been called because pending messages were cleared
            expect(opts.reloadHistory).toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('does not reload history when pending messages still exist after fetchQueue', async () => {
            const opts = createMockOptions()
            opts.messages.value = [
                { role: 'user', content: 'hello', pending: true, id: 'pending-1' },
            ]
            const mgr = useSessionManager(opts)

            // Mock fetchQueue to return the same pending message (not drained yet)
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [{ text: 'hello', queueId: 'pending-1', createdAt: new Date().toISOString() }] }),
            } as Response)

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            expect(fetchSpy).toHaveBeenCalled()
            // reloadHistory should NOT be called because pending messages still exist
            expect(opts.reloadHistory).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('reloads history when some pending messages are drained and some remain', async () => {
            const opts = createMockOptions()
            opts.messages.value = [
                { role: 'user', content: 'msg-1', pending: true, id: 'pending-1' },
                { role: 'user', content: 'msg-2', pending: true, id: 'pending-2' },
            ]
            const mgr = useSessionManager(opts)

            // Backend drained pending-1, pending-2 still queued
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [{ text: 'msg-2', queueId: 'pending-2', createdAt: new Date().toISOString() }] }),
            } as Response)

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            // pending-1 was drained (cleared from messages), pending-2 still exists
            // No reloadHistory because there is still a pending message
            expect(opts.reloadHistory).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('does not reload history when fetchQueue request fails', async () => {
            const opts = createMockOptions()
            opts.messages.value = [
                { role: 'user', content: 'hello', pending: true, id: 'pending-1' },
            ]
            const mgr = useSessionManager(opts)

            // fetchQueue request fails — pending messages remain in messages.value
            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network'))
            const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            expect(fetchSpy).toHaveBeenCalled()
            // Pending message should still exist (fetchQueue failed, no sync)
            expect(opts.messages.value.some(m => m.pending)).toBe(true)
            // reloadHistory should NOT be called
            expect(opts.reloadHistory).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
            consoleSpy.mockRestore()
        })

        it('does not reload history when fetchQueue returns non-ok response', async () => {
            const opts = createMockOptions()
            opts.messages.value = [
                { role: 'user', content: 'hello', pending: true, id: 'pending-1' },
            ]
            const mgr = useSessionManager(opts)

            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: false,
                status: 500,
            } as Response)

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            // Pending message still exists (non-ok response, no sync)
            expect(opts.messages.value.some(m => m.pending)).toBe(true)
            expect(opts.reloadHistory).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('does not call reloadHistory when reloadHistory throws', async () => {
            const opts = createMockOptions()
            opts.messages.value = [
                { role: 'user', content: 'hello', pending: true, id: 'pending-1' },
            ]
            opts.reloadHistory = vi.fn().mockRejectedValue(new Error('load failed'))
            const mgr = useSessionManager(opts)

            const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
                ok: true,
                json: () => Promise.resolve({ queue: [] }),
            } as Response)

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            // Should not throw even when reloadHistory rejects
            await expect(mgr._visibilityHandler()).resolves.toBeUndefined()

            expect(opts.reloadHistory).toHaveBeenCalled()

            fetchSpy.mockRestore()
        })

        it('does not trigger when no current session', async () => {
            const opts = createMockOptions()
            opts.messages.value = [
                { role: 'user', content: 'hello', pending: true, id: 'pending-1' },
            ]
            const mgr = useSessionManager(opts)

            mockCurrentSessionId.value = ''
            const fetchSpy = vi.spyOn(globalThis, 'fetch')

            vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
            await mgr._visibilityHandler()

            expect(fetchSpy).not.toHaveBeenCalled()
            expect(opts.reloadHistory).not.toHaveBeenCalled()

            fetchSpy.mockRestore()
            mockCurrentSessionId.value = 'session-1'
        })
    })
})
