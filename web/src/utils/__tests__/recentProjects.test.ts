import { describe, expect, it } from 'vitest'
import { recentProjectDisplayPath } from '@/utils/recentProjects'

describe('recentProjectDisplayPath', () => {
  it('is relative to homeDir when the path starts with it (Unix)', () => {
    expect(recentProjectDisplayPath('/home/user/projects/myapp', '/home/user')).toBe('projects/myapp')
  })

  it('keeps the original path when not under homeDir', () => {
    expect(recentProjectDisplayPath('/opt/other/project', '/home/user')).toBe('/opt/other/project')
  })

  it('keeps the path when homeDir is empty', () => {
    expect(recentProjectDisplayPath('/home/user/proj', '')).toBe('/home/user/proj')
  })

  it('strips the homeDir prefix using the original path separators (Windows)', () => {
    expect(recentProjectDisplayPath('C:\\Users\\x\\proj', 'C:\\Users\\x')).toBe('proj')
  })

  it('matches a homeDir with backslashes against a forward-slash path', () => {
    expect(recentProjectDisplayPath('C:/Users/x/other', 'C:\\Users\\x')).toBe('other')
  })

  it('keeps the full path when the prefix is a partial segment match', () => {
    // '/home/user2' must not match homeDir '/home/user'.
    expect(recentProjectDisplayPath('/home/user2/proj', '/home/user')).toBe('/home/user2/proj')
  })
})
