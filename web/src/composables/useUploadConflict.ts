import { ref } from 'vue'

/**
 * The name-collision prompt shown before a drop/paste upload lands on top of
 * existing files.
 *
 * ## Why a prompt at all
 *
 * The upload endpoint disambiguates silently (`report.pdf` → `report_1.pdf`).
 * That is the right default for chat attachments, but when the user drops files
 * into the file manager or the terminal they are usually copying a build
 * artifact over an older one, and a silently renamed copy is a confusing
 * outcome. So the batch is preflighted against the target directory and the
 * user picks: overwrite, auto-rename, or cancel.
 *
 * ## Why a singleton
 *
 * The upload path lives in `useFileUpload` (module-level shared state), and the
 * prompt is rendered once at the app root — there is no per-component owner
 * that could hold the resolver. Same pattern as `useDialog`: one prompt at a
 * time, and a second request resolves the first as cancelled instead of leaving
 * its awaiter hanging forever.
 */

export type UploadConflictChoice = 'overwrite' | 'rename' | 'cancel'

export interface UploadConflictItem {
  /** Name as the user sees it on their disk. */
  name: string
  /** Directory relative to the drop target ('' for a loose file). */
  relpath: string
  /** Destination path relative to the drop target, already normalized. */
  path: string
}

interface UploadConflictState {
  visible: boolean
  conflicts: UploadConflictItem[]
}

const state = ref<UploadConflictState>({ visible: false, conflicts: [] })

let pending: ((choice: UploadConflictChoice) => void) | null = null

/**
 * Ask how a batch of conflicting uploads should be handled.
 *
 * Resolves `'rename'` immediately when there is nothing to ask, so callers can
 * call this unconditionally.
 */
export function askUploadConflict(conflicts: UploadConflictItem[]): Promise<UploadConflictChoice> {
  if (conflicts.length === 0) return Promise.resolve('rename')

  // A second drop while the prompt is open: treat the first as cancelled. Its
  // upload is then abandoned, which is exactly what closing the dialog means.
  const previous = pending
  pending = null
  previous?.('cancel')

  state.value = { visible: true, conflicts }
  return new Promise<UploadConflictChoice>((resolve) => {
    pending = resolve
  })
}

/** Close the prompt and settle the pending request. Safe to call when closed. */
export function resolveUploadConflict(choice: UploadConflictChoice): void {
  const resolve = pending
  pending = null
  state.value = { visible: false, conflicts: [] }
  resolve?.(choice)
}

export function useUploadConflict() {
  return { state, askUploadConflict, resolveUploadConflict }
}

/** Test-only: drop any pending prompt without settling it. */
export function _resetUploadConflict(): void {
  pending = null
  state.value = { visible: false, conflicts: [] }
}
