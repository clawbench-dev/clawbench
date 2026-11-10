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

  // The query forms carry the real path in a parameter; their own pathname has
  // no usable last segment (`/api/fs/raw/` or `thumb`), which used to yield an
  // empty / extension-less name and an extension-less download.
  it('extracts the basename from the ?target= absolute form', () => {
    const result = extractImageName('/api/fs/raw/?target=%2Ftmp%2Fout%2Fchart.png')
    expect(result).toBe('chart.png')
  })

  it('extracts the basename from the thumbnail endpoint', () => {
    const result = extractImageName('/api/fs/thumb?target=docs%2Fimg%2Farch.png&w=400')
    expect(result).toBe('arch.png')
  })

  it('extracts the basename from the share ?path= form', () => {
    const result = extractImageName('/api/share/abc123/local?path=%2Fhome%2Fu%2Fa.jpg')
    expect(result).toBe('a.jpg')
  })

  it('does not double-decode a literal percent in the target', () => {
    const result = extractImageName('/api/fs/raw/?target=%2Ftmp%2F100%2525.png')
    expect(result).toBe('100%25.png')
  })
})
