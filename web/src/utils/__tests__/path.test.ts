import { describe, expect, it } from 'vitest'
import { splitPath, baseName, dirName, toRelativePath, joinPath } from '@/utils/path.ts'

describe('splitPath', () => {
  it('splits on forward slashes', () => {
    expect(splitPath('/home/user/project')).toEqual(['', 'home', 'user', 'project'])
  })

  it('splits on backslashes', () => {
    expect(splitPath('C:\\Users\\dev')).toEqual(['C:', 'Users', 'dev'])
  })

  it('splits on mixed separators', () => {
    expect(splitPath('C:/Users\\dev')).toEqual(['C:', 'Users', 'dev'])
  })
})

describe('baseName', () => {
  it('returns the last segment', () => {
    expect(baseName('/home/user/file.txt')).toBe('file.txt')
  })

  it('handles trailing slash', () => {
    // /home/user/ splits to ['', 'home', 'user', ''], pop removes '', rejoin → '/home/user'
    expect(dirName('/home/user/')).toBe('/home/user')
  })

  it('returns the path itself for a single segment', () => {
    expect(baseName('file.txt')).toBe('file.txt')
  })

  it('handles Windows paths', () => {
    expect(baseName('C:\\Users\\dev\\project')).toBe('project')
  })

  it('handles root path', () => {
    expect(baseName('/')).toBe('/')
  })
})

describe('dirName', () => {
  it('returns parent directory', () => {
    expect(dirName('/home/user/file.txt')).toBe('/home/user')
  })

  it('handles trailing slash', () => {
    // /home/user/ splits to ['', 'home', 'user', ''], pop removes '', rejoin → '/home/user'
    expect(dirName('/home/user/')).toBe('/home/user')
  })

  it('returns empty for single segment', () => {
    expect(dirName('file.txt')).toBe('')
  })

  it('handles Windows drive root', () => {
    expect(dirName('C:\\Users')).toBe('C:\\')
  })

  it('handles Windows paths with backslash', () => {
    expect(dirName('C:\\Users\\dev')).toBe('C:\\Users')
  })
})

describe('toRelativePath', () => {
  it('returns relative path from base', () => {
    expect(toRelativePath('/home/user/project/file.txt', '/home/user/project')).toBe('file.txt')
  })

  it('returns slash when path equals base', () => {
    expect(toRelativePath('/home/user/project', '/home/user/project')).toBe('/')
  })

  it('returns original if not starting with base', () => {
    expect(toRelativePath('/other/path', '/home/user')).toBe('/other/path')
  })

  it('returns original if base is empty', () => {
    expect(toRelativePath('/home/user', '')).toBe('/home/user')
  })

  it('handles Windows-style paths', () => {
    expect(toRelativePath('C:\\Users\\dev\\file.txt', 'C:\\Users\\dev')).toBe('file.txt')
  })

  it('strips leading slash from relative part', () => {
    expect(toRelativePath('/home/user/project/sub/file.txt', '/home/user/project')).toBe('sub/file.txt')
  })
})

describe('joinPath', () => {
  it('joins dir and name', () => {
    expect(joinPath('docs', 'file.txt')).toBe('docs/file.txt')
  })

  it('returns name only when dir is empty', () => {
    expect(joinPath('', 'file.txt')).toBe('file.txt')
  })

  it('normalizes "/" to root (empty string)', () => {
    expect(joinPath('/', 'file.txt')).toBe('file.txt')
  })

  it('handles subdirectory paths', () => {
    expect(joinPath('.clawbench/tmp', 'data.json')).toBe('.clawbench/tmp/data.json')
  })

  it('strips leading slash from dir', () => {
    expect(joinPath('/src', 'file.ts')).toBe('src/file.ts')
  })

  it('strips multiple leading slashes', () => {
    expect(joinPath('///deep', 'file.ts')).toBe('deep/file.ts')
  })

  it('strips trailing slash from dir', () => {
    expect(joinPath('src/', 'file.ts')).toBe('src/file.ts')
  })

  it('strips leading and trailing slashes together', () => {
    expect(joinPath('/src/', 'file.ts')).toBe('src/file.ts')
  })
})
