import { describe, expect, it } from 'vitest'
import { teamMemberColorVar } from '@/utils/teamMemberColor'

// The mapping is shared by the team roster (TeamPanel) and the permission card
// (renderToolDetail), so a member must resolve to the same colour in both.
describe('teamMemberColorVar', () => {
  it('maps every wire colour name to a theme token', () => {
    for (const name of ['blue', 'green', 'red', 'yellow', 'orange', 'purple', 'pink', 'cyan']) {
      const v = teamMemberColorVar(name)
      expect(v).toBeTruthy()
      expect(v).not.toBe(teamMemberColorVar('__unknown__'))
    }
  })

  it('resolves blue/green to the semantic theme tokens', () => {
    expect(teamMemberColorVar('blue')).toContain('--color-info')
    expect(teamMemberColorVar('green')).toContain('--color-green')
  })

  it('falls back to the secondary text token for unknown/absent colours', () => {
    const fallback = 'var(--text-secondary, #495057)'
    expect(teamMemberColorVar(undefined)).toBe(fallback)
    expect(teamMemberColorVar(null)).toBe(fallback)
    expect(teamMemberColorVar('')).toBe(fallback)
    expect(teamMemberColorVar('chartreuse')).toBe(fallback)
  })
})
