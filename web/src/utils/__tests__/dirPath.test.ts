import { describe, expect, it } from 'vitest'
import { resolveDirRequest } from '../dirPath'

const ROOT = '/home/me/proj'

describe('resolveDirRequest', () => {
  it('resolves a project-relative directory through /api/dir', () => {
    expect(resolveDirRequest('src/components', ROOT)).toEqual({
      dir: 'src/components',
      isExternal: false,
      url: '/api/dir?path=src%2Fcomponents',
    })
  })

  it('lists the project root with an empty path', () => {
    expect(resolveDirRequest('', ROOT)).toEqual({
      dir: '',
      isExternal: false,
      url: '/api/dir?path=',
    })
  })

  it('strips a trailing slash from a relative path', () => {
    expect(resolveDirRequest('src/', ROOT).dir).toBe('src')
    expect(resolveDirRequest('src/components//', ROOT).dir).toBe('src/components')
  })

  it('treats a leading slash as absolute (POSIX), not as a relative path', () => {
    expect(resolveDirRequest('/src/', ROOT).isExternal).toBe(true)
  })

  it('normalizes Windows separators before comparing', () => {
    expect(resolveDirRequest('src\\components', ROOT).url).toBe('/api/dir?path=src%2Fcomponents')
  })

  it('turns an absolute path inside the project into project-relative', () => {
    expect(resolveDirRequest('/home/me/proj/src', ROOT)).toEqual({
      dir: 'src',
      isExternal: false,
      url: '/api/dir?path=src',
    })
  })

  it('matches the project root case-insensitively (Windows drive letters)', () => {
    expect(resolveDirRequest('E:/Git/Proj/src', 'e:/git/proj')).toEqual({
      dir: 'src',
      isExternal: false,
      url: '/api/dir?path=src',
    })
  })

  it('lists an absolute path outside the project through /api/projects', () => {
    expect(resolveDirRequest('/var/log', ROOT)).toEqual({
      dir: '/var/log',
      isExternal: true,
      url: '/api/projects?path=%2Fvar%2Flog',
    })
  })

  it('keeps the filesystem root as "/" rather than collapsing it to the project root', () => {
    expect(resolveDirRequest('/', ROOT)).toEqual({
      dir: '/',
      isExternal: true,
      url: '/api/projects?path=%2F',
    })
  })

  it('treats an absolute path as project-relative when the root is unknown', () => {
    // Without a root the path cannot be classified; the legacy fallback keeps
    // the request inside the project instead of guessing at /api/projects.
    expect(resolveDirRequest('/var/log', '')).toEqual({
      dir: 'var/log',
      isExternal: false,
      url: '/api/dir?path=var%2Flog',
    })
  })

  it('does not treat a relative path as external even with a root', () => {
    expect(resolveDirRequest('..', ROOT).isExternal).toBe(false)
  })
})
