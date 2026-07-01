import { ref } from 'vue'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { store } from '@/stores/app.ts'
import { useChatContext } from '@/composables/useChatContext.ts'

export function useFileUpload() {
  const toast = useToast()

  const pendingFiles = ref<Array<{ path: string; previewUrl: string | null; isImage: boolean; uploading: boolean; progress: number; size: number }>>([])

  // attachedFiles is managed globally via useChatContext so any tab
  // (file preview, chat input, quote-question) can read/write it.
  const { attachedFiles, addAttachedFile, removeAttachedFile } = useChatContext()

  // Upload progress for directory uploads (file manager)
  const dirUploading = ref(false)
  const dirUploadProgress = ref(0)
  const dirUploadTotal = ref(0)
  const dirUploadDone = ref(0)

  function uploadOneFile(file: File, dir?: string) {
    return new Promise((resolve) => {
      const isImage = file.type.startsWith('image/')
      const previewUrl = isImage ? URL.createObjectURL(file) : null

      // Push entry then get reactive proxy from array (only for chat upload, not dir upload)
      const isDirUpload = !!dir
      let entry = null
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

      const xhr = new XMLHttpRequest()
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
        try {
          const data = JSON.parse(xhr.responseText)
          if (data.ok) {
            if (entry) {
              entry.uploading = false
              entry.progress = 100
              entry.path = data.path
            }
            resolve(true)
          } else {
            if (entry) {
              if (previewUrl) URL.revokeObjectURL(previewUrl)
              const i = pendingFiles.value.indexOf(entry)
              if (i !== -1) pendingFiles.value.splice(i, 1)
            }
            toast.show(gt('upload.failed', { error: data.error || gt('upload.unknownError') }), { icon: '⚠️', type: 'error' })
            resolve(false)
          }
        } catch {
          if (entry) {
            if (previewUrl) URL.revokeObjectURL(previewUrl)
            const i = pendingFiles.value.indexOf(entry)
            if (i !== -1) pendingFiles.value.splice(i, 1)
          }
          toast.show(gt('upload.parseError'), { icon: '⚠️', type: 'error' })
          resolve(false)
        }
      }

      xhr.onerror = () => {
        if (entry) {
          entry.uploading = false
          if (previewUrl) URL.revokeObjectURL(previewUrl)
          const i = pendingFiles.value.indexOf(entry)
          if (i !== -1) pendingFiles.value.splice(i, 1)
        }
        toast.show(gt('upload.networkError'), { icon: '⚠️', type: 'error' })
        resolve(false)
      }

      xhr.ontimeout = () => {
        if (entry) {
          entry.uploading = false
          if (previewUrl) URL.revokeObjectURL(previewUrl)
          const i = pendingFiles.value.indexOf(entry)
          if (i !== -1) pendingFiles.value.splice(i, 1)
        }
        toast.show(gt('upload.timeout'), { icon: '⚠️', type: 'error' })
        resolve(false)
      }

      xhr.send(formData)
    })
  }

  async function uploadFiles(files: File[], dir?: string) {
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
      dirUploadTotal.value = toUpload.length
      dirUploadDone.value = 0
      dirUploadProgress.value = 0
    }

    for (const file of toUpload) {
      if (file.size > maxSizeBytes) {
        toast.show(gt('upload.fileTooLarge', { name: file.name, max: store.state.uploadMaxSizeMB }), { icon: '⚠️', type: 'error' })
        if (isDirUpload) dirUploadDone.value++
        continue
      }
      await uploadOneFile(file, dir)
      if (isDirUpload) dirUploadDone.value++
    }

    if (isDirUpload) {
      dirUploading.value = false
      dirUploadProgress.value = 0
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

  function removeFile(index: number) {
    const f = pendingFiles.value[index]
    if (f?.previewUrl) {
      URL.revokeObjectURL(f.previewUrl)
    }
    pendingFiles.value.splice(index, 1)
  }

  function cleanupPreviewUrls() {
    pendingFiles.value.forEach(f => {
      if (f.previewUrl) URL.revokeObjectURL(f.previewUrl)
    })
  }

  function clearPendingFiles() {
    cleanupPreviewUrls()
    pendingFiles.value = []
  }

  return {
    pendingFiles,
    attachedFiles,
    handleFileSelect,
    handleFileDrop,
    removeFile,
    addAttachedFile,
    removeAttachedFile,
    cleanupPreviewUrls,
    clearPendingFiles,
    // Directory upload (file manager)
    dirUploading,
    dirUploadProgress,
    dirUploadTotal,
    dirUploadDone,
    uploadFilesToDir: uploadFiles,
    handleFileSelectToDir,
    handleFileDropToDir,
  }
}
