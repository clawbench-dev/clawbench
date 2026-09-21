import { describe, it, expect } from 'vitest'
import { THEMES } from '@/utils/themeMeta'
import { contrastRatio } from '@/utils/tagColor'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The running-session indicator must be visible on every theme.
 *
 * The signal is a light band along the bottom edge of a running row (and of
 * the chat input button). It has to work on all 36 themes, which a fixed
 * colour cannot do: the original was a hardcoded green that read fine on dark
 * backgrounds and all but vanished on light ones (1.10:1 against the row,
 * 1.08:1 on the chat button). It is an animated *signal* — "this session is
 * running" — so being near-invisible is a functional failure, not a cosmetic
 * one.
 *
 * Three designs were tried, and the two rejected ones are pinned by tests
 * below because each looked reasonable until measured:
 *
 *   1. Fixed green — invisible on light themes (above).
 *   2. Accent tint across the whole row, plus a wide sweep band. This washed
 *      dark themes milky, because there the accent sits far above the row
 *      background (OKLab L +0.50, vs −0.40 on a light theme), so any alpha
 *      that showed the band also flooded the row. The band then read as a
 *      grey smudge on top of it.
 *   3. The current design: a 2px solid line with a short upward glow, no row
 *      tint at all. Confining the light to the bottom edge means it can be
 *      fully opaque without touching the row's colour.
 *
 * The "does not tint the row" test is what keeps design 2 from coming back,
 * and the contrast test keeps it visible.
 *
 * jsdom has no CSS engine and does not resolve `color-mix()`/`var()`, so this
 * parses variables.css directly and does the colour maths itself.
 */

// ── colour helpers ───────────────────────────────────────────────────────────

type RGB = [number, number, number]

function parseHex(hex: string): RGB {
  const m = /^#([0-9a-f]{6})$/i.exec(hex.trim())
  expect(m, `expected a 6-digit hex colour, got "${hex}"`).not.toBeNull()
  const v = m![1]
  return [
    parseInt(v.slice(0, 2), 16),
    parseInt(v.slice(2, 4), 16),
    parseInt(v.slice(4, 6), 16),
  ]
}

function toHex(c: RGB): string {
  return '#' + c.map((x) => Math.round(x).toString(16).padStart(2, '0')).join('')
}

/** Composite `fg` at `alpha` over an opaque `bg`. */
function over(fg: RGB, bg: RGB, alpha: number): RGB {
  return [
    fg[0] * alpha + bg[0] * (1 - alpha),
    fg[1] * alpha + bg[1] * (1 - alpha),
    fg[2] * alpha + bg[2] * (1 - alpha),
  ]
}

function luminance(c: RGB): number {
  const f = (x: number) => {
    const s = x / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2])
}

/** Perceptual lightness (OKLab L) — needed to compare across light/dark. */
function oklabLightness(c: RGB): number {
  const lin = (x: number) => {
    const s = x / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  const [r, g, b] = c.map(lin)
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
  return 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s
}

// ── stylesheet parsing ───────────────────────────────────────────────────────

const css = readWebFile('css/variables.css')

/** All `--name: value;` declarations inside a block body. */
function declarations(body: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const m of body.matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/g)) out[m[1]] = m[2].trim()
  return out
}

/** The body of a `selector { … }` block, or null when absent. */
function blockBody(selector: string): string | null {
  const m = css.match(
    new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([\\s\\S]*?)\\n\\}'),
  )
  return m ? m[1] : null
}

const rootVars = declarations(blockBody(':root')!)

/** Per-theme colour variables, keyed by theme id. */
const themeVars: Record<string, Record<string, string>> = {}
for (const theme of THEMES) {
  const body = blockBody(`[data-theme="${theme.id}"]`)
  expect(body, `variables.css should define theme "${theme.id}"`).not.toBeNull()
  themeVars[theme.id] = declarations(body!)
}

/** The variables in effect for a theme (theme block + :root fallbacks). */
function varsFor(themeId: string): Record<string, string> {
  return { ...rootVars, ...themeVars[themeId] }
}

/**
 * Resolve a token expression to an opaque colour plus its alpha.
 *
 * Handles the shapes the token uses: a bare reference to another token, a
 * plain colour, and `color-mix(in srgb, X N%, transparent)` — which does NOT
 * premultiply, so it yields X's exact colour at alpha N.
 */
function evalToken(
  raw: string,
  vars: Record<string, string>,
  depth = 0,
): { color: RGB; alpha: number } {
  expect(depth, `token chain too deep (cycle?): ${raw}`).toBeLessThan(5)

  const bare = raw.match(/^var\((--[a-z-]+)\)$/)
  if (bare) return evalToken(vars[bare[1]], vars, depth + 1)

  if (/^#[0-9a-f]{3,8}$/i.test(raw.trim())) {
    return { color: parseHex(raw.trim()), alpha: 1 }
  }

  const withAlpha = raw.match(/color-mix\(in srgb,\s*(.+?)\s*([\d.]+)%,\s*transparent\s*\)$/)
  if (withAlpha) {
    const inner = evalToken(withAlpha[1], vars, depth + 1)
    return { color: inner.color, alpha: Number(withAlpha[2]) / 100 }
  }

  throw new Error(`unrecognised token expression: ${raw}`)
}

// ── tests ────────────────────────────────────────────────────────────────────

/**
 * Floors measured across all 36 themes. The original fixed green scored
 * 1.10 / 1.39. Both tokens are translucent by design (a softer signal), so
 * these floors are set just under the measured worst case — enough to catch a
 * regression that makes the indicator hard to see, without pinning the exact
 * alpha.
 */
const MIN_LINE = 1.40

describe('running-session indicator is theme-derived and visible on every theme', () => {
  it('draws the band from the theme accent, not a literal colour', () => {
    expect(rootVars['--running-line'], '--running-line should be declared').toBeDefined()
    expect(rootVars['--running-line']).toMatch(/var\(--accent-color\)/)
    expect(rootVars['--running-line'], 'must not hardcode a colour').not.toMatch(/#[0-9a-f]{3,6}/i)
    // Blending the accent away is what turned earlier versions grey.
    expect(rootVars['--running-line']).not.toMatch(
      /var\(--(text-primary|text-secondary|text-muted)\)/,
    )
    expect(rootVars['--running-line']).not.toMatch(/,\s*(black|white)\s*\)/)
  })

  it('leaves the row background alone — the light is confined to the band', () => {
    // Design 2 tinted the whole row and washed dark themes milky. Guard the
    // property that fixed it: a running row must not set a background, and no
    // `--running-fill` token should exist to tempt it back.
    expect(rootVars['--running-fill'], '--running-fill should be gone').toBeUndefined()

    const list = readWebFile('src/components/session/SessionList.vue')
    // Only rules whose subject IS the running row — `.cross-session-row.running
    // .cross-session-item:hover` is a descendant rule and legitimately sets a
    // hover background, so a looser pattern would flag it.
    const runningRules = [
      ...list.matchAll(/\.(?:session-row|cross-session-row)\.running\s*\{[^}]*\}/g),
    ].map((m) => m[0])
    expect(runningRules.length, 'running-row rules should exist').toBeGreaterThan(0)
    for (const rule of runningRules) {
      expect(rule, `a running row must not paint a background: ${rule}`).not.toMatch(
        /background(-color)?\s*:/,
      )
    }
    // The modifier must not smuggle a fill in through a descendant either.
    expect(list, 'no running-row descendant should carry the old fill').not.toMatch(
      /\.(?:session-row|cross-session-row)\.running\s+[^{]*\{[^}]*var\(--running-fill\)/,
    )
  })

  it('draws every band surface from the token, with no hardcoded colour', () => {
    // Scoped to the rules that paint the signal — not a whole-file scan, which
    // would trip over unrelated uses (the chat input bar's context-usage gauge
    // is legitimately green).
    //
    // The two surfaces use different motifs on purpose: the session rows get a
    // bottom band (--running-line), the input bar's session button gets a
    // travelling sweep (--running-sweep), because the button is a small chip
    // rather than a full-width row.
    const cases: { file: string; selector: RegExp; token: string }[] = [
      {
        file: 'src/components/session/SessionList.vue',
        selector: /\.session-running-line::before\s*\{[^}]*\}/,
        token: '--running-glow',
      },
      {
        file: 'src/components/session/SessionList.vue',
        selector: /\.session-running-line::after\s*\{[^}]*\}/,
        token: '--running-line',
      },
      {
        file: 'src/components/chat/ChatInputBar.vue',
        selector: /\.chat-action-btn\.has-running::before\s*\{[^}]*\}/,
        token: '--running-sweep',
      },
    ]

    for (const { file, selector, token } of cases) {
      const rule = readWebFile(file).match(selector)?.[0]
      expect(rule, `${file}: ${selector} should exist`).toBeTruthy()
      expect(rule, `${file}: ${selector} should use var(${token})`).toContain(`var(${token})`)
      expect(rule, `${file}: ${selector} still hardcodes the old green`)
        .not.toMatch(/rgba?\(\s*34\s*,\s*197\s*,\s*94|#22c55e/i)
    }
  })

  it('keeps the band visible on every theme', () => {
    const failures: string[] = []
    for (const theme of THEMES) {
      const vars = varsFor(theme.id)
      const { color, alpha } = evalToken(vars['--running-line'], vars)
      // The band sits on the row background, which may be either surface.
      for (const bgToken of ['--bg-primary', '--bg-secondary']) {
        const raw = vars[bgToken]
        if (!raw) continue
        const bg = parseHex(raw)
        const ratio = contrastRatio(toHex(over(color, bg, alpha)), toHex(bg))
        if (ratio < MIN_LINE) {
          failures.push(`${theme.id} (${bgToken}): ${ratio.toFixed(2)}:1`)
        }
      }
    }
    expect(failures, `band below ${MIN_LINE}:1:\n${failures.join('\n')}`).toEqual([])
  })

  it('keeps the edge faintly lit between passes, without competing with the band', () => {
    // The travelling band only covers 80% of the row at any instant, so
    // whatever it is not covering must still show the static base glow —
    // otherwise the edge blinks off and on between passes. Two properties:
    // the glow is visible at all, and it stays clearly weaker than the band
    // so the band still reads as the moving highlight on top of it.
    const failures: string[] = []
    for (const theme of THEMES) {
      const vars = varsFor(theme.id)
      const glow = evalToken(vars['--running-glow'], vars)
      const band = evalToken(vars['--running-line'], vars)
      for (const bgToken of ['--bg-primary', '--bg-secondary']) {
        const raw = vars[bgToken]
        if (!raw) continue
        const bg = parseHex(raw)
        const glowRatio = contrastRatio(toHex(over(glow.color, bg, glow.alpha)), toHex(bg))
        const bandRatio = contrastRatio(toHex(over(band.color, bg, band.alpha)), toHex(bg))
        // Visible at all — it is a deliberate signal, not a rounding artefact.
        if (glowRatio < 1.03) {
          failures.push(`${theme.id} (${bgToken}): glow ${glowRatio.toFixed(3)}:1 — invisible`)
        }
        // Strictly weaker than the band, so the band stays the highlight.
        if (glowRatio >= bandRatio) {
          failures.push(
            `${theme.id} (${bgToken}): glow ${glowRatio.toFixed(2)} >= band ${bandRatio.toFixed(2)}`,
          )
        }
      }
    }
    expect(failures, `base glow:\n${failures.join('\n')}`).toEqual([])
  })

  it('keeps the theme colour instead of washing it out to grey', () => {
    // Design 2's other failure mode: forcing contrast by blending the accent
    // toward black/white muted every theme (github-light #4a90d9 → #ccd7e1,
    // saturation 0.66 → 0.09). Neither token may blend the accent away.
    for (const token of ['--running-glow', '--running-line', '--running-sweep']) {
      for (const theme of THEMES) {
        const vars = varsFor(theme.id)
        const accent = parseHex(vars['--accent-color'])
        const { color } = evalToken(vars[token], vars)
        expect(toHex(color), `${theme.id}: ${token} should be the accent's own colour`).toBe(
          toHex(accent),
        )
      }
    }
    // Both tokens are translucent — the band is a soft signal, not a hard
    // stripe. Resolve through a full theme's vars: both reference
    // `--accent-color`, which is declared per-theme rather than in `:root`,
    // so `rootVars` alone cannot resolve the chain.
    const vars = varsFor(THEMES[0].id)
    const lineAlpha = evalToken(vars['--running-line'], vars).alpha
    const sweepAlpha = evalToken(vars['--running-sweep'], vars).alpha
    expect(lineAlpha).toBeGreaterThan(0)
    expect(lineAlpha, 'the band should be translucent, not a solid stripe').toBeLessThan(1)
    expect(sweepAlpha).toBeGreaterThan(0)
    expect(sweepAlpha, 'sweep should be translucent').toBeLessThan(1)
  })

  it('keeps the session button sweep visible on every theme', () => {
    // The button's sweep sits over its own background (--bg-tertiary). It is a
    // small chip, so the bar is lower than the list band's — but it still has
    // to be visible, which the original fixed green was not (1.08:1 there).
    const failures: string[] = []
    for (const theme of THEMES) {
      const vars = varsFor(theme.id)
      const { color, alpha } = evalToken(vars['--running-sweep'], vars)
      const bg = parseHex(vars['--bg-tertiary'] ?? vars['--bg-primary'])
      const ratio = contrastRatio(toHex(over(color, bg, alpha)), toHex(bg))
      if (ratio < 1.30) failures.push(`${theme.id}: ${ratio.toFixed(2)}:1`)
    }
    expect(failures, `button sweep below 1.30:1:\n${failures.join('\n')}`).toEqual([])
  })

  it('lifts nothing outside the band, so no theme can go milky', () => {
    // The band's glow is masked to a few px at the row's bottom edge. Whatever
    // the design, it must not lift the row as a whole — that was the milky
    // wash. Assert the visible band is short, so the mask cannot silently
    // become a full-row gradient.
    const list = readWebFile('src/components/session/SessionList.vue')
    const rule = list.match(/\.session-running-line\s*\{[^}]*\}/)?.[0]
    expect(rule, '.session-running-line should exist').toBeTruthy()
    const height = Number(rule!.match(/height:\s*(\d+)px/)?.[1])
    expect(height, 'band height should be a small px value').toBeGreaterThan(0)
    expect(height, `band is ${height}px — too tall to be a bottom-edge glow`).toBeLessThanOrEqual(20)

    // The mask must actually fade out, or the glow would be a solid block.
    const after = list.match(/\.session-running-line::after\s*\{[^}]*\}/)?.[0]
    expect(after, '.session-running-line::after should exist').toBeTruthy()
    expect(after, 'glow needs a mask to fade upward').toMatch(/mask-image:\s*linear-gradient/)
    expect(after).toMatch(/transparent\s+\d+px/)
  })

  it('keeps the dark band no brighter than the light one relative to its row', () => {
    // A dark theme's accent sits ~+0.50 OKLab L above its background (light
    // themes ~−0.40), so an opaque band steps the dark row much further. That
    // is fine for a 2px band (it is the point — it must be visible), but it
    // should not be wildly out of scale. Guard the ratio.
    const isDark = (id: string) => oklabLightness(parseHex(varsFor(id)['--bg-primary'])) < 0.5
    const step = (id: string): number => {
      const vars = varsFor(id)
      const { color, alpha } = evalToken(vars['--running-line'], vars)
      const bg = parseHex(vars['--bg-primary'])
      return oklabLightness(over(color, bg, alpha)) - oklabLightness(bg)
    }

    const dark = THEMES.filter((t) => isDark(t.id)).map((t) => Math.abs(step(t.id)))
    const light = THEMES.filter((t) => !isDark(t.id)).map((t) => Math.abs(step(t.id)))
    expect(dark.length).toBeGreaterThan(0)
    expect(light.length).toBeGreaterThan(0)

    const avg = (xs: number[]) => xs.reduce((a, b) => a + b, 0) / xs.length
    const ratio = avg(dark) / avg(light)
    // Measured ~2.2x, which is inherent to opaque accents on the two bases.
    // Anything past 3x would mean a token change made dark themes glare.
    expect(ratio, `dark/light band step ratio is ${ratio.toFixed(2)}`).toBeLessThan(3)
  })
})
