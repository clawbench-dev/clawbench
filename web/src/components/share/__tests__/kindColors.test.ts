import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The four share kinds must be visually distinguishable, and the mapping must
 * hold in every theme.
 *
 * jsdom has no CSS engine and never applies a scoped stylesheet, so the colours
 * are read off the source — the same pattern as wideDockIconSize.css.test.ts.
 *
 * The important assertion is the SECOND one: the colours may not come from
 * `--accent-color`. That token is a different colour in each of the 36 themes,
 * so a kind mapped to it collides with a status hue in some themes and not
 * others — the list would be colour-coded correctly on one theme and have two
 * identical icons on another. The status palette is the same in every theme.
 */

const SRC = join(__dirname, '..', 'ShareCenterPanel.vue')

function source(): string {
  return readFileSync(SRC, 'utf8')
}

/** Declaration block of the rule whose selector is exactly `sel`. */
function ruleDecls(css: string, sel: string): string | null {
  const stripped = css.replace(/\/\*[\s\S]*?\*\//g, '')
  const escaped = sel.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = stripped.match(new RegExp(`(?:^|[},])\\s*${escaped}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

const KINDS = ['dir', 'file', 'session', 'meeting'] as const

describe('share row kind colours', () => {
  it('gives every kind its own colour', () => {
    const src = source()
    const seen = new Map<string, string>()

    for (const kind of KINDS) {
      const decls = ruleDecls(src, `.share-kind-icon.kind-${kind}`)
      expect(decls, `.share-kind-icon.kind-${kind} must exist`).not.toBeNull()

      const color = decls!.match(/--kind-color:\s*var\((--[a-z-]+)\)/)?.[1]
      expect(color, `kind-${kind} must set --kind-color to a token`).toBeTruthy()

      const previous = seen.get(color!)
      expect(
        previous,
        `kind-${kind} reuses ${color}, already used by kind-${previous}`,
      ).toBeUndefined()
      seen.set(color!, kind)
    }

    expect(seen.size, 'all four kinds must differ').toBe(4)
  })

  it('does not take its colours from the accent token', () => {
    const src = source()
    for (const kind of KINDS) {
      const decls = ruleDecls(src, `.share-kind-icon.kind-${kind}`)!
      // The accent is theme-specific: on some themes it lands on top of a
      // status hue, which would collapse two kinds into the same colour.
      expect(decls, `kind-${kind} must not use --accent-color`).not.toContain('--accent-color')
    }
  })

  it('paints the icon from the per-kind variable, not a hard-coded colour', () => {
    const decls = ruleDecls(source(), '.share-kind-icon')
    expect(decls, '.share-kind-icon must exist').not.toBeNull()
    // Both the glyph and its tinted plate follow the one variable, so a kind
    // cannot end up with a coloured icon on a grey plate.
    expect(decls).toMatch(/color:\s*var\(--kind-color\)/)
    expect(decls).toMatch(/background:\s*color-mix\(in srgb,\s*var\(--kind-color\)/)
  })

  it('maps the four kinds to four distinct status tokens', () => {
    const src = source()
    const expected: Record<string, string> = {
      dir: '--color-info',
      file: '--color-green',
      session: '--color-purple',
      meeting: '--color-orange',
    }
    for (const [kind, token] of Object.entries(expected)) {
      const decls = ruleDecls(src, `.share-kind-icon.kind-${kind}`)!
      expect(decls, `kind-${kind} must map to ${token}`).toContain(`var(${token})`)
    }
  })
})
