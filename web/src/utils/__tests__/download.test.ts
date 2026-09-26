import { describe, expect, it, vi, afterEach } from 'vitest'
import { buildLocalFileUrl, downloadFileByPath, downloadByUrl } from '@/utils/download.ts'
import { setShareToken } from '@/share/shareMode'

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

describe('downloadFileByPath', () => {
  it('does nothing for empty path', () => {
    // Should not throw or create any DOM elements
    downloadFileByPath('')
    // No assertion needed — just verifying no crash
  })

  it('creates anchor element with correct href for web mode', () => {
    const appendChildSpy = vi.spyOn(document.body, 'appendChild')
    downloadFileByPath('test.pdf')

    expect(appendChildSpy).toHaveBeenCalled()
    const anchor = appendChildSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.href).toContain('/api/fs/raw/test.pdf')
    expect(anchor.href).toContain('download=1')
    expect(anchor.download).toBe('test.pdf')

    appendChildSpy.mockRestore()
    // Clean up the anchor if still in the DOM
    anchor.remove()
  })

  it('creates anchor element with ?target= for absolute paths', () => {
    const appendChildSpy = vi.spyOn(document.body, 'appendChild')
    downloadFileByPath('/home/user/docs/report.pdf')

    expect(appendChildSpy).toHaveBeenCalled()
    const anchor = appendChildSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.href).toContain('/api/fs/raw/')
    expect(anchor.href).toContain('target=')
    expect(anchor.download).toBe('report.pdf')

    appendChildSpy.mockRestore()
    anchor.remove()
  })
})

describe('downloadByUrl', () => {
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

    delete (window as any).ClawBenchNative
  })
})
