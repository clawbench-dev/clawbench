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
  vi.useRealTimers()
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
  /** Headers set by the caller (the download sends a Range header). */
  requestHeaders: Record<string, string> = {}
  /** Headers the fake server would answer with. */
  responseHeaders: Record<string, string> = {}
  onprogress: ((e: { loaded: number; total: number; lengthComputable: boolean }) => void) | null = null
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  ontimeout: (() => void) | null = null
  aborted = false

  constructor() { FakeXhr.instances.push(this) }
  open(method: string, url: string) { this.method = method; this.url = url }
  setRequestHeader(name: string, value: string) { this.requestHeaders[name] = value }
  getResponseHeader(name: string): string | null { return this.responseHeaders[name] ?? null }
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
    // A 4xx is a definitive answer — resending the same request cannot help.
    expect(FakeXhr.instances).toHaveLength(1)
  })

  describe('range resume and retry', () => {
    // Mirrors DOWNLOAD_CHUNK_BYTES in download.ts.
    const CHUNK = 4 * 1024 * 1024

    /** Answer the nth request with one 206 slice of `fill` bytes. */
    function answerChunk(index: number, start: number, total: number, fill = 1, size = CHUNK) {
      const xhr = FakeXhr.instances[index]
      xhr.status = 206
      xhr.responseHeaders['Content-Range'] = `bytes ${start}-${start + size - 1}/${total}`
      xhr.response = new Blob([new Uint8Array(size).fill(fill)])
      xhr.onload?.()
    }

    it('requests bounded Range chunks and resumes at the last completed one', async () => {
      vi.useFakeTimers()
      const { downloadVisible, downloadReceived } = useDownloadProgress()
      downloadFileByPath('big.bin', 'big.bin')

      expect(FakeXhr.instances[0].requestHeaders.Range).toBe(`bytes=0-${CHUNK - 1}`)
      answerChunk(0, 0, CHUNK * 2)

      // The next chunk starts where the first ended.
      expect(FakeXhr.instances).toHaveLength(2)
      expect(FakeXhr.instances[1].requestHeaders.Range).toBe(`bytes=${CHUNK}-${CHUNK * 2 - 1}`)

      // Connection drops mid-transfer: the bar must survive the backoff
      // instead of vanishing (the reported "progress bar disappears at 10%").
      FakeXhr.instances[1].onerror?.()
      expect(downloadVisible.value).toBe(true)
      expect(downloadReceived.value).toBe(CHUNK)

      await vi.advanceTimersByTimeAsync(500)
      expect(FakeXhr.instances).toHaveLength(3)
      expect(FakeXhr.instances[2].requestHeaders.Range).toBe(`bytes=${CHUNK}-${CHUNK * 2 - 1}`)
    })

    it('handles a last chunk shorter than the requested range', async () => {
      // Real ServeContent clamps the final slice to the file size, so the last
      // 206 is shorter than CHUNK and its Content-Range end is total-1.
      const TOTAL = CHUNK + 10
      const saved: Blob[] = []
      vi.spyOn(URL, 'createObjectURL').mockImplementation((obj: Blob | MediaSource) => {
        saved.push(obj as Blob)
        return 'blob:test'
      })
      downloadFileByPath('odd.bin', 'odd.bin')

      answerChunk(0, 0, TOTAL, 1)
      answerChunk(1, CHUNK, TOTAL, 2, 10)

      expect(saved).toHaveLength(1)
      expect(saved[0].size).toBe(TOTAL)
      expect(FakeXhr.instances).toHaveLength(2)
    })

    it('refuses a 206 slice whose length contradicts Content-Range', async () => {
      vi.useFakeTimers()
      const saved: Blob[] = []
      vi.spyOn(URL, 'createObjectURL').mockImplementation((obj: Blob | MediaSource) => {
        saved.push(obj as Blob)
        return 'blob:test'
      })
      downloadFileByPath('short.bin', 'short.bin')

      const xhr = FakeXhr.instances[0]
      xhr.status = 206
      // Header promises CHUNK bytes, body carries one less.
      xhr.responseHeaders['Content-Range'] = `bytes 0-${CHUNK - 1}/${CHUNK * 2}`
      xhr.response = new Blob([new Uint8Array(CHUNK - 1)])
      xhr.onload?.()

      expect(saved).toHaveLength(0)
      await vi.advanceTimersByTimeAsync(500)
      expect(FakeXhr.instances).toHaveLength(2)
      expect(FakeXhr.instances[1].requestHeaders.Range).toBe(`bytes=0-${CHUNK - 1}`)
    })

    it('refuses a 206 slice that starts at the wrong offset', async () => {
      // A cache that keeps Range out of its key can answer with a slice from
      // another offset. Appending it would corrupt the file silently, so the
      // guard must retry the same offset instead of accepting the bytes.
      vi.useFakeTimers()
      const saved: Blob[] = []
      vi.spyOn(URL, 'createObjectURL').mockImplementation((obj: Blob | MediaSource) => {
        saved.push(obj as Blob)
        return 'blob:test'
      })
      downloadFileByPath('big.bin', 'big.bin')

      answerChunk(0, 0, CHUNK * 2, 1)

      const second = FakeXhr.instances[1]
      second.status = 206
      second.responseHeaders['Content-Range'] = `bytes 0-${CHUNK - 1}/${CHUNK * 2}`
      second.response = new Blob([new Uint8Array(CHUNK).fill(9)])
      second.onload?.()

      expect(saved).toHaveLength(0)
      await vi.advanceTimersByTimeAsync(500)
      expect(FakeXhr.instances[2].requestHeaders.Range).toBe(`bytes=${CHUNK}-${CHUNK * 2 - 1}`)
    })

    it('saves the concatenated file once the resumed transfer finishes', async () => {
      // The bytes matter, not just "the bar went away": dropping, duplicating
      // or reordering a chunk would still hide the bar and click the anchor.
      vi.useFakeTimers()
      const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
      const saved: Blob[] = []
      vi.spyOn(URL, 'createObjectURL').mockImplementation((obj: Blob | MediaSource) => {
        saved.push(obj as Blob)
        return 'blob:test'
      })
      const { downloadVisible } = useDownloadProgress()
      downloadFileByPath('big.bin', 'big.bin')

      answerChunk(0, 0, CHUNK * 2, 1)
      FakeXhr.instances[1].onerror?.()
      await vi.advanceTimersByTimeAsync(500)
      answerChunk(2, CHUNK, CHUNK * 2, 2)

      expect(downloadVisible.value).toBe(false)
      expect(clickSpy).toHaveBeenCalled()
      expect(saved).toHaveLength(1)
      const blob = saved[0]
      expect(blob.size).toBe(CHUNK * 2)
      const bytes = async (from: number, to: number) =>
        Array.from(new Uint8Array(await blob.slice(from, to).arrayBuffer()))
      // Chunk order preserved, seam intact, nothing dropped or duplicated.
      expect(await bytes(0, 4)).toEqual([1, 1, 1, 1])
      expect(await bytes(CHUNK - 4, CHUNK + 4)).toEqual([1, 1, 1, 1, 2, 2, 2, 2])
      expect(await bytes(CHUNK * 2 - 4, CHUNK * 2)).toEqual([2, 2, 2, 2])
    })

    it('treats a 200 response as the whole file when Range is ignored', () => {
      const { downloadVisible } = useDownloadProgress()
      downloadFileByPath('plain.bin', 'plain.bin')

      const xhr = FakeXhr.instances[0]
      xhr.status = 200
      xhr.response = new Blob(['whole file'])
      xhr.onload?.()

      expect(downloadVisible.value).toBe(false)
      // One request only: a 200 body is complete, so no follow-up chunk.
      expect(FakeXhr.instances).toHaveLength(1)
    })

    it('falls back to a plain request when Range is refused (zero-byte file)', () => {
      // http.ServeContent answers 416 to a Range request on an empty file;
      // that download used to work, so it must keep working.
      const { downloadVisible } = useDownloadProgress()
      downloadFileByPath('empty.txt', 'empty.txt')

      const first = FakeXhr.instances[0]
      first.status = 416
      first.onload?.()

      expect(FakeXhr.instances).toHaveLength(2)
      expect(FakeXhr.instances[1].requestHeaders.Range).toBeUndefined()

      const second = FakeXhr.instances[1]
      second.status = 200
      second.response = new Blob([])
      second.onload?.()

      expect(downloadVisible.value).toBe(false)
    })

    it('retries a 5xx response and gives up after the retry budget', async () => {
      vi.useFakeTimers()
      const { downloadVisible } = useDownloadProgress()
      downloadFileByPath('busy.bin', 'busy.bin')

      for (let i = 0; i < 4; i++) {
        const xhr = FakeXhr.instances[i]
        xhr.status = 502
        xhr.onload?.()
        if (i < 3) await vi.advanceTimersByTimeAsync(3000)
      }

      // Three retries after the first attempt, then a final failure.
      expect(FakeXhr.instances).toHaveLength(4)
      expect(downloadVisible.value).toBe(false)
    })
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
