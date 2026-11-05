/**
 * Turning a RUNNING meeting into a quote — one builder for both entries: the
 * stage's quote button/drag handle, and dropping the record row of a meeting
 * that has not finished yet.
 *
 * A finished meeting is addressed by token and its body is fetched from the
 * record's artifacts. A running one has no artifacts at all, so the transcript
 * held in memory is the only content there is; without this the row of a live
 * meeting looked draggable and then said "quote unavailable".
 */
import type { QuoteData } from '@/composables/useChatContext'
import { meetingQuoteLabel } from '@/composables/useMeetingHistory'
import { meetingDragPayload } from '@/utils/quoteDrag'

/** Longest transcript inlined into a quote, matching the record path's cap. */
export const LIVE_QUOTE_MAX = 6000

export interface LiveQuoteSegment {
  speaker?: string
  text?: string
}

/**
 * The transcript so far, as "speaker: text" lines.
 *
 * The speaker prefix is what makes the quote readable to the model: a wall of
 * unattributed lines cannot be summarised into "who said what", which is the
 * whole point of quoting a meeting.
 */
export function liveTranscriptText(segments: readonly LiveQuoteSegment[]): string {
  const joined = segments
    .map((seg) => {
      const text = (seg.text || '').trim()
      if (!text) return ''
      const speaker = (seg.speaker || '').trim()
      return speaker ? `${speaker}: ${text}` : text
    })
    .filter(Boolean)
    .join('\n')
  return joined.length > LIVE_QUOTE_MAX ? `${joined.slice(0, LIVE_QUOTE_MAX)}…` : joined
}

/**
 * Build the quote for a running meeting, or null when there is no token.
 *
 * `startedAt` is 0 on purpose: the meeting is still running, so the card shows
 * its name without a time — the same "no time yet" label the record uses before
 * it is finalised.
 */
export function buildLiveMeetingQuote(args: {
  token: string
  title: string
  segments: readonly LiveQuoteSegment[]
  /**
   * Project-relative notes folder. Absent until the server has written
   * something for this meeting — a meeting that has produced no files has no
   * folder to point the AI at.
   */
  sourceDir?: string
}): QuoteData | null {
  const token = (args.token || '').trim()
  if (!token) return null
  return meetingDragPayload({
    token,
    label: meetingQuoteLabel({ title: args.title || '', startedAt: 0 }),
    text: liveTranscriptText(args.segments),
    sourceDir: args.sourceDir,
  })
}
