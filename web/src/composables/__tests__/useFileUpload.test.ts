import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { useFileUpload } from '@/composables/useFileUpload'
import { useChatContext } from '@/composables/useChatContext'

// Mock dependencies
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast.ts', () => ({
  useToast: () => ({
    show: mockToastShow,
  }),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string, params?: Record<string, any>) => params ? `${key}:${JSON.stringify(params)}` : key,
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      uploadMaxFiles: 5,
      uploadMaxSizeMB: 2,
    },
  },
}))

// ── XMLHttpRequest mock ──
// We intercept XMLHttpRequest so tests can simulate responses.
// The mock XHR auto-resolves on send() based on a configurable handler.
let xhrSendHandler: ((xhr: any, formData: FormData) => void) | null = null

function setupXHRMock() {
  const OrigXHR = globalThis.XMLHttpRequest

  // @ts-expect-error mock
  globalThis.XMLHttpRequest = function () {
    const xhr = {
      open: vi.fn(),
      send: vi.fn((formData: FormData) => {
        // Auto-fire response using handler
        if (xhrSendHandler) {
          xhrSendHandler(xhr, formData)
        }
      }),
      timeout: 0,
      upload: { onprogress: null as ((e: any) => void) | null },
      onload: null as (() => void) | null,
      onerror: null as (() => void) | null,
      ontimeout: null as (() => void) | null,
      responseText: '',
      status: 200,
    }
    return xhr
  }

  return () => {
    globalThis.XMLHttpRequest = OrigXHR
  }
}

// Helper: simulate a successful XHR response
function respondSuccess(xhr: any, path: string) {
  xhr.responseText = JSON.stringify({ ok: true, path })
  if (xhr.onload) xhr.onload()
}

// Helper: simulate a failed XHR response
function respondError(xhr: any, error: string) {
  xhr.responseText = JSON.stringify({ ok: false, error })
  if (xhr.onload) xhr.onload()
}

// Helper: simulate a network error
function triggerNetworkError(xhr: any) {
  if (xhr.onerror) xhr.onerror()
}

// Helper: simulate a timeout
function triggerTimeout(xhr: any) {
  if (xhr.ontimeout) xhr.ontimeout()
}

// Helper: create a fake File object
function makeFile(name: string, size = 100, type = 'text/plain') {
  return { name, size, type } as File
}

describe('useFileUpload', () => {
  let teardownXHR: () => void

  beforeEach(() => {
    vi.clearAllMocks()
    xhrSendHandler = null
    teardownXHR = setupXHRMock()
    // Clear global attachedFiles from useChatContext singleton
    useChatContext().clearAll()
  })

  afterEach(() => {
    teardownXHR()
  })

  describe('initial state', () => {
    it('exposes all expected refs and functions', () => {
      const upload = useFileUpload()
      expect(upload.pendingFiles).toBeDefined()
      expect(upload.attachedFiles).toBeDefined()
      expect(upload.dirUploading).toBeDefined()
      expect(upload.dirUploadProgress).toBeDefined()
      expect(upload.dirUploadTotal).toBeDefined()
      expect(upload.dirUploadDone).toBeDefined()
      expect(typeof upload.handleFileSelect).toBe('function')
      expect(typeof upload.handleFileDrop).toBe('function')
      expect(typeof upload.handleFileSelectToDir).toBe('function')
      expect(typeof upload.handleFileDropToDir).toBe('function')
      expect(typeof upload.removeFile).toBe('function')
      expect(typeof upload.addAttachedFile).toBe('function')
      expect(typeof upload.removeAttachedFile).toBe('function')
      expect(typeof upload.cleanupPreviewUrls).toBe('function')
      expect(typeof upload.clearPendingFiles).toBe('function')
      expect(typeof upload.uploadFilesToDir).toBe('function')
    })

    it('starts with empty state', () => {
      const upload = useFileUpload()
      expect(upload.dirUploading.value).toBe(false)
      expect(upload.dirUploadProgress.value).toBe(0)
      expect(upload.dirUploadTotal.value).toBe(0)
      expect(upload.dirUploadDone.value).toBe(0)
      expect(upload.pendingFiles.value).toHaveLength(0)
      expect(upload.attachedFiles.value).toHaveLength(0)
    })
  })

  describe('attachedFiles', () => {
    it('addAttachedFile adds a file path', () => {
      const upload = useFileUpload()
      upload.addAttachedFile('/some/path.txt')
      expect(upload.attachedFiles.value).toContain('/some/path.txt')
    })

    it('addAttachedFile does not add duplicates', () => {
      const upload = useFileUpload()
      upload.addAttachedFile('/some/path.txt')
      upload.addAttachedFile('/some/path.txt')
      expect(upload.attachedFiles.value).toHaveLength(1)
    })

    it('addAttachedFile ignores empty string', () => {
      const upload = useFileUpload()
      upload.addAttachedFile('')
      expect(upload.attachedFiles.value).toHaveLength(0)
    })

    it('removeAttachedFile removes by index', () => {
      const upload = useFileUpload()
      upload.addAttachedFile('/a.txt')
      upload.addAttachedFile('/b.txt')
      upload.removeAttachedFile(0)
      expect(upload.attachedFiles.value).toHaveLength(1)
      expect(upload.attachedFiles.value[0]).toBe('/b.txt')
    })
  })

  describe('pendingFiles', () => {
    it('clearPendingFiles empties the array', () => {
      const upload = useFileUpload()
      upload.pendingFiles.value.push({ path: '', previewUrl: null, isImage: false, uploading: false, progress: 0, size: 0 })
      expect(upload.pendingFiles.value).toHaveLength(1)
      upload.clearPendingFiles()
      expect(upload.pendingFiles.value).toHaveLength(0)
    })

    it('removeFile removes by index', () => {
      const upload = useFileUpload()
      upload.pendingFiles.value.push({ path: 'a', previewUrl: null, isImage: false, uploading: false, progress: 0, size: 0 })
      upload.pendingFiles.value.push({ path: 'b', previewUrl: null, isImage: false, uploading: false, progress: 0, size: 0 })
      upload.removeFile(0)
      expect(upload.pendingFiles.value).toHaveLength(1)
      expect(upload.pendingFiles.value[0].path).toBe('b')
    })
  })

  describe('chat upload (no dir)', () => {
    it('successful upload adds to pendingFiles and updates entry', async () => {
      xhrSendHandler = (xhr) => respondSuccess(xhr, '.clawbench/uploads/test.txt')

      const upload = useFileUpload()
      await upload.handleFileDrop([makeFile('test.txt')])

      expect(upload.pendingFiles.value).toHaveLength(1)
      expect(upload.pendingFiles.value[0].uploading).toBe(false)
      expect(upload.pendingFiles.value[0].progress).toBe(100)
      expect(upload.pendingFiles.value[0].path).toBe('.clawbench/uploads/test.txt')
    })

    it('failed upload removes entry and shows toast', async () => {
      xhrSendHandler = (xhr) => respondError(xhr, 'FileTooLarge')

      const upload = useFileUpload()
      await upload.handleFileDrop([makeFile('bad.txt')])

      expect(upload.pendingFiles.value).toHaveLength(0)
      expect(mockToastShow).toHaveBeenCalled()
    })

    it('network error removes entry and shows toast', async () => {
      xhrSendHandler = (xhr) => triggerNetworkError(xhr)

      const upload = useFileUpload()
      await upload.handleFileDrop([makeFile('neterr.txt')])

      expect(upload.pendingFiles.value).toHaveLength(0)
      expect(mockToastShow).toHaveBeenCalled()
    })

    it('timeout removes entry and shows toast', async () => {
      xhrSendHandler = (xhr) => triggerTimeout(xhr)

      const upload = useFileUpload()
      await upload.handleFileDrop([makeFile('timeout.txt')])

      expect(upload.pendingFiles.value).toHaveLength(0)
      expect(mockToastShow).toHaveBeenCalled()
    })

    it('invalid JSON response removes entry and shows toast', async () => {
      xhrSendHandler = (xhr) => {
        xhr.responseText = 'not valid json'
        if (xhr.onload) xhr.onload()
      }

      const upload = useFileUpload()
      await upload.handleFileDrop([makeFile('parseerr.txt')])

      expect(upload.pendingFiles.value).toHaveLength(0)
      expect(mockToastShow).toHaveBeenCalled()
    })

    it('sends FormData with file via XHR POST', async () => {
      let capturedFormData: FormData | null = null
      xhrSendHandler = (xhr, formData) => {
        capturedFormData = formData
        respondSuccess(xhr, '.clawbench/uploads/test.txt')
      }

      const upload = useFileUpload()
      await upload.handleFileDrop([makeFile('test.txt')])

      expect(capturedFormData).toBeTruthy()
      expect(capturedFormData!.get('file')).toBeTruthy()
      // No 'dir' field for chat upload
      expect(capturedFormData!.get('dir')).toBeNull()
    })
  })

  describe('dir upload', () => {
    it('successful dir upload updates progress refs', async () => {
      xhrSendHandler = (xhr) => respondSuccess(xhr, 'some/dir/file.txt')

      const upload = useFileUpload()
      const promise = upload.handleFileDropToDir([makeFile('file.txt')], '/some/dir')

      // Dir upload should not add to pendingFiles
      expect(upload.pendingFiles.value).toHaveLength(0)
      // dirUploading should be true during upload
      expect(upload.dirUploading.value).toBe(true)
      expect(upload.dirUploadTotal.value).toBe(1)
      expect(upload.dirUploadDone.value).toBe(0)

      await promise

      expect(upload.dirUploadDone.value).toBe(1)
      expect(upload.dirUploading.value).toBe(false)
      expect(upload.dirUploadProgress.value).toBe(0)
    })

    it('dir upload with XHR error completes cycle', async () => {
      xhrSendHandler = (xhr) => triggerNetworkError(xhr)

      const upload = useFileUpload()
      await upload.handleFileDropToDir([makeFile('err.txt')], '/dir')

      expect(upload.dirUploading.value).toBe(false)
      expect(upload.dirUploadDone.value).toBe(1)
    })

    it('sends FormData with dir field for dir upload', async () => {
      let capturedFormData: FormData | null = null
      xhrSendHandler = (xhr, formData) => {
        capturedFormData = formData
        respondSuccess(xhr, 'my/dir/a.txt')
      }

      const upload = useFileUpload()
      await upload.handleFileSelectToDir(
        { target: { files: [makeFile('a.txt')], value: '' } } as any,
        '/my/dir'
      )

      expect(capturedFormData).toBeTruthy()
      expect(capturedFormData!.get('dir')).toBe('/my/dir')
      expect(capturedFormData!.get('file')).toBeTruthy()
    })

    it('dir upload progress tracking with progress event', async () => {
      xhrSendHandler = (xhr) => {
        // Simulate upload progress
        if (xhr.upload.onprogress) {
          xhr.upload.onprogress({ lengthComputable: true, loaded: 50, total: 100 })
        }
        respondSuccess(xhr, 'dir/f.txt')
      }

      const upload = useFileUpload()
      await upload.handleFileDropToDir([makeFile('f.txt')], '/dir')

      // After completion, progress is reset to 0
      expect(upload.dirUploadProgress.value).toBe(0)
      expect(upload.dirUploadDone.value).toBe(1)
    })

    it('multiple files in dir upload', async () => {
      let callCount = 0
      xhrSendHandler = (xhr) => {
        callCount++
        respondSuccess(xhr, `dir/file${callCount}.txt`)
      }

      const upload = useFileUpload()
      await upload.handleFileDropToDir([makeFile('a.txt'), makeFile('b.txt')], '/dir')

      expect(upload.dirUploadTotal.value).toBe(2)
      expect(upload.dirUploadDone.value).toBe(2)
      expect(upload.dirUploading.value).toBe(false)
    })
  })

  describe('uploadFiles — file too large', () => {
    it('skips file larger than max and shows toast', async () => {
      // max size is 2MB, create a 3MB file
      const upload = useFileUpload()
      await upload.handleFileDropToDir([makeFile('big.txt', 3 * 1024 * 1024)], '/dir')

      expect(mockToastShow).toHaveBeenCalledWith(
        expect.stringContaining('upload.fileTooLarge'),
        expect.any(Object)
      )
      // Upload cycle should complete (1 file skipped)
      expect(upload.dirUploadDone.value).toBe(1)
      expect(upload.dirUploading.value).toBe(false)
    })
  })

  describe('uploadFiles — max files reached', () => {
    it('shows toast when no remaining slots', async () => {
      const upload = useFileUpload()
      // Pre-fill pendingFiles to max (5)
      for (let i = 0; i < 5; i++) {
        upload.pendingFiles.value.push({ path: `f${i}.txt`, previewUrl: null, isImage: false, uploading: false, progress: 0, size: 0 })
      }

      await upload.handleFileDrop([makeFile('extra.txt')])

      expect(mockToastShow).toHaveBeenCalledWith(
        expect.stringContaining('upload.maxFiles'),
        expect.any(Object)
      )
    })

    it('truncates file list when too many and shows warning', async () => {
      xhrSendHandler = (xhr) => respondSuccess(xhr, '.clawbench/uploads/f.txt')

      const upload = useFileUpload()
      const files = Array.from({ length: 8 }, (_, i) => makeFile(`file${i}.txt`))

      await upload.handleFileDrop(files)

      // Should have shown too-many-files warning
      const tooManyCalls = mockToastShow.mock.calls.some(
        (call: any[]) => typeof call[0] === 'string' && call[0].includes('upload.tooManyFiles')
      )
      expect(tooManyCalls).toBe(true)
    })
  })

  describe('handleFileSelectToDir', () => {
    it('does nothing when no files selected', async () => {
      const upload = useFileUpload()
      const mockEvent = { target: { files: [], value: 'fake' } }
      await upload.handleFileSelectToDir(mockEvent as any, '/some/dir')
      expect(upload.dirUploading.value).toBe(false)
    })

    it('resets the input value after selection', async () => {
      const upload = useFileUpload()
      const mockEvent = { target: { files: [], value: 'fake' } }
      await upload.handleFileSelectToDir(mockEvent as any, '/some/dir')
      expect(mockEvent.target.value).toBe('')
    })
  })

  describe('handleFileDropToDir', () => {
    it('does nothing when empty file list', async () => {
      const upload = useFileUpload()
      await upload.handleFileDropToDir([], '/some/dir')
      expect(upload.dirUploading.value).toBe(false)
    })
  })

  describe('handleFileSelect', () => {
    it('does nothing when no files', async () => {
      const upload = useFileUpload()
      const mockEvent = { target: { files: [], value: 'x' } }
      await upload.handleFileSelect(mockEvent as any)
    })

    it('resets input value', async () => {
      const upload = useFileUpload()
      const mockEvent = { target: { files: [], value: 'x' } }
      await upload.handleFileSelect(mockEvent as any)
      expect(mockEvent.target.value).toBe('')
    })
  })

  describe('handleFileDrop', () => {
    it('does nothing when empty', async () => {
      const upload = useFileUpload()
      await upload.handleFileDrop([])
    })
  })

  describe('cleanupPreviewUrls', () => {
    it('revokes all preview URLs', () => {
      // Ensure revokeObjectURL exists in jsdom (may not be available in all environments)
      if (!URL.revokeObjectURL) {
        URL.revokeObjectURL = vi.fn()
      }
      const revokeSpy = vi.spyOn(URL, 'revokeObjectURL')
      const upload = useFileUpload()
      upload.pendingFiles.value.push(
        { path: 'a', previewUrl: 'blob:a', isImage: true, uploading: false, progress: 0, size: 100 },
        { path: 'b', previewUrl: null, isImage: false, uploading: false, progress: 0, size: 200 },
        { path: 'c', previewUrl: 'blob:c', isImage: true, uploading: false, progress: 0, size: 300 },
      )
      upload.cleanupPreviewUrls()
      expect(revokeSpy).toHaveBeenCalledTimes(2)
      revokeSpy.mockRestore()
    })
  })
})
