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
vi.mock('@dicebear/styles/initials.json', () => ({ default: { $id: 'initials' } }))
vi.mock('@dicebear/styles/shapes.json', () => ({ default: { $id: 'shapes' } }))
vi.mock('@dicebear/styles/glass.json', () => ({ default: { $id: 'glass' } }))
vi.mock('@dicebear/styles/pixel-art.json', () => ({ default: { $id: 'pixel-art' } }))
vi.mock('@dicebear/styles/fun-emoji.json', () => ({ default: { $id: 'fun-emoji' } }))
vi.mock('@dicebear/styles/lorelei.json', () => ({ default: { $id: 'lorelei' } }))

import { AVATAR_STYLES, getAvatarLib, loadAvatarKit } from '@/utils/lazyAvatar'
import { svgToDataUri } from '@/utils/svgDataUri'

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

  it('loadAvatarKit returns a Style instance for every curated style', async () => {
    const kit = await loadAvatarKit()
    expect(typeof kit.Avatar).toBe('function')
    expect(Object.keys(kit.styles).sort()).toEqual([...AVATAR_STYLES].sort())
  })

  it('svgToDataUri encodes the SVG so "#" in url(#id) survives', () => {
    const svg = '<svg><rect fill="url(#grad)"/></svg>'
    const uri = svgToDataUri(svg)
    expect(uri.startsWith('data:image/svg+xml;charset=utf-8,')).toBe(true)
    expect(uri).toContain('%23') // '#' encoded, not left raw
    expect(decodeURIComponent(uri.split(',')[1])).toBe(svg)
  })
})
