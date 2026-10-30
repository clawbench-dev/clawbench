import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The three header icon buttons (theme → live share → server) must be spaced
 * identically.
 *
 * They all sit in the same flex row with `gap: var(--space-2)`, and both the
 * share and the server button pull themselves 8dp closer with a negative
 * margin-left so the rhythm stays tight. That only holds while BOTH negative
 * margins actually apply. The share button also carries `.theme-quick-toggle`,
 * so `.header--has-meter .theme-quick-toggle { margin-left: 0 }` — same
 * specificity, later in the file — silently zeroed it whenever the usage
 * capsule was on screen. Measured on a 543px header: 61px between the theme and
 * share icons vs 50px between share and server, i.e. exactly the missing 8dp.
 *
 * jsdom has no CSS cascade, so a mount test cannot see this. The guard below
 * compares real specificities instead of asserting a selector string, so it
 * keeps working if the rule is rewritten as long as the share rule still wins.
 */

function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '')
}

const header = stripComments(readWebFile('src/components/common/AppHeader.vue'))

/** `[a, b, c]` for a compound/descendant selector. Ids/classes/elements only —
 *  enough for this file, which uses no attribute selectors or pseudo-classes. */
function specificity(selector: string): [number, number, number] {
  const ids = (selector.match(/#[\w-]+/g) ?? []).length
  const classes = (selector.match(/\.[\w-]+/g) ?? []).length
  const pseudos = (selector.match(/:(?!:)[\w-]+/g) ?? []).length
  const stripped = selector
    .replace(/#[\w-]+/g, ' ')
    .replace(/\.[\w-]+/g, ' ')
    .replace(/::?[\w-]+(\([^)]*\))?/g, ' ')
  const elements = stripped.split(/\s+/).filter(Boolean).length
  return [ids, classes + pseudos, elements]
}

function compare(a: [number, number, number], b: [number, number, number]): number {
  for (let i = 0; i < 3; i++) {
    if (a[i] !== b[i]) return a[i] - b[i]
  }
  return 0
}

/** Every rule in the component whose declarations mention `margin-left`, in
 *  source order. */
function marginRules(): { selector: string; decls: string; at: number }[] {
  const out: { selector: string; decls: string; at: number }[] = []
  const re = /([^{}]+)\{([^{}]*)\}/g
  let m: RegExpExecArray | null
  while ((m = re.exec(header)) !== null) {
    const selector = m[1].trim().replace(/\s+/g, ' ')
    if (!/margin-left\s*:/.test(m[2])) continue
    out.push({ selector, decls: m[2], at: m.index })
  }
  return out
}

function findRule(match: (selector: string) => boolean) {
  const rule = marginRules().find((r) => match(r.selector))
  expect(rule, 'expected a margin-left rule for that selector').toBeDefined()
  return rule!
}

function marginLeftOf(decls: string): string {
  return decls.match(/margin-left\s*:\s*([^;]+);/)?.[1].trim() ?? ''
}

describe('header icon cluster — equal spacing between theme, share and server', () => {
  it('finds the three rules it is meant to guard', () => {
    // Without this the suite could pass by matching nothing at all.
    expect(findRule((s) => s === '.theme-quick-toggle')).toBeDefined()
    expect(findRule((s) => s.includes('.live-share-toggle'))).toBeDefined()
    expect(findRule((s) => s === '.server-toggle')).toBeDefined()
  })

  it('the share button uses the same negative margin as the server button', () => {
    // Both tuck up against their left neighbour by 8dp; the icon gap is then
    // `--space-2` (4dp) - 8dp on both sides, i.e. symmetric.
    const share = marginLeftOf(findRule((s) => s.includes('.live-share-toggle')).decls)
    const server = marginLeftOf(findRule((s) => s === '.server-toggle').decls)
    expect(share).toBe(server)
    expect(share).toBe('calc(-8 * var(--dp))')
  })

  it('the share rule outranks `.header--has-meter .theme-quick-toggle`', () => {
    // This is the regression itself: with equal specificity the later rule wins
    // and the share button loses its negative margin while the capsule is shown.
    const hasMeter = findRule((s) => s === '.header--has-meter .theme-quick-toggle')
    const share = findRule((s) => s.includes('.live-share-toggle'))
    expect(hasMeter).toBeDefined()

    const cmp = compare(specificity(share.selector), specificity(hasMeter.selector))
    expect(
      cmp,
      `share rule "${share.selector}" must outrank "${hasMeter.selector}" ` +
        `(${specificity(share.selector).join(',')} vs ${specificity(hasMeter.selector).join(',')})`,
    ).toBeGreaterThan(0)
  })

  it('the share rule also outranks the bare .theme-quick-toggle auto margin', () => {
    // `.theme-quick-toggle { margin-left: auto }` would otherwise fling the
    // share button to the far right of the header.
    const base = findRule((s) => s === '.theme-quick-toggle')
    const share = findRule((s) => s.includes('.live-share-toggle'))
    expect(compare(specificity(share.selector), specificity(base.selector))).toBeGreaterThan(0)
  })
})
