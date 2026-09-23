import { ref } from 'vue'
import { apiGet } from '@/utils/api'
import { useFileUpload } from '@/composables/useFileUpload'
import { hasAttachDragData } from '@/utils/attachDrag'

/**
 * Drag-and-drop of OS files onto the terminal panel.
 *
 * Dropped files are uploaded into the shell's CURRENT working directory, which
 * is fetched from `/api/terminal/status?session=<id>` — the backend resolves it
 * live from the PTY's foreground process group, so it follows `cd` (including
 * inside nested shells).
 *
 * ## Why the cwd is prefetched during dragover
 *
 * `handleFolderDropExpanded` → `expandDataTransfer` snapshots
 * `dataTransfer.items[].webkitGetAsEntry()` SYNCHRONOUSLY and only then awaits.
 * Once the drop handler yields to the event loop the DataTransfer is cleared, so
 * the upload call must be made synchronously from the drop handler — it cannot
 * be preceded by an `await fetch(...)` for the cwd. Hence the prefetch: by the
 * time the user releases the mouse we already know the directory.
 */

interface TerminalStatusResponse {
  cwd?: string
  hasSession?: boolean
  running?: boolean
  cwd_probe_supported?: boolean
}

export interface TerminalFileDropOptions {
  /** Session id of the active tab; empty when the session has not connected yet. */
  getSessionId: () => string | undefined
  /** Directory to fall back to when the live cwd is unavailable (the tab's launch dir). */
  getFallbackDir: () => string
}

/** Whether a drag event carries OS files (as opposed to internal attach drags). */
function isOSFileDrag(dt: DataTransfer | null | undefined): boolean {
  if (!dt) return false
  try {
    return !!dt.types?.includes?.('Files')
  } catch {
    return false
  }
}

export function useTerminalFileDrop(opts: TerminalFileDropOptions) {
  // The upload state (progress, pending files) lives in module-level singletons
  // inside useFileUpload, so the terminal and the file manager share it.
  const { handleFolderDropExpanded } = useFileUpload()

  const dropActive = ref(false)

  // Live cwd for the current drag. Reset per drag so a `cd` between two drops
  // is picked up rather than reusing the previous answer.
  let liveCwd = ''
  let cwdFetchInFlight = false
  let dragCounter = 0

  /**
   * Fetch the shell's live cwd for the active session.
   *
   * `dragover` fires every ~50-350ms, so an unguarded fetch here would flood the
   * endpoint. Only one request is kept in flight; later dragover events reuse
   * whatever landed (or the fallback).
   */
  function prefetchCwd() {
    if (cwdFetchInFlight) return
    const sessionId = opts.getSessionId()
    // Without a session id the endpoint takes the "all sessions" branch and
    // returns no cwd at all — skip the round-trip and use the fallback.
    if (!sessionId) return

    cwdFetchInFlight = true
    apiGet<TerminalStatusResponse>(`/api/terminal/status?session=${encodeURIComponent(sessionId)}`)
      .then((data) => {
        if (data?.cwd) liveCwd = data.cwd
      })
      .catch(() => {
        // Keep the fallback — an unreachable status endpoint must not block the
        // upload itself.
      })
      .finally(() => {
        cwdFetchInFlight = false
      })
  }

  /** Directory the upload should target, in priority order. */
  function resolveTargetDir(): string {
    if (liveCwd) return liveCwd
    // A tab created without an explicit cwd carries '' — which means "project
    // root", NOT "no directory". Passing '' to the upload endpoint would drop
    // the file into .clawbench/uploads/ instead, so map it to '.' (resolved
    // against the project root server-side).
    return opts.getFallbackDir() || '.'
  }

  function onDragEnter(e: DragEvent) {
    if (!isOSFileDrag(e.dataTransfer)) return
    // Internal file→chat drags also carry files in some browsers; they are not
    // uploads and must not show the overlay.
    if (hasAttachDragData(e.dataTransfer)) return
    if (dragCounter === 0) {
      liveCwd = ''
      prefetchCwd()
    }
    dragCounter++
    dropActive.value = true
  }

  function onDragOver(e: DragEvent) {
    if (!isOSFileDrag(e.dataTransfer)) return
    if (hasAttachDragData(e.dataTransfer)) return
    // Required for the drop event to fire at all.
    e.preventDefault()
    // Covers the case where dragover arrives before dragenter (or after the
    // first fetch failed).
    if (!liveCwd) prefetchCwd()
  }

  function onDragLeave() {
    dragCounter--
    if (dragCounter <= 0) {
      dragCounter = 0
      dropActive.value = false
    }
  }

  function onDrop(e: DragEvent) {
    dragCounter = 0
    dropActive.value = false
    if (!isOSFileDrag(e.dataTransfer)) return
    if (hasAttachDragData(e.dataTransfer)) return

    e.preventDefault()
    // Synchronous by design — see the module doc: the DataTransfer is only
    // valid until this handler returns to the event loop.
    void handleFolderDropExpanded(e, resolveTargetDir())
  }

  return {
    dropActive,
    onDragEnter,
    onDragOver,
    onDragLeave,
    onDrop,
  }
}
