import { describe, expect, it } from 'vitest'
import { splitGitFilePath } from '@/utils/gitFileHistory'

describe('splitGitFilePath', () => {
  it('splits a nested path into name and parent directory', () => {
    expect(splitGitFilePath('web/src/foo.ts')).toEqual({ name: 'foo.ts', dir: 'web/src' })
  })

  it('returns empty dir for a root-level file', () => {
    expect(splitGitFilePath('README.md')).toEqual({ name: 'README.md', dir: '' })
  })

  it('handles a single subdirectory', () => {
    expect(splitGitFilePath('src/main.go')).toEqual({ name: 'main.go', dir: 'src' })
  })

  it('returns empty dir for an empty path', () => {
    expect(splitGitFilePath('')).toEqual({ name: '', dir: '' })
  })

  it('treats a trailing-slash dir as a leaf name (untracked dir from porcelain)', () => {
    expect(splitGitFilePath('foo/')).toEqual({ name: 'foo', dir: '' })
    expect(splitGitFilePath('sub/dir/')).toEqual({ name: 'dir', dir: 'sub' })
  })

  it('keeps dots and unusual characters in the name intact', () => {
    expect(splitGitFilePath('docs/my.file.name.txt')).toEqual({ name: 'my.file.name.txt', dir: 'docs' })
  })
})
