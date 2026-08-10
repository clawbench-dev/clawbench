import { describe, expect, it } from 'vitest'
import { normalizeFileEntry, isUploadPath, isImageFile, dedupeFiles, folderRelPath, isDirUploadFile } from '@/utils/fileAttachmentUtils.ts'

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

  it('prefers entry with line-range metadata over simpler entry', () => {
    const files = [
      { path: '/a.go', isDir: false },                                    // simple (no line info)
      { path: '/b.go', isDir: false },
      { path: '/a.go', isDir: false, startLine: 10, endLine: 20 },       // richer (has line info)
    ]
    expect(dedupeFiles(files)).toEqual([
      { path: '/a.go', isDir: false, startLine: 10, endLine: 20 },
      { path: '/b.go', isDir: false },
    ])
  })

  it('keeps simpler entry when richer entry comes first', () => {
    // If the first occurrence already has line info, keep it
    const files = [
      { path: '/a.go', isDir: false, startLine: 5, endLine: 15 },
      { path: '/a.go', isDir: false },  // simpler, later — not replaced
    ]
    expect(dedupeFiles(files)).toEqual([
      { path: '/a.go', isDir: false, startLine: 5, endLine: 15 },
    ])
  })

  it('does not replace when both entries have line info', () => {
    const files = [
      { path: '/a.go', isDir: false, startLine: 1, endLine: 10 },
      { path: '/a.go', isDir: false, startLine: 20, endLine: 30 },
    ]
    // Keeps the first entry (already has line info)
    expect(dedupeFiles(files)).toEqual([
      { path: '/a.go', isDir: false, startLine: 1, endLine: 10 },
    ])
  })

  it('handles mixed uploaded + project files dedup', () => {
    // Simulates sendMessage merge: uploaded file (no line info) + project file (with line info)
    const uploaded = [{ path: '.clawbench/uploads/img.png', isDir: false }]
    const project = [{ path: '/src/main.go', isDir: false, startLine: 42, endLine: 50 }]
    // No overlap — both kept
    expect(dedupeFiles([...uploaded, ...project])).toEqual([...uploaded, ...project])
  })

  it('dedupes auto-attached upload path that also appears as project reference', () => {
    // A file auto-attached from upload AND manually attached as project reference
    const uploaded = [{ path: '/src/main.go', isDir: false }]                          // from pendingFiles (no line info)
    const project = [{ path: '/src/main.go', isDir: false, startLine: 10, endLine: 20 }] // from attachedFiles (has line info)
    expect(dedupeFiles([...uploaded, ...project])).toEqual([
      { path: '/src/main.go', isDir: false, startLine: 10, endLine: 20 },
    ])
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
