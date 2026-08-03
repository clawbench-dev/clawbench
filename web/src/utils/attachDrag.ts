/**
 * Internal drag-and-drop payload for attaching files/directories to chat.
 *
 * HTML5 DnD only carries a restricted set of types across the app, so we use
 * a custom MIME type holding a JSON payload. The file manager writes it on
 * dragstart; the chat column reads it on drop and attaches via useChatContext.
 * This deliberately does NOT put the item in dataTransfer.files, so the
 * file manager's own OS-file upload drop handler ignores internal drags.
 */

export const ATTACH_DRAG_MIME = 'application/x-clawbench-attach'

export interface AttachDragData {
  path: string
  isDir: boolean
}

/** Write the internal attach payload into a drag event's dataTransfer. */
export function setAttachDragData(dt: DataTransfer, path: string, isDir: boolean) {
  try {
    dt.setData(ATTACH_DRAG_MIME, JSON.stringify({ path, isDir } satisfies AttachDragData))
    dt.setData('text/plain', path)
  } catch {
    // dataTransfer may be unavailable in some synthetic events — ignore
  }
}

/** Read the internal attach payload, or null if this is not an internal drag. */
export function readAttachDragData(dt: DataTransfer | null | undefined): AttachDragData | null {
  if (!dt) return null
  try {
    const raw = dt.getData(ATTACH_DRAG_MIME)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && typeof (parsed as AttachDragData).path === 'string') {
      const data = parsed as AttachDragData
      return { path: data.path, isDir: data.isDir === true }
    }
  } catch {
    // malformed payload — treat as non-internal
  }
  return null
}

/** Whether a drag event carries our internal attach payload (used to gate dragover/drop). */
export function hasAttachDragData(dt: DataTransfer | null | undefined): boolean {
  if (!dt) return false
  try {
    return !!dt.types?.includes?.(ATTACH_DRAG_MIME)
  } catch {
    return false
  }
}
