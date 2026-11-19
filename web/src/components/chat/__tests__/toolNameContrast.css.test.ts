import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Issue #701 — the tool-call NAME keeps its category colour but must still be
 * readable.
 *
 * The raw `--tool-accent` was painted straight onto the name: on the light
 * themes bash measured ≈2.5:1 and task/interrupt ≈2.1:1 against the row's own
 * tint, well under WCAG AA's 4.5:1. A flat `--text-secondary` fixed that but
 * lost the category colour, so the ink is now the accent mixed toward black
 * (light) / white (dark) by a per-category ratio — same hue, readable.
 *
 * jsdom does not load SFC <style>, so this reads the stylesheet sources and
 * re-runs the same arithmetic the browser does for `color-mix(in srgb, …)`.
 */

function hexToRgb(hex: string): [number, number, number] {
  let h = hex.trim().replace('#', '')
  if (h.length === 3) h = h.split('').map(c => c + c).join('')
  return [0, 2, 4].map(i => parseInt(h.slice(i, i + 2), 16)) as [number, number, number]
}

function mix(a: [number, number, number], b: [number, number, number], p: number) {
  return [0, 1, 2].map(i => a[i] * p + b[i] * (1 - p)) as [number, number, number]
}

function luminance(c: [number, number, number]): number {
  const [r, g, b] = c.map(v => {
    const x = v / 255
    return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: [number, number, number], b: [number, number, number]): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

const contentBlocks = readWebFile('src/components/chat/ContentBlocks.vue')
const variables = readWebFile('css/variables.css')
const themeMeta = readWebFile('src/utils/themeMeta.ts')

/** Every theme block with the two tokens this rule depends on. */
const themes = Array.from(variables.matchAll(/\[data-theme="([^"]+)"\]\s*\{([^}]*)\}/g)).map(
  ([, id, body]) => {
    const tokens = Object.fromEntries(
      Array.from(body.matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{3,6});/g)).map(([, k, v]) => [k, v]),
    )
    return {
      id,
      dark: new RegExp(`id:\\s*'${id}',\\s*dark:\\s*true`).test(themeMeta),
      bg: tokens['--bg-secondary'],
      accent: tokens['--accent-color'],
    }
  },
)

/** Per-category keep ratios declared on `.chat-tool-call[data-category="…"]`. */
function keepFor(block: string, selector: RegExp): Record<string, number> {
  const out: Record<string, number> = {}
  for (const [, cat, body] of block.matchAll(selector)) {
    const keep = body.match(/--tool-ink-keep:\s*(\d+)%/)
    if (keep) out[cat] = Number(keep[1])
  }
  return out
}

// Anchored at line start: the dark overrides repeat the same selector with a
// `:root[data-theme-base="dark"] …` prefix, and an unanchored match would let
// them overwrite the light values (which is exactly how the first version of
// this guard read a light accent as #a78bfa).
const lightRule = /(?:^|\n)\.chat-tool-call\[data-category="(\w+)"\]\s*\{([^}]*)\}/g

const lightKeep = keepFor(contentBlocks, lightRule)

/** `--tool-accent` per category on the light rules (file/plan use the theme accent). */
const lightAccent: Record<string, string> = {}
for (const [, cat, body] of contentBlocks.matchAll(lightRule)) {
  const value = body.match(/--tool-accent:\s*([^;]+);/)?.[1]?.trim()
  if (value && value.startsWith('#')) lightAccent[cat] = value
}

/** Dark overrides live in the non-scoped block at the end of the file. */
const darkBlock = contentBlocks.match(
  /:root\[data-theme-base="dark"\]\s*\.content-blocks\s*\.chat-tool-call\s*\{([^}]*)\}/,
)?.[1]
const darkKeepDefault = Number(darkBlock?.match(/--tool-ink-keep:\s*(\d+)%/)?.[1] ?? 0)
const darkKeepAccentRidden = Number(
  contentBlocks
    .match(
      /:root\[data-theme-base="dark"\]\s*\.content-blocks\s*\.chat-tool-call\[data-category="file"\],[\s\S]*?\{([^}]*)\}/,
    )?.[1]
    ?.match(/--tool-ink-keep:\s*(\d+)%/)?.[1] ?? 0,
)
const darkAccent: Record<string, string> = {}
for (const [, cat, body] of contentBlocks.matchAll(
  /:root\[data-theme-base="dark"\]\s*\.content-blocks\s*\.chat-tool-call\[data-category="(\w+)"\]\s*\{([^}]*)\}/g,
)) {
  const value = body.match(/--tool-accent:\s*(#[0-9a-fA-F]{3,6})/)?.[1]
  if (value) darkAccent[cat] = value
}

const CATEGORIES = ['file', 'bash', 'search', 'task', 'plan', 'agent', 'skill', 'ask', 'permission', 'thinking']

describe('ContentBlocks — tool name keeps its hue and stays readable', () => {
  it('paints the name with the category-derived ink, never the raw accent', () => {
    const rule = contentBlocks.match(/\.chat-tool-call \.tool-name\s*\{([^}]*)\}/)?.[1]
    expect(rule, '.tool-name rule must exist').not.toBeNull()
    expect(rule!).toMatch(/color:\s*var\(--tool-ink\)/)
    expect(rule!).not.toMatch(/color:\s*var\(--tool-accent\)/)
  })

  it('uses the same ink on the running group strip', () => {
    const rule = contentBlocks.match(
      /\.chat-tool-group-strip\[data-category\] \.tool-name\s*\{([^}]*)\}/,
    )?.[1]
    expect(rule, 'running strip rule must exist').not.toBeNull()
    expect(rule!).toMatch(/color:\s*var\(--tool-ink\)/)
  })

  it('gives the category-less fallback plain secondary text', () => {
    // --text-muted is already a quiet grey; mixing it darker only makes mud.
    const rule = contentBlocks.match(
      /\.chat-tool-call\[data-category="fallback"\]\s*\{([^}]*)\}/,
    )?.[1]
    expect(rule).toMatch(/--tool-ink:\s*var\(--text-secondary\)/)
  })

  it('declares an ink ratio for every category, on both bases', () => {
    for (const cat of CATEGORIES) {
      expect(lightKeep[cat], `light --tool-ink-keep for ${cat}`).toBeGreaterThan(0)
    }
    expect(darkKeepDefault).toBeGreaterThan(0)
    expect(darkKeepAccentRidden).toBeGreaterThan(0)
  })

  it('clears WCAG AA on every theme, at rest and on hover', () => {
    expect(themes.length).toBeGreaterThan(30)
    const unmeasured = themes.filter(t => !t.bg || !t.accent)
    expect(unmeasured.map(t => t.id), 'themes this guard cannot measure').toEqual([])

    const failures: string[] = []
    for (const theme of themes) {
      const anchor = hexToRgb(theme.dark ? '#fff' : '#000')
      for (const cat of CATEGORIES) {
        const accentHex = theme.dark
          ? darkAccent[cat] ?? theme.accent
          : lightAccent[cat] ?? theme.accent
        const accent = hexToRgb(accentHex)
        const keep = theme.dark
          ? cat === 'file' || cat === 'plan'
            ? darkKeepAccentRidden
            : darkKeepDefault
          : lightKeep[cat]
        const ink = mix(accent, anchor, keep / 100)
        for (const tint of [0.06, 0.12]) {
          const surface = mix(accent, hexToRgb(theme.bg), tint)
          const ratio = contrast(ink, surface)
          if (ratio < 4.5) {
            failures.push(`${theme.id}/${cat} @${tint * 100}%: ${ratio.toFixed(2)}:1`)
          }
        }
      }
    }
    expect(failures, 'category ink below 4.5:1').toEqual([])
  })

  it('keeps the ink visibly coloured, not a neutral grey', () => {
    // A hue that survives the mix is the whole point of the fix: at least one
    // channel must differ from the anchor by a real amount on the default
    // (light) theme set.
    const light = themes.filter(t => !t.dark)
    const hues = light.map(theme => {
      const accent = hexToRgb(lightAccent['task'])
      return mix(accent, [0, 0, 0], lightKeep['task'] / 100)
    })
    expect(hues.length).toBeGreaterThan(0)
    const [r, g, b] = hues[0]
    expect(Math.max(r, g, b) - Math.min(r, g, b)).toBeGreaterThan(20)
    expect(b).toBeLessThan(g)
  })
})

describe('ContentBlocks — the tool summary is legible on light themes', () => {
  const summaryOverride = contentBlocks.match(
    /:root\[data-theme-base="light"\]\s*\.content-blocks\s*\.chat-tool-call\s*\.tool-summary\s*\{([^}]*)\}/,
  )?.[1]
  const baseSummary = contentBlocks.match(/\.chat-tool-call \.tool-summary\s*\{([^}]*)\}/)?.[1]

  it('replaces the hint grey with a dimmed secondary text on light themes', () => {
    expect(baseSummary, 'base .tool-summary rule must exist').not.toBeNull()
    // Dark themes (and the unscoped base) keep --text-hint: it reads fine there.
    expect(baseSummary!).toMatch(/color:\s*var\(--text-hint\)/)
    expect(summaryOverride, 'light-theme summary override must exist').toBeTruthy()
    expect(summaryOverride!).toMatch(
      /color:\s*color-mix\(in srgb,\s*var\(--text-secondary\)\s*85%,\s*var\(--bg-secondary\)\)/,
    )
  })

  it('clears 3:1 on every light theme and category tint', () => {
    // The summary is supporting text, not body copy, so the bar here is 3:1 —
    // the point is that it can no longer dissolve into the pill (the old hint
    // grey measured 1.7–2.6:1). The share is read from the stylesheet, not
    // repeated here: a hard-coded copy would keep passing after the real value
    // was lowered.
    const share = Number(summaryOverride?.match(/(\d+)%/)?.[1] ?? 0) / 100
    expect(share).toBeGreaterThan(0.5)
    const light = themes.filter(t => !t.dark && t.bg && t.accent)
    expect(light.length).toBeGreaterThan(10)
    const failures: string[] = []
    for (const theme of light) {
      const surfaceBg = hexToRgb(theme.bg)
      const secondary = hexToRgb(
        (variables.match(
          new RegExp(`\\[data-theme="${theme.id}"\\]\\s*\\{([^}]*)\\}`),
        )?.[1] ?? '').match(/--text-secondary:\s*(#[0-9a-fA-F]{3,6})/)?.[1] ?? '#000000',
      )
      const ink = mix(secondary, surfaceBg, share)
      for (const cat of CATEGORIES) {
        const accent = hexToRgb(lightAccent[cat] ?? theme.accent)
        for (const tint of [0.06, 0.12]) {
          const ratio = contrast(ink, mix(accent, surfaceBg, tint))
          if (ratio < 3) failures.push(`${theme.id}/${cat} @${tint * 100}%: ${ratio.toFixed(2)}:1`)
        }
      }
    }
    expect(failures, 'tool summary below 3:1').toEqual([])
  })
})
