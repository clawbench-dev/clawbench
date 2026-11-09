import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Contract: a <button> starts from the theme's text colour, not from the UA's.
 *
 * Why this exists: `<button>` is a form control, so the UA stylesheet sets
 * `color: buttontext` on it — pure white under `color-scheme: dark` — and it
 * does NOT inherit the text colour like a <span> would. A button that declares
 * its background/border but forgets `color` therefore renders pure white while
 * everything around it is warm theme text. That is exactly how the meeting
 * detail header's back and rename icons looked: two cold white glyphs next to a
 * cream title under the Gruvbox Dark theme. A scan of all 856 <button>s found
 * 60+ more in the same state, so the fix is the one global reset in base.css —
 * every rule that does set a colour outranks it.
 *
 * jsdom has no CSS engine, so this is a source-sniffing check, and base.css
 * lives outside the vitest source root (web/css): it is read off disk, probing
 * both cwd roots because a bare `vitest` run starts in web/ while
 * scripts/vitest-run.sh starts at the repo root.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Bodies of every rule whose selector list contains the given element selector. */
function ruleBodiesFor(css: string, element: string): string[] {
  const bodies: string[] = []
  const re = /([^{}]+)\{([^{}]*)\}/g
  let m: RegExpExecArray | null
  while ((m = re.exec(css))) {
    const selectors = m[1].split(',').map((s) => s.trim())
    if (selectors.includes(element)) bodies.push(m[2])
  }
  return bodies
}

const baseCss = stripComments(readWebFile('css/base.css'))

describe('control colour reset', () => {
  it('makes <button> inherit the themed text colour', () => {
    const bodies = ruleBodiesFor(baseCss, 'button')
    expect(bodies.length).toBeGreaterThan(0)
    const inherited = bodies.some((body) =>
      /(?:^|;)\s*color\s*:\s*inherit\s*(?:;|$)/.test(body.trim().replace(/\s+/g, ' ')),
    )
    expect(
      inherited,
      'base.css must keep `button { color: inherit }`: without it every button ' +
        'that does not declare a colour falls back to the UA button colour ' +
        '(pure white in dark themes) instead of the theme text colour',
    ).toBe(true)
  })

  it('covers every form control, not just <button>', () => {
    // Inputs/selects/textareas hit the same UA default (`fieldtext`), which is
    // also pure white under a dark scheme — measured in Chromium against this
    // sheet. They are part of the same reset, not a separate one.
    for (const el of ['button', 'input', 'select', 'textarea']) {
      const bodies = ruleBodiesFor(baseCss, el)
      expect(bodies.length, `${el} must be in the reset`).toBeGreaterThan(0)
      const inherited = bodies.some((body) =>
        /(?:^|;)\s*color\s*:\s*inherit\s*(?:;|$)/.test(body.trim().replace(/\s+/g, ' ')),
      )
      expect(inherited, `${el} must inherit the themed text colour`).toBe(true)
    }
  })
})

describe('meeting detail header icons', () => {
  it('declares an explicit colour for the back/rename buttons', () => {
    // These sit outside the ActionBar row, so they get neither its background
    // nor its `color: var(--text-secondary)`. Inheriting (now possible thanks to
    // the reset) would paint them like the title instead of like the row they
    // belong to, so the rule has to keep its own declaration.
    const vue = readWebFile('src/components/live/MeetingDetail.vue')
    const scoped = stripComments(vue)
    const block = /\.meeting-icon-btn\s*\{([^}]*)\}/.exec(scoped)
    expect(block, '.meeting-icon-btn rule not found').toBeTruthy()
    expect(block![1]).toMatch(/(?:^|;)\s*color\s*:\s*var\(--text-secondary\)/)
  })
})
