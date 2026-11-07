import { describe, it, expect } from 'vitest'
import { projectTintVar } from '@/utils/projectTint'

describe('projectTintVar', () => {
  it('is stable for the same path', () => {
    expect(projectTintVar('/home/me/projects/a')).toBe(projectTintVar('/home/me/projects/a'))
  })

  it('always returns one of the theme tokens', () => {
    const allowed = ['var(--color-purple)', 'var(--color-orange)', 'var(--color-green)', 'var(--color-info)']
    for (const p of ['/a', '/b/c', 'C:\\work\\x', '/很长/中文路径', '']) {
      expect(allowed).toContain(projectTintVar(p))
    }
  })

  it('spreads different paths over more than one tint', () => {
    const tints = new Set(['/a', '/b', '/c', '/d', '/e', '/f', '/g', '/h'].map(projectTintVar))
    expect(tints.size).toBeGreaterThan(1)
  })
})
