import { describe, expect, it, vi, beforeEach } from 'vitest'

// Mock the heavy DiceBear packages so the test exercises our wrapper logic
// (style dispatch, raw-SVG output) without loading real JSON/ESM chunks.
vi.mock('@dicebear/core', () => {
  class Style {
    constructor(public definition: unknown) {}
  }
  class Avatar {
    constructor(public style: unknown, public options: Record<string, unknown>) {}
    toString() {
      return `<svg data-seed="${this.options.seed}"></svg>`
    }
  }
  return { Avatar, Style }
})

vi.mock('@dicebear/styles/bottts.json', () => ({ default: { $id: 'bottts' } }))
vi.mock('@dicebear/styles/identicon.json', () => ({ default: { $id: 'identicon' } }))

import { AVATAR_STYLES, getAvatarLib, renderAvatar } from '@/utils/lazyAvatar'

describe('lazyAvatar', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('exposes the curated style set', () => {
    expect(AVATAR_STYLES).toContain('bottts')
    expect(AVATAR_STYLES).toContain('identicon')
    expect(AVATAR_STYLES).toContain('micah')
    expect(AVATAR_STYLES).toContain('voxel-bot')
    expect(AVATAR_STYLES.length).toBe(40)
    // No duplicates in the curated list.
    expect(new Set(AVATAR_STYLES).size).toBe(AVATAR_STYLES.length)
  })

  it('getAvatarLib returns the cached core module', async () => {
    const a = await getAvatarLib()
    const b = await getAvatarLib()
    expect(a).toBe(b)
    expect(typeof a.Avatar).toBe('function')
  })

  it('renderAvatar returns a raw SVG string for the requested seed', async () => {
    const svg = await renderAvatar('bottts', 'CodeBuddy')
    expect(svg.startsWith('<svg')).toBe(true)
    expect(svg).toContain('CodeBuddy')
  })

  it('renderAvatar accepts a size option', async () => {
    const svg = await renderAvatar('identicon', 'x', 128)
    expect(svg).toContain('<svg')
  })
})
