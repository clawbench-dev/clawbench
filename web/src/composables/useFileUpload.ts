import { ref } from 'vue'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { store } from '@/stores/app.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { folderRelPath } from '@/utils/fileAttachmentUtils'
import { expandDataTransfer, type ExpandResult } from '@/utils/dropFolder'
import { writeFileToTree } from '@/utils/dirHandle'
import { buildLocalFileUrl } from '@/utils/download'
import { appLog, diagLog } from '@/utils/appLog'
import { createTransferSpeedMeter } from '@/utils/transferSpeed'
import { askUploadConflict, type UploadConflictChoice, type UploadConflictItem } from '@/composables/useUploadConflict'

// ── Module-level singleton state ──
// pendingFiles MUST be shared across all callers (AttachDrawer, ChatPanelContent,
// FileManagerContent) so that uploads initiated in the drawer are visible to
// sendMessage in ChatPanelContent. Same pattern as useChatContext.

export interface PendingFile {
  path: string
  previewUrl: string | null
  isImage: boolean
  uploading: boolean
  progress: number
  size: number
  xhr?: XMLHttpRequest
  cancelled?: boolean
}

const pendingFiles = ref<PendingFile[]>([])
let uploadGeneration = 0

// ── Chat upload batch cancellation (AttachDrawer) ──
// The chat picker uploads sequentially and shows one terminate button for the
// whole batch. Directory uploads keep their own flag (dirUploadCancelled) and
// progress UI, so the two never cancel each other.
// A ref (not a plain let) so the drawer can tell, after `await handleFileSelect`,
// whether the batch it was waiting on was terminated.
const chatUploadCancelled = ref(false)
/**
 * Identity of the current chat upload batch.
 *
 * `chatUploadCancelled` alone cannot answer "was *my* batch cancelled": every
 * new batch resets that ref to false, and a retry keeps the previous batch's
 * promise alive for up to the backoff delay. Without a per-batch token, a file
 * still waiting to retry when the user cancels and starts a new batch would be
 * resurrected by that reset and keep uploading files the user already
 * terminated. The object is replaced per batch, so an older upload keeps
 * pointing at the cancelled one.
 */
let chatBatch: { cancelled: boolean } = { cancelled: false }
// Paths that finished uploading in the CURRENT chat batch. Terminating the
// batch removes exactly these from the attachment list ("clear everything"),
// leaving attachments that predate the batch untouched.
let chatBatchPaths: string[] = []
// The XHR of the file currently in flight in the chat batch, so terminating
// the batch aborts the transfer that is actually running (not just the ones
// queued behind it).
let activeChatXhr: XMLHttpRequest | null = null

// Error messages collected during a directory upload (file manager). Individual
// per-file failures are suppressed while uploading and aggregated into a single
// summary toast at the end, otherwise the per-file error toast is immediately
// overwritten by the completion toast (useToast is a singleton) and the user
// sees a false "success".
let dirUploadErrorMsgs: string[] = []

// Upload progress for directory uploads (file manager)
const dirUploading = ref(false)
const dirUploadProgress = ref(0)
const dirUploadTotal = ref(0)
const dirUploadDone = ref(0)
const dirUploadCancelled = ref(false)
/** Per-batch identity for directory uploads — same reason as `chatBatch`. */
let dirBatch: { cancelled: boolean } = { cancelled: false }
/**
 * Bytes per second across the whole batch. Same meter the download bar uses,
 * fed by the cumulative byte count `reportDirProgress` already receives.
 */
const dirUploadSpeedMeter = createTransferSpeedMeter()
const dirUploadSpeed = dirUploadSpeedMeter.speed
let activeDirXhr: XMLHttpRequest | null = null
let activeDownloadAbort: AbortController | null = null

// Cumulative byte progress for the current batch (dir upload or tree download).
//
// The bar must advance ACROSS files, so progress is reported against the whole
// batch. Reporting each file's own 0-100 (as it did before) made the bar restart
// at every file boundary — a two-file upload produced [50,100,50,100], which
// reads as "stuck / jumping backwards".
let dirUploadTotalBytes = 0
let dirUploadLoadedBytes = 0

/**
 * Sum the batch's bytes and reset the bar. Call once per batch, before any file.
 *
 * Oversized files are excluded: they are rejected before a single byte is sent,
 * so counting them would leave the bar permanently short of 100%.
 */
function beginDirProgress(files: Iterable<{ size: number }>, maxSizeBytes: number): void {
  let total = 0
  for (const f of files) {
    const size = f.size || 0
    if (maxSizeBytes > 0 && size > maxSizeBytes) continue
    total += size
  }
  dirUploadTotalBytes = total
  dirUploadLoadedBytes = 0
  dirUploadProgress.value = 0
  dirUploadSpeedMeter.reset()
}

/**
 * Advance the bar to `loaded` cumulative bytes.
 *
 * Monotonic by construction: a later file's early progress events must never
 * undo bytes already banked for earlier files. A batch whose bytes are all
 * unknown (every file empty) leaves the bar at 0 rather than dividing by zero.
 */
function reportDirProgress(loaded: number): void {
  if (dirUploadTotalBytes <= 0) return
  // Sample before the percentage guard: a file large enough to hold the
  // rounded percentage still moves real bytes, and the rate must see them.
  dirUploadSpeedMeter.sample(loaded)
  const pct = Math.min(100, Math.round((loaded / dirUploadTotalBytes) * 100))
  if (pct > dirUploadProgress.value) dirUploadProgress.value = pct
}

/**
 * Bank a finished file's bytes so the next file's progress continues from here.
 *
 * The full declared size is banked even when the file failed: a partially-sent
 * file would otherwise leave a permanent gap and the bar could never reach 100%.
 * Oversized files are skipped — they were excluded from the total, so banking
 * them would overshoot. (They also never reach here: they are rejected before
 * the request is built.)
 */
function bankDirProgress(size: number, maxSizeBytes: number): void {
  if (maxSizeBytes > 0 && size > maxSizeBytes) return
  dirUploadLoadedBytes += size || 0
  reportDirProgress(dirUploadLoadedBytes)
}

/**
 * Ask the server which of these files would land on an existing path.
 *
 * The upload endpoint disambiguates silently by default, so the only way to
 * offer the user a choice is to ask first. A preflight that cannot answer
 * (network hiccup, older server without the endpoint) must never block the
 * upload — the batch then proceeds with the default rename behaviour.
 */
async function findUploadConflicts(
  items: { name: string; relpath?: string }[],
  dir: string,
): Promise<UploadConflictItem[]> {
  if (items.length === 0) return []
  try {
    const resp = await fetch('/api/upload/check', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        dir,
        files: items.map(i => ({ name: i.name, relpath: i.relpath || '' })),
      }),
    })
    if (!resp.ok) return []
    const data = await resp.json()
    return Array.isArray(data?.conflicts) ? data.conflicts as UploadConflictItem[] : []
  } catch {
    return []
  }
}

/**
 * Resolve how a batch should handle name collisions.
 *
 * Returns the `onconflict` policy for the upload, or `'cancel'` when the user
 * dismissed the prompt (the whole batch is then abandoned — a partially
 * uploaded batch would be the worst of both worlds).
 */
async function decideUploadConflicts(
  items: { name: string; relpath?: string }[],
  dir: string,
): Promise<UploadConflictChoice> {
  if (!dir) return 'rename'
  const conflicts = await findUploadConflicts(items, dir)
  if (conflicts.length === 0) return 'rename'
  return askUploadConflict(conflicts)
}

/** Logger tag for the upload diagnostics relayed to /api/client-log. */
const TAG = 'FileUpload'

/**
 * Attempts per file (1 initial + 2 retries).
 *
 * Uploads used to be single-shot: one dropped response at 100% threw the whole
 * transfer away and the user had to drag the file in again ("多试几次就成功").
 * The File object is still in memory, so resending costs nothing but time.
 *
 * Trade-off: if the first attempt did reach the server and only its response
 * was lost, the resend lands as a second copy (`name_1.ext`) — the server
 * renames instead of overwriting. That is the same outcome the user gets by
 * retrying by hand, and it is the lesser evil compared to losing the upload.
 */
const UPLOAD_MAX_ATTEMPTS = 3
/** Backoff before attempt 2 and 3. */
const UPLOAD_RETRY_DELAYS_MS = [500, 1500]

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/** Outcome of one POST, so the retry loop (not the XHR handlers) decides. */
type UploadAttempt =
  | { kind: 'ok'; path: string }
  | { kind: 'cancelled' }
  /** The server answered with a business error — resending cannot help. */
  | { kind: 'fatal'; message: string }
  /** Transport / response-stage failure worth another attempt. */
  | { kind: 'retry'; message: string; reason: string; detail?: Record<string, unknown> }

export function useFileUpload() {
  const toast = useToast()

  // attachedFiles is managed globally via useChatContext so any tab
  // (file preview, chat input, quote-question) can read/write it.
  const { attachedFiles, addAttachedFile, removeAttachedFile, removeAttachedFileByPath, snapshotAttachments, clearAll } = useChatContext()

  /**
   * Upload one file. Resolves with the server-assigned path on success, or
   * null on any failure / cancellation — callers that need to auto-select the
   * freshly uploaded file read the path from here rather than scanning the
   * shared `pendingFiles` list (which cannot distinguish two same-sized files).
   */
  function uploadOneFile(
    file: File,
    dir: string | undefined,
    autoAttach: boolean | undefined,
    relPath: string | undefined,
    onConflict: UploadConflictChoice | undefined,
    // The caller's own batch token. Passed in rather than read from the module
    // here: with two batches in flight the module-level one may already belong
    // to the newer batch, which would make this upload ignore its own
    // cancellation and obey a stranger's.
    batch: { cancelled: boolean },
  ): Promise<string | null> {
    // Hoisted out of the request builder so the banking step below shares the same cap.
    const maxSizeBytes = store.state.uploadMaxSizeMB * 1024 * 1024
    const isDirUpload = !!dir
    // The most recent request, so the settle path can tell whether this file
    // still owns the chat batch's in-flight slot.
    let lastXhr: XMLHttpRequest | null = null

    // Route a failure reason either to an immediate toast (chat attachment)
    // or into the dir-upload collector for a single summary toast at the end.
    // Dir mode must NOT toast per-file, otherwise the completion toast
    // (singleton) overwrites the error and the user sees no failure at all.
    // The stored reason stays bare (no "upload failed" prefix); finishDirUpload
    // wraps it in the aggregate message exactly once.
    function notifyError(msg: string) {
      // A late failure from a superseded batch must not end up in the new
      // batch's summary toast.
      if (isDirUpload) { if (batch === dirBatch) dirUploadErrorMsgs.push(msg) }
      else toast.show(msg, { icon: 'warning', type: 'error' })
    }
    // Chat toasts keep the "上传失败:" prefix; dir mode stores the bare reason.
    function failError(reason: string) {
      if (isDirUpload) { if (batch === dirBatch) dirUploadErrorMsgs.push(reason) }
      else toast.show(gt('upload.failed', { error: reason }), { icon: 'warning', type: 'error' })
    }

    // Pre-flight size check: prevent sending a request that will be
    // rejected by the server's MaxBytesReader (which causes onerror
    // instead of a readable error response).
    if (file.size > maxSizeBytes) {
      notifyError(gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }))
      return Promise.resolve(null)
    }

    const isImage = file.type.startsWith('image/')
    const previewUrl = isImage ? URL.createObjectURL(file) : null

    // Push entry then get reactive proxy from array (only for chat upload, not dir upload)
    let entry: PendingFile | null = null
    if (!isDirUpload) {
      const idx = pendingFiles.value.length
      pendingFiles.value.push({
        path: '',
        previewUrl,
        isImage,
        uploading: true,
        progress: 0,
        size: file.size,
      })
      entry = pendingFiles.value[idx]
    }

    /** Drop the pending card (chat upload only) and release its preview URL. */
    function dropEntry() {
      if (!entry) return
      if (previewUrl) URL.revokeObjectURL(previewUrl)
      const i = pendingFiles.value.indexOf(entry)
      if (i !== -1) pendingFiles.value.splice(i, 1)
    }

    /** True once the user removed this file or terminated the whole batch. */
    function isCancelled() {
      return !!entry?.cancelled || batch.cancelled
    }

    /**
     * Send the file once and classify the outcome.
     *
     * A fresh FormData (and XHR) per attempt: the same `file` is re-read from
     * disk by the browser, so a retry costs no extra memory beyond the body.
     */
    function sendOnce(): Promise<UploadAttempt> {
      return new Promise((resolve) => {
        const xhr = new XMLHttpRequest()

        const formData = new FormData()
        formData.append('file', file)
        if (dir) formData.append('dir', dir)
        if (relPath) formData.append('relpath', relPath)
        // Only the explicit overwrite choice is sent: the server's default is
        // rename, which is what every other caller wants.
        if (dir && onConflict === 'overwrite') formData.append('onconflict', 'overwrite')

        lastXhr = xhr
        if (entry) entry.xhr = xhr
        if (isDirUpload) activeDirXhr = xhr
        else activeChatXhr = xhr
        xhr.open('POST', '/api/upload/file')
        xhr.timeout = 300000

        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            if (entry) entry.progress = Math.round((e.loaded / e.total) * 100)
            // Batch progress = bytes banked for finished files + this file's
            // sent bytes, so the bar keeps climbing across file boundaries
            // instead of restarting from 0 on each one.
            if (isDirUpload) reportDirProgress(dirUploadLoadedBytes + e.loaded)
          }
        }

        xhr.onload = () => {
          if (isCancelled()) {
            resolve({ kind: 'cancelled' })
            return
          }
          let data: { ok?: boolean; path?: string; error?: string }
          try {
            data = JSON.parse(xhr.responseText)
          } catch {
            // The body was fully sent (progress reached 100%) but the answer is
            // not the JSON envelope: a proxy error page, a truncated body, or a
            // connection dropped while the response was in flight. This is the
            // reported "100% 后解析失败" — the one case a retry actually fixes.
            resolve({
              kind: 'retry',
              message: gt('upload.parseError'),
              reason: 'parse-error',
              detail: {
                status: xhr.status,
                readyState: xhr.readyState,
                body: (xhr.responseText || '').slice(0, 200),
              },
            })
            return
          }
          if (data?.ok) {
            const path = data.path || ''
            if (entry) {
              entry.uploading = false
              entry.progress = 100
              entry.path = path
              if (autoAttach) addAttachedFile(path)
            }
            resolve({ kind: 'ok', path })
          } else {
            // A structured business error (too large, forbidden, no file…):
            // resending the same bytes can never change the answer. Our own
            // 5xx errors also arrive in this envelope, so they are definitive
            // too — only a *non-JSON* 5xx (a proxy page) is retried.
            resolve({ kind: 'fatal', message: data?.error || gt('upload.unknownError') })
          }
        }

        xhr.onerror = () => {
          if (isCancelled()) {
            resolve({ kind: 'cancelled' })
            return
          }
          // When the server's MaxBytesReader rejects the upload, the XHR
          // gets onerror instead of onload with a parseable response.
          // If the file exceeds the threshold, retrying cannot help.
          if (file.size > maxSizeBytes) {
            resolve({
              kind: 'fatal',
              message: gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }),
            })
            return
          }
          resolve({
            kind: 'retry',
            message: gt('upload.networkError'),
            reason: 'network-error',
            detail: { status: xhr.status, readyState: xhr.readyState },
          })
        }

        xhr.ontimeout = () => {
          if (isCancelled()) {
            resolve({ kind: 'cancelled' })
            return
          }
          resolve({
            kind: 'retry',
            message: gt('upload.timeout'),
            reason: 'timeout',
            detail: { status: xhr.status, readyState: xhr.readyState },
          })
        }

        // Removing a pending card aborts the request without showing a network error.
        xhr.onabort = () => resolve({ kind: 'cancelled' })

        xhr.send(formData)
      })
    }

    /** Drive the attempts and surface the outcome exactly once. */
    async function run(): Promise<string | null> {
      for (let attempt = 1; attempt <= UPLOAD_MAX_ATTEMPTS; attempt++) {
        const result = await sendOnce()
        if (result.kind === 'ok') return result.path
        if (result.kind === 'cancelled') return null
        if (result.kind === 'fatal') {
          dropEntry()
          failError(result.message)
          return null
        }
        // Transport / response-stage failure: log what is observable and try
        // again before bothering the user. The response stage is exactly where
        // an intermediary can fail without leaving any usable error behind, so
        // status/readyState/body prefix are all the diagnosis available.
        appLog.w(TAG, 'upload attempt failed', {
          file: file.name, attempt, reason: result.reason, ...(result.detail || {}),
        })
        if (attempt === UPLOAD_MAX_ATTEMPTS) {
          dropEntry()
          // Terminal line: it must reach the server even with 调试日志捕获 off
          // (its default), otherwise this bug stays undiagnosable in the field.
          // Bounded to one line per failed file.
          diagLog(TAG, `upload failed after ${attempt} attempts ${JSON.stringify({
            file: file.name, reason: result.reason, ...(result.detail || {}),
          })}`)
          notifyError(result.message)
          return null
        }
        // Restart this file's own bar: the bytes are being sent again.
        if (entry) entry.progress = 0
        await sleep(UPLOAD_RETRY_DELAYS_MS[Math.min(attempt, UPLOAD_RETRY_DELAYS_MS.length) - 1])
        if (isCancelled()) return null
      }
      return null
    }

    // Bank this file's bytes on every settle path (success, failure, abort) so
    // the batch bar continues from here instead of restarting. Doing it here
    // rather than in each caller means a caller cannot forget and stall the bar.
    return run().then((path) => {
      // Banking a superseded batch's bytes would move the new batch's bar.
      if (dir && batch === dirBatch) bankDirProgress(file.size, maxSizeBytes)
      // The in-flight slot is free once this file settles (success or not).
      if (!dir && activeChatXhr === lastXhr) activeChatXhr = null
      return path
    })
  }

  /**
   * Aggregate the dir-upload result into a single summary toast.
   *
   * `batch` is the caller's token: a batch that was cancelled and then
   * superseded by a new one finishes LATE (its retry backoff outlives the
   * switch), and tearing the shared progress bar down here would hide the new
   * batch's bar and drop its cancel handle.
   */
  function finishDirUpload(batch: { cancelled: boolean }, okCount: number, failCount: number) {
    if (batch !== dirBatch) return
    dirUploading.value = false
    dirUploadProgress.value = 0
    dirUploadSpeedMeter.reset()
    activeDirXhr = null
    const cancelled = dirUploadCancelled.value
    const errs = dirUploadErrorMsgs
    dirUploadCancelled.value = false
    dirUploadErrorMsgs = []
    if (cancelled) {
      toast.show(gt('upload.cancelled'), { icon: 'stop', type: 'info' })
      return
    }
    if (failCount === 0) {
      if (okCount > 0) {
        toast.show(gt('upload.completed', { count: okCount }), { icon: 'success', type: 'success' })
      }
      return
    }
    const firstErr = errs[0] || gt('upload.unknownError')
    if (okCount > 0) {
      toast.show(gt('upload.partial', { ok: okCount, failed: failCount, error: firstErr }), { icon: 'warning', type: 'error' })
    } else {
      toast.show(gt('upload.failed', { error: firstErr }), { icon: 'warning', type: 'error' })
    }
  }

  /**
   * Upload a batch of files picked from a native picker.
   *
   * `onFileUploaded` fires the instant ONE chat file lands, before the rest of
   * the batch is even sent. Callers that mirror the result into another list
   * (the attachment drawer → the composer) must use it instead of the batch's
   * return value: the in-flight card unmounts the moment `uploading` flips
   * false, so a caller that waits for the whole batch leaves a window with
   * neither a pending card nor a committed one — the reported "缩略图一闪即逝"
   * (issue #629). With several files that window is a whole file transfer long.
   */
  async function uploadFiles(
    files: File[],
    dir?: string,
    preserveStructure = false,
    onFileUploaded?: (path: string) => void,
  ): Promise<string[]> {
    const maxFiles = store.state.uploadMaxFiles
    const currentCount = pendingFiles.value.filter(f => !f.uploading).length
    const remaining = maxFiles - currentCount
    if (remaining <= 0) {
      toast.show(gt('upload.maxFiles', { max: maxFiles }), { icon: 'warning', type: 'error' })
      return []
    }

    const toUpload = files.slice(0, remaining)
    if (files.length > remaining) {
      toast.show(gt('upload.tooManyFiles', { total: files.length, remaining }), { icon: 'warning', type: 'error' })
    }

    const maxSizeBytes = store.state.uploadMaxSizeMB * 1024 * 1024

    // Dir upload progress tracking
    const isDirUpload = !!dir
    // Ask about name collisions before a single byte is sent. Doing it here
    // (rather than letting the server rename) is the whole point of the
    // conflict dialog: the user is copying into a real directory, so a silent
    // `_1` copy is usually the wrong outcome.
    let onConflict: UploadConflictChoice = 'rename'
    if (dir) {
      onConflict = await decideUploadConflicts(
        toUpload.map(f => ({
          name: f.name,
          relpath: preserveStructure ? folderRelPath(f) : '',
        })),
        dir,
      )
      if (onConflict === 'cancel') return []
    }
    if (isDirUpload) {
      dirUploading.value = true
      // A fresh batch gets a fresh token: the previous batch's in-flight retry
      // must stay cancelled even though this flag is reset (see `chatBatch`).
      dirBatch = { cancelled: false }
      dirUploadCancelled.value = false
      activeDirXhr = null
      dirUploadTotal.value = toUpload.length
      dirUploadDone.value = 0
      // Size the bar against the whole batch, not the current file.
      beginDirProgress(toUpload, maxSizeBytes)
      dirUploadErrorMsgs = []
    } else {
      // A new chat batch SUPERSEDES the one still in flight. Two picks can
      // overlap — the drawer stays open while the first one uploads — and the
      // older loop holds its own token, so simply replacing the module-level
      // one would leave it uploading files the user has already replaced, with
      // no handle left for terminate/cancel (they only know the current token).
      // `cancelChatUpload` is exactly "stop that batch, abort its transfer,
      // drop its rows"; the flag it raises is reset right below for the new one.
      cancelChatUpload()
      // Fresh chat batch: clear the terminate flag and forget the previous
      // batch's paths, so a new upload never inherits an old cancellation.
      chatBatch = { cancelled: false }
      chatUploadCancelled.value = false
      chatBatchPaths = []
      activeChatXhr = null
    }

    // This batch's own token, captured before the loop: a later batch replaces
    // the module-level one, and the loop must still see *its* cancellation.
    const batch = isDirUpload ? dirBatch : chatBatch

    let okCount = 0
    let failCount = 0
    // Paths of files that finished uploading, in upload order. The chat picker
    // uses these to auto-select what the user just uploaded.
    const uploadedPaths: string[] = []

    for (const file of toUpload) {
      if (isDirUpload && batch.cancelled) break
      if (file.size > maxSizeBytes) {
        const msg = gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB })
        if (isDirUpload) {
          dirUploadErrorMsgs.push(msg)
          failCount++
          dirUploadDone.value++
        } else {
          toast.show(msg, { icon: 'warning', type: 'error' })
        }
        continue
      }
      // When preserving folder structure, derive each file's relative sub-path
      // (including the top-level folder) from webkitRelativePath.
      const relPath = preserveStructure ? folderRelPath(file) || undefined : undefined
      const uploadedPath = await uploadOneFile(file, dir, false, relPath, onConflict, batch)
      if (batch.cancelled) break
      if (isDirUpload) {
        if (uploadedPath) okCount++
        else failCount++
        dirUploadDone.value++
      } else if (uploadedPath) {
        uploadedPaths.push(uploadedPath)
        chatBatchPaths.push(uploadedPath)
        // Per-file, not per-batch: the pending card for THIS file is already
        // gone by the time the next `await` yields to paint (see the doc above).
        onFileUploaded?.(uploadedPath)
      }
    }

    if (isDirUpload) {
      finishDirUpload(batch, okCount, failCount)
    }
    return uploadedPaths
  }

  /**
   * How many files the user actually picked in this change event.
   *
   * MUST be called BEFORE `handleFileSelect`/`handleFileSelectToDir` get the
   * same event: those clear `input.value`, and clearing a file input also
   * empties its `FileList` — so a later read always reports 0.
   *
   * Callers used to inline this read and rely on that ordering, with a comment
   * as the only record of the contract. The moment the clearing moved (or an
   * `await` was inserted before it) the caller would silently see 0 and, for
   * the attachment drawer, never close after a real pick. The contract now
   * lives with the code that performs the clearing, and the drawer's ordering
   * is pinned by a behavioural test (see AttachDrawer.test.ts).
   */
  function readPickedCount(e: Event): number {
    return (e.target as HTMLInputElement)?.files?.length ?? 0
  }

  async function handleFileSelect(
    e: Event,
    onFileUploaded?: (path: string) => void,
  ): Promise<string[]> {
    const files = Array.from((e.target as HTMLInputElement).files || [])
    // Reset input immediately to prevent Android WebView from re-firing
    // the change event with stale file data on picker cancellation
    ;(e.target as HTMLInputElement).value = ''
    if (files.length === 0) return []
    return uploadFiles(files, undefined, false, onFileUploaded)
  }

  async function handleFileDrop(files: File[]): Promise<string[]> {
    if (files.length === 0) return []
    return uploadFiles(files)
  }

  /** Upload files and auto-attach each one after it succeeds (for drag-drop / clipboard paste). */
  async function uploadAndAttach(files: File[]) {
    if (files.length === 0) return
    const maxFiles = store.state.uploadMaxFiles
    const currentCount = pendingFiles.value.filter(f => !f.uploading).length
    const remaining = maxFiles - currentCount
    if (remaining <= 0) {
      toast.show(gt('upload.maxFiles', { max: maxFiles }), { icon: 'warning', type: 'error' })
      return
    }
    const toUpload = files.slice(0, remaining)
    if (files.length > remaining) {
      toast.show(gt('upload.tooManyFiles', { total: files.length, remaining }), { icon: 'warning', type: 'error' })
    }
    const maxSizeBytes = store.state.uploadMaxSizeMB * 1024 * 1024
    // A drag/paste batch is the same "current chat batch" as the picker's: the
    // terminate button lives in the shared Uploads tab and must stop whichever
    // one is showing, so both reset the same flag/paths here.
    chatBatch = { cancelled: false }
    chatUploadCancelled.value = false
    chatBatchPaths = []
    activeChatXhr = null
    const generation = uploadGeneration
    // Same reason as in `uploadFiles`: hold this batch's own token.
    const batch = chatBatch
    for (const file of toUpload) {
      if (generation !== uploadGeneration || batch.cancelled) break
      if (file.size > maxSizeBytes) {
        toast.show(gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }), { icon: 'warning', type: 'error' })
        continue
      }
      const uploadedPath = await uploadOneFile(file, undefined, true, undefined, undefined, batch)
      if (batch.cancelled) break
      if (uploadedPath) chatBatchPaths.push(uploadedPath)
    }
  }

  async function handleFileSelectToDir(e: Event, dir: string) {
    // Same read-before-clear ordering as handleFileSelect; the count is not
    // returned here because no directory-upload caller needs it (the progress
    // bar derives its total from the batch itself).
    const files = Array.from((e.target as HTMLInputElement).files || [])
    ;(e.target as HTMLInputElement).value = ''
    if (files.length === 0) return
    await uploadFiles(files, dir)
  }

  async function handleFileDropToDir(files: File[], dir: string) {
    if (files.length === 0) return
    await uploadFiles(files, dir)
  }

  /** Upload a directory to a target dir, preserving nested folder structure. */
  async function handleFolderSelect(e: Event, dir: string) {
    const files = Array.from((e.target as HTMLInputElement).files || [])
    ;(e.target as HTMLInputElement).value = ''
    if (files.length === 0) return
    await uploadFiles(files, dir, true)
  }

  /** Drop files into a dir, preserving structure when any file is from a folder. */
  async function handleFileDropToDirStructured(files: File[], dir: string) {
    if (files.length === 0) return
    const isFolder = files.some(f => folderRelPath(f) !== '')
    await uploadFiles(files, dir, isFolder)
  }

  /** Create a directory under dir (used for empty folders found in a drop). */
  async function createDir(dir: string, name: string): Promise<boolean> {
    try {
      const resp = await fetch('/api/dir/create', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ path: dir || '.', name }),
      })
      return resp.ok
    } catch {
      return false
    }
  }

  /**
   * Upload an expanded folder drop (files each carrying their relPath) and
   * recreate any empty directories. Progress bar stays byte-based per file
   * (from uploadOneFile); dirUploadDone/Track the overall count.
   */
  async function uploadExpandedFolder(result: ExpandResult, dir: string) {
    const maxSizeBytes = store.state.uploadMaxSizeMB * 1024 * 1024
    // Name collisions are decided before the progress bar appears: cancelling
    // the prompt must leave no trace of an upload that never started.
    const onConflict = await decideUploadConflicts(
      result.files.map(f => ({ name: f.file.name, relpath: f.relPath })),
      dir,
    )
    if (onConflict === 'cancel') return
    dirUploading.value = true
    dirBatch = { cancelled: false }
    const batch = dirBatch
    dirUploadCancelled.value = false
    activeDirXhr = null
    dirUploadErrorMsgs = []
    const total = result.files.length + result.emptyDirs.length
    dirUploadTotal.value = total
    dirUploadDone.value = 0
    // Size the bar against the whole batch, not the current file. Empty dirs
    // carry no bytes, so they do not affect the byte total.
    beginDirProgress(result.files.map((f) => f.file), maxSizeBytes)
    let done = 0
    let okCount = 0
    let failCount = 0

    for (const { file, relPath } of result.files) {
      if (batch.cancelled) break
      if (file.size > maxSizeBytes) {
        dirUploadErrorMsgs.push(gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }))
        failCount++
      } else {
        const uploadedPath = await uploadOneFile(file, dir, false, relPath || undefined, onConflict, batch)
        if (batch.cancelled) break
        if (uploadedPath) okCount++
        else failCount++
      }
      done++
      dirUploadDone.value = done
    }

    for (const emptyDir of result.emptyDirs) {
      if (batch.cancelled) break
      const ok = await createDir(dir, emptyDir)
      if (ok) {
        done++
        okCount++
        dirUploadDone.value = done
      }
    }

    finishDirUpload(batch, okCount, failCount)
  }

  /** Abort an in-progress directory upload or tree download. */
  function cancelDirUpload() {
    if (!dirUploading.value) return
    dirBatch.cancelled = true
    dirUploadCancelled.value = true
    activeDirXhr?.abort()
    activeDirXhr = null
    activeDownloadAbort?.abort()
    activeDownloadAbort = null
    dirUploading.value = false
    dirUploadProgress.value = 0
  }

  /**
   * Terminate the whole in-flight chat upload batch (the AttachDrawer button).
   *
   * Sets the flag the sequential loop checks between files, aborts the transfer
   * currently in flight, and returns the paths that had already succeeded so
   * the caller can clear them from the attachment list ("终止 = 全部清空").
   *
   * The failed/aborted in-flight entry is removed from `pendingFiles` here
   * rather than waiting for the XHR's own error path: `abort()` fires
   * `onabort`, which only resolves null and never touches `pendingFiles`, so a
   * row would otherwise linger at its last percentage.
   */
  function cancelChatUpload(): string[] {
    chatBatch.cancelled = true
    chatUploadCancelled.value = true
    activeChatXhr?.abort()
    activeChatXhr = null
    const done = chatBatchPaths.slice()
    chatBatchPaths = []
    // Drop every in-flight row; the loop has stopped, so nothing will clean them.
    for (const f of pendingFiles.value) {
      if (f.uploading) {
        f.cancelled = true
        if (f.previewUrl) URL.revokeObjectURL(f.previewUrl)
      }
    }
    pendingFiles.value = pendingFiles.value.filter(f => !f.uploading)
    return done
  }

  /**
   * Download a directory (or file) as a reconstructed tree on the local disk
   * using the File System Access API. Picks a target directory via
   * showDirectoryPicker(), then fetches each file and writes it back under the
   * same relative path. Reuses the dir-upload progress bar and cancel.
   */
  async function downloadDirAsTree(path: string) {
    const picker = (window as unknown as { showDirectoryPicker?: () => Promise<FileSystemDirectoryHandle> }).showDirectoryPicker
    if (typeof picker !== 'function') {
      toast.show(gt('upload.dirDownloadUnsupported'), { icon: 'warning', type: 'error' })
      return
    }

    dirUploading.value = true
    // The tree download uses the same bar as directory uploads; claiming the
    // token makes a late, superseded upload batch leave it alone.
    dirBatch = { cancelled: false }
    dirUploadCancelled.value = false
    activeDownloadAbort = null
    dirUploadTotal.value = 0
    dirUploadDone.value = 0
    dirUploadProgress.value = 0

    let files: { rel: string; size: number }[] | undefined
    try {
      const resp = await fetch('/api/file/list-tree?path=' + encodeURIComponent(path))
      if (!resp.ok) throw new Error('list-tree failed')
      files = (await resp.json()).files
    } catch {
      dirUploading.value = false
      dirUploadProgress.value = 0
      toast.show(gt('upload.dirDownloadFailed'), { icon: 'warning', type: 'error' })
      return
    }
    const tree = files ?? []
    dirUploadTotal.value = tree.length
    // Size the bar against every file in the tree, not the current one.
    beginDirProgress(tree, 0)

    let rootHandle: FileSystemDirectoryHandle
    try {
      rootHandle = await picker()
    } catch {
      // User cancelled the directory picker — abort quietly.
      dirUploading.value = false
      dirUploadProgress.value = 0
      return
    }

    const abort = new AbortController()
    activeDownloadAbort = abort
    const base = path.replace(/\/+$/, '')
    let done = 0

    for (const f of tree) {
      if (dirUploadCancelled.value || abort.signal.aborted) break
      const fullRel = base ? `${base}/${f.rel}` : f.rel
      try {
        const resp = await fetch(buildLocalFileUrl(fullRel, { download: true }), { signal: abort.signal })
        if (!resp.ok) continue

        const reader = resp.body!.getReader()
        const chunks: BlobPart[] = []
        let received = 0
        for (;;) {
          const { done: rd, value } = await reader.read()
          if (rd) break
          chunks.push(value as unknown as BlobPart)
          received += value.length
          // Batch progress: bytes banked for earlier files + this file's so
          // far, so the bar keeps climbing across file boundaries.
          reportDirProgress(dirUploadLoadedBytes + received)
        }
        const blob = new Blob(chunks)
        await writeFileToTree(rootHandle, f.rel, blob)
        done++
        dirUploadDone.value = done
      } catch {
        if (abort.signal.aborted) break
      } finally {
        // Bank on every path (success, skip, failure) so the bar keeps moving
        // and a single bad file cannot stall it for the rest of the tree.
        bankDirProgress(f.size, 0)
      }
    }

    activeDownloadAbort = null
    dirUploading.value = false
    dirUploadProgress.value = 0
    if (dirUploadCancelled.value) {
      toast.show(gt('upload.cancelled'), { icon: 'stop', type: 'info' })
    } else if (done > 0) {
      toast.show(gt('upload.downloaded', { count: done }), { icon: 'success', type: 'success' })
    }
    dirUploadCancelled.value = false
  }

  /** Expand a folder drop (webkitGetAsEntry) then upload files + empty dirs. */
  async function handleFolderDropExpanded(e: DragEvent, dir: string) {
    if (!e.dataTransfer) return
    const result = await expandDataTransfer(e.dataTransfer)
    if (result.files.length === 0 && result.emptyDirs.length === 0) return
    await uploadExpandedFolder(result, dir)
  }

  /**
   * Terminate the whole in-flight chat upload batch and detach whatever it had
   * already finished ("终止 = 全部清空").
   *
   * THE single implementation of that action, shared by both entry points:
   *   - the drawer's batch banner (only reachable while the drawer is open), and
   *   - the attachment strip on the input bar, which is where the user actually
   *     is during an upload — the drawer closes as soon as files are picked.
   *
   * Keeping it in one place is what stops the two from drifting on the
   * semantics that are easy to get subtly different: the queued files stop, the
   * transfer in flight is aborted, AND the files that already completed are
   * detached again, so the input returns to its pre-upload state.
   *
   * Returns the paths that were detached (also useful for tests/telemetry).
   */
  function terminateChatUploadBatch(): string[] {
    const finished = cancelChatUpload()
    const detached: string[] = []
    for (const path of finished) {
      // Only entries that are actually attached need removing; the upload may
      // have completed without being auto-attached. Uses the same path-based
      // removal the attachment cards use, so a ranged reference to the same
      // file is not silently dropped along with the whole-file entry.
      const before = attachedFiles.value.length
      removeAttachedFileByPath(path)
      if (attachedFiles.value.length < before) detached.push(path)
    }
    return detached
  }

  function removeFile(index: number) {
    const f = pendingFiles.value[index]
    if (f) cancelPendingFile(f)
    pendingFiles.value.splice(index, 1)
  }

  function cancelPendingFile(file: PendingFile) {
    if (file.uploading) {
      file.cancelled = true
      file.xhr?.abort()
    }
    if (file.previewUrl) URL.revokeObjectURL(file.previewUrl)
  }

  /**
   * Remove the completed pending mirror of an attached file.
   *
   * A drag-drop / clipboard-paste upload lives in TWO stores: `pendingFiles`
   * (upload lifecycle) and `attachedFiles` (the visible card AND the send
   * payload source in ChatPanelContent.sendMessage). Removing the attached
   * card used to clear only `attachedFiles`, so the stale `pendingFiles`
   * entry kept the file in the send payload and kept the (now childless)
   * tags container mounted — reserving dead vertical space. Clear the mirror
   * here so removal is symmetric.
   *
   * In-flight entries are left alone; those are cancelled via removeFile.
   */
  function removePendingByPath(path: string) {
    if (!path) return
    let changed = false
    const next = pendingFiles.value.filter(f => {
      if (f.uploading || f.path !== path) return true
      if (f.previewUrl) URL.revokeObjectURL(f.previewUrl)
      changed = true
      return false
    })
    if (changed) pendingFiles.value = next
  }

  function cleanupPreviewUrls() {
    pendingFiles.value.forEach(f => {
      if (f.previewUrl) URL.revokeObjectURL(f.previewUrl)
    })
  }

  function clearPendingFiles() {
    uploadGeneration++
    pendingFiles.value.forEach(cancelPendingFile)
    pendingFiles.value = []
  }

  /**
   * Leave the current session's staged input.
   *
   * Snapshots the staged attachments / quotes / mentions (plus any uploads that
   * finished and hold a server path) under `sessionId`, then clears the live
   * refs — the exact counterpart of `restoreAttachments`, and what a session
   * switch already does via the session manager.
   *
   * Needed on its own because a PROJECT switch does not go through the session
   * manager: it rebuilds the app subtree, so the module-level live refs
   * (`attachedFiles` / `stagedQuotes` / `stagedMentions` / `pendingFiles`)
   * survive into the next project and the previous project's quote reappears in
   * the new input box. Calling this before the switch keeps the new project's
   * input clean while preserving the draft for the session the user returns to.
   *
   * In-flight uploads are cancelled, not snapshotted: they have no server path
   * yet and were targeted at the project being left behind.
   */
  function leaveInputDraft(sessionId: string) {
    if (sessionId) {
      const pendingPaths = pendingFiles.value.filter(f => f.path && !f.uploading).map(f => f.path)
      for (const p of pendingPaths) addAttachedFile(p)
    }
    snapshotAttachments(sessionId)
    clearAll()
    clearPendingFiles()
  }

  return {
    pendingFiles,
    attachedFiles,
    handleFileSelect,
    readPickedCount,
    handleFileDrop,
    uploadAndAttach,
    removeFile,
    removePendingByPath,
    addAttachedFile,
    removeAttachedFile,
    cleanupPreviewUrls,
    clearPendingFiles,
    leaveInputDraft,
    // Chat upload batch termination (AttachDrawer + the input bar's strip)
    cancelChatUpload,
    terminateChatUploadBatch,
    chatUploadCancelled,
    // Directory upload (file manager)
    dirUploading,
    dirUploadProgress,
    dirUploadSpeed,
    dirUploadTotal,
    dirUploadDone,
    cancelDirUpload,
    uploadFilesToDir: uploadFiles,
    handleFileSelectToDir,
    handleFileDropToDir,
    handleFolderSelect,
    handleFileDropToDirStructured,
    handleFolderDropExpanded,
    downloadDirAsTree,
  }
}
