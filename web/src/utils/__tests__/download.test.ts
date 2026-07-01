import { describe, expect, it, vi } from 'vitest'
import { buildLocalFileUrl, downloadFileByPath } from '@/utils/download.ts'

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
})
