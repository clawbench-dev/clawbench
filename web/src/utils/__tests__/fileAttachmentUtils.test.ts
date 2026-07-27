import { describe, expect, it } from 'vitest'
import { normalizeFileEntry, isUploadPath, isImageFile } from '@/utils/fileAttachmentUtils.ts'

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
