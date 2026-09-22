import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Contract for the first-block margin reset in css/content.css.
 *
 * Why this exists: every heading/paragraph in `.markdown-body` carries a
 * leading margin (`h1` alone gets `margin-top: 1.5em`). On a container that
 * opens with `#`, that margin is pure waste — the container already supplies
 * its own inset, and `1.5em` is scaled by the heading's OWN (largest) font
 * size, so the gap is the biggest one on the page. Measured on the 13px
 * completion card this rule was written for, an opening h1 sat 33px down (25%
 * of its 132px collapsed budget); the file preview wasted 41px. (That card has
 * since become a plain notification, but the reset still serves the file
 * preview, the share page and the task prompt.)
 *
 * The trap this guards — and the reason it is asserted rather than trusted:
 * `.markdown-body` is built in TWO DOM shapes. Most call sites put rendered
 * blocks directly inside it, but the file preview (MarkdownPreview.vue, reused
 * by the share page) inserts an extra `.markdown-content` div as the direct
 * child. With only `.markdown-body > :first-child`, that rule matches the
 * wrapper — which has no margin of its own — and quietly does nothing, so the
 * preview keeps its 41px band while every test of the other call sites passes.
 * A plain "does the rule exist" assertion cannot catch that, so both shapes
 * are pinned explicitly.
 *
 * jsdom has no CSS engine, so these are source-sniffing checks — the same
 * pattern MarkdownPreviewWideLayout.css.test.ts uses. cwd differs between a
 * bare `vitest` run (web/) and scripts/vitest-run.sh (repo root), so probe both.
 */

function readFromRepo(relPath: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, relPath), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(relPath + ' not found from cwd: ' + process.cwd())
}

const contentCss = readFromRepo('css/content.css')

/** Comment-free copy — the file documents the very selectors it defines. */
const contentCode = contentCss.replace(/\/\*[\s\S]*?\*\//g, '')

/**
 * Declarations of the rule whose selector list contains `selector`.
 *
 * The rule under test is a two-selector list, so a naive
 * `selector\s*\{` regex never matches it. Normalise the selector list (split
 * on commas, collapse whitespace) and compare tokens instead.
 */
function declsOf(selector: string): string | null {
  for (const m of contentCode.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const selectors = m[1].split(',').map((s) => s.replace(/\s+/g, ' ').trim())
    if (selectors.includes(selector)) return m[2]
  }
  return null
}

describe('markdown-body first block has no leading margin', () => {
  it('resets margin-top on the first block of a .markdown-body', () => {
    const decls = declsOf('.markdown-body > :first-child')
    expect(decls, '.markdown-body > :first-child rule must exist').not.toBeNull()
    expect(decls).toMatch(/margin-top:\s*0/)
  })

  it('also covers the .markdown-content wrapper shape used by the file preview', () => {
    // The preview nests `.markdown-content` directly under `.markdown-body`, so
    // the plain `> :first-child` selector lands on the wrapper (margin 0) and
    // has no effect on the heading inside. Both selectors must be present.
    const decls = declsOf('.markdown-body > .markdown-content > :first-child')
    expect(decls, 'the .markdown-content variant must be covered').not.toBeNull()
    expect(decls).toMatch(/margin-top:\s*0/)
  })

  it('still declares the leading margin it is overriding', () => {
    // Guards the reverse mistake: dropping the `margin-top: 1.5em` base rule
    // would make the reset pass while silently flattening every heading's
    // separation from the content ABOVE it (i.e. all non-first blocks).
    expect(contentCode).toMatch(/\.markdown-body h1,[\s\S]*?\{[^}]*margin-top:\s*1\.5em/)
  })

  it('resets only the top margin, leaving bottom spacing intact', () => {
    // The reset must not become a blanket `margin: 0` — the first block still
    // needs its gap from the block BELOW it.
    const decls = declsOf('.markdown-body > :first-child')!
    expect(decls).not.toMatch(/margin-bottom/)
    expect(decls).not.toMatch(/margin:\s*0/)
  })

  it('matches the two DOM shapes the app actually renders', () => {
    // The reset is only correct if both shapes exist as assumed. If the preview
    // ever drops `.markdown-content` (or another call site adds one), the
    // selector pair is wrong and this test should be revisited.
    const preview = readFromRepo('src/components/file/MarkdownPreview.vue')
    expect(preview).toMatch(/class="markdown-body"[\s\S]{0,400}?class="markdown-content"/)

    // The other shape: blocks rendered straight into the markdown-body element.
    // (The completion card used to be the witness here; it is now a plain
    // notification with no Markdown, so the task prompt takes over the role.)
    const taskPrompt = readFromRepo('src/components/task/TaskOverviewTab.vue')
    expect(taskPrompt).toMatch(/class="prompt-body markdown-body"/)
    expect(taskPrompt).not.toContain('markdown-content')
  })
})
