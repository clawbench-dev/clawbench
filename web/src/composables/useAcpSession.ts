import { ref, type Ref } from 'vue'
import { useToast } from '@/composables/useToast'
import { gt } from '@/composables/useLocale'
import { appLog } from '@/utils/appLog'

const TAG = 'useAcpSession'

export interface AcpSessionInfo {
  sessionId: string
  title: string
  cwd: string
  createdAt: string
  updatedAt: string
}

export interface UseAcpSessionOptions {
  currentAgentId: Ref<string>
}

// Module-level singleton state
const acpSessions = ref<AcpSessionInfo[]>([])
const acpSessionsLoading = ref(false)
const acpResuming = ref(false)
const acpSessionsNotSupported = ref(false)
const nextCursor = ref<string | null>(null)
const lastAgentId = ref('')

export function useAcpSession(options: UseAcpSessionOptions) {
  const { currentAgentId } = options
  const toast = useToast()

  async function loadAcpSessions(agentId?: string, append = false): Promise<void> {
    const aid = agentId || currentAgentId.value
    if (!aid) return

    // Reset if different agent
    if (!append && aid !== lastAgentId.value) {
      acpSessions.value = []
      nextCursor.value = null
      acpSessionsNotSupported.value = false
      lastAgentId.value = aid
    }

    acpSessionsLoading.value = true
    try {
      let url = `/api/agents/${encodeURIComponent(aid)}/acp-sessions`
      if (append && nextCursor.value) {
        url += `?cursor=${encodeURIComponent(nextCursor.value)}`
      }
      const resp = await fetch(url)
      if (!resp.ok) {
        // 501 = ListSessions not supported by this agent
        if (resp.status === 501) {
          acpSessionsNotSupported.value = true
        } else {
          toast.show(gt('chat.acpSession.loadFailed'), { type: 'error', icon: '⚠️' })
        }
        return
      }
      const data = await resp.json()
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const sessions: AcpSessionInfo[] = (data.sessions || []).map((s: any) => ({
        sessionId: s.sessionId || s.session_id || '',
        title: s.title || '',
        cwd: s.cwd || '',
        createdAt: s.createdAt || s.created_at || '',
        updatedAt: s.updatedAt || s.updated_at || '',
      }))
      if (append) {
        acpSessions.value.push(...sessions)
      } else {
        acpSessions.value = sessions
      }
      nextCursor.value = data.nextCursor || null
    } catch (err: unknown) {
      appLog.e(TAG, 'loadAcpSessions failed:', err)
    } finally {
      acpSessionsLoading.value = false
    }
  }

  /** Load an ACP session into a new ClawBench session. Returns the new sessionId, or 'not-found' if the session no longer exists on the agent side. */
  async function acpLoadSession(acpSessionId: string): Promise<string | null> {
    const aid = currentAgentId.value
    if (!aid || !acpSessionId) return null

    acpResuming.value = true
    try {
      const resp = await fetch('/api/ai/session/acp-load', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          agentId: aid,
          acpSessionId,
        }),
      })
      if (!resp.ok) {
        // Try to extract msgKey from error response for specific error messages
        let msgKey = ''
        try {
          const errData = await resp.json()
          msgKey = errData?.msgKey || ''
        } catch { /* ignore parse error */ }
        if (msgKey === 'ACPSessionNotFound') {
          toast.show(gt('chat.acpSession.sessionNotFound'), { type: 'error', icon: '⚠️' })
          // The list is host-sourced (ListSessions). Don't permanently drop the
          // entry from the local cache on a possibly-transient/misclassified load
          // failure — that would permanently hide sessions that still exist on the
          // agent side. Reconcile from the host instead: if the session genuinely
          // no longer exists it won't be returned, otherwise it stays so the user
          // can retry.
          await loadAcpSessions()
          return 'not-found'
        } else {
          toast.show(gt('chat.acpSession.loadFailed'), { type: 'error', icon: '⚠️' })
        }
        return null
      }
      const data = await resp.json()
      return data.sessionId || ''
    } catch (err: unknown) {
      appLog.e(TAG, 'acpLoadSession failed:', err)
      toast.show(gt('chat.acpSession.loadFailed'), { type: 'error', icon: '⚠️' })
      return null
    } finally {
      acpResuming.value = false
    }
  }

  /**
   * Incrementally sync the current session: reuse the ACP LoadSession replay to
   * merge external new messages into the local session. Returns { added }, or null
   * (with a toast) if there is no ACP session.
   */
  async function acpSyncSession(sessionId: string): Promise<{ added: number } | null> {
    const aid = currentAgentId.value
    if (!aid || !sessionId) return null
    try {
      const resp = await fetch('/api/ai/session/acp-sync', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ agentId: aid, sessionId }),
      })
      if (!resp.ok) {
        let msgKey = ''
        try {
          const errData = await resp.json()
          msgKey = errData?.msgKey || ''
        } catch { /* ignore parse error */ }
        if (msgKey === 'NoAcpSession') {
          toast.show(gt('chat.acpSession.noAcpSession'), { type: 'info', icon: '🔄' })
        } else {
          toast.show(gt('chat.acpSession.syncFailed'), { type: 'error', icon: '⚠️' })
        }
        return null
      }
      const data = await resp.json()
      return { added: typeof data.added === 'number' ? data.added : 0 }
    } catch (err: unknown) {
      appLog.e(TAG, 'acpSyncSession failed:', err)
      toast.show(gt('chat.acpSession.syncFailed'), { type: 'error', icon: '⚠️' })
      return null
    }
  }

  function clearAcpSessions(): void {
    acpSessions.value = []
    nextCursor.value = null
    acpSessionsNotSupported.value = false
    lastAgentId.value = ''
  }

  return {
    acpSessions,
    acpSessionsLoading,
    acpResuming,
    acpSessionsNotSupported,
    nextCursor,
    loadAcpSessions,
    acpLoadSession,
    acpSyncSession,
    clearAcpSessions,
  }
}
