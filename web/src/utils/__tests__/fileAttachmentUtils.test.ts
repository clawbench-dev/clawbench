import { describe, expect, it } from 'vitest'
import { normalizeFileEntry, isUploadPath, isImageFile, dedupeFiles, buildSendChannels, buildSendPayload, folderRelPath, isDirUploadFile, isUrlEntry, isSafeExternalUrl } from '@/utils/fileAttachmentUtils.ts'

describe('normalizeFileEntry', () => {
  it('normalizes string to { path, isDir: false } object', () => {
    expect(normalizeFileEntry('/foo/bar.txt')).toEqual({ path: '/foo/bar.txt', isDir: false })
  })
  it('normalizes object with path only', () => {
    expect(normalizeFileEntry({ path: '/baz/qux.go' })).toEqual({ path: '/baz/qux.go', isDir: false })
  })
  it('normalizes object with path and isDir true', () => {
    expect(normalizeFileEntry({ path: '/src', isDir: true })).toEqual({ path: '/src', isDir: true })
  })
  it('normalizes object with path and isDir false', () => {
    expect(normalizeFileEntry({ path: '/main.go', isDir: false })).toEqual({ path: '/main.go', isDir: false })
  })
  it('preserves startLine and endLine', () => {
    expect(normalizeFileEntry({ path: '/foo.ts', isDir: false, startLine: 10, endLine: 20 })).toEqual({ path: '/foo.ts', isDir: false, startLine: 10, endLine: 20 })
  })
  it('preserves startLine only', () => {
    expect(normalizeFileEntry({ path: '/bar.go', isDir: false, startLine: 5 })).toEqual({ path: '/bar.go', isDir: false, startLine: 5 })
  })
  it('string input has no line info', () => {
    const result = normalizeFileEntry('/foo.ts')
    expect(result.startLine).toBeUndefined()
    expect(result.endLine).toBeUndefined()
  })
  it('handles object with empty path', () => {
    expect(normalizeFileEntry({ path: '' })).toEqual({ path: '', isDir: false })
  })
  it('handles object with undefined path', () => {
    expect(normalizeFileEntry({ path: undefined as any })).toEqual({ path: '', isDir: false })
  })
})

describe('isUploadPath', () => {
  it('returns true for .clawbench/uploads/ path', () => {
    expect(isUploadPath('.clawbench/uploads/image.png')).toBe(true)
  })
  it('returns true for .clawbench\\uploads\\ path (Windows)', () => {
    expect(isUploadPath('.clawbench\\uploads\\image.png')).toBe(true)
  })
  it('returns false for regular path', () => {
    expect(isUploadPath('/src/main.go')).toBe(false)
  })
  it('returns false for path that contains uploads but does not start with it', () => {
    expect(isUploadPath('project/.clawbench/uploads/image.png')).toBe(false)
  })
  it('returns false for empty string', () => {
    expect(isUploadPath('')).toBe(false)
  })
})

describe('isImageFile', () => {
  it('detects .png', () => { expect(isImageFile('photo.png')).toBe(true) })
  it('detects .jpg', () => { expect(isImageFile('photo.jpg')).toBe(true) })
  it('detects .jpeg', () => { expect(isImageFile('photo.jpeg')).toBe(true) })
  it('detects .gif', () => { expect(isImageFile('anim.gif')).toBe(true) })
  it('detects .webp', () => { expect(isImageFile('photo.webp')).toBe(true) })
  it('detects .svg', () => { expect(isImageFile('icon.svg')).toBe(true) })
  it('detects .bmp', () => { expect(isImageFile('image.bmp')).toBe(true) })
  it('detects .avif', () => { expect(isImageFile('photo.avif')).toBe(true) })
  it('detects uppercase extension', () => { expect(isImageFile('photo.PNG')).toBe(true) })
  it('detects mixed case extension', () => { expect(isImageFile('photo.JpG')).toBe(true) })
  it('returns false for non-image extension', () => { expect(isImageFile('main.go')).toBe(false) })
  it('returns false for .txt', () => { expect(isImageFile('readme.txt')).toBe(false) })
  it('returns false for null', () => { expect(isImageFile(null)).toBe(false) })
  it('returns false for undefined', () => { expect(isImageFile(undefined)).toBe(false) })
  it('returns false for empty string', () => { expect(isImageFile('')).toBe(false) })
  it('returns false for path without extension', () => { expect(isImageFile('/path/to/file')).toBe(false) })
  it('handles .ico', () => { expect(isImageFile('favicon.ico')).toBe(true) })
  it('handles .tiff', () => { expect(isImageFile('scan.tiff')).toBe(true) })
  it('handles .tif', () => { expect(isImageFile('scan.tif')).toBe(true) })
})

describe('dedupeFiles', () => {
  it('returns empty array for empty input', () => {
    expect(dedupeFiles([])).toEqual([])
  })

  it('returns same array when no duplicates', () => {
    const files = [
      { path: '/a.go', isDir: false },
      { path: '/b.go', isDir: false },
    ]
    expect(dedupeFiles(files)).toEqual(files)
  })

  it('removes duplicate paths keeping first occurrence', () => {
    const files = [
      { path: '/a.go', isDir: false },
      { path: '/b.go', isDir: false },
      { path: '/a.go', isDir: false },
    ]
    expect(dedupeFiles(files)).toEqual([
      { path: '/a.go', isDir: false },
      { path: '/b.go', isDir: false },
    ])
  })

  it('keeps distinct line ranges of one file separate', () => {
    const files = [
      { path: '/a.go', isDir: false },
      { path: '/a.go', isDir: false, startLine: 5, endLine: 15 },
      { path: '/a.go', isDir: false, startLine: 20, endLine: 30 },
    ]
    expect(dedupeFiles(files)).toEqual([
      { path: '/a.go', isDir: false },
      { path: '/a.go', isDir: false, startLine: 5, endLine: 15 },
      { path: '/a.go', isDir: false, startLine: 20, endLine: 30 },
    ])
  })

  it('collapses exact duplicates (same path and range)', () => {
    const files = [
      { path: '/a.go', isDir: false, startLine: 10, endLine: 20 },
      { path: '/a.go', isDir: false, startLine: 10, endLine: 20 },
    ]
    expect(dedupeFiles(files)).toEqual([
      { path: '/a.go', isDir: false, startLine: 10, endLine: 20 },
    ])
  })

  it('normalizes string entries', () => {
    expect(dedupeFiles(['/a.go' as unknown as { path: string }])).toEqual([
      { path: '/a.go', isDir: false },
    ])
  })
})

describe('buildSendChannels', () => {
  it('sends plain files through filePaths', () => {
    const { filePaths, entries } = buildSendChannels([
      { path: '/a.go', isDir: false },
      { path: '/b.go', isDir: false },
    ])
    expect(filePaths).toEqual(['/a.go', '/b.go'])
    expect(entries).toEqual([])
  })

  it('routes every entry of a ranged path through entries and excludes the path from filePaths', () => {
    const { filePaths, entries } = buildSendChannels([
      { path: '/md/guide.md', isDir: false },                               // whole-file attach
      { path: '/md/guide.md', isDir: false, startLine: 5, endLine: 15 },    // diagram range
      { path: '/md/other.md', isDir: false },
    ])
    // guide.md carries a range → ALL its entries go through entries channel,
    // and guide.md never appears in filePaths (backend would strip the ranged
    // entry otherwise).
    expect(filePaths).toEqual(['/md/other.md'])
    expect(entries).toEqual([
      { path: '/md/guide.md', isDir: false },
      { path: '/md/guide.md', isDir: false, startLine: 5, endLine: 15 },
    ])
  })

  it('keeps multiple distinct ranges of one path together in entries', () => {
    const { filePaths, entries } = buildSendChannels([
      { path: '/md/guide.md', isDir: false, startLine: 5, endLine: 15 },
      { path: '/md/guide.md', isDir: false, startLine: 30, endLine: 40 },
    ])
    expect(filePaths).toEqual([])
    expect(entries).toHaveLength(2)
  })
})

describe('folderRelPath', () => {
  it('returns the full directory portion including top-level folder', () => {
    expect(folderRelPath({ webkitRelativePath: 'src/utils/helper.ts' })).toBe('src/utils')
  })
  it('returns the single top-level folder for a file at folder root', () => {
    expect(folderRelPath({ webkitRelativePath: 'src/helper.ts' })).toBe('src')
  })
  it('handles deeply nested paths', () => {
    expect(folderRelPath({ webkitRelativePath: 'a/b/c/d/file.txt' })).toBe('a/b/c/d')
  })
  it('normalizes backslash separators (Windows)', () => {
    expect(folderRelPath({ webkitRelativePath: 'src\\utils\\helper.ts' })).toBe('src/utils')
  })
  it('returns empty string for a file without webkitRelativePath (loose drop)', () => {
    expect(folderRelPath({ name: 'file.txt' })).toBe('')
  })
  it('returns empty string for empty webkitRelativePath', () => {
    expect(folderRelPath({ webkitRelativePath: '' })).toBe('')
  })
  it('returns empty string for missing webkitRelativePath', () => {
    expect(folderRelPath({})).toBe('')
  })
  it('returns empty string for null/undefined file', () => {
    expect(folderRelPath(null as any)).toBe('')
    expect(folderRelPath(undefined as any)).toBe('')
  })
})

describe('isDirUploadFile', () => {
  it('returns true for a file that belongs to a folder', () => {
    expect(isDirUploadFile({ webkitRelativePath: 'src/main.go' })).toBe(true)
  })
  it('returns false for a loose file', () => {
    expect(isDirUploadFile({ name: 'main.go' })).toBe(false)
  })
})

// ── URL attachments (GitHub issue / PR references) ──

describe('URL attachments', () => {
  it('normalizeFileEntry preserves kind and url', () => {
    const entry = { path: 'acme/widgets#123', kind: 'url' as const, url: 'https://github.com/acme/widgets/issues/123' }
    const norm = normalizeFileEntry(entry)
    expect(norm.kind).toBe('url')
    expect(norm.url).toBe('https://github.com/acme/widgets/issues/123')
    expect(norm.path).toBe('acme/widgets#123')
  })

  it('isUrlEntry is true only for url kind with an address', () => {
    expect(isUrlEntry({ path: 'x', kind: 'url', url: 'https://x' })).toBe(true)
    expect(isUrlEntry({ path: 'x', kind: 'url' })).toBe(false)
    expect(isUrlEntry({ path: '/local/file.ts' })).toBe(false)
  })

  it('isSafeExternalUrl allows only http(s) and rejects executable schemes', () => {
    // URL entries are persisted then re-rendered into an anchor href, so the
    // address is data: a javascript:/data: URL must never become a live link.
    expect(isSafeExternalUrl('https://github.com/o/r/issues/1')).toBe(true)
    expect(isSafeExternalUrl('http://example.com/x')).toBe(true)
    expect(isSafeExternalUrl('javascript:alert(1)')).toBe(false)
    expect(isSafeExternalUrl('data:text/html,<script>alert(1)</script>')).toBe(false)
    expect(isSafeExternalUrl('file:///etc/passwd')).toBe(false)
    expect(isSafeExternalUrl('')).toBe(false)
    expect(isSafeExternalUrl(undefined)).toBe(false)
  })

  it('dedupeFiles dedupes URLs by address, not label', () => {
    const a = { path: 'label-a', kind: 'url' as const, url: 'https://github.com/o/r/issues/1' }
    const b = { path: 'label-b', kind: 'url' as const, url: 'https://github.com/o/r/issues/1' }
    const result = dedupeFiles([a, b])
    expect(result).toHaveLength(1)
    expect(result[0].url).toBe('https://github.com/o/r/issues/1')
  })

  it('dedupeFiles keeps distinct URLs', () => {
    const a = { path: 'a', kind: 'url' as const, url: 'https://github.com/o/r/issues/1' }
    const b = { path: 'b', kind: 'url' as const, url: 'https://github.com/o/r/issues/2' }
    expect(dedupeFiles([a, b])).toHaveLength(2)
  })

  it('buildSendChannels routes URL entries through entries, never filePaths', () => {
    const url = { path: 'acme/widgets#9', kind: 'url' as const, url: 'https://github.com/acme/widgets/pull/9' }
    const file = { path: '/src/main.go' }
    const { filePaths, entries } = buildSendChannels([url, file])
    // A URL must never be treated as a filesystem path.
    expect(filePaths).toEqual(['/src/main.go'])
    expect(entries).toHaveLength(1)
    expect(entries[0].kind).toBe('url')
    expect(entries[0].url).toBe('https://github.com/acme/widgets/pull/9')
  })
})

describe('buildSendPayload', () => {
  it('preserves kind/url on a URL attachment end-to-end', () => {
    // Regression: the send path used to rebuild entries as
    // {path,isDir,startLine,endLine}, dropping kind/url. The backend then
    // treated the URL as a local path and rejected the send with 404.
    const url = { path: 'acme/widgets#9', kind: 'url' as const, url: 'https://github.com/acme/widgets/pull/9' }
    const { allFiles, filePaths } = buildSendPayload([], [url])

    const urlEntry = allFiles.find(f => f.url === 'https://github.com/acme/widgets/pull/9')
    expect(urlEntry).toBeDefined()
    expect(urlEntry?.kind).toBe('url')
    // A URL must never be sent as a filesystem path.
    expect(filePaths).not.toContain('acme/widgets#9')
    expect(filePaths).toHaveLength(0)
  })

  it('keeps a line-range attachment on the entries channel only', () => {
    const ranged = { path: '/src/main.go', startLine: 10, endLine: 20 }
    const { allFiles, filePaths } = buildSendPayload([], [ranged])

    expect(filePaths).not.toContain('/src/main.go')
    expect(allFiles.some(f => f.path === '/src/main.go' && f.startLine === 10)).toBe(true)
  })

  it('sends a plain project file through filePaths', () => {
    const plain = { path: '/src/app.go' }
    const { filePaths } = buildSendPayload([], [plain])
    expect(filePaths).toEqual(['/src/app.go'])
  })

  it('merges uploads and attachments, deduping by entry key', () => {
    const uploaded = [{ path: '/src/dup.go' }]
    const attached = [{ path: '/src/dup.go' }]
    const { allFiles, filePaths } = buildSendPayload(uploaded, attached)
    expect(filePaths).toEqual(['/src/dup.go'])
    expect(allFiles).toHaveLength(1)
  })
})
