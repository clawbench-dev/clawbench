/**
 * Parses `transcript-revised.md` back into the rows the 转写 tab draws.
 *
 * The proofread artifact is plain text on purpose — a human may open and edit
 * it — so it is not a subtitle track and `parseVttCues` cannot read it. The
 * shape is one utterance per line:
 *
 *     [mm:ss] 说话人：内容
 *
 * under a heading and a one-line note. The record's 校对 tab is the same reading
 * surface as its 转写 tab, so the heading, the note and the offsets are stripped
 * here rather than rendered as Markdown: a wall of single-newline lines has no
 * paragraph breaks to preserve, and Markdown would fold all of them into one
 * block.
 */

/** One line of the proofread transcript, in the shape the row list draws. */
export interface RevisedLine {
  /** Speaker label, or '' when the line carries no separator at all. */
  speaker: string
  text: string
  /** Offset from the meeting start; 0 for `[--:--]` and un-timed lines. */
  startMs: number
  /**
   * Set for a file or screen event rather than speech.
   *
   * The writer labels those lines with a localized speaker (`文件` / `File`,
   * `屏幕` / `Screen`) and no other marker, so the kind has to be recovered from
   * the label. Both languages are matched regardless of the UI language: the
   * file was written under whatever locale was active at the time.
   */
  kind?: 'file' | 'screen'
}

/** `[mm:ss] rest`, with `[--:--]` for a line whose timing is unknown. */
const TIMED_LINE = /^\[(?:(\d+):(\d{2})|--:--)\]\s*(.*)$/

/** The separator the writer used: `：` in Chinese, `: ` in English. */
const SEPARATOR = /[:：]/

const FILE_LABELS = new Set(['文件', 'File'])
const SCREEN_LABELS = new Set(['屏幕', 'Screen'])

/**
 * Every utterance of a proofread transcript, in file order.
 *
 * A line without the `[mm:ss]` prefix is kept as text rather than dropped: the
 * model is told to preserve the shape, and when it does not, showing a stray
 * paragraph beats losing part of the meeting.
 */
export function parseRevisedTranscript(markdown: string): RevisedLine[] {
  const out: RevisedLine[] = []
  for (const raw of markdown.split(/\r?\n/)) {
    const line = raw.trim()
    // Heading and the note under it are document chrome, not speech.
    if (!line || line.startsWith('#') || line.startsWith('>')) continue

    const m = TIMED_LINE.exec(line)
    if (!m) {
      out.push({ speaker: '', text: line, startMs: 0 })
      continue
    }
    const payload = (m[3] ?? '').trim()
    if (!payload) continue
    const startMs = m[1] ? Number(m[1]) * 60_000 + Number(m[2]) * 1000 : 0
    const cut = payload.search(SEPARATOR)
    const speaker = cut >= 0 ? payload.slice(0, cut).trim() : ''
    const text = cut >= 0 ? payload.slice(cut + 1).trim() : payload
    // A label with nothing after it is a blank cue, not something to draw.
    if (!text) continue
    const kind = FILE_LABELS.has(speaker)
      ? ('file' as const)
      : SCREEN_LABELS.has(speaker)
        ? ('screen' as const)
        : undefined
    out.push({ speaker, text, startMs, ...(kind ? { kind } : {}) })
  }
  return out
}

/**
 * The copy payload: the same `说话人：内容` lines, without the document chrome.
 *
 * Mirrors `vttToPlainText` so the two transcript tabs copy the same shape, and
 * so pasting a proofread transcript into a chat does not carry a heading and a
 * note that only make sense inside the file.
 */
export function revisedToPlainText(markdown: string): string {
  return parseRevisedTranscript(markdown)
    .map((line) => (line.speaker ? `${line.speaker}：${line.text}` : line.text))
    .join('\n\n')
}
