import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest'
import { buildLocalFileUrl, downloadFileByPath, downloadByUrl, downloadUrlWithProgress, postForBlobWithProgress } from '@/utils/download.ts'
import { setShareToken } from '@/share/shareMode'
import { cancelDownload, useDownloadProgress } from '@/composables/useDownloadProgress'

// Track setTimeout IDs to clean up after each test
const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

afterEach(() => {
  for (const id of pendingTimers) {
    clearTimeout(id)
  }
  pendingTimers.length = 0
  delete (window as any).ClawBenchNative
  // A leaked visible bar would make the next test's beginDownload() fail.
  cancelDownload()
})

describe('buildLocalFileUrl', () => {
  it('encodes path segments individually', () => {
    expect(buildLocalFileUrl('foo/bar baz/file.pdf')).toBe(
      '/api/fs/raw/foo/bar%20baz/file.pdf'
    )
  })

  it('adds download=1 query param', () => {
    expect(buildLocalFileUrl('doc.pdf', { download: true })).toBe(
      '/api/fs/raw/doc.pdf?download=1'
    )
  })

  it('handles simple filename without slashes', () => {
    expect(buildLocalFileUrl('readme.md')).toBe('/api/fs/raw/readme.md')
  })

  it('uses ?path= query param for absolute paths', () => {
    const url = buildLocalFileUrl('/home/user/docs/report.pdf')
    expect(url).toBe('/api/fs/raw/?target=%2Fhome%2Fuser%2Fdocs%2Freport.pdf')
  })

  it('uses ?path= query param for absolute paths with download', () => {
    const url = buildLocalFileUrl('/tmp/data.csv', { download: true })
    expect(url).toBe('/api/fs/raw/?download=1&target=%2Ftmp%2Fdata.csv')
  })
})

describe('buildLocalFileUrl in share mode', () => {
  afterEach(() => {
    setShareToken(null)
  })

  it('routes absolute paths through the token-scoped local endpoint', () => {
    setShareToken('tok123')
    const url = buildLocalFileUrl('/home/user/docs/report.pdf')
    expect(url).toBe('/api/share/tok123/local?path=%2Fhome%2Fuser%2Fdocs%2Freport.pdf')
  })

  it('routes relative paths through the token-scoped local endpoint', () => {
    setShareToken('tok123')
    expect(buildLocalFileUrl('docs/a.png')).toBe('/api/share/tok123/local/docs/a.png')
    expect(buildLocalFileUrl('foo/bar baz/file.pdf')).toBe('/api/share/tok123/local/foo/bar%20baz/file.pdf')
  })

  it('supports download param in share mode', () => {
    setShareToken('tok123')
    const url = buildLocalFileUrl('/abs/file.bin', { download: true })
    expect(url).toBe('/api/share/tok123/local?download=1&path=%2Fabs%2Ffile.bin')
  })

  it('restores normal URLs after token cleared', () => {
    setShareToken('tok123')
    expect(buildLocalFileUrl('readme.md')).toContain('/api/share/tok123/')
    setShareToken(null)
    expect(buildLocalFileUrl('readme.md')).toBe('/api/fs/raw/readme.md')
  })
})

/**
 * Minimal XMLHttpRequest double. The download path uses XHR rather than fetch
 * because only XHR reports download-side progress, so the tests drive the
 * handlers directly instead of going through a network stack.
 */
class FakeXhr {
  static instances: FakeXhr[] = []
  method = ''
  url = ''
  status = 200
  response: unknown = new Blob(['data'])
  responseType = ''
  onprogress: ((e: { loaded: number; total: number; lengthComputable: boolean }) => void) | null = null
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  aborted = false

  constructor() { FakeXhr.instances.push(this) }
  open(method: string, url: string) { this.method = method; this.url = url }
  setRequestHeader() {}
  send() {}
  abort() { this.aborted = true; this.onabort?.() }
}

function installFakeXhr() {
  FakeXhr.instances = []
  vi.stubGlobal('XMLHttpRequest', FakeXhr as unknown as typeof XMLHttpRequest)
}

describe('downloadFileByPath', () => {
  beforeEach(() => {
    installFakeXhr()
    // The anchor is only used by the progress-less fallback; stub it so a
    // mis-routed test fails loudly instead of mutating the DOM.
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('does nothing for empty path', () => {
    downloadFileByPath('')
    expect(FakeXhr.instances).toHaveLength(0)
  })

  it('streams via XHR with download=1 for a relative path', () => {
    downloadFileByPath('test.pdf')

    expect(FakeXhr.instances).toHaveLength(1)
    const xhr = FakeXhr.instances[0]
    expect(xhr.method).toBe('GET')
    expect(xhr.url).toBe('/api/fs/raw/test.pdf?download=1')
    expect(xhr.responseType).toBe('blob')
  })

  it('uses ?target= for absolute paths', () => {
    downloadFileByPath('/home/user/docs/report.pdf')

    const xhr = FakeXhr.instances[0]
    expect(xhr.url).toContain('/api/fs/raw/?')
    expect(xhr.url).toContain('target=')
    expect(xhr.url).toContain('download=1')
  })

  it('reports byte progress and hides the bar when done', () => {
    const { downloadVisible, downloadReceived, downloadTotal, downloadFileName } = useDownloadProgress()
    downloadFileByPath('big.bin', 'big.bin')

    expect(downloadVisible.value).toBe(true)
    expect(downloadFileName.value).toBe('big.bin')

    const xhr = FakeXhr.instances[0]
    xhr.onprogress?.({ loaded: 250, total: 1000, lengthComputable: true })
    expect(downloadReceived.value).toBe(250)
    expect(downloadTotal.value).toBe(1000)

    xhr.onload?.()
    expect(downloadVisible.value).toBe(false)
  })

  it('treats a non-2xx response as a failure and still hides the bar', () => {
    const { downloadVisible } = useDownloadProgress()
    downloadFileByPath('missing.bin')

    const xhr = FakeXhr.instances[0]
    xhr.status = 404
    xhr.onload?.()

    expect(downloadVisible.value).toBe(false)
  })

  it('falls back to the anchor when another download is already showing', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    const { downloadVisible } = useDownloadProgress()

    downloadFileByPath('first.bin')
    expect(downloadVisible.value).toBe(true)

    downloadFileByPath('second.bin')
    // Only the first used XHR; the second went to the anchor fallback.
    expect(FakeXhr.instances).toHaveLength(1)
    expect(appendSpy).toHaveBeenCalled()

    appendSpy.mockRestore()
  })

  it('uses the native bridge when the host exposes downloadFileWithProgress', () => {
    const downloadFileWithProgress = vi.fn().mockResolvedValue(undefined)
    ;(window as any).ClawBenchNative = {
      downloadFile: vi.fn(),
      downloadFileWithProgress,
      cancelDownload: vi.fn(),
    }

    downloadFileByPath('native.bin', 'native.bin')

    expect(FakeXhr.instances).toHaveLength(0)
    expect(downloadFileWithProgress).toHaveBeenCalledWith('native.bin', 'native.bin', expect.any(Number))
  })

  it('falls back to the legacy native downloadFile when the host lacks progress support', () => {
    const downloadFile = vi.fn()
    ;(window as any).ClawBenchNative = { downloadFile }

    downloadFileByPath('native.bin', 'native.bin')

    // No bar (the host cannot report progress) and no XHR — the host downloads.
    expect(downloadFile).toHaveBeenCalledWith('native.bin')
    expect(FakeXhr.instances).toHaveLength(0)
    expect(useDownloadProgress().downloadVisible.value).toBe(false)
  })

  it('drives the bar from native progress events and settles on done', () => {
    const downloadFileWithProgress = vi.fn().mockResolvedValue(undefined)
    ;(window as any).ClawBenchNative = { downloadFileWithProgress, cancelDownload: vi.fn() }
    const { downloadVisible, downloadReceived } = useDownloadProgress()

    downloadFileByPath('native.bin', 'native.bin')
    const id = (downloadFileWithProgress.mock.calls[0] as unknown[])[2] as number

    window.dispatchEvent(new CustomEvent('clawbench-download-progress', {
      detail: { id, received: 512, total: 2048, done: false, error: false },
    }))
    expect(downloadReceived.value).toBe(512)

    window.dispatchEvent(new CustomEvent('clawbench-download-progress', {
      detail: { id, received: 2048, total: 2048, done: true, error: false },
    }))
    expect(downloadVisible.value).toBe(false)
  })

  it('hides the bar when an async host (Electron) resolves its promise', async () => {
    // Electron's ipcRenderer.invoke settles when the main-process transfer is
    // done and never sends a terminal event, so the promise is the only signal.
    let resolveDownload!: () => void
    const downloadFileWithProgress = vi.fn(() => new Promise<void>((r) => { resolveDownload = r }))
    ;(window as any).ClawBenchNative = { downloadFileWithProgress, cancelDownload: vi.fn() }
    const { downloadVisible } = useDownloadProgress()

    downloadFileByPath('native.bin', 'native.bin')
    expect(downloadVisible.value).toBe(true)

    resolveDownload()
    await Promise.resolve()
    await Promise.resolve()
    expect(downloadVisible.value).toBe(false)
  })

  it('keeps the bar visible while a sync host (Android) has not reported done', async () => {
    // Android's @JavascriptInterface method returns undefined synchronously;
    // treating that as completion would hide the bar the instant it appeared.
    // Microtasks must be flushed or this would pass for the wrong reason.
    const downloadFileWithProgress = vi.fn(() => undefined)
    ;(window as any).ClawBenchNative = { downloadFileWithProgress, cancelDownload: vi.fn() }
    const { downloadVisible, downloadReceived } = useDownloadProgress()

    downloadFileByPath('native.bin', 'native.bin')
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
    expect(downloadVisible.value).toBe(true)

    // And the host's later progress event must still be accepted.
    const id = (downloadFileWithProgress.mock.calls[0] as unknown[])[2] as number
    window.dispatchEvent(new CustomEvent('clawbench-download-progress', {
      detail: { id, received: 128, total: 256, done: false, error: false },
    }))
    expect(downloadReceived.value).toBe(128)
  })

  it('settles silently when the host reports a cancellation', () => {
    const downloadFileWithProgress = vi.fn().mockResolvedValue(undefined)
    ;(window as any).ClawBenchNative = { downloadFileWithProgress, cancelDownload: vi.fn() }
    const { downloadVisible } = useDownloadProgress()

    downloadFileByPath('native.bin', 'native.bin')
    const id = (downloadFileWithProgress.mock.calls[0] as unknown[])[2] as number

    // A cancelled transfer must hide the bar without reporting a failure.
    window.dispatchEvent(new CustomEvent('clawbench-download-progress', {
      detail: { id, received: 100, total: 200, done: true, error: false, cancelled: true },
    }))
    expect(downloadVisible.value).toBe(false)
  })

  it('ignores native progress events for a different download id', () => {
    const downloadFileWithProgress = vi.fn().mockResolvedValue(undefined)
    ;(window as any).ClawBenchNative = { downloadFileWithProgress, cancelDownload: vi.fn() }
    const { downloadReceived } = useDownloadProgress()

    downloadFileByPath('native.bin', 'native.bin')

    window.dispatchEvent(new CustomEvent('clawbench-download-progress', {
      detail: { id: 999999, received: 999, total: 999, done: false, error: false },
    }))
    expect(downloadReceived.value).toBe(0)
  })
})

describe('downloadUrlWithProgress', () => {
  beforeEach(() => {
    installFakeXhr()
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('streams the URL and exposes the file name', () => {
    const { downloadVisible, downloadFileName } = useDownloadProgress()
    downloadUrlWithProgress('/api/share/tok/download', 'shared.md')

    expect(FakeXhr.instances).toHaveLength(1)
    expect(FakeXhr.instances[0].url).toBe('/api/share/tok/download')
    expect(downloadVisible.value).toBe(true)
    expect(downloadFileName.value).toBe('shared.md')
  })

  it('falls back to the anchor when the bar is already taken', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    downloadFileByPath('first.bin')

    downloadUrlWithProgress('/api/share/tok/download', 'shared.md')

    // Only the first used XHR; the second could not show progress.
    expect(FakeXhr.instances).toHaveLength(1)
    expect(appendSpy).toHaveBeenCalled()
    appendSpy.mockRestore()
  })
})

describe('postForBlobWithProgress', () => {
  beforeEach(() => {
    installFakeXhr()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('POSTs JSON and resolves the response blob', async () => {
    const p = postForBlobWithProgress('/api/file/archive', { paths: ['a'] }, 'a.zip')
    const xhr = FakeXhr.instances[0]
    expect(xhr.method).toBe('POST')
    xhr.onload?.()
    const result = await p
    expect(result.blob).toBeInstanceOf(Blob)
  })

  it('falls back to fetch when another download already owns the bar', async () => {
    // Regression guard: returning early here used to drop the request, so the
    // archive silently failed to download whenever a file download was running.
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      blob: () => Promise.resolve(new Blob(['zip'])),
    })
    vi.stubGlobal('fetch', fetchMock)

    downloadFileByPath('first.bin')
    const result = await postForBlobWithProgress('/api/file/archive', { paths: ['a'] }, 'a.zip')

    expect(fetchMock).toHaveBeenCalledWith('/api/file/archive', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ paths: ['a'] }),
    }))
    expect(result.blob).toBeInstanceOf(Blob)
    // The fallback must not have created a second XHR for the archive.
    expect(FakeXhr.instances).toHaveLength(1)
  })

  it('surfaces the server error message from a failed fallback fetch', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'too many files' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    downloadFileByPath('first.bin')
    const result = await postForBlobWithProgress('/api/file/archive', { paths: ['a'] }, 'a.zip')

    expect(result.blob).toBeNull()
    expect(result.errorDetail).toBe('too many files')
  })
})

describe('downloadByUrl', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('does nothing for empty URL', () => {
    downloadByUrl('')
  })

  it('creates anchor element with correct href for web mode', () => {
    const appendChildSpy = vi.spyOn(document.body, 'appendChild')
    downloadByUrl('/api/apk', 'clawbench-android.apk')

    expect(appendChildSpy).toHaveBeenCalled()
    const anchor = appendChildSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.href).toContain('/api/apk')
    expect(anchor.download).toBe('clawbench-android.apk')

    appendChildSpy.mockRestore()
    anchor.remove()
  })

  it('uses last URL segment as default fileName', () => {
    const appendChildSpy = vi.spyOn(document.body, 'appendChild')
    downloadByUrl('/api/apk')

    const anchor = appendChildSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.download).toBe('apk')

    appendChildSpy.mockRestore()
    anchor.remove()
  })

  it('calls ClawBenchNative.downloadUrl in app mode', () => {
    const mockDownloadUrl = vi.fn()
    ;(window as any).ClawBenchNative = { downloadUrl: mockDownloadUrl }

    downloadByUrl('/api/apk', 'clawbench-android.apk')
    expect(mockDownloadUrl).toHaveBeenCalledWith('/api/apk', 'clawbench-android.apk')
  })
})
