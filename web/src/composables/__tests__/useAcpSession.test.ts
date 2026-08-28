import { describe, expect, it, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

// Mock fetch globally
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

// Mock toast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// Mock useLocale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import { useAcpSession } from '@/composables/useAcpSession'

describe('useAcpSession', () => {
  const currentAgentId = ref('agent-1')

  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockReset()
    currentAgentId.value = 'agent-1'
    // Clear module-level state by calling clearAcpSessions
    const { clearAcpSessions } = useAcpSession({ currentAgentId })
    clearAcpSessions()
  })

  describe('loadAcpSessions', () => {
    it('loads sessions from API', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { sessionId: 's1', title: 'Session 1', cwd: '/project', createdAt: '2025-01-01', updatedAt: '2025-01-02' },
            { sessionId: 's2', title: 'Session 2', cwd: '/other', created_at: '2025-01-03', updated_at: '2025-01-04' },
          ],
          nextCursor: 'cursor-1',
        }),
      })

      const { acpSessions, acpSessionsLoading, nextCursor, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(acpSessions.value).toHaveLength(2)
      expect(acpSessions.value[0]).toEqual({ sessionId: 's1', title: 'Session 1', cwd: '/project', createdAt: '2025-01-01', updatedAt: '2025-01-02' })
      expect(acpSessions.value[1]).toEqual({ sessionId: 's2', title: 'Session 2', cwd: '/other', createdAt: '2025-01-03', updatedAt: '2025-01-04' })
      expect(nextCursor.value).toBe('cursor-1')
      expect(acpSessionsLoading.value).toBe(false)
    })

    it('maps empty cwd to empty string', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [{ sessionId: 's1', title: 'No cwd' }], nextCursor: null }),
      })

      const { acpSessions, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(acpSessions.value[0].cwd).toBe('')
    })

    it('sets notSupported on 501 response', async () => {
      mockFetch.mockResolvedValue({ ok: false, status: 501 })

      const { acpSessionsNotSupported, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(acpSessionsNotSupported.value).toBe(true)
    })

    it('shows error toast on non-501 failure', async () => {
      mockFetch.mockResolvedValue({ ok: false, status: 500 })

      const { loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(mockToastShow).toHaveBeenCalledWith('chat.acpSession.loadFailed', expect.objectContaining({ type: 'error' }))
    })

    it('returns early when no agentId', async () => {
      currentAgentId.value = ''
      const { loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(mockFetch).not.toHaveBeenCalled()
    })

    it('appends sessions when append=true', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 's1', title: 'First' }],
            nextCursor: 'cursor-1',
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 's2', title: 'Second' }],
            nextCursor: null,
          }),
        })

      const { acpSessions, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()
      await loadAcpSessions(undefined, true)

      expect(acpSessions.value).toHaveLength(2)
    })

    it('deduplicates sessions repeated on later ACP pages', async () => {
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 's1', title: 'First' }],
            nextCursor: 'cursor-1',
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({
            sessions: [
              { sessionId: 's1', title: 'Repeated' },
              { sessionId: 's2', title: 'Second' },
            ],
            nextCursor: null,
          }),
        })

      const { acpSessions, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()
      await loadAcpSessions(undefined, true)

      expect(acpSessions.value.map((session) => session.sessionId)).toEqual(['s1', 's2'])
    })

    it('handles fetch exception gracefully', async () => {
      mockFetch.mockRejectedValue(new Error('network error'))

      const { acpSessionsLoading, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(acpSessionsLoading.value).toBe(false)
    })

    it('dedupes overlapping sessions when appending paginated pages', async () => {
      // Agent pagination is unstable: the second page repeats a sessionId
      // already present in the first page (timestamp-collision cursor).
      mockFetch
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({
            sessions: [
              { sessionId: 's1', title: 'First page' },
              { sessionId: 's2', title: 'Second page' },
            ],
            nextCursor: 'cursor-1',
          }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({
            sessions: [
              { sessionId: 's2', title: 'Second page' },
              { sessionId: 's3', title: 'Third page' },
            ],
            nextCursor: null,
          }),
        })

      const { acpSessions, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()
      await loadAcpSessions(undefined, true)

      expect(acpSessions.value).toHaveLength(3)
      expect(acpSessions.value.map((s) => s.sessionId)).toEqual(['s1', 's2', 's3'])
    })

    it('drops stale append responses that resolve after a list reset', async () => {
      // A slow append=true request (load more) is in flight when the list is
      // reset for a new agent via loadAcpSessions(append=false). The stale
      // response must NOT be merged into the fresh list.
      let resolveSlowFetch: (value: unknown) => void = () => {}
      const slowFetch = new Promise((resolve) => { resolveSlowFetch = resolve })
      mockFetch
        .mockImplementationOnce(() => Promise.resolve({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 'old-agent-s1', title: 'Old page 1' }],
            nextCursor: 'cursor-1',
          }),
        }))
        .mockImplementationOnce(() => slowFetch)
        .mockImplementationOnce(() => Promise.resolve({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 'new-agent-s1', title: 'New agent' }],
            nextCursor: null,
          }),
        }))

      const { acpSessions, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions('agent-old')
      const appendPromise = loadAcpSessions('agent-old', true)
      // Reset to a different agent while the append is still in flight
      await loadAcpSessions('agent-new')
      // The slow append finally resolves with stale page-2 data
      resolveSlowFetch({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ sessionId: 'old-page-2', title: 'Stale page 2' }],
          nextCursor: null,
        }),
      })
      await appendPromise

      // Only the new agent's sessions survive; the stale append is discarded.
      expect(acpSessions.value.map((s) => s.sessionId)).toEqual(['new-agent-s1'])
    })

    it('drops stale responses that resolve after json() body read', async () => {
      // The reset lands after the HTTP response but before json() resolves.
      // The post-json generation re-check must still drop the stale data.
      let resolveJson: (value: unknown) => void = () => {}
      const jsonPromise = new Promise((resolve) => { resolveJson = resolve })
      mockFetch
        .mockImplementationOnce(() => Promise.resolve({
          ok: true,
          json: () => jsonPromise,
        }))
        .mockImplementationOnce(() => Promise.resolve({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 'new-agent-s1', title: 'New agent' }],
            nextCursor: null,
          }),
        }))

      const { acpSessions, loadAcpSessions } = useAcpSession({ currentAgentId })
      const stalePromise = loadAcpSessions('agent-old')
      // Reset to a different agent while the stale request is reading its body
      await loadAcpSessions('agent-new')
      // The stale body finally parses with old-agent data
      resolveJson({
        sessions: [{ sessionId: 'old-agent-s1', title: 'Stale body' }],
        nextCursor: null,
      })
      await stalePromise

      expect(acpSessions.value.map((s) => s.sessionId)).toEqual(['new-agent-s1'])
    })

    it('resets and drops in-flight appends when append targets a different agent', async () => {
      // An append for agent A is in flight; a reset/append for agent B starts.
      // The append must reset the list (different agent) and the stale A
      // response must not land on B's list.
      let resolveSlowFetch: (value: unknown) => void = () => {}
      const slowFetch = new Promise((resolve) => { resolveSlowFetch = resolve })
      mockFetch
        .mockImplementationOnce(() => Promise.resolve({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 'agent-a-s1', title: 'A page 1' }],
            nextCursor: 'cursor-1',
          }),
        }))
        .mockImplementationOnce(() => slowFetch)
        .mockImplementationOnce(() => Promise.resolve({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 'agent-b-s1', title: 'B page 1' }],
            nextCursor: null,
          }),
        }))

      const { acpSessions, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions('agent-a')
      const appendPromise = loadAcpSessions('agent-a', true)
      // Append for a different agent — must reset, not merge onto A's list
      await loadAcpSessions('agent-b', true)
      resolveSlowFetch({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ sessionId: 'agent-a-s2', title: 'A stale page 2' }],
          nextCursor: null,
        }),
      })
      await appendPromise

      expect(acpSessions.value.map((s) => s.sessionId)).toEqual(['agent-b-s1'])
    })
  })

  describe('acpLoadSession', () => {
    it('loads ACP session and returns new sessionId', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessionId: 'new-session-1' }),
      })

      const { acpResuming, acpLoadSession } = useAcpSession({ currentAgentId })
      const result = await acpLoadSession('acp-s1')

      expect(result).toBe('new-session-1')
      expect(acpResuming.value).toBe(false)
      expect(mockFetch).toHaveBeenCalledWith('/api/ai/session/acp-load', expect.objectContaining({
        method: 'POST',
      }))
    })

    it('returns null when no agentId', async () => {
      currentAgentId.value = ''
      const { acpLoadSession } = useAcpSession({ currentAgentId })
      const result = await acpLoadSession('acp-s1')

      expect(result).toBeNull()
    })

    it('returns null when no acpSessionId', async () => {
      const { acpLoadSession } = useAcpSession({ currentAgentId })
      const result = await acpLoadSession('')

      expect(result).toBeNull()
    })

    it('shows sessionNotFound toast and reconciles from host for ACPSessionNotFound', async () => {
      // First call: acp-load returns ACPSessionNotFound. Second call: reconcile
      // the list from the host, which still has the session (it only disappears
      // if the host no longer returns it).
      mockFetch
        .mockResolvedValueOnce({
          ok: false,
          json: () => Promise.resolve({ msgKey: 'ACPSessionNotFound' }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({
            sessions: [{ sessionId: 'acp-s1', title: 'Test', createdAt: '', updatedAt: '' }],
            nextCursor: null,
          }),
        })

      const { acpLoadSession, acpSessions } = useAcpSession({ currentAgentId })
      // Pre-populate sessions list
      acpSessions.value = [{ sessionId: 'acp-s1', title: 'Test', cwd: '', createdAt: '', updatedAt: '' }]

      const result = await acpLoadSession('acp-s1')

      expect(result).toBe('not-found')
      expect(mockToastShow).toHaveBeenCalledWith('chat.acpSession.sessionNotFound', expect.objectContaining({ type: 'error' }))
      // Session is NOT permanently removed locally — it stays because the host
      // still reports it. The list is re-fetched from the host.
      expect(mockFetch).toHaveBeenLastCalledWith('/api/agents/agent-1/acp-sessions')
      expect(acpSessions.value).toContainEqual(expect.objectContaining({ sessionId: 'acp-s1' }))
    })

    it('reconciles session away from list when host no longer returns it', async () => {
      // acp-load returns ACPSessionNotFound; reconcile shows the host no longer
      // has the session, so the entry disappears from the list.
      mockFetch
        .mockResolvedValueOnce({
          ok: false,
          json: () => Promise.resolve({ msgKey: 'ACPSessionNotFound' }),
        })
        .mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ sessions: [], nextCursor: null }),
        })

      const { acpLoadSession, acpSessions } = useAcpSession({ currentAgentId })
      acpSessions.value = [{ sessionId: 'acp-s1', title: 'Test', cwd: '', createdAt: '', updatedAt: '' }]

      const result = await acpLoadSession('acp-s1')

      expect(result).toBe('not-found')
      expect(acpSessions.value).not.toContainEqual(expect.objectContaining({ sessionId: 'acp-s1' }))
    })

    it('shows generic error toast for other errors', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        json: () => Promise.resolve({ msgKey: 'OtherError' }),
      })

      const { acpLoadSession } = useAcpSession({ currentAgentId })
      const result = await acpLoadSession('acp-s1')

      expect(result).toBeNull()
      expect(mockToastShow).toHaveBeenCalledWith('chat.acpSession.loadFailed', expect.objectContaining({ type: 'error' }))
    })

    it('handles fetch exception in acpLoadSession', async () => {
      mockFetch.mockRejectedValue(new Error('network error'))

      const { acpLoadSession } = useAcpSession({ currentAgentId })
      const result = await acpLoadSession('acp-s1')

      expect(result).toBeNull()
      expect(mockToastShow).toHaveBeenCalledWith('chat.acpSession.loadFailed', expect.objectContaining({ type: 'error' }))
    })
  })

  describe('acpSyncSession', () => {
    it('returns added count on success', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ ok: true, added: 3 }),
      })

      const { acpSyncSession } = useAcpSession({ currentAgentId })
      const result = await acpSyncSession('sid-1')

      expect(result).toEqual({ added: 3 })
      const [url, init] = mockFetch.mock.calls[0]
      expect(url).toBe('/api/ai/session/acp-sync')
      expect(init.method).toBe('POST')
      expect(JSON.parse(init.body)).toEqual({ agentId: 'agent-1', sessionId: 'sid-1' })
    })

    it('returns null and shows toast on NoAcpSession', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 400,
        json: () => Promise.resolve({ msgKey: 'NoAcpSession' }),
      })

      const { acpSyncSession } = useAcpSession({ currentAgentId })
      const result = await acpSyncSession('sid-1')

      expect(result).toBeNull()
      expect(mockToastShow).toHaveBeenCalledWith('chat.acpSession.noAcpSession', expect.objectContaining({ type: 'info' }))
    })

    it('coerces non-numeric added to 0', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ ok: true, added: 'abc' }),
      })

      const { acpSyncSession } = useAcpSession({ currentAgentId })
      const result = await acpSyncSession('sid-1')

      expect(result).toEqual({ added: 0 })
    })

    it('shows syncFailed toast for generic errors', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        status: 500,
        json: () => Promise.resolve({ msgKey: 'InternalError' }),
      })

      const { acpSyncSession } = useAcpSession({ currentAgentId })
      const result = await acpSyncSession('sid-1')

      expect(result).toBeNull()
      expect(mockToastShow).toHaveBeenCalledWith('chat.acpSession.syncFailed', expect.objectContaining({ type: 'error' }))
    })
  })

  describe('clearAcpSessions', () => {
    it('clears all session state', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ sessionId: 's1', title: 'Session 1' }],
          nextCursor: 'cursor-1',
        }),
      })

      const { acpSessions, nextCursor, acpSessionsNotSupported, loadAcpSessions, clearAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(acpSessions.value).toHaveLength(1)

      clearAcpSessions()

      expect(acpSessions.value).toHaveLength(0)
      expect(nextCursor.value).toBeNull()
      expect(acpSessionsNotSupported.value).toBe(false)
    })
  })
})
