/**
 * Line-range suffix parsing for file-path annotations.
 *
 * A path annotation may carry a single line/range or a comma-separated list,
 * e.g. `path.go:90-91,309,324,343,938-943`. Each token is `N`, `N-M`, `LN`,
 * `LN-LM` or `LN-M` (the `L` prefix is case-insensitive). Whitespace after a
 * comma is tolerated; tokens may be in any order and are normalized into a
 * sorted, merged, de-duplicated list.
 *
 * This module is the single source of truth for the suffix grammar. The
 * `FILE_PATH_RE` tail in useFilePathAnnotation.ts mirrors LINE_TOKEN_SRC.
 */

export interface LineRange {
    /** 1-based inclusive start line. */
    start: number
    /** 1-based inclusive end line (=== start for a single line). */
    end: number
}

/** Maximum number of ranges accepted from one annotation (defensive cap). */
export const MAX_LINE_RANGES = 100

/**
 * Grammar source for one comma-separated suffix (without the leading `:`).
 * `[ \t]*` (not `\s`) keeps a match from spanning lines.
 */
export const LINE_TOKEN_SRC =
    '[Ll]?\\d+(?:-[Ll]?\\d+)?(?:[ \\t]*,[ \\t]*[Ll]?\\d+(?:-[Ll]?\\d+)?)*'

/** Matches a trailing `:N,N-M,…` suffix; capture group 1 is the token list. */
export const LINE_SUFFIX_RE = new RegExp(':(' + LINE_TOKEN_SRC + ')$', 'i')

/** Matches a bare token list (e.g. a `#L10-L20,309` hash fragment). */
export const LINE_FRAGMENT_RE = new RegExp('^' + LINE_TOKEN_SRC + '$', 'i')

const TOKEN_RE = /^(\d+)(?:-[Ll]?(\d+))?$/

/**
 * Parse a comma-separated line suffix into sorted, merged, non-overlapping
 * ranges. Invalid tokens (zero/negative, malformed) are dropped individually.
 *
 * Backward-compat: an inverted range (`20-10`) degrades to a single line at
 * the start (`20`), matching the historical single-range behavior.
 */
export function parseLineRanges(suffix: string | undefined | null): LineRange[] {
    if (!suffix) return []
    const parsed: LineRange[] = []
    for (const rawToken of suffix.split(',')) {
        if (parsed.length >= MAX_LINE_RANGES) break
        const token = rawToken.trim().replace(/^[Ll]/, '')
        const m = token.match(TOKEN_RE)
        if (!m) continue
        const start = parseInt(m[1], 10)
        if (!(start > 0)) continue
        let end = start
        if (m[2]) {
            const parsedEnd = parseInt(m[2], 10)
            if (parsedEnd >= start) end = parsedEnd
        }
        parsed.push({ start, end })
    }
    return mergeRanges(parsed)
}

/** Sort by start, then merge overlapping or adjacent ranges. */
function mergeRanges(ranges: LineRange[]): LineRange[] {
    if (ranges.length <= 1) return ranges
    const sorted = [...ranges].sort((a, b) => a.start - b.start || a.end - b.end)
    const merged: LineRange[] = [{ ...sorted[0] }]
    for (let i = 1; i < sorted.length; i++) {
        const last = merged[merged.length - 1]
        const cur = sorted[i]
        if (cur.start <= last.end + 1) {
            if (cur.end > last.end) last.end = cur.end
        } else {
            merged.push({ ...cur })
        }
    }
    return merged
}

/** Serialize ranges back to the canonical `N-M,N` suffix form (no `:`). */
export function serializeLineRanges(ranges: LineRange[]): string {
    return ranges
        .map(r => (r.end > r.start ? `${r.start}-${r.end}` : `${r.start}`))
        .join(',')
}

/**
 * The jump target for a range list: the range with the smallest start.
 * `lineEnd` is omitted for a single-line range so legacy consumers keep
 * seeing `undefined` (matching `:10` → `{ lineStart: 10 }`).
 */
export function firstLineTarget(ranges: LineRange[]): { lineStart?: number; lineEnd?: number } {
    if (ranges.length === 0) return {}
    const first = ranges[0]
    return first.end > first.start
        ? { lineStart: first.start, lineEnd: first.end }
        : { lineStart: first.start }
}

/**
 * Flatten ranges into an ascending, de-duplicated list of line numbers.
 * Ascending order is required by CodeMirror's RangeSetBuilder.
 */
export function flattenLineNumbers(ranges: LineRange[], cap = 1000): number[] {
    const out: number[] = []
    for (const r of ranges) {
        for (let n = r.start; n <= r.end && out.length < cap; n++) {
            if (out.length === 0 || n > out[out.length - 1]) out.push(n)
        }
        if (out.length >= cap) break
    }
    return out
}

/** Intersect ranges with an inclusive `[start, end]` window. */
export function clampRanges(ranges: LineRange[], start: number, end: number): LineRange[] {
    const out: LineRange[] = []
    for (const r of ranges) {
        const s = Math.max(r.start, start)
        const e = Math.min(r.end, end)
        if (s <= e) out.push({ start: s, end: e })
    }
    return out
}
