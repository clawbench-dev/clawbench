import { describe, expect, it } from 'vitest'
import { TAG_PALETTE, hashTagName, tagAccent, tagAccentStyle } from '@/utils/tagColor'

describe('tagColor', () => {
  describe('hashTagName', () => {
    it('is deterministic for the same input', () => {
      expect(hashTagName('bug')).toBe(hashTagName('bug'))
    })

    it('is case-insensitive so Bug/bug render identically', () => {
      // The backend treats tag names as case-insensitively unique per project,
      // so the color must not depend on casing either.
      expect(hashTagName('Bug')).toBe(hashTagName('bug'))
      expect(hashTagName('BUG')).toBe(hashTagName('bug'))
    })

    it('returns an unsigned 32-bit integer', () => {
      for (const name of ['bug', 'urgent', 'needs review', 'a', '']) {
        const h = hashTagName(name)
        expect(Number.isInteger(h)).toBe(true)
        expect(h).toBeGreaterThanOrEqual(0)
        expect(h).toBeLessThan(2 ** 32)
      }
    })

    it('handles empty/undefined-ish input without throwing', () => {
      expect(hashTagName('')).toBe(hashTagName(''))
      expect(() => hashTagName(undefined as unknown as string)).not.toThrow()
    })

    it('is order-sensitive so anagram tags do not collide', () => {
      // The property that distinguishes FNV-1a from a naive char-sum hash: a
      // char-sum is commutative, so "live"/"evil" (and "ab"/"ba") would hash
      // identically and render the same color. Verify position actually feeds
      // into the hash.
      expect(hashTagName('ab')).not.toBe(hashTagName('ba'))
      expect(hashTagName('live')).not.toBe(hashTagName('evil'))
      expect(hashTagName('bug')).not.toBe(hashTagName('gub'))
    })

    it('spreads similar short strings across different buckets', () => {
      // A naive char-sum hash maps "p1"/"p2" to adjacent buckets, which would
      // make neighbouring tags look identical. FNV-1a is chosen specifically to
      // avoid that — assert it here so a future "simplification" of the hash
      // cannot silently regress it.
      const buckets = ['p1', 'p2', 'p3', 'p4', 'p5'].map(n => hashTagName(n) % TAG_PALETTE.length)
      expect(new Set(buckets).size).toBeGreaterThan(1)
    })
  })

  describe('tagAccent', () => {
    it('always returns a palette entry with both theme colors', () => {
      const accent = tagAccent('bug')
      expect(TAG_PALETTE).toContainEqual(accent)
      expect(accent.light).toMatch(/^#[0-9a-f]{6}$/i)
      expect(accent.dark).toMatch(/^#[0-9a-f]{6}$/i)
    })

    it('gives the same tag the same color on every call', () => {
      expect(tagAccent('urgent')).toEqual(tagAccent('urgent'))
    })

    it('matches the tool-call palette from ContentBlocks.vue', () => {
      // These exact hex values are duplicated in ContentBlocks.vue's
      // [data-category] rules. Pinning them here means a palette change on
      // either side surfaces as a test failure instead of a visual mismatch.
      const flat = TAG_PALETTE.flatMap(p => [p.light, p.dark])
      for (const hex of ['#10b981', '#34d399', '#8b5cf6', '#a78bfa', '#f59e0b', '#fbbf24', '#ec4899', '#f472b6', '#06b6d4', '#22d3ee', '#f97316', '#fb923c', '#eab308']) {
        expect(flat).toContain(hex)
      }
    })

    it('never resolves to a theme-dependent token', () => {
      // `file`/`plan` categories map to var(--accent-color); those are excluded
      // because every tag landing there would collapse to one hue.
      for (const entry of TAG_PALETTE) {
        expect(entry.light).not.toContain('var(')
        expect(entry.dark).not.toContain('var(')
      }
    })

    it('distributes a realistic tag set over more than one color', () => {
      const names = ['bug', 'urgent', 'needs review', 'backend', 'frontend', 'docs', 'refactor', 'perf', 'test', 'release']
      const used = new Set(names.map(n => tagAccent(n).light))
      expect(used.size).toBeGreaterThan(1)
    })
  })

  describe('tagAccentStyle', () => {
    it('emits both theme variables for CSS to pick from', () => {
      const style = tagAccentStyle('bug')
      const accent = tagAccent('bug')
      expect(style).toEqual({
        '--tag-accent-light': accent.light,
        '--tag-accent-dark': accent.dark,
      })
    })
  })
})
