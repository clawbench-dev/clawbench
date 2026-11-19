import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Issue #701 — the tool-call NAME must not be painted in the category accent.
 *
 * `--tool-accent` is a categorical hue, not body ink: on the light themes bash
 * lands at ≈2.5:1 and task/interrupt at ≈2.1:1 against `--bg-secondary` (WCAG AA
 * asks for 4.5:1), and a saturated child under a neutral parent also reads as a
 * stronger layer than the container it belongs to. The icon, the 6% tint and
 * the 15% outline keep encoding the category.
 *
 * jsdom does not load SFC <style>, so this is a source guard.
 */
function nameRule(src: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const rule = src.match(new RegExp(`(?:^|\\n)${escaped}\\s*\\{([^}]*)\\}`, 'm'))
  expect(rule, `${selector} rule must exist`).not.toBeNull()
  return rule![1]
}

describe('ContentBlocks — tool name contrast', () => {
  const src = readWebFile('src/components/chat/ContentBlocks.vue')

  it('paints the standalone pill name with the text palette, not the accent', () => {
    const rule = nameRule(src, '.chat-tool-call .tool-name')
    expect(rule).toMatch(/color:\s*var\(--text-secondary\)/)
    expect(rule).not.toMatch(/color:\s*var\(--tool-accent\)/)
  })

  it('keeps the running group strip on the same colour as a settled one', () => {
    // A running strip wore the category accent while running and snapped to
    // --text-secondary once settled — the name changed weight mid-turn for no
    // information the icon and ring did not already carry.
    const rule = nameRule(src, '.chat-tool-group-strip[data-category] .tool-name')
    expect(rule).toMatch(/color:\s*var\(--text-secondary\)/)
    expect(rule).not.toMatch(/color:\s*var\(--tool-accent\)/)
  })

  it('keeps --text-secondary ≥ 4.5:1 on every theme (the name is real text now)', () => {
    // Switching the name to --text-secondary only fixes the contrast if that
    // token itself clears AA. Three light themes did not: solarized-light
    // #586e75 on #eee8d5 = 4.39, everforest-light #6f7b80 on #f2e9d0 = 3.6,
    // ayu-light #777c81 on #f3f3f3 = 3.8. They are darkened in variables.css and
    // this guard keeps every theme honest.
    const css = readWebFile('css/variables.css')
    const themes = Array.from(css.matchAll(/(\[data-theme[^{]*)\{([^}]*)\}/g))
      .filter(([, name]) => /^\[data-theme="/.test(name.trim()))
      .map(([, name, body]) => ({
      name: name.trim(),
      tokens: Object.fromEntries(Array.from(body.matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{3,6});/g)).map(([, k, v]) => [k, v])),
    }))
    expect(themes.length).toBeGreaterThan(30)
    // A theme that writes the token as rgba/var would silently leave the check,
    // and an empty failing list would then pass for the wrong reason.
    const unparsed = themes.filter(
      t => t.tokens['--text-secondary'] && !/^#[0-9a-fA-F]{3,6}$/.test(t.tokens['--text-secondary']),
    )
    expect(unparsed.map(t => t.name), 'non-hex --text-secondary is not covered by this guard').toEqual([])
    const uncovered = themes.filter(t => !t.tokens['--text-secondary'] || !t.tokens['--bg-secondary'])
    expect(uncovered.map(t => t.name), 'themes this guard cannot check').toEqual([])
    const contrast = (a: string, b: string) => {
      const rgb = (hex: string) => {
        let h = hex.slice(1)
        if (h.length === 3) h = h.split('').map(c => c + c).join('')
        return [0, 2, 4].map(i => parseInt(h.slice(i, i + 2), 16))
      }
      const lum = (c: number[]) => {
        const [r, g, b] = c.map(v => {
          const x = v / 255
          return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4
        })
        return 0.2126 * r + 0.7152 * g + 0.0722 * b
      }
      const [la, lb] = [lum(rgb(a)), lum(rgb(b))].sort((x, y) => y - x)
      return (la + 0.05) / (lb + 0.05)
    }
    const failing = themes
      .filter(t => t.tokens['--text-secondary'] && t.tokens['--bg-secondary'])
      .map(t => [t.name, contrast(t.tokens['--text-secondary'], t.tokens['--bg-secondary'])] as const)
      .filter(([, ratio]) => ratio < 4.5)
      .map(([name, ratio]) => `${name}: ${ratio.toFixed(2)}`)
    expect(failing, `themes where --text-secondary drops below 4.5:1 on --bg-secondary`).toEqual([])
  })

  it('still encodes the category in the icon', () => {
    // The fix must not have taken the colour out of the card altogether.
    const icon = nameRule(src, '.chat-tool-call .tool-icon')
    expect(icon).toMatch(/--tool-accent/)
  })
})
