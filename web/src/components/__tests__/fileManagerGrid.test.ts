import { describe, expect, it, beforeEach } from 'vitest'
import {
  buildThumbUrl, isImage, isAudio, isVideo, isThumbable,
  loadViewMode, saveViewMode, formatSize, VIEW_MODE_KEY, THUMBABLE_EXTS,
} from '@/utils/fileManager'

// ─── Thumbnail URL generation ───

describe('buildThumbUrl', () => {
  it('builds correct URL for root-level image', () => {
    expect(buildThumbUrl('', 'photo.png')).toBe('/api/fs/thumb?target=photo.png&w=200')
  })

  it('builds correct URL for nested image', () => {
    expect(buildThumbUrl('assets/img', 'logo.png')).toBe('/api/fs/thumb?target=assets%2Fimg%2Flogo.png&w=200')
  })

  it('respects custom width parameter', () => {
    expect(buildThumbUrl('', 'photo.jpg', 400)).toBe('/api/fs/thumb?target=photo.jpg&w=400')
  })

  it('encodes special characters in path', () => {
    expect(buildThumbUrl('my folder', 'test image.png')).toBe(
      '/api/fs/thumb?target=my%20folder%2Ftest%20image.png&w=200'
    )
  })

  it('encodes unicode characters', () => {
    expect(buildThumbUrl('图片', '截图.png')).toBe(
      '/api/fs/thumb?target=%E5%9B%BE%E7%89%87%2F%E6%88%AA%E5%9B%BE.png&w=200'
    )
  })

  it('handles deeply nested paths', () => {
    expect(buildThumbUrl('a/b/c/d', 'file.png')).toBe(
      '/api/fs/thumb?target=a%2Fb%2Fc%2Fd%2Ffile.png&w=200'
    )
  })
})

// ─── Image type detection ───

describe('isImage (file type detection)', () => {
  it('detects PNG via backend type', () => {
    expect(isImage({ type: 'image', name: 'photo.png' })).toBe(true)
  })

  it('detects JPG via getFileType', () => {
    expect(isImage({ type: 'file', name: 'photo.jpg' })).toBe(true)
  })

  it('detects SVG via getFileType', () => {
    expect(isImage({ type: 'file', name: 'logo.svg' })).toBe(true)
  })

  it('returns false for non-image files', () => {
    expect(isImage({ type: 'file', name: 'readme.md' })).toBe(false)
  })

  it('returns false for directories', () => {
    expect(isImage({ type: 'dir', name: 'images' })).toBe(false)
  })

  it('detects case-insensitive extensions', () => {
    expect(isImage({ type: 'file', name: 'photo.PNG' })).toBe(true)
    expect(isImage({ type: 'file', name: 'photo.Jpg' })).toBe(true)
  })

  it('detects WebP as image', () => {
    expect(isImage({ type: 'file', name: 'photo.webp' })).toBe(true)
  })

  it('returns false for audio files', () => {
    expect(isImage({ type: 'file', name: 'song.mp3' })).toBe(false)
  })
})

// ─── Audio type detection ───

describe('isAudio', () => {
  it('detects MP3 files', () => {
    expect(isAudio({ type: 'file', name: 'song.mp3' })).toBe(true)
  })

  it('detects WAV files', () => {
    expect(isAudio({ type: 'file', name: 'sound.wav' })).toBe(true)
  })

  it('returns false for image files', () => {
    expect(isAudio({ type: 'file', name: 'photo.png' })).toBeFalsy()
  })

  it('returns false for non-audio files', () => {
    expect(isAudio({ type: 'file', name: 'readme.md' })).toBeFalsy()
  })
})

// ─── Video type detection ───

describe('isVideo', () => {
  it('detects MP4 files', () => {
    expect(isVideo({ type: 'file', name: 'clip.mp4' })).toBe(true)
  })

  it('returns false for image files', () => {
    expect(isVideo({ type: 'file', name: 'photo.png' })).toBeFalsy()
  })
})

// ─── View mode persistence ───

describe('view mode persistence', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('defaults to list mode when nothing stored', () => {
    expect(loadViewMode()).toBe('list')
  })

  it('loads grid mode from localStorage', () => {
    localStorage.setItem(VIEW_MODE_KEY, 'grid')
    expect(loadViewMode()).toBe('grid')
  })

  it('loads list mode from localStorage', () => {
    localStorage.setItem(VIEW_MODE_KEY, 'list')
    expect(loadViewMode()).toBe('list')
  })

  it('falls back to list for invalid stored values', () => {
    localStorage.setItem(VIEW_MODE_KEY, 'invalid')
    expect(loadViewMode()).toBe('list')
  })

  it('falls back to list for empty string', () => {
    localStorage.setItem(VIEW_MODE_KEY, '')
    expect(loadViewMode()).toBe('list')
  })

  it('saves view mode to localStorage', () => {
    saveViewMode('grid')
    expect(localStorage.getItem(VIEW_MODE_KEY)).toBe('grid')
  })

  it('overwrites previous value', () => {
    saveViewMode('grid')
    saveViewMode('list')
    expect(localStorage.getItem(VIEW_MODE_KEY)).toBe('list')
  })
})

// ─── Thumbnail eligibility ───

describe('isThumbable (thumbnail eligibility)', () => {
  it('allows PNG files', () => {
    expect(isThumbable({ type: 'image', name: 'photo.png' })).toBe(true)
  })

  it('allows JPG files', () => {
    expect(isThumbable({ type: 'file', name: 'photo.jpg' })).toBe(true)
  })

  it('allows JPEG files', () => {
    expect(isThumbable({ type: 'file', name: 'photo.jpeg' })).toBe(true)
  })

  it('allows GIF files', () => {
    expect(isThumbable({ type: 'file', name: 'anim.gif' })).toBe(true)
  })

  it('excludes SVG (vector, not raster)', () => {
    expect(isThumbable({ type: 'file', name: 'logo.svg' })).toBe(false)
  })

  it('excludes WebP (not in Go stdlib decoder)', () => {
    expect(isThumbable({ type: 'file', name: 'photo.webp' })).toBe(false)
  })

  it('excludes PDF files', () => {
    expect(isThumbable({ type: 'file', name: 'doc.pdf' })).toBe(false)
  })

  it('excludes BMP files', () => {
    expect(isThumbable({ type: 'file', name: 'image.bmp' })).toBe(false)
  })

  it('excludes directories', () => {
    expect(isThumbable({ type: 'dir', name: 'images' })).toBe(false)
  })

  it('allows an unknown non-dir entry type (blacklist, not whitelist)', () => {
    // A future backend entry type must not silently lose its thumbnail.
    expect(isThumbable({ type: 'video', name: 'clip.png' })).toBe(true)
  })

  it('excludes an unknown entry type only when the extension is not thumbable', () => {
    expect(isThumbable({ type: 'video', name: 'clip.mp4' })).toBe(false)
  })

  it('excludes non-image files', () => {
    expect(isThumbable({ type: 'file', name: 'readme.md' })).toBe(false)
  })

  it('is case-insensitive', () => {
    expect(isThumbable({ type: 'image', name: 'photo.PNG' })).toBe(true)
    expect(isThumbable({ type: 'file', name: 'photo.JPG' })).toBe(true)
    expect(isThumbable({ type: 'file', name: 'photo.GIF' })).toBe(true)
  })

  it('allows any non-dir type whose extension is thumbable', () => {
    // Deliberate rule: only 'dir' is excluded. The backend never emits
    // 'symlink' (symlinks are typed dir/file/image plus a `symlink` flag), and
    // a future non-dir type must not silently lose its thumbnail.
    expect(isThumbable({ type: 'symlink', name: 'link.png' })).toBe(true)
  })

  it('matches exact extensions, not substrings', () => {
    // .png must be the extension, not just part of the name
    expect(isThumbable({ type: 'file', name: 'photo.png.bak' })).toBe(false)
  })
})

// ─── THUMBABLE_EXTS constant ───

describe('THUMBABLE_EXTS', () => {
  it('contains expected extensions', () => {
    expect(THUMBABLE_EXTS.has('.png')).toBe(true)
    expect(THUMBABLE_EXTS.has('.jpg')).toBe(true)
    expect(THUMBABLE_EXTS.has('.jpeg')).toBe(true)
    expect(THUMBABLE_EXTS.has('.gif')).toBe(true)
  })

  it('does not contain excluded extensions', () => {
    expect(THUMBABLE_EXTS.has('.svg')).toBe(false)
    expect(THUMBABLE_EXTS.has('.webp')).toBe(false)
    expect(THUMBABLE_EXTS.has('.bmp')).toBe(false)
    expect(THUMBABLE_EXTS.has('.pdf')).toBe(false)
  })
})

// ─── File size formatting ───

describe('formatSize', () => {
  it('returns empty string for null', () => {
    expect(formatSize(null)).toBe('')
  })

  it('returns empty string for undefined', () => {
    expect(formatSize(undefined)).toBe('')
  })

  it('formats bytes', () => {
    expect(formatSize(0)).toBe('0 B')
    expect(formatSize(512)).toBe('512 B')
    expect(formatSize(1023)).toBe('1023 B')
  })

  it('formats kilobytes', () => {
    expect(formatSize(1024)).toBe('1.0 K')
    expect(formatSize(1536)).toBe('1.5 K')
    expect(formatSize(1024 * 1024 - 1)).toBe('1024.0 K')
  })

  it('formats megabytes', () => {
    expect(formatSize(1024 * 1024)).toBe('1.0 M')
    expect(formatSize(1024 * 1024 * 512)).toBe('512.0 M')
    expect(formatSize(1024 * 1024 * 1024)).toBe('1024.0 M')
  })

  it('uses single-letter K/M suffix (not KB/MB)', () => {
    expect(formatSize(2048)).toContain('K')
    expect(formatSize(2048)).not.toContain('KB')
    expect(formatSize(2 * 1024 * 1024)).toContain('M')
    expect(formatSize(2 * 1024 * 1024)).not.toContain('MB')
  })

  it('handles fractional sizes with one decimal place', () => {
    expect(formatSize(1500)).toBe('1.5 K')
    expect(formatSize(1500000)).toBe('1.4 M')
  })
})
