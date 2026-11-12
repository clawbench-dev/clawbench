/**
 * One flat list of every public link this project has handed out.
 *
 * WHY AGGREGATE ON THE CLIENT
 * ---------------------------
 * The three kinds live in three different tables with three different shapes,
 * and each already has its own list endpoint that other screens depend on
 * (files → the file manager, conversations → the session list, meetings → the
 * meeting panel). A server-side union endpoint would either duplicate those
 * queries or become a fourth contract to keep in sync. Reading the three lists
 * and normalising them here keeps the server untouched and means a row can
 * never disagree with the screen it came from.
 *
 * The cost is three requests per refresh, which is why the panel loads once per
 * open rather than per row or per keystroke.
 */
import { ref } from 'vue'
import { buildShareLinks, type ShareLink } from '@/composables/useShareLinks'
import { appLog } from '@/utils/appLog'

const TAG = 'ShareCenter'

/** What a row points at. */
export type ShareKind = 'file' | 'dir' | 'session' | 'meeting'

/** One public link, whatever it was minted from. */
export interface ShareItem {
  /** Stable key: the token, prefixed by kind (tokens are per-table). */
  key: string
  kind: ShareKind
  /** The share token, as stored server-side. */
  token: string
  /**
   * The id of the thing behind the link, when it is not the token: a
   * conversation's session id. '' for kinds whose source is addressed another
   * way (a file by its path, a meeting by its own token).
   */
  sourceId: string
  /** Display name (file name, conversation title, meeting title). */
  name: string
  /** Secondary line: the file path, message count, or meeting time. */
  detail: string
  /** ISO timestamp of creation ('' when the server did not report one). */
  createdAt: string
  /** Server-relative public path, e.g. `/share/abc`. */
  path: string
  /**
   * A row that still has a working link but whose source is gone: a deleted
   * file, an archived conversation. The link keeps working, so the row is kept
   * and labelled rather than hidden.
   */
  sourceGone: boolean
  /** Label key for `sourceGone` ('' when nothing is wrong). */
  goneLabelKey: string
  /**
   * Meetings only: whether the record is published for after the meeting ends.
   * `null` for other kinds. When false the link only works while the meeting
   * runs, which the row must say — otherwise the user shares a link that dies
   * the moment the meeting is over.
   */
  shareEnabled: boolean | null
  /** Meetings only: the room is still live. */
  running: boolean
}

/** Which of the three lists failed, so the panel can say so without hiding the rest. */
export interface ShareCenterErrors {
  files: boolean
  sessions: boolean
  meetings: boolean
}

interface FileShareRow {
  token?: string
  name?: string
  path?: string
  createdAt?: string
  exists?: boolean
  isDir?: boolean
}

interface SessionShareRow {
  token?: string
  sessionId?: string
  title?: string
  messageCount?: number
  createdAt?: string
  archived?: boolean
}

interface MeetingRow {
  token?: string
  title?: string
  startedAt?: number
  endedAt?: number
  shareEnabled?: boolean
}

async function getJSON<T>(url: string): Promise<T> {
  const resp = await fetch(url, { credentials: 'same-origin' })
  if (!resp.ok) throw new Error(`${url}: ${resp.status}`)
  return (await resp.json()) as T
}

function sharePath(token: string): string {
  return `/share/${encodeURIComponent(token)}`
}

/** Format a meeting's start time the way the meeting list does (date + time). */
function meetingWhen(startedAt: number | undefined): string {
  if (!startedAt) return ''
  const d = new Date(startedAt)
  if (Number.isNaN(d.getTime())) return ''
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function useShareCenter() {
  const items = ref<ShareItem[]>([])
  const loading = ref(false)
  const errors = ref<ShareCenterErrors>({ files: false, sessions: false, meetings: false })

  /**
   * Read all three lists.
   *
   * Each list is fetched independently and a failure is recorded per kind: one
   * broken endpoint must not blank the whole panel, because the links that DID
   * load are exactly what the user came here to copy.
   */
  async function load(): Promise<void> {
    loading.value = true
    const next: ShareItem[] = []
    const failed: ShareCenterErrors = { files: false, sessions: false, meetings: false }

    const [files, sessions, meetings] = await Promise.all([
      getJSON<{ shares?: FileShareRow[] }>('/api/share/list').catch((e) => {
        appLog.w(TAG, 'file share list failed', e)
        failed.files = true
        return { shares: [] as FileShareRow[] }
      }),
      getJSON<{ shares?: SessionShareRow[] }>('/api/share/session/list').catch((e) => {
        appLog.w(TAG, 'session share list failed', e)
        failed.sessions = true
        return { shares: [] as SessionShareRow[] }
      }),
      getJSON<{ sessions?: MeetingRow[] }>('/api/live-share/notes/sessions').catch((e) => {
        appLog.w(TAG, 'meeting list failed', e)
        failed.meetings = true
        return { sessions: [] as MeetingRow[] }
      }),
    ])

    for (const s of files.shares ?? []) {
      if (!s.token) continue
      const isDir = s.isDir === true
      const exists = s.exists !== false
      next.push({
        key: `file:${s.token}`,
        kind: isDir ? 'dir' : 'file',
        token: s.token,
        sourceId: '',
        name: s.name || s.path || s.token,
        detail: s.path || '',
        createdAt: s.createdAt || '',
        path: sharePath(s.token),
        sourceGone: !exists,
        goneLabelKey: exists ? '' : 'shareCenter.fileMissing',
        shareEnabled: null,
        running: false,
      })
    }

    for (const s of sessions.shares ?? []) {
      if (!s.token) continue
      const archived = s.archived === true
      const count = s.messageCount ?? 0
      next.push({
        key: `session:${s.token}`,
        kind: 'session',
        token: s.token,
        sourceId: s.sessionId || '',
        name: s.title || s.token,
        detail: count > 0 ? String(count) : '',
        createdAt: s.createdAt || '',
        path: sharePath(s.token),
        sourceGone: archived,
        goneLabelKey: archived ? 'shareCenter.sessionArchived' : '',
        shareEnabled: null,
        running: false,
      })
    }

    for (const m of meetings.sessions ?? []) {
      if (!m.token) continue
      const running = !m.endedAt
      const enabled = m.shareEnabled === true
      // A meeting row is only a "share" when the record is actually published,
      // OR when the room is still running (the link works right now — that is
      // how people joined). A finished, unpublished meeting is not a share at
      // all: its token resolves to 404, so listing it here would offer the user
      // a link that cannot be opened.
      if (!running && !enabled) continue
      next.push({
        key: `meeting:${m.token}`,
        kind: 'meeting',
        token: m.token,
        sourceId: '',
        name: m.title || m.token,
        detail: meetingWhen(m.startedAt),
        createdAt: m.startedAt ? new Date(m.startedAt).toISOString() : '',
        path: sharePath(m.token),
        sourceGone: false,
        goneLabelKey: '',
        shareEnabled: enabled,
        running,
      })
    }

    // Newest first, across kinds. Rows with no timestamp sink to the bottom
    // rather than sorting as 1970 and landing behind everything.
    next.sort((a, b) => {
      const ta = a.createdAt ? Date.parse(a.createdAt) : 0
      const tb = b.createdAt ? Date.parse(b.createdAt) : 0
      if (ta === tb) return a.name.localeCompare(b.name)
      return tb - ta
    })

    items.value = next
    errors.value = failed
    loading.value = false
  }

  /** Every reachable address for one row's link (primary first). */
  async function linksFor(item: ShareItem): Promise<ShareLink[]> {
    return buildShareLinks(item.path)
  }

  /** Revoke one share. Meetings are unpublished instead of deleted. */
  async function revoke(item: ShareItem): Promise<void> {
    if (item.kind === 'meeting') {
      const resp = await fetch(
        `/api/live-share/notes/sessions/share?token=${encodeURIComponent(item.token)}`,
        {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ enabled: false }),
        },
      )
      if (!resp.ok) throw new Error(resp.statusText)
      items.value = items.value
        .map((i) => (i.key === item.key ? { ...i, shareEnabled: false } : i))
        // A meeting that is no longer published and is not running stops being
        // a share entirely — same rule as on load.
        .filter((i) => i.key !== item.key || i.running)
      return
    }

    const url = item.kind === 'session' ? '/api/share/session/list' : '/api/share/list'
    const resp = await fetch(url, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token: item.token }),
    })
    if (!resp.ok) throw new Error(resp.statusText)
    items.value = items.value.filter((i) => i.key !== item.key)
  }

  /**
   * Stop every share in the project.
   *
   * Files and conversations are deleted by their own bulk endpoints. Meetings
   * have no bulk call, so each published one is unpublished individually —
   * sequential rather than parallel because they all write the same SQLite
   * table through one process-wide write lock, and a burst of parallel writes
   * would only queue there.
   */
  async function revokeAll(): Promise<void> {
    const meetings = items.value.filter((i) => i.kind === 'meeting' && i.shareEnabled)

    await Promise.all([
      fetch('/api/share/list', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ all: true }),
      }).then((r) => {
        if (!r.ok) throw new Error(`files: ${r.statusText}`)
      }),
      fetch('/api/share/session/list', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ all: true }),
      }).then((r) => {
        if (!r.ok) throw new Error(`sessions: ${r.statusText}`)
      }),
    ])

    for (const m of meetings) {
      await fetch(`/api/live-share/notes/sessions/share?token=${encodeURIComponent(m.token)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: false }),
      }).then((r) => {
        if (!r.ok) throw new Error(`meeting ${m.token}: ${r.statusText}`)
      })
    }

    // Running meetings survive the sweep: their links are still the way people
    // join. Everything else is gone.
    items.value = items.value.filter((i) => i.kind === 'meeting' && i.running)
  }

  return { items, loading, errors, load, linksFor, revoke, revokeAll }
}
