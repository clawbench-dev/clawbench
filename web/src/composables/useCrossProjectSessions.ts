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
  /**
   * Display name of `agentId`, supplied by the server.
   *
   * The local agent list (GET /api/agents) is scoped to the CURRENT project,
   * but this pane lists sessions from OTHER projects — a row may therefore run
   * an agent scoped to its own project, which the local list does not contain.
   * Without this the row rendered a raw agent id.
   */
  agentName?: string
  /** Custom avatar SVG for exactly those unresolvable agents (see agentName). */
  agentAvatar?: string
  /**
   * Display name of the agent this session was taken over FROM (its agent was
   * deleted and the default agent inherited it). Empty/absent = never taken
   * over; drives the "已接管" badge.
   */
  agentTakeoverFrom?: string
  model?: string
  /** 'group' for a group-chat session; drives the group row rendering. */
  sessionType?: string
  /** Active-member preview for a group row (see model.GroupMemberPreview). */
  groupMembers?: GroupMemberPreview[]
  running: boolean
  pendingApproval: boolean
  unreadCount: number
  updatedAt: string
}

/** One active group member in a session-list row (mirrors the Go wire type). */
export interface GroupMemberPreview {
  id: string
  agentId: string
  name: string
  backend: string
  /** 成员自身头像 SVG（agent_avatar）；缺失时 AvatarStack 回退智能体头像。 */
  avatar?: string
  /** The group host; the row's mode chip is derived from its presence. */
  isHost?: boolean
}

export interface CrossProjectGroup {
  /** Absolute project path (identity key). */
  name: string
  /** basename of the project path. */
  displayName: string
  /** Abbreviated path (homeDir → "~", middle-truncated when long). */
  displayPath: string
  sessions: CrossProjectSession[]
  /**
   * When this project was last touched, ISO-ish timestamp. Set on BOTH kinds of
   * card: active ones take it from their newest session, inactive ones from the
   * server's recency query. Empty only when neither source had a usable value.
   */
  lastActiveAt: string
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
    agentName: raw.agentName ? String(raw.agentName) : undefined,
    agentAvatar: raw.agentAvatar ? String(raw.agentAvatar) : undefined,
    // Field-by-field mapping: anything the server adds must be listed here or it
    // silently disappears in this pane (the badge did exactly that).
    agentTakeoverFrom: raw.agentTakeoverFrom ? String(raw.agentTakeoverFrom) : undefined,
    model: raw.model ? String(raw.model) : undefined,
    sessionType: raw.sessionType ? String(raw.sessionType) : undefined,
    groupMembers: Array.isArray(raw.groupMembers)
      ? (raw.groupMembers as GroupMemberPreview[])
      : undefined,
    running: !!raw.running,
    pendingApproval: !!raw.pendingApproval,
    unreadCount: Number(raw.unreadCount ?? 0),
    updatedAt: String(raw.updatedAt ?? ''),
  }
}

/**
 * Fetch the overview once and rebuild `groups`:
 * - exclude the current project (its sessions already show in the main list)
 * - keep BOTH kinds of group the server sends: ones with active sessions, and
 *   ones that only carry `lastActiveAt` (recently touched, nothing active now).
 *   The latter arrive without sessions on purpose — the card stays collapsed
 *   until the user opens it, so the response stays small.
 * - order: projects with active sessions first, then the merely-recent ones;
 *   within each bucket by most recent activity (the server already sends both
 *   buckets in that order, so the sort is a guard against a future reshuffle,
 *   not the source of truth)
 * - within a group: updatedAt descending (most recent activity first)
 *
 * `projectRoot`/`homeDir` are read AFTER the await on purpose: whichever
 * project is current when the response lands is the one to exclude. That also
 * means a response that was already in flight when the project changed is still
 * correctly filtered, so the caller does not need to discard it.
 */
async function fetchOverview(): Promise<void> {
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
    const serverLastActive = p.lastActiveAt ? String(p.lastActiveAt) : ''
    // A group with neither sessions nor a recency stamp carries no information;
    // dropping it keeps the section from rendering an empty card.
    if (rawSessions.length === 0 && !serverLastActive) continue
    const sessions = rawSessions.map(toSession)
    sessions.sort((a, b) => (b.updatedAt || '').localeCompare(a.updatedAt || ''))
    next.push({
      name,
      displayName: baseName(name),
      displayPath: abbreviatePath(name, homeDir),
      sessions,
      // The newest session is the same fact as the server's MAX(updated_at),
      // so an active card reads the time off its own rows and only falls back
      // to the server's stamp.
      lastActiveAt: sessions[0]?.updatedAt || serverLastActive,
    })
  }
  // Active cards first; within each bucket newest activity first. Array#sort is
  // stable, so the server's ordering survives ties.
  next.sort((a, b) => {
    const aActive = a.sessions.length > 0
    const bActive = b.sessions.length > 0
    if (aActive !== bActive) return aActive ? -1 : 1
    return (b.lastActiveAt || '').localeCompare(a.lastActiveAt || '')
  })

  groups.value = next
  loaded.value = true
}

/**
 * The in-flight refresh, if any, plus whether another pass was requested while
 * it ran.
 *
 * Three independent triggers feed this composable — a `sessionListVersion`
 * bump, a `projectRoot` change, and WS `session_update` events — and a single
 * project switch fires several of them at once. Without coalescing each one
 * started its own fetch: a measured project switch issued `/api/ai/sessions/
 * overview` five times, four of them redundant.
 *
 * Concurrent callers now share one request. A caller that arrives mid-flight
 * sets `rerunRequested` instead of racing a second request, so an update that
 * lands during a fetch is still picked up — N concurrent triggers cost at most
 * two requests (the in-flight one plus one trailing pass), not N.
 */
let inFlightRefresh: Promise<void> | null = null
let rerunRequested = false

/**
 * Refresh the cross-project overview, coalescing concurrent callers.
 *
 * Never rejects: a transient failure keeps the previous snapshot (see below)
 * and is logged, so callers need not guard.
 */
export function refresh(): Promise<void> {
  if (inFlightRefresh) {
    // Already fetching. Its response will be filtered against the project that
    // is current when it lands, but data may have changed since it started —
    // so ask for one trailing pass rather than issuing a parallel request.
    rerunRequested = true
    return inFlightRefresh
  }

  inFlightRefresh = (async () => {
    try {
      do {
        rerunRequested = false
        loading.value = true
        try {
          await fetchOverview()
        } catch (err) {
          // Keep the previous snapshot — a transient failure must not blank the tab.
          appLog.e(TAG, 'failed to load cross-project overview', err)
        } finally {
          loading.value = false
        }
      } while (rerunRequested)
    } finally {
      inFlightRefresh = null
    }
  })()

  return inFlightRefresh
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
// or the current project changes (the excluded project flips).
//
// Both signals fire from the same project switch, and Vue batches a watcher
// whose sources change together — so watching them as one array yields a single
// refresh where two separate watchers produced two concurrent requests.
watch(
  [() => store.state.sessionListVersion, () => store.state.projectRoot],
  () => { refresh() },
)

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
  // Drop the coalescing state too: a test that leaves a request "in flight"
  // would otherwise make the next test's refresh() join a stale promise and
  // never issue its own fetch.
  inFlightRefresh = null
  rerunRequested = false
  groups.value = []
  loaded.value = false
  loading.value = false
}

export function useCrossProjectSessions() {
  return { groups, loading, loaded, total, refresh, scheduleRefresh }
}
