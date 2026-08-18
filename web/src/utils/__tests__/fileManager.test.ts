import { describe, expect, it } from 'vitest'
import { numberedName } from '@/utils/fileManager.ts'

describe('numberedName', () => {
  it('appends a numeric suffix before the extension', () => {
    expect(numberedName('report.txt', 1)).toBe('report_1.txt')
    expect(numberedName('report.txt', 2)).toBe('report_2.txt')
  })

  it('handles names with multiple dots (uses last dot as extension)', () => {
    expect(numberedName('archive.tar.gz', 1)).toBe('archive.tar_1.gz')
  })

  it('handles names without an extension', () => {
    expect(numberedName('notes', 1)).toBe('notes_1')
    expect(numberedName('notes', 3)).toBe('notes_3')
  })

  it('handles hidden files without extension', () => {
    expect(numberedName('.env', 1)).toBe('.env_1')
  })

  it('handles hidden files with extension', () => {
    expect(numberedName('.gitignore', 2)).toBe('.gitignore_2')
  })

  it('handles single-character names', () => {
    expect(numberedName('a', 5)).toBe('a_5')
  })

  it('handles single-character extension', () => {
    expect(numberedName('file.c', 1)).toBe('file_1.c')
  })
})
