import { describe, expect, it, vi, afterEach } from 'vitest'
import { buildLocalFileUrl, downloadFileByPath, downloadByUrl } from '@/utils/download.ts'

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
      '/api/local-file/foo/bar%20baz/file.pdf'
    )
  })

  it('adds download=1 query param', () => {
    expect(buildLocalFileUrl('doc.pdf', { download: true })).toBe(
      '/api/local-file/doc.pdf?download=1'
    )
  })

  it('adds timestamp query param', () => {
    const url = buildLocalFileUrl('test.txt', { timestamp: true })
    expect(url).toMatch(/\/api\/local-file\/test\.txt\?t=\d+/)
  })

  it('combines download and timestamp params', () => {
    const url = buildLocalFileUrl('file.pdf', { download: true, timestamp: true })
    expect(url).toContain('download=1')
    expect(url).toMatch(/t=\d+/)
  })

  it('handles simple filename without slashes', () => {
    expect(buildLocalFileUrl('readme.md')).toBe('/api/local-file/readme.md')
  })

  it('uses ?path= query param for absolute paths', () => {
    const url = buildLocalFileUrl('/home/user/docs/report.pdf')
    expect(url).toBe('/api/local-file/?path=%2Fhome%2Fuser%2Fdocs%2Freport.pdf')
  })

  it('uses ?path= query param for absolute paths with download', () => {
    const url = buildLocalFileUrl('/tmp/data.csv', { download: true })
    expect(url).toBe('/api/local-file/?download=1&path=%2Ftmp%2Fdata.csv')
  })

  it('uses ?path= query param for absolute paths with timestamp', () => {
    const url = buildLocalFileUrl('/var/log/syslog', { timestamp: true })
    expect(url).toMatch(/\/api\/local-file\/\?t=\d+&path=%2Fvar%2Flog%2Fsyslog/)
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
    expect(anchor.href).toContain('/api/local-file/test.pdf')
    expect(anchor.href).toContain('download=1')
    expect(anchor.download).toBe('test.pdf')

    appendChildSpy.mockRestore()
    // Clean up the anchor if still in the DOM
    anchor.remove()
  })

  it('creates anchor element with ?path= for absolute paths', () => {
    const appendChildSpy = vi.spyOn(document.body, 'appendChild')
    downloadFileByPath('/home/user/docs/report.pdf')

    expect(appendChildSpy).toHaveBeenCalled()
    const anchor = appendChildSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.href).toContain('/api/local-file/')
    expect(anchor.href).toContain('path=')
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

  it('calls AndroidNative.downloadUrl in app mode', () => {
    const mockDownloadUrl = vi.fn()
    ;(window as any).AndroidNative = { downloadUrl: mockDownloadUrl }

    downloadByUrl('/api/apk', 'clawbench-android.apk')
    expect(mockDownloadUrl).toHaveBeenCalledWith('/api/apk', 'clawbench-android.apk')

    delete (window as any).AndroidNative
  })
})
