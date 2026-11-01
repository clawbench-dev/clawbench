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
  status: 'idle' | 'generating' | 'ready' | 'failed'
  /**
   * What the room carried. Empty means "unknown": records written before the
   * column existed, and rows created by a late segment upload, have no kind —
   * the list draws no type pill for those rather than guessing.
   */
  kind?: 'voice' | 'video' | ''
  /** Distinct speakers the transcript heard; 0 when there is no transcript. */
  speakerCount?: number
  updatedAt: number
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

export type MeetingArtifactKind = 'minutes' | 'transcript' | 'revised' | 'chat'

/** A quote carries at most this much meeting text into the prompt. */
export const MEETING_QUOTE_MAX_CHARS = 4000

async function readJSON<T>(resp: Response, what: string): Promise<T> {
  if (!resp.ok) throw new Error(`${what} failed: ${resp.status}`)
  return (await resp.json()) as T
}

/**
 * A meeting is "stuck generating" when the minutes job was interrupted by a
 * process restart. The row is permanent now, so without this the detail would
 * spin forever on a job nobody is running.
 */
export function isStaleGenerating(record: Pick<MeetingRecord, 'status' | 'updatedAt'>, now = Date.now()): boolean {
  return record.status === 'generating' && now - record.updatedAt > 10 * 60 * 1000
}

/** Truncate meeting text for a quote, saying so when it was cut. */
export function clipQuoteText(text: string, max = MEETING_QUOTE_MAX_CHARS): string {
  const trimmed = text.trim()
  if (trimmed.length <= max) return trimmed
  const note = i18n.global.t('meeting.quote.truncated', { n: trimmed.length })
  return `${trimmed.slice(0, max)}\n${note}`
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

export function useMeetingHistory() {
  const sessions = ref<MeetingRecord[]>([])
  const loading = ref(false)
  const error = ref('')

  async function load(): Promise<void> {
    if (loading.value) return
    loading.value = true
    error.value = ''
    try {
      const data = await readJSON<{ sessions?: MeetingRecord[] }>(
        await fetch('/api/live-share/notes/sessions'),
        'meeting list',
      )
      sessions.value = Array.isArray(data.sessions) ? data.sessions : []
    } catch (e) {
      appLog.w(TAG, 'load failed', e)
      error.value = String(e)
    } finally {
      loading.value = false
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
  async function artifact(token: string, kind: MeetingArtifactKind): Promise<{ content: string; file: string } | null> {
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

  /**
   * Delete one meeting (folder + rows). The server refuses while the meeting is
   * running or while the minutes job is running; both surface as `409`.
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

  return { sessions, loading, error, load, get, artifact, chat, remove, buildQuote }
}
