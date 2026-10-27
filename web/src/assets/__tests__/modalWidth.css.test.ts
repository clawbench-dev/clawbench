import { describe, expect, it } from 'vitest'
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Width contract for the two shared shells.
 *
 * There are exactly TWO width tiers and they are declared once, as tokens in
 * css/variables.css. ModalDialog and BottomSheet each expose `size="narrow" |
 * "wide"`, which renders a modifier class; modal-card.css maps that class onto
 * one of the two tokens. A call site therefore chooses a TIER and can never
 * invent a width.
 *
 * Two regressions are fenced here:
 *
 *  1. Per-call-site numbers. The codebase once had six dialog widths
 *     (380/420/480/520/560/720), each silently multiplied by a 1.3 fudge factor
 *     at render time, plus a `maxWidth` prop that kept the drift alive.
 *  2. Local `--modal-max-width` overrides. Four drawers widened themselves in
 *     their own (non-scoped) stylesheet — tool detail / session search /
 *     team member / content search — so the "two tiers" rule was true on paper
 *     and false in the product. Those overrides are gone; the `wide` tier is
 *     the only way to get a wide card now.
 *
 * jsdom has no CSS engine, so this reads the stylesheets. The narrow-viewport
 * half is a layout property (`.modal-dialog` is `width: 100%` inside an overlay
 * with side padding) and cannot bind below ~1000px, which is why phones are
 * unaffected by the cap and need no separate rule.
 */

const CARD_CSS = readWebFile('src/assets/modal-card.css')
const VARS_CSS = readWebFile('css/variables.css')

function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Declarations of the first rule whose selector list is exactly `selector`. */
function declsOf(css: string, selector: string): string | null {
  const pattern = selector.split(',').map((s) => escapeRe(s.trim())).join('\\s*,\\s*')
  const m = stripComments(css).match(new RegExp(`(?:^|[},])\\s*${pattern}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

describe('modal width tokens', () => {
  it('declares exactly two width tiers', () => {
    const m = stripComments(VARS_CSS).match(/:root\s*\{([\s\S]*?)\n\}/)
    expect(m, 'the :root block must exist').not.toBeNull()
    const tokens = [...m![1].matchAll(/(--modal-width-[a-z]+)\s*:/g)].map((x) => x[1]).sort()
    expect(tokens, 'adding a third tier is how the drift came back').toEqual([
      '--modal-width-narrow',
      '--modal-width-wide',
    ])
  })

  it('keeps both tiers scaled by --dp, not literal px', () => {
    const root = stripComments(VARS_CSS)
    for (const tier of ['narrow', 'wide']) {
      const m = root.match(new RegExp(`--modal-width-${tier}:\\s*([^;]+);`))
      expect(m, `--modal-width-${tier} must be declared`).not.toBeNull()
      expect(m![1], 'a literal px width would not follow the UI scale').toMatch(
        /calc\(\s*\d+\s*\*\s*var\(--dp\)\s*\)/,
      )
    }
  })

  it('makes narrow the smaller of the two', () => {
    const px = (tier: string): number => {
      const m = stripComments(VARS_CSS).match(
        new RegExp(`--modal-width-${tier}:\\s*calc\\(\\s*(\\d+)\\s*\\*\\s*var\\(--dp\\)\\s*\\)`),
      )
      expect(m, `--modal-width-${tier} must be a scaled number`).not.toBeNull()
      return Number(m![1])
    }
    expect(px('narrow')).toBeLessThan(px('wide'))
  })
})

describe('shared shell width', () => {
  it('falls back to the narrow tier when no modifier class is present', () => {
    const decls = declsOf(CARD_CSS, '.modal-dialog, .bs-panel.bs-wide-auto')
    expect(decls, 'the shared card rule must exist').not.toBeNull()
    expect(decls).toMatch(/max-width:\s*var\(--modal-max-width,\s*var\(--modal-width-narrow/)
  })

  it('maps the default and the narrow modifier onto the narrow tier', () => {
    const decls = declsOf(
      CARD_CSS,
      '.modal-dialog, .bs-panel.bs-wide-auto, .modal-dialog.modal-dialog--narrow, .bs-panel.bs-wide-auto.bs-panel--narrow',
    )
    expect(decls, 'the narrow-tier rule must exist').not.toBeNull()
    expect(decls).toMatch(/--modal-max-width:\s*var\(--modal-width-narrow\)/)
  })

  it('maps the wide modifier onto the wide tier', () => {
    const decls = declsOf(
      CARD_CSS,
      '.modal-dialog.modal-dialog--wide, .bs-panel.bs-wide-auto.bs-panel--wide',
    )
    expect(decls, 'the wide-tier rule must exist').not.toBeNull()
    expect(decls).toMatch(/--modal-max-width:\s*var\(--modal-width-wide\)/)
  })

  it('declares no width anywhere except those two tiers', () => {
    // A stray `--modal-max-width: calc(960 * var(--dp))` (or a bare px cap) is
    // exactly how four drawers ended up wider than the rest. The only legal
    // declarations are the three above, all pointing at a token.
    const stripped = stripComments(CARD_CSS)
    for (const m of stripped.matchAll(/--modal-max-width:\s*([^;]+);/g)) {
      expect(m[1], 'every width must come from a token').toMatch(
        /^var\(--modal-width-(?:narrow|wide)\)$/,
      )
    }
  })
})

describe('call sites cannot override the width', () => {
  const SRC_ROOTS = ['src/components', 'src/share', 'src/composables']

  function resolveWeb(rel: string): string {
    for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
      const p = join(base, rel)
      if (existsSync(p)) return p
    }
    throw new Error(`${rel} not found from cwd: ` + process.cwd())
  }

  function walk(rel: string, out: string[] = []): string[] {
    for (const entry of readdirSync(resolveWeb(rel))) {
      const child = `${rel}/${entry}`
      if (statSync(resolveWeb(child)).isDirectory()) walk(child, out)
      else if (entry.endsWith('.vue')) out.push(child)
    }
    return out
  }

  const allVueFiles = SRC_ROOTS.filter((r) => existsSync(resolveWeb(r))).flatMap((r) => walk(r))

  function styleBlocks(file: string): string {
    const sfc = readFileSync(resolveWeb(file), 'utf8')
    return stripComments(
      [...sfc.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1]).join('\n'),
    )
  }

  it('finds the components it is meant to scan', () => {
    // A guard that silently scans nothing passes forever.
    expect(allVueFiles.length).toBeGreaterThan(100)
  })

  it('no component sets --modal-max-width itself', () => {
    const offenders = allVueFiles.filter((f) => styleBlocks(f).includes('--modal-max-width'))
    expect(
      offenders,
      'width is chosen with size="narrow" | "wide"; a local --modal-max-width override is the drift this test exists to stop',
    ).toEqual([])
  })

  it('no dialog shell caps itself with a bare max-width number', () => {
    // The custom (non-shell) popups used to hard-code 320/380/420; they now use
    // the narrow token. This catches a new one being added.
    const shells =
      /\.(?:modal-dialog|bs-panel|dlg-box|ug-panel|up-panel|welcome-panel|install-box|settings-restart-dialog)[\w-]*\s*\{([^}]*)\}/g
    const offenders: string[] = []
    for (const file of allVueFiles) {
      for (const rule of styleBlocks(file).matchAll(shells)) {
        if (/max-width:\s*(?:calc\(|\d)/.test(rule[1])) offenders.push(file)
      }
    }
    expect(offenders, 'a dialog width must come from a --modal-width-* token').toEqual([])
  })
})
