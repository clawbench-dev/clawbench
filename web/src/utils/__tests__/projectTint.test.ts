import { describe, it, expect } from 'vitest'
import { PROJECT_TINTS, projectTint, projectTintStyle } from '@/utils/projectTint'
import { readWebFile } from '@/testUtils/readWebFile'

/** #rrggbb -> [r,g,b], or null when the string is not a plain hex colour. */
function hexToRgb(hex: string): [number, number, number] | null {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec((hex || '').trim())
  return m ? [parseInt(m[1], 16), parseInt(m[2], 16), parseInt(m[3], 16)] : null
}

/** WCAG 2.1 relative luminance. */
function luminance([r, g, b]: [number, number, number]): number {
  const f = (c: number) => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}

function contrast(a: [number, number, number], b: [number, number, number]): number {
  const la = luminance(a)
  const lb = luminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

/** `color-mix(in srgb, fg a%, bg)` flattened into an opaque colour. */
function over(fg: [number, number, number], bg: [number, number, number], alpha: number): [number, number, number] {
  return [0, 1, 2].map(i => Math.round(fg[i] * alpha + bg[i] * (1 - alpha))) as [number, number, number]
}

/**
 * Every shipped theme's real card surface, read out of the stylesheet.
 *
 * Reading the file rather than restating a list is the point: a new theme must
 * be covered without editing this test, and a palette value that only works on
 * the default theme must fail here.
 */
function themeSurfaces(): { name: string; bg: [number, number, number]; dark: boolean }[] {
  const src = readWebFile('css/variables.css')
  const root = /:root\s*\{([\s\S]*?)\n\}/.exec(src)
  const base: Record<string, string> = {}
  if (root) {
    for (const [, k, v] of root[1].matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+);/g)) base[k] = v.trim()
  }
  const out: { name: string; bg: [number, number, number]; dark: boolean }[] = []
  for (const m of src.matchAll(/\[data-theme="([a-z0-9-]+)"\]\s*\{([\s\S]*?)\n\}/g)) {
    const vars = { ...base }
    for (const [, k, v] of m[2].matchAll(/--([a-z0-9-]+)\s*:\s*([^;]+);/g)) vars[k] = v.trim()
    const rgb = hexToRgb(vars['bg-primary'] || vars['bg-secondary'])
    if (rgb) out.push({ name: m[1], bg: rgb, dark: luminance(rgb) < 0.35 })
  }
  return out
}

describe('projectTint', () => {
  it('is stable for the same path', () => {
    expect(projectTint('/home/me/projects/a')).toEqual(projectTint('/home/me/projects/a'))
  })

  it('folds case so one project keeps one colour on case-insensitive filesystems', () => {
    expect(projectTint('/Home/Me/Project')).toEqual(projectTint('/home/me/project'))
  })

  it('is position-sensitive so anagrams do not collide', () => {
    // A char-sum hash is commutative: "/a/b" and "/b/a" would land on the same
    // colour. This is the assertion that distinguishes FNV-1a from that.
    expect(projectTint('/proj/alpha')).not.toEqual(projectTint('/proj/ahpla'))
  })

  it('has enough colours that a dozen projects rarely repeat', () => {
    // The whole point of replacing the four status tokens: with four buckets,
    // ten projects necessarily repeat, so the colour stops distinguishing
    // anything.
    expect(PROJECT_TINTS.length).toBeGreaterThanOrEqual(10)
    const paths = Array.from({ length: 12 }, (_, i) => `/home/me/projects/p${i}`)
    const used = new Set(paths.map(p => projectTint(p).light))
    expect(used.size, 'a dozen projects should spread over most of the palette').toBeGreaterThanOrEqual(6)
  })

  it('always returns a palette entry with both theme values', () => {
    for (const p of ['/a', '/b/c', 'C:\\work\\x', '/很长/中文路径', '']) {
      const tint = projectTint(p)
      expect(PROJECT_TINTS).toContainEqual(tint)
      expect(tint.light).toMatch(/^#[0-9a-f]{6}$/i)
      expect(tint.dark).toMatch(/^#[0-9a-f]{6}$/i)
    }
  })

  it('never resolves to a theme-dependent token', () => {
    // A `var(--color-purple)`-style value would collapse every project onto
    // whatever hue that theme happens to define, which is the behaviour this
    // palette replaced.
    for (const entry of PROJECT_TINTS) {
      expect(entry.light).not.toContain('var(')
      expect(entry.dark).not.toContain('var(')
    }
  })

  it('publishes both theme values for the card CSS to pick from', () => {
    const style = projectTintStyle('/home/me/projects/a')
    const tint = projectTint('/home/me/projects/a')
    expect(style).toEqual({ '--pc-light': tint.light, '--pc-dark': tint.dark })
  })

  describe('legibility across every theme', () => {
    it('has both light and dark themes to check', () => {
      const surfaces = themeSurfaces()
      expect(surfaces.length).toBeGreaterThan(20)
      expect(surfaces.filter(s => s.dark).length).toBeGreaterThan(5)
      expect(surfaces.filter(s => !s.dark).length).toBeGreaterThan(5)
    })

    it('keeps the card icon visible against every theme surface', () => {
      // The icon is painted in the bare accent on the card's own surface, and
      // the card border is the accent at 45% — WCAG asks 3:1 for non-text UI.
      const worst: string[] = []
      for (const { name, bg, dark } of themeSurfaces()) {
        for (const entry of PROJECT_TINTS) {
          const accent = hexToRgb(dark ? entry.dark : entry.light)
          if (!accent) continue
          const cr = contrast(accent, bg)
          if (cr < 3) worst.push(`${name}/${dark ? 'dark' : 'light'} ${dark ? entry.dark : entry.light} = ${cr.toFixed(2)}`)
        }
      }
      expect(worst, `below 3:1:\n${worst.join('\n')}`).toEqual([])
    })

    it('keeps the accent readable on its own tinted header band', () => {
      // The header paints the accent at 15% into the surface and puts the icon
      // and count badge on top. A value that only just clears 3:1 on the bare
      // surface can still vanish on its own tint.
      const worst: string[] = []
      for (const { name, bg, dark } of themeSurfaces()) {
        for (const entry of PROJECT_TINTS) {
          const accent = hexToRgb(dark ? entry.dark : entry.light)
          if (!accent) continue
          const band = over(accent, bg, 0.15)
          const cr = contrast(accent, band)
          if (cr < 3) worst.push(`${name}/${dark ? 'dark' : 'light'} ${dark ? entry.dark : entry.light} = ${cr.toFixed(2)}`)
        }
      }
      expect(worst, `below 3:1:\n${worst.join('\n')}`).toEqual([])
    })

    it('keeps every palette entry distinguishable from its neighbours', () => {
      // Ten hues only help if they can be told apart. Two entries that resolve
      // to nearly the same colour would make the palette look like it has fewer
      // colours than it does.
      const hue = (hex: string): number | null => {
        const rgb = hexToRgb(hex)
        if (!rgb) return null
        const [r, g, b] = rgb.map(v => v / 255)
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
      for (const mode of ['light', 'dark'] as const) {
        const hues = PROJECT_TINTS.map(e => hue(e[mode])).filter((h): h is number => h !== null)
        expect(hues.length, `${mode} entries must all be real colours`).toBe(PROJECT_TINTS.length)
        for (let i = 0; i < hues.length; i++) {
          for (let j = i + 1; j < hues.length; j++) {
            const d = Math.abs(hues[i] - hues[j]) % 360
            const dist = Math.min(d, 360 - d)
            expect(dist, `${mode}: entries ${i} and ${j} are only ${dist.toFixed(0)}° apart`).toBeGreaterThan(15)
          }
        }
      }
    })
  })
})
