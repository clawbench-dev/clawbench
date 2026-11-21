import { describe, expect, it } from 'vitest'
import {
  AUTO_AVATAR_STYLES,
  AVATAR_BACKGROUNDS,
  AVATAR_STYLE_GROUPS,
  AVATAR_STYLE_META,
  AVATAR_STYLES,
  autoAvatarStyle,
  avatarBackgroundFor,
  avatarStyleLabelKey,
} from '@/utils/avatarPolicy'

describe('avatar styles', () => {
  it('exposes a duplicate-free catalog', () => {
    expect(AVATAR_STYLES.length).toBe(40)
    expect(new Set(AVATAR_STYLES).size).toBe(AVATAR_STYLES.length)
  })

  it('labels every style and puts each one in exactly one group', () => {
    for (const style of AVATAR_STYLES) {
      expect(AVATAR_STYLE_META[style], style).toBeTruthy()
      expect(avatarStyleLabelKey(style)).toBe(`settings.items.avatarStyles.${style}`)
    }
    const grouped = AVATAR_STYLE_GROUPS.flatMap((g) => g.styles)
    expect(grouped.length).toBe(AVATAR_STYLES.length)
    expect(new Set(grouped).size).toBe(AVATAR_STYLES.length)
    expect(new Set(grouped)).toEqual(new Set(AVATAR_STYLES))
  })

  it('keeps automatic avatars on the character subset', () => {
    expect(AUTO_AVATAR_STYLES.length).toBeGreaterThan(4)
    expect(new Set(AUTO_AVATAR_STYLES).size).toBe(AUTO_AVATAR_STYLES.length)
    for (const style of AUTO_AVATAR_STYLES) expect(AVATAR_STYLES).toContain(style)
    // Not a face, not a character — never assigned automatically.
    expect(AUTO_AVATAR_STYLES).not.toContain('landscape')
    expect(AUTO_AVATAR_STYLES).not.toContain('stripes')
    expect(AUTO_AVATAR_STYLES).not.toContain('shape-grid')
  })

  it('uses one normalization for style and background', () => {
    expect(autoAvatarStyle('访客  2')).toBe(autoAvatarStyle('访客 2'))
    expect(avatarBackgroundFor('访客  2')).toBe(avatarBackgroundFor('访客 2'))
  })

  it('maps a seed to a stable automatic style', () => {
    for (const seed of ['Alice', '小明', '访客 2']) {
      const style = autoAvatarStyle(seed)
      expect(AUTO_AVATAR_STYLES).toContain(style)
      expect(autoAvatarStyle(seed)).toBe(style)
      expect(autoAvatarStyle(` ${seed.toUpperCase()} `)).toBe(style)
    }
  })
})

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
