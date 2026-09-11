import { ref } from 'vue'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { store } from '@/stores/app.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { folderRelPath } from '@/utils/fileAttachmentUtils'
import { expandDataTransfer, type ExpandResult } from '@/utils/dropFolder'
import { writeFileToTree } from '@/utils/dirHandle'
import { buildLocalFileUrl } from '@/utils/download'

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
let activeDirXhr: XMLHttpRequest | null = null
let activeDownloadAbort: AbortController | null = null

export function useFileUpload() {
  const toast = useToast()

  // attachedFiles is managed globally via useChatContext so any tab
  // (file preview, chat input, quote-question) can read/write it.
  const { attachedFiles, addAttachedFile, removeAttachedFile } = useChatContext()

  function uploadOneFile(file: File, dir?: string, autoAttach?: boolean, relPath?: string) {
    return new Promise((resolve) => {
      const isDirUpload = !!dir

      // Route a failure reason either to an immediate toast (chat attachment)
      // or into the dir-upload collector for a single summary toast at the end.
      // Dir mode must NOT toast per-file, otherwise the completion toast
      // (singleton) overwrites the error and the user sees no failure at all.
      // The stored reason stays bare (no "upload failed" prefix); finishDirUpload
      // wraps it in the aggregate message exactly once.
      function notifyError(msg: string) {
        if (isDirUpload) dirUploadErrorMsgs.push(msg)
        else toast.show(msg, { icon: '⚠️', type: 'error' })
      }
      // Chat toasts keep the "上传失败:" prefix; dir mode stores the bare reason.
      function failError(reason: string) {
        if (isDirUpload) dirUploadErrorMsgs.push(reason)
        else toast.show(gt('upload.failed', { error: reason }), { icon: '⚠️', type: 'error' })
      }

      // Pre-flight size check: prevent sending a request that will be
      // rejected by the server's MaxBytesReader (which causes onerror
      // instead of a readable error response).
      const maxSizeBytes = store.state.uploadMaxSizeMB * 1024 * 1024
      if (file.size > maxSizeBytes) {
        notifyError(gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }))
        resolve(false)
        return
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

      const formData = new FormData()
      formData.append('file', file)
      if (dir) formData.append('dir', dir)
      if (relPath) formData.append('relpath', relPath)

      const xhr = new XMLHttpRequest()
      if (entry) entry.xhr = xhr
      if (isDirUpload) activeDirXhr = xhr
      xhr.open('POST', '/api/upload/file')
      xhr.timeout = 300000

      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) {
          const pct = Math.round((e.loaded / e.total) * 100)
          if (entry) entry.progress = pct
          if (isDirUpload) dirUploadProgress.value = pct
        }
      }

      xhr.onload = () => {
        if (entry?.cancelled) {
          resolve(false)
          return
        }
        try {
          const data = JSON.parse(xhr.responseText)
          if (data.ok) {
            if (entry) {
              entry.uploading = false
              entry.progress = 100
              entry.path = data.path
              if (autoAttach) addAttachedFile(entry.path)
            }
            resolve(true)
          } else {
            if (entry) {
              if (previewUrl) URL.revokeObjectURL(previewUrl)
              const i = pendingFiles.value.indexOf(entry)
              if (i !== -1) pendingFiles.value.splice(i, 1)
            }
            failError(data.error || gt('upload.unknownError'))
            resolve(false)
          }
        } catch {
          if (entry) {
            if (previewUrl) URL.revokeObjectURL(previewUrl)
            const i = pendingFiles.value.indexOf(entry)
            if (i !== -1) pendingFiles.value.splice(i, 1)
          }
          notifyError(gt('upload.parseError'))
          resolve(false)
        }
      }

      xhr.onerror = () => {
        if (entry?.cancelled) {
          resolve(false)
          return
        }
        if (entry) {
          entry.uploading = false
          if (previewUrl) URL.revokeObjectURL(previewUrl)
          const i = pendingFiles.value.indexOf(entry)
          if (i !== -1) pendingFiles.value.splice(i, 1)
        }
        // When the server's MaxBytesReader rejects the upload, the XHR
        // gets onerror instead of onload with a parseable response.
        // If the file exceeds the threshold, show a size-specific error.
        notifyError(file.size > maxSizeBytes
          ? gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB })
          : gt('upload.networkError'))
        resolve(false)
      }

      xhr.ontimeout = () => {
        if (entry?.cancelled) {
          resolve(false)
          return
        }
        if (entry) {
          entry.uploading = false
          if (previewUrl) URL.revokeObjectURL(previewUrl)
          const i = pendingFiles.value.indexOf(entry)
          if (i !== -1) pendingFiles.value.splice(i, 1)
        }
        notifyError(gt('upload.timeout'))
        resolve(false)
      }

      // Removing a pending card aborts the request without showing a network error.
      xhr.onabort = () => resolve(false)

      xhr.send(formData)
    })
  }

  /** Aggregate the dir-upload result into a single summary toast. */
  function finishDirUpload(okCount: number, failCount: number) {
    dirUploading.value = false
    dirUploadProgress.value = 0
    activeDirXhr = null
    const cancelled = dirUploadCancelled.value
    const errs = dirUploadErrorMsgs
    dirUploadCancelled.value = false
    dirUploadErrorMsgs = []
    if (cancelled) {
      toast.show(gt('upload.cancelled'), { icon: '⏹️', type: 'info' })
      return
    }
    if (failCount === 0) {
      if (okCount > 0) {
        toast.show(gt('upload.completed', { count: okCount }), { icon: '✅', type: 'success' })
      }
      return
    }
    const firstErr = errs[0] || gt('upload.unknownError')
    if (okCount > 0) {
      toast.show(gt('upload.partial', { ok: okCount, failed: failCount, error: firstErr }), { icon: '⚠️', type: 'error' })
    } else {
      toast.show(gt('upload.failed', { error: firstErr }), { icon: '⚠️', type: 'error' })
    }
  }

  async function uploadFiles(files: File[], dir?: string, preserveStructure = false) {
    const maxFiles = store.state.uploadMaxFiles
    const currentCount = pendingFiles.value.filter(f => !f.uploading).length
    const remaining = maxFiles - currentCount
    if (remaining <= 0) {
      toast.show(gt('upload.maxFiles', { max: maxFiles }), { icon: '⚠️', type: 'error' })
      return
    }

    const toUpload = files.slice(0, remaining)
    if (files.length > remaining) {
      toast.show(gt('upload.tooManyFiles', { total: files.length, remaining }), { icon: '⚠️', type: 'error' })
    }

    const maxSizeBytes = store.state.uploadMaxSizeMB * 1024 * 1024

    // Dir upload progress tracking
    const isDirUpload = !!dir
    if (isDirUpload) {
      dirUploading.value = true
      dirUploadCancelled.value = false
      activeDirXhr = null
      dirUploadTotal.value = toUpload.length
      dirUploadDone.value = 0
      dirUploadProgress.value = 0
      dirUploadErrorMsgs = []
    }

    let okCount = 0
    let failCount = 0

    for (const file of toUpload) {
      if (isDirUpload && dirUploadCancelled.value) break
      if (file.size > maxSizeBytes) {
        const msg = gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB })
        if (isDirUpload) {
          dirUploadErrorMsgs.push(msg)
          failCount++
          dirUploadDone.value++
        } else {
          toast.show(msg, { icon: '⚠️', type: 'error' })
        }
        continue
      }
      // When preserving folder structure, derive each file's relative sub-path
      // (including the top-level folder) from webkitRelativePath.
      const relPath = preserveStructure ? folderRelPath(file) || undefined : undefined
      const ok = await uploadOneFile(file, dir, false, relPath)
      if (isDirUpload && dirUploadCancelled.value) break
      if (isDirUpload) {
        if (ok) okCount++
        else failCount++
        dirUploadDone.value++
      }
    }

    if (isDirUpload) {
      finishDirUpload(okCount, failCount)
    }
  }

  async function handleFileSelect(e: Event) {
    const files = Array.from((e.target as HTMLInputElement).files || [])
    // Reset input immediately to prevent Android WebView from re-firing
    // the change event with stale file data on picker cancellation
    ;(e.target as HTMLInputElement).value = ''
    if (files.length === 0) return
    await uploadFiles(files)
  }

  async function handleFileDrop(files: File[]) {
    if (files.length === 0) return
    await uploadFiles(files)
  }

  /** Upload files and auto-attach each one after it succeeds (for drag-drop / clipboard paste). */
  async function uploadAndAttach(files: File[]) {
    if (files.length === 0) return
    const maxFiles = store.state.uploadMaxFiles
    const currentCount = pendingFiles.value.filter(f => !f.uploading).length
    const remaining = maxFiles - currentCount
    if (remaining <= 0) {
      toast.show(gt('upload.maxFiles', { max: maxFiles }), { icon: '⚠️', type: 'error' })
      return
    }
    const toUpload = files.slice(0, remaining)
    if (files.length > remaining) {
      toast.show(gt('upload.tooManyFiles', { total: files.length, remaining }), { icon: '⚠️', type: 'error' })
    }
    const maxSizeBytes = store.state.uploadMaxSizeMB * 1024 * 1024
    const generation = uploadGeneration
    for (const file of toUpload) {
      if (generation !== uploadGeneration) break
      if (file.size > maxSizeBytes) {
        toast.show(gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }), { icon: '⚠️', type: 'error' })
        continue
      }
      await uploadOneFile(file, undefined, true)
    }
  }

  async function handleFileSelectToDir(e: Event, dir: string) {
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
    dirUploading.value = true
    dirUploadCancelled.value = false
    activeDirXhr = null
    dirUploadErrorMsgs = []
    const total = result.files.length + result.emptyDirs.length
    dirUploadTotal.value = total
    dirUploadDone.value = 0
    dirUploadProgress.value = 0
    let done = 0
    let okCount = 0
    let failCount = 0

    for (const { file, relPath } of result.files) {
      if (dirUploadCancelled.value) break
      if (file.size > maxSizeBytes) {
        dirUploadErrorMsgs.push(gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }))
        failCount++
      } else {
        const ok = await uploadOneFile(file, dir, false, relPath || undefined)
        if (dirUploadCancelled.value) break
        if (ok) okCount++
        else failCount++
      }
      done++
      dirUploadDone.value = done
    }

    for (const emptyDir of result.emptyDirs) {
      if (dirUploadCancelled.value) break
      const ok = await createDir(dir, emptyDir)
      if (ok) {
        done++
        okCount++
        dirUploadDone.value = done
      }
    }

    finishDirUpload(okCount, failCount)
  }

  /** Abort an in-progress directory upload or tree download. */
  function cancelDirUpload() {
    if (!dirUploading.value) return
    dirUploadCancelled.value = true
    activeDirXhr?.abort()
    activeDirXhr = null
    activeDownloadAbort?.abort()
    activeDownloadAbort = null
    dirUploading.value = false
    dirUploadProgress.value = 0
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
      toast.show(gt('upload.dirDownloadUnsupported'), { icon: '⚠️', type: 'error' })
      return
    }

    dirUploading.value = true
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
      toast.show(gt('upload.dirDownloadFailed'), { icon: '⚠️', type: 'error' })
      return
    }
    const tree = files ?? []
    dirUploadTotal.value = tree.length

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
        const total = f.size || 1
        let received = 0
        for (;;) {
          const { done: rd, value } = await reader.read()
          if (rd) break
          chunks.push(value as unknown as BlobPart)
          received += value.length
          dirUploadProgress.value = Math.min(100, Math.round((received / total) * 100))
        }
        const blob = new Blob(chunks)
        await writeFileToTree(rootHandle, f.rel, blob)
        done++
        dirUploadDone.value = done
      } catch {
        if (abort.signal.aborted) break
      }
    }

    activeDownloadAbort = null
    dirUploading.value = false
    dirUploadProgress.value = 0
    if (dirUploadCancelled.value) {
      toast.show(gt('upload.cancelled'), { icon: '⏹️', type: 'info' })
    } else if (done > 0) {
      toast.show(gt('upload.downloaded', { count: done }), { icon: '✅', type: 'success' })
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

  return {
    pendingFiles,
    attachedFiles,
    handleFileSelect,
    handleFileDrop,
    uploadAndAttach,
    removeFile,
    removePendingByPath,
    addAttachedFile,
    removeAttachedFile,
    cleanupPreviewUrls,
    clearPendingFiles,
    // Directory upload (file manager)
    dirUploading,
    dirUploadProgress,
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
