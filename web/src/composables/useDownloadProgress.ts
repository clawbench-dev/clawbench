/**
 * Download progress state (module-level singleton).
 *
 * Downloads start from many places — the file manager context menu, the file
 * preview header, the media previews, the lightbox — while the progress bar is
 * mounted once (App.vue), so the state lives here rather than inside a
 * component. This mirrors the dir-upload singletons in `useFileUpload`.
 *
 * A download is identified by a numeric id so a late progress event from a
 * cancelled or superseded transfer cannot move the bar of the current one. The
 * native hosts (Android/Electron) echo the id back with every progress report
 * for exactly this reason.
 *
 * `downloadTotal` of 0 means "unknown size" — the server sent no Content-Length
 * (e.g. the streamed archive endpoint). The bar renders an indeterminate state
 * then, rather than sitting at a misleading 0%.
 */
import { ref } from 'vue'

const downloadVisible = ref(false)
const downloadFileName = ref('')
const downloadReceived = ref(0)
const downloadTotal = ref(0)

/** Monotonic id source; the first download gets 1 (0 means "none"). */
let downloadSeq = 0
let activeDownloadId = 0
/**
 * Cancellation for the in-flight transfer. Each transport registers its own
 * (XHR abort, native cancel) so the bar's button works on every host.
 */
let activeCancel: (() => void) | null = null

/** Allocate an id for a new download. */
export function nextDownloadId(): number {
  return ++downloadSeq
}

/** True when `id` is the download the UI is currently showing. */
export function isCurrentDownload(id: number): boolean {
  return downloadVisible.value && id === activeDownloadId
}

/**
 * Begin showing progress for a download. `total` of 0 means unknown size.
 * Returns false when another download is already running — the UI only has one
 * bar, so the caller falls back to a plain (progress-less) download.
 */
export function beginDownload(id: number, fileName: string, total: number): boolean {
  if (downloadVisible.value) return false
  activeDownloadId = id
  downloadFileName.value = fileName
  downloadReceived.value = 0
  downloadTotal.value = total > 0 ? total : 0
  downloadVisible.value = true
  return true
}

/** Record the total size once the response headers arrive. */
export function setDownloadTotal(id: number, total: number): void {
  if (!isCurrentDownload(id)) return
  downloadTotal.value = total > 0 ? total : 0
}

/** Report bytes received so far. Stale ids (cancelled/superseded) are ignored. */
export function reportDownloadProgress(id: number, received: number, total?: number): void {
  if (!isCurrentDownload(id)) return
  if (typeof total === 'number' && total > 0) downloadTotal.value = total
  downloadReceived.value = received
}

/** Register the cancellation callback for the in-flight transfer. */
export function setDownloadCancel(id: number, cancel: (() => void) | null): void {
  if (!isCurrentDownload(id)) return
  activeCancel = cancel
}

/** Hide the bar for `id`. No-op for a stale id. */
export function endDownload(id: number): void {
  if (id !== activeDownloadId) return
  downloadVisible.value = false
  downloadReceived.value = 0
  downloadTotal.value = 0
  downloadFileName.value = ''
  activeCancel = null
}

/** Cancel the in-flight download (user pressed the bar's cancel button). */
export function cancelDownload(): void {
  if (!downloadVisible.value) return
  const cancel = activeCancel
  activeCancel = null
  downloadVisible.value = false
  downloadReceived.value = 0
  downloadTotal.value = 0
  downloadFileName.value = ''
  try {
    cancel?.()
  } catch {
    // A failing transport cancel must not break the UI reset above.
  }
}

export function useDownloadProgress() {
  return {
    downloadVisible,
    downloadFileName,
    downloadReceived,
    downloadTotal,
    cancelDownload,
  }
}
