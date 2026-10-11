import { describe, it, expect } from 'vitest'

/**
 * Contrast guard for the shared `.modal-close-btn` (ModalDialog + BottomSheet
 * headers).
 *
 * The button sits on the header's own tinted band (accent 6% over
 * `--bg-secondary`), so its resting fill must actually differ from that band in
 * EVERY theme. A transparent fill — the obvious first choice — measures exactly
 * 1.000 against the header, i.e. the button is invisible there.
 *
 * The fix derives the colour by mixing `--text-primary` into `--bg-secondary`,
 * which makes the direction correct by construction. This is the SAME recipe
 * the window-control cluster uses (AppHeader.vue) and the reason is the same:
 * `--bg-tertiary` / `--bg-elevated` are not reliable "one step from secondary"
 * ramps (15 of 36 themes sit under 1.12). See design-guide red line 4.
 *
 * jsdom cannot resolve `color-mix()` or `var()`, so — like the other source
 * guards — this reads the raw stylesheet and pins the RECIPE, not a hex, so
 * retuning any theme cannot silently reintroduce an invisible button.
 */

async function rawCss(): Promise<string> {
  const mod = await import('@/assets/modal-card.css?raw')
  return String(mod.default)
}

/** The `background:` a rule declares, matched by its full selector text. */
function bgOf(css: string, selector: string): string {
  const rule = css.match(new RegExp(`${escapeRe(selector)}\\s*\\{([\\s\\S]*?)\\}`))
  expect(rule, `${selector} rule must exist`).not.toBeNull()
  const bg = rule![1].match(/background:\s*([^;]+);/)
  expect(bg, `${selector} must declare a background`).not.toBeNull()
  return bg![1].trim()
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

describe('modal-close-btn resting fill derives from the header colour', () => {
  it('mixes --text-primary into --bg-secondary instead of a fixed step', async () => {
    const css = await rawCss()
    const bg = bgOf(css, '.modal-close-btn')
    expect(
      bg,
      'the fill must mix --text-primary into --bg-secondary; a transparent fill ' +
        'or a fixed step like --bg-tertiary is invisible on the header in many themes',
    ).toContain('color-mix(')
    expect(bg).toContain('var(--text-primary')
    expect(bg).toContain('var(--bg-secondary')
  })

  it('does not fall back to a fixed background step', async () => {
    const css = await rawCss()
    const bg = bgOf(css, '.modal-close-btn')
    for (const bad of ['--bg-tertiary', '--bg-elevated', '--bg-hover', '--bg-active']) {
      expect(bg, `${bad} is not a reliable one-step ramp across themes`).not.toContain(bad)
    }
  })

  it('keeps hover and active on the same recipe (deeper mix), not a different token', async () => {
    const css = await rawCss()
    const hover = bgOf(css, '.modal-close-btn:hover')
    const active = bgOf(css, '.modal-close-btn:active')
    for (const [name, bg] of [['hover', hover], ['active', active]] as const) {
      expect(bg, `${name} must stay on the color-mix recipe`).toContain('color-mix(')
      expect(bg).toContain('var(--text-primary')
      expect(bg).toContain('var(--bg-secondary')
      // Switching to --bg-primary would not be reliably "deeper" in every theme.
      expect(bg, `${name} must not switch to --bg-primary`).not.toContain('--bg-primary')
    }
  })

  it('deepens monotonically: active > hover > rest mix percentage', async () => {
    const css = await rawCss()
    const pct = (bg: string) => {
      const m = bg.match(/var\(--text-primary[^)]*\)\s+(\d+)%/)
      expect(m, `could not read mix percentage from: ${bg}`).not.toBeNull()
      return Number(m![1])
    }
    const rest = pct(bgOf(css, '.modal-close-btn'))
    const hover = pct(bgOf(css, '.modal-close-btn:hover'))
    const active = pct(bgOf(css, '.modal-close-btn:active'))
    expect(hover, 'hover must be a deeper mix than rest').toBeGreaterThan(rest)
    expect(active, 'active must be a deeper mix than hover').toBeGreaterThan(hover)
  })

  it('gates hover behind a real pointer so touch taps do not stick', async () => {
    const css = await rawCss()
    // The hover rule must live inside an @media (hover: hover) block; on touch
    // devices a bare :hover sticks after a tap and reads as a pressed button.
    expect(css).toMatch(/@media\s*\(hover:\s*hover\)\s*\{[\s\S]*?\.modal-close-btn:hover/)
  })
})
