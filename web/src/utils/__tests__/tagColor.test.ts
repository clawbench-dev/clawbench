import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { TAG_PALETTE, hashTagName, tagAccent, tagAccentStyle } from '@/utils/tagColor'

/**
 * Parse the tool-call accent palette out of ContentBlocks.vue.
 *
 * Reading the real component is the point: an earlier version of this test
 * asserted a hardcoded list of hex strings, so changing the palette in
 * ContentBlocks.vue left the test green (verified by mutation) — it guarded
 * nothing despite claiming to catch drift between the two color systems.
 */
function toolCallPaletteFromComponent() {
  // Vitest runs with the web/ package as cwd, so a path relative to it is
  // stable (import.meta.url is not a file: URL under vitest's transform).
  const path = resolve(process.cwd(), 'src/components/chat/ContentBlocks.vue')
  const src = readFileSync(path, 'utf8')
  const light: Record<string, string> = {}
  const dark: Record<string, string> = {}
  const re = /\.chat-tool-call\[data-category="([a-z]+)"\]\s*\{\s*--tool-accent:\s*([^;]+);\s*\}/g

  // Walk line by line: the dark rules reuse the same selector, so matching the
  // whole file with one regex would let a dark value overwrite its light twin.
  for (const line of src.split('\n')) {
    const m = re.exec(line)
    re.lastIndex = 0
    if (!m) continue
    const [, category, value] = m
    const hex = value.trim()
    // Only raw hex participates in the tag palette; `var(--accent-color)`
    // categories (file/plan) are theme-dependent and deliberately excluded.
    if (!hex.startsWith('#')) continue
    if (line.includes('data-theme-base="dark"')) dark[category] = hex.toLowerCase()
    else light[category] = hex.toLowerCase()
  }
  return { light, dark }
}

describe('tagColor', () => {
  describe('palette consistency with ContentBlocks.vue', () => {
    it('every tag palette color exists in the tool-call palette', () => {
      // Guards requirement "different tags get different colors, following the
      // tool-call color logic": the values must come FROM that palette, so a
      // change on either side is caught here rather than by eye.
      const { light, dark } = toolCallPaletteFromComponent()
      const toolLight = new Set(Object.values(light))
      const toolDark = new Set(Object.values(dark))
      expect(toolLight.size).toBeGreaterThan(0)
      expect(toolDark.size).toBeGreaterThan(0)

      for (const entry of TAG_PALETTE) {
        expect(toolLight).toContain(entry.light.toLowerCase())
        expect(toolDark).toContain(entry.dark.toLowerCase())
      }
    })

    it('covers every non-theme-dependent tool-call category', () => {
      // If a new hex-accented category is added to ContentBlocks.vue, the tag
      // palette should pick it up rather than silently diverging.
      const { light } = toolCallPaletteFromComponent()
      expect(TAG_PALETTE.length).toBe(Object.keys(light).length)
    })
  })

  describe('hashTagName', () => {
    it('is deterministic for the same input', () => {
      expect(hashTagName('bug')).toBe(hashTagName('bug'))
    })

    it('is case-insensitive so Bug/bug render identically', () => {
      // The backend folds tag names to lowercase (NormalizeSessionTagName), so
      // "Bug" and "bug" are literally the same tag — the color must not depend
      // on casing either.
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
      //
      // This is the assertion that actually discriminates — a "distinct buckets
      // for p1..p5" style check passes with a commutative hash too, so it would
      // give false confidence.
      expect(hashTagName('ab')).not.toBe(hashTagName('ba'))
      expect(hashTagName('live')).not.toBe(hashTagName('evil'))
      expect(hashTagName('bug')).not.toBe(hashTagName('gub'))
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
