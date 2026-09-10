import { describe, expect, it } from 'vitest'
import { getFileType, formatFileSize } from '@/utils/fileType.ts'

describe('getFileType', () => {
  it('detects Go files', () => {
    const ft = getFileType('main.go')
    expect(ft.lang).toBe('go')
    expect(ft.label).toBe('Go')
    expect(ft.isMarkdown).toBe(false)
  })

  it('detects TypeScript files', () => {
    const ft = getFileType('app.ts')
    expect(ft.lang).toBe('typescript')
    expect(ft.label).toBe('TS')
  })

  it('detects markdown files', () => {
    const ft = getFileType('README.md')
    expect(ft.lang).toBe('markdown')
    expect(ft.isMarkdown).toBe(true)
  })

  it('detects PNG images', () => {
    const ft = getFileType('screenshot.png')
    expect(ft.isImage).toBe(true)
    expect(ft.lang).toBe('image')
  })

  it('detects JPG images (case insensitive)', () => {
    const ft = getFileType('photo.JPG')
    expect(ft.isImage).toBe(true)
  })

  it('detects MP3 audio', () => {
    const ft = getFileType('song.mp3')
    expect(ft.isAudio).toBe(true)
  })

  it('detects MP4 video', () => {
    const ft = getFileType('clip.mp4')
    expect(ft.isVideo).toBe(true)
  })

  it('detects PDF files', () => {
    const ft = getFileType('doc.pdf')
    expect(ft.isPdf).toBe(true)
    expect(ft.isImage).toBeFalsy()
  })

  it('detects YAML files', () => {
    const ft = getFileType('config.yaml')
    expect(ft.lang).toBe('yaml')
  })

  it('detects JSON files', () => {
    const ft = getFileType('package.json')
    expect(ft.lang).toBe('json')
  })

  it('detects Vue files', () => {
    const ft = getFileType('App.vue')
    expect(ft.lang).toBe('vue')
  })

  it('returns plaintext for unknown extensions', () => {
    const ft = getFileType('data.xyz')
    expect(ft.lang).toBe('plaintext')
    expect(ft.label).toBe('TXT')
  })

  it('returns plaintext for files with no extension', () => {
    const ft = getFileType('Makefile')
    expect(ft.lang).toBe('plaintext')
  })

  it('detects Dockerfile by extension', () => {
    const ft = getFileType('Dockerfile.dockerfile')
    expect(ft.lang).toBe('dockerfile')
  })

  it('handles files with multiple dots', () => {
    const ft = getFileType('test.spec.ts')
    expect(ft.lang).toBe('typescript')
  })

  it('detects shell scripts', () => {
    const ft = getFileType('deploy.sh')
    expect(ft.lang).toBe('bash')
  })

  it('detects SQL files', () => {
    const ft = getFileType('query.sql')
    expect(ft.lang).toBe('sql')
  })

  it('detects .yml extension as yaml', () => {
    const ft = getFileType('docker-compose.yml')
    expect(ft.lang).toBe('yaml')
  })

  it('detects .tsx as typescript', () => {
    const ft = getFileType('Component.tsx')
    expect(ft.lang).toBe('typescript')
  })

  it('detects .jsx as javascript', () => {
    const ft = getFileType('Component.jsx')
    expect(ft.lang).toBe('javascript')
  })

  it('detects Rust files', () => {
    const ft = getFileType('main.rs')
    expect(ft.lang).toBe('rust')
  })

  it('detects Python files', () => {
    const ft = getFileType('app.py')
    expect(ft.lang).toBe('python')
  })

  it('detects .svg as image', () => {
    const ft = getFileType('logo.svg')
    expect(ft.isImage).toBe(true)
  })

  it('detects .webm as video', () => {
    const ft = getFileType('video.webm')
    expect(ft.isVideo).toBe(true)
  })

  it('detects .flac as audio', () => {
    const ft = getFileType('music.flac')
    expect(ft.isAudio).toBe(true)
  })

  it('detects .diff extension', () => {
    const ft = getFileType('changes.diff')
    expect(ft.lang).toBe('diff')
  })

  it('detects .toml extension', () => {
    const ft = getFileType('pyproject.toml')
    expect(ft.lang).toBe('toml')
  })

  it('detects .graphql extension', () => {
    const ft = getFileType('schema.graphql')
    expect(ft.lang).toBe('graphql')
  })

  it('is case-insensitive for all extensions', () => {
    expect(getFileType('main.GO').lang).toBe('go')
    expect(getFileType('readme.MD').lang).toBe('markdown')
    expect(getFileType('app.TS').lang).toBe('typescript')
  })
})

describe('formatFileSize', () => {
  it('formats bytes', () => {
    expect(formatFileSize(0)).toBe('0 B')
  })

  it('formats small bytes', () => {
    expect(formatFileSize(512)).toBe('512 B')
  })

  it('formats kilobytes', () => {
    expect(formatFileSize(1024)).toBe('1.0 KB')
  })

  it('formats large kilobytes', () => {
    expect(formatFileSize(512 * 1024)).toBe('512.0 KB')
  })

  it('formats megabytes', () => {
    expect(formatFileSize(1024 * 1024)).toBe('1.0 MB')
  })

  it('formats large megabytes', () => {
    expect(formatFileSize(50 * 1024 * 1024)).toBe('50.0 MB')
  })

  it('formats multi-GB sizes instead of thousands of MB', () => {
    // RAG indexes on a large store can exceed 1 GB; must not render as "2861.0 MB".
    expect(formatFileSize(2861 * 1024 * 1024)).toBe('2.8 GB')
  })

  it('formats fractional KB', () => {
    expect(formatFileSize(1536)).toBe('1.5 KB')
  })

  it('formats 1 byte', () => {
    expect(formatFileSize(1)).toBe('1 B')
  })

  it('formats boundary between B and KB', () => {
    expect(formatFileSize(1023)).toBe('1023 B')
  })

  it('formats boundary between KB and MB', () => {
    expect(formatFileSize(1024 * 1024 - 1)).toBe('1024.0 KB')
  })

  it('rolls over to GB at 1 GiB instead of showing 1024.0 MB', () => {
    expect(formatFileSize(1024 * 1024 * 1024)).toBe('1.0 GB')
  })

  it('formats boundary between MB and GB', () => {
    expect(formatFileSize(1024 * 1024 * 1024 - 1)).toBe('1024.0 MB')
  })
})
