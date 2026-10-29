import { ref } from 'vue'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'

const TAG = 'AgentSessions'

/** How the agent relates to one listed conversation. */
export type AgentSessionRole = 'chat' | 'host' | 'member'

/**
 * One conversation that uses an agent (mirrors service.AgentSessionItem).
 *
 * camelCase because that is what the server puts on the wire — the Go struct
 * carries json tags for exactly this reason. Getting this wrong is silent: every
 * row would render "untitled" with no project and clicking it would dispatch an
 * undefined id.
 */
export interface AgentSessionEntry {
  /** The referencing row: the chat id, or the group MEMBER row id. */
  entryId: string
  /** What to open: the chat itself, or the group timeline row for a membership. */
  sessionId: string
  title: string
  projectPath: string
  sessionType: string
  role: AgentSessionRole
  /** The instance's display name inside the group; empty for 1:1 chats. */
  memberName: string
  archived: boolean
  updatedAt: string
}

interface AgentSessionsResponse {
  sessions?: AgentSessionEntry[]
  hasMore?: boolean
  nextCursor?: string
  nextCursorId?: string
  total?: number
}

const PAGE_SIZE = 50

/**
 * Paged loader for "conversations using this agent".
 *
 * The list is CROSS-PROJECT and includes archived chats on purpose: it must
 * have the same length as the session count the settings page prints, which is
 * computed without a project or archive filter. Anything narrower would make
 * the header contradict the rows.
 */
export function useAgentSessions() {
  const sessions = ref<AgentSessionEntry[]>([])
  const total = ref(0)
  const hasMore = ref(false)
  const loading = ref(false)
  const loadingMore = ref(false)
  /** First-page failure: there is nothing to show, so the page renders an error. */
  const error = ref('')
  /**
   * Append failure: the rows already loaded stay on screen and only the footer
   * shows the problem. Kept separate from `error` because replacing a populated
   * list with "failed to load" on a transient blip would throw away everything
   * the user had scrolled through.
   */
  const loadMoreError = ref('')

  let cursor = ''
  let cursorId = ''
  let currentAgentId = ''
  // Bumped on every reset so a page that lands after the user left (or after a
  // different agent was opened) is dropped instead of appended to a fresh list.
  let generation = 0

  function reset(agentId: string) {
    generation++
    currentAgentId = agentId
    sessions.value = []
    total.value = 0
    hasMore.value = false
    loading.value = false
    loadingMore.value = false
    error.value = ''
    loadMoreError.value = ''
    cursor = ''
    cursorId = ''
  }

  async function fetchPage(agentId: string): Promise<void> {
    const gen = generation
    const params = new URLSearchParams({ limit: String(PAGE_SIZE) })
    if (cursor) {
      params.set('cursor', cursor)
      params.set('cursor_id', cursorId)
    }
    const isAppend = sessions.value.length > 0
    try {
      const data = await apiGet<AgentSessionsResponse>(`/api/agents/${encodeURIComponent(agentId)}/sessions?${params.toString()}`)
      // A stale response (the user switched agent / remounted) must not touch
      // the current list.
      if (gen !== generation || agentId !== currentAgentId) return
      const page = data.sessions || []
      sessions.value = sessions.value.concat(page)
      total.value = data.total ?? sessions.value.length
      hasMore.value = !!data.hasMore
      cursor = data.nextCursor || ''
      cursorId = data.nextCursorId || ''
      loadMoreError.value = ''
    } catch (err) {
      if (gen !== generation) return
      appLog.e(TAG, 'failed to load agent sessions', err)
      if (isAppend) {
        // Keep the loaded rows and the cursor intact: retrying the same page is
        // the only sensible recovery, so hasMore must stay true.
        loadMoreError.value = String(err)
      } else {
        error.value = String(err)
        hasMore.value = false
      }
    }
  }

  /** First page. Safe to call repeatedly (it resets). */
  async function load(agentId: string): Promise<void> {
    reset(agentId)
    loading.value = true
    try {
      await fetchPage(agentId)
    } finally {
      if (currentAgentId === agentId) loading.value = false
    }
  }

  /** Next page. No-ops while a page is in flight or the list is exhausted. */
  async function loadMore(): Promise<void> {
    if (!hasMore.value || loading.value || loadingMore.value || !currentAgentId) return
    loadingMore.value = true
    try {
      await fetchPage(currentAgentId)
    } finally {
      loadingMore.value = false
    }
  }

  /** Retry the failed append page (footer button). */
  async function retryLoadMore(): Promise<void> {
    loadMoreError.value = ''
    await loadMore()
  }

  return { sessions, total, hasMore, loading, loadingMore, error, loadMoreError, load, loadMore, retryLoadMore }
}
