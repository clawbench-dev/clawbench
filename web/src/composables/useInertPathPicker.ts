import { reactive, readonly } from 'vue'
import type { NavigationSurface } from '@/composables/useNavigationContext'

/**
 * State for the "search for a verified-missing path by filename" picker.
 *
 * A chat path annotation is verified against exactly TWO candidates (relative
 * to the file's own directory, then relative to the project root — see
 * `resolveFilePathDual`). When both miss, the file usually still exists
 * somewhere else in the project: the AI simply guessed the wrong prefix. The
 * chip therefore becomes a filename-search entry point rather than a dead end.
 *
 * Module-level singleton (like `useSessionIdentity` / `fileNav`): the click
 * layer that opens it is installed once at the document level and has no
 * component instance to own the state.
 */
export interface InertPathPickerState {
  open: boolean
  /** Filename to search for (basename of the failed path). */
  query: string
  /** Full path as annotated, shown in the panel header for context. */
  sourcePath: string
  /** Line target stashed on the chip (`data-inert-line-*`), if any. */
  lineStart?: number
  lineEnd?: number
  lineRanges?: string
  /** Surface the click came from, forwarded to `openFilePath`. */
  source?: NavigationSurface
}

const _state = reactive<InertPathPickerState>({
  open: false,
  query: '',
  sourcePath: '',
  lineStart: undefined,
  lineEnd: undefined,
  lineRanges: undefined,
  source: undefined,
})

/** Read-only state for the panel component. */
export const inertPathPickerState = readonly(_state) as Readonly<InertPathPickerState>

/** @internal Reset all state — for tests only. */
export function _resetInertPathPickerForTesting(): void {
  _state.open = false
  _state.query = ''
  _state.sourcePath = ''
  _state.lineStart = undefined
  _state.lineEnd = undefined
  _state.lineRanges = undefined
  _state.source = undefined
}

/**
 * Reduce an annotated path to the filename to search for.
 *
 * The backend's `exact` match compares the query against a directory entry's
 * basename (`matchName(d.Name(), query, exact)` in dir_search.go), NOT against
 * the full path — so passing the whole path would never match. The basename is
 * also exactly the part that survives a wrong prefix guess, which is the case
 * this feature exists for.
 *
 * A trailing line suffix (`foo.go:42`, `foo.go#L42`) is not part of the name.
 * Glob metacharacters should never reach here (glob chips are not clickable),
 * but they are stripped defensively so a stray `*` cannot poison the query.
 */
export function deriveSearchQuery(pathOrText: string | null | undefined): string {
  const raw = (pathOrText ?? '').trim()
  if (!raw) return ''

  // Drop a trailing `:42`, `:42-48`, `:42,90` or `#L42` line reference.
  const withoutLine = raw.replace(/[:#]L?\d+(?:[-,]\d+)*$/i, '')
  // Strip glob metacharacters: the query is a literal filename.
  const withoutGlob = withoutLine.replace(/[*?[\]<>]/g, '')
  const segments = withoutGlob.split(/[/\\]/).filter(Boolean)
  const base = segments.length > 0 ? segments[segments.length - 1] : ''
  // A path that was only a glob (`**/` strips to `/`) leaves nothing usable as
  // a filename. Fall back to the original text so the user sees what was
  // clicked instead of searching for a bare separator.
  return base || raw
}

/** Open the picker for a verified-missing path. */
export function openInertPathPicker(detail: {
  path: string
  lineStart?: number
  lineEnd?: number
  lineRanges?: string
  source?: NavigationSurface
}): void {
  const query = deriveSearchQuery(detail.path)
  if (!query) return

  _state.query = query
  _state.sourcePath = detail.path
  _state.lineStart = detail.lineStart
  _state.lineEnd = detail.lineEnd
  _state.lineRanges = detail.lineRanges
  _state.source = detail.source
  _state.open = true
}

/** Close the picker and drop the query (so a stale search cannot linger). */
export function closeInertPathPicker(): void {
  _state.open = false
  _state.query = ''
  _state.sourcePath = ''
  _state.lineStart = undefined
  _state.lineEnd = undefined
  _state.lineRanges = undefined
  _state.source = undefined
}

/**
 * Consume the stashed line target and close, returning what to pass on to
 * `openFilePath`. Kept here so the panel does not read mutable singleton state
 * after closing it.
 */
export function takeInertPathTarget(): {
  lineStart?: number
  lineEnd?: number
  lineRanges?: string
  source?: NavigationSurface
} {
  const target = {
    lineStart: _state.lineStart,
    lineEnd: _state.lineEnd,
    lineRanges: _state.lineRanges,
    source: _state.source,
  }
  closeInertPathPicker()
  return target
}
