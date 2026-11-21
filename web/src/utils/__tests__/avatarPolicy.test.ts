import { describe, expect, it } from 'vitest'
import { AVATAR_BACKGROUNDS, avatarBackgroundFor } from '@/utils/avatarPolicy'

describe('avatarBackgroundFor', () => {
  it('is deterministic and always returns a palette colour', () => {
    for (const seed of ['Luna', '小明', '阿强', '', 'Agent 2']) {
      const color = avatarBackgroundFor(seed)
      expect(AVATAR_BACKGROUNDS).toContain(color)
      expect(avatarBackgroundFor(seed)).toBe(color)
    }
  })

  it('normalizes case and surrounding whitespace', () => {
    expect(avatarBackgroundFor(' Luna ')).toBe(avatarBackgroundFor('luna'))
  })

  it('does not collapse every name onto one colour', () => {
    const colors = new Set(['Luna', 'Max', '小明', '阿强', 'Nova'].map(avatarBackgroundFor))
    expect(colors.size).toBeGreaterThan(1)
  })
})
