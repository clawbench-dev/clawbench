import { describe, expect, it } from 'vitest'
import { buildDirListUrl } from '@/utils/dirList'

describe('buildDirListUrl', () => {
  it('routes an absolute path to /api/projects (project-external listing)', () => {
    expect(buildDirListUrl('/tmp/outside')).toBe('/api/projects?path=%2Ftmp%2Foutside')
  })

  it('routes a relative path to /api/dir', () => {
    expect(buildDirListUrl('src/components')).toBe('/api/dir?path=src%2Fcomponents')
  })

  it('routes an empty path (project root) to /api/dir with an empty param', () => {
    expect(buildDirListUrl('')).toBe('/api/dir?path=')
  })

  it('routes a Windows absolute path to /api/projects', () => {
    expect(buildDirListUrl('C:\\Users\\dev')).toBe('/api/projects?path=C%3A%5CUsers%5Cdev')
  })

  it('percent-encodes characters that would otherwise break the query', () => {
    expect(buildDirListUrl('a b&c=d')).toBe('/api/dir?path=a%20b%26c%3Dd')
  })
})
