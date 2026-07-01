import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { buildLocalFileUrl, downloadFileByPath, downloadBlob } from '../download'

describe('buildLocalFileUrl', () => {
  it('encodes path segments', () => {
    expect(buildLocalFileUrl('img/logo.png')).toBe('/api/local-file/img/logo.png')
  })

  it('encodes special characters', () => {
    expect(buildLocalFileUrl('my file.txt')).toBe('/api/local-file/my%20file.txt')
  })

  it('adds download param', () => {
    expect(buildLocalFileUrl('file.txt', { download: true })).toBe('/api/local-file/file.txt?download=1')
  })

  it('adds timestamp param', () => {
    const url = buildLocalFileUrl('file.txt', { timestamp: true })
    expect(url).toContain('t=')
    expect(url).toContain('/api/local-file/file.txt?')
  })

  it('adds both params', () => {
    const url = buildLocalFileUrl('file.txt', { download: true, timestamp: true })
    expect(url).toContain('download=1')
    expect(url).toContain('t=')
  })

  it('handles CJK path', () => {
    expect(buildLocalFileUrl('日本語/文件.txt')).toBe('/api/local-file/%E6%97%A5%E6%9C%AC%E8%AA%9E/%E6%96%87%E4%BB%B6.txt')
  })
})

describe('downloadFileByPath', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns early for empty path', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild')
    downloadFileByPath('')
    expect(appendSpy).not.toHaveBeenCalled()
    appendSpy.mockRestore()
  })

  it('uses AndroidNative bridge when available', () => {
    const mockDownload = vi.fn()
    ;(window as any).AndroidNative = { downloadFile: mockDownload }
    downloadFileByPath('img/logo.png')
    expect(mockDownload).toHaveBeenCalledWith('img/logo.png')
    delete (window as any).AndroidNative
  })

  it('creates anchor element for web download', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    const removeSpy = vi.spyOn(document.body, 'removeChild').mockImplementation((el) => el)
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    downloadFileByPath('img/logo.png')

    expect(appendSpy).toHaveBeenCalled()
    const anchor = appendSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.href).toContain('img/logo.png')
    expect(anchor.download).toBe('logo.png')

    vi.advanceTimersByTime(1500)
    expect(removeSpy).toHaveBeenCalled()

    appendSpy.mockRestore()
    removeSpy.mockRestore()
    clickSpy.mockRestore()
  })

  it('uses provided fileName', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    const removeSpy = vi.spyOn(document.body, 'removeChild').mockImplementation((el) => el)
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    downloadFileByPath('img/logo.png', 'custom.png')

    const anchor = appendSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.download).toBe('custom.png')

    vi.advanceTimersByTime(1500)
    appendSpy.mockRestore()
    removeSpy.mockRestore()
    clickSpy.mockRestore()
  })
})

describe('downloadBlob', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('creates blob URL for web download', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    const removeSpy = vi.spyOn(document.body, 'removeChild').mockImplementation((el) => el)
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    const createUrlSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test')
    const revokeUrlSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})

    downloadBlob('hello world', 'test.txt', 'text/plain')

    expect(createUrlSpy).toHaveBeenCalled()
    expect(appendSpy).toHaveBeenCalled()
    const anchor = appendSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.download).toBe('test.txt')

    vi.advanceTimersByTime(1500)
    expect(removeSpy).toHaveBeenCalled()
    expect(revokeUrlSpy).toHaveBeenCalled()

    appendSpy.mockRestore()
    removeSpy.mockRestore()
    clickSpy.mockRestore()
    createUrlSpy.mockRestore()
    revokeUrlSpy.mockRestore()
  })

  it('uses AndroidNative.downloadBlob when available', async () => {
    const mockDownloadBlob = vi.fn()
    ;(window as any).AndroidNative = { downloadBlob: mockDownloadBlob }

    downloadBlob('hello world', 'test.txt', 'text/plain')

    // Wait for FileReader to read the blob as data URL
    await vi.waitFor(() => {
      expect(mockDownloadBlob).toHaveBeenCalled()
    })

    // The base64 content should be passed (after the comma in data URL)
    expect(mockDownloadBlob).toHaveBeenCalledWith(expect.any(String), 'test.txt')

    delete (window as any).AndroidNative
  })

  it('does not create blob URL when AndroidNative is available', async () => {
    const mockDownloadBlob = vi.fn()
    ;(window as any).AndroidNative = { downloadBlob: mockDownloadBlob }
    const createUrlSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:test')

    downloadBlob('hello world', 'test.txt', 'text/plain')

    await vi.waitFor(() => {
      expect(mockDownloadBlob).toHaveBeenCalled()
    })

    expect(createUrlSpy).not.toHaveBeenCalled()

    createUrlSpy.mockRestore()
    delete (window as any).AndroidNative
  })
})

describe('downloadFileByPath - edge cases', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns early for null/undefined path', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild')
    downloadFileByPath(null as any)
    expect(appendSpy).not.toHaveBeenCalled()
    appendSpy.mockRestore()
  })

  it('extracts filename from path with multiple segments', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    const removeSpy = vi.spyOn(document.body, 'removeChild').mockImplementation((el) => el)
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    downloadFileByPath('a/b/c/file.txt')

    const anchor = appendSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.download).toBe('file.txt')

    vi.advanceTimersByTime(1500)
    appendSpy.mockRestore()
    removeSpy.mockRestore()
    clickSpy.mockRestore()
  })

  it('uses download URL with ?download=1', () => {
    const appendSpy = vi.spyOn(document.body, 'appendChild').mockImplementation((el) => el)
    const removeSpy = vi.spyOn(document.body, 'removeChild').mockImplementation((el) => el)
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    downloadFileByPath('file.txt')

    const anchor = appendSpy.mock.calls[0][0] as HTMLAnchorElement
    expect(anchor.href).toContain('download=1')

    vi.advanceTimersByTime(1500)
    appendSpy.mockRestore()
    removeSpy.mockRestore()
    clickSpy.mockRestore()
  })
})
