import { computed } from 'vue'

export type FileBackTarget = 'file' | 'browse' | 'origin' | null

/**
 * Share the return destination between the file header and the back handler.
 *
 * Precedence: an in-file jump (previous entry in the file history) is spent
 * first — it belongs to the visit the user is looking at. Only when the visit
 * has no history left does the visit itself end, and then a file opened from
 * the file manager (`browseSession`) returns to its directory rather than
 * skipping straight to the jump origin.
 *
 * The excursion case is why `browse` outranks `origin`: unwinding
 * "file → directory → file C" must go through the browse panel (Back →
 * directory → Back → origin file) instead of jumping straight to the origin
 * file and skipping the directory in between.
 */
export function useFileBackTarget(state: () => {
  browseSession: boolean
  canGoBackFile: boolean
  hasOrigin: boolean
}) {
  return computed<FileBackTarget>(() => {
    const current = state()
    if (current.canGoBackFile) return 'file'
    if (current.browseSession) return 'browse'
    if (current.hasOrigin) return 'origin'
    return null
  })
}
