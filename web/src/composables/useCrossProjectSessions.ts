/**
 * useCrossProjectSessions — cross-project "active session" overview.
 *
 * Feeds the session list's bottom tab ("其他项目" / Other projects): sessions
 * in OTHER projects that are running, pending approval, or unread. The backend
 * endpoint `/api/ai/sessions/overview` already returns exactly that set grouped
 * by project (it was built for the Android floating window).
 *
 * Design notes:
 * - Module-level singleton: SessionList is mounted twice (sidebar + drawer) and
 *   both instances must share ONE fetched snapshot and ONE request.
 * - All watchers/WS subscriptions are registered at MODULE TOP LEVEL, never
 *   from inside a component's setup() call path. A watcher created during
 *   setup() binds to that component's effect scope and is destroyed when the
 *   component unmounts — and hot project switching rebuilds the whole component
 *   subtree, so a component-scoped watcher would be killed permanently and the
 *   cross-project list would silently stop updating.
 * - The current project is excluded on the FRONTEND so the Android consumer of
 *   the same endpoint is unaffected.
 */
import { ref, computed, watch } from 'vue'
import { store } from '@/stores/app.ts'
import { appLog } from '@/utils/appLog'
import { baseName, normalizeSlashes } from '@/utils/path.ts'
import { middleEllipsis } from '@/utils/completionMatch.ts'
import { useGlobalEvents } from '@/composables/useGlobalEvents'

const TAG = 'CrossProjectSessions'

/** Max characters for the abbreviated path shown under the project name. */
const DISPLAY_PATH_MAX_LEN = 32
const RELOAD_DEBOUNCE_MS = 400

export interface CrossProjectSession {
  id: string
  title: string
  backend?: string
  agentId?: string
  model?: string
  running: boolean
  pendingApproval: boolean
  unreadCount: number
  updatedAt: string
}

export interface CrossProjectGroup {
  /** Absolute project path (identity key). */
  name: string
  /** basename of the project path. */
  displayName: string
  /** Abbreviated path (homeDir → "~", middle-truncated when long). */
  displayPath: string
  sessions: CrossProjectSession[]
}

// ── Module-level singleton state ──
const groups = ref<CrossProjectGroup[]>([])
const loading = ref(false)
/** True once a fetch has completed successfully at least once. */
const loaded = ref(false)

let reloadDebounce: ReturnType<typeof setTimeout> | null = null

/** Total active sessions across all other projects (tab badge). */
const total = computed(() =>
  groups.value.reduce((sum, g) => sum + g.sessions.length, 0),
)

/**
 * Abbreviate an absolute path for display: replace the user's home directory
 * prefix with "~" and middle-truncate when still too long. Handles Windows
 * separators by normalizing before comparison.
 */
export function abbreviatePath(path: string, homeDir: string): string {
  let display = normalizeSlashes(path)
  const home = normalizeSlashes(homeDir || '').replace(/\/+$/, '')
  if (home && (display === home || display.toLowerCase().startsWith(home.toLowerCase() + '/'))) {
    display = '~' + display.slice(home.length)
  }
  return middleEllipsis(display, DISPLAY_PATH_MAX_LEN)
}

function toSession(raw: Record<string, unknown>): CrossProjectSession {
  return {
    id: String(raw.id ?? ''),
    title: String(raw.title ?? ''),
    backend: raw.backend ? String(raw.backend) : undefined,
    agentId: raw.agentId ? String(raw.agentId) : undefined,
    model: raw.model ? String(raw.model) : undefined,
    running: !!raw.running,
    pendingApproval: !!raw.pendingApproval,
    unreadCount: Number(raw.unreadCount ?? 0),
    updatedAt: String(raw.updatedAt ?? ''),
  }
}

/**
 * Fetch the overview and rebuild `groups`:
 * - exclude the current project (its sessions already show in the main list)
 * - group order: project path ascending, case-insensitive
 * - within a group: updatedAt descending (most recent activity first)
 */
export async function refresh(): Promise<void> {
  loading.value = true
  try {
    const resp = await fetch('/api/ai/sessions/overview')
    if (!resp.ok) throw new Error(`overview HTTP ${resp.status}`)
    const data = await resp.json()
    const rawProjects: Array<Record<string, unknown>> = data.projects || []
    const currentProject = store.state.projectRoot
    const homeDir = store.state.homeDir || ''

    const next: CrossProjectGroup[] = []
    for (const p of rawProjects) {
      const name = String(p.name ?? '')
      if (!name || name === currentProject) continue
      const rawSessions: Array<Record<string, unknown>> = (p.sessions as Array<Record<string, unknown>>) || []
      if (rawSessions.length === 0) continue
      const sessions = rawSessions.map(toSession)
      sessions.sort((a, b) => (b.updatedAt || '').localeCompare(a.updatedAt || ''))
      next.push({
        name,
        displayName: baseName(name),
        displayPath: abbreviatePath(name, homeDir),
        sessions,
      })
    }
    next.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()))

    groups.value = next
    loaded.value = true
  } catch (err) {
    // Keep the previous snapshot — a transient failure must not blank the tab.
    appLog.e(TAG, 'failed to load cross-project overview', err)
  } finally {
    loading.value = false
  }
}

/** Debounced refresh so bursty WS events (running→completed etc.) coalesce. */
export function scheduleRefresh(): void {
  if (reloadDebounce) clearTimeout(reloadDebounce)
  reloadDebounce = setTimeout(() => {
    reloadDebounce = null
    refresh()
  }, RELOAD_DEBOUNCE_MS)
}

// ── Module top-level side effects (see design note above) ──
// Re-fetch when the session list version bumps (create/archive/read/completion)
// and when the current project changes (the excluded project flips).
watch(() => store.state.sessionListVersion, () => { refresh() })
watch(() => store.state.projectRoot, () => { refresh() })

// WS session lifecycle events are broadcast globally (not project-scoped), so
// this also covers activity happening in other projects.
const { onEvent } = useGlobalEvents()
onEvent((event) => {
  if (event === 'session_update') scheduleRefresh()
})

// Kick off an initial load at module init so the tab badge is populated even
// before the list is opened.
refresh()

/** @internal Reset all state — for tests only. */
export function resetCrossProjectSessionsForTest(): void {
  if (reloadDebounce) { clearTimeout(reloadDebounce); reloadDebounce = null }
  groups.value = []
  loaded.value = false
  loading.value = false
}

export function useCrossProjectSessions() {
  return { groups, loading, loaded, total, refresh, scheduleRefresh }
}
