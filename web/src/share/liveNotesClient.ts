/**
 * Viewer-side AI 听记 client.
 *
 * The transcript is not part of the P2P media stream — the publisher uploads it
 * over HTTP — so a viewer reads it by polling the public, token-scoped endpoint.
 * Polling (rather than a socket) keeps the viewer's connection model unchanged:
 * the only socket on this page is the signalling one, and losing the transcript
 * must never disturb the video.
 *
 * Polling only runs while the panel is open (the panel is collapsed by
 * default), so a viewer who is just watching costs nothing.
 */
import { appLog } from '@/utils/appLog'
import { mergeNoteSegments, type LiveNoteSegment } from '@/composables/liveNotes'
import { shareApiUrl } from './shareMode'

const TAG = 'LiveNotesClient'

export interface ShareNotesMinutes {
  status: string
  markdown?: string
  error?: string
  /** Proofreading stage; `text` is only present in the record view. */
  revise?: ShareNotesRevise
}

export interface ShareNotesRevise {
  status: string
  text?: string
  error?: string
}

/** One archived chat line, as the record view receives it. */
export interface ShareChatMessage {
  speaker: string
  text: string
  at: number
}

/**
 * The meeting record as a viewer reads it after the meeting ended.
 *
 * The share link is the credential and it outlives the room: this is the same
 * `/notes` endpoint the live poll uses, asked with `full=1` for the two parts
 * the poll skips (proofread text and the archived chat).
 */
export interface ShareMeetingRecord {
  title: string
  startedAt: number
  endedAt: number
  ended: boolean
  segments: LiveNoteSegment[]
  files: ShareNotesFile[]
  minutes: ShareNotesMinutes
  chat: ShareChatMessage[]
}

export interface ShareNotesSnapshot {
  segments: LiveNoteSegment[]
  files: ShareNotesFile[]
  minutes: ShareNotesMinutes
}

/**
 * A file event as a viewer receives it.
 *
 * No path: the server redacts it for the public endpoint. The name is enough to
 * follow the meeting, and it is all a viewer is entitled to know.
 */
export interface ShareNotesFile {
  name: string
  openedAt: number
  closedAt: number
}

/** Shape of the public transcript payload. */
interface NotesPayload {
  title?: string
  startedAt?: number
  endedAt?: number
  ended?: boolean
  segments?: LiveNoteSegment[]
  files?: ShareNotesFile[]
  minutes?: ShareNotesMinutes
  chat?: ShareChatMessage[]
}

function toSnapshot(data: NotesPayload): ShareNotesSnapshot {
  return {
    segments: Array.isArray(data.segments) ? data.segments : [],
    files: Array.isArray(data.files) ? data.files : [],
    minutes: data.minutes ?? { status: 'idle' },
  }
}

/**
 * Read the transcript ONCE, without starting the poll.
 *
 * The 听记 tab only exists while the publisher has it on — but a viewer may join
 * after it was turned off, and the lines recorded before that are still on the
 * server. This answers "did this meeting record anything?" so the tab (and the
 * content behind it) is not lost for that viewer. `null` means "could not tell"
 * — a 404, a dropped request — and the caller keeps the tab hidden.
 */
export async function fetchNotesSnapshot(
  fetchImpl: typeof fetch = (input, init) => fetch(input, init),
): Promise<ShareNotesSnapshot | null> {
  try {
    const resp = await fetchImpl(shareApiUrl('notes?since=0'))
    if (!resp.ok) return null
    return toSnapshot((await resp.json()) as NotesPayload)
  } catch (e) {
    appLog.d(TAG, 'notes probe failed', e)
    return null
  }
}

/**
 * Read the whole meeting record in one request (`?full=1`).
 *
 * Used by the viewer's record view once the meeting is over — and by the same
 * view when a link is opened days later, which is why it does not depend on
 * the room existing. `null` means the token no longer resolves (the record was
 * deleted) or the request failed; the caller shows the not-found state.
 */
export async function fetchMeetingRecord(
  fetchImpl: typeof fetch = (input, init) => fetch(input, init),
): Promise<ShareMeetingRecord | null> {
  try {
    const resp = await fetchImpl(shareApiUrl('notes?since=0&full=1'))
    if (!resp.ok) return null
    const data = (await resp.json()) as NotesPayload
    return {
      title: data.title ?? '',
      startedAt: data.startedAt ?? 0,
      endedAt: data.endedAt ?? 0,
      ended: Boolean(data.ended),
      segments: Array.isArray(data.segments) ? data.segments : [],
      files: Array.isArray(data.files) ? data.files : [],
      minutes: data.minutes ?? { status: 'idle' },
      chat: Array.isArray(data.chat) ? data.chat : [],
    }
  } catch (e) {
    appLog.d(TAG, 'record fetch failed', e)
    return null
  }
}

export class ShareNotesPoller {
  private timer: ReturnType<typeof setTimeout> | null = null
  private stopped = false
  private segments: LiveNoteSegment[] = []
  private files: ShareNotesFile[] = []

  constructor(
    private readonly onUpdate: (snapshot: ShareNotesSnapshot) => void,
    private readonly intervalMs = 3000,
    private readonly fetchImpl: typeof fetch = (input, init) => fetch(input, init),
  ) {}

  start(): void {
    if (this.timer !== null || this.stopped) return
    void this.poll()
  }

  stop(): void {
    this.stopped = true
    if (this.timer !== null) {
      clearTimeout(this.timer)
      this.timer = null
    }
  }

  private async poll(): Promise<void> {
    if (this.stopped) return
    try {
      const since = this.maxId()
      const resp = await this.fetchImpl(shareApiUrl(`notes?since=${since}`))
      if (resp.status === 404) {
        // The share ended: stop polling rather than hammering a dead token.
        this.stop()
        return
      }
      if (!resp.ok) throw new Error(`notes poll failed: ${resp.status}`)
      const data = (await resp.json()) as {
        segments?: LiveNoteSegment[]
        files?: ShareNotesFile[]
        minutes?: ShareNotesMinutes
      }
      if (data.segments?.length) {
        this.segments = mergeNoteSegments(this.segments, data.segments)
      }
      // The public endpoint always returns the whole list (there is no `since`
      // for files), so this is a replace, not a merge.
      this.files = Array.isArray(data.files) ? data.files : []
      this.onUpdate({
        segments: this.segments,
        files: this.files,
        minutes: data.minutes ?? { status: 'idle' },
      })
    } catch (e) {
      appLog.d(TAG, 'poll failed', e)
    }
    if (this.stopped) return
    this.timer = setTimeout(() => {
      this.timer = null
      void this.poll()
    }, this.intervalMs)
  }

  private maxId(): number {
    let max = 0
    for (const s of this.segments) if (s.id > max) max = s.id
    return max
  }
}
