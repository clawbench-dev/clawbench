import type { ContentMatchRange } from '@/composables/useFileContentSearch'
import { escapeHtml } from '@/utils/fileSearchMark'

/**
 * Highlight rune-offset ranges inside a line of text.
 *
 * Ranges are RUNE (code point) offsets, matching the Go backend's
 * `utf8.DecodeRuneInString` walk — not UTF-16 indices and not byte offsets.
 * Spreading the string yields code points, so the two coordinate systems line
 * up even when the line contains surrogate pairs or multi-byte CJK.
 *
 * SECURITY: each character is escaped individually before any markup is added,
 * so a line containing `<script>` (or a file whose content is attacker
 * controlled) cannot inject HTML.
 *
 * Overlapping and adjacent ranges are merged first: the backend can report
 * overlapping matches for a pattern like `aa` against `aaa`, and emitting
 * nested `<mark>` tags would produce broken markup.
 */
export function highlightRanges(text: string, ranges: ContentMatchRange[] | undefined): string {
  const runes = [...text]
  if (!ranges || ranges.length === 0) return escapeHtml(text)

  const merged = mergeRanges(ranges, runes.length)
  if (merged.length === 0) return escapeHtml(text)

  let result = ''
  let cursor = 0
  for (const range of merged) {
    if (range.start > cursor) {
      result += escapeRunes(runes.slice(cursor, range.start))
    }
    result += `<mark>${escapeRunes(runes.slice(range.start, range.end))}</mark>`
    cursor = range.end
  }
  if (cursor < runes.length) {
    result += escapeRunes(runes.slice(cursor))
  }
  return result
}

/** Escape a run of characters, one at a time (keeps surrogate pairs intact). */
function escapeRunes(runes: string[]): string {
  let out = ''
  for (const ch of runes) out += escapeHtml(ch)
  return out
}

/**
 * Sort, clamp and coalesce ranges. Touching ranges (end === next.start) are
 * merged too, so `a` + `b` adjacent matches render as one continuous highlight
 * rather than two adjacent marks.
 */
export function mergeRanges(ranges: ContentMatchRange[], length: number): ContentMatchRange[] {
  const cleaned = ranges
    .map(r => ({
      start: Math.max(0, Math.min(r.start, length)),
      end: Math.max(0, Math.min(r.end, length)),
    }))
    .filter(r => r.end > r.start)
    .sort((a, b) => a.start - b.start || a.end - b.end)

  const out: ContentMatchRange[] = []
  for (const r of cleaned) {
    const last = out[out.length - 1]
    if (last && r.start <= last.end) {
      if (r.end > last.end) last.end = r.end
    } else {
      out.push({ ...r })
    }
  }
  return out
}

/** Directory portion of a path, or '' when the file sits at the root. */
export function parentDirOf(path: string): string {
  const i = path.lastIndexOf('/')
  return i > 0 ? path.slice(0, i) : ''
}
