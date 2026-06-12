import { describe, expect, it } from 'vitest'
import { getFileType, formatFileSize } from '@/utils/fileType.ts'

describe('getFileType', () => {
  it('detects TypeScript', () => {
    const ft = getFileType('app.ts')
    expect(ft.lang).toBe('typescript')
    expect(ft.label).toBe('TS')
  })

  it('detects Python', () => {
    const ft = getFileType('main.py')
    expect(ft.lang).toBe('python')
  })

  it('detects Go', () => {
    const ft = getFileType('server.go')
    expect(ft.lang).toBe('go')
  })

  it('detects Markdown', () => {
    const ft = getFileType('README.md')
    expect(ft.lang).toBe('markdown')
    expect(ft.isMarkdown).toBe(true)
  })

  it('detects JSON', () => {
    const ft = getFileType('package.json')
    expect(ft.lang).toBe('json')
  })

  it('detects HTML', () => {
    const ft = getFileType('index.html')
    expect(ft.isHtml).toBe(true)
  })

  it('detects PNG images', () => {
    const ft = getFileType('photo.png')
    expect(ft.isImage).toBe(true)
  })

  it('detects PDF', () => {
    const ft = getFileType('doc.pdf')
    expect(ft.isPdf).toBe(true)
  })

  it('detects MP3 audio', () => {
    const ft = getFileType('song.mp3')
    expect(ft.isAudio).toBe(true)
  })

  it('detects MP4 video', () => {
    const ft = getFileType('movie.mp4')
    expect(ft.isVideo).toBe(true)
  })

  it('detects Shell', () => {
    const ft = getFileType('script.sh')
    expect(ft.lang).toBe('bash')
  })

  it('returns plaintext for unknown extensions', () => {
    const ft = getFileType('data.xyz')
    expect(ft.lang).toBe('plaintext')
  })

  it('handles case-insensitive matching', () => {
    const ft = getFileType('APP.TS')
    expect(ft.lang).toBe('typescript')
  })

  it('detects YAML', () => {
    expect(getFileType('config.yaml').lang).toBe('yaml')
    expect(getFileType('config.yml').lang).toBe('yaml')
  })

  it('detects Dockerfile', () => {
    expect(getFileType('Dockerfile').lang).toBe('dockerfile')
  })

  it('detects CSS', () => {
    expect(getFileType('style.css').lang).toBe('css')
  })

  it('detects Vue', () => {
    expect(getFileType('App.vue').lang).toBe('vue')
  })

  it('detects Rust', () => {
    expect(getFileType('main.rs').lang).toBe('rust')
  })

  it('detects SQL', () => {
    expect(getFileType('query.sql').lang).toBe('sql')
  })

  it('detects PDF by extension', () => {
    const ft = getFileType('document.pdf')
    expect(ft.isPdf).toBe(true)
    expect(ft.lang).toBe('pdf')
  })
})

describe('formatFileSize', () => {
  it('formats bytes', () => {
    expect(formatFileSize(500)).toBe('500 B')
  })

  it('formats kilobytes', () => {
    expect(formatFileSize(2048)).toBe('2.0 KB')
  })

  it('formats megabytes', () => {
    expect(formatFileSize(2 * 1024 * 1024)).toBe('2.0 MB')
  })
})
