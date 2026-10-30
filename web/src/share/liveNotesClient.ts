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
}

export interface ShareNotesSnapshot {
  segments: LiveNoteSegment[]
  minutes: ShareNotesMinutes
}

export class ShareNotesPoller {
  private timer: ReturnType<typeof setTimeout> | null = null
  private stopped = false
  private segments: LiveNoteSegment[] = []

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
        minutes?: ShareNotesMinutes
      }
      if (data.segments?.length) {
        this.segments = mergeNoteSegments(this.segments, data.segments)
      }
      this.onUpdate({
        segments: this.segments,
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
