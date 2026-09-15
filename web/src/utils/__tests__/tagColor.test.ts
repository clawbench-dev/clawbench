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
  describe('palette relationship to the tool-call palette', () => {
    /**
     * Hue in degrees (0-360), or null for a fully desaturated colour.
     * Tags deliberately keep the tool palette's HUES while using different
     * lightness values (see TAG_PALETTE's doc comment), so hue is the property
     * that must still line up.
     */
    function hue(hex: string): number | null {
      const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex)
      if (!m) return null
      const [r, g, b] = m.slice(1).map(v => parseInt(v, 16) / 255)
      const max = Math.max(r, g, b)
      const min = Math.min(r, g, b)
      const d = max - min
      if (d === 0) return null
      let h: number
      if (max === r) h = ((g - b) / d) % 6
      else if (max === g) h = (b - r) / d + 2
      else h = (r - g) / d + 4
      return (h * 60 + 360) % 360
    }

    function hueDistance(a: number, b: number) {
      const d = Math.abs(a - b) % 360
      return Math.min(d, 360 - d)
    }

    it('every tag colour shares a hue with a tool-call accent', () => {
      // The tags must stay recognisably in the same colour family as the tool
      // cards, but are NOT the same values: the tool accents are tuned for an
      // icon + 6% tint, and using them verbatim as 11px text left 128 of 252
      // theme×colour combinations below 4.5:1. Asserting hue (not equality)
      // still catches a genuinely new colour being introduced on either side.
      const { light, dark } = toolCallPaletteFromComponent()
      const huePool = (values: string[]) =>
        values.map(hue).filter((h): h is number => h !== null)

      const lightHues = huePool(Object.values(light))
      const darkHues = huePool(Object.values(dark))
      expect(lightHues.length).toBeGreaterThan(0)
      expect(darkHues.length).toBeGreaterThan(0)

      for (const entry of TAG_PALETTE) {
        const lh = hue(entry.light)
        const dh = hue(entry.dark)
        expect(lh, `${entry.light} must be a hex colour`).not.toBeNull()
        expect(dh, `${entry.dark} must be a hex colour`).not.toBeNull()
        // 25° tolerance: enough to allow the re-lighting, tight enough that a
        // blue swapped for a green is still caught.
        expect(
          Math.min(...lightHues.map(h => hueDistance(lh!, h))),
          `${entry.light} should match a light tool-call hue`,
        ).toBeLessThanOrEqual(25)
        expect(
          Math.min(...darkHues.map(h => hueDistance(dh!, h))),
          `${entry.dark} should match a dark tool-call hue`,
        ).toBeLessThanOrEqual(25)
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

  describe('legibility across every theme', () => {
    /**
     * Every theme's real surface colour, parsed out of variables.css.
     *
     * Reading the stylesheet (rather than a hardcoded list) is the point: the
     * palette has to work on whatever surfaces the app actually ships, and a
     * new theme must be covered without editing this test.
     */
    function surfaces(): { name: string; bg: [number, number, number]; dark: boolean }[] {
      const path = resolve(process.cwd(), '../web/css/variables.css')
      const src = readFileSync(path, 'utf8')
      const root = /:root\s*\{([\s\S]*?)\n\}/.exec(src)
      const base: Record<string, string> = {}
      if (root) {
        for (const [, k, v] of root[1].matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+);/g)) base[k] = v.trim()
      }
      const out: { name: string; bg: [number, number, number]; dark: boolean }[] = []
      for (const m of src.matchAll(/\[data-theme="([a-z0-9-]+)"\]\s*\{([\s\S]*?)\n\}/g)) {
        const vars = { ...base }
        for (const [, k, v] of m[2].matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+);/g)) vars[k] = v.trim()
        const hex = vars['bg-secondary'] || vars['bg-primary']
        const rgb = hexToRgb(hex)
        if (rgb) out.push({ name: m[1], bg: rgb, dark: luminance(rgb) < 0.35 })
      }
      return out
    }

    function hexToRgb(hex: string): [number, number, number] | null {
      const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec((hex || '').trim())
      return m ? [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)] : null
    }

    function luminance([r, g, b]: [number, number, number]) {
      const f = (c: number) => {
        const s = c / 255
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
      }
      return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
    }

    function contrast(a: [number, number, number], b: [number, number, number]) {
      const la = luminance(a)
      const lb = luminance(b)
      return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
    }

    /** color-mix(in srgb, fg a%, transparent) painted over bg. */
    function over(fg: [number, number, number], bg: [number, number, number], a: number): [number, number, number] {
      return [0, 1, 2].map(i => Math.round(fg[i] * a + bg[i] * (1 - a))) as [number, number, number]
    }

    it('has themes to check', () => {
      expect(surfaces().length).toBeGreaterThan(20)
    })

    it('keeps every tag readable as text on every theme surface', () => {
      // Regression: the palette used to be the tool-call accents verbatim.
      // Those are tuned for an icon plus a 6% tint, so as 11px chip text they
      // left 128 of 252 theme×colour combinations under WCAG AA 4.5:1 — amber
      // and yellow bottomed out at 1.45:1 on light themes, i.e. invisible.
      const worst: string[] = []
      for (const { name, bg, dark } of surfaces()) {
        for (const entry of TAG_PALETTE) {
          const accent = hexToRgb(dark ? entry.dark : entry.light)
          if (!accent) continue
          // The chip paints its own accent at 12% behind the text.
          const chipBg = over(accent, bg, 0.12)
          const cr = contrast(accent, chipBg)
          if (cr < 4.5) worst.push(`${name}/${dark ? 'dark' : 'light'} ${dark ? entry.dark : entry.light} = ${cr.toFixed(2)}`)
        }
      }
      expect(worst, `below 4.5:1:\n${worst.join('\n')}`).toEqual([])
    })

    it('keeps the accent visible as a border against every theme surface', () => {
      // The chip's 1px border is the bare accent on the surface; WCAG asks 3:1
      // for non-text UI.
      const worst: string[] = []
      for (const { name, bg, dark } of surfaces()) {
        for (const entry of TAG_PALETTE) {
          const accent = hexToRgb(dark ? entry.dark : entry.light)
          if (!accent) continue
          const cr = contrast(accent, bg)
          if (cr < 3) worst.push(`${name}/${dark ? 'dark' : 'light'} ${dark ? entry.dark : entry.light} = ${cr.toFixed(2)}`)
        }
      }
      expect(worst, `below 3:1:\n${worst.join('\n')}`).toEqual([])
    })
  })
})
