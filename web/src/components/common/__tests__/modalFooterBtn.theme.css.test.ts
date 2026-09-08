import { describe, it, expect } from 'vitest'

/**
 * Theme-parity guards for the shared footer pill (.fbtn) stylesheet.
 *
 * Regression: when the per-dialog footer buttons were consolidated into the
 * shared .fbtn pill system, the neutral hover background stayed hard-coded to
 * the light-theme grey `#e5e7eb`. On dark themes the pill's rest background is
 * `var(--bg-tertiary)` (a dark grey), so hovering flipped the pill near-white
 * while the text stayed `var(--text-primary)` (light) — the label visually
 * blended into the hover background (e.g. the "Open page" link in the file
 * share dialog). A `[data-theme-base="dark"]` override now lightens the dark
 * pill with a text-primary tint instead.
 *
 * The tinted variants (.fbtn-danger/warn/success) also carry dark-theme
 * overrides so their deep light-theme text shades do not sink into the dark
 * tinted backgrounds (~2:1 contrast otherwise).
 */

async function rawCss(): Promise<string> {
  const mod = await import('@/assets/modal-footer-btn.css?raw')
  return String(mod.default)
}

describe('modal-footer-btn.css theme-adaptive hover', () => {
  it('keeps the light-theme hover grey but overrides it for dark themes with a theme-derived tint', async () => {
    const css = await rawCss()
    const lightHover = css.match(/\.fbtn:hover\s*\{[\s\S]*?\}/)?.[0]
    expect(lightHover).toContain('background: #e5e7eb')

    const darkHover = css.match(/\[data-theme-base="dark"\]\s*\.fbtn:hover\s*\{[\s\S]*?\}/)?.[0]
    expect(darkHover).toBeTruthy()
    expect(darkHover).toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--text-primary[^)]*\)\s*[^,]*,\s*var\(--bg-tertiary/,
    )
  })

  it('neutral .fbtn rest state keeps theme tokens for both background and foreground', async () => {
    const css = await rawCss()
    const restBlock = css.match(/\.fbtn\s*\{[\s\S]*?\}/)?.[0]
    expect(restBlock).toContain('background: var(--bg-tertiary, #f1f3f5)')
    expect(restBlock).toContain('color: var(--text-secondary, #4b5563)')
  })

  it('tinted variants carry dark-theme foreground overrides so labels stay readable', async () => {
    const css = await rawCss()
    for (const name of ['danger', 'warn', 'success']) {
      const darkRest = css.match(
        new RegExp(`\\[data-theme-base="dark"\\]\\s*\\.fbtn-${name}\\s*\\{[\\s\\S]*?\\}`),
      )?.[0]
      const darkHover = css.match(
        new RegExp(`\\[data-theme-base="dark"\\]\\s*\\.fbtn-${name}:hover\\s*\\{[\\s\\S]*?\\}`),
      )?.[0]
      expect(darkRest, `dark override for .fbtn-${name} rest`).toBeTruthy()
      expect(darkRest).toContain('color:')
      expect(darkHover, `dark override for .fbtn-${name}:hover`).toBeTruthy()
      expect(darkHover).toContain('color:')
    }
  })
})
