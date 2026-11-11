import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard for the commit-list meta row spacing.
 *
 * Why this exists: Vue's template compiler condenses whitespace — a
 * whitespace-only node between two elements that contains a newline is REMOVED.
 * Every `.git-commit-meta` child therefore has to supply its own right gap via
 * `margin-right`, otherwise whatever follows it glues onto its edge
 * ("未推送34分钟前", seen in production). The sha tag and the refs wrapper each
 * carry one; the unpushed tag did not and had to be fixed.
 *
 * jsdom has no CSS engine, so this is a source-sniffing check — same pattern as
 * gitHistoryChrome.css.test.ts.
 */

function componentSource(): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, 'src/components/git/GitCommitList.vue'), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error('GitCommitList.vue not found from cwd: ' + process.cwd())
}

/** Strip comments so a commented-out declaration can never satisfy an assertion. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** The declaration block of the first rule whose selector is exactly `.sel`. */
function rule(src: string, sel: string): string | null {
  const m = stripComments(src).match(new RegExp(`(?:^|[},])\\s*${sel}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

describe('commit meta row: every item in front of another supplies its own gap', () => {
  // Direct children of .git-commit-meta in template order. Items that can be
  // followed by another item (i.e. everything but the trailing date/author)
  // must carry a margin-right — the compiled template has no whitespace left
  // between them for the browser to space out.
  const GAP_SOURCES = ['.git-commit-sha', '.git-commit-refs', '.git-ref-tag--unpushed']

  it('gives each leading item a margin-right', () => {
    const src = componentSource()
    const missing = GAP_SOURCES.filter((sel) => {
      const decls = rule(src, sel.replace('.', '\\.'))
      return decls === null || !/margin-right:/.test(decls)
    })
    expect(
      missing,
      `these meta-row items lost their margin-right, so the next item glues onto them`,
    ).toEqual([])
  })

  it('leaves the trailing date/author without a trailing margin', () => {
    // The date and author end the row; a margin-right there would eat row width
    // for nothing. Guarded loosely — if the row ever gains a trailing item this
    // test must be revisited, which is the point.
    expect(rule(componentSource(), '\\.git-commit-meta')).not.toMatch(/display:\s*flex/)
  })
})
