/**
 * Completion matching — pure functions shared by the slash-command and
 * @-file-reference menus in the chat input.
 *
 * Kept dependency-free (besides path helpers) so the fuzzy scoring, candidate
 * merging and trigger parsing can be unit-tested in isolation.
 */

import { baseName, dirName, normalizeSlashes, toProjectRelative } from '@/utils/path.ts'

// ── Fuzzy matching ─────────────────────────────────────────

const CONSECUTIVE_BONUS = 5
const BOUNDARY_BONUS = 3

/** Separators that mark a word boundary for scoring purposes. */
const SEPARATOR_RE = /[/\-_.\s]/

export interface FuzzyMatchResult {
  score: number
  /** Matched character indices in the target, in ascending order. */
  positions: number[]
}

/**
 * Whether the character at `index` sits on a word boundary: the start of the
 * string, right after a separator, or at a camelCase transition (aB).
 * Uses the ORIGINAL (non-lowercased) target so camelCase is detectable.
 */
function isBoundary(target: string, index: number): boolean {
  if (index === 0) return true
  const prev = target[index - 1]
  if (SEPARATOR_RE.test(prev)) return true
  const cur = target[index]
  return prev === prev.toLowerCase() && cur !== cur.toLowerCase()
}

/**
 * Case-insensitive subsequence match: every query character must appear in
 * the target in order. Consecutive and word-boundary hits score higher so
 * more relevant candidates sort first.
 *
 * Returns null when the query does not match. An empty query matches with a
 * zero score and no positions.
 */
export function fuzzyMatch(query: string, target: string): FuzzyMatchResult | null {
  if (!query) return { score: 0, positions: [] }

  const q = query.toLowerCase()
  const t = target.toLowerCase()
  const positions: number[] = []
  let score = 0
  let cursor = 0

  for (let qi = 0; qi < q.length; qi++) {
    const ch = q[qi]
    let found = -1
    while (cursor < t.length) {
      if (t[cursor] === ch) {
        found = cursor
        break
      }
      cursor++
    }
    if (found === -1) return null

    positions.push(found)
    score += 1
    if (positions.length > 1 && found === positions[positions.length - 2] + 1) {
      score += CONSECUTIVE_BONUS
    }
    if (isBoundary(target, found)) score += BOUNDARY_BONUS
    cursor = found + 1
  }

  return { score, positions }
}

// ── Candidate sources ──────────────────────────────────────

export type CompletionSource =
  | 'recent-open'
  | 'current-dir'
  | 'recent-ref'
  | 'recent-upload'
  | 'recent-share'
  | 'clawbench'
  | 'agent'

/** Source priority: lower index wins a dedupe conflict / an empty-query tie. */
export const SOURCE_PRIORITY: CompletionSource[] = [
  'recent-open',
  'current-dir',
  'recent-ref',
  'recent-upload',
  'recent-share',
]

export interface PathCandidate {
  path: string
  /** True when the candidate is a directory (attachments carry this to the backend). */
  isDir?: boolean
}

export interface FileCandidateSources {
  recentOpen?: PathCandidate[]
  currentDir?: PathCandidate[]
  recentRef?: PathCandidate[]
  recentUpload?: PathCandidate[]
  recentShare?: PathCandidate[]
}

export interface CompletionItem {
  /** Stable identity — the normalized project-relative path, or the command key. */
  key: string
  label: string
  description: string
  source: CompletionSource
  /**
   * Whether the candidate is a directory. Preserved from the source so the
   * select handler can attach it with the right isDir flag (the backend
   * resolves dirs differently from files).
   */
  isDir?: boolean
  /** Optional row icon component (files pass FileIcon; commands omit it). */
  icon?: unknown
  /** Matched basename indices for per-character highlighting. */
  positions?: number[]
  score?: number
}

const MAX_CANDIDATES = 50

function normalizePath(path: string): string {
  return normalizeSlashes(path).replace(/^\.\/+/, '').replace(/\/+$/, '')
}

/**
 * Canonicalize a path for identity comparison: normalize slashes / "./" /
 * trailing slash, and — when a project root is known — relativize absolute
 * paths that lie under it. Without the relativization an absolute attachment
 * path would never match the project-relative candidate of the same file, so
 * the "already attached" filter would silently fail.
 */
function canonicalPath(path: string, projectRoot?: string): string {
  const normalized = normalizePath(path)
  if (!projectRoot) return normalized
  return toProjectRelative(normalized, normalizeSlashes(projectRoot))
}

/**
 * Merge the five file sources into one ranked candidate list.
 *
 * Dedupes by normalized path (highest-priority source wins), drops paths that
 * are already attached, fuzzy-matches the basename when a query is present,
 * sorts, and truncates to {@link MAX_CANDIDATES}.
 *
 * `projectRoot` (when provided) makes the attached-path comparison robust to
 * absolute-vs-relative spellings of the same file.
 */
export function buildFileCandidates(
  sources: FileCandidateSources,
  query: string,
  attachedPaths: string[] = [],
  projectRoot?: string,
): CompletionItem[] {
  const attached = new Set(attachedPaths.map(p => canonicalPath(p, projectRoot)))

  const ordered: { source: CompletionSource; list: PathCandidate[] }[] = [
    { source: 'recent-open', list: sources.recentOpen || [] },
    { source: 'current-dir', list: sources.currentDir || [] },
    { source: 'recent-ref', list: sources.recentRef || [] },
    { source: 'recent-upload', list: sources.recentUpload || [] },
    { source: 'recent-share', list: sources.recentShare || [] },
  ]

  const byPath = new Map<string, CompletionItem>()
  for (const { source, list } of ordered) {
    for (const candidate of list) {
      const key = canonicalPath(candidate.path || '', projectRoot)
      if (!key || attached.has(key) || byPath.has(key)) continue
      byPath.set(key, {
        key,
        label: baseName(key),
        description: dirName(key),
        source,
        isDir: candidate.isDir === true,
      })
    }
  }

  let items = [...byPath.values()]

  if (query) {
    const matched: CompletionItem[] = []
    for (const item of items) {
      const m = fuzzyMatch(query, item.label)
      if (!m) continue
      matched.push({ ...item, positions: m.positions, score: m.score })
    }
    items = matched
  }

  const priority = new Map(SOURCE_PRIORITY.map((s, i) => [s, i]))
  items.sort((a, b) => {
    if (query) {
      const diff = (b.score || 0) - (a.score || 0)
      if (diff !== 0) return diff
    }
    const pa = priority.get(a.source) ?? SOURCE_PRIORITY.length
    const pb = priority.get(b.source) ?? SOURCE_PRIORITY.length
    if (pa !== pb) return pa - pb
    return a.key.localeCompare(b.key)
  })

  return items.slice(0, MAX_CANDIDATES)
}

// ── Trigger parsing ────────────────────────────────────────

export interface TriggerRange {
  start: number
  end: number
  query: string
}

/**
 * Parse an `@` file-reference trigger from the text left of the caret.
 *
 * The `@` must sit at the start of a line or right after whitespace (so an
 * email-like `a@b` never triggers). The query runs from `@` to the caret and
 * must not contain whitespace. Returns null when there is no active trigger.
 */
export function parseAtQuery(text: string, caret: number): TriggerRange | null {
  const left = text.slice(0, caret)
  const at = left.lastIndexOf('@')
  if (at === -1) return null
  if (at > 0 && !/\s/.test(left[at - 1])) return null
  const query = left.slice(at + 1)
  if (/\s/.test(query)) return null
  return { start: at, end: caret, query }
}

/**
 * Parse a slash-command trigger. Preserved semantics: the input must start
 * with `/` and contain no space, in which case the whole text is the query.
 */
export function parseSlashQuery(text: string): TriggerRange | null {
  if (!text.startsWith('/') || text.includes(' ')) return null
  return { start: 0, end: text.length, query: text.slice(1) }
}

// ── Display helpers ────────────────────────────────────────

/**
 * Truncate a long path in the middle so both the leading segment (where the
 * file lives) and the trailing segment (nearest the filename) stay visible.
 * Returns the input unchanged when it already fits.
 */
export function middleEllipsis(text: string, maxLen: number): string {
  if (!text) return ''
  if (maxLen <= 1) return '…'
  if (text.length <= maxLen) return text
  const keep = maxLen - 1 // room for the ellipsis character
  const head = Math.ceil(keep / 2)
  const tail = keep - head
  return text.slice(0, head) + '…' + (tail > 0 ? text.slice(text.length - tail) : '')
}
