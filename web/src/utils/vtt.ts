/**
 * Renders a stored WebVTT transcript as the plain "说话人：内容" view.
 *
 * The transcript artifact is a subtitle track: a WEBVTT header, one cue per
 * utterance, the speaker carried by a cue identifier (`mic` / `viewer:v1`) and
 * a voice span (`<v 名字>`). That shape is what makes the file playable and
 * re-attributable later, but the transcript tab is a reading surface — printing
 * the raw markup there buries the conversation under timestamps.
 *
 * Only the display path goes through here; the file on disk keeps its markup.
 */

/** The one token that separates a cue's timing line from its payload. */
const TIMING = '-->'

/** One cue, parsed back into the shape the transcript UI draws. */
export interface VttCue {
  /** Voice-span name, or '' for a cue the writer emitted without one. */
  speaker: string
  text: string
  startMs: number
  endMs: number
  /**
   * Set for a file event rather than speech.
   *
   * The writer marks these with the cue id `file` (see LiveNoteFileCueID in
   * internal/service). The payload still carries a voice span — `<v 文件>` — so
   * a plain subtitle player shows something readable.
   */
  kind?: 'file' | 'screen'
}

/**
 * Parses a stored transcript back into cues.
 *
 * The meeting record's transcript tab shows the SAME conversation the live
 * panel showed, so it needs the same per-speaker rows, not a wall of text.
 * Everything it needs is still in the file: the voice span carries the speaker
 * and the timing line carries the position.
 */
export function parseVttCues(vtt: string): VttCue[] {
  const cues: VttCue[] = []
  for (const block of vtt.split(/\r?\n\r?\n+/)) {
    const lines = block.split(/\r?\n/)
    const timing = lines.findIndex((line) => line.includes(TIMING))
    // The header, NOTE/STYLE/REGION blocks and stray blank lines carry no text.
    if (timing < 0) continue
    const payload = lines.slice(timing + 1).join(' ').trim()
    if (!payload) continue
    const [startMs, endMs] = parseTiming(lines[timing])
    // The line above the timing line is the cue identifier, when there is one.
    const id = timing > 0 ? lines[timing - 1].trim() : ''
    cues.push({
      speaker: parseSpeaker(payload),
      text: renderCue(payload),
      startMs,
      endMs,
      ...(id === 'file' ? { kind: 'file' as const } : id === 'screen' ? { kind: 'screen' as const } : {}),
    })
  }
  return cues
}

export function vttToPlainText(vtt: string): string {
  return parseVttCues(vtt)
    .map((cue) => (cue.speaker ? `${cue.speaker}：${cue.text}` : cue.text))
    .join('\n\n')
}

/** `HH:MM:SS.mmm --> HH:MM:SS.mmm` (WebVTT also allows `MM:SS.mmm`). */
function parseTiming(line: string): [number, number] {
  const [from, to] = line.split(TIMING)
  return [clockMs(from), clockMs(to)]
}

function clockMs(value: string | undefined): number {
  if (!value) return 0
  // Cue settings may follow the end time (`align:start position:0%`).
  const stamp = value.trim().split(/\s+/)[0] ?? ''
  const m = stamp.match(/^(?:(\d+):)?(\d{1,2}):(\d{2})(?:\.(\d{1,3}))?$/)
  if (!m) return 0
  const [, h, min, sec, frac] = m
  return (
    Number(h ?? 0) * 3_600_000 +
    Number(min) * 60_000 +
    Number(sec) * 1000 +
    Number((frac ?? '').padEnd(3, '0'))
  )
}

/** The voice span's name, which is what the live panel calls the speaker. */
function parseSpeaker(payload: string): string {
  const m = payload.match(/<v(?:\.[^\s>]*)?\s+([^>]+)>/)
  return m ? m[1].trim() : ''
}

/**
 * Turns one cue payload into a readable line.
 *
 * Entities are unescaped last-to-first for `&amp;`: a literal "&lt;" in the
 * meeting arrives as "&amp;lt;" and must come out as "&lt;", not "<".
 */
function renderCue(payload: string): string {
  return payload
    .replace(/<v(?:\.[^\s>]*)?\s+[^>]+>/g, '')
    .replace(/<\/v>/g, '')
    // Any other inline tag (b / i / u / c.class / lang) is formatting we do not render.
    .replace(/<[^>]*>/g, '')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&')
}
