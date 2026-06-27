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
            { sessionId: 's1', title: 'Session 1', createdAt: '2025-01-01', updatedAt: '2025-01-02' },
            { sessionId: 's2', title: 'Session 2', created_at: '2025-01-03', updated_at: '2025-01-04' },
          ],
          nextCursor: 'cursor-1',
        }),
      })

      const { acpSessions, acpSessionsLoading, nextCursor, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(acpSessions.value).toHaveLength(2)
      expect(acpSessions.value[0]).toEqual({ sessionId: 's1', title: 'Session 1', createdAt: '2025-01-01', updatedAt: '2025-01-02' })
      expect(acpSessions.value[1]).toEqual({ sessionId: 's2', title: 'Session 2', createdAt: '2025-01-03', updatedAt: '2025-01-04' })
      expect(nextCursor.value).toBe('cursor-1')
      expect(acpSessionsLoading.value).toBe(false)
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

    it('handles fetch exception gracefully', async () => {
      mockFetch.mockRejectedValue(new Error('network error'))

      const { acpSessionsLoading, loadAcpSessions } = useAcpSession({ currentAgentId })
      await loadAcpSessions()

      expect(acpSessionsLoading.value).toBe(false)
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

    it('shows sessionNotFound toast for ACPSessionNotFound error', async () => {
      mockFetch.mockResolvedValue({
        ok: false,
        json: () => Promise.resolve({ msgKey: 'ACPSessionNotFound' }),
      })

      const { acpLoadSession, acpSessions } = useAcpSession({ currentAgentId })
      // Pre-populate sessions list so we can verify removal
      acpSessions.value = [{ sessionId: 'acp-s1', title: 'Test', createdAt: '', updatedAt: '' }]

      const result = await acpLoadSession('acp-s1')

      expect(result).toBe('not-found')
      expect(mockToastShow).toHaveBeenCalledWith('chat.acpSession.sessionNotFound', expect.objectContaining({ type: 'error' }))
      // The stale session should be removed from the list
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
