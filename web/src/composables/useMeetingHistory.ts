/**
 * Meeting records: list, one record, artifacts, chat, delete — and the quote
 * payload a meeting turns into when it is dragged into the chat.
 *
 * The record is the DATABASE row keyed by token, not a folder on disk. A
 * meeting exists from the moment the publisher attaches (even one that recorded
 * nothing), the folder is created only once there is something to write, and
 * the token is what every endpoint addresses. That is why nothing here deals in
 * directory names.
 */
import { ref } from 'vue'
import type { QuoteData } from '@/composables/useChatContext'
import i18n from '@/i18n'
import { appLog } from '@/utils/appLog'

const TAG = 'MeetingHistory'

/** The three artifacts that can be regenerated, in dependency order. */
export type MeetingArtifactKind = 'transcript' | 'revised' | 'minutes'

/** Artifact kinds the read endpoint can return (chat has its own path). */
export type MeetingArtifactReadKind = MeetingArtifactKind | 'chat'

export type ArtifactJobStatus = 'idle' | 'queued' | 'running' | 'failed'

/** How a transcript step runs. */
export type RegenMode = 'asr' | 'render' | 'model'

/** Why a step is in the plan. */
export type RegenReason = 'missing' | 'stale' | 'pending' | 'target' | 'downstream'

/** One artifact's two axes, as the server reports them. */
export interface MeetingArtifactState {
  hasContent: boolean
  jobStatus: ArtifactJobStatus
  errorCode?: string
  error?: string
  detail?: string
  /** Unix ms of the last content CHANGE; 0 = unknown/never generated. */
  contentAt: number
  /** The upstream level is newer than this artifact. */
  stale: boolean
}

/** The derived "what is running now" summary; null when nothing is running. */
export interface MeetingGeneration {
  step: MeetingArtifactKind
  remaining: MeetingArtifactKind[]
  detail?: string
  /** Unix ms. */
  startedAt: number
}

/** One stage of a regeneration plan. */
export interface RegenStep {
  kind: MeetingArtifactKind
  mode: RegenMode
  reason: RegenReason
}

export interface RegenBlock {
  code: string
  message: string
}

export interface RegenPlan {
  target: MeetingArtifactKind
  steps: RegenStep[]
  warnings: string[]
  blocked: RegenBlock | null
}

/** One meeting, as returned by `GET /api/live-share/notes/sessions`. */
export interface MeetingRecord {
  token: string
  title: string
  /** Unix ms, server clock. */
  startedAt: number
  /** Unix ms; 0 while the meeting is still running. */
  endedAt: number
  /** Folder NAME inside the project's notes root; absent when no artifacts. */
  dir?: string
  /** Absolute folder path (publisher only). */
  dirPath?: string
  hasMinutes: boolean
  hasTranscript: boolean
  hasRevised: boolean
  /** True once the recording has been merged into `audio/meeting.webm`. */
  hasMeetingAudio: boolean
  audioFiles: number
  chatCount: number
  /** Key frames kept for this meeting (derived from the folder). */
  shotCount?: number
  /** Any transcript segments at all. */
  hasSegments: boolean
  /** Any recording at all (per-channel or the mix). */
  hasAudio: boolean
  artifacts: Record<MeetingArtifactKind, MeetingArtifactState>
  generation: MeetingGeneration | null
  /** Distinct speakers the transcript heard; 0 when there is no transcript. */
  speakerCount?: number
  updatedAt: number
}

/** One stored key frame. */
export interface MeetingShot {
  id: number
  /** Unix ms. */
  at: number
  /** Offset from the transcript's timeline origin, in ms. */
  offsetMs: number
  reason: string
  ref?: string
  /** Relative to the meeting folder, e.g. `shots/1700000000000-file-3f2a.webp`. */
  fileName: string
  width?: number
  height?: number
  bytes?: number
}

/** One persisted chat line. */
export interface MeetingChatMessage {
  id: number
  clientId: string
  speaker: string
  viewerId?: string
  text: string
  /** Unix ms, server clock. */
  at: number
}

/** A quote carries at most this much meeting text into the prompt. */
export const MEETING_QUOTE_MAX_CHARS = 4000

/**
 * Whether this server can turn a dropped recording into a meeting.
 *
 * The conversion needs ffmpeg, so the panel has to know before it accepts a
 * multi-gigabyte upload that would only be rejected at the end.
 */
export interface MeetingImportAvailability {
  available: boolean
  /** Stable reason code (`ffmpeg_missing`), localized by the caller. */
  reason: string
}

/** One import response: the new token, or the server's message key. */
interface ImportResponse {
  token?: string
  msgKey?: string
  error?: string
}

/** Parse an import response body, tolerating a non-JSON error page. */
function parseImportResponse(text: string): ImportResponse | null {
  try {
    const parsed: unknown = JSON.parse(text)
    return parsed && typeof parsed === 'object' ? (parsed as ImportResponse) : null
  } catch {
    return null
  }
}

/** Outcome of importing one file: the new meeting's token, or why it failed. */
export type MeetingImportResult = { ok: true; token: string } | { ok: false; reason: string }

/** Empty state used before the first record arrives. */
export function emptyArtifactState(): MeetingArtifactState {
  return { hasContent: false, jobStatus: 'idle', contentAt: 0, stale: false }
}

async function readJSON<T>(resp: Response, what: string): Promise<T> {
  if (!resp.ok) throw new Error(`${what} failed: ${resp.status}`)
  return (await resp.json()) as T
}

/** Truncate meeting text for a quote, saying so when it was cut. */
export function clipQuoteText(text: string, max = MEETING_QUOTE_MAX_CHARS): string {
  const trimmed = text.trim()
  if (trimmed.length <= max) return trimmed
  const note = i18n.global.t('meeting.quote.truncated', { n: trimmed.length })
  return `${trimmed.slice(0, max)}\n${note}`
}

/**
 * The server's truth for "this meeting is still open": the record has no end
 * stamp. Records are only created when the publisher attaches, so a listed
 * row always has `startedAt`; the `> 0` guard just keeps a malformed row out
 * of the running set.
 *
 * This is the ONE predicate behind the row's 进行中 pill, the ticking clock
 * and the footer's join action — every consumer reads it from here, so the
 * badges and the actions can never disagree about whether a meeting runs.
 */
export function isLiveRecord(record: Pick<MeetingRecord, 'endedAt' | 'startedAt'>): boolean {
  return !record.endedAt && record.startedAt > 0
}

/** `会议 · 发版评审 · 11-01 14:30` — the card's label and the prompt's title. */
export function meetingQuoteLabel(record: Pick<MeetingRecord, 'title' | 'startedAt'>): string {
  const title = record.title.trim() || i18n.global.t('meeting.list.untitled')
  if (!record.startedAt) return i18n.global.t('meeting.quote.labelNoTime', { title })
  const d = new Date(record.startedAt)
  const pad = (n: number) => String(n).padStart(2, '0')
  const stamp = `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  return i18n.global.t('meeting.quote.label', { title, time: stamp })
}

/** One artifact state, tolerating an old server that omits the map. */
export function artifactStateOf(record: MeetingRecord | null, kind: MeetingArtifactKind): MeetingArtifactState {
  return record?.artifacts?.[kind] ?? emptyArtifactState()
}

export function useMeetingHistory() {
  const sessions = ref<MeetingRecord[]>([])
  const loading = ref(false)
  const error = ref('')
  /**
   * Server capability, refreshed with every list load. Optimistic before the
   * first load: assuming "yes" only costs a rejected request, while assuming
   * "no" would hide the drop zone from every user.
   */
  const importAvailability = ref<MeetingImportAvailability>({ available: true, reason: '' })

  /** A load requested while one was in flight: re-run once it settles. */
  let reloadQueued = false

  async function load(): Promise<void> {
    if (loading.value) {
      // Dropping this silently loses the caller's intent: a refresh clicked
      // during a poll would do nothing, and an import would leave the new
      // record invisible until the next poll (which is only armed for rows
      // that are already on screen).
      reloadQueued = true
      return
    }
    loading.value = true
    error.value = ''
    try {
      const data = await readJSON<{
        sessions?: MeetingRecord[]
        import?: Partial<MeetingImportAvailability>
      }>(await fetch('/api/live-share/notes/sessions'), 'meeting list')
      sessions.value = Array.isArray(data.sessions) ? data.sessions : []
      if (data.import) {
        importAvailability.value = {
          available: data.import.available !== false,
          reason: data.import.reason ?? '',
        }
      }
    } catch (e) {
      appLog.w(TAG, 'load failed', e)
      error.value = String(e)
    } finally {
      loading.value = false
      if (reloadQueued) {
        reloadQueued = false
        await load()
      }
    }
  }

  /** One record by token (used right after a meeting ends). */
  async function get(token: string): Promise<MeetingRecord | null> {
    if (!token) return null
    try {
      const data = await readJSON<{ session?: MeetingRecord }>(
        await fetch(`/api/live-share/notes/sessions?token=${encodeURIComponent(token)}`),
        'meeting record',
      )
      return data.session ?? null
    } catch (e) {
      appLog.w(TAG, 'get failed', e)
      return null
    }
  }

  /**
   * Read one text artifact. Returns null when it does not exist (a meeting with
   * no minutes is normal, not an error).
   */
  async function artifact(
    token: string,
    kind: MeetingArtifactReadKind,
  ): Promise<{ content: string; file: string } | null> {
    try {
      const resp = await fetch(
        `/api/live-share/notes/sessions/artifact?token=${encodeURIComponent(token)}&kind=${kind}`,
      )
      if (resp.status === 404) return null
      const data = await readJSON<{ content?: string; file?: string }>(resp, 'meeting artifact')
      return { content: data.content ?? '', file: data.file ?? '' }
    } catch (e) {
      appLog.w(TAG, 'artifact failed', e)
      return null
    }
  }

  async function chat(token: string, since = 0): Promise<{ messages: MeetingChatMessage[]; count: number }> {
    const data = await readJSON<{ messages?: MeetingChatMessage[]; count?: number }>(
      await fetch(
        `/api/live-share/notes/sessions/chat?token=${encodeURIComponent(token)}&since=${since}`,
      ),
      'meeting chat',
    )
    return { messages: data.messages ?? [], count: data.count ?? 0 }
  }

  /** The meeting's key frames, already ordered and offset onto the timeline. */
  async function shots(token: string): Promise<MeetingShot[]> {
    try {
      const data = await readJSON<{ shots?: MeetingShot[] }>(
        await fetch(`/api/live-share/notes/shots?token=${encodeURIComponent(token)}`),
        'meeting shots',
      )
      return Array.isArray(data.shots) ? data.shots : []
    } catch (e) {
      appLog.w(TAG, 'shots failed', e)
      return []
    }
  }

  /** Remove one frame (row + file). */
  async function deleteShot(token: string, id: number): Promise<boolean> {
    try {
      const resp = await fetch(
        `/api/live-share/notes/shot?token=${encodeURIComponent(token)}&id=${id}`,
        { method: 'DELETE' },
      )
      return resp.ok
    } catch (e) {
      appLog.w(TAG, 'delete shot failed', e)
      return false
    }
  }

  /**
   * Preview what a regeneration would run. The confirmation sheet renders this
   * exact plan; the POST recomputes it server-side.
   */
  async function regenPlan(
    token: string,
    target: MeetingArtifactKind,
    includeDownstream: boolean,
  ): Promise<RegenPlan | null> {
    try {
      const query = new URLSearchParams({
        token,
        target,
        downstream: includeDownstream ? '1' : '0',
      })
      const resp = await fetch(`/api/live-share/notes/regenerate/plan?${query.toString()}`)
      if (!resp.ok) return null
      return (await resp.json()) as RegenPlan
    } catch (e) {
      appLog.w(TAG, 'regen plan failed', e)
      return null
    }
  }

  /**
   * Start a regeneration. A 409 carries the blocked plan (no source, or the
   * meeting is still live); the caller shows it instead of pretending the
   * request worked.
   */
  async function regenerate(
    token: string,
    target: MeetingArtifactKind,
    includeDownstream: boolean,
  ): Promise<{ ok: boolean; session?: MeetingRecord; plan?: RegenPlan; error?: string }> {
    try {
      const resp = await fetch('/api/live-share/notes/regenerate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, target, includeDownstream }),
      })
      if (resp.status === 409) {
        const data = (await resp.json().catch(() => ({}))) as { plan?: RegenPlan; error?: string }
        return { ok: false, plan: data.plan, error: data.error }
      }
      if (!resp.ok) return { ok: false }
      const data = (await resp.json()) as { session?: MeetingRecord }
      return { ok: true, session: data.session }
    } catch (e) {
      appLog.w(TAG, 'regenerate failed', e)
      return { ok: false }
    }
  }

  /** Ask the server to stop the running chain. */
  async function cancelRegeneration(token: string): Promise<boolean> {
    try {
      const resp = await fetch('/api/live-share/notes/regenerate/cancel', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
      })
      return resp.ok
    } catch (e) {
      appLog.w(TAG, 'cancel regeneration failed', e)
      return false
    }
  }

  /**
   * Rename one meeting. Only the record's title changes: the folder keeps the
   * name it was created with, because every artifact path points at it.
   *
   * Returns the refreshed record (the caller redraws its header from it), or
   * null when the server refused — an empty title, an unknown token, a failed
   * request. The list copy is updated in place so going back shows the new
   * name without a reload.
   */
  async function rename(token: string, title: string): Promise<MeetingRecord | null> {
    const trimmed = title.trim()
    if (!token || !trimmed) return null
    try {
      const resp = await fetch(`/api/live-share/notes/sessions?token=${encodeURIComponent(token)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: trimmed }),
      })
      if (!resp.ok) return null
      const data = (await resp.json()) as { session?: MeetingRecord }
      const record = data.session ?? null
      if (record) {
        const index = sessions.value.findIndex((s) => s.token === token)
        if (index >= 0) sessions.value[index] = record
      }
      return record
    } catch (e) {
      appLog.w(TAG, 'rename failed', e)
      return null
    }
  }

  /**
   * Upload one recording (or video) and turn it into a meeting.
   *
   * XHR rather than fetch: a meeting recording is hundreds of megabytes and
   * `fetch` reports no upload progress, so the user would stare at nothing for
   * minutes. The server answers as soon as the file is stored — decoding and
   * transcription continue in the background and show up through the record's
   * artifact state, which the list already polls.
   */
  function importFile(file: File, onProgress?: (percent: number) => void): Promise<MeetingImportResult> {
    return new Promise((resolve) => {
      const form = new FormData()
      // The file's own timestamp, so a recording made last week is filed under
      // the day it happened rather than the day it was dragged in.
      form.append('startedAt', String(file.lastModified || 0))
      form.append('file', file, file.name)
      const xhr = new XMLHttpRequest()
      xhr.open('POST', '/api/live-share/notes/import')
      xhr.upload.onprogress = (event) => {
        if (event.lengthComputable && event.total > 0) {
          onProgress?.(Math.round((event.loaded / event.total) * 100))
        }
      }
      xhr.onload = () => {
        const payload = parseImportResponse(xhr.responseText)
        if (xhr.status >= 200 && xhr.status < 300 && payload?.token) {
          onProgress?.(100)
          resolve({ ok: true, token: payload.token })
          return
        }
        appLog.w(TAG, 'import failed', xhr.status, payload?.msgKey ?? '')
        resolve({ ok: false, reason: payload?.msgKey || payload?.error || `HTTP ${xhr.status}` })
      }
      xhr.onerror = () => resolve({ ok: false, reason: 'network' })
      xhr.onabort = () => resolve({ ok: false, reason: 'aborted' })
      xhr.send(form)
    })
  }

  /**
   * Delete one meeting (folder + rows). The server refuses while the meeting is
   * running or while any artifact job is running; both surface as `409`.
   */
  async function remove(token: string): Promise<{ ok: true } | { ok: false; reason: 'busy' | 'failed' }> {
    try {
      const resp = await fetch(`/api/live-share/notes/sessions?token=${encodeURIComponent(token)}`, {
        method: 'DELETE',
      })
      if (resp.status === 409) return { ok: false, reason: 'busy' }
      if (!resp.ok) return { ok: false, reason: 'failed' }
      sessions.value = sessions.value.filter((s) => s.token !== token)
      return { ok: true }
    } catch (e) {
      appLog.w(TAG, 'delete failed', e)
      return { ok: false, reason: 'failed' }
    }
  }

  /**
   * Build the quote payload for a meeting, fetching its content first.
   *
   * The drag itself carries only the identity: `dragstart` must write the
   * DataTransfer synchronously, and a list row has no text to carry. Fetching
   * here keeps the list drag and the detail button on the SAME path, so both
   * produce an identical card.
   *
   * Returns null when there is nothing to attach — a quote with only a title
   * would waste a prompt slot and tell the model nothing.
   */
  async function buildQuote(record: Pick<MeetingRecord, 'token' | 'title' | 'startedAt'>): Promise<QuoteData | null> {
    const token = record.token
    if (!token) return null
    let content = ''
    for (const kind of ['minutes', 'revised', 'transcript'] as const) {
      const found = await artifact(token, kind)
      if (found?.content) {
        content = found.content
        break
      }
    }
    if (!content.trim()) return null
    return {
      text: clipQuoteText(content),
      filePath: meetingQuoteLabel(record),
      language: 'meeting',
      startLine: 0,
      endLine: 0,
      sourceKind: 'meeting',
      meetingToken: token,
    }
  }

  return {
    sessions,
    loading,
    error,
    importAvailability,
    load,
    get,
    artifact,
    chat,
    shots,
    deleteShot,
    regenPlan,
    regenerate,
    cancelRegeneration,
    importFile,
    remove,
    rename,
    buildQuote,
  }
}
