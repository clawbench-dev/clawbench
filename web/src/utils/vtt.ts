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

export function vttToPlainText(vtt: string): string {
  const cues: string[] = []
  for (const block of vtt.split(/\r?\n\r?\n+/)) {
    const lines = block.split(/\r?\n/)
    const timing = lines.findIndex((line) => line.includes(TIMING))
    // The header, NOTE/STYLE/REGION blocks and stray blank lines carry no text.
    if (timing < 0) continue
    const payload = lines.slice(timing + 1).join(' ').trim()
    if (payload) cues.push(renderCue(payload))
  }
  return cues.join('\n\n')
}

/**
 * Turns one cue payload into a readable line.
 *
 * The voice span becomes a speaker prefix so the result reads exactly like the
 * live transcript panel. Entities are unescaped last-to-first for `&amp;`: a
 * literal "&lt;" in the meeting arrives as "&amp;lt;" and must come out as
 * "&lt;", not "<".
 */
function renderCue(payload: string): string {
  return payload
    .replace(/<v(?:\.[^\s>]*)?\s+([^>]+)>/g, '$1：')
    .replace(/<\/v>/g, '')
    // Any other inline tag (b / i / u / c.class / lang) is formatting we do not render.
    .replace(/<[^>]*>/g, '')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&')
}
