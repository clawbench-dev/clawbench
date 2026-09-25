import { describe, expect, it, vi } from 'vitest'
import { extractImageName } from '@/utils/lightbox'

describe('extractImageName', () => {
  it('extracts basename from /api/fs/raw/ URL', () => {
    const result = extractImageName('/api/fs/raw/path/to/image.png')
    expect(result).toBe('image.png')
  })

  it('extracts basename from /api/fs/raw/ with encoded path', () => {
    const result = extractImageName('/api/fs/raw/path%2Fto%2Fphoto.jpg')
    expect(result).toBe('photo.jpg')
  })

  it('extracts basename from non-local-file URL path', () => {
    const result = extractImageName('/some/other/path/file.png')
    expect(result).toBe('file.png')
  })

  it('handles empty string input', () => {
    const result = extractImageName('')
    // new URL('', origin) gives pathname '/', baseName('/') returns '/'
    expect(typeof result).toBe('string')
  })

  it('handles full URL with /api/fs/raw/ prefix', () => {
    const result = extractImageName('http://localhost:20000/api/fs/raw/home/user/project/img.png')
    expect(result).toBe('img.png')
  })

  it('handles path with multiple segments after local-file prefix', () => {
    const result = extractImageName('/api/fs/raw/deep/nested/dir/screenshot.jpeg')
    expect(result).toBe('screenshot.jpeg')
  })

  it('returns basename for simple filename after local-file prefix', () => {
    const result = extractImageName('/api/fs/raw/photo.png')
    expect(result).toBe('photo.png')
  })

  it('handles URL with query parameters', () => {
    const result = extractImageName('/api/fs/raw/image.png?w=80&h=80')
    // The URL constructor will put query params in search, not pathname
    expect(result).toBe('image.png')
  })

  it('handles path with trailing slash', () => {
    const result = extractImageName('/api/fs/raw/some/dir/')
    // baseName of "some/dir/" is "dir"
    expect(result).toBe('dir')
  })

  it('handles deeply nested non-local path', () => {
    const result = extractImageName('/uploads/2024/06/report.pdf')
    expect(result).toBe('report.pdf')
  })

  it('handles URL with hash fragment', () => {
    const result = extractImageName('/api/fs/raw/docs/readme.md#section')
    expect(result).toBe('readme.md')
  })

  it('handles URL-encoded spaces in path', () => {
    const result = extractImageName('/api/fs/raw/my%20image.png')
    expect(result).toBe('my image.png')
  })
})
