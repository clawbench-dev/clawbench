import { describe, expect, it } from 'vitest'
import { splitPath, baseName, dirName, toRelativePath, joinPath, isWindowsAbsolutePath, normalizeSlashes, isAbsolutePath, toProjectRelative } from '@/utils/path.ts'

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

  it('returns forward-slash drive root for forward-slash input', () => {
    // When the path has been normalized to forward slashes (as navToFileInManager
    // does), the drive root must come back with "/" so joinPath produces a
    // consistent separator style that DOM data-path matching relies on.
    expect(dirName('C:/Users/dev')).toBe('C:/Users')
    expect(dirName('C:/dev')).toBe('C:/')
  })
})

describe('isWindowsAbsolutePath', () => {
  it('detects drive-letter absolute paths with forward slashes', () => {
    expect(isWindowsAbsolutePath('C:/Users/foo/a.go')).toBe(true)
  })

  it('detects drive-letter absolute paths with backslashes', () => {
    expect(isWindowsAbsolutePath('C:\\Users\\foo\\a.go')).toBe(true)
  })

  it('detects bare drive roots', () => {
    expect(isWindowsAbsolutePath('C:\\')).toBe(true)
    expect(isWindowsAbsolutePath('C:/')).toBe(true)
  })

  it('detects UNC paths', () => {
    expect(isWindowsAbsolutePath('\\\\server\\share\\a.go')).toBe(true)
  })

  it('does not match relative paths or bare drive letters', () => {
    expect(isWindowsAbsolutePath('src/main.go')).toBe(false)
    expect(isWindowsAbsolutePath('C:')).toBe(false)
  })

  it('does not match Unix absolute paths', () => {
    expect(isWindowsAbsolutePath('/home/user/a.go')).toBe(false)
  })
})

describe('normalizeSlashes', () => {
  it('converts backslashes to forward slashes', () => {
    expect(normalizeSlashes('E:\\git\\vllm-input\\test-results')).toBe('E:/git/vllm-input/test-results')
  })

  it('leaves forward-slash paths unchanged', () => {
    expect(normalizeSlashes('/home/user/a.go')).toBe('/home/user/a.go')
  })

  it('handles mixed separators', () => {
    expect(normalizeSlashes('C:/Users\\dev')).toBe('C:/Users/dev')
  })
})

describe('isAbsolutePath', () => {
  it('matches Unix absolute paths', () => {
    expect(isAbsolutePath('/home/user/a.go')).toBe(true)
  })

  it('matches Windows drive absolute paths', () => {
    expect(isAbsolutePath('E:/git/a.go')).toBe(true)
    expect(isAbsolutePath('E:\\git\\a.go')).toBe(true)
  })

  it('matches UNC paths', () => {
    expect(isAbsolutePath('\\\\server\\share\\a.go')).toBe(true)
  })

  it('does not match relative paths', () => {
    expect(isAbsolutePath('src/main.go')).toBe(false)
    expect(isAbsolutePath('a.go')).toBe(false)
  })
})

describe('toProjectRelative', () => {
  it('relativizes an absolute path under the root', () => {
    expect(toProjectRelative('E:/git/vllm-input/internal/app', 'E:/git/vllm-input')).toBe('internal/app')
  })

  it('handles backslash root and path', () => {
    expect(toProjectRelative('E:\\git\\vllm-input\\internal\\app', 'E:\\git\\vllm-input')).toBe('internal/app')
  })

  it('is case-insensitive for drive letters', () => {
    expect(toProjectRelative('e:/git/vllm-input/internal/app', 'E:/git/vllm-input')).toBe('internal/app')
  })

  it('returns the path unchanged when not under the root', () => {
    expect(toProjectRelative('D:/other/dir', 'E:/git/vllm-input')).toBe('D:/other/dir')
  })

  it('returns the path unchanged when root is empty', () => {
    expect(toProjectRelative('E:/git/a.go', '')).toBe('E:/git/a.go')
  })

  it('relativizes Unix absolute paths under the root', () => {
    expect(toProjectRelative('/home/user/project/src/main.go', '/home/user/project')).toBe('src/main.go')
  })

  it('does not relativize a path that merely shares a prefix without boundary', () => {
    expect(toProjectRelative('E:/git/vllm-input-other/app', 'E:/git/vllm-input')).toBe('E:/git/vllm-input-other/app')
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
