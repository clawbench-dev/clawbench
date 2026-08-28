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
// Generation counter guards against stale responses racing a list reset
// (agent switch / drawer reopen / clearAcpSessions). Each reset bumps the
// generation; a load response whose captured generation is stale is dropped.
let loadGen = 0
// In-flight request count so a dropped stale response can't clear the loading
// flag while a newer request for the reset list is still pending.
let pendingLoads = 0

export function useAcpSession(options: UseAcpSessionOptions) {
  const { currentAgentId } = options
  const toast = useToast()

  async function loadAcpSessions(agentId?: string, append = false): Promise<void> {
    const aid = agentId || currentAgentId.value
    if (!aid) return

    // Any request for a different agent invalidates the previous list,
    // including appends that slip through after a switch (the old in-flight
    // responses must not land on a new agent's list).
    if (aid !== lastAgentId.value) {
      acpSessions.value = []
      nextCursor.value = null
      acpSessionsNotSupported.value = false
      lastAgentId.value = aid
      loadGen++
    }

    // Capture the generation at request start; if a reset happens while this
    // request is in flight, the response must be dropped instead of being
    // merged onto the fresh list (prevents duplicate / cross-agent entries).
    const gen = loadGen

    pendingLoads++
    acpSessionsLoading.value = true
    try {
      let url = `/api/agents/${encodeURIComponent(aid)}/acp-sessions`
      if (append && nextCursor.value) {
        url += `?cursor=${encodeURIComponent(nextCursor.value)}`
      }
      const resp = await fetch(url)
      // A reset (agent switch / clear) started after this request — discard
      // the response so it can't corrupt the fresh list.
      if (gen !== loadGen) return
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
      // Re-check after parsing: a reset may have landed during body read.
      if (gen !== loadGen) return
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const sessions: AcpSessionInfo[] = (data.sessions || []).map((s: any) => ({
        sessionId: s.sessionId || s.session_id || '',
        title: s.title || '',
        cwd: s.cwd || '',
        createdAt: s.createdAt || s.created_at || '',
        updatedAt: s.updatedAt || s.updated_at || '',
      }))
      if (append) {
        // Idempotent merge: agents may overlap pages (unstable cursor), so a
        // sessionId already present must not be appended again.
        const seen = new Set(acpSessions.value.map((s) => s.sessionId))
        const fresh = sessions.filter((s) => !s.sessionId || !seen.has(s.sessionId))
        acpSessions.value.push(...fresh)
      } else {
        acpSessions.value = sessions
      }
      nextCursor.value = data.nextCursor || null
    } catch (err: unknown) {
      appLog.e(TAG, 'loadAcpSessions failed:', err)
    } finally {
      pendingLoads = Math.max(0, pendingLoads - 1)
      // Only clear the flag when no request is in flight anymore. A stale
      // response dropped above must not clear the flag of a newer pending load.
      acpSessionsLoading.value = pendingLoads > 0
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
    loadGen++
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
